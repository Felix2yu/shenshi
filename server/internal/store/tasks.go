package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("记录不存在")

const taskSelect = `
SELECT t.id, t.list_id, t.title, t.notes, t.status, t.priority,
       t.due_date, t.due_time, t.end_time, t.reminders, t.repeat_rule,
       t.important, t.urgent, t.completed_at, t.sort_order, t.created_at, t.updated_at,
       l.name, l.color, l.folder_id
FROM tasks t JOIN lists l ON l.id = t.list_id`

// TaskFilter 是任务查询条件。零值表示不过滤。
type TaskFilter struct {
	Smart    string // inbox|today|next7|overdue|all|done|nodate
	ListID   *int64
	FolderID *int64
	Status   string // todo|done|all
	From     *string
	To       *string
	TagID    *int64
	Priority *int
	Quadrant string // 1=重要且紧急 2=重要不紧急 3=紧急不重要 4=都不
	Search   string
	SortBy   string // smart(默认)|manual|priority|due|created|title
	Limit    int
}

// where 把过滤器翻译为 WHERE 子句与绑定参数。所有值均通过占位符绑定，杜绝注入。
// statusOpenWithTodayDone 是智能清单的默认状态：未完成的事项，加上「今日已完成」的留存。
// 这样刚被勾掉的任务不会立刻消失，而是沉到列表末尾的「已完成」分区，
// 让「完成」这个动作有落地感（对应「敬终」）。
const statusOpenWithTodayDone = "open+today_done"

// where 返回查询条件与参数（用于列表查询）。
func (f TaskFilter) where() (string, []any) { return f.build(false) }

// whereCount 返回统计口径的条件：角标只关心「还剩多少未完成」，
// 因此不把今日已完成的事项计入。
func (f TaskFilter) whereCount() (string, []any) { return f.build(true) }

func (f TaskFilter) build(countOnly bool) (string, []any) {
	var where []string
	var args []any
	today := time.Now().Format("2006-01-02")
	in7 := time.Now().AddDate(0, 0, 7).Format("2006-01-02")

	status := f.Status
	switch f.Smart {
	case model.SmartInbox:
		where = append(where, "l.is_inbox = 1")
		if status == "" {
			status = statusOpenWithTodayDone
		}
	case model.SmartToday:
		// 「今天」包含今日到期与已逾期未完成的事项，避免遗漏。
		where = append(where, "t.due_date IS NOT NULL AND t.due_date <= ?")
		args = append(args, today)
		status = statusOpenWithTodayDone
	case model.SmartNext7:
		where = append(where, "t.due_date IS NOT NULL AND t.due_date > ? AND t.due_date <= ?")
		args = append(args, today, in7)
		status = statusOpenWithTodayDone
	case model.SmartOverdue:
		where = append(where, "t.due_date IS NOT NULL AND t.due_date < ?")
		args = append(args, today)
		status = statusOpenWithTodayDone
	case model.SmartNoDate:
		where = append(where, "t.due_date IS NULL")
		status = statusOpenWithTodayDone
	case model.SmartDone:
		status = model.StatusDone
	case model.SmartAll:
		if status == "" {
			status = "all"
		}
	}

	if countOnly && status == statusOpenWithTodayDone {
		status = model.StatusTodo
	}

	switch status {
	case model.StatusTodo:
		where = append(where, "t.status = 'todo'")
	case model.StatusDone:
		where = append(where, "t.status = 'done'")
	case statusOpenWithTodayDone:
		now := time.Now()
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Format(time.RFC3339)
		where = append(where, "(t.status = 'todo' OR (t.status = 'done' AND t.completed_at >= ?))")
		args = append(args, dayStart)
	}

	if f.ListID != nil {
		where = append(where, "t.list_id = ?")
		args = append(args, *f.ListID)
	}
	if f.FolderID != nil {
		where = append(where, "l.folder_id = ?")
		args = append(args, *f.FolderID)
	}
	if f.From != nil {
		where = append(where, "t.due_date >= ?")
		args = append(args, *f.From)
	}
	if f.To != nil {
		where = append(where, "t.due_date <= ?")
		args = append(args, *f.To)
	}
	if f.Priority != nil {
		where = append(where, "t.priority = ?")
		args = append(args, *f.Priority)
	}
	if f.TagID != nil {
		where = append(where, "EXISTS (SELECT 1 FROM task_tags tt WHERE tt.task_id = t.id AND tt.tag_id = ?)")
		args = append(args, *f.TagID)
	}
	if f.Search != "" {
		where = append(where, "(t.title LIKE ? OR t.notes LIKE ?)")
		like := "%" + f.Search + "%"
		args = append(args, like, like)
	}
	switch f.Quadrant {
	case "1":
		where = append(where, "t.important = 1 AND t.urgent = 1")
	case "2":
		where = append(where, "t.important = 1 AND t.urgent = 0")
	case "3":
		where = append(where, "t.important = 0 AND t.urgent = 1")
	case "4":
		where = append(where, "t.important = 0 AND t.urgent = 0")
	}

	// 纯计数场景不需要 ORDER BY，交由调用方决定。
	return strings.Join(where, " AND "), args
}

