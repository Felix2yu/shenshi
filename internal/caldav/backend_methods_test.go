package caldav

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-ical"
	emcaldav "github.com/emersion/go-webdav/caldav"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// newTestBackend 建一个带临时库的 CalDAV 后端。
func newTestBackend(t *testing.T) (*Backend, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewBackend(st), st
}

func mustTask(t *testing.T, st *store.Store, title string, due *string) *model.Task {
	t.Helper()
	inbox, err := st.InboxListID()
	if err != nil {
		t.Fatalf("InboxListID: %v", err)
	}
	tk, err := st.CreateTask(model.TaskInput{
		Title:   model.Opt[string]{Set: true, Value: title},
		ListID:  model.Opt[int64]{Set: true, Value: inbox},
		DueDate: model.Opt[*string]{Set: true, Value: due},
	}, inbox)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return tk
}

// TestBackendCollections 集合列举、获取与 404。
func TestBackendCollections(t *testing.T) {
	b, _ := newTestBackend(t)
	ctx := context.Background()

	if p, err := b.CurrentUserPrincipal(ctx); err != nil || p != PrincipalPath {
		t.Errorf("Principal = %q err=%v", p, err)
	}
	if p, err := b.CalendarHomeSetPath(ctx); err != nil || p != HomeSetPath {
		t.Errorf("HomeSet = %q err=%v", p, err)
	}

	// Apple 见 501 会放弃同步，必须回 403。
	if err := b.CreateCalendar(ctx, &emcaldav.Calendar{}); err == nil {
		t.Error("新建日历应被拒绝")
	} else if code := httpErrorCode(t, err); code != 403 {
		t.Errorf("CreateCalendar code = %d，期望 403", code)
	}

	cals, err := b.ListCalendars(ctx)
	if err != nil {
		t.Fatalf("ListCalendars: %v", err)
	}
	if len(cals) != 2 {
		t.Fatalf("集合数 = %d，期望 2", len(cals))
	}
	kinds := map[string]bool{}
	for _, c := range cals {
		kinds[c.Path] = true
	}
	if !kinds[EventsPath] || !kinds[TasksPath] {
		t.Errorf("集合路径 = %+v", cals)
	}

	// GetCalendar 容忍尾斜杠差异。
	if _, err := b.GetCalendar(ctx, EventsPath); err != nil {
		t.Errorf("GetCalendar(events): %v", err)
	}
	if _, err := b.GetCalendar(ctx, strings.TrimSuffix(TasksPath, "/")); err != nil {
		t.Errorf("GetCalendar(tasks 去斜杠): %v", err)
	}
	if _, err := b.GetCalendar(ctx, "/caldav/user/calendars/nope/"); err == nil {
		t.Error("未知集合应 404")
	} else if code := httpErrorCode(t, err); code != 404 {
		t.Errorf("code = %d，期望 404", code)
	}
}

// TestListCalendarObjects events 只含有日期任务，todos 收全部。
func TestListCalendarObjects(t *testing.T) {
	b, st := newTestBackend(t)
	ctx := context.Background()
	due := "2026-10-01"
	mustTask(t, st, "有日期", &due)
	mustTask(t, st, "无日期", nil)

	todos, err := b.ListCalendarObjects(ctx, TasksPath, nil)
	if err != nil {
		t.Fatalf("ListCalendarObjects(todos): %v", err)
	}
	if len(todos) == 0 {
		t.Fatal("todos 集合不应为空")
	}
	for _, o := range todos {
		if o.Data == nil || o.ETag == "" || !strings.HasSuffix(o.Path, ".ics") {
			t.Errorf("对象不完整: %+v", o)
		}
	}

	events, err := b.ListCalendarObjects(ctx, EventsPath, nil)
	if err != nil {
		t.Fatalf("ListCalendarObjects(events): %v", err)
	}
	// 找到「无日期」对应的任务 id，events 里不该有它的 VEVENT。
	nd, err := findTaskIDByTitle(st, "无日期")
	if err != nil {
		t.Fatalf("findTaskID: %v", err)
	}
	for _, o := range events {
		if strings.HasSuffix(o.Path, EventUID(nd)+".ics") {
			t.Errorf("无日期任务不应出现在日历集合: %s", o.Path)
		}
	}
	// 「有日期」必须在。
	dated, err := findTaskIDByTitle(st, "有日期")
	if err != nil {
		t.Fatalf("findTaskID(dated): %v", err)
	}
	found := false
	for _, o := range events {
		if strings.HasSuffix(o.Path, EventUID(dated)+".ics") {
			found = true
		}
	}
	if !found {
		t.Error("有日期任务应出现在日历集合")
	}

	// 非法集合 404。
	if _, err := b.ListCalendarObjects(ctx, "/caldav/user/calendars/nope/", nil); err == nil {
		t.Error("未知集合应 404")
	}
	// Query 与 List 同口径。
	q, err := b.QueryCalendarObjects(ctx, TasksPath, &emcaldav.CalendarQuery{})
	if err != nil || len(q) != len(todos) {
		t.Errorf("Query 结果应与 List 一致: n=%d err=%v", len(q), err)
	}
}

