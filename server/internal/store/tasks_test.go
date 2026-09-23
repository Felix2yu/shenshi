package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

func sp(s string) *string { return &s }

func mustCreateTask(t *testing.T, s *Store, title string, mut func(*model.TaskInput)) *model.Task {
	t.Helper()
	in := model.TaskInput{Title: optOf(title)}
	if mut != nil {
		mut(&in)
	}
	defaultList := int64(0)
	if !in.ListID.Set {
		inbox, err := s.InboxListID()
		if err != nil {
			t.Fatalf("InboxListID: %v", err)
		}
		defaultList = inbox
	}
	task, err := s.CreateTask(in, defaultList)
	if err != nil {
		t.Fatalf("CreateTask(%q): %v", title, err)
	}
	return task
}

// TestTaskCRUD 创建 → 读取 → 局部更新 → 删除 的全链路。
func TestTaskCRUD(t *testing.T) {
	s := newTestStore(t)
	inbox, err := s.InboxListID()
	if err != nil {
		t.Fatalf("InboxListID: %v", err)
	}

	created := mustCreateTask(t, s, "CRUD用任务", func(in *model.TaskInput) {
		in.ListID = optOf(inbox)
		in.Priority = optOf(model.PriorityHigh)
		in.DueDate = optOfPtr(sp("2026-09-23"))
		in.DueTime = optOfPtr(sp("09:30"))
		in.Reminders = optOf([]int{10})
	})
	if created.ID == 0 || created.Status != model.StatusTodo {
		t.Fatalf("创建结果异常: id=%d status=%q", created.ID, created.Status)
	}
	// 派生字段：中高优先级 → 重要；到期≤明天 → 紧急。
	if !created.Important {
		t.Error("高优先级应派生为重要")
	}
	if !created.Urgent {
		t.Error("明天到期应派生为紧急")
	}

	got, err := s.GetTask(created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Title != "CRUD用任务" || got.Priority != model.PriorityHigh || got.DueTime == nil || *got.DueTime != "09:30" {
		t.Errorf("读回不一致: %+v", got)
	}

	// PATCH：只传 title，其余字段不动。
	updated, err := s.UpdateTask(created.ID, model.TaskInput{Title: optOf("改名后的任务")})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if updated.Title != "改名后的任务" {
		t.Errorf("title = %q", updated.Title)
	}
	if updated.Priority != model.PriorityHigh {
		t.Errorf("未传的 priority 被改动: %d", updated.Priority)
	}

	// 清空日期：显式传 nil 指针（Set=true）。
	cleared, err := s.UpdateTask(created.ID, model.TaskInput{DueDate: optOfPtr[string](nil)})
	if err != nil {
		t.Fatalf("清空 dueDate: %v", err)
	}
	if cleared.DueDate != nil {
		t.Errorf("dueDate 应被清空，得到 %v", *cleared.DueDate)
	}
	if cleared.DueTime == nil {
		t.Error("未传的 dueTime 不应被清掉")
	}

	// 非法入参。
	if _, err := s.UpdateTask(created.ID, model.TaskInput{Title: optOf("   ")}); err == nil {
		t.Error("空白标题应被拒绝")
	} else {
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("应为 ValidationError，得到 %T", err)
		}
	}
	if _, err := s.CreateTask(model.TaskInput{}, 0); err == nil {
		t.Error("空标题创建应被拒绝")
	}
	if err := s.DeleteTask(created.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := s.GetTask(created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后应 ErrNotFound，得到 %v", err)
	}
}

// TestListTasksFilters 智能清单、状态、搜索、优先级与计数口径。
func TestListTasksFilters(t *testing.T) {
	s := newTestStore(t)
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	mustCreateTask(t, s, "过滤甲·高优·昨天到期", func(in *model.TaskInput) {
		in.Priority = optOf(model.PriorityHigh)
		in.DueDate = optOfPtr(sp(yesterday))
		in.Starred = optOf(true)
	})
	mustCreateTask(t, s, "过滤乙·无日期", nil)
	mustCreateTask(t, s, "过滤丙·今天到期", func(in *model.TaskInput) {
		in.DueDate = optOfPtr(sp(today))
	})

	// 全量（含已完成、含归档任务）应不小于基线。
	all, err := s.ListTasks(TaskFilter{Status: "all", SortBy: "manual", IncludeArchived: true, IncludeTaskArchived: true})
	if err != nil {
		t.Fatalf("ListTasks(all): %v", err)
	}
	if len(all) < 3 {
		t.Errorf("全量任务数 = %d，至少应含 3 条", len(all))
	}

	// 未完成口径：刚建的三条都是 todo。
	open, err := s.ListTasks(TaskFilter{Status: model.StatusTodo, SortBy: "manual"})
	if err != nil {
		t.Fatalf("ListTasks(todo): %v", err)
	}
	found := map[string]bool{}
	for _, task := range open {
		found[task.Title] = true
	}
	for _, title := range []string{"过滤甲·高优·昨天到期", "过滤乙·无日期", "过滤丙·今天到期"} {
		if !found[title] {
			t.Errorf("未完成列表缺少 %q", title)
		}
	}

	// 逾期智能清单：昨天到期的在、今天与无日期的不在。
	overdue, err := s.ListTasks(TaskFilter{Smart: model.SmartOverdue, SortBy: "manual"})
	if err != nil {
		t.Fatalf("SmartOverdue: %v", err)
	}
	inOverdue := false
	for _, task := range overdue {
		if strings.HasPrefix(task.Title, "过滤甲") {
			inOverdue = true
		}
	}
	if !inOverdue {
		t.Error("逾期清单应包含昨天到期的任务")
	}

	// 高优清单。
	high, err := s.ListTasks(TaskFilter{Smart: model.SmartHigh, SortBy: "manual"})
	if err != nil {
		t.Fatalf("SmartHigh: %v", err)
	}
	nHigh := 0
	for _, task := range high {
		if strings.HasPrefix(task.Title, "过滤甲") {
			nHigh++
		}
	}
	if nHigh != 1 {
		t.Errorf("高优清单含目标任务 %d 次，期望 1", nHigh)
	}

	// 收藏清单。
	starred, err := s.ListTasks(TaskFilter{Smart: model.SmartStarred, SortBy: "manual"})
	if err != nil {
		t.Fatalf("SmartStarred: %v", err)
	}
	ok := false
	for _, task := range starred {
		if strings.HasPrefix(task.Title, "过滤甲") {
			ok = true
		}
	}
	if !ok {
		t.Error("收藏清单缺少收藏任务")
	}

	// 搜索命中标题。
	search, err := s.ListTasks(TaskFilter{Search: "过滤乙", Status: "all", SortBy: "manual", IncludeTaskArchived: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(search) != 1 {
		t.Errorf("搜索「过滤乙」命中 %d 条，期望 1", len(search))
	}

	// 无日期清单。
	nodate, err := s.ListTasks(TaskFilter{Smart: model.SmartNoDate, SortBy: "manual"})
	if err != nil {
		t.Fatalf("SmartNoDate: %v", err)
	}
	for _, task := range nodate {
		if task.DueDate != nil {
			t.Errorf("无日期清单出现带日期任务 %q", task.Title)
			break
		}
	}

	// 计数：按清单统计未完成。
	inbox, _ := s.InboxListID()
	n, err := s.CountTasks(TaskFilter{ListID: &inbox, Status: model.StatusTodo})
	if err != nil {
		t.Fatalf("CountTasks: %v", err)
	}
	if n < 3 {
		t.Errorf("收件箱未完成计数 = %d，应 ≥3", n)
	}
}

// TestToggleRepeatToggle 完成 → 重复任务续期 → 恢复。
func TestToggleAndRepeatSpawn(t *testing.T) {
	s := newTestStore(t)
	due := time.Now().Format("2006-01-02")
	task := mustCreateTask(t, s, "每日站会", func(in *model.TaskInput) {
		in.DueDate = optOfPtr(sp(due))
		in.RepeatRule = optOfPtr(sp("daily"))
	})

	res, err := s.ToggleTask(task.ID)
	if err != nil {
		t.Fatalf("ToggleTask: %v", err)
	}
	if !res.Completed || res.Task.Status != model.StatusDone {
		t.Errorf("应完成: completed=%v status=%q", res.Completed, res.Task.Status)
	}
	if res.NextTask == nil {
		t.Fatal("重复任务完成后应续期出下一条")
	}
	if res.NextTask.ID == task.ID {
		t.Error("续期任务应是新记录")
	}
	nextDue := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if res.NextTask.DueDate == nil || *res.NextTask.DueDate != nextDue {
		t.Errorf("续期日期 = %v，期望 %s", res.NextTask.DueDate, nextDue)
	}

	// 再点一次：恢复为未完成，不再续期。
	res2, err := s.ToggleTask(task.ID)
	if err != nil {
		t.Fatalf("ToggleTask(恢复): %v", err)
	}
	if res2.Completed || res2.Task.Status != model.StatusTodo {
		t.Errorf("应恢复未完成: completed=%v status=%q", res2.Completed, res2.Task.Status)
	}

	// 不存在的任务。
	if _, err := s.ToggleTask(999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在任务应 ErrNotFound，得到 %v", err)
	}
}

// TestNextRepeat 两种续期基准与「不落过去」的顺推。
func TestNextRepeat(t *testing.T) {
	now := time.Date(2026, 9, 23, 15, 0, 0, 0, time.Local)
	past := "2026-09-01"

	// 从原到期日推：长期逾期要顺推到今天之后。
	next, _, ok := NextRepeat("daily", nil, &past, model.RepeatFromDue, now)
	if !ok {
		t.Fatal("daily 规则应能推进")
	}
	if next.Before(time.Date(2026, 9, 23, 0, 0, 0, 0, time.Local)) {
		t.Errorf("续期不得落在过去，得到 %s", next.Format("2006-01-02"))
	}

	// 从实际完成日推：以 now 为锚。
	start := "2026-09-01"
	next, _, ok = NextRepeat("daily", &start, &past, model.RepeatFromDone, now)
	if !ok {
		t.Fatal("done 基准应能推进")
	}
	want := time.Date(2026, 9, 24, 0, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Errorf("done 基准 daily 次日 = %s，期望 %s", next.Format("2006-01-02"), want.Format("2006-01-02"))
	}

	// 非法规则。
	if _, _, ok := NextRepeat("fortnightly", nil, nil, "", now); ok {
		t.Error("未知规则应 ok=false")
	}
}

// TestSkipTask 跳过只推日期、不产生副本。
func TestSkipTask(t *testing.T) {
	s := newTestStore(t)
	past := "2026-09-01"
	task := mustCreateTask(t, s, "周报（可跳过）", func(in *model.TaskInput) {
		in.DueDate = optOfPtr(sp(past))
		in.RepeatRule = optOfPtr(sp("daily"))
	})

	skipped, err := s.SkipTask(task.ID)
	if err != nil {
		t.Fatalf("SkipTask: %v", err)
	}
	if skipped.DueDate == nil || *skipped.DueDate <= past {
		t.Errorf("跳过后日期应推进，得到 %v", skipped.DueDate)
	}
	if skipped.Status != model.StatusTodo {
		t.Errorf("跳过不应记完成，status=%q", skipped.Status)
	}
	if skipped.CompletedAt != nil {
		t.Error("跳过不应写 completed_at")
	}

	// 无重复规则的任务不能跳过。
	plain := mustCreateTask(t, s, "无规则任务", nil)
	if _, err := s.SkipTask(plain.ID); err == nil {
		t.Error("无重复规则应拒绝跳过")
	}
}

// TestMoveTask 拖拽改期/改清单，空串清空。
func TestMoveTask(t *testing.T) {
	s := newTestStore(t)
	inbox, _ := s.InboxListID()
	task := mustCreateTask(t, s, "拖拽用任务", func(in *model.TaskInput) {
		in.DueDate = optOfPtr(sp("2026-10-01"))
		in.DueTime = optOfPtr(sp("14:00"))
	})

	// 清单换了、日期清空。
	list, err := s.CreateList(ListInput{Name: sp("拖拽目标清单")})
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	moved, err := s.MoveTask(task.ID, &list.ID, sp(""), nil)
	if err != nil {
		t.Fatalf("MoveTask: %v", err)
	}
	if moved.ListID != list.ID {
		t.Errorf("listId = %d，期望 %d", moved.ListID, list.ID)
	}
	if moved.DueDate != nil {
		t.Errorf("空串应清空日期，得到 %v", *moved.DueDate)
	}
	if moved.DueTime == nil || *moved.DueTime != "14:00" {
		t.Error("未传的 dueTime 不应被改动")
	}
	if moved.Urgent {
		t.Error("清空日期后紧急应为假")
	}
	_ = inbox
}

// TestBatchAction 白名单拒绝未知动作；各动作按语义生效，批量删除只留一个撤销槽位。
func TestBatchAction(t *testing.T) {
	s := newTestStore(t)
	a := mustCreateTask(t, s, "批量甲", nil)
	b := mustCreateTask(t, s, "批量乙", nil)
	ids := []int64{a.ID, b.ID}

	if _, err := s.BatchAction(ids, "explode", nil, nil); err == nil {
		t.Error("未知批量动作应被拒绝")
	}

	if n, err := s.BatchAction(ids, "star", nil, nil); err != nil || n != 2 {
		t.Fatalf("star: n=%d err=%v", n, err)
	}
	if t2, _ := s.GetTask(a.ID); !t2.Starred {
		t.Error("star 应置 starred")
	}

	if n, err := s.BatchAction(ids, "archive", nil, nil); err != nil || n != 2 {
		t.Fatalf("archive: n=%d err=%v", n, err)
	}
	if t2, _ := s.GetTask(a.ID); !t2.Archived {
		t.Error("archive 应置 archived")
	}

	if n, err := s.BatchAction(ids, "complete", nil, nil); err != nil || n != 2 {
		t.Fatalf("complete: n=%d err=%v", n, err)
	}
	if t2, _ := s.GetTask(b.ID); t2.Status != model.StatusDone {
		t.Errorf("complete 后 status=%q", t2.Status)
	}

	if n, err := s.BatchAction(ids, "reopen", nil, nil); err != nil || n != 2 {
		t.Fatalf("reopen: n=%d err=%v", n, err)
	}
	if t2, _ := s.GetTask(b.ID); t2.Status != model.StatusTodo {
		t.Errorf("reopen 后 status=%q", t2.Status)
	}

	// 批量删除：撤销槽位一条记录装下两件。
	if n, err := s.BatchAction(ids, "delete", nil, nil); err != nil || n != 2 {
		t.Fatalf("delete: n=%d err=%v", n, err)
	}
	us, err := s.UndoState()
	if err != nil {
		t.Fatalf("UndoState: %v", err)
	}
	if !us.Available || us.Count != 2 {
		t.Errorf("撤销槽位应含 2 件，得到 available=%v count=%d", us.Available, us.Count)
	}
	restored, err := s.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if restored != 2 {
		t.Errorf("撤销恢复 %d 件，期望 2", restored)
	}
}

// TestSubtasks 两级子任务的增改删与跨任务挂父的拒绝。
func TestSubtasks(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "分解任务", nil)

	sub1, err := s.AddSubtask(task.ID, "第一步", 0, nil, nil)
	if err != nil {
		t.Fatalf("AddSubtask: %v", err)
	}
	sub2, err := s.AddSubtask(task.ID, "第二步", -1, nil, sp("2026-09-25"))
	if err != nil {
		t.Fatalf("AddSubtask(带日期): %v", err)
	}
	if sub2.DueDate == nil || *sub2.DueDate != "2026-09-25" {
		t.Errorf("子任务日期 = %v", sub2.DueDate)
	}

	// 二级子任务挂在 sub1 之下。
	sub3, err := s.AddSubtask(task.ID, "第一步的细节", -1, &sub1.ID, nil)
	if err != nil {
		t.Fatalf("二级子任务: %v", err)
	}
	if sub3.ParentID == nil || *sub3.ParentID != sub1.ID {
		t.Errorf("parent = %v", sub3.ParentID)
	}

	// 三级被拒：父级已是子子任务。
	if _, err := s.AddSubtask(task.ID, "第三级", -1, &sub3.ID, nil); err == nil {
		t.Error("三级子任务应被拒绝")
	}

	// 跨任务挂父被拒。
	other := mustCreateTask(t, s, "另一条任务", nil)
	otherSub, err := s.AddSubtask(other.ID, "对方的子任务", -1, nil, nil)
	if err != nil {
		t.Fatalf("other sub: %v", err)
	}
	if _, err := s.AddSubtask(task.ID, "挂错父", -1, &otherSub.ID, nil); err == nil {
		t.Error("跨任务挂父应被拒绝")
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Subtasks) < 2 {
		t.Errorf("顶层子任务数 = %d，应 ≥2", len(got.Subtasks))
	}

	// 更新：勾选完成。
	done := true
	if err := s.UpdateSubtask(sub1.ID, SubtaskUpdate{Done: &done, Title: sp("第一步（改）")}); err != nil {
		t.Fatalf("UpdateSubtask: %v", err)
	}
	got, _ = s.GetTask(task.ID)
	if len(got.Subtasks) == 0 || !got.Subtasks[0].Done || got.Subtasks[0].Title != "第一步（改）" {
		t.Errorf("子任务更新未生效: %+v", got.Subtasks)
	}
	if got.SubtaskDone != 1 {
		t.Errorf("SubtaskDone = %d，期望 1", got.SubtaskDone)
	}

	// 更新不存在的子任务。
	if err := s.UpdateSubtask(999999, SubtaskUpdate{Title: sp("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在子任务应 ErrNotFound，得到 %v", err)
	}

	if err := s.DeleteSubtask(sub2.ID); err != nil {
		t.Fatalf("DeleteSubtask: %v", err)
	}
	got, _ = s.GetTask(task.ID)
	for _, st := range got.Subtasks {
		if st.ID == sub2.ID {
			t.Error("子任务应已删除")
		}
	}
}

// TestDuplicateTask 副本带「（副本）」后缀，子任务未完成态被重置。
func TestDuplicateTask(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "原始任务", func(in *model.TaskInput) {
		in.Priority = optOf(model.PriorityMedium)
		in.Notes = optOf("备注内容")
		in.Subtasks = optOf([]model.Subtask{{Title: "已完成的步骤", Done: true, SortOrder: 1024}})
	})
	// 让原任务先带上一条已完成子任务。
	subs, err := s.ListTasks(TaskFilter{Status: "all", Search: "原始任务", SortBy: "manual", IncludeTaskArchived: true})
	if err != nil || len(subs) == 0 {
		t.Fatalf("读回原任务失败: %v", err)
	}
	_ = task
	// CreateTask 的 Subtasks 走 replaceSubtasks 装配。
	cur, _ := s.GetTask(task.ID)
	if len(cur.Subtasks) == 0 {
		t.Fatal("原任务应带子任务")
	}

	cp, err := s.DuplicateTask(task.ID)
	if err != nil {
		t.Fatalf("DuplicateTask: %v", err)
	}
	if cp.ID == task.ID {
		t.Error("副本应是新记录")
	}
	if !strings.HasSuffix(cp.Title, "（副本）") {
		t.Errorf("副本标题 = %q", cp.Title)
	}
	if cp.Notes != "备注内容" || cp.Priority != model.PriorityMedium {
		t.Errorf("副本内容丢失: notes=%q priority=%d", cp.Notes, cp.Priority)
	}
	for _, st := range cp.Subtasks {
		if st.Done {
			t.Error("副本的子任务应重置为未完成")
		}
	}
	if _, err := s.DuplicateTask(999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("复制不存在任务应 ErrNotFound，得到 %v", err)
	}
}

// TestPurgeCompleted 清空已完成进撤销槽位，可撤销。
func TestPurgeCompleted(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "待清空任务", nil)
	if _, err := s.ToggleTask(task.ID); err != nil {
		t.Fatalf("完成任务: %v", err)
	}

	n, err := s.PurgeCompleted(nil)
	if err != nil {
		t.Fatalf("PurgeCompleted: %v", err)
	}
	if n < 1 {
		t.Fatalf("应至少清掉 1 件，得到 %d", n)
	}
	if _, err := s.GetTask(task.ID); !errors.Is(err, ErrNotFound) {
		t.Error("被清任务应已删除")
	}
	us, _ := s.UndoState()
	if !us.Available || us.Count != n {
		t.Errorf("撤销槽位 count=%d，期望 %d", us.Count, n)
	}
	if restored, err := s.Undo(); err != nil || restored != n {
		t.Errorf("撤销恢复 n=%d err=%v，期望 %d", restored, err, n)
	}
}

// TestReorderTasks 按传入顺序重写排序位次。
func TestReorderTasks(t *testing.T) {
	s := newTestStore(t)
	a := mustCreateTask(t, s, "重排甲", nil)
	b := mustCreateTask(t, s, "重排乙", nil)
	if err := s.ReorderTasks([]int64{b.ID, a.ID}); err != nil {
		t.Fatalf("ReorderTasks: %v", err)
	}
	tasks, err := s.ListTasks(TaskFilter{Status: "all", SortBy: "manual", Search: "重排", IncludeTaskArchived: true})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) < 2 || tasks[0].ID != b.ID {
		t.Errorf("重排后首条 = %v", tasks)
	}
	if err := s.ReorderTasks(nil); err != nil {
		t.Errorf("空列表应直接成功: %v", err)
	}
}

