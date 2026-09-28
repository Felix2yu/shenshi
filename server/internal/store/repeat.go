package store

import (
	"strconv"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/lunar"
)

// 重复规则采用紧凑的字符串语法（由前端生成、后端解析），形如：
//
//	daily | weekdays | weekly | monthly | yearly
//	every:3:day | every:2:week | every:6:month
//	weekly:1,3,5          // 0=周日 ... 6=周六
//	monthly:15            // 每月 15 日
//	monthly:last          // 每月最后一天
//	monthly:lastworkday   // 每月最后一个工作日
//	monthly:nth:3:0       // 每月第 3 个周日（0=周日）
//	yearly:3-15           // 每年 3 月 15 日
//	ebbinghaus:2          // 艾宾浩斯复习曲线，2 为下一次间隔下标
//
// 与农历 / 法定节假日相关的规则（农历数据、放假与调休均来自 internal/lunar）：
//
//	lunar:monthly         // 农历每月同一天（如农历每月初五）
//	lunar:yearly          // 农历每年同一天（如农历生日、中秋）
//	weekdays              // 每周工作日（周一至周五，不看放假安排）
//	legalworkday          // 法定工作日（跳过法定节假日，含周末调休补班）
//	weekends              // 每周末（周六、周日）
//	legalholiday          // 法定节假日（放假期间每天）
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

	// 以下是与农历 / 法定节假日挂钩的规则，判定一律走 internal/lunar，
	// 与日历展示用的是同一份数据，不会出现「日历写着放假、任务却排到那天」。
	case "lunar":
		switch arg {
		case "monthly":
			next, ok := lunar.NextLunarMonthDay(d)
			if !ok {
				return time.Time{}, "", false
			}
			return next, rule, true
		case "yearly":
			next, ok := lunar.NextLunarYearDay(d)
			if !ok {
				return time.Time{}, "", false
			}
			return next, rule, true
		}
		return time.Time{}, "", false

	case "weekends":
		next, ok := lunar.NextWeekendDay(d)
		if !ok {
			return time.Time{}, "", false
		}
		return next, rule, true

	case "legalworkday":
		next, ok := lunar.NextLegalWorkday(d)
		if !ok {
			return time.Time{}, "", false
		}
		return next, rule, true

	case "legalholiday":
		next, ok := lunar.NextStatutoryHoliday(d)
		if !ok {
			return time.Time{}, "", false
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
		// 每月最后一个工作日：末段先退到最近的工作日，落在周末就继续往回找。
		if arg == "lastworkday" {
			return lastWeekdayOfMonth(base.Year(), base.Month(), base.Location()), rule, true
		}
		// 每月第 N 个星期几：monthly:nth:3:0 —— 第三个周日（0=周日）。
		if seg := strings.Split(arg, ":"); len(seg) == 3 && seg[0] == "nth" {
			n, err1 := strconv.Atoi(seg[1])
			wd, err2 := strconv.Atoi(seg[2])
			if err1 != nil || err2 != nil || n < 1 || n > 5 || wd < 0 || wd > 6 {
				return time.Time{}, "", false
			}
			hit, ok := nthWeekdayOfMonth(base.Year(), base.Month(), n, time.Weekday(wd), base.Location())
			if !ok {
				return time.Time{}, "", false
			}
			return hit, rule, true
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

// lastWeekdayOfMonth 返回某月最后一个「法定工作日」。
//
// 口径与 internal/lunar 一致（同 legalworkday 规则）：调休补班的周末算上班，
// 法定假日与连休区间不算 —— 只按周六日回退会把「月末恰逢国庆/春节假」的
// 周五当成工作日排任务。整月无工作日（农历数据异常）时退回周末口径兜底。
func lastWeekdayOfMonth(year int, month time.Month, loc *time.Location) time.Time {
	last := daysInMonth(year, month)
	first := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	d := time.Date(year, month, last, 0, 0, 0, 0, loc)
	for !d.Before(first) {
		if lunar.IsLegalWorkday(d) {
			return d
		}
		d = d.AddDate(0, 0, -1)
	}
	// 理论上不可能（没有整月都是假日的日历），数据异常时按周末口径退回月末。
	d = time.Date(year, month, last, 0, 0, 0, 0, loc)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// ValidRepeatRule 按语法白名单校验重复规则写得对不对，不试算具体日期。
//
// 与 NextOccurrence 的 ok=false 分工明确：那里表示「这一刻算不出下一次」
// （可能是规则本身非法，也可能是规则已耗尽——如 ebbinghaus 走完全部间隔、
// weekly 挑的日子 14 天内没出现），而这里只看语法。若拿试算当校验，
// ebbinghaus:7 这类「已经推进到末尾的合法存量值」会被误拒。
func ValidRepeatRule(rule string) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return false
	}
	parts := strings.Split(rule, ":")
	arg := ""
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], ":")
	}
	intIn := func(s string, lo, hi int) bool {
		n, err := strconv.Atoi(s)
		return err == nil && n >= lo && n <= hi
	}
	switch parts[0] {
	case "daily", "weekdays", "weekends", "legalworkday", "legalholiday":
		return arg == ""

	case "weekly":
		if arg == "" {
			return true
		}
		for _, s := range strings.Split(arg, ",") {
			if !intIn(strings.TrimSpace(s), 0, 6) {
				return false
			}
		}
		return true

	case "monthly":
		if arg == "" || arg == "last" || arg == "lastworkday" {
			return true
		}
		if seg := strings.Split(arg, ":"); len(seg) == 3 && seg[0] == "nth" {
			return intIn(seg[1], 1, 5) && intIn(seg[2], 0, 6)
		}
		return intIn(arg, 1, 31)

	case "yearly":
		if arg == "" {
			return true
		}
		md := strings.Split(arg, "-")
		return len(md) == 2 && intIn(md[0], 1, 12) && intIn(md[1], 1, 31)

	case "every":
		seg := strings.Split(arg, ":")
		if len(seg) != 2 {
			return false
		}
		n, err := strconv.Atoi(seg[0])
		if err != nil || n < 1 {
			return false
		}
		switch seg[1] {
		case "day", "week", "month", "year":
			return true
		}
		return false

	case "ebbinghaus":
		// 下标 0 ~ len 都可能已存在库里（nextRule 会把它推进到 len）。
		if arg == "" {
			return true
		}
		return intIn(arg, 0, len(ebbinghausOffsets))

	case "lunar":
		return arg == "monthly" || arg == "yearly"
	}
	return false
}

// checkRepeatRule 校验「可为空的重复规则」：空 = 不设规则，直接放行。
func checkRepeatRule(p *string) error {
	if p == nil || strings.TrimSpace(*p) == "" {
		return nil
	}
	if !ValidRepeatRule(*p) {
		return ValidationError{Msg: "重复规则不合法: " + *p}
	}
	return nil
}

// nthWeekdayOfMonth 返回某月第 n 个指定的星期几。该月凑不满 n 个时返回 ok=false
// （例如「第五个周一」，多数月份都没有）。
func nthWeekdayOfMonth(year int, month time.Month, n int, wd time.Weekday, loc *time.Location) (time.Time, bool) {
	first := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	delta := (int(wd) - int(first.Weekday()) + 7) % 7
	d := first.AddDate(0, 0, delta+(n-1)*7)
	if d.Month() != month {
		return time.Time{}, false
	}
	return d, true
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
