package store

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/yufei/shendu/server/internal/model"
)

// 导入模式。
const (
	ImportReplace = "replace" // 清空现有数据后按备份重建
	ImportMerge   = "merge"   // 保留现有数据，把备份作为副本追加进来
)

const exportVersion = 1

// ExportBundle 是全量备份的结构。Version 预留给将来做格式升级。
type ExportBundle struct {
	Version     int                  `json:"version"`
	App         string               `json:"app"`
	ExportedAt  string               `json:"exportedAt"`
	InboxListID int64                `json:"inboxListId"`
	Folders     []model.Folder       `json:"folders"`
	Lists       []model.List         `json:"lists"`
	Tasks       []model.Task         `json:"tasks"`
	Tags        []model.Tag          `json:"tags"`
	Reviews     []model.Review       `json:"reviews"`
	Focus       []model.FocusSession `json:"focus"`
	Habits      []model.Habit        `json:"habits"`
	HabitLogs   []model.HabitLog     `json:"habitLogs"`
	Settings    map[string]string    `json:"settings"`
}

// ImportResult 汇报各类数据的导入条数。
type ImportResult struct {
	Mode      string `json:"mode"`
	Folders   int    `json:"folders"`
	Lists     int    `json:"lists"`
	Tasks     int    `json:"tasks"`
	Tags      int    `json:"tags"`
	Reviews   int    `json:"reviews"`
	Focus     int    `json:"focus"`
	Habits    int    `json:"habits"`
	HabitLogs int    `json:"habitLogs"`
}

// txer 是 *sql.DB 与 *sql.Tx 共有的方法集，让同一套导入逻辑在事务内外都能复用。
// 方法签名必须与 database/sql 完全一致，否则 *sql.Tx 不满足该接口。
type txer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Export 导出全量数据。任务一律含子任务与标签，保证备份是自洽的。
func (s *Store) Export() (*ExportBundle, error) {
	folders, err := s.Folders()
	if err != nil {
		return nil, err
	}
	lists, err := s.Lists()
	if err != nil {
		return nil, err
	}
	tasks, err := s.ListTasks(TaskFilter{Status: "all", SortBy: "manual"})
	if err != nil {
		return nil, err
	}
	tags, err := s.Tags()
	if err != nil {
		return nil, err
	}
	// limit<0 表示取全部，否则备份会静默截断到默认条数。
	reviews, err := s.ListReviews(-1)
	if err != nil {
		return nil, err
	}
	focus, err := s.ListFocus(-1)
	if err != nil {
		return nil, err
	}
	settings, err := s.Settings()
	if err != nil {
		return nil, err
	}
	habits, err := s.Habits(true) // 含已归档：备份要能完整还原
	if err != nil {
		return nil, err
	}
	habitLogs, err := s.AllHabitLogs()
	if err != nil {
		return nil, err
	}
	inbox, err := s.InboxListID()
	if err != nil {
		return nil, err
	}

	// 分组里嵌套的清单与顶层 lists 重复，导出时清掉。
	for i := range folders {
		folders[i].Lists = nil
	}

	return &ExportBundle{
		Version:     exportVersion,
		App:         "慎始",
		ExportedAt:  model.Now(),
		InboxListID: inbox,
		Folders:     folders,
		Lists:       lists,
		Tasks:       tasks,
		Tags:        tags,
		Reviews:     reviews,
		Focus:       focus,
		Habits:      habits,
		HabitLogs:   habitLogs,
		Settings:    settings,
	}, nil
}

// ExportJSON 返回带缩进的 JSON，便于人工查看与 diff。
func (s *Store) ExportJSON() ([]byte, error) {
	b, err := s.Export()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(b, "", "  ")
}

var priorityLabel = map[int]string{0: "无", 1: "低", 2: "中", 3: "高"}