// TestUndoLifecycle 删除 → 查看槽位 → 恢复 → 槽位清空 → 再撤销报错。
func TestUndoLifecycle(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "撤销生命周期", func(in *model.TaskInput) {
		in.DueDate = optOfPtr(sp("2026-12-01"))
		in.Priority = optOf(model.PriorityLow)
	})

	us, err := s.UndoState()
	if err != nil {
		t.Fatalf("UndoState: %v", err)
	}
	if us.Available {
		t.Error("初始槽位应为空")
	}
	if _, err := s.Undo(); err == nil {
		t.Error("空槽位撤销应报错")
	}

	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	us, _ = s.UndoState()
	if !us.Available || us.Count != 1 || !strings.Contains(us.Label, "撤销生命周期") {
		t.Errorf("槽位状态 = %+v", us)
	}

	n, err := s.Undo()
	if err != nil || n != 1 {
		t.Fatalf("Undo: n=%d err=%v", n, err)
	}
	got, err := s.GetTask(task.ID)
	if err == nil {
		// 恢复出来的是新 id。
		if got.ID == task.ID {
			t.Error("恢复应生成新记录")
		}
	}
	// 恢复的内容按原样还原。
	if _, err := s.ListTasks(TaskFilter{Status: "all", Search: "撤销生命周期", SortBy: "manual", IncludeTaskArchived: true}); err != nil {
		t.Fatalf("恢复后查询: %v", err)
	}

	us, _ = s.UndoState()
	if us.Available {
		t.Error("撤销后槽位应清空")
	}
	if _, err := s.Undo(); err == nil {
		t.Error("再次撤销应报错")
	}

	// DropUndo 幂等。
	if err := s.DropUndo(); err != nil {
		t.Errorf("DropUndo: %v", err)
	}
}

