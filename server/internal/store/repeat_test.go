package store

import (
	"testing"
	"time"
)

// mustDay 解析 YYYY-MM-DD（本地时区），失败即终止——测试里日期写错是笔误不是断言。
func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatalf("测试日期 %q 无法解析: %v", s, err)
	}
	return v
}

// TestNextOccurrence 覆盖全部规则分支：合法推进、顺延钳制与非法输入。
// 基准日 2026-09-22 是周二，星期类用例以它为锚。
func TestNextOccurrence(t *testing.T) {
	cases := []struct {
		name     string
		rule     string
		from     string
		want     string // "" 表示 ok=false
		wantRule string // "" 表示不校验 nextRule
	}{
		// —— daily / weekdays ——
		{"daily", "daily", "2026-09-22", "2026-09-23", ""},
		{"weekdays 从周二", "weekdays", "2026-09-22", "2026-09-23", ""},
		{"weekdays 从周六跳到周一", "weekdays", "2026-09-26", "2026-09-28", ""},
		{"weekdays 从周日跳到周一", "weekdays", "2026-09-27", "2026-09-28", ""},

		// —— weekly ——
		{"weekly 裸规则加七天", "weekly", "2026-09-22", "2026-09-29", ""},
		{"weekly: 空参数等同裸规则", "weekly:", "2026-09-22", "2026-09-29", ""},
		{"weekly:3 下一个周三", "weekly:3", "2026-09-22", "2026-09-23", ""},
		{"weekly:0 下一个周日", "weekly:0", "2026-09-22", "2026-09-27", ""},
		{"weekly:1,3,5 取最近的周三", "weekly:1,3,5", "2026-09-22", "2026-09-23", ""},
		{"weekly:6 下一个周六", "weekly:6", "2026-09-22", "2026-09-26", ""},
		{"weekly:7 非法星期", "weekly:7", "2026-09-22", "", ""},
		{"weekly:-1 非法星期", "weekly:-1", "2026-09-22", "", ""},
		{"weekly:abc 非数字", "weekly:abc", "2026-09-22", "", ""},

		// —— monthly ——
		{"monthly 裸规则下月同日", "monthly", "2026-09-22", "2026-10-22", ""},
		{"monthly 月末钳制到 2/28", "monthly", "2026-01-31", "2026-02-28", ""},
		{"monthly:15 每月 15 日", "monthly:15", "2026-09-22", "2026-10-15", ""},
		{"monthly:31 在 2 月顺延到月末", "monthly:31", "2026-01-31", "2026-02-28", ""},
		{"monthly:last 下月最后一天", "monthly:last", "2026-09-22", "2026-10-31", ""},
		{"monthly:last 在 2 月", "monthly:last", "2026-01-15", "2026-02-28", ""},
		{"monthly:lastworkday 10/31 是周六退到周五", "monthly:lastworkday", "2026-09-22", "2026-10-30", ""},
		{"monthly:nth:3:0 第三个周日", "monthly:nth:3:0", "2026-09-22", "2026-10-18", ""},
		{"monthly:nth:5:1 第五个周一该月没有", "monthly:nth:5:1", "2026-09-22", "", ""},
		{"monthly:nth:0:1 序号非法", "monthly:nth:0:1", "2026-09-22", "", ""},
		{"monthly:nth:1:7 星期非法", "monthly:nth:1:7", "2026-09-22", "", ""},
		{"monthly:nth:x:1 非数字", "monthly:nth:x:1", "2026-09-22", "", ""},
		{"monthly:0 日号非法", "monthly:0", "2026-09-22", "", ""},
		{"monthly:32 日号非法", "monthly:32", "2026-09-22", "", ""},
		{"monthly:xx 非数字", "monthly:xx", "2026-09-22", "", ""},

		// —— yearly ——
		{"yearly 裸规则明年同日", "yearly", "2026-09-22", "2027-09-22", ""},
		{"yearly:3-15 指定月日", "yearly:3-15", "2026-09-22", "2027-03-15", ""},
		{"yearly:2-29 平年钳制到 2/28", "yearly:2-29", "2026-09-22", "2027-02-28", ""},
		{"yearly:2-29 闰年保留 2/29", "yearly:2-29", "2027-06-01", "2028-02-29", ""},
		{"yearly:13-1 月份非法", "yearly:13-1", "2026-09-22", "", ""},
		{"yearly:0-1 月份非法", "yearly:0-1", "2026-09-22", "", ""},
		{"yearly:3-32 日号非法", "yearly:3-32", "2026-09-22", "", ""},
		{"yearly:abc 缺少月日", "yearly:abc", "2026-09-22", "", ""},

		// —— every ——
		{"every:3:day", "every:3:day", "2026-09-22", "2026-09-25", ""},
		{"every:2:week", "every:2:week", "2026-09-22", "2026-10-06", ""},
		{"every:6:month 从 1/31 钳制", "every:6:month", "2026-01-31", "2026-07-31", ""},
		{"every:1:year 从 2/29 钳制", "every:1:year", "2024-02-29", "2025-02-28", ""},
		{"every:0:day 间隔非法", "every:0:day", "2026-09-22", "", ""},
		{"every:3:hour 单位非法", "every:3:hour", "2026-09-22", "", ""},
		{"every:3 缺单位", "every:3", "2026-09-22", "", ""},
		{"every:x:day 间隔非数字", "every:x:day", "2026-09-22", "", ""},

		// —— ebbinghaus ——
		{"ebbinghaus 无参数从下标 0 起", "ebbinghaus", "2026-09-22", "2026-09-23", "ebbinghaus:1"},
		{"ebbinghaus:0 首次间隔一天", "ebbinghaus:0", "2026-09-22", "2026-09-23", "ebbinghaus:1"},
		{"ebbinghaus:2 第三档 +4 天", "ebbinghaus:2", "2026-09-22", "2026-09-26", "ebbinghaus:3"},
		{"ebbinghaus:6 末档 +60 天", "ebbinghaus:6", "2026-09-22", "2026-11-21", "ebbinghaus:7"},
		{"ebbinghaus:7 下标越界", "ebbinghaus:7", "2026-09-22", "", ""},
		{"ebbinghaus:xx 非数字按 0 处理", "ebbinghaus:xx", "2026-09-22", "2026-09-23", "ebbinghaus:1"},

		// —— 非法整体 ——
		{"空规则", "", "2026-09-22", "", ""},
		{"纯空白规则", "   ", "2026-09-22", "", ""},
		{"未知规则头", "fortnightly", "2026-09-22", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			from := mustDay(t, c.from)
			next, nextRule, ok := NextOccurrence(c.rule, from)
			if c.want == "" {
				if ok {
					t.Fatalf("规则 %q 期望失败，却得到 %s", c.rule, next.Format("2006-01-02"))
				}
				return
			}
			if !ok {
				t.Fatalf("规则 %q 期望得到 %s，却失败了", c.rule, c.want)
			}
			if got := next.Format("2006-01-02"); got != c.want {
				t.Errorf("规则 %q 下一次 = %s，期望 %s", c.rule, got, c.want)
			}
			if next.Hour() != 0 || next.Minute() != 0 || next.Second() != 0 {
				t.Errorf("规则 %q 结果未归零到日: %s", c.rule, next)
			}
			if c.wantRule != "" && nextRule != c.wantRule {
				t.Errorf("规则 %q nextRule = %q，期望 %q", c.rule, nextRule, c.wantRule)
			}
			if c.wantRule == "" && nextRule != c.rule {
				t.Errorf("规则 %q nextRule 应原样带回，得到 %q", c.rule, nextRule)
			}
		})
	}
}

