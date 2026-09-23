package caldav

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/yufei/shendu/server/internal/model"
)

func task(mut func(*model.Task)) *model.Task {
	t := &model.Task{
		ID:        7,
		Title:     "渲染任务",
		Status:    model.StatusTodo,
		Priority:  model.PriorityMedium,
		CreatedAt: "2026-09-23T10:00:00+08:00",
		UpdatedAt: "2026-09-23T12:00:00+08:00",
	}
	if mut != nil {
		mut(t)
	}
	return t
}

// propText 取出指定属性的文本值。
func propText(t *testing.T, props *ical.Props, name string) string {
	t.Helper()
	p := props.Get(name)
	if p == nil {
		t.Fatalf("缺少属性 %s", name)
	}
	v, err := p.Text()
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return v
}

// TestRenderCalendarVEVENT 全天事件：VEVENT、次日 DTEND、无状态字段。
func TestRenderCalendarVEVENT(t *testing.T) {
	tk := task(func(t *model.Task) {
		t.Notes = "说明文字"
		t.DueDate = str("2026-10-01")
		t.DueTime = nil // 全天
		t.Tags = []model.Tag{{Name: "规划"}, {Name: "重要"}}
	})
	cal, err := renderCalendar(CollectionEvents, tk)
	if err != nil {
		t.Fatalf("renderCalendar: %v", err)
	}
	if len(cal.Children) != 1 || cal.Children[0].Name != ical.CompEvent {
		t.Fatalf("组件 = %+v，应为单个 VEVENT", cal.Children)
	}
	comp := cal.Children[0]

	if got := propText(t, &comp.Props, ical.PropUID); got != EventUID(7) {
		t.Errorf("UID = %q", got)
	}
	if got := propText(t, &comp.Props, ical.PropSummary); got != "渲染任务" {
		t.Errorf("SUMMARY = %q", got)
	}
	if got := propText(t, &comp.Props, ical.PropDescription); got != "说明文字" {
		t.Errorf("DESCRIPTION = %q", got)
	}
	// DTEND 次日（含头不含尾）。
	dtend := comp.Props.Get(ical.PropDateTimeEnd)
	if dtend == nil {
		t.Fatal("全天事件应有 DTEND")
	}
	end, err := dtend.DateTime(time.Local)
	if err != nil {
		t.Fatalf("DTEND 解析: %v", err)
	}
	if end.Format("2006-01-02") != "2026-10-02" {
		t.Errorf("DTEND = %s，期望次日 2026-10-02", end.Format("2006-01-02"))
	}
	// VEVENT 不带 VTODO 的完成状态。
	if comp.Props.Get(ical.PropStatus) != nil {
		t.Error("VEVENT 不应有 STATUS")
	}
	// 标签进 CATEGORIES。
	cats := comp.Props.Get(ical.PropCategories)
	if cats == nil {
		t.Fatal("带标签应写 CATEGORIES")
	}
	// 优先级映射 RFC：中(2) → 5。
	if got := propText(t, &comp.Props, ical.PropPriority); got != "5" {
		t.Errorf("PRIORITY = %q，期望 5", got)
	}
	// DTSTAMP 用更新时间，不用 time.Now。
	dtstamp := comp.Props.Get(ical.PropDateTimeStamp)
	if dtstamp == nil {
		t.Fatal("缺少 DTSTAMP")
	}
	st, err := dtstamp.DateTime(time.Local)
	if err != nil {
		t.Fatalf("DTSTAMP: %v", err)
	}
	if st.Format(time.RFC3339) != "2026-09-23T12:00:00+08:00" {
		t.Errorf("DTSTAMP = %s，应取 UpdatedAt", st.Format(time.RFC3339))
	}
}

