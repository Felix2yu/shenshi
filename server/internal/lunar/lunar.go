// Package lunar 提供农历、二十四节气与中国法定节假日的计算。
//
// 之所以单独成包，是因为「日历上显示什么」与「重复规则排到哪天」必须读同一份数据：
// 否则迟早出现「日历写着放假、重复任务却排到那天上班」这种自相矛盾的结果。
// 后端是这份数据的唯一来源，前端通过 /api/meta/calendar 取用，不再自己算一遍。
package lunar

import (
	"math"
	"sort"
	"time"
)

// 农历数据的有效区间。
const (
	MinYear = 1900
	MaxYear = 2100
)

// lunarInfo 每年一个整数，覆盖 1900 ~ 2100：
//   - 低 4 位：闰月月份，0 表示当年无闰月
//   - 第 4 ~ 15 位：正月到腊月的大小月（1 为 30 天，0 为 29 天），第 4 位是正月
//   - 第 16 位：闰月的大小（仅当年有闰月时有意义）
var lunarInfo = [MaxYear - MinYear + 1]uint32{
	0x04bd8, 0x04ae0, 0x0a570, 0x054d5, 0x0d260, 0x0d950, 0x16554, 0x056a0, 0x09ad0, 0x055d2, // 1900-1909
	0x04ae0, 0x0a5b6, 0x0a4d0, 0x0d250, 0x1d255, 0x0b540, 0x0d6a0, 0x0ada2, 0x095b0, 0x14977, // 1910-1919
	0x04970, 0x0a4b0, 0x0b4b5, 0x06a50, 0x06d40, 0x1ab54, 0x02b60, 0x09570, 0x052f2, 0x04970, // 1920-1929
	0x06566, 0x0d4a0, 0x0ea50, 0x06e95, 0x05ad0, 0x02b60, 0x186e3, 0x092e0, 0x1c8d7, 0x0c950, // 1930-1939
	0x0d4a0, 0x1d8a6, 0x0b550, 0x056a0, 0x1a5b4, 0x025d0, 0x092d0, 0x0d2b2, 0x0a950, 0x0b557, // 1940-1949
	0x06ca0, 0x0b550, 0x15355, 0x04da0, 0x0a5b0, 0x14573, 0x052b0, 0x0a9a8, 0x0e950, 0x06aa0, // 1950-1959
	0x0aea6, 0x0ab50, 0x04b60, 0x0aae4, 0x0a570, 0x05260, 0x0f263, 0x0d950, 0x05b57, 0x056a0, // 1960-1969
	0x096d0, 0x04dd5, 0x04ad0, 0x0a4d0, 0x0d4d4, 0x0d250, 0x0d558, 0x0b540, 0x0b6a0, 0x195a6, // 1970-1979
	0x095b0, 0x049b0, 0x0a974, 0x0a4b0, 0x0b27a, 0x06a50, 0x06d40, 0x0af46, 0x0ab60, 0x09570, // 1980-1989
	0x04af5, 0x04970, 0x064b0, 0x074a3, 0x0ea50, 0x06b58, 0x055c0, 0x0ab60, 0x096d5, 0x092e0, // 1990-1999
	0x0c960, 0x0d954, 0x0d4a0, 0x0da50, 0x07552, 0x056a0, 0x0abb7, 0x025d0, 0x092d0, 0x0cab5, // 2000-2009
	0x0a950, 0x0b4a0, 0x0baa4, 0x0ad50, 0x055d9, 0x04ba0, 0x0a5b0, 0x15176, 0x052b0, 0x0a930, // 2010-2019
	0x07954, 0x06aa0, 0x0ad50, 0x05b52, 0x04b60, 0x0a6e6, 0x0a4e0, 0x0d260, 0x0ea65, 0x0d530, // 2020-2029
	0x05aa0, 0x076a3, 0x096d0, 0x04afb, 0x04ad0, 0x0a4d0, 0x1d0b6, 0x0d250, 0x0d520, 0x0dd45, // 2030-2039
	0x0b5a0, 0x056d0, 0x055b2, 0x049b0, 0x0a577, 0x0a4b0, 0x0aa50, 0x1b255, 0x06d20, 0x0ada0, // 2040-2049
	0x14b63, 0x09370, 0x049f8, 0x04970, 0x064b0, 0x168a6, 0x0ea50, 0x06b20, 0x1a6c4, 0x0aae0, // 2050-2059
	0x0a2e0, 0x0d2e3, 0x0c960, 0x0d557, 0x0d4a0, 0x0da50, 0x05d55, 0x056a0, 0x0a6d0, 0x055d4, // 2060-2069
	0x052d0, 0x0a9b8, 0x0a950, 0x0b4a0, 0x0b6a6, 0x0ad50, 0x055a0, 0x0aba4, 0x0a5b0, 0x052b0, // 2070-2079
	0x0b273, 0x06930, 0x07337, 0x06aa0, 0x0ad50, 0x14b55, 0x04b60, 0x0a570, 0x054e4, 0x0d160, // 2080-2089
	0x0e968, 0x0d520, 0x0daa0, 0x16aa6, 0x056d0, 0x04ae0, 0x0a9d4, 0x0a2d0, 0x0d150, 0x0f252, // 2090-2099
	0x0d520, // 2100
}

