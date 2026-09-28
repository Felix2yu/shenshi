package lunar

import "time"

// Kind 是一天在「放假 / 上班」维度上的性质。
type Kind string

const (
	KindNormal  Kind = "normal"  // 普通工作日
	KindWeekend Kind = "weekend" // 周末休息（非法定节假日）
	KindHoliday Kind = "holiday" // 法定节假日当天
	KindRest    Kind = "rest"    // 连休里靠调休凑出来的休息日
	KindWorkday Kind = "workday" // 周末调休上班
)

// LabelKind 说明日历格里的那行小字来自哪一类，前端据此决定配色。
type LabelKind string

const (
	LabelFestival LabelKind = "festival" // 农历传统节日：春节、中秋、除夕…
	LabelHoliday  LabelKind = "holiday"  // 法定节假日：元旦、清明、劳动节、国庆
	LabelTerm     LabelKind = "term"     // 二十四节气
	LabelLunar    LabelKind = "lunar"    // 普通农历日
)

// DayInfo 是一天里与日历展示、重复规则有关的全部信息。
// 前端不再自己算农历，全部按这份结果渲染，避免两端算法各自漂移。
type DayInfo struct {
	Date string `json:"date"`

	LunarMonth   int    `json:"lunarMonth"`   // 1~12
	LunarDay     int    `json:"lunarDay"`     // 1~30
	IsLeapMonth  bool   `json:"isLeapMonth"`  // 是否闰月
	LunarText    string `json:"lunarText"`    // 初一显示「八月」，其余显示「十五」
	LunarFull    string `json:"lunarFull"`    // 「农历八月初五」
	GanZhi       string `json:"ganZhi"`       // 「丙午」
	Zodiac       string `json:"zodiac"`       // 「马」
	Festival     string `json:"festival"`     // 农历传统节日
	SolarTerm    string `json:"solarTerm"`    // 二十四节气
	Holiday      string `json:"holiday"`      // 法定节假日名称（放 13 天口径）
	HolidaySpan  string `json:"holidaySpan"`  // 所在官方连休区间的节名（10-05 仍属「国庆节」）
	Label        string `json:"label"`        // 日历格里要显示的那一行的文本
	LabelKind    string `json:"labelKind"`    // festival / holiday / term / lunar
	Kind         string `json:"kind"`         // normal / weekend / holiday / workday
	IsRest       bool   `json:"isRest"`       // 法定休息日
	IsWorkday    bool   `json:"isWorkday"`    // 法定工作日
	IsHoliday    bool   `json:"isHoliday"`    // 法定节假日（放假）
	MakeUpWork   bool   `json:"makeUpWork"`   // 周末调休上班
	Estimated    bool   `json:"estimated"`    // 该年调休安排尚未公布，休息日为推算值
	NotSupported bool   `json:"notSupported"` // 超出农历数据范围
}

// DayInfoOf 组装某一天的完整信息。
func DayInfoOf(t time.Time) DayInfo {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	out := DayInfo{Date: dayKey(d), Estimated: IsEstimated(d.Year())}

	ld, ok := FromSolar(d)
	if !ok {
		// 超出 1900~2100：农历无从算起，但公历的周末还能给。
		out.NotSupported = true
		out.Kind = string(KindNormal)
		if IsWeekend(d) {
			out.Kind, out.IsRest = string(KindWeekend), true
		} else {
			out.IsWorkday = true
		}
		return out
	}

	out.LunarMonth = ld.Month
	out.LunarDay = ld.Day
	out.IsLeapMonth = ld.IsLeap
	out.LunarFull = ld.FullName()
	out.GanZhi = ld.GanZhi()
	out.Zodiac = ld.Zodiac()
	// 初一显示月名（闰月带「闰」），其余显示日名 —— 与苹果日历一致。
	if ld.Day == 1 {
		out.LunarText = ld.MonthFullName()
	} else {
		out.LunarText = ld.DayName()
	}

	out.Festival = Festival(d)
	out.SolarTerm = SolarTerm(d)
	// 名字只标在法定节假日当天：连休 7 天里 10 月 4 至 7 日是调休凑的，
	// 那天该显示的是农历，靠「休」标记说明它放假即可。
	out.Holiday = StatutoryName(d)
	out.HolidaySpan = HolidaySpanName(d)
	out.MakeUpWork = IsMakeUpWorkday(d)
	out.IsHoliday = out.Holiday != ""
	out.IsRest = IsRestDay(d)
	out.IsWorkday = !out.IsRest

	switch {
	case out.IsHoliday:
		out.Kind = string(KindHoliday)
	case out.MakeUpWork:
		out.Kind = string(KindWorkday)
	case IsWeekend(d):
		out.Kind = string(KindWeekend)
	case out.HolidaySpan != "":
		// 连休里靠调休凑出来的工作日放假（如国庆的 10-05）。
		out.Kind = string(KindRest)
	default:
		out.Kind = string(KindNormal)
	}

	// 日历格里只放得下一行：农历节日 > 法定节假日 > 节气 > 农历日。
	// 元旦、国庆这类只有法定口径的节日才会走到 Holiday 这一档。
	switch {
	case out.Festival != "":
		out.Label, out.LabelKind = out.Festival, string(LabelFestival)
	case out.Holiday != "":
		out.Label, out.LabelKind = out.Holiday, string(LabelHoliday)
	case out.SolarTerm != "":
		out.Label, out.LabelKind = out.SolarTerm, string(LabelTerm)
	default:
		out.Label, out.LabelKind = out.LunarText, string(LabelLunar)
	}
	return out
}

