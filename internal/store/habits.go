package store

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// ---------- 习惯 ----------

// HabitInput 习惯入参。用指针表示「未传」，与项目其余 UPDATE 语义一致。
type HabitInput struct {
	Name      *string  `json:"name"`
	Icon      *string  `json:"icon"`
	Color     *string  `json:"color"`
	Cadence   *string  `json:"cadence"`
	Weekdays  *string  `json:"weekdays"`
	Target    *int     `json:"target"`
	StartDate *string  `json:"startDate"`
	Note      *string  `json:"note"`
	Archived  *bool    `json:"archived"`
	SortOrder *float64 `json:"sortOrder"`
}

// CheckInput 打卡入参。Count 为空表示「加一次」，显式赋值表示「设为该次数」。
type CheckInput struct {
	Day   string  `json:"day"`
	Count *int    `json:"count"`
	Note  *string `json:"note"`
}

const habitCols = `id, name, icon, color, cadence, weekdays, target, start_date, note, archived, sort_order, created_at, updated_at`

func scanHabit(sc interface{ Scan(...any) error }) (model.Habit, error) {
	var h model.Habit
	var archived int
	err := sc.Scan(&h.ID, &h.Name, &h.Icon, &h.Color, &h.Cadence, &h.Weekdays, &h.Target,
		&h.StartDate, &h.Note, &archived, &h.SortOrder, &h.CreatedAt, &h.UpdatedAt)
	h.Archived = archived == 1
	return h, err
}

// Habits 返回习惯列表（默认不含已归档）。
func (s *Store) Habits(includeArchived bool) ([]model.Habit, error) {
	q := `SELECT ` + habitCols + ` FROM habits`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY sort_order, id`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Habit{}
	for rows.Next() {
		h, err := scanHabit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Habit 按 ID 取单个习惯。
func (s *Store) Habit(id int64) (*model.Habit, error) {
	row := s.db.QueryRow(`SELECT `+habitCols+` FROM habits WHERE id = ?`, id)
	h, err := scanHabit(row)
	if err != nil {
		return nil, ErrNotFound
	}
	return &h, nil
}

func normalizeHabitIn(in *HabitInput, creating bool) error {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return errBlank("习惯名称")
	}
	if in.Cadence != nil {
		c := strings.TrimSpace(*in.Cadence)
		if c != model.CadenceDaily && c != model.CadenceWeekly {
			return ValidationError{Msg: "节奏只能是 daily 或 weekly"}
		}
		*in.Cadence = c
	}
	if in.Target != nil && *in.Target < 1 {
		return ValidationError{Msg: "达标次数至少为 1"}
	}
	if in.Weekdays != nil {
		wd, err := normalizeWeekdays(*in.Weekdays)
		if err != nil {
			return err
		}
		*in.Weekdays = wd
		if *in.Weekdays == "" && in.Cadence != nil && *in.Cadence == model.CadenceWeekly {
			return ValidationError{Msg: "按周重复需要至少选择一个星期"}
		}
	}
	if in.StartDate != nil && *in.StartDate != "" {
		if err := checkDay(*in.StartDate); err != nil {
			return err
		}
	}
	_ = creating
	return nil
}

// CreateHabit 新建习惯。
func (s *Store) CreateHabit(in HabitInput) (*model.Habit, error) {
	if err := normalizeHabitIn(&in, true); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(derefStr(in.Name, ""))
	if name == "" {
		return nil, errBlank("习惯名称")
	}
	cadence := derefStr(in.Cadence, model.CadenceDaily)
	weekdays := derefStr(in.Weekdays, "")
	if cadence == model.CadenceWeekly && weekdays == "" {
		weekdays = "1,2,3,4,5,6,0" // 未指定即每日，避免出现「永远不该做」的习惯
	}
	target := 1
	if in.Target != nil {
		target = *in.Target
	}
	start := derefStr(in.StartDate, "")
	if start == "" {
		start = time.Now().Format("2006-01-02")
	}
	order := 0.0
	if in.SortOrder != nil {
		order = *in.SortOrder
	} else {
		var max *float64
		_ = s.db.QueryRow(`SELECT MAX(sort_order) FROM habits`).Scan(&max)
		if max != nil {
			order = *max + 1024
		}
	}
	ts := model.Now()
	res, err := s.db.Exec(`INSERT INTO habits(name, icon, color, cadence, weekdays, target, start_date, note, archived, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,0,?,?,?)`,
		name, derefStr(in.Icon, "habit"), derefStr(in.Color, "#6b7f6e"), cadence, weekdays, target, start,
		derefStr(in.Note, ""), order, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Habit(id)
}

// UpdateHabit 更新习惯。
func (s *Store) UpdateHabit(id int64, in HabitInput) (*model.Habit, error) {
	if err := normalizeHabitIn(&in, false); err != nil {
		return nil, err
	}
	sets, args := []string{}, []any{}
	add := func(col string, v any) {
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if in.Name != nil {
		add("name", strings.TrimSpace(*in.Name))
	}
	if in.Icon != nil {
		add("icon", *in.Icon)
	}
	if in.Color != nil {
		add("color", *in.Color)
	}
	if in.Cadence != nil {
		add("cadence", *in.Cadence)
	}
	if in.Weekdays != nil {
		add("weekdays", *in.Weekdays)
	}
	if in.Target != nil {
		add("target", *in.Target)
	}
	if in.StartDate != nil && *in.StartDate != "" {
		add("start_date", *in.StartDate)
	}
	if in.Note != nil {
		add("note", *in.Note)
	}
	if in.Archived != nil {
		add("archived", boolInt(*in.Archived))
	}
	if in.SortOrder != nil {
		add("sort_order", *in.SortOrder)
	}
	if len(sets) == 0 {
		return s.Habit(id)
	}
	add("updated_at", model.Now())
	args = append(args, id)
	if _, err := s.db.Exec(`UPDATE habits SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return nil, err
	}
	return s.Habit(id)
}