// base 是农历 1900 年正月初一对应的公历日期，一切换算都以它为原点。
var base = time.Date(1900, 1, 31, 0, 0, 0, 0, time.UTC)

// Date 是一个农历日期。Month 取值 1~12，IsLeap 表示是否闰月。
type Date struct {
	Year   int
	Month  int
	Day    int
	IsLeap bool
}

// info 取某年的农历信息字；区间外返回 0（等价于「无闰月、全年小月」），
// 调用方用 Supported 提前挡掉更稳妥。
func info(year int) uint32 {
	if year < MinYear || year > MaxYear {
		return 0
	}
	return lunarInfo[year-MinYear]
}

// Supported 报告年份是否落在农历数据的覆盖范围内。
func Supported(year int) bool { return year >= MinYear && year <= MaxYear }

// LeapMonth 返回该年的闰月月份，0 表示无闰月。
func LeapMonth(year int) int { return int(info(year) & 0xf) }

// LeapDays 返回闰月的天数；无闰月时为 0。
func LeapDays(year int) int {
	if LeapMonth(year) == 0 {
		return 0
	}
	if info(year)&0x10000 != 0 {
		return 30
	}
	return 29
}

// 年表在包初始化时一次性展开：换算会被重复规则按天调用（找「下一个法定工作日」
// 一天天试），每次都从 1900 年累加一遍太浪费。
var (
	monthsPre    [MaxYear - MinYear + 1][]month
	yearDaysPre  [MaxYear - MinYear + 1]int
	yearStartPre [MaxYear - MinYear + 2]int // 末位是 2100 年结束时的累计偏移，用作二分哨兵
)

func init() {
	acc := 0
	for i := 0; i <= MaxYear-MinYear; i++ {
		year := MinYear + i
		ms := buildMonths(year)
		monthsPre[i] = ms
		yearStartPre[i] = acc
		for _, m := range ms {
			acc += m.days
		}
		yearDaysPre[i] = acc - yearStartPre[i]
	}
	yearStartPre[MaxYear-MinYear+1] = acc
}

// buildMonths 展开某一年的农历月。
func buildMonths(year int) []month {
	leap := LeapMonth(year)
	out := make([]month, 0, 13)
	for m := 1; m <= 12; m++ {
		out = append(out, month{num: m, isLeap: false, days: MonthDays(year, m)})
		if m == leap {
			out = append(out, month{num: m, isLeap: true, days: LeapDays(year)})
		}
	}
	return out
}

// MonthDays 返回农历某年某平月的天数（29 或 30）。
func MonthDays(year, month int) int {
	if month < 1 || month > 12 {
		return 0
	}
	if info(year)&(0x10000>>uint(month)) != 0 {
		return 30
	}
	return 29
}

// month 是展开后的一个农历月，闰月紧跟在同月的平月之后。
type month struct {
	num    int
	isLeap bool
	days   int
}

// monthsOf 返回某一年的农历月序列（闰月插在同月之后）。返回的切片是包级缓存，请勿修改。
func monthsOf(year int) []month {
	if !Supported(year) {
		return nil
	}
	return monthsPre[year-MinYear]
}

// YearDays 返回农历某年的总天数。
func YearDays(year int) int {
	if !Supported(year) {
		return 0
	}
	return yearDaysPre[year-MinYear]
}

// daysBetween 按「日历日」计的天数差，两端都先归一到当日零点，避免时区与时分干扰。
func daysBetween(from, to time.Time) int {
	a := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(b.Sub(a).Hours() / 24)
}