// DayInfos 批量组装 [from, to] 闭区间内每一天的信息。
// 区间过长时按 366 天截断，避免接口被拿来当负载测试用。
func DayInfos(from, to time.Time) []DayInfo {
	if to.Before(from) {
		from, to = to, from
	}
	if daysBetween(from, to) > 365 {
		to = from.AddDate(0, 0, 365)
	}
	out := make([]DayInfo, 0, daysBetween(from, to)+1)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, DayInfoOf(d))
	}
	return out
}

// ---------- 重复规则用的查询 ----------

// NextLegalWorkday 返回 from 之后的第一个法定工作日。
// 最多往后找 400 天，找不到返回 ok=false。
func NextLegalWorkday(from time.Time) (time.Time, bool) {
	return nextDay(from, 400, IsLegalWorkday)
}

// NextStatutoryHoliday 返回 from 之后的第一个法定节假日。
func NextStatutoryHoliday(from time.Time) (time.Time, bool) {
	return nextDay(from, 400, IsStatutoryHoliday)
}

// NextWeekendDay 返回 from 之后的第一个周末（周六或周日）。
func NextWeekendDay(from time.Time) (time.Time, bool) {
	return nextDay(from, 7, IsWeekend)
}

// NextWeekday 返回 from 之后的第一个周一至周五。
func NextWeekday(from time.Time) (time.Time, bool) {
	return nextDay(from, 7, func(t time.Time) bool { return !IsWeekend(t) })
}

func nextDay(from time.Time, limit int, match func(time.Time) bool) (time.Time, bool) {
	d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	for i := 0; i < limit; i++ {
		d = d.AddDate(0, 0, 1)
		if match(d) {
			return d, true
		}
	}
	return time.Time{}, false
}

// NextLunarMonthDay 返回农历「每月第 day 天」在 from 之后的下一次。
// 目标月不足 day 天（如「三十」遇上小月）时取该月最后一天。
func NextLunarMonthDay(from time.Time) (time.Time, bool) {
	cur, ok := FromSolar(from)
	if !ok {
		return time.Time{}, false
	}
	next := nextLunarMonth(cur)
	if next.Year > MaxYear {
		return time.Time{}, false
	}
	return solarIn(from, Date{Year: next.Year, Month: next.Month, Day: cur.Day, IsLeap: next.IsLeap})
}

// NextLunarYearDay 返回农历「每年这一天」在 from 之后的下一次。
// 闰月生日在没闰月的年份会退回平月，日期超出月长则取月末。
func NextLunarYearDay(from time.Time) (time.Time, bool) {
	cur, ok := FromSolar(from)
	if !ok {
		return time.Time{}, false
	}
	if cur.Year+1 > MaxYear {
		return time.Time{}, false
	}
	return solarIn(from, Date{Year: cur.Year + 1, Month: cur.Month, Day: cur.Day, IsLeap: cur.IsLeap})
}

// solarIn 是 ToSolar 的「按 from 所在时区落地」版本。
//
// ToSolar 以 UTC 午夜为原点（base 是 UTC），直接返回会让这条日线与
// nextDay 系（from.Location() 午夜）不一致：调用方（NextRepeat 的
// `next.Before(today)`）拿本地午夜比 UTC 午夜，在 UTC 负时区（如美洲部署）
// 会把「同一天」判成落在过去，lunar 重复规则于是每次多跳一天。
// 只取日期分量（Format）的地方不受影响，故只在这里、把结果交给外部比较的
// 出口统一归一。
func solarIn(from time.Time, d Date) (time.Time, bool) {
	t, ok := ToSolar(d)
	if !ok {
		return time.Time{}, false
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, from.Location()), true
}

// nextLunarMonth 给出农历意义上的「下一个月」：闰月紧随同月的平月之后，腊月之后进下一年。
func nextLunarMonth(cur Date) Date {
	leap := LeapMonth(cur.Year)
	switch {
	case cur.IsLeap:
		// 闰四月之后是五月
		return Date{Year: cur.Year, Month: cur.Month + 1}
	case cur.Month == leap:
		// 四月之后是闰四月
		return Date{Year: cur.Year, Month: cur.Month, IsLeap: true}
	case cur.Month == 12:
		return Date{Year: cur.Year + 1, Month: 1}
	default:
		return Date{Year: cur.Year, Month: cur.Month + 1}
	}
}