// DeleteHabit 删除习惯，打卡流水随外键级联删除。
func (s *Store) DeleteHabit(id int64) error {
	res, err := s.db.Exec(`DELETE FROM habits WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReorderHabits 按传入顺序重写习惯排序。
func (s *Store) ReorderHabits(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`UPDATE habits SET sort_order = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, id := range ids {
		if _, err := stmt.Exec(float64(i+1)*1024, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- 打卡 ----------

// AllHabitLogs 返回全部打卡流水（导出与连续天数计算需要完整历史）。
func (s *Store) AllHabitLogs() ([]model.HabitLog, error) {
	return s.habitLogs(``, nil)
}

// habitLogs 按可选区间读取流水。where 为空表示不限。
func (s *Store) habitLogs(where string, args []any) ([]model.HabitLog, error) {
	rows, err := s.db.Query(`SELECT id, habit_id, day, count, note, created_at FROM habit_logs`+where+` ORDER BY day, habit_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.HabitLog{}
	for rows.Next() {
		var l model.HabitLog
		if err := rows.Scan(&l.ID, &l.HabitID, &l.Day, &l.Count, &l.Note, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CheckIn 打卡。Count 为空表示加一次；减到 0 及以下即撤销当天记录。
func (s *Store) CheckIn(habitID int64, in CheckInput) (*model.HabitLog, error) {
	if err := checkDay(in.Day); err != nil {
		return nil, err
	}
	if _, err := s.Habit(habitID); err != nil {
		return nil, err
	}
	note := ""
	if in.Note != nil {
		note = *in.Note
	}
	if in.Count == nil {
		// 加一次：用 SQL 自增，避免「读出来 +1 再写回」在并发下丢更新。
		if _, err := s.db.Exec(`INSERT INTO habit_logs(habit_id, day, count, note, created_at) VALUES(?,?,1,?,?)
			ON CONFLICT(habit_id, day) DO UPDATE SET count = habit_logs.count + 1`+(ternary(in.Note != nil, ", note = excluded.note", "")),
			habitID, in.Day, note, model.Now()); err != nil {
			return nil, err
		}
	} else {
		if *in.Count <= 0 {
			if err := s.Uncheck(habitID, in.Day); err != nil {
				return nil, err
			}
			return nil, nil
		}
		// 显式设值：覆盖当天计数。
		if _, err := s.db.Exec(`INSERT INTO habit_logs(habit_id, day, count, note, created_at) VALUES(?,?,?,?,?)
			ON CONFLICT(habit_id, day) DO UPDATE SET count = excluded.count`+(ternary(in.Note != nil, ", note = excluded.note", "")),
			habitID, in.Day, *in.Count, note, model.Now()); err != nil {
			return nil, err
		}
	}
	var l model.HabitLog
	if err := s.db.QueryRow(`SELECT id, habit_id, day, count, note, created_at FROM habit_logs WHERE habit_id = ? AND day = ?`, habitID, in.Day).
		Scan(&l.ID, &l.HabitID, &l.Day, &l.Count, &l.Note, &l.CreatedAt); err != nil {
		return nil, err
	}
	return &l, nil
}

// Uncheck 撤销某天打卡。
func (s *Store) Uncheck(habitID int64, day string) error {
	if err := checkDay(day); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM habit_logs WHERE habit_id = ? AND day = ?`, habitID, day)
	return err
}

// HabitBoard 一次性返回习惯视图所需数据：区间内的流水 + 每个习惯的统计。
// 连续天数的计算需要完整历史，因此内部读取全量流水，只把区间内的部分返回给前端。
// includeArchived 为 true 时把已归档习惯一并带上，供「显示已归档」开关用；
// 归档习惯的字段带 archived 标记，统计照算，由调用方决定是否计入汇总。
func (s *Store) HabitBoard(from, to string, includeArchived bool) (*model.HabitBoard, error) {
	today := time.Now().Format("2006-01-02")
	if to == "" {
		to = today
	}
	if from == "" {
		from = time.Now().AddDate(0, 0, -83).Format("2006-01-02") // 12 周
	}
	if err := checkDay(from); err != nil {
		return nil, err
	}
	if err := checkDay(to); err != nil {
		return nil, err
	}
	if from > to {
		from, to = to, from
	}

	habits, err := s.Habits(includeArchived)
	if err != nil {
		return nil, err
	}
	all, err := s.AllHabitLogs()
	if err != nil {
		return nil, err
	}

	byHabit := map[int64]map[string]int{}
	firstLog := map[int64]string{}
	for _, l := range all {
		m, ok := byHabit[l.HabitID]
		if !ok {
			m = map[string]int{}
			byHabit[l.HabitID] = m
		}
		m[l.Day] += l.Count
		if cur, ok := firstLog[l.HabitID]; !ok || l.Day < cur {
			firstLog[l.HabitID] = l.Day
		}
	}

	board := &model.HabitBoard{From: from, To: to, Today: today, Habits: habits, Logs: []model.HabitLog{}, Stats: []model.HabitStat{}}
	for _, l := range all {
		if l.Day >= from && l.Day <= to {
			board.Logs = append(board.Logs, l)
		}
	}
	for _, h := range habits {
		board.Stats = append(board.Stats, habitStat(h, byHabit[h.ID], firstLog[h.ID], from, to, today))
	}
	return board, nil
}

// habitStat 计算单个习惯在区间内的统计。
func habitStat(h model.Habit, days map[string]int, firstLog, from, to, today string) model.HabitStat {
	st := model.HabitStat{HabitID: h.ID}
	met := func(day string) bool { return days[day] >= h.Target }

	st.TodayAt = days[today]
	st.Today = habitScheduled(h, today) && met(today)

	// 区间内「应达标」以今天为右界：未来的日子还没到，不该算作欠账。
	end := to
	if end > today {
		end = today
	}
	for d := from; d <= end; d = nextDay(d) {
		if !habitScheduled(h, d) {
			continue
		}
		st.Due++
		if met(d) {
			st.Done++
		}
	}
	if st.Due > 0 {
		st.Rate = float64(st.Done) / float64(st.Due)
	}

	// 历史最长连续：从起始日走到今天，跳过错峰的非计划日。
	// 补记在起始日之前的打卡同样计入，故起点取起始日与最早记录中更早的那个。
	run, best := 0, 0
	start := h.StartDate
	if firstLog != "" && firstLog < start {
		start = firstLog
	}
	limit := 0
	for d := start; d <= today && limit < 4000; d, limit = nextDay(d), limit+1 {
		if !habitScheduled(h, d) && days[d] == 0 {
			continue
		}
		if met(d) {
			run++
			if run > best {
				best = run
			}
		} else if habitScheduled(h, d) {
			run = 0
		}
	}
	st.Best = best

	// 当前连续：从今天往回数。今天尚未达标不算断，从昨天起算。
	//
	// 起始日之前的日期本不该「排期」，但如果那天确实打过卡（补记），
	// 就应该算进连续天数里，否则新建习惯后补记前几天会白记。
	cur := today
	if !met(today) {
		cur = prevDay(today)
	}
	streak, guard := 0, 0
	for guard < 4000 {
		guard++
		if !habitScheduled(h, cur) {
			if days[cur] > 0 {
				streak++
				cur = prevDay(cur)
				continue
			}
			if cur < h.StartDate {
				break // 既非排期日、又无记录，且已越过起始日，说明历史到此为止
			}
			cur = prevDay(cur)
			continue
		}
		if met(cur) {
			streak++
			cur = prevDay(cur)
			continue
		}
		break
	}
	st.Streak = streak
	return st
}

// HabitScheduled 判断某天该习惯是否排期。导出与测试也会用到，故导出。
func HabitScheduled(h model.Habit, day string) bool { return habitScheduled(h, day) }

func habitScheduled(h model.Habit, day string) bool {
	if day < h.StartDate {
		return false
	}
	if h.Cadence != model.CadenceWeekly {
		return true
	}
	list := parseWeekdays(h.Weekdays)
	if len(list) == 0 {
		return true
	}
	_, ok := list[weekdayOf(day)]
	return ok
}

// ---------- 小工具 ----------

func checkDay(day string) error {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return ValidationError{Msg: "日期格式应为 YYYY-MM-DD"}
	}
	return nil
}

func weekdayOf(day string) int {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return 0
	}
	return int(t.Weekday()) // 0 = 周日
}

func nextDay(day string) string {
	t, _ := time.Parse("2006-01-02", day)
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

func prevDay(day string) string {
	t, _ := time.Parse("2006-01-02", day)
	return t.AddDate(0, 0, -1).Format("2006-01-02")
}

// parseWeekdays 把 '1,3,5' 解析为集合。容忍中文逗号与空格。
func parseWeekdays(s string) map[int]struct{} {
	out := map[int]struct{}{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ' ' }) {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 0 || n > 6 {
			continue
		}
		out[n] = struct{}{}
	}
	return out
}

// normalizeWeekdays 去重、排序并规范化为 '0,1,2' 形式。
func normalizeWeekdays(s string) (string, error) {
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	set := parseWeekdays(s)
	if len(set) == 0 {
		return "", ValidationError{Msg: "星期取值应为 0-6（0 表示周日）"}
	}
	list := make([]int, 0, len(set))
	for n := range set {
		list = append(list, n)
	}
	sort.Ints(list)
	parts := make([]string, len(list))
	for i, n := range list {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ","), nil
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
