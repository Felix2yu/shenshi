package store

import (
	"testing"
	"time"
)

// TestResolveDueTime 「日期 + 可选时刻」到时间点的口径：
// 无时刻/坏时刻回落到 09:00（人清醒的时段），非法日期直接失败。
func TestResolveDueTime(t *testing.T) {
	got, ok := resolveDueTime("2026-09-23", nil)
	if !ok || got.Hour() != 9 || got.Minute() != 0 {
		t.Errorf("无时刻应回落 09:00，得到 %v ok=%v", got, ok)
	}

	at := "08:30"
	got, ok = resolveDueTime("2026-09-23", &at)
	if !ok || got.Hour() != 8 || got.Minute() != 30 {
		t.Errorf("带时刻应取 08:30，得到 %v ok=%v", got, ok)
	}
	want := time.Date(2026, time.September, 23, 8, 30, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("解析结果 = %v，期望 %v", got, want)
	}

	// 长度不是 5 的时刻（如手填 8:30）按无效处理，回落 09:30 之外的默认 09:00。
	bad := "8:30"
	got, _ = resolveDueTime("2026-09-23", &bad)
	if got.Hour() != 9 {
		t.Errorf("非 HH:MM 时刻应回落 09:00，得到 %v", got)
	}

	if _, ok := resolveDueTime("2026-02-31", nil); ok {
		t.Error("非法日期应返回 ok=false")
	}
	if _, ok := resolveDueTime("nope", nil); ok {
		t.Error("非日期串应返回 ok=false")
	}
}

// TestReminderKey 台账键格式：taskID|fireAt。
func TestReminderKey(t *testing.T) {
	if got := key(7, "2026-09-23T08:30:00+08:00"); got != "7|2026-09-23T08:30:00+08:00" {
		t.Errorf("key = %q", got)
	}
	// 子任务回执 id 为负，须与任务 id 空间隔离。
	if got := key(-3, "x"); got != "-3|x" {
		t.Errorf("负数 id 的 key = %q", got)
	}
}

// TestDescribeDue 到期描述的分档：逾期 / 今天 / 明天 / 后天 / 具体日期。
func TestDescribeDue(t *testing.T) {
	now := time.Date(2026, time.September, 23, 10, 0, 0, 0, time.Local)
	day := func(offset int, hour int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day()+offset, hour, 5, 0, 0, time.Local)
	}
	cases := []struct {
		name string
		due  time.Time
		want string
	}{
		{"昨天已逾期（只给时刻）", day(-1, 9), "已逾期 09:05"},
		{"今天", day(0, 18), "今天 18:05"},
		{"明天", day(1, 9), "明天 09:05"},
		{"后天", day(2, 9), "后天 09:05"},
		{"五天后带日期", day(5, 9), "9月28日 09:05"},
	}
	for _, c := range cases {
		if got := describeDue(c.due, now); got != c.want {
			t.Errorf("%s: describeDue = %q，期望 %q", c.name, got, c.want)
		}
	}
}
