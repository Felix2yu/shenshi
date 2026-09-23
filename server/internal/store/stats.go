package store

import (
	"database/sql"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// Stats 汇总统计视图所需数据。days 为趋势窗口长度。
func (s *Store) Stats(days int) (*model.Stats, error) {
	if days <= 0 {
		days = 30
	}
	// 上限 366：趋势循环按天展开，不封顶时 ?days=999999999 能把请求打爆。
	if days > 366 {
		days = 366
	}
	today := time.Now().Format("2006-01-02")
	from := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	st := &model.Stats{Trend: []model.TrendPoint{}, ByList: []model.CountByKey{}, ByPriority: []model.CountByKey{}, ByQuadrant: []model.CountByKey{}}

	// 总量：「欠账」统一含进行中，与智能清单角标口径一致。
	var openNull, doneNull sql.NullInt64
	_ = s.db.QueryRow(`SELECT
		SUM(CASE WHEN status IN ('todo','in_progress') THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='done' THEN 1 ELSE 0 END) FROM tasks`).Scan(&openNull, &doneNull)
	st.TotalOpen = int(openNull.Int64)
	st.TotalDone = int(doneNull.Int64)
	st.TotalAll = st.TotalOpen + st.TotalDone

	scalar := func(q string, args ...any) int {
		var n sql.NullInt64
		_ = s.db.QueryRow(q, args...).Scan(&n)
		return int(n.Int64)
	}
	st.DoneToday = scalar(`SELECT COUNT(*) FROM tasks WHERE status='done' AND substr(completed_at,1,10)=?`, today)
	st.DueToday = scalar(`SELECT COUNT(*) FROM tasks WHERE status IN ('todo','in_progress') AND due_date=?`, today)
	st.DueTodayDone = scalar(`SELECT COUNT(*) FROM tasks WHERE status='done' AND due_date=?`, today)
	st.Overdue = scalar(`SELECT COUNT(*) FROM tasks WHERE status IN ('todo','in_progress') AND due_date IS NOT NULL AND due_date<?`, today)
	if st.TotalAll > 0 {
		st.Completion = float64(st.TotalDone) / float64(st.TotalAll) * 100
	}

	// 趋势：每日新建 / 完成 / 专注分钟
	type point struct{ created, done, focus int }
	byDate := map[string]*point{}
	rows, err := s.db.Query(`
		SELECT d, SUM(c), SUM(x) FROM (
			SELECT substr(created_at,1,10) AS d, 1 AS c, 0 AS x FROM tasks WHERE substr(created_at,1,10) >= ?
			UNION ALL
			SELECT substr(completed_at,1,10) AS d, 0 AS c, 1 AS x FROM tasks WHERE completed_at IS NOT NULL AND substr(completed_at,1,10) >= ?
		) GROUP BY d`, from, from)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		var c, x sql.NullInt64
		if err := rows.Scan(&d, &c, &x); err != nil {
			rows.Close()
			return nil, err
		}
		byDate[d] = &point{created: int(c.Int64), done: int(x.Int64)}
	}
	rows.Close()

	fRows, err := s.db.Query(`SELECT substr(started_at,1,10) AS d, SUM(minutes) FROM focus_sessions WHERE substr(started_at,1,10) >= ? GROUP BY d`, from)
	if err != nil {
		return nil, err
	}
	for fRows.Next() {
		var d string
		var m sql.NullInt64
		if err := fRows.Scan(&d, &m); err != nil {
			fRows.Close()
			return nil, err
		}
		if p, ok := byDate[d]; ok {
			p.focus = int(m.Int64)
		} else {
			byDate[d] = &point{focus: int(m.Int64)}
		}
	}
	fRows.Close()

	for i := days - 1; i >= 0; i-- {
		d := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		p := byDate[d]
		tp := model.TrendPoint{Date: d}
		if p != nil {
			tp.Created, tp.Done, tp.Focus = p.created, p.done, p.focus
		}
		st.Trend = append(st.Trend, tp)
	}

	// 专注总时长（窗口内）
	st.FocusMinutes = scalar(`SELECT SUM(minutes) FROM focus_sessions WHERE substr(started_at,1,10) >= ?`, from)

	// 连续完成天数：从今天（或昨天）向前连续有完成记录的天数。
	st.StreakDays = s.streak()

	// 分组计数
	collect := func(q string, args ...any) []model.CountByKey {
		out := []model.CountByKey{}
		r, err := s.db.Query(q, args...)
		if err != nil {
			return out
		}
		defer r.Close()
		for r.Next() {
			var c model.CountByKey
			var n sql.NullInt64
			if err := r.Scan(&c.Key, &c.Label, &n); err != nil {
				return out
			}
			c.Count = int(n.Int64)
			out = append(out, c)
		}
		return out
	}

	st.ByList = collect(`
		SELECT CAST(l.id AS TEXT), l.name, COUNT(*) FROM tasks t JOIN lists l ON l.id = t.list_id
		WHERE t.status IN ('todo','in_progress') GROUP BY l.id ORDER BY COUNT(*) DESC, l.sort_order LIMIT 8`)

	st.ByPriority = collect(`
		SELECT CAST(priority AS TEXT), CASE priority WHEN 3 THEN '高' WHEN 2 THEN '中' WHEN 1 THEN '低' ELSE '无' END, COUNT(*)
		FROM tasks WHERE status IN ('todo','in_progress') GROUP BY priority ORDER BY priority DESC`)

	st.ByQuadrant = collect(`
		SELECT CAST(CASE WHEN t.important = 1 AND t.urgent = 1 THEN 1
		                 WHEN t.important = 1 THEN 2
		                 WHEN t.urgent = 1 THEN 3
		                 ELSE 4 END AS TEXT),
		       CASE WHEN t.important = 1 AND t.urgent = 1 THEN '重要且紧急'
		            WHEN t.important = 1 THEN '重要不紧急'
		            WHEN t.urgent = 1 THEN '紧急不重要'
		            ELSE '不紧急不重要' END,
		       COUNT(*)
		FROM tasks t WHERE t.status IN ('todo','in_progress')
		GROUP BY 1 ORDER BY 1`)

	return st, nil
}

