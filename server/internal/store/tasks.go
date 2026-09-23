package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("记录不存在")

const taskSelect = `
SELECT t.id, t.list_id, t.title, t.notes, t.status, t.priority,
       t.start_date, t.due_date, t.due_time, t.end_time, t.url, t.reminders, t.repeat_rule, t.repeat_from,
       t.important, t.urgent, t.pinned, t.starred, t.estimate_minutes, t.progress, t.archived,
       t.completed_at, t.sort_order, t.created_at, t.updated_at,
       l.name, l.color, l.folder_id
FROM tasks t JOIN lists l ON l.id = t.list_id`

// TaskFilter 是任务查询条件。零值表示不过滤。
type TaskFilter struct {
	Smart    string // inbox|today|tomorrow|week|next7|overdue|all|done|nodate|high|starred|updated|recentdone
	ListID   *int64
	FolderID *int64
	Status   string // todo|done|all
	From     *string
	To       *string
	TagID    *int64
	Priority *int
	Quadrant string // 1=重要且紧急 2=重要不紧急 3=紧急不重要 4=都不
	Search   string
	SortBy   string // smart(默认)|manual|priority|due|created|updated|completed|title
	Limit    int
	// IncludeArchived 让被归档清单里的任务也参与查询。默认排除：
	// 「归档」的语义就是「从日常视野里收起来」，否则智能清单会被旧事灌满。
	IncludeArchived bool
	// IncludeTaskArchived 让已归档的任务也参与查询（备份导出用）。
	// TaskArchived 显式指定归档态（侧栏「已归档」恢复区只看 true），
	// 两者都未设置时，已归档任务从一切日常视野中排除。
	IncludeTaskArchived bool
	TaskArchived        *bool
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
	now := time.Now()
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	in7 := now.AddDate(0, 0, 7).Format("2006-01-02")
	// 「本周」以周一为一周之始，取到本周日为止（今天恰是周日时即为今天）。
	weekEnd := now.AddDate(0, 0, (7-int(now.Weekday()))%7).Format("2006-01-02")

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
	case model.SmartTomorrow:
		where = append(where, "t.due_date = ?")
		args = append(args, tomorrow)
		status = statusOpenWithTodayDone
	case model.SmartWeek:
		where = append(where, "t.due_date IS NOT NULL AND t.due_date >= ? AND t.due_date <= ?")
		args = append(args, today, weekEnd)
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
	case model.SmartHigh:
		where = append(where, "t.priority = ?")
		args = append(args, model.PriorityHigh)
		status = statusOpenWithTodayDone
	case model.SmartStarred:
		where = append(where, "t.starred = 1")
		status = statusOpenWithTodayDone
	case model.SmartUpdated:
		// 「最近修改」看的是全部状态（刚改过的往往是刚勾掉的），由排序决定读法。
		if status == "" {
			status = "all"
		}
	case model.SmartRecentDone:
		status = model.StatusDone
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
		// 「未完成」口径包含进行中：进行中的事同样是欠着的账。
		where = append(where, "t.status IN ('todo', 'in_progress')")
	case model.StatusInProgress:
		where = append(where, "t.status = 'in_progress'")
	case model.StatusDone:
		where = append(where, "t.status = 'done'")
	case statusOpenWithTodayDone:
		dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Format(time.RFC3339)
		where = append(where, "(t.status IN ('todo', 'in_progress') OR (t.status = 'done' AND t.completed_at >= ?))")
		args = append(args, dayStart)
	}

	// 归档清单里的任务默认不进入任何聚合视图；显式打开该清单时才算例外。
	if !f.IncludeArchived && !(f.ListID != nil && f.Smart == "") {
		where = append(where, "l.archived = 0")
	}

	// 任务级归档：显式按归档态查询时以它为准，否则已归档任务一律收起。
	if f.TaskArchived != nil {
		where = append(where, "t.archived = ?")
		args = append(args, boolInt(*f.TaskArchived))
	} else if !f.IncludeTaskArchived {
		where = append(where, "t.archived = 0")
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

// 排序：已完成沉底；未完成里置顶的排最前。已完成任务一律沉底，这是所有排序模式的共同前提。
const doneLast = `CASE WHEN t.status = 'done' THEN 1 ELSE 0 END`

// 置顶只对未完成的任务生效：勾掉之后就该沉到「已完成」分区，而不是继续压在顶上。
const pinnedFirst = `CASE WHEN t.status <> 'done' AND t.pinned = 1 THEN 0 ELSE 1 END`

// orderByFor 返回列表排序子句。
//
// 默认的 smart 模式让到期日与优先级主导顺序——这是「今天」这类视图应有的读法。
// 但要支持手动拖拽排序，就必须有一个让 sort_order 说了算的模式，即 manual；
// 否则用户拖完会发现顺序纹丝不动（sort_order 只是 smart 模式里的第五顺位）。
// smart 参数用于两个「按时间倒序才读得通」的智能清单：最近修改与最近完成。
func orderByFor(sortBy, smart string) string {
	switch sortBy {
	case "manual":
		return " ORDER BY " + doneLast + ", " + pinnedFirst + `, t.sort_order ASC, t.id ASC`
	case "priority":
		return " ORDER BY " + doneLast + ", " + pinnedFirst + `, t.priority DESC, t.sort_order ASC, t.id ASC`
	case "due":
		return " ORDER BY " + doneLast + ", " + pinnedFirst + `,
          CASE WHEN t.due_date IS NULL THEN 1 ELSE 0 END,
          t.due_date ASC, COALESCE(t.due_time, '99:99') ASC,
          t.sort_order ASC, t.id ASC`
	case "created":
		return " ORDER BY " + doneLast + ", " + pinnedFirst + `, t.created_at DESC, t.id DESC`
	case "updated":
		return " ORDER BY " + doneLast + ", " + pinnedFirst + `, t.updated_at DESC, t.id DESC`
	case "completed":
		return " ORDER BY " + doneLast + `, t.completed_at DESC, t.id DESC`
	case "title":
		return " ORDER BY " + doneLast + ", " + pinnedFirst + `, t.title COLLATE NOCASE ASC, t.id ASC`
	default:
		switch smart {
		case model.SmartRecentDone:
			return " ORDER BY " + doneLast + `, t.completed_at DESC, t.id DESC`
		case model.SmartUpdated:
			return " ORDER BY " + doneLast + ", " + pinnedFirst + `, t.updated_at DESC, t.id DESC`
		}
		return " ORDER BY " + doneLast + ", " + pinnedFirst + `,
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
	q += orderByFor(f.SortBy, f.Smart)
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
	// 附件只在列表接口上带数量级很小的一条 SQL，不必为此再开一个端点。
	if err := s.attachAttachments(tasks); err != nil {
		return nil, err
	}
	if err := s.attachLinks(tasks); err != nil {
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
	if err := s.attachAttachments(list); err != nil {
		return nil, err
	}
	if err := s.attachLinks(list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(r rowScanner) (model.Task, error) {
	var t model.Task
	var startDate, dueDate, dueTime, endTime, repeatRule, completedAt sql.NullString
	var reminders, url, repeatFrom string
	var important, urgent, pinned, starred, archived int
	var listName, listColor sql.NullString
	var folderID sql.NullInt64

	err := r.Scan(&t.ID, &t.ListID, &t.Title, &t.Notes, &t.Status, &t.Priority,
		&startDate, &dueDate, &dueTime, &endTime, &url, &reminders, &repeatRule, &repeatFrom,
		&important, &urgent, &pinned, &starred, &t.EstimateMinutes, &t.Progress, &archived,
		&completedAt, &t.SortOrder, &t.CreatedAt, &t.UpdatedAt,
		&listName, &listColor, &folderID)
	if err != nil {
		return t, err
	}
	if startDate.Valid {
		v := startDate.String
		t.StartDate = &v
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
	t.URL = url
	t.RepeatFrom = repeatFrom
	if t.RepeatFrom == "" {
		t.RepeatFrom = model.RepeatFromDue
	}
	t.Important = important == 1
	t.Urgent = urgent == 1
	t.Pinned = pinned == 1
	t.Starred = starred == 1
	t.Archived = archived == 1
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

// attachSubtasks 装配子任务树。子任务可再套子任务（parent_id），
// 查询一次取平，再在内存里挂成树：根节点直接归入任务，其余各归其父。
// SubtaskDone/Open 统计全树的叶子口径，父任务的进度条不受嵌套层级影响。
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
	q := `SELECT id, task_id, parent_id, title, due_date, reminders, done, sort_order
	      FROM subtasks WHERE task_id IN (` + strings.Join(ph, ",") + `) ORDER BY sort_order, id`
	rows, err := s.db.Query(q, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()

	// 先摊平收集，再挂树。引用子任务作 map 值后不能再用下标写入，因此统一走指针副本。
	type node struct {
		sub      model.Subtask
		parentID *int64
	}
	nodes := map[int64]*node{}
	var order []int64
	for rows.Next() {
		var n node
		var sub model.Subtask
		var done int
		var reminders string
		if err := rows.Scan(&sub.ID, &sub.TaskID, &n.parentID, &sub.Title, &sub.DueDate, &reminders, &done, &sub.SortOrder); err != nil {
			return err
		}
		sub.Done = done == 1
		sub.Reminders = []int{}
		if reminders != "" {
			_ = json.Unmarshal([]byte(reminders), &sub.Reminders)
		}
		sub.Children = []model.Subtask{}
		n.sub = sub
		nodes[sub.ID] = &n
		order = append(order, sub.ID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	// 自底向上挂：先给每个节点装 Children，再把根节点挂到任务上。
	// 断链的子任务（父节点已被删但级联未及）视作根，不让它凭空消失。
	byParent := map[int64][]int64{}
	for _, id := range order {
		n := nodes[id]
		if n.parentID != nil && *n.parentID != id {
			byParent[*n.parentID] = append(byParent[*n.parentID], id)
		}
	}
	var attach func(id int64) model.Subtask
	attach = func(id int64) model.Subtask {
		n := nodes[id]
		sub := n.sub
		sub.ParentID = n.parentID
		for _, cid := range byParent[id] {
			sub.Children = append(sub.Children, attach(cid))
		}
		return sub
	}
	for _, id := range order {
		n := nodes[id]
		if n.parentID == nil || *n.parentID == id || nodes[*n.parentID] == nil {
			sub := attach(id)
			if i, ok := idx[sub.TaskID]; ok {
				tasks[i].Subtasks = append(tasks[i].Subtasks, sub)
			}
		}
	}
	// 完成度按全树计数。
	var count func(sub model.Subtask) (done, open int)
	count = func(sub model.Subtask) (done, open int) {
		if sub.Done {
			done = 1
		} else {
			open = 1
		}
		for _, c := range sub.Children {
			d, o := count(c)
			done += d
			open += o
		}
		return done, open
	}
	for i := range tasks {
		for _, sub := range tasks[i].Subtasks {
			d, o := count(sub)
			tasks[i].SubtaskDone += d
			tasks[i].SubtaskOpen += o
		}
	}
	return nil
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
	startDate := cleanDatePtr(derefPtr(in.StartDate, nil))
	dueDate := cleanDatePtr(derefPtr(in.DueDate, nil))
	dueTime := derefPtr(in.DueTime, nil)
	endTime := derefPtr(in.EndTime, nil)
	repeatRule := derefPtr(in.RepeatRule, nil)
	if repeatRule != nil && strings.TrimSpace(*repeatRule) == "" {
		repeatRule = nil
	}
	repeatFrom := normalizeRepeatFrom(deref(in.RepeatFrom, model.RepeatFromDue))

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
	res, err := tx.Exec(`INSERT INTO tasks(list_id, title, notes, status, priority, start_date, due_date, due_time, end_time, url, reminders, repeat_rule, repeat_from, important, urgent, pinned, starred, estimate_minutes, progress, sort_order, created_at, updated_at)
		VALUES(?,?,?,'todo',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		listID, title, deref(in.Notes, ""), priority, ptrStr(startDate), ptrStr(dueDate), ptrStr(dueTime), ptrStr(endTime),
		deref(in.URL, ""), mustJSON(reminders), ptrStr(repeatRule), repeatFrom,
		boolInt(important), boolInt(urgent), boolInt(deref(in.Pinned, false)), boolInt(deref(in.Starred, false)),
		maxInt(deref(in.EstimateMinutes, 0), 0), clampProgress(deref(in.Progress, 0)),
		sortOrder, ts, ts)
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
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	s.emit(model.EventTaskCreated, t)
	s.logActivity(model.ActCreated, t.ID, t.Title, "归入 "+t.ListName)
	return t, nil
}

// cleanDatePtr 把空串日期归一为 nil，避免「空日期」在比较与展示上像模像样地存在。
func cleanDatePtr(p *string) *string {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(*p) == "" {
		return nil
	}
	return p
}

// normalizeRepeatFrom 只接受两种取值，其余一律回落到「从原到期日生成」。
func normalizeRepeatFrom(v string) string {
	if v == model.RepeatFromDone {
		return model.RepeatFromDone
	}
	return model.RepeatFromDue
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
	if in.StartDate.Set {
		add("start_date = ?", ptrStr(cleanDatePtr(in.StartDate.Value)))
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
	if in.URL.Set {
		add("url = ?", strings.TrimSpace(in.URL.Value))
	}
	if in.Pinned.Set {
		add("pinned = ?", boolInt(in.Pinned.Value))
	}
	if in.Starred.Set {
		add("starred = ?", boolInt(in.Starred.Value))
	}
	if in.Archived.Set {
		add("archived = ?", boolInt(in.Archived.Value))
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
	if in.RepeatFrom.Set {
		add("repeat_from = ?", normalizeRepeatFrom(in.RepeatFrom.Value))
	}
	if in.SortOrder.Set {
		add("sort_order = ?", in.SortOrder.Value)
	}
	if in.EstimateMinutes.Set {
		add("estimate_minutes = ?", maxInt(in.EstimateMinutes.Value, 0))
	}
	if in.Progress.Set {
		add("progress = ?", clampProgress(in.Progress.Value))
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
		case model.StatusInProgress:
			add("status = 'in_progress'")
			add("completed_at = NULL")
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
	t, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	// 完成与恢复比「改了某个字段」更值得单独订阅，因此拆成独立事件。
	switch {
	case cur.Status != t.Status && t.Status == model.StatusDone:
		s.emit(model.EventTaskCompleted, t)
		s.logActivity(model.ActCompleted, t.ID, t.Title, "在 "+t.ListName+" 中完成")
	case cur.Status != t.Status && t.Status != model.StatusDone:
		s.emit(model.EventTaskReopened, t)
		s.logActivity(model.ActReopened, t.ID, t.Title, "恢复为未完成")
	case cur.ListID != t.ListID:
		s.emit(model.EventTaskUpdated, t)
		s.logActivity(model.ActMoved, t.ID, t.Title, cur.ListName+" → "+t.ListName)
	default:
		s.emit(model.EventTaskUpdated, t)
	}
	// 归档态变化单独记一笔：它与完成不同，是「主动把事情收起来」的动作。
	if cur.Archived != t.Archived {
		if t.Archived {
			s.logActivity(model.ActArchived, t.ID, t.Title, "在 "+t.ListName+" 中归档")
		} else {
			s.logActivity(model.ActUnarchived, t.ID, t.Title, "恢复归档任务")
		}
	}
	return t, nil
}

// DeleteTask 删除任务。删除前会把任务存进撤销槽位，附件文件暂缓清理，
// 用户可在十分钟内反悔（见 undo.go）。
func (s *Store) DeleteTask(id int64) error {
	cur, err := s.GetTask(id)
	if err != nil {
		return err
	}
	if err := s.stageUndo("删除「"+cur.Title+"」", []model.Task{*cur}); err != nil {
		return err
	}
	return s.deleteTaskRows(id, cur, false)
}

// deleteTaskRows 是真正落地的删除：记录删掉、文件留着交给撤销槽位管。
// quiet 为 true 时不逐条记历史 —— 批量删除由调用方统一记一笔，否则日志会被刷满。
func (s *Store) deleteTaskRows(id int64, cur *model.Task, quiet bool) error {
	if err := s.dropTaskAttachments(id, false); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if cur != nil {
		s.emit(model.EventTaskDeleted, cur)
		if !quiet {
			s.logActivity(model.ActDeleted, cur.ID, cur.Title, "从 "+cur.ListName+" 删除")
		}
	}
	return nil
}

// DuplicateTask 复制一条任务：结构照搬，状态归零。
//
// 复制出的新任务不继承「置顶」（置顶是对此刻的一个表态，不该自己长出来），
// 子任务的勾选状态也一律清空 —— 副本的用处是再走一遍，而不是冒充走过了。
func (s *Store) DuplicateTask(id int64) (*model.Task, error) {
	cur, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	subs := make([]model.Subtask, 0, len(cur.Subtasks))
	for _, sub := range cur.Subtasks {
		subs = append(subs, model.Subtask{Title: sub.Title, Done: false})
	}
	in := model.TaskInput{
		Title:      optOf(cur.Title + "（副本）"),
		Notes:      optOf(cur.Notes),
		ListID:     optOf(cur.ListID),
		Priority:   optOf(cur.Priority),
		StartDate:  optOfPtr(cur.StartDate),
		DueDate:    optOfPtr(cur.DueDate),
		DueTime:    optOfPtr(cur.DueTime),
		EndTime:    optOfPtr(cur.EndTime),
		URL:        optOf(cur.URL),
		Reminders:  optOf(cur.Reminders),
		RepeatRule: optOfPtr(cur.RepeatRule),
		RepeatFrom: optOf(cur.RepeatFrom),
		Important:  optOf(cur.Important),
		Urgent:     optOf(cur.Urgent),
		Starred:    optOf(cur.Starred),
		Subtasks:   optOf(subs),
	}
	nt, err := s.CreateTask(in, cur.ListID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(cur.Tags))
	for _, tg := range cur.Tags {
		ids = append(ids, tg.ID)
	}
	if len(ids) > 0 {
		if err := syncTaskTags(s.db, nt.ID, ids); err != nil {
			return nil, err
		}
	}
	s.logActivity(model.ActDuplicated, nt.ID, nt.Title, "复制自「"+cur.Title+"」")
	return s.GetTask(nt.ID)
}

// PurgeCompleted 清空已完成任务。listID 为 nil 表示跨全部清单。
func (s *Store) PurgeCompleted(listID *int64) (int, error) {
	f := TaskFilter{Status: model.StatusDone, SortBy: "manual", IncludeArchived: true}
	if listID != nil {
		f.ListID = listID
	}
	tasks, err := s.ListTasks(f)
	if err != nil {
		return 0, err
	}
	if len(tasks) == 0 {
		return 0, nil
	}
	label := "清空全部已完成任务"
	if listID != nil {
		label = "清空清单内已完成任务"
	}
	if err := s.stageUndo(label, tasks); err != nil {
		return 0, err
	}
	n := 0
	for i := range tasks {
		if err := s.deleteTaskRows(tasks[i].ID, &tasks[i], true); err == nil {
			n++
		}
	}
	s.logActivity(model.ActPurged, tasks[0].ID, strconv.Itoa(n)+" 件已完成任务", label)
	return n, nil
}

// ToggleResult 描述一次完成/恢复操作的结果。
type ToggleResult struct {
	Task      *model.Task `json:"task"`
	NextTask  *model.Task `json:"nextTask"`  // 重复任务续期后生成的新任务
	Completed bool        `json:"completed"` // 本次是否为「完成」
}

// ToggleTask 切换完成状态。完成一个重复任务时，自动续期出下一次任务——
// 「敬终」之后立即「慎始」，让循环不断。
func (s *Store) ToggleTask(id int64) (*ToggleResult, error) { return s.toggleTask(id, false) }

// toggleTask 是 ToggleTask 的内部实现。quiet 用于批量场景：
// 一次勾掉三十件不该在操作历史里刷出三十行，由调用方记一笔汇总。
func (s *Store) toggleTask(id int64, quiet bool) (*ToggleResult, error) {
	cur, err := s.GetTask(id)
	if err != nil {
		return nil, err
	}
	ts := model.Now()
	if cur.Status != model.StatusDone {
		if _, err := s.db.Exec(`UPDATE tasks SET status='done', completed_at=?, updated_at=? WHERE id=?`, ts, ts, id); err != nil {
			return nil, err
		}
		res := &ToggleResult{Completed: true}
		if cur.RepeatRule != nil && *cur.RepeatRule != "" {
			if next, nextRule, ok := NextRepeat(*cur.RepeatRule, cur.StartDate, cur.DueDate, cur.RepeatFrom, time.Now()); ok {
				nd := next.Format("2006-01-02")
				in := model.TaskInput{
					Title:      optOf(cur.Title),
					Notes:      optOf(cur.Notes),
					ListID:     optOf(cur.ListID),
					Priority:   optOf(cur.Priority),
					StartDate:  optOfPtr(shiftStart(cur.StartDate, cur.DueDate, nd)),
					DueDate:    optOfPtr(&nd),
					DueTime:    optOfPtr(cur.DueTime),
					EndTime:    optOfPtr(cur.EndTime),
					URL:        optOf(cur.URL),
					Reminders:  optOf(cur.Reminders),
					RepeatRule: optOfPtr(&nextRule),
					RepeatFrom: optOf(cur.RepeatFrom),
					Pinned:     optOf(cur.Pinned),
					Starred:    optOf(cur.Starred),
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
		s.emit(model.EventTaskCompleted, res.Task)
		if !quiet {
			s.logActivity(model.ActCompleted, res.Task.ID, res.Task.Title, "在 "+res.Task.ListName+" 中完成")
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
	s.emit(model.EventTaskReopened, t)
	if !quiet {
		s.logActivity(model.ActReopened, t.ID, t.Title, "恢复为未完成")
	}
	return &ToggleResult{Task: t, Completed: false}, nil
}

// NextRepeat 计算下一次发生的日期，并处理两种续期基准。
//
// 「从原到期日生成」（默认）保持节奏不乱，长期逾期后完成时会把日期顺推到今天之后，
// 否则次日就会冒出一条「昨天该做」的任务，越积越显得欠债。
// 「从实际完成日生成」则适合「松一口气再排下一次」的安排。
func NextRepeat(rule string, startDate, dueDate *string, repeatFrom string, now time.Time) (time.Time, string, bool) {
	base := now
	// 有开始日期时以开始日为锚，没有才退到到期日 —— 与「任务从哪天起算」的直觉一致。
	for _, src := range []*string{startDate, dueDate} {
		if src == nil {
			continue
		}
		if d, err := time.ParseInLocation("2006-01-02", *src, time.Local); err == nil {
			base = d
			break
		}
	}
	if normalizeRepeatFrom(repeatFrom) == model.RepeatFromDone {
		base = now
	}
	next, nextRule, ok := NextOccurrence(rule, base)
	if !ok {
		return time.Time{}, "", false
	}
	// 从原到期日推的规则要避免「下一次落在过去」，否则逾期任务会永远补不完。
	if normalizeRepeatFrom(repeatFrom) != model.RepeatFromDone {
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		for i := 0; i < 400 && next.Before(today); i++ {
			n2, r2, ok2 := NextOccurrence(nextRule, next)
			if !ok2 {
				return time.Time{}, "", false
			}
			next, nextRule = n2, r2
		}
	}
	return next, nextRule, true
}

// shiftStart 在续期时把开始日期按到期日平移相同天数，让「开始 — 截止」这段跨度保持稳定。
func shiftStart(startDate, oldDue *string, newDue string) *string {
	if startDate == nil || oldDue == nil {
		return nil
	}
	from, err1 := time.ParseInLocation("2006-01-02", *oldDue, time.Local)
	to, err2 := time.ParseInLocation("2006-01-02", newDue, time.Local)
	if err1 != nil || err2 != nil {
		return nil
	}
	d, err := time.ParseInLocation("2006-01-02", *startDate, time.Local)
	if err != nil {
		return nil
	}
	shifted := d.AddDate(0, 0, int(to.Sub(from).Hours()/24))
	out := shifted.Format("2006-01-02")
	return &out
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
	// 跳过一律从原定日期往后推：这次不做，但不能把节奏带偏。
	next, nextRule, ok := NextRepeat(*cur.RepeatRule, cur.StartDate, cur.DueDate, model.RepeatFromDue, time.Now())
	if !ok {
		return nil, ValidationError{Msg: "重复规则已无后续，无法跳过"}
	}
	nd := next.Format("2006-01-02")
	if _, err := s.db.Exec(
		`UPDATE tasks SET due_date = ?, start_date = ?, repeat_rule = ?, urgent = ?, updated_at = ? WHERE id = ?`,
		nd, shiftStart(cur.StartDate, cur.DueDate, nd), nextRule, boolInt(deriveUrgent(&nd, false)), model.Now(), id,
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

// BatchAction 批量操作：complete | reopen | delete | move | pin | unpin | star | unstar。
//
// 批量删除只占一个撤销槽位。逐个 stage 的话，最后一笔会把前面全盖掉，
// 用户点「撤销」只能收回一件 —— 那不是撤销，那是捉弄人。
func (s *Store) BatchAction(ids []int64, action string, listID *int64, dueDate *string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	if action == "delete" {
		found := make([]model.Task, 0, len(ids))
		for _, id := range ids {
			if cur, err := s.GetTask(id); err == nil {
				found = append(found, *cur)
			}
		}
		if len(found) > 0 {
			label := "删除「" + found[0].Title + "」"
			if len(found) > 1 {
				label = fmt.Sprintf("删除 %d 件任务", len(found))
			}
			if err := s.stageUndo(label, found); err != nil {
				return 0, err
			}
		}
		n := 0
		for i := range found {
			if err := s.deleteTaskRows(found[i].ID, &found[i], true); err == nil {
				n++
			} else if !errors.Is(err, ErrNotFound) {
				return n, err
			}
		}
		if n > 0 {
			s.logActivity(model.ActDeleted, found[0].ID, found[0].Title, fmt.Sprintf("批量删除 %d 件任务", n))
		}
		return n, nil
	}

	n := 0
	firstTitle := ""
	for _, id := range ids {
		var err error
		switch action {
		case "complete":
			if _, err = s.toggleTask(id, true); err == nil {
				n++
			}
		case "reopen":
			if _, err = s.db.Exec(`UPDATE tasks SET status='todo', completed_at=NULL, updated_at=? WHERE id=?`, model.Now(), id); err == nil {
				n++
			}
		case "move":
			if _, err = s.MoveTask(id, listID, dueDate, nil); err == nil {
				n++
			}
		case "pin", "unpin", "star", "unstar":
			col, val := "pinned", 1
			if action == "unpin" || action == "unstar" {
				val = 0
			}
			if action == "star" || action == "unstar" {
				col = "starred"
			}
			if _, err = s.db.Exec(`UPDATE tasks SET `+col+` = ?, updated_at = ? WHERE id = ?`, val, model.Now(), id); err == nil {
				n++
			}
		case "archive", "unarchive":
			val := 1
			if action == "unarchive" {
				val = 0
			}
			if _, err = s.db.Exec(`UPDATE tasks SET archived = ?, updated_at = ? WHERE id = ?`, val, model.Now(), id); err == nil {
				n++
			}
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return n, err
		}
		if firstTitle == "" {
			if t, gerr := s.GetTask(id); gerr == nil {
				firstTitle = t.Title
			}
		}
	}

	// 批量动作只记一笔汇总：逐条记会把操作历史变成一张流水账，反而看不出做过什么。
	switch action {
	case "complete":
		s.logActivity(model.ActCompleted, ids[0], firstTitle, fmt.Sprintf("批量完成 %d 件任务", n))
	case "reopen":
		s.logActivity(model.ActReopened, ids[0], firstTitle, fmt.Sprintf("批量恢复 %d 件任务", n))
	case "archive":
		s.logActivity(model.ActArchived, ids[0], firstTitle, fmt.Sprintf("批量归档 %d 件任务", n))
	case "unarchive":
		s.logActivity(model.ActUnarchived, ids[0], firstTitle, fmt.Sprintf("批量恢复归档 %d 件任务", n))
	case "move":
		if listID != nil {
			name := ""
			if l, err := s.List(*listID); err == nil {
				name = l.Name
			}
			s.logActivity(model.ActMoved, ids[0], firstTitle, fmt.Sprintf("批量移动 %d 件任务到 %s", n, name))
		}
	}
	return n, nil
}

// List 按 id 取单个清单。
func (s *Store) List(id int64) (*model.List, error) {
	rows, err := s.db.Query(`
		SELECT l.id, l.folder_id, l.name, l.color, l.icon, l.sort_order, l.archived, l.starred, l.created_at,
		       (SELECT COUNT(*) FROM tasks t WHERE t.list_id = l.id AND t.status = 'todo')
		FROM lists l WHERE l.id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	var l model.List
	var fid *int64
	var archived, starred int
	if err := rows.Scan(&l.ID, &fid, &l.Name, &l.Color, &l.Icon, &l.SortOrder, &archived, &starred, &l.CreatedAt, &l.TaskCount); err != nil {
		return nil, err
	}
	l.FolderID = fid
	l.Archived = archived == 1
	l.Starred = starred == 1
	return &l, rows.Err()
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

// AddSubtask 新增子任务。parentID 非空时挂在同级子任务之下（多级分解），
// dueDate 让这条步骤自己有节律，不必与父任务同起同落。
func (s *Store) AddSubtask(taskID int64, title string, position int, parentID *int64, dueDate *string) (*model.Subtask, error) {
	// 父级必须同属一条任务，且自身不再有父级 —— 分解树允许任意深度，
	// 但「跨任务挂父」会造成删除与统计的语义混乱，直接拒绝。
	if parentID != nil {
		var parentTask int64
		var parentParent *int64
		if err := s.db.QueryRow(`SELECT task_id, parent_id FROM subtasks WHERE id = ?`, *parentID).Scan(&parentTask, &parentParent); err != nil {
			return nil, ErrNotFound
		}
		if parentTask != taskID {
			return nil, ValidationError{Msg: "父级子任务不属于该任务"}
		}
		if parentParent != nil {
			return nil, ValidationError{Msg: "暂只支持两级子任务，父级已是子子任务"}
		}
	}
	if dueDate != nil && strings.TrimSpace(*dueDate) == "" {
		dueDate = nil
	}
	var maxOrder sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(sort_order) FROM subtasks WHERE task_id = ?`, taskID).Scan(&maxOrder)
	order := int(maxOrder.Int64) + 1
	if position >= 0 {
		order = position
	}
	res, err := s.db.Exec(`INSERT INTO subtasks(task_id, parent_id, title, due_date, reminders, done, sort_order) VALUES(?,?,?,?,?,0,?)`,
		taskID, parentID, strings.TrimSpace(title), dueDate, "[]", order)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	_, _ = s.db.Exec(`UPDATE tasks SET updated_at = ? WHERE id = ?`, model.Now(), taskID)
	return &model.Subtask{ID: id, TaskID: taskID, ParentID: parentID, Title: title, DueDate: dueDate, Reminders: []int{}, Children: []model.Subtask{}, SortOrder: order}, nil
}

// SubtaskUpdate 子任务的局部更新入参，nil 表示不改动。
type SubtaskUpdate struct {
	Title     *string
	Done      *bool
	SortOrder *int
	DueDate   *string
	Reminders *[]int
}

// UpdateSubtask 更新子任务。
func (s *Store) UpdateSubtask(id int64, u SubtaskUpdate) error {
	sets := []string{}
	args := []any{}
	if u.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, strings.TrimSpace(*u.Title))
	}
	if u.Done != nil {
		sets = append(sets, "done = ?")
		args = append(args, boolInt(*u.Done))
	}
	if u.SortOrder != nil {
		sets = append(sets, "sort_order = ?")
		args = append(args, *u.SortOrder)
	}
	if u.DueDate != nil {
		d := strings.TrimSpace(*u.DueDate)
		if d == "" {
			sets = append(sets, "due_date = NULL")
		} else {
			sets = append(sets, "due_date = ?")
			args = append(args, d)
		}
	}
	if u.Reminders != nil {
		r := *u.Reminders
		if r == nil {
			r = []int{}
		}
		sets = append(sets, "reminders = ?")
		args = append(args, mustJSON(r))
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

// clampProgress 把进度收进 0-100：越界值一律贴边，而不是报错打断录入。
func clampProgress(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

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