// FromSolar 把公历日期换算成农历。超出数据范围时 ok=false。
func FromSolar(t time.Time) (Date, bool) {
	offset := daysBetween(base, t)
	if offset < 0 || offset >= yearStartPre[MaxYear-MinYear+1] {
		return Date{}, false
	}
	// yearStartPre 单调递增，二分定位到所在的农历年。
	i := sort.Search(MaxYear-MinYear+1, func(i int) bool { return yearStartPre[i+1] > offset })
	year := MinYear + i
	rest := offset - yearStartPre[i]
	for _, m := range monthsOf(year) {
		if rest < m.days {
			return Date{Year: year, Month: m.num, Day: rest + 1, IsLeap: m.isLeap}, true
		}
		rest -= m.days
	}
	return Date{}, false
}

// ToSolar 把农历日期换算成公历。目标月没有闰月时自动退回平月；
// 目标月没有这一天（如「三十」落在小月）时取该月最后一天。
func ToSolar(d Date) (time.Time, bool) {
	if !Supported(d.Year) || d.Month < 1 || d.Month > 12 {
		return time.Time{}, false
	}
	if d.IsLeap && LeapMonth(d.Year) != d.Month {
		d.IsLeap = false
	}
	offset := yearStartPre[d.Year-MinYear]
	for _, m := range monthsOf(d.Year) {
		if m.num == d.Month && m.isLeap == d.IsLeap {
			day := d.Day
			if day < 1 {
				day = 1
			}
			if day > m.days {
				day = m.days
			}
			return base.AddDate(0, 0, offset+day-1), true
		}
		offset += m.days
	}
	return time.Time{}, false
}

// ---------- 名称 ----------

var monthNames = [...]string{"正", "二", "三", "四", "五", "六", "七", "八", "九", "十", "冬", "腊"}

var dayNames = [...]string{
	"初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
	"十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十",
	"廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十",
}

var ganNames = [...]string{"甲", "乙", "丙", "丁", "戊", "己", "庚", "辛", "壬", "癸"}
var zhiNames = [...]string{"子", "丑", "寅", "卯", "辰", "巳", "午", "未", "申", "酉", "戌", "亥"}
var zodiacNames = [...]string{"鼠", "牛", "虎", "兔", "龙", "蛇", "马", "羊", "猴", "鸡", "狗", "猪"}

// MonthName 返回农历月名，如「八」「闰八」。
func (d Date) MonthName() string {
	if d.Month < 1 || d.Month > 12 {
		return ""
	}
	name := monthNames[d.Month-1]
	if d.IsLeap {
		return "闰" + name
	}
	return name
}

// MonthFullName 返回「八月」/「闰八月」这类完整月名。
func (d Date) MonthFullName() string {
	if d.Month < 1 || d.Month > 12 {
		return ""
	}
	name := monthNames[d.Month-1] + "月"
	if d.IsLeap {
		return "闰" + name
	}
	return name
}

// DayName 返回农历日名，如「十五」。
func (d Date) DayName() string {
	if d.Day < 1 || d.Day > 30 {
		return ""
	}
	return dayNames[d.Day-1]
}

// FullName 返回「农历八月初五」/「农历闰八月初五」。
func (d Date) FullName() string {
	return "农历" + d.MonthFullName() + d.DayName()
}

// GanZhi 返回农历年的干支，如「丙午」。
func (d Date) GanZhi() string {
	if !Supported(d.Year) {
		return ""
	}
	i := d.Year - 4
	return ganNames[i%10] + zhiNames[i%12]
}

// Zodiac 返回农历年的生肖，如「马」。
func (d Date) Zodiac() string {
	if !Supported(d.Year) {
		return ""
	}
	return zodiacNames[(d.Year-4)%12]
}

// ---------- 农历节日 ----------

// lunarFestivals 按农历月日索引的传统节日。节日只认平月，闰月不重复过节。
var lunarFestivals = map[[2]int]string{
	{1, 1}:   "春节",
	{1, 15}:  "元宵节",
	{2, 2}:   "龙抬头",
	{5, 5}:   "端午节",
	{7, 7}:   "七夕",
	{7, 15}:  "中元节",
	{8, 15}:  "中秋节",
	{9, 9}:   "重阳节",
	{12, 8}:  "腊八节",
	{12, 23}: "小年",
}

// Festival 返回这一天的农历传统节日；平闰月都算，但闰月不重复过节（取平月）。
func Festival(t time.Time) string {
	d, ok := FromSolar(t)
	if !ok || d.IsLeap {
		return ""
	}
	// 除夕是腊月的最后一天，小年除夕是廿九，得按月长算。
	if d.Month == 12 && d.Day == MonthDays(d.Year, 12) {
		return "除夕"
	}
	return lunarFestivals[[2]int{d.Month, d.Day}]
}