// TestNthWeekdayOfMonth 单独钉住「第 N 个星期几」的边界：凑不满返回 false。
func TestNthWeekdayOfMonth(t *testing.T) {
	loc := time.Local
	// 2026-10-01 是周四，10 月有 4 个周一（5/12/19/26），没有第 5 个。
	if d, ok := nthWeekdayOfMonth(2026, time.October, 4, time.Monday, loc); !ok || d.Format("2006-01-02") != "2026-10-26" {
		t.Errorf("2026 年 10 月第 4 个周一 = %s ok=%v，期望 2026-10-26", d.Format("2006-01-02"), ok)
	}
	if d, ok := nthWeekdayOfMonth(2026, time.October, 5, time.Monday, loc); ok {
		t.Errorf("2026 年 10 月第 5 个周一不应存在，却得到 %s", d.Format("2006-01-02"))
	}
	got, ok := nthWeekdayOfMonth(2026, time.October, 1, time.Sunday, loc)
	if !ok || got.Format("2006-01-02") != "2026-10-04" {
		t.Errorf("2026 年 10 月第一个周日 = %s ok=%v，期望 2026-10-04", got.Format("2006-01-02"), ok)
	}
}

// TestLastWeekdayOfMonth 覆盖「最后一个工作日」遇到周末回退。
func TestLastWeekdayOfMonth(t *testing.T) {
	// 2026-10-31 是周六 → 退到 10-30 周五。
	got := lastWeekdayOfMonth(2026, time.October, time.Local)
	if got.Format("2006-01-02") != "2026-10-30" {
		t.Errorf("2026 年 10 月最后工作日 = %s，期望 2026-10-30", got.Format("2006-01-02"))
	}
	// 2026-11-30 是周一，本身就是工作日。
	got = lastWeekdayOfMonth(2026, time.November, time.Local)
	if got.Format("2006-01-02") != "2026-11-30" {
		t.Errorf("2026 年 11 月最后工作日 = %s，期望 2026-11-30", got.Format("2006-01-02"))
	}
}

// TestDaysInMonth 覆盖 2 月与闰年。
func TestDaysInMonth(t *testing.T) {
	cases := []struct {
		year int
		mon  time.Month
		want int
	}{
		{2026, time.January, 31},
		{2026, time.February, 28},
		{2028, time.February, 29}, // 2028 是闰年
		{2100, time.February, 28}, // 世纪年非 400 倍数不闰
		{2000, time.February, 29}, // 400 倍数闰
		{2026, time.December, 31},
	}
	for _, c := range cases {
		if got := daysInMonth(c.year, c.mon); got != c.want {
			t.Errorf("daysInMonth(%d, %d) = %d，期望 %d", c.year, c.mon, got, c.want)
		}
	}
}

// TestEblinghausOffsets 返回副本：调用方改动不能污染内部表。
func TestEblinghausOffsets(t *testing.T) {
	got := EbbinghausOffsets()
	if len(got) != 7 || got[0] != 1 || got[6] != 60 {
		t.Fatalf("EbbinghausOffsets = %v，期望 [1 2 4 7 15 30 60]", got)
	}
	got[0] = -1
	again := EbbinghausOffsets()
	if again[0] != 1 {
		t.Errorf("调用方改写了内部表，第二次读到 %v", again)
	}
}
