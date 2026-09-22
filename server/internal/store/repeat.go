package store

import (
	"strconv"
	"strings"
	"time"
)

// 重复规则采用紧凑的字符串语法（由前端生成、后端解析），形如：
//
//	daily | weekdays | weekly | monthly | yearly
//	every:3:day | every:2:week | every:6:month
//	weekly:1,3,5          // 0=周日 ... 6=周六
//	monthly:15            // 每月 15 日
//	monthly:last          // 每月最后一天
//	yearly:3-15           // 每年 3 月 15 日
//	ebbinghaus:2          // 艾宾浩斯复习曲线，2 为下一次间隔下标
//
// 之所以自建语法而非引入 RRULE 库，是因为这里只需要「下一次发生时间」这一件事，
// 保持零依赖、易调试，也便于前端直接生成选项。

// ebbinghausOffsets 是艾宾浩斯遗忘曲线的复习间隔（天）。
var ebbinghausOffsets = []int{1, 2, 4, 7, 15, 30, 60}

// NextOccurrence 依据规则计算下一次发生日期。ok=false 表示规则耗尽或不合法。
// nextRule 是下一次任务应当携带的规则（艾宾浩斯会推进下标，其余保持不变）。
func NextOccurrence(rule string, from time.Time) (next time.Time, nextRule string, ok bool) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return time.Time{}, "", false
	}
	d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	parts := strings.Split(rule, ":")
	arg := ""
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], ":")
	}

	switch parts[0] {
	case "daily":
		return d.AddDate(0, 0, 1), rule, true

	case "weekdays":
		next := d.AddDate(0, 0, 1)
		for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.AddDate(0, 0, 1)
		}
		return next, rule, true

	case "weekly":
		if arg == "" {
			return d.AddDate(0, 0, 7), rule, true
		}
		want := map[time.Weekday]bool{}
		for _, s := range strings.Split(arg, ",") {
			v, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil || v < 0 || v > 6 {
				return time.Time{}, "", false
			}
			want[time.Weekday(v)] = true
		}
		if len(want) == 0 {
			return time.Time{}, "", false
		}
		next := d.AddDate(0, 0, 1)
		for i := 0; i < 14; i++ {
			if want[next.Weekday()] {
				return next, rule, true
			}
			next = next.AddDate(0, 0, 1)
		}
		return time.Time{}, "", false

	case "monthly":
		base := addMonthClamped(d, 1)
		if arg == "" {
			return base, rule, true
		}
		if arg == "last" {
			return time.Date(base.Year(), base.Month(), daysInMonth(base.Year(), base.Month()), 0, 0, 0, 0, base.Location()), rule, true
		}
		day, err := strconv.Atoi(arg)
		if err != nil || day < 1 || day > 31 {
			return time.Time{}, "", false
		}
		if day > daysInMonth(base.Year(), base.Month()) {
			// 该月无此日（如 2 月 31 日），顺延到该月最后一天。
			day = daysInMonth(base.Year(), base.Month())
		}
		return time.Date(base.Year(), base.Month(), day, 0, 0, 0, 0, base.Location()), rule, true

	case "yearly":
		if arg == "" {
			return addYearClamped(d, 1), rule, true
		}
		md := strings.Split(arg, "-")
		if len(md) != 2 {
			return time.Time{}, "", false
		}
		m, err1 := strconv.Atoi(md[0])
		day, err2 := strconv.Atoi(md[1])
		if err1 != nil || err2 != nil || m < 1 || m > 12 || day < 1 || day > 31 {
			return time.Time{}, "", false
		}
		year := d.Year() + 1
		day = min(day, daysInMonth(year, time.Month(m)))
		return time.Date(year, time.Month(m), day, 0, 0, 0, 0, d.Location()), rule, true

	case "every":
		seg := strings.Split(arg, ":")
		if len(seg) != 2 {
			return time.Time{}, "", false
		}
		n, err := strconv.Atoi(seg[0])
		if err != nil || n < 1 {
			return time.Time{}, "", false
		}
		switch seg[1] {
		case "day":
			return d.AddDate(0, 0, n), rule, true
		case "week":
			return d.AddDate(0, 0, 7*n), rule, true
		case "month":
			return addMonthClamped(d, n), rule, true
		case "year":
			return addYearClamped(d, n), rule, true
		}
		return time.Time{}, "", false

	case "ebbinghaus":
		idx := 0
		if arg != "" {
			if v, err := strconv.Atoi(arg); err == nil {
				idx = v
			}
		}
		if idx >= len(ebbinghausOffsets) {
			return time.Time{}, "", false
		}
		return d.AddDate(0, 0, ebbinghausOffsets[idx]), "ebbinghaus:" + strconv.Itoa(idx+1), true
	}
	return time.Time{}, "", false
}

func addMonthClamped(d time.Time, n int) time.Time {
	y, m, day := d.Date()
	target := time.Date(y, m, 1, 0, 0, 0, 0, d.Location()).AddDate(0, n, 0)
	day = min(day, daysInMonth(target.Year(), target.Month()))
	return time.Date(target.Year(), target.Month(), day, 0, 0, 0, 0, d.Location())
}

func addYearClamped(d time.Time, n int) time.Time {
	y, m, day := d.Date()
	targetYear := y + n
	day = min(day, daysInMonth(targetYear, m))
	return time.Date(targetYear, m, day, 0, 0, 0, 0, d.Location())
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// EbbinghausOffsets 暴露复习间隔，供前端展示。
func EbbinghausOffsets() []int { return append([]int(nil), ebbinghausOffsets...) }