func findTaskIDByTitle(st *store.Store, title string) (int64, error) {
	tasks, err := st.ListTasks(store.TaskFilter{Status: "all", Search: title, SortBy: "manual"})
	if err != nil {
		return 0, err
	}
	for _, tk := range tasks {
		if tk.Title == title {
			return tk.ID, nil
		}
	}
	return 0, store.ErrNotFound
}

// TestGetCalendarObject 单对象读取与 404。
func TestGetCalendarObject(t *testing.T) {
	b, st := newTestBackend(t)
	ctx := context.Background()
	due := "2026-10-01"
	tk := mustTask(t, st, "单对象", &due)

	obj, err := b.GetCalendarObject(ctx, objectPath(CollectionTasks, tk.ID), nil)
	if err != nil {
		t.Fatalf("GetCalendarObject: %v", err)
	}
	if obj.Path != objectPath(CollectionTasks, tk.ID) {
		t.Errorf("Path = %q", obj.Path)
	}
	if obj.Data == nil {
		t.Fatal("Data 为空")
	}

	if _, err := b.GetCalendarObject(ctx, TasksPath+TodoUID(999999)+".ics", nil); err == nil {
		t.Error("不存在对象应 404")
	} else if code := httpErrorCode(t, err); code != 404 {
		t.Errorf("code = %d，期望 404", code)
	}
	if _, err := b.GetCalendarObject(ctx, "/caldav/user/calendars/shenshi-tasks/bad-path", nil); err == nil {
		t.Error("非法路径应报错")
	}
}