// CountTasks 返回满足条件的任务数量（未完成口径，用于角标）。
func (s *Store) CountTasks(f TaskFilter) (int, error) {
	where, args := f.whereCount()
	q := `SELECT COUNT(*) FROM tasks t JOIN lists l ON l.id = t.list_id`
	if where != "" {
		q += " WHERE " + where
	}
	var n int
	if err := s.db.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// 排序：已完成沉底；未完成按 到期日 → 时间 → 优先级 → 手工顺序。
// 已完成任务一律沉底，这是所有排序模式的共同前提。
const doneLast = `CASE WHEN t.status = 'done' THEN 1 ELSE 0 END`

// orderByFor 返回列表排序子句。
//
// 默认的 smart 模式让到期日与优先级主导顺序——这是「今天」这类视图应有的读法。
// 但要支持手动拖拽排序，就必须有一个让 sort_order 说了算的模式，即 manual；
// 否则用户拖完会发现顺序纹丝不动（sort_order 只是 smart 模式里的第五顺位）。
func orderByFor(sortBy string) string {
	switch sortBy {
	case "manual":
		return " ORDER BY " + doneLast + `, t.sort_order ASC, t.id ASC`
	case "priority":
		return " ORDER BY " + doneLast + `, t.priority DESC, t.sort_order ASC, t.id ASC`
	case "due":
		return " ORDER BY " + doneLast + `,
          CASE WHEN t.due_date IS NULL THEN 1 ELSE 0 END,
          t.due_date ASC, COALESCE(t.due_time, '99:99') ASC,
          t.sort_order ASC, t.id ASC`
	case "created":
		return " ORDER BY " + doneLast + `, t.created_at DESC, t.id DESC`
	case "title":
		return " ORDER BY " + doneLast + `, t.title COLLATE NOCASE ASC, t.id ASC`
	default:
		return " ORDER BY " + doneLast + `,
          CASE WHEN t.due_date IS NULL THEN 1 ELSE 0 END,
          t.due_date ASC, COALESCE(t.due_time, '99:99') ASC,
          t.priority DESC, t.sort_order ASC, t.id ASC`
	}
}

// ListTasks 按条件返回任务（含子任务与标签）。
func (s *Store) ListTasks(f TaskFilter) ([]model.Task, error) {
	where, args := f.where()
	q := taskSelect
	if where != "" {
		q += " WHERE " + where
	}
	q += orderByFor(f.SortBy)
	if f.Limit > 0 {
		q += " LIMIT " + strconv.Itoa(f.Limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}

	tasks := []model.Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	// 必须显式关闭游标后再发起后续查询，否则连接池可能被占满。
	rows.Close()

	if err := s.attachSubtasks(tasks); err != nil {
		return nil, err
	}
	if err := s.attachTags(tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// GetTask 返回单个任务。
func (s *Store) GetTask(id int64) (*model.Task, error) {
	rows, err := s.db.Query(taskSelect+" WHERE t.id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	t, err := scanTask(rows)
	if err != nil {
		return nil, err
	}
	rows.Close()
	list := []model.Task{t}
	if err := s.attachSubtasks(list); err != nil {
		return nil, err
	}
	if err := s.attachTags(list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(r rowScanner) (model.Task, error) {
	var t model.Task
	var dueDate, dueTime, endTime, repeatRule, completedAt sql.NullString
	var reminders string
	var important, urgent int
	var listName, listColor sql.NullString
	var folderID sql.NullInt64

	err := r.Scan(&t.ID, &t.ListID, &t.Title, &t.Notes, &t.Status, &t.Priority,
		&dueDate, &dueTime, &endTime, &reminders, &repeatRule,
		&important, &urgent, &completedAt, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt,
		&listName, &listColor, &folderID)
	if err != nil {
		return t, err
	}
	if dueDate.Valid {
		v := dueDate.String
		t.DueDate = &v
	}
	if dueTime.Valid {
		v := dueTime.String
		t.DueTime = &v
	}
	if endTime.Valid {
		v := endTime.String
		t.EndTime = &v
	}
	if repeatRule.Valid {
		v := repeatRule.String
		t.RepeatRule = &v
	}
	if completedAt.Valid {
		v := completedAt.String
		t.CompletedAt = &v
	}
	t.Important = important == 1
	t.Urgent = urgent == 1
	t.ListName = listName.String
	t.ListColor = listColor.String
	if folderID.Valid {
		v := folderID.Int64
		t.FolderID = &v
	}
	t.Reminders = []int{}
	if reminders != "" {
		_ = json.Unmarshal([]byte(reminders), &t.Reminders)
	}
	if t.Reminders == nil {
		t.Reminders = []int{}
	}
	return t, nil
}

func (s *Store) attachSubtasks(tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	idx := map[int64]int{}
	ids := make([]any, 0, len(tasks))
	ph := make([]string, 0, len(tasks))
	for i := range tasks {
		tasks[i].Subtasks = []model.Subtask{}
		idx[tasks[i].ID] = i
		ids = append(ids, tasks[i].ID)
		ph = append(ph, "?")
	}
	q := `SELECT id, task_id, title, done, sort_order FROM subtasks WHERE task_id IN (` + strings.Join(ph, ",") + `) ORDER BY sort_order, id`
	rows, err := s.db.Query(q, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sub model.Subtask
		var done int
		if err := rows.Scan(&sub.ID, &sub.TaskID, &sub.Title, &done, &sub.SortOrder); err != nil {
			return err
		}
		sub.Done = done == 1
		if i, ok := idx[sub.TaskID]; ok {
			tasks[i].Subtasks = append(tasks[i].Subtasks, sub)
			if sub.Done {
				tasks[i].SubtaskDone++
			} else {
				tasks[i].SubtaskOpen++
			}
		}
	}
	return rows.Err()
}

func (s *Store) attachTags(tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	idx := map[int64]int{}
	args := make([]any, 0, len(tasks))
	ph := make([]string, 0, len(tasks))
	for i := range tasks {
		tasks[i].Tags = []model.Tag{}
		idx[tasks[i].ID] = i
		args = append(args, tasks[i].ID)
		ph = append(ph, "?")
	}
	q := `SELECT tt.task_id, g.id, g.name, g.color FROM task_tags tt JOIN tags g ON g.id = tt.tag_id
	      WHERE tt.task_id IN (` + strings.Join(ph, ",") + `) ORDER BY g.name`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID int64
		var tag model.Tag
		if err := rows.Scan(&taskID, &tag.ID, &tag.Name, &tag.Color); err != nil {
			return err
		}
		if i, ok := idx[taskID]; ok {
			tasks[i].Tags = append(tasks[i].Tags, tag)
		}
	}
	return rows.Err()
}

// CreateTask 新建任务。未显式给出 important/urgent 时，按优先级与到期日推导，
// 让「四象限」开箱即用，同时保留用户手工调整的自由。
func (s *Store) CreateTask(in model.TaskInput, defaultListID int64) (*model.Task, error) {
	title := strings.TrimSpace(deref(in.Title, ""))
	if title == "" {
		return nil, ValidationError{Msg: "任务标题不能为空"}
	}
	listID := defaultListID
	if in.ListID.Set && in.ListID.Value > 0 {
		listID = in.ListID.Value
	}

	priority := deref(in.Priority, model.PriorityNone)
	dueDate := derefPtr(in.DueDate, nil)
	dueTime := derefPtr(in.DueTime, nil)
	endTime := derefPtr(in.EndTime, nil)
	repeatRule := derefPtr(in.RepeatRule, nil)
	if repeatRule != nil && strings.TrimSpace(*repeatRule) == "" {
		repeatRule = nil
	}

	important := deriveImportant(priority)
	if in.Important.Set {
		important = in.Important.Value
	}
	urgent := deriveUrgent(dueDate, false)
	if in.Urgent.Set {
		urgent = in.Urgent.Value
	}
	reminders := deref(in.Reminders, []int{0})
	if reminders == nil {
		reminders = []int{}
	}
	// 新任务默认追加到末尾。原实现取「当前毫秒 % 1000000」，会在约 16.7 分钟后回绕，
	// 使新任务插到列表最前面；改为读取当前最大值再加固定步长。
	sortOrder := deref(in.SortOrder, 0)
	if !in.SortOrder.Set {
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1024 FROM tasks`).Scan(&sortOrder); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ts := model.Now()
	res, err := tx.Exec(`INSERT INTO tasks(list_id, title, notes, status, priority, due_date, due_time, end_time, reminders, repeat_rule, important, urgent, sort_order, created_at, updated_at)
		VALUES(?,?,?,'todo',?,?,?,?,?,?,?,?,?,?,?)`,
		listID, title, deref(in.Notes, ""), priority, ptrStr(dueDate), ptrStr(dueTime), ptrStr(endTime),
		mustJSON(reminders), ptrStr(repeatRule), boolInt(important), boolInt(urgent), sortOrder, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()

	if in.Subtasks.Set {
		for i, sub := range in.Subtasks.Value {
			if strings.TrimSpace(sub.Title) == "" {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO subtasks(task_id, title, done, sort_order) VALUES(?,?,?,?)`,
				id, strings.TrimSpace(sub.Title), boolInt(sub.Done), i); err != nil {
				return nil, err
			}
		}
	}
	if in.TagIDs.Set {
		if err := syncTaskTags(tx, id, in.TagIDs.Value); err != nil {
			return nil, err
		}
	} else {
		// 从标题里提取的 #标签 由前端解析后通过 tagIds 传入；此处仅兜底。
		if err := syncTaskTags(tx, id, nil); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetTask(id)
}

// UpdateTask 按 PATCH 语义更新任务。
func (s *Store) UpdateTask(id int64, in model.TaskInput) (*model.Task, error) {
	cur, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}

	sets := []string{}
	args := []any{}
	add := func(expr string, v ...any) {
		sets = append(sets, expr)
		args = append(args, v...)
	}

	if in.Title.Set {
		t := strings.TrimSpace(in.Title.Value)
		if t == "" {
			return nil, ValidationError{Msg: "任务标题不能为空"}
		}
		add("title = ?", t)
	}
	if in.Notes.Set {
		add("notes = ?", in.Notes.Value)
	}
	if in.ListID.Set && in.ListID.Value > 0 {
		add("list_id = ?", in.ListID.Value)
	}
	if in.Priority.Set {
		add("priority = ?", in.Priority.Value)
	}
	if in.DueDate.Set {
		add("due_date = ?", ptrStr(in.DueDate.Value))
	}
	if in.DueTime.Set {
		add("due_time = ?", ptrStr(in.DueTime.Value))
	}
	if in.EndTime.Set {
		add("end_time = ?", ptrStr(in.EndTime.Value))
	}
	if in.Reminders.Set {
		r := in.Reminders.Value
		if r == nil {
			r = []int{}
		}
		add("reminders = ?", mustJSON(r))
	}
	if in.RepeatRule.Set {
		add("repeat_rule = ?", ptrStr(in.RepeatRule.Value))
	}
	if in.SortOrder.Set {
		add("sort_order = ?", in.SortOrder.Value)
	}

	// important / urgent 自动跟随优先级与到期日，除非本次显式指定。
	newPriority := cur.Priority
	if in.Priority.Set {
		newPriority = in.Priority.Value
	}
	newDue := cur.DueDate
	if in.DueDate.Set {
		newDue = in.DueDate.Value
		if newDue != nil && strings.TrimSpace(*newDue) == "" {
			newDue = nil
		}
	}
	if in.Important.Set {
		add("important = ?", boolInt(in.Important.Value))
	} else if in.Priority.Set {
		add("important = ?", boolInt(deriveImportant(newPriority)))
	}
	if in.Urgent.Set {
		add("urgent = ?", boolInt(in.Urgent.Value))
	} else if in.DueDate.Set {
		add("urgent = ?", boolInt(deriveUrgent(newDue, false)))
	}

	if in.Status.Set {
		switch in.Status.Value {
		case model.StatusDone:
			add("status = 'done'")
			add("completed_at = ?", model.Now())
		case model.StatusTodo:
			add("status = 'todo'")
			add("completed_at = NULL")
		}
	}

	add("updated_at = ?", model.Now())
	args = append(args, id)
	if _, err := s.db.Exec("UPDATE tasks SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
		return nil, err
	}

	if in.TagIDs.Set {
		if err := syncTaskTags(s.db, id, in.TagIDs.Value); err != nil {
			return nil, err
		}
	}
	if in.Subtasks.Set {
		if err := replaceSubtasks(s.db, id, in.Subtasks.Value); err != nil {
			return nil, err
		}
	}
	return s.GetTask(id)
}

// DeleteTask 删除任务。
func (s *Store) DeleteTask(id int64) error {
	res, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ToggleResult 描述一次完成/恢复操作的结果。
type ToggleResult struct {
	Task      *model.Task `json:"task"`
	NextTask  *model.Task `json:"nextTask"`  // 重复任务续期后生成的新任务
	Completed bool        `json:"completed"` // 本次是否为「完成」
}

// ToggleTask 切换完成状态。完成一个重复任务时，自动续期出下一次任务——
// 「敬终」之后立即「慎始」，让循环不断。
func (s *Store) ToggleTask(id int64) (*ToggleResult, error) {
	cur, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	ts := model.Now()
	if cur.Status == model.StatusTodo {
		if _, err := s.db.Exec(`UPDATE tasks SET status='done', completed_at=?, updated_at=? WHERE id=?`, ts, ts, id); err != nil {
			return nil, err
		}
		res := &ToggleResult{Completed: true}
		if cur.RepeatRule != nil && *cur.RepeatRule != "" {
			base := time.Now()
			if cur.DueDate != nil {
				if d, err := time.ParseInLocation("2006-01-02", *cur.DueDate, time.Local); err == nil {
					base = d
				}
			}
			if next, nextRule, ok := NextOccurrence(*cur.RepeatRule, base); ok {
				nd := next.Format("2006-01-02")
				in := model.TaskInput{
					Title:      optOf(cur.Title),
					Notes:      optOf(cur.Notes),
					ListID:     optOf(cur.ListID),
					Priority:   optOf(cur.Priority),
					DueDate:    optOfPtr(&nd),
					DueTime:    optOfPtr(cur.DueTime),
					EndTime:    optOfPtr(cur.EndTime),
					Reminders:  optOf(cur.Reminders),
					RepeatRule: optOfPtr(&nextRule),
				}
				in.Important = optOf(cur.Important)
				in.Urgent = optOf(deriveUrgent(&nd, false))
				if nt, err := s.CreateTask(in, cur.ListID); err == nil {
					ids := make([]int64, 0, len(cur.Tags))
					for _, tg := range cur.Tags {
						ids = append(ids, tg.ID)
					}
					if len(ids) > 0 {
						_ = syncTaskTags(s.db, nt.ID, ids)
					}
					res.NextTask, _ = s.GetTask(nt.ID)
				}
			}
		}
		res.Task, err = s.GetTask(id)
		if err != nil {
			return nil, err
		}
		return res, nil
	}
	if _, err := s.db.Exec(`UPDATE tasks SET status='todo', completed_at=NULL, updated_at=? WHERE id=?`, ts, id); err != nil {
		return nil, err
	}
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	return &ToggleResult{Task: t, Completed: false}, nil
}

// SkipTask 跳过重复任务的本次发生：只把日期推进到下一次，不记为完成。
// 与 ToggleTask 的区别在于不写 completed_at、也不留下新的任务副本——
// 语义是「这次不做了，下次照旧」，用于出差、生病等需要整次跳过的情况。
func (s *Store) SkipTask(id int64) (*model.Task, error) {
	cur, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	if cur.RepeatRule == nil || strings.TrimSpace(*cur.RepeatRule) == "" {
		return nil, ValidationError{Msg: "该任务没有重复规则，无法跳过本次"}
	}
	base := time.Now()
	if cur.DueDate != nil {
		if d, err := time.ParseInLocation("2006-01-02", *cur.DueDate, time.Local); err == nil {
			base = d
		}
	}
	next, nextRule, ok := NextOccurrence(*cur.RepeatRule, base)
	if !ok {
		return nil, ValidationError{Msg: "重复规则已无后续，无法跳过"}
	}
	nd := next.Format("2006-01-02")
	if _, err := s.db.Exec(
		`UPDATE tasks SET due_date = ?, repeat_rule = ?, urgent = ?, updated_at = ? WHERE id = ?`,
		nd, nextRule, boolInt(deriveUrgent(&nd, false)), model.Now(), id,
	); err != nil {
		return nil, err
	}
	return s.GetTask(id)
}

// MoveTask 用于日历/看板拖拽：改期或改清单。
// dueDate / dueTime 传入空串表示清空该字段（与「未传」区分）。
func (s *Store) MoveTask(id int64, listID *int64, dueDate *string, dueTime *string) (*model.Task, error) {
	sets := []string{"updated_at = ?"}
	args := []any{model.Now()}
	if listID != nil {
		sets = append(sets, "list_id = ?")
		args = append(args, *listID)
	}
	if dueDate != nil {
		sets = append(sets, "due_date = ?")
		args = append(args, nullableStr(*dueDate))
		sets = append(sets, "urgent = ?")
		if *dueDate == "" {
			args = append(args, boolInt(false))
		} else {
			args = append(args, boolInt(deriveUrgent(dueDate, false)))
		}
	}
	if dueTime != nil {
		sets = append(sets, "due_time = ?")
		args = append(args, nullableStr(*dueTime))
	}
	args = append(args, id)
	if _, err := s.db.Exec("UPDATE tasks SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
		return nil, err
	}
	return s.GetTask(id)
}

// nullableStr 把空串归一为 NULL，避免空日期参与比较时造成误判。
func nullableStr(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// ptrStr 与 nullableStr 同理，但接受指针。
func ptrStr(p *string) any {
	if p == nil {
		return nil
	}
	return nullableStr(*p)
}

// BatchAction 批量操作：complete | reopen | delete | move。
func (s *Store) BatchAction(ids []int64, action string, listID *int64, dueDate *string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	n := 0
	for _, id := range ids {
		var err error
		switch action {
		case "complete":
			if _, err = s.ToggleTask(id); err == nil {
				n++
			}
		case "reopen":
			if _, err = s.db.Exec(`UPDATE tasks SET status='todo', completed_at=NULL, updated_at=? WHERE id=?`, model.Now(), id); err == nil {
				n++
			}
		case "delete":
			if err = s.DeleteTask(id); err == nil {
				n++
			}
		case "move":
			if _, err = s.MoveTask(id, listID, dueDate, nil); err == nil {
				n++
			}
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return n, err
		}
	}
	return n, nil
}

// ReorderTasks 按传入的 id 顺序重写 sort_order，供手动拖拽排序落库。
// 步长取 1024，给「插到两者之间」留出余地，避免每次都全量重排。
func (s *Store) ReorderTasks(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ts := model.Now()
	stmt, err := tx.Prepare(`UPDATE tasks SET sort_order = ?, updated_at = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, id := range ids {
		if _, err := stmt.Exec(float64(i+1)*1024, ts, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AddSubtask 新增子任务。
func (s *Store) AddSubtask(taskID int64, title string, position int) (*model.Subtask, error) {
	var maxOrder sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(sort_order) FROM subtasks WHERE task_id = ?`, taskID).Scan(&maxOrder)
	order := int(maxOrder.Int64) + 1
	if position >= 0 {
		order = position
	}
	res, err := s.db.Exec(`INSERT INTO subtasks(task_id, title, done, sort_order) VALUES(?,?,0,?)`, taskID, strings.TrimSpace(title), order)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Subtask{ID: id, TaskID: taskID, Title: title, SortOrder: order}, nil
}

// UpdateSubtask 更新子任务。
func (s *Store) UpdateSubtask(id int64, title *string, done *bool, sortOrder *int) error {
	sets := []string{}
	args := []any{}
	if title != nil {
		sets = append(sets, "title = ?")
		args = append(args, strings.TrimSpace(*title))
	}
	if done != nil {
		sets = append(sets, "done = ?")
		args = append(args, boolInt(*done))
	}
	if sortOrder != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *sortOrder)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	res, err := s.db.Exec("UPDATE subtasks SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// 子任务变动后刷新父任务的更新时间，便于同步与排序。
	_, _ = s.db.Exec(`UPDATE tasks SET updated_at = ? WHERE id = (SELECT task_id FROM subtasks WHERE id = ?)`, model.Now(), id)
	return nil
}

// DeleteSubtask 删除子任务。
func (s *Store) DeleteSubtask(id int64) error {
	_, err := s.db.Exec(`DELETE FROM subtasks WHERE id = ?`, id)
	return err
}

func replaceSubtasks(db execer, taskID int64, subs []model.Subtask) error {
	if _, err := db.Exec(`DELETE FROM subtasks WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for i, sub := range subs {
		if strings.TrimSpace(sub.Title) == "" {
			continue
		}
		if _, err := db.Exec(`INSERT INTO subtasks(task_id, title, done, sort_order) VALUES(?,?,?,?)`,
			taskID, strings.TrimSpace(sub.Title), boolInt(sub.Done), i); err != nil {
			return err
		}
	}
	return nil
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func syncTaskTags(db execer, taskID int64, tagIDs []int64) error {
	if _, err := db.Exec(`DELETE FROM task_tags WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, id := range tagIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := db.Exec(`INSERT OR IGNORE INTO task_tags(task_id, tag_id) VALUES(?,?)`, taskID, id); err != nil {
			return err
		}
	}
	return nil
}

// deriveImportant：优先级「中」及以上视为重要。
func deriveImportant(priority int) bool { return priority >= model.PriorityMedium }

// deriveUrgent：今天或明天到期、以及已逾期，视为紧急。
func deriveUrgent(dueDate *string, _ bool) bool {
	if dueDate == nil {
		return false
	}
	d, err := time.ParseInLocation("2006-01-02", *dueDate, time.Local)
	if err != nil {
		return false
	}
	today := time.Now()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	return !d.After(today.AddDate(0, 0, 1))
}

func nz[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func deref[T any](o model.Opt[T], fallback T) T {
	if o.Set {
		return o.Value
	}
	return fallback
}

func derefPtr[T any](o model.Opt[*T], fallback *T) *T {
	if o.Set {
		return o.Value
	}
	return fallback
}

func optOf[T any](v T) model.Opt[T] { return model.Opt[T]{Set: true, Value: v} }

func optOfPtr[T any](v *T) model.Opt[*T] { return model.Opt[*T]{Set: true, Value: v} }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