// streak 计算连续完成天数（允许今天尚未完成，则从昨天起算）。
func (s *Store) streak() int {
	rows, err := s.db.Query(`SELECT DISTINCT substr(completed_at,1,10) AS d FROM tasks WHERE completed_at IS NOT NULL ORDER BY d DESC LIMIT 400`)
	if err != nil {
		return 0
	}
	days := []string{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return 0
		}
		days = append(days, d)
	}
	rows.Close()
	if len(days) == 0 {
		return 0
	}
	has := map[string]bool{}
	for _, d := range days {
		has[d] = true
	}
	cur := time.Now()
	if !has[cur.Format("2006-01-02")] {
		cur = cur.AddDate(0, 0, -1)
	}
	n := 0
	for i := 0; i < 400; i++ {
		if !has[cur.Format("2006-01-02")] {
			break
		}
		n++
		cur = cur.AddDate(0, 0, -1)
	}
	return n
}

// ---------- 日省（复盘） ----------

// ReviewInput 复盘入参。
type ReviewInput struct {
	Date     string `json:"date"`
	Mood     string `json:"mood"`
	Wins     string `json:"wins"`
	Blockers string `json:"blockers"`
	Tomorrow string `json:"tomorrow"`
}

// ListReviews 返回最近的复盘记录。
// ListReviews 列出复盘记录。limit==0 用默认条数，limit<0 表示取全部（导出用）。
func (s *Store) ListReviews(limit int) ([]model.Review, error) {
	if limit == 0 {
		limit = 30
	}
	q := `SELECT id, date, mood, wins, blockers, tomorrow, created_at, updated_at FROM reviews ORDER BY date DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Review{}
	for rows.Next() {
		var r model.Review
		if err := rows.Scan(&r.ID, &r.Date, &r.Mood, &r.Wins, &r.Blockers, &r.Tomorrow, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetReview 按日期取复盘，不存在时返回空记录（不报错），便于前端直接渲染表单。
func (s *Store) GetReview(date string) (*model.Review, error) {
	var r model.Review
	err := s.db.QueryRow(`SELECT id, date, mood, wins, blockers, tomorrow, created_at, updated_at FROM reviews WHERE date=?`, date).
		Scan(&r.ID, &r.Date, &r.Mood, &r.Wins, &r.Blockers, &r.Tomorrow, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return &model.Review{Date: date}, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UpsertReview 写入或更新某日复盘。
func (s *Store) UpsertReview(in ReviewInput) (*model.Review, error) {
	date := strings.TrimSpace(in.Date)
	if date == "" {
		date = time.Now().Format("2006-01-02")
	} else if err := checkDay(date); err != nil {
		return nil, err
	}
	ts := model.Now()
	_, err := s.db.Exec(`
		INSERT INTO reviews(date, mood, wins, blockers, tomorrow, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(date) DO UPDATE SET mood=excluded.mood, wins=excluded.wins, blockers=excluded.blockers, tomorrow=excluded.tomorrow, updated_at=excluded.updated_at`,
		date, in.Mood, in.Wins, in.Blockers, in.Tomorrow, ts, ts)
	if err != nil {
		return nil, err
	}
	return s.GetReview(date)
}

// ---------- 专注（番茄工作法） ----------

// FocusInput 专注记录入参。
type FocusInput struct {
	TaskID    *int64 `json:"taskId"`
	Minutes   int    `json:"minutes"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt"`
}

// AddFocus 记录一段专注。
func (s *Store) AddFocus(in FocusInput) (*model.FocusSession, error) {
	started := in.StartedAt
	if started == "" {
		started = model.Now()
	}
	ended := in.EndedAt
	if ended == "" {
		ended = model.Now()
	}
	if in.Minutes <= 0 {
		in.Minutes = 25
	}
	res, err := s.db.Exec(`INSERT INTO focus_sessions(task_id, minutes, started_at, ended_at) VALUES(?,?,?,?)`,
		in.TaskID, in.Minutes, started, ended)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.FocusSession{ID: id, TaskID: in.TaskID, Minutes: in.Minutes, StartedAt: started, EndedAt: ended}, nil
}

// ListFocus 返回最近专注记录。
// ListFocus 列出专注记录。limit==0 用默认条数，limit<0 表示取全部（导出用）。
func (s *Store) ListFocus(limit int) ([]model.FocusSession, error) {
	if limit == 0 {
		limit = 20
	}
	q := `
		SELECT f.id, f.task_id, COALESCE(t.title,''), f.minutes, f.started_at, f.ended_at
		FROM focus_sessions f LEFT JOIN tasks t ON t.id = f.task_id
		ORDER BY f.started_at DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.FocusSession{}
	for rows.Next() {
		var f model.FocusSession
		var taskID *int64
		if err := rows.Scan(&f.ID, &taskID, &f.TaskTitle, &f.Minutes, &f.StartedAt, &f.EndedAt); err != nil {
			return nil, err
		}
		f.TaskID = taskID
		out = append(out, f)
	}
	return out, rows.Err()
}