// ---------- 二十四节气 ----------

// solarTermNames 从「小寒」开始，与 sTermInfo 的下标一一对应。
var solarTermNames = [...]string{
	"小寒", "大寒", "立春", "雨水", "惊蛰", "春分", "清明", "谷雨",
	"立夏", "小满", "芒种", "夏至", "小暑", "大暑", "立秋", "处暑",
	"白露", "秋分", "寒露", "霜降", "立冬", "小雪", "大雪", "冬至",
}

// 节气由太阳视黄经定义：春分 0°，之后每 15° 一个。
// 下表给出各节气的目标黄经，与 solarTermNames 一一对应（小寒 285° … 冬至 270°）。
func termLongitude(idx int) float64 { return float64(((idx-5)*15%360 + 360) % 360) }

// termGuessDOY 是各节气在 2000 年前后的平均年内日序，仅用作迭代起点：
// 真正求解靠牛顿迭代，起点差一两天不影响收敛。
var termGuessDOY = [24]int{
	6, 20, 35, 50, 65, 80, 95, 111,
	126, 142, 157, 173, 189, 204, 220, 236,
	251, 266, 282, 297, 312, 327, 342, 356,
}

const (
	jd2000       = 2451544.5 // 2000-01-01 00:00 UTC 的儒略日
	jdUnixEpoch  = 2440587.5 // 1970-01-01 00:00 UTC
	tropicalYear = 365.2422
	// 太阳视黄经的日变化量，牛顿迭代的导数。
	longitudePerDay = 0.9856
	degPerRad       = 180 / math.Pi
	radPerDeg       = math.Pi / 180
)

// ChinaTZ 是国内日历使用的时区：节气、农历都以北京时间为准。
var ChinaTZ = time.FixedZone("CST", 8*3600)

func jdOf(t time.Time) float64      { return float64(t.UTC().Unix())/86400 + jdUnixEpoch }
func timeOfJD(jd float64) time.Time { return time.Unix(int64((jd-jdUnixEpoch)*86400), 0).UTC() }

// solarLongitude 返回某时刻的太阳视黄经（度），采用《天文年历》的低精度公式，
// 精度约 0.01°，折算到时间约 15 分钟 —— 判定节气落在哪一天绰绰有余。
func solarLongitude(jd float64) float64 {
	t := (jd - 2451545.0) / 36525
	l := 280.460 + 36000.772*t
	g := (357.528 + 35999.050*t) * radPerDeg
	lambda := l + 1.915*math.Sin(g) + 0.020*math.Sin(2*g)
	return math.Mod(lambda, 360)
}

// SolarTermTime 返回某年第 idx 个节气（0=小寒 … 23=冬至）的精确时刻（UTC）。
func SolarTermTime(year, idx int) (time.Time, bool) {
	if !Supported(year) || idx < 0 || idx >= len(solarTermNames) {
		return time.Time{}, false
	}
	jd := jd2000 + float64(termGuessDOY[idx]-1) + tropicalYear*float64(year-2000)
	want := termLongitude(idx)
	for i := 0; i < 12; i++ {
		// 取 (−180, 180] 内的角差，避免跨 0° 时迭代方向跑反。
		diff := math.Mod(solarLongitude(jd)-want+540, 360) - 180
		jd -= diff / longitudePerDay
	}
	return timeOfJD(jd), true
}

// SolarTermDate 返回某年第 idx 个节气所在的北京时间日期（零点）。
func SolarTermDate(year, idx int) (time.Time, bool) {
	t, ok := SolarTermTime(year, idx)
	if !ok {
		return time.Time{}, false
	}
	bj := t.In(ChinaTZ)
	return time.Date(bj.Year(), bj.Month(), bj.Day(), 0, 0, 0, 0, ChinaTZ), true
}

// SolarTerm 返回这一天的节气名，不是节气则返回空串。
func SolarTerm(t time.Time) string {
	y := t.Year()
	if !Supported(y) {
		return ""
	}
	for i := range solarTermNames {
		d, ok := SolarTermDate(y, i)
		if !ok {
			continue
		}
		if d.Year() == t.Year() && d.Month() == t.Month() && d.Day() == t.Day() {
			return solarTermNames[i]
		}
	}
	return ""
}