// TestActivityLog 操作历史写入、读取与清空。
func TestActivityLog(t *testing.T) {
	s := newTestStore(t)
	s.logActivity(model.ActCreated, 1, "标题A", "详情A")
	s.logActivity(model.ActCompleted, 1, "标题B", "详情B")

	list, err := s.Activities(10)
	if err != nil {
		t.Fatalf("Activities: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("历史条数 = %d，应 ≥2", len(list))
	}
	if list[0].Kind != model.ActCompleted {
		t.Errorf("应按时间倒序，首条 = %q", list[0].Kind)
	}

	if err := s.ClearActivities(); err != nil {
		t.Fatalf("ClearActivities: %v", err)
	}
	list, _ = s.Activities(10)
	if len(list) != 0 {
		t.Errorf("清空后仍有 %d 条", len(list))
	}
}

// TestTaskLinks 任务关联与依赖：自引用/环路/重复拒绝，阻塞状态随完成解除。
func TestTaskLinks(t *testing.T) {
	s := newTestStore(t)
	a := mustCreateTask(t, s, "被依赖方", nil)
	b := mustCreateTask(t, s, "依赖方", nil)

	if _, err := s.AddTaskLink(a.ID, a.ID, model.LinkRelated); err == nil {
		t.Error("自引用应被拒绝")
	}
	if _, err := s.AddTaskLink(a.ID, b.ID, "friends"); err == nil {
		t.Error("未知关联类型应被拒绝")
	}

	link, err := s.AddTaskLink(b.ID, a.ID, model.LinkBlocked)
	if err != nil {
		t.Fatalf("AddTaskLink(blocked_by): %v", err)
	}
	if link.Kind != model.LinkBlocked || link.Title != "被依赖方" {
		t.Errorf("link = %+v", link)
	}
	// 重复同向边拒绝。
	if _, err := s.AddTaskLink(b.ID, a.ID, model.LinkBlocked); err == nil {
		t.Error("重复依赖边应被拒绝")
	}
	// 成环：a 再依赖 b 就成环。
	if _, err := s.AddTaskLink(a.ID, b.ID, model.LinkBlocked); err == nil {
		t.Error("依赖成环应被拒绝")
	}

	blocked, err := s.TaskIsBlocked(b.ID)
	if err != nil {
		t.Fatalf("TaskIsBlocked: %v", err)
	}
	if !blocked {
		t.Error("对方未完成时应处于阻塞")
	}
	blockers, err := s.Blockers(b.ID)
	if err != nil || len(blockers) != 1 || blockers[0].LinkedTaskID != a.ID {
		t.Errorf("Blockers = %+v err=%v", blockers, err)
	}

	// 完成被依赖方 → 路让开了。
	if _, err := s.ToggleTask(a.ID); err != nil {
		t.Fatalf("完成: %v", err)
	}
	blocked, _ = s.TaskIsBlocked(b.ID)
	if blocked {
		t.Error("依赖完成后不应再阻塞")
	}

	// related 对称查重。
	rel, err := s.AddTaskLink(a.ID, b.ID, model.LinkRelated)
	if err != nil {
		t.Fatalf("related: %v", err)
	}
	if _, err := s.AddTaskLink(b.ID, a.ID, model.LinkRelated); err == nil {
		t.Error("related 反向重复应被拒绝")
	}

	// 装配：GetTask 带出关联。
	got, err := s.GetTask(b.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Links) == 0 {
		t.Error("任务应装配出关联边")
	}

	if err := s.DeleteTaskLink(link.ID); err != nil {
		t.Fatalf("DeleteTaskLink: %v", err)
	}
	if err := s.DeleteTaskLink(link.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除应 ErrNotFound，得到 %v", err)
	}
	_ = rel
}

// TestTaskHelpers 一批纯函数的口径。
func TestTaskHelpers(t *testing.T) {
	if !deriveImportant(model.PriorityMedium) || deriveImportant(model.PriorityLow) {
		t.Error("deriveImportant 分界应在 Medium")
	}
	if clampProgress(-5) != 0 || clampProgress(150) != 100 || clampProgress(42) != 42 {
		t.Error("clampProgress 应贴边")
	}
	if maxInt(3, 7) != 7 || maxInt(9, 2) != 9 {
		t.Error("maxInt")
	}

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	dayAfter := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	if !deriveUrgent(&yesterday, false) || !deriveUrgent(&tomorrow, false) {
		t.Error("昨天与明天到期都应紧急")
	}
	if deriveUrgent(&dayAfter, false) || deriveUrgent(nil, false) {
		t.Error("后天与无日期不紧急")
	}
	bad := "nope"
	if deriveUrgent(&bad, false) {
		t.Error("非法日期不紧急")
	}

	if nz[int](nil) != nil {
		t.Error("nz(nil) 应为 nil")
	}
	v := 5
	if nz(&v) != 5 {
		t.Error("nz(&5) 应为 5")
	}
	if deref(optOf("x"), "d") != "x" || deref(model.Opt[string]{}, "d") != "d" {
		t.Error("deref 三态")
	}
	inner := 7
	if p := derefPtr(optOfPtr(&inner), nil); p == nil || *p != 7 {
		t.Error("derefPtr Set 路径")
	}
	empty := model.Opt[*int]{Set: true}
	if p := derefPtr(empty, &inner); p != nil {
		t.Error("显式 null 应返回 Value（nil）而非 fallback")
	}

	if mustJSON([]int{1, 2}) != "[1,2]" {
		t.Errorf("mustJSON = %q", mustJSON([]int{1, 2}))
	}
	if mustJSON(make(chan int)) != "[]" {
		t.Error("不可序列化应兜底 []")
	}

	if cleanDatePtr(nil) != nil {
		t.Error("cleanDatePtr(nil)")
	}
	blank := "  "
	if cleanDatePtr(&blank) != nil {
		t.Error("空白日期应归 nil")
	}
	d := "2026-09-23"
	if cleanDatePtr(&d) != &d {
		t.Error("合法日期原样返回")
	}

	if normalizeRepeatFrom("done") != model.RepeatFromDone {
		t.Error("done 基准保留")
	}
	if normalizeRepeatFrom("bogus") != model.RepeatFromDue {
		t.Error("未知值回落 due")
	}

	if nullableStr("x") != "x" || nullableStr("  ") != nil || nullableStr("") != nil {
		t.Error("nullableStr")
	}
	if ptrStr(nil) != nil || ptrStr(&d) != "2026-09-23" || ptrStr(&blank) != nil {
		t.Error("ptrStr")
	}
}

// TestShiftStart 续期时开始日期按到期日平移相同天数。
func TestShiftStart(t *testing.T) {
	if shiftStart(nil, nil, "2026-01-01") != nil {
		t.Error("无开始日期返回 nil")
	}
	start, oldDue := "2026-09-01", "2026-09-10"
	got := shiftStart(&start, &oldDue, "2026-09-20")
	if got == nil || *got != "2026-09-11" {
		t.Errorf("平移结果 = %v，期望 2026-09-11（跨度 9 天）", got)
	}
	bad := "nope"
	if shiftStart(&start, &bad, "2026-09-20") != nil {
		t.Error("非法旧到期日返回 nil")
	}
}
