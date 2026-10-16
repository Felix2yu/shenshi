package push

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/model"
	"github.com/yufei/shendu/server/internal/store"
)

// newTestStore 开一个临时库（含迁移与种子数据）。
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func opt[T any](v T) model.Opt[T] { return model.Opt[T]{Set: true, Value: v} }

func optPtr[T any](v T) model.Opt[*T] { return model.Opt[*T]{Set: true, Value: &v} }

// clearTasks 清掉种子数据里的任务，让计数与样例断言从零起算。
func clearTasks(t *testing.T, s *store.Store) {
	t.Helper()
	tasks, err := s.ListTasks(store.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, task := range tasks {
		if err := s.DeleteTask(task.ID); err != nil {
			t.Fatalf("DeleteTask(%d): %v", task.ID, err)
		}
	}
}

// mustCreateTask 建一条带到期信息与提醒点的任务，落在收集箱。
func mustCreateTask(t *testing.T, s *store.Store, title, dueDate, dueTime string, reminders []int) *model.Task {
	t.Helper()
	in := model.TaskInput{
		Title:     opt(title),
		DueDate:   optPtr(dueDate),
		Reminders: opt(reminders),
	}
	if dueTime != "" {
		in.DueTime = optPtr(dueTime)
	}
	inbox, err := s.InboxListID()
	if err != nil {
		t.Fatalf("InboxListID: %v", err)
	}
	task, err := s.CreateTask(in, inbox)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// createRecentTask 建一条「提醒触发点在 2 分钟前」的任务（offset 0，准点触发），
// 保证落在 5 分钟补推窗口内，不受测试运行时刻影响。
func createRecentTask(t *testing.T, s *store.Store, title string) {
	t.Helper()
	fireAt := time.Now().Add(-2 * time.Minute).Truncate(time.Minute)
	mustCreateTask(t, s, title, fireAt.Format("2006-01-02"), fireAt.Format("15:04"), []int{0})
}

// newTestPusher 注入假发送器，记录每次推送的 (title, body)。
func newTestPusher(t *testing.T, s *store.Store, failFirst int) (*Pusher, *[]string) {
	t.Helper()
	sent := &[]string{}
	remaining := failFirst
	p := New(s, t.Logf)
	p.send = func(urls []string, title, body string) error {
		if remaining > 0 {
			remaining--
			return errors.New("模拟发送失败")
		}
		*sent = append(*sent, title+"|"+body)
		return nil
	}
	return p, sent
}

func setPushSettings(t *testing.T, s *store.Store, kv map[string]string) {
	t.Helper()
	if err := s.SaveSettings(kv); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
}

func baseSettings() map[string]string {
	return map[string]string{
		SetEnabled: "1",
		SetURLs:    "ntfy://example/topic\n# 注释行\n\nbark://key@api.day.app",
	}
}

// TestLoadConfig 配置解析：拆行、去注释、时间校验与默认值。
func TestLoadConfig(t *testing.T) {
	cfg := LoadConfig(baseSettings())
	if !cfg.Enabled {
		t.Error("enabled=1 应解析为开启")
	}
	if cfg.DailyEnabled {
		t.Error("未设置 dailyEnabled 不应默认开启")
	}
	if len(cfg.URLs) != 2 {
		t.Fatalf("应解析出 2 条 URL（跳过注释与空行），得到 %d: %v", len(cfg.URLs), cfg.URLs)
	}
	if cfg.DailyTime != "09:00" {
		t.Errorf("未设置每日时间应默认 09:00，得到 %q", cfg.DailyTime)
	}

	cfg = LoadConfig(map[string]string{SetDailyTime: "8:30"})
	if cfg.DailyTime != "09:00" {
		t.Errorf("非法时间应回落默认 09:00，得到 %q", cfg.DailyTime)
	}
	cfg = LoadConfig(map[string]string{SetDailyTime: "07:5x"})
	if cfg.DailyTime != "09:00" {
		t.Errorf("非法时间应回落默认 09:00，得到 %q", cfg.DailyTime)
	}
	cfg = LoadConfig(map[string]string{SetDailyTime: "23:59"})
	if cfg.DailyTime != "23:59" {
		t.Errorf("合法时间应原样保留，得到 %q", cfg.DailyTime)
	}
}

// TestKeys 台账键格式。
func TestKeys(t *testing.T) {
	if got := ReminderKey(7, "x"); got != "R|7|x" {
		t.Errorf("ReminderKey = %q", got)
	}
	if got := ReminderKey(-3, "x"); got != "R|-3|x" {
		t.Errorf("子任务负 id 的 ReminderKey = %q", got)
	}
	if got := DailyKey("2026-10-01"); got != "D|2026-10-01" {
		t.Errorf("DailyKey = %q", got)
	}
}

// TestTickReminderOnce 提醒只推一次：同一条提醒第二个周期不得重复推送。
func TestTickReminderOnce(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	createRecentTask(t, s, "写周报")
	setPushSettings(t, s, baseSettings())

	p, sent := newTestPusher(t, s, 0)
	p.tick(now)
	if len(*sent) != 1 {
		t.Fatalf("应推送 1 次，得到 %d 次: %v", len(*sent), *sent)
	}
	wantTitle := "慎始 · 写周报"
	if !slices.ContainsFunc(*sent, func(s string) bool { return len(s) >= len(wantTitle) && s[:len(wantTitle)] == wantTitle }) {
		t.Errorf("推送标题应含 %q: %v", wantTitle, *sent)
	}

	// 再巡检一轮：已落账，不再推。
	p.tick(now.Add(tickEvery))
	if len(*sent) != 1 {
		t.Errorf("重复巡检不应再推，得到 %d 次", len(*sent))
	}
}

// TestTickSkippedEarly 提前量提醒不该被服务端推：lookahead=0，只推已到点的。
func TestTickSkippedEarly(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	// 到期 10:00，提醒提前 30 分钟 → 09:30 触发；现在 09:10 还没到。
	mustCreateTask(t, s, "提前提醒不推", now.Format("2006-01-02"), "10:00", []int{30})
	setPushSettings(t, s, baseSettings())

	p, sent := newTestPusher(t, s, 0)
	tenOclock := time.Date(now.Year(), now.Month(), now.Day(), 9, 10, 0, 0, time.Local)
	p.tick(tenOclock)
	if len(*sent) != 0 {
		t.Errorf("未到触发点不应推送: %v", *sent)
	}
}

// TestTickDisabled 未开启或未配置地址时不推。
func TestTickDisabled(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	mustCreateTask(t, s, "开关测试", now.Format("2006-01-02"), "10:00", []int{0})

	p, sent := newTestPusher(t, s, 0)
	// 未开启
	p.tick(now)
	if len(*sent) != 0 {
		t.Errorf("未开启时不应推送: %v", *sent)
	}
	// 开启但没地址
	setPushSettings(t, s, map[string]string{SetEnabled: "1"})
	p.tick(now)
	if len(*sent) != 0 {
		t.Errorf("无地址时不应推送: %v", *sent)
	}
}

// TestTickRetryLimit 发送失败重试至多 3 次，之后放弃。
func TestTickRetryLimit(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	clearTasks(t, s)
	fireAt := now.Add(-2 * time.Minute).Truncate(time.Minute)
	mustCreateTask(t, s, "重试测试", fireAt.Format("2006-01-02"), fireAt.Format("15:04"), []int{0})
	setPushSettings(t, s, baseSettings())

	p, sent := newTestPusher(t, s, 99) // 永远失败
	for i := 0; i < 5; i++ {
		p.tick(now.Add(time.Duration(i) * tickEvery))
	}
	if len(*sent) != 0 {
		t.Errorf("持续失败不应有成功推送: %v", *sent)
	}
	state, err := s.PushState(ReminderKey(mustFirstTaskID(t, s), fireAt.Format(time.RFC3339)))
	if err != nil {
		t.Fatalf("PushState: %v", err)
	}
	// fireAt 的时区串因机器而异，这里只关心尝试次数封顶。
	if state.Attempts > 3 {
		t.Errorf("尝试次数应封顶 3，得到 %d", state.Attempts)
	}
	// 次数用尽后 Done，不再重试。
	if !state.Done {
		t.Error("尝试次数用尽后应标记 Done")
	}
}

func mustFirstTaskID(t *testing.T, s *store.Store) int64 {
	t.Helper()
	tasks, err := s.ListTasks(store.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("没有任务")
	}
	return tasks[0].ID
}

// TestDailyDigest 每日概览：到点推一次、含今日与逾期计数、空日不推。
func TestDailyDigest(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	clearTasks(t, s)
	today := now.Format("2006-01-02")
	mustCreateTask(t, s, "今日事项", today, "18:00", nil)
	mustCreateTask(t, s, "逾期事项", "2026-01-01", "09:00", nil)
	kv := baseSettings()
	kv[SetDailyEnabled] = "1"
	kv[SetDailyTime] = "08:00"
	setPushSettings(t, s, kv)

	p, sent := newTestPusher(t, s, 0)
	// 07:00 还没到点
	p.tick(time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, time.Local))
	if len(*sent) != 0 {
		t.Errorf("未到每日时点不应推送: %v", *sent)
	}
	// 09:00 已过点，推一条
	p.tick(time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.Local))
	if len(*sent) != 1 {
		t.Fatalf("每日概览应推送 1 次，得到 %d: %v", len(*sent), *sent)
	}
	if (*sent)[0] != "慎始 · 今日概览|今日待办 1 项，已逾期 1 项：今日事项" {
		t.Errorf("概览内容不符: %q", (*sent)[0])
	}
	// 再巡检不重推
	p.tick(time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local))
	if len(*sent) != 1 {
		t.Errorf("每日概览不应重复推送: %v", *sent)
	}
}