// ExportCSV 把任务摊平成一张表，供 Excel 或其它工具消费。
// 只导出任务本身——CSV 往返会丢失子任务与重复规则等结构，需要完整备份请用 JSON。
func (s *Store) ExportCSV() ([]byte, error) {
	lists, err := s.Lists()
	if err != nil {
		return nil, err
	}
	folders, err := s.Folders()
	if err != nil {
		return nil, err
	}
	tasks, err := s.ListTasks(TaskFilter{Status: "all", SortBy: "manual"})
	if err != nil {
		return nil, err
	}

	folderOf := map[int64]string{}
	for _, f := range folders {
		folderOf[f.ID] = f.Name
	}
	listOf := map[int64]model.List{}
	for _, l := range lists {
		listOf[l.ID] = l
	}

	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // BOM：让 Excel 正确识别 UTF-8
	w := csv.NewWriter(&buf)

	if err := w.Write([]string{
		"id", "标题", "清单", "分组", "状态", "优先级",
		"日期", "开始时间", "结束时间", "重复规则", "重要", "紧急",
		"标签", "子任务已完成", "子任务总数", "备注", "创建时间", "完成时间",
	}); err != nil {
		return nil, err
	}

	for _, t := range tasks {
		l := listOf[t.ListID]
		folder := ""
		if l.FolderID != nil {
			folder = folderOf[*l.FolderID]
		}
		status := "未完成"
		if t.Status == model.StatusDone {
			status = "已完成"
		}
		names := make([]string, 0, len(t.Tags))
		for _, g := range t.Tags {
			names = append(names, g.Name)
		}
		if err := w.Write([]string{
			strconv.FormatInt(t.ID, 10),
			t.Title,
			l.Name,
			folder,
			status,
			priorityLabel[t.Priority],
			derefStr(t.DueDate, ""),
			derefStr(t.DueTime, ""),
			derefStr(t.EndTime, ""),
			derefStr(t.RepeatRule, ""),
			boolWord(t.Important),
			boolWord(t.Urgent),
			strings.Join(names, " "),
			strconv.Itoa(t.SubtaskDone),
			strconv.Itoa(len(t.Subtasks)),
			t.Notes,
			t.CreatedAt,
			derefStr(t.CompletedAt, ""),
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func boolWord(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

// Import 导入备份。merge 重建 id 映射并把数据作为副本追加；
// replace 则在清空后按原 id 精确复原，用于灾难恢复。
func (s *Store) Import(b *ExportBundle, mode string) (*ImportResult, error) {
	if mode != ImportMerge && mode != ImportReplace {
		return nil, ValidationError{Msg: "导入模式只能是 merge 或 replace"}
	}
	if b == nil || (len(b.Lists) == 0 && len(b.Tasks) == 0 && len(b.Habits) == 0) {
		return nil, ValidationError{Msg: "备份内容为空，无法导入"}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res := &ImportResult{Mode: mode}
	if mode == ImportReplace {
		err = importReplace(tx, b, res)
	} else {
		err = importMerge(tx, b, res)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

func importReplace(tx txer, b *ExportBundle, res *ImportResult) error {
	// 按外键层级自上而下清空；reminder_log 一并清掉，免得旧台账挡住新数据的提醒。
	for _, stmt := range []string{
		`DELETE FROM task_tags`,
		`DELETE FROM subtasks`,
		`DELETE FROM tasks`,
		`DELETE FROM lists`,
		`DELETE FROM folders`,
		`DELETE FROM tags`,
		`DELETE FROM reviews`,
		`DELETE FROM focus_sessions`,
		`DELETE FROM habit_logs`,
		`DELETE FROM habits`,
		`DELETE FROM reminder_log`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}

	for _, f := range b.Folders {
		if _, err := tx.Exec(
			`INSERT INTO folders(id, name, color, icon, sort_order, collapsed, created_at) VALUES(?,?,?,?,?,?,?)`,
			f.ID, f.Name, f.Color, f.Icon, f.SortOrder, boolInt(f.Collapsed), stamp(f.CreatedAt),
		); err != nil {
			return err
		}
		res.Folders++
	}

	valid := map[int64]bool{}
	for _, l := range b.Lists {
		if _, err := tx.Exec(
			`INSERT INTO lists(id, folder_id, name, color, icon, sort_order, is_inbox, created_at) VALUES(?,?,?,?,?,?,?,?)`,
			l.ID, l.FolderID, l.Name, l.Color, l.Icon, l.SortOrder, boolInt(l.ID == b.InboxListID), stamp(l.CreatedAt),
		); err != nil {
			return err
		}
		valid[l.ID] = true
		res.Lists++
	}

	for _, g := range b.Tags {
		if _, err := tx.Exec(
			`INSERT INTO tags(id, name, color, created_at) VALUES(?,?,?,?)`,
			g.ID, g.Name, g.Color, stamp(g.CreatedAt),
		); err != nil {
			return err
		}
		res.Tags++
	}

	for _, t := range b.Tasks {
		// 备份若有损坏（任务指向不存在的清单），跳过它而不是让整次导入失败。
		if !valid[t.ListID] {
			continue
		}
		if err := insertTaskWithID(tx, t); err != nil {
			return err
		}
		res.Tasks++
	}

	for _, r := range b.Reviews {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO reviews(id, date, mood, wins, blockers, tomorrow, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, r.Date, r.Mood, r.Wins, r.Blockers, r.Tomorrow, stamp(r.CreatedAt), stamp(r.UpdatedAt),
		); err != nil {
			return err
		}
		res.Reviews++
	}

	for _, f := range b.Focus {
		if _, err := tx.Exec(
			`INSERT INTO focus_sessions(id, task_id, minutes, started_at, ended_at) VALUES(?,?,?,?,?)`,
			f.ID, f.TaskID, f.Minutes, stamp(f.StartedAt), stamp(f.EndedAt),
		); err != nil {
			return err
		}
		res.Focus++
	}

	for k, v := range b.Settings {
		if _, err := tx.Exec(
			`INSERT INTO settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v,
		); err != nil {
			return err
		}
	}

	habitOK := map[int64]bool{}
	for _, h := range b.Habits {
		if _, err := tx.Exec(`INSERT INTO habits(id, name, icon, color, cadence, weekdays, target, start_date, note, archived, sort_order, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.ID, h.Name, h.Icon, h.Color, habitCadenceOr(h.Cadence), h.Weekdays, habitTargetOr(h.Target),
			habitDayOr(h.StartDate), h.Note, boolInt(h.Archived), h.SortOrder, stamp(h.CreatedAt), stamp(h.UpdatedAt),
		); err != nil {
			return err
		}
		habitOK[h.ID] = true
		res.Habits++
	}
	for _, l := range b.HabitLogs {
		if !habitOK[l.HabitID] || l.Count <= 0 {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO habit_logs(id, habit_id, day, count, note, created_at) VALUES(?,?,?,?,?,?)`,
			l.ID, l.HabitID, l.Day, l.Count, l.Note, stamp(l.CreatedAt),
		); err != nil {
			return err
		}
		res.HabitLogs++
	}

	// 收件箱必须有且仅有一个，否则新建任务会找不到落点。
	return ensureInbox(tx)
}

func importMerge(tx txer, b *ExportBundle, res *ImportResult) error {
	// 分组与标签按名称合并到已有记录上；清单与任务一律作为新纪录追加。
	folderID, err := mergeFolders(tx, b.Folders, res)
	if err != nil {
		return err
	}
	tagID, err := mergeTags(tx, b.Tags, res)
	if err != nil {
		return err
	}

	listID := map[int64]int64{}
	for _, l := range b.Lists {
		var fid *int64
		if l.FolderID != nil {
			if mapped, ok := folderID[*l.FolderID]; ok {
				fid = &mapped
			}
		}
		// 备份里的收件箱不重复创建，直接并入现有收件箱。
		if l.ID == b.InboxListID {
			if existing, err := firstInboxID(tx); err == nil {
				listID[l.ID] = existing
				continue
			}
		}
		r, err := tx.Exec(
			`INSERT INTO lists(folder_id, name, color, icon, sort_order, is_inbox, created_at) VALUES(?,?,?,?,?,0,?)`,
			fid, l.Name, l.Color, l.Icon, l.SortOrder, stamp(l.CreatedAt),
		)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		listID[l.ID] = id
		res.Lists++
	}

	if err := ensureInbox(tx); err != nil {
		return err
	}

	for _, t := range b.Tasks {
		target, ok := listID[t.ListID]
		if !ok {
			continue // 备份里的清单缺失时跳过该任务，而不是整批失败
		}
		newID, err := insertTaskCopy(tx, t, target)
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(t.Tags))
		for _, g := range t.Tags {
			if mapped, ok := tagID[g.ID]; ok {
				ids = append(ids, mapped)
			}
		}
		if err := syncTaskTags(tx, newID, ids); err != nil {
			return err
		}
		res.Tasks++
	}

	for _, r := range b.Reviews {
		// 复盘按日期唯一，同一天保留现有内容。
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO reviews(date, mood, wins, blockers, tomorrow, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
			r.Date, r.Mood, r.Wins, r.Blockers, r.Tomorrow, stamp(r.CreatedAt), stamp(r.UpdatedAt),
		); err != nil {
			return err
		}
		res.Reviews++
	}

	for _, f := range b.Focus {
		// 专注记录原本指向的任务已被重新编号，不强行关联，避免挂到错误的任务上。
		if _, err := tx.Exec(
			`INSERT INTO focus_sessions(task_id, minutes, started_at, ended_at) VALUES(NULL,?,?,?)`,
			f.Minutes, stamp(f.StartedAt), stamp(f.EndedAt),
		); err != nil {
			return err
		}
		res.Focus++
	}

	// 习惯一律作为新纪录追加，并把打卡流水挂到新 id 上。
	habitID := map[int64]int64{}
	for _, h := range b.Habits {
		r, err := tx.Exec(`INSERT INTO habits(name, icon, color, cadence, weekdays, target, start_date, note, archived, sort_order, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.Name, h.Icon, h.Color, habitCadenceOr(h.Cadence), h.Weekdays, habitTargetOr(h.Target),
			habitDayOr(h.StartDate), h.Note, boolInt(h.Archived), h.SortOrder, stamp(h.CreatedAt), stamp(h.UpdatedAt),
		)
		if err != nil {
			return err
		}
		id, _ := r.LastInsertId()
		habitID[h.ID] = id
		res.Habits++
	}
	for _, l := range b.HabitLogs {
		target, ok := habitID[l.HabitID]
		if !ok || l.Count <= 0 {
			continue
		}
		// 同一天已有打卡则保留次数较多的一次，避免合并后进度倒退。
		if _, err := tx.Exec(`INSERT INTO habit_logs(habit_id, day, count, note, created_at) VALUES(?,?,?,?,?)
			ON CONFLICT(habit_id, day) DO UPDATE SET count = MAX(habit_logs.count, excluded.count)`,
			target, l.Day, l.Count, l.Note, stamp(l.CreatedAt)); err != nil {
			return err
		}
		res.HabitLogs++
	}

	return nil
}

func habitCadenceOr(s string) string {
	if s == model.CadenceWeekly {
		return model.CadenceWeekly
	}
	return model.CadenceDaily
}

func habitTargetOr(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func habitDayOr(s string) string {
	if err := checkDay(s); err != nil {
		return model.Now()[:10]
	}
	return s
}

func mergeFolders(tx txer, folders []model.Folder, res *ImportResult) (map[int64]int64, error) {
	existing := map[string]int64{}
	rows, err := tx.Query(`SELECT id, name FROM folders`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		existing[name] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mapped := map[int64]int64{}
	for _, f := range folders {
		if id, ok := existing[f.Name]; ok {
			mapped[f.ID] = id
			continue
		}
		r, err := tx.Exec(
			`INSERT INTO folders(name, color, icon, sort_order, collapsed, created_at) VALUES(?,?,?,?,?,?)`,
			f.Name, f.Color, f.Icon, f.SortOrder, boolInt(f.Collapsed), stamp(f.CreatedAt),
		)
		if err != nil {
			return nil, err
		}
		id, _ := r.LastInsertId()
		mapped[f.ID] = id
		existing[f.Name] = id
		res.Folders++
	}
	return mapped, nil
}

func mergeTags(tx txer, tags []model.Tag, res *ImportResult) (map[int64]int64, error) {
	existing := map[string]int64{}
	rows, err := tx.Query(`SELECT id, name FROM tags`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		existing[name] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mapped := map[int64]int64{}
	for _, g := range tags {
		if id, ok := existing[g.Name]; ok {
			mapped[g.ID] = id
			continue
		}
		r, err := tx.Exec(`INSERT INTO tags(name, color, created_at) VALUES(?,?,?)`, g.Name, g.Color, stamp(g.CreatedAt))
		if err != nil {
			return nil, err
		}
		id, _ := r.LastInsertId()
		mapped[g.ID] = id
		existing[g.Name] = id
		res.Tags++
	}
	return mapped, nil
}

// insertTaskWithID 按原 id 精确复原一条任务（replace 模式）。
func insertTaskWithID(tx txer, t model.Task) error {
	if _, err := tx.Exec(`INSERT INTO tasks(id, list_id, title, notes, status, priority, due_date, due_time, end_time, reminders, repeat_rule, important, urgent, completed_at, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ListID, t.Title, t.Notes, statusOr(t.Status), t.Priority,
		t.DueDate, t.DueTime, t.EndTime, mustJSON(nonNil(t.Reminders)), t.RepeatRule,
		boolInt(t.Important), boolInt(t.Urgent), t.CompletedAt, t.SortOrder,
		stamp(t.CreatedAt), stamp(t.UpdatedAt),
	); err != nil {
		return err
	}
	if err := insertSubtasks(tx, t, t.ID); err != nil {
		return err
	}
	for _, g := range t.Tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO task_tags(task_id, tag_id) VALUES(?,?)`, t.ID, g.ID); err != nil {
			return err
		}
	}
	return nil
}

// insertTaskCopy 追加一条任务副本，id 由数据库重新分配（merge 模式）。
func insertTaskCopy(tx txer, t model.Task, listID int64) (int64, error) {
	r, err := tx.Exec(`INSERT INTO tasks(list_id, title, notes, status, priority, due_date, due_time, end_time, reminders, repeat_rule, important, urgent, completed_at, sort_order, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		listID, t.Title, t.Notes, statusOr(t.Status), t.Priority,
		t.DueDate, t.DueTime, t.EndTime, mustJSON(nonNil(t.Reminders)), t.RepeatRule,
		boolInt(t.Important), boolInt(t.Urgent), t.CompletedAt, t.SortOrder,
		stamp(t.CreatedAt), stamp(t.UpdatedAt),
	)
	if err != nil {
		return 0, err
	}
	id, _ := r.LastInsertId()
	// 标签由调用方按 id 映射写入，这里只处理子任务。
	return id, insertSubtasks(tx, t, id)
}

func insertSubtasks(tx txer, t model.Task, taskID int64) error {
	for i, s := range t.Subtasks {
		order := s.SortOrder
		if order == 0 {
			order = (i + 1) * 1024
		}
		if _, err := tx.Exec(
			`INSERT INTO subtasks(task_id, title, done, sort_order) VALUES(?,?,?,?)`,
			taskID, s.Title, boolInt(s.Done), order,
		); err != nil {
			return err
		}
	}
	return nil
}

// ensureInbox 保证系统里有且仅有一个收件箱。
func ensureInbox(tx txer) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM lists WHERE is_inbox = 1`).Scan(&n); err != nil {
		return err
	}
	switch {
	case n == 0:
		var first int64
		err := tx.QueryRow(`SELECT id FROM lists ORDER BY id LIMIT 1`).Scan(&first)
		if err != nil {
			// 一个清单都没有：补一个空的收集箱，否则无处新建任务。
			_, err = tx.Exec(
				`INSERT INTO lists(name, color, icon, sort_order, is_inbox, created_at) VALUES('收集箱','#b4553d','inbox',0,1,?)`,
				model.Now(),
			)
			return err
		}
		_, err = tx.Exec(`UPDATE lists SET is_inbox = 1 WHERE id = ?`, first)
		return err
	case n > 1:
		var keep int64
		if err := tx.QueryRow(`SELECT id FROM lists WHERE is_inbox = 1 ORDER BY id LIMIT 1`).Scan(&keep); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE lists SET is_inbox = 0 WHERE is_inbox = 1 AND id <> ?`, keep)
		return err
	}
	return nil
}

func firstInboxID(tx txer) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM lists WHERE is_inbox = 1 ORDER BY id LIMIT 1`).Scan(&id)
	return id, err
}

func statusOr(s string) string {
	if s == model.StatusDone {
		return model.StatusDone
	}
	return model.StatusTodo
}

// stamp 补全缺失的时间戳，避免导入的空值污染排序与展示。
func stamp(s string) string {
	if strings.TrimSpace(s) == "" {
		return model.Now()
	}
	return s
}

func nonNil(v []int) []int {
	if v == nil {
		return []int{}
	}
	return v
}