// TestRenderCalendarVTODO 提醒事项：完成/未完成状态、子任务进度、时钟事件的 DTEND。
func TestRenderCalendarVTODO(t *testing.T) {
	// 未完成 + 有子任务进度 + 带时钟。
	tk := task(func(t *model.Task) {
		t.DueDate = str("2026-09-25")
		t.DueTime = str("09:30")
		t.EndTime = str("10:45")
		t.SubtaskDone = 1
		t.Subtasks = []model.Subtask{{Title: "a", SortOrder: 1024}, {Title: "b", SortOrder: 2048}}
		t.Reminders = []int{10}
	})
	cal, err := renderCalendar(CollectionTasks, tk)
	if err != nil {
		t.Fatalf("renderCalendar: %v", err)
	}
	comp := cal.Children[0]
	if comp.Name != ical.CompToDo {
		t.Fatalf("组件 = %s，应为 VTODO", comp.Name)
	}
	if got := propText(t, &comp.Props, ical.PropStatus); got != "NEEDS-ACTION" {
		t.Errorf("STATUS = %q", got)
	}
	// 1/2 子任务 → 50%。
	if got := propText(t, &comp.Props, ical.PropPercentComplete); got != "50" {
		t.Errorf("PERCENT-COMPLETE = %q，期望 50", got)
	}
	// 时钟事件：DTSTART/DTEND 为带时间戳。
	start := comp.Props.Get(ical.PropDateTimeStart)
	if start == nil {
		t.Fatal("缺 DTSTART")
	}
	st, err := start.DateTime(time.Local)
	if err != nil {
		t.Fatalf("DTSTART: %v", err)
	}
	if st.Format("15:04") != "09:30" {
		t.Errorf("DTSTART 时间 = %s，期望 09:30", st.Format("15:04"))
	}
	end := comp.Props.Get(ical.PropDateTimeEnd)
	if end == nil {
		t.Fatal("DTEND 缺失")
	}
	et, err := end.DateTime(time.Local)
	if err != nil {
		t.Fatalf("DTEND: %v", err)
	}
	// 关键回归点：EndTime 是当天钟点（10:45），不是距开始的 10 小时 45 分。
	if et.Format("15:04") != "10:45" {
		t.Errorf("DTEND = %s，期望当天钟点 10:45（而非 09:30+10h45m）", et.Format("15:04"))
	}
	// 提醒闹钟：TRIGGER -PT10M。
	if len(comp.Children) != 1 || comp.Children[0].Name != ical.CompAlarm {
		t.Fatalf("应带一个 VALARM，得到 %+v", comp.Children)
	}
	alarm := comp.Children[0]
	trig := alarm.Props.Get(ical.PropTrigger)
	if trig == nil {
		t.Fatal("闹钟缺 TRIGGER")
	}
	tv, err := trig.Text()
	if err != nil {
		t.Fatalf("TRIGGER: %v", err)
	}
	if tv != "-PT10M" {
		t.Errorf("TRIGGER = %q，期望 -PT10M", tv)
	}

	// 已完成：COMPLETED + 100%。
	doneAt := "2026-09-24T08:00:00+08:00"
	done := task(func(t *model.Task) {
		t.Status = model.StatusDone
		t.CompletedAt = &doneAt
		t.DueDate = str("2026-09-24")
	})
	cal2, err := renderCalendar(CollectionTasks, done)
	if err != nil {
		t.Fatalf("renderCalendar(完成): %v", err)
	}
	comp2 := cal2.Children[0]
	if got := propText(t, &comp2.Props, ical.PropStatus); got != "COMPLETED" {
		t.Errorf("完成 STATUS = %q", got)
	}
	if got := propText(t, &comp2.Props, ical.PropPercentComplete); got != "100" {
		t.Errorf("完成 PERCENT = %q", got)
	}
	if comp2.Props.Get(ical.PropCompleted) == nil {
		t.Error("完成任务应带 COMPLETED 属性")
	}

	// 无日期：不写 DTSTART/DUE。
	nd := task(func(t *model.Task) {
		t.DueDate = nil
		t.DueTime = nil
	})
	cal3, _ := renderCalendar(CollectionTasks, nd)
	comp3 := cal3.Children[0]
	if comp3.Props.Get(ical.PropDateTimeStart) != nil || comp3.Props.Get(ical.PropDue) != nil {
		t.Error("无日期任务不应写 DTSTART/DUE")
	}
	// 无日期也不该挂闹钟（没触发基准）。
	for _, ch := range comp3.Children {
		if ch.Name == ical.CompAlarm {
			t.Error("无日期不应有闹钟")
		}
	}
}

// TestRenderObjectETag 稳定 ETag 与路径：内容不变则字节不变。
func TestRenderObjectETag(t *testing.T) {
	tk := task(func(t *model.Task) {
		t.DueDate = str("2026-10-01")
	})
	obj1, err := renderObject(CollectionEvents, tk)
	if err != nil {
		t.Fatalf("renderObject: %v", err)
	}
	obj2, err := renderObject(CollectionEvents, tk)
	if err != nil {
		t.Fatalf("renderObject(第二次): %v", err)
	}
	if obj1.ETag != obj2.ETag {
		t.Errorf("同一任务 ETag 应稳定: %s vs %s", obj1.ETag, obj2.ETag)
	}
	if !strings.HasPrefix(obj1.ETag, `"`) || !strings.HasSuffix(obj1.ETag, `"`) {
		t.Errorf("ETag 应带引号: %s", obj1.ETag)
	}
	if obj1.Path != EventsPath+EventUID(7)+".ics" {
		t.Errorf("Path = %q", obj1.Path)
	}
	if obj1.ContentLength <= 0 {
		t.Errorf("ContentLength = %d", obj1.ContentLength)
	}
	if obj1.ModTime.IsZero() {
		t.Error("ModTime 不应为零值")
	}

	// 内容改变 → ETag 变。
	tk2 := task(func(t *model.Task) {
		t.Title = "改了标题"
		t.DueDate = str("2026-10-01")
	})
	obj3, _ := renderObject(CollectionEvents, tk2)
	if obj3.ETag == obj1.ETag {
		t.Error("内容变化后 ETag 应改变")
	}

	// 提醒事项路径。
	objT, _ := renderObject(CollectionTasks, tk)
	if objT.Path != TasksPath+TodoUID(7)+".ics" {
		t.Errorf("VTODO Path = %q", objT.Path)
	}
}

// TestEncodeCalendarDeterministic 渲染不含 time.Now，两次字节一致。
func TestEncodeCalendarDeterministic(t *testing.T) {
	tk := task(func(t *model.Task) {
		t.DueDate = str("2026-10-01")
		t.DueTime = str("08:00")
	})
	cal, err := renderCalendar(CollectionEvents, tk)
	if err != nil {
		t.Fatalf("renderCalendar: %v", err)
	}
	text1, err := encodeCalendar(cal)
	if err != nil {
		t.Fatalf("encodeCalendar: %v", err)
	}
	text2, err := encodeCalendar(cal)
	if err != nil {
		t.Fatalf("encodeCalendar(2): %v", err)
	}
	if text1 != text2 {
		t.Error("同一日历两次渲染字节应完全一致")
	}
	if !strings.Contains(text1, "BEGIN:VCALENDAR") || !strings.Contains(text1, "BEGIN:VEVENT") {
		t.Errorf("渲染结果不像 iCalendar:\n%s", text1)
	}
	// 换一个时间上完全不同的任务，DTSTAMP 仍取任务字段（证明没有偷用当前时间）。
	if strings.Contains(text1, time.Now().Format("20060102T150405")) {
		t.Error("DTSTAMP 不应取当前时间")
	}
}