// TestDailyEmpty 空概览不推送（但要落账）。
func TestDailyEmpty(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	clearTasks(t, s)
	kv := baseSettings()
	kv[SetDailyEnabled] = "1"
	setPushSettings(t, s, kv)

	p, sent := newTestPusher(t, s, 0)
	// 用固定时点而不是 time.Now()：每日概览的默认时点是 09:00，
	// 在早上九点之前跑测试会直接走到「未到点」分支，落账也就无从谈起。
	p.tick(time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local))
	if len(*sent) != 0 {
		t.Errorf("空概览不应推送: %v", *sent)
	}
	state, err := s.PushState(DailyKey(now.Format("2006-01-02")))
	if err != nil {
		t.Fatalf("PushState: %v", err)
	}
	if !state.Done || !state.OK {
		t.Errorf("空概览应落账成功: %+v", state)
	}
}

// TestDailySummary DailySummary 的计数口径：待办与进行中计入，已完成与归档不计。
func TestDailySummary(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	clearTasks(t, s)
	today := now.Format("2006-01-02")
	mustCreateTask(t, s, "甲", today, "09:00", nil)
	mustCreateTask(t, s, "乙", today, "", nil)
	done := mustCreateTask(t, s, "已完成", today, "", nil)
	if _, err := s.ToggleTask(done.ID); err != nil {
		t.Fatalf("ToggleTask: %v", err)
	}
	mustCreateTask(t, s, "逾期件", "2026-01-02", "", nil)

	sum, err := s.DailySummary(now)
	if err != nil {
		t.Fatalf("DailySummary: %v", err)
	}
	if sum.Today != 2 {
		t.Errorf("今日应计 2 件，得到 %d", sum.Today)
	}
	if sum.Overdue != 1 {
		t.Errorf("逾期应计 1 件，得到 %d", sum.Overdue)
	}
	if len(sum.Sample) != 2 {
		t.Fatalf("样例应有 2 条，得到 %v", sum.Sample)
	}
	// 有时刻的排前（按时间），无时刻的殿后。
	if sum.Sample[0] != "甲" {
		t.Errorf("样例排序不符: %v", sum.Sample)
	}
}