// TestPutCalendarObject 只放行提醒事项集合的完成状态更新。
func TestPutCalendarObject(t *testing.T) {
	b, st := newTestBackend(t)
	ctx := context.Background()
	tk := mustTask(t, st, "可勾选", nil)

	// 日历集合只读。
	if _, err := b.PutCalendarObject(ctx, objectPath(CollectionEvents, tk.ID), ical.NewCalendar(), nil); err == nil {
		t.Error("日历集合 PUT 应 403")
	} else if code := httpErrorCode(t, err); code != 403 {
		t.Errorf("events PUT code = %d", code)
	}
	// 空请求体。
	if _, err := b.PutCalendarObject(ctx, objectPath(CollectionTasks, tk.ID), nil, nil); err == nil {
		t.Error("nil 日历应 400")
	} else if code := httpErrorCode(t, err); code != 400 {
		t.Errorf("nil body code = %d", code)
	}
	// 无 STATUS → 403（拒绝改标题日期）。
	if _, err := b.PutCalendarObject(ctx, objectPath(CollectionTasks, tk.ID), ical.NewCalendar(), nil); err == nil {
		t.Error("无状态更新应 403")
	} else if code := httpErrorCode(t, err); code != 403 {
		t.Errorf("no-status code = %d", code)
	}

	// STATUS=COMPLETED → 任务被勾掉，返回的对象也是完成态。
	doneCal := todoWithStatus("COMPLETED")
	obj, err := b.PutCalendarObject(ctx, objectPath(CollectionTasks, tk.ID), doneCal, nil)
	if err != nil {
		t.Fatalf("PutCalendarObject(完成): %v", err)
	}
	if obj.Data == nil {
		t.Fatal("返回对象缺数据")
	}
	cur, err := st.GetTask(tk.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if cur.Status != model.StatusDone {
		t.Errorf("任务状态 = %q，期望完成", cur.Status)
	}

	// STATUS=NEEDS-ACTION → 恢复未完成。
	reopenCal := todoWithStatus("NEEDS-ACTION")
	if _, err := b.PutCalendarObject(ctx, objectPath(CollectionTasks, tk.ID), reopenCal, nil); err != nil {
		t.Fatalf("PutCalendarObject(恢复): %v", err)
	}
	cur, _ = st.GetTask(tk.ID)
	if cur.Status != model.StatusTodo {
		t.Errorf("任务状态 = %q，期望恢复未完成", cur.Status)
	}

	// 状态相同则不重复 toggle（幂等）：再 PUT 一次仍成功。
	if _, err := b.PutCalendarObject(ctx, objectPath(CollectionTasks, tk.ID), todoWithStatus("NEEDS-ACTION"), nil); err != nil {
		t.Errorf("幂等 PUT: %v", err)
	}

	// 对象不存在 404。
	if _, err := b.PutCalendarObject(ctx, objectPath(CollectionTasks, 999999), todoWithStatus("COMPLETED"), nil); err == nil {
		t.Error("不存在对象应 404")
	} else if code := httpErrorCode(t, err); code != 404 {
		t.Errorf("code = %d", code)
	}
	// 非法路径。
	if _, err := b.PutCalendarObject(ctx, "garbage", todoWithStatus("COMPLETED"), nil); err == nil {
		t.Error("非法路径应报错")
	}
}

// todoWithStatus 造一个带 STATUS 的最小 VTODO 日历。
func todoWithStatus(status string) *ical.Calendar {
	cal := ical.NewCalendar()
	comp := ical.NewComponent(ical.CompToDo)
	comp.Props.SetText(ical.PropUID, "x")
	comp.Props.SetText(ical.PropStatus, status)
	cal.Children = append(cal.Children, comp)
	return cal
}

// TestDeleteCalendarObject 日历端删除一律 403：手机上误删不可挽回。
func TestDeleteCalendarObject(t *testing.T) {
	b, _ := newTestBackend(t)
	err := b.DeleteCalendarObject(context.Background(), objectPath(CollectionTasks, 1))
	if err == nil {
		t.Fatal("删除应被拒绝")
	}
	if code := httpErrorCode(t, err); code != 403 {
		t.Errorf("code = %d，期望 403", code)
	}
}

// TestSyncHook 事件钩子写入两个集合的变更；无日期任务在 events 侧记删除。
func TestSyncHook(t *testing.T) {
	b, st := newTestBackend(t)
	hook := b.SyncHook()

	// 有日期任务：events 不删除，todos 不删除。
	due := "2026-10-01"
	tk := mustTask(t, st, "钩子有日期", &due)
	hook(model.EventTaskUpdated, tk)

	// 无日期任务：events 侧必须记删除（清掉可能残留的僵尸日程）。
	nd := mustTask(t, st, "钩子无日期", nil)
	hook(model.EventTaskUpdated, nd)

	// 删除事件：两边都记删除。
	del := *nd
	hook(model.EventTaskDeleted, &del)

	// 非 Task 载荷静默忽略。
	hook(model.EventTaskUpdated, "not-a-task")
	hook(model.EventTaskUpdated, nil)

	ev, _, err := st.CalDAVChangesSince(CollectionEvents, 0, 0)
	if err != nil {
		t.Fatalf("CalDAVChangesSince(events): %v", err)
	}
	// 无日期任务应出现 deleted=true 的事件变更。
	ndDeleted := false
	for _, c := range ev {
		if c.UID == EventUID(nd.ID) && c.Deleted {
			ndDeleted = true
		}
	}
	if !ndDeleted {
		t.Errorf("无日期任务应在 events 记删除变更，得到 %+v", ev)
	}
	// 有日期任务的正常更新不该被标删。
	for _, c := range ev {
		if c.UID == EventUID(tk.ID) && c.Deleted {
			t.Errorf("有日期任务更新不应在 events 标删: %+v", c)
		}
	}
	todos, _, err := st.CalDAVChangesSince(CollectionTasks, 0, 0)
	if err != nil {
		t.Fatalf("CalDAVChangesSince(todos): %v", err)
	}
	if len(todos) == 0 {
		t.Error("todos 侧应记录变更")
	}
}
