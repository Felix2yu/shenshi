package caldav

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/yufei/shendu/server/internal/model"
)

// httpErrorCode 从 go-webdav 的 *internal.HTTPError（外部无法 import）里取出状态码。
func httpErrorCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("期望得到 HTTPError，实际为 nil")
	}
	v := reflect.ValueOf(err)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			t.Fatalf("期望非空 HTTPError，得到 %v", err)
		}
		v = v.Elem()
	}
	f := v.FieldByName("Code")
	if !f.IsValid() {
		t.Fatalf("错误类型 %T 不是 HTTPError: %v", err, err)
	}
	return int(f.Int())
}

func str(s string) *string { return &s }

// TestNormalizePath 尾斜杠归一化：Apple 补斜杠后仍要命中同一路由。
func TestNormalizePath(t *testing.T) {
	cases := map[string]string{
		"/caldav/user/":  "/caldav/user",
		"/caldav/user":   "/caldav/user",
		"/":              "/",
		"":               "/",
		"/caldav/user//": "/caldav/user/",
	}
	for in, want := range cases {
		if got := normalizePath(in); got != want {
			t.Errorf("normalizePath(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestObjectPathRoundTrip 对象路径生成与解析互为逆操作。
func TestObjectPathRoundTrip(t *testing.T) {
	for _, kind := range []string{CollectionEvents, CollectionTasks} {
		p := objectPath(kind, 42)
		gotKind, id, err := parseObjectPath(p)
		if err != nil {
			t.Errorf("parseObjectPath(%q): %v", p, err)
			continue
		}
		if gotKind != kind || id != 42 {
			t.Errorf("往返结果 kind=%q id=%d，期望 %q/42", gotKind, id, kind)
		}
	}
	if p := objectPath(CollectionTasks, 7); !strings.HasSuffix(p, "shenshi-td-7.ics") {
		t.Errorf("提醒事项对象路径 = %q", p)
	}
	if p := objectPath(CollectionEvents, 7); !strings.HasSuffix(p, "shenshi-ev-7.ics") {
		t.Errorf("日程对象路径 = %q", p)
	}
}

// TestParseObjectPathRejects 错前缀、集合与 uid 不匹配、非正 id 一律 404。
func TestParseObjectPathRejects(t *testing.T) {
	bad := []string{
		EventsPath + "foo.ics",            // 非我们的 uid
		EventsPath + "shenshi-td-7.ics",   // 提醒的 uid 放进日程集合
		TasksPath + "shenshi-ev-7.ics",    // 日程的 uid 放进提醒集合
		EventsPath + "shenshi-ev-0.ics",   // id 非正
		EventsPath + "shenshi-ev--3.ics",  // 负 id
		EventsPath + "shenshi-ev-abc.ics", // 非数字
		"/elsewhere/shenshi-ev-7.ics",     // 集合目录不对
	}
	for _, p := range bad {
		if code := httpErrorCode(t, func() error { _, _, err := parseObjectPath(p); return err }()); code != 404 {
			t.Errorf("parseObjectPath(%q) 状态码 = %d，期望 404", p, code)
		}
	}
}

// TestSplitPath 拆目录与文件名（尾斜杠先剥）。
func TestSplitPath(t *testing.T) {
	cases := []struct {
		in        string
		dir, file string
	}{
		{"/a/b/c", "/a/b/", "c"},
		{"/a/b/c/", "/a/b/", "c"},
		{"c", "/", "c"},
		{"/c", "/", "c"},
	}
	for _, c := range cases {
		dir, file := splitPath(c.in)
		if dir != c.dir || file != c.file {
			t.Errorf("splitPath(%q) = (%q, %q)，期望 (%q, %q)", c.in, dir, file, c.dir, c.file)
		}
	}
}

// TestUID 三种 UID 生成器互不重叠。
func TestUID(t *testing.T) {
	if EventUID(9) != "shenshi-ev-9" || TodoUID(9) != "shenshi-td-9" {
		t.Errorf("UID 格式变了: ev=%q td=%q", EventUID(9), TodoUID(9))
	}
	if uidFor(CollectionTasks, 9) != "shenshi-td-9" {
		t.Errorf("uidFor(todos) = %q", uidFor(CollectionTasks, 9))
	}
	if uidFor(CollectionEvents, 9) != "shenshi-ev-9" {
		t.Errorf("uidFor(events) = %q", uidFor(CollectionEvents, 9))
	}
}

// TestPriorityToRFC 内部 0..3 → RFC 5545 的 1..9（1 最高，0 表示未设置）。
func TestPriorityToRFC(t *testing.T) {
	cases := map[int]int{
		model.PriorityHigh:   1,
		model.PriorityMedium: 5,
		model.PriorityLow:    7,
		model.PriorityNone:   0,
		99:                   0, // 越界按未设置处理
	}
	for in, want := range cases {
		if got := priorityToRFC(in); got != want {
			t.Errorf("priorityToRFC(%d) = %d，期望 %d", in, got, want)
		}
	}
}

// TestIsodur 提醒提前量 → ISO 8601 时长。
func TestIsodur(t *testing.T) {
	cases := map[int]string{
		0:    "PT0S",
		-10:  "PT0S",
		1440: "P1D",
		2880: "P2D",
		120:  "PT2H",
		60:   "PT1H",
		90:   "PT90M",
	}
	for in, want := range cases {
		if got := isodur(in); got != want {
			t.Errorf("isodur(%d) = %q，期望 %q", in, got, want)
		}
	}
}

// TestTaskStart 有无日期/时刻/坏值四种组合。
func TestTaskStart(t *testing.T) {
	// 无日期 → ok=false，客户端收到全天事件之外的「无时间」口径。
	if _, allDay, ok := taskStart(&model.Task{}); ok || allDay {
		t.Error("无日期应 ok=false")
	}
	empty := ""
	if _, _, ok := taskStart(&model.Task{DueDate: &empty}); ok {
		t.Error("空白日期应 ok=false")
	}
	badDate := "nope"
	if _, _, ok := taskStart(&model.Task{DueDate: &badDate}); ok {
		t.Error("非法日期应 ok=false")
	}

	// 有日期无时刻 → 全天事件。
	d := "2026-09-23"
	got, allDay, ok := taskStart(&model.Task{DueDate: &d})
	if !ok || !allDay {
		t.Errorf("无时刻应为全天事件，allDay=%v ok=%v", allDay, ok)
	}
	if got.Hour() != 0 || got.Day() != 23 {
		t.Errorf("全天事件应落在当天零点，得到 %v", got)
	}

	// 有时刻 → 定点。
	at := "08:30"
	got, allDay, ok = taskStart(&model.Task{DueDate: &d, DueTime: &at})
	if !ok || allDay {
		t.Errorf("带时刻应为定点，allDay=%v ok=%v", allDay, ok)
	}
	if got.Hour() != 8 || got.Minute() != 30 {
		t.Errorf("定点 = %v，期望 08:30", got)
	}

	// 坏时刻 → 回落全天，不让一个手误毁掉整场同步。
	_, allDay, ok = taskStart(&model.Task{DueDate: &d, DueTime: str("bad")})
	if !ok || !allDay {
		t.Errorf("坏时刻应回落全天，allDay=%v ok=%v", allDay, ok)
	}
}

// TestTaskEnd 结束时刻：正常、结束不晚于开始（退化一小时）、坏值。
func TestTaskEnd(t *testing.T) {
	start := time.Date(2026, 9, 23, 9, 0, 0, 0, time.Local)

	if _, ok := taskEnd(&model.Task{}, start); ok {
		t.Error("无结束时刻应 ok=false")
	}

	got, ok := taskEnd(&model.Task{EndTime: str("10:00")}, start)
	if !ok || got.Hour() != 10 {
		t.Errorf("taskEnd(10:00) = %v ok=%v", got, ok)
	}

	// 跨夜写错（结束=开始）→ 退化为一小时，避免零时长事件。
	got, ok = taskEnd(&model.Task{EndTime: str("09:00")}, start)
	if !ok || !got.Equal(start.Add(time.Hour)) {
		t.Errorf("结束不晚于开始应退化一小时，得到 %v ok=%v", got, ok)
	}

	if _, ok := taskEnd(&model.Task{EndTime: str("bad")}, start); ok {
		t.Error("坏结束时刻应 ok=false")
	}
}

// TestParseHHMM 时刻解析与范围校验。
func TestParseHHMM(t *testing.T) {
	cases := []struct {
		in   string
		h, m int
	}{
		{"08:30", 8, 30},
		{"8:30", 8, 30},
		{" 09:15 ", 9, 15},
		{"0:0", 0, 0},
		{"23:59", 23, 59},
	}
	for _, c := range cases {
		h, m, err := parseHHMM(c.in)
		if err != nil || h != c.h || m != c.m {
			t.Errorf("parseHHMM(%q) = (%d,%d,%v)，期望 (%d,%d)", c.in, h, m, err, c.h, c.m)
		}
	}
	for _, bad := range []string{"24:00", "08:60", "abc", "0830", "-1:00", "", "08:"} {
		if _, _, err := parseHHMM(bad); err == nil {
			t.Errorf("parseHHMM(%q) 应报错", bad)
		}
	}
}

// TestParseStamp 三种输入：RFC3339、纯日期、垃圾 → 零值。
func TestParseStamp(t *testing.T) {
	if got := parseStamp("2026-09-23T08:30:00+08:00"); got.IsZero() || got.Hour() != 8 {
		t.Errorf("RFC3339 解析 = %v", got)
	}
	if got := parseStamp("2026-09-23"); got.IsZero() || got.Day() != 23 || got.Hour() != 0 {
		t.Errorf("纯日期解析 = %v", got)
	}
	if got := parseStamp(""); !got.IsZero() {
		t.Errorf("空串应得零值，得到 %v", got)
	}
	if got := parseStamp("not-a-time"); !got.IsZero() {
		t.Errorf("垃圾串应得零值，得到 %v", got)
	}
}

// TestTodoCompleted 读 VTODO 完成状态的四条分支（手机端勾选的入口）。
func TestTodoCompleted(t *testing.T) {
	makeCal := func(setup func(*ical.Component)) *ical.Calendar {
		cal := ical.NewCalendar()
		comp := ical.NewComponent(ical.CompToDo)
		setup(comp)
		cal.Children = append(cal.Children, comp)
		return cal
	}

	// STATUS=COMPLETED → 勾掉。
	c, has := todoCompleted(makeCal(func(c *ical.Component) {
		c.Props.SetText(ical.PropStatus, "COMPLETED")
	}))
	if !c || !has {
		t.Errorf("COMPLETED: completed=%v hasStatus=%v", c, has)
	}

	// STATUS=NEEDS-ACTION → 未勾，但有状态。
	c, has = todoCompleted(makeCal(func(c *ical.Component) {
		c.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
	}))
	if c || !has {
		t.Errorf("NEEDS-ACTION: completed=%v hasStatus=%v", c, has)
	}

	// 无 STATUS 但有 COMPLETED 属性 → 也算勾掉。
	c, has = todoCompleted(makeCal(func(c *ical.Component) {
		c.Props.SetText(ical.PropCompleted, "2026-09-23T08:00:00Z")
	}))
	if !c || !has {
		t.Errorf("仅 COMPLETED 属性: completed=%v hasStatus=%v", c, has)
	}

	// 两者皆无 → 不动服务端状态。
	c, has = todoCompleted(makeCal(func(c *ical.Component) {}))
	if c || has {
		t.Errorf("无状态: completed=%v hasStatus=%v", c, has)
	}

	// 没有 VTODO 组件。
	empty := ical.NewCalendar()
	if c, has := todoCompleted(empty); c || has {
		t.Errorf("空日历: completed=%v hasStatus=%v", c, has)
	}
}

// TestPathConstantsAndResolve 层级压不得：主体 < 主目录 < 集合，resolveCollection 带不带尾斜杠都认。
func TestPathConstantsAndResolve(t *testing.T) {
	if !strings.HasPrefix(HomeSetPath, PrincipalPath) {
		t.Error("主目录必须挂在主体之下")
	}
	if !strings.HasPrefix(EventsPath, HomeSetPath) || !strings.HasPrefix(TasksPath, HomeSetPath) {
		t.Error("集合必须挂在主目录之下")
	}

	b := &Backend{}
	for _, p := range []string{EventsPath, strings.TrimSuffix(EventsPath, "/"), TasksPath, strings.TrimSuffix(TasksPath, "/")} {
		if _, _, err := b.resolveCollection(p); err != nil {
			t.Errorf("resolveCollection(%q) 应放行，却报错: %v", p, err)
		}
	}
	if _, _, err := b.resolveCollection("/caldav/user/calendars/nope/"); err == nil {
		t.Error("未知集合应 404")
	}
}

// TestForbiddenAndErrorWrappers Apple 兼容：不支持的操作返回 403 绝不 501；
// 数据库错误 503（客户端会重试），查无此物 404。
func TestForbiddenAndErrorWrappers(t *testing.T) {
	b := &Backend{}
	if code := httpErrorCode(t, b.CreateCalendar(nil, nil)); code != 403 {
		t.Errorf("CreateCalendar 状态码 = %d，Apple 见到 501 会放弃；必须 403", code)
	}
	if code := httpErrorCode(t, unavailable(errors.New("db down"))); code != 503 {
		t.Errorf("unavailable 状态码 = %d，期望 503（客户端会重试，500 被当成永久失败）", code)
	}
	if code := httpErrorCode(t, notFound(errors.New("x"))); code != 404 {
		t.Errorf("notFound 状态码 = %d，期望 404", code)
	}
}

// TestCalName 两个集合的展示名。
func TestCalName(t *testing.T) {
	if calName(CollectionTasks) == calName(CollectionEvents) || calName(CollectionEvents) == "" {
		t.Errorf("calName: tasks=%q events=%q", calName(CollectionTasks), calName(CollectionEvents))
	}
}
