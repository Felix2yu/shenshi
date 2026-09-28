package lunar

import (
	"fmt"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func fmtDay(t time.Time) string { return t.Format("2006-01-02") }

// TestSpringFestivalDates 用正月初一的公历日期校验整张农历表。
// 农历是从 1900 年起逐月累加出来的，任何一年的月长数据写错都会让后面全部错位，
// 因此跨度拉到 40 年：一旦某年出错，后面所有年份都会跟着偏。
func TestSpringFestivalDates(t *testing.T) {
	want := map[int]string{
		1990: "1990-01-27", 1995: "1995-01-31", 2000: "2000-02-05", 2005: "2005-02-09",
		2008: "2008-02-07", 2012: "2012-01-23", 2015: "2015-02-19", 2018: "2018-02-16",
		2020: "2020-01-25", 2021: "2021-02-12", 2022: "2022-02-01", 2023: "2023-01-22",
		2024: "2024-02-10", 2025: "2025-01-29", 2026: "2026-02-17", 2027: "2027-02-06",
		2028: "2028-01-26", 2029: "2029-02-13", 2030: "2030-02-03", 2035: "2035-02-08",
	}
	for year, expect := range want {
		got, ok := ToSolar(Date{Year: year, Month: 1, Day: 1})
		if !ok {
			t.Fatalf("%d 年正月初一换算失败", year)
		}
		if fmtDay(got) != expect {
			t.Errorf("%d 年正月初一 = %s，应为 %s", year, fmtDay(got), expect)
		}
		// 换算要能原路返回。
		back, ok := FromSolar(got)
		if !ok || back.Year != year || back.Month != 1 || back.Day != 1 || back.IsLeap {
			t.Errorf("%s 反查农历得到 %+v", expect, back)
		}
	}
}

func TestLunarNames(t *testing.T) {
	cases := []struct {
		date   string
		full   string
		text   string
		ganzhi string
		zodiac string
	}{
		{"2026-02-17", "农历正月初一", "正月", "丙午", "马"},
		{"2026-09-25", "农历八月十五", "十五", "丙午", "马"},
		{"2026-09-26", "农历八月十六", "十六", "丙午", "马"},
		{"2027-02-06", "农历正月初一", "正月", "丁未", "羊"},
	}
	for _, c := range cases {
		info := DayInfoOf(day(c.date))
		if info.LunarFull != c.full {
			t.Errorf("%s 农历 = %s，应为 %s", c.date, info.LunarFull, c.full)
		}
		if info.LunarText != c.text {
			t.Errorf("%s 农历短名 = %s，应为 %s", c.date, info.LunarText, c.text)
		}
		if info.GanZhi != c.ganzhi {
			t.Errorf("%s 干支 = %s，应为 %s", c.date, info.GanZhi, c.ganzhi)
		}
		if info.Zodiac != c.zodiac {
			t.Errorf("%s 生肖 = %s，应为 %s", c.date, info.Zodiac, c.zodiac)
		}
	}
}

// TestSolarTerms2027 对齐香港天文台公布的 2027 年节气日期。
// 节气由太阳视黄经定义，用天文公式算，不依赖任何可能抄错的经验表。
func TestSolarTerms2027(t *testing.T) {
	want := []string{
		"2027-01-05", "2027-01-20", "2027-02-04", "2027-02-19", // 小寒 大寒 立春 雨水
		"2027-03-06", "2027-03-21", "2027-04-05", "2027-04-20", // 惊蛰 春分 清明 谷雨
		"2027-05-06", "2027-05-21", "2027-06-06", "2027-06-21", // 立夏 小满 芒种 夏至
		"2027-07-07", "2027-07-23", "2027-08-08", "2027-08-23", // 小暑 大暑 立秋 处暑
		"2027-09-08", "2027-09-23", "2027-10-08", "2027-10-23", // 白露 秋分 寒露 霜降
		"2027-11-07", "2027-11-22", "2027-12-07", "2027-12-22", // 立冬 小雪 大雪 冬至
	}
	for i, expect := range want {
		got, ok := SolarTermDate(2027, i)
		if !ok {
			t.Fatalf("第 %d 个节气算不出来", i)
		}
		if fmtDay(got) != expect {
			t.Errorf("%s（第 %d 个节气）= %s，应为 %s", solarTermNames[i], i, fmtDay(got), expect)
		}
		if name := SolarTerm(day(expect)); name != solarTermNames[i] {
			t.Errorf("%s 的节气 = %q，应为 %s", expect, name, solarTermNames[i])
		}
	}
}

func TestFestivals(t *testing.T) {
	cases := map[string]string{
		"2026-02-16": "除夕",
		"2026-02-17": "春节",
		"2026-03-03": "元宵节",
		"2026-06-19": "端午节",
		"2026-09-25": "中秋节",
		"2025-08-25": "", // 闰七月不过节
	}
	for date, want := range cases {
		if got := DayInfoOf(day(date)).Festival; got != want {
			t.Errorf("%s 农历节日 = %q，应为 %q", date, got, want)
		}
	}
}

// TestOfficialHolidays2026 核对 2026 年官方安排（国办 2025-11-04 发布）。
func TestOfficialHolidays2026(t *testing.T) {
	cases := []struct {
		date string
		name string
		kind string
		rest bool
		work bool
	}{
		{"2026-01-01", "元旦", "holiday", true, false},
		{"2026-01-03", "", "weekend", true, false},   // 元旦连休里的周六，非法定假日
		{"2026-01-04", "", "workday", false, true},   // 周日补班
		{"2026-02-14", "", "workday", false, true},   // 周六补班
		{"2026-02-15", "", "weekend", true, false},   // 春节连休首日是周日
		{"2026-02-16", "春节", "holiday", true, false}, // 除夕
		{"2026-02-19", "春节", "holiday", true, false}, // 正月初三
		{"2026-02-20", "", "rest", true, false},      // 正月初四：调休凑出来的假
		{"2026-02-23", "", "rest", true, false},      // 正月初七：同上
		{"2026-02-28", "", "workday", false, true},
		{"2026-04-04", "", "weekend", true, false},    // 清明假期首日是周六
		{"2026-04-05", "清明节", "holiday", true, false}, // 清明当天（周日）
		{"2026-04-06", "", "rest", true, false},       // 调休凑出来的周一
		{"2026-05-01", "劳动节", "holiday", true, false},
		{"2026-05-09", "", "workday", false, true},
		{"2026-06-19", "端午节", "holiday", true, false},
		{"2026-09-20", "", "workday", false, true},
		{"2026-09-25", "中秋节", "holiday", true, false},
		{"2026-10-01", "国庆节", "holiday", true, false},
		{"2026-10-03", "国庆节", "holiday", true, false}, // 周六，但当天是法定假日
		{"2026-10-05", "", "rest", true, false},       // 调休凑出来的假
		{"2026-10-10", "", "workday", false, true},
		{"2026-09-28", "", "normal", false, true}, // 普通周一
	}
	for _, c := range cases {
		info := DayInfoOf(day(c.date))
		if info.Holiday != c.name {
			t.Errorf("%s 节假日名 = %q，应为 %q", c.date, info.Holiday, c.name)
		}
		if info.Kind != c.kind {
			t.Errorf("%s kind = %s，应为 %s", c.date, info.Kind, c.kind)
		}
		if info.IsRest != c.rest {
			t.Errorf("%s isRest = %v，应为 %v", c.date, info.IsRest, c.rest)
		}
		if info.IsWorkday != c.work {
			t.Errorf("%s isWorkday = %v，应为 %v", c.date, info.IsWorkday, c.work)
		}
	}
}

// TestStatutoryDaysEras 法定假日天数分两个时代：2024 及以前 11 天，2025 年起 13 天。
// 修订后的《放假办法》自 2025-01-01 施行，除夕与 5 月 2 日才成为法定假日；
// 若一视同仁按 13 天算，2024-02-09（除夕）会被误标成放假。
func TestStatutoryDaysEras(t *testing.T) {
	if n := len(StatutoryDays(2024)); n != 11 {
		t.Errorf("2024 年法定假日 %d 天，应为 11 天：%v", n, StatutoryDays(2024))
	}
	if n := len(StatutoryDays(2025)); n != 13 {
		t.Errorf("2025 年法定假日 %d 天，应为 13 天：%v", n, StatutoryDays(2025))
	}
	if n := len(StatutoryDays(2027)); n != 13 {
		t.Errorf("2027 年法定假日 %d 天，应为 13 天", n)
	}
	// 2024-02-09 是除夕，但当年还不是法定假日，那天要上班。
	eve := DayInfoOf(day("2024-02-09"))
	if eve.Festival != "除夕" {
		t.Errorf("2024-02-09 农历节日 = %q，应为「除夕」", eve.Festival)
	}
	if eve.IsHoliday || eve.Holiday != "" {
		t.Errorf("2024-02-09 不算法定节假日，实为 holiday=%q isHoliday=%v", eve.Holiday, eve.IsHoliday)
	}
	// 2024-05-02 同理：旧办法下劳动节只有 5 月 1 日。
	if DayInfoOf(day("2024-05-02")).IsHoliday {
		t.Error("2024-05-02 不算法定节假日")
	}
	// 2025 年起两者都是法定假日。
	if !DayInfoOf(day("2025-01-28")).IsHoliday {
		t.Error("2025-01-28（除夕）应算法定节假日")
	}
	if !DayInfoOf(day("2025-05-02")).IsHoliday {
		t.Error("2025-05-02 应算法定节假日")
	}
}

// TestOfficialHolidays2024 对齐国务院 2023-10 发文（holiday-cn 的 2024.json 已逐日核对过）。
func TestOfficialHolidays2024(t *testing.T) {
	cases := []struct {
		date string
		kind string
		rest bool
		work bool
	}{
		{"2024-01-01", "holiday", true, false},
		{"2024-02-09", "normal", false, true},  // 除夕：旧办法下仍要上班
		{"2024-02-10", "holiday", true, false}, // 正月初一
		{"2024-02-04", "workday", false, true}, // 春节前补班
		{"2024-04-07", "workday", false, true}, // 清明假期后的周日补班
		{"2024-06-10", "holiday", true, false}, // 端午单日放假，与周末连休
		{"2024-09-14", "workday", false, true}, // 中秋前补班
		{"2024-10-07", "rest", true, false},    // 国庆假期第 7 天，靠调休
		{"2024-10-12", "workday", false, true},
	}
	for _, c := range cases {
		info := DayInfoOf(day(c.date))
		if info.Kind != c.kind {
			t.Errorf("%s kind = %s，应为 %s", c.date, info.Kind, c.kind)
		}
		if info.IsRest != c.rest {
			t.Errorf("%s isRest = %v，应为 %v", c.date, info.IsRest, c.rest)
		}
		if info.IsWorkday != c.work {
			t.Errorf("%s isWorkday = %v，应为 %v", c.date, info.IsWorkday, c.work)
		}
	}
}

// firstEstimatedYear 返回第一个尚无国务院官方安排的年份（官方数据逐年录入：
// 每年 11 月发文后由 CI 同步），全部已收录时返回 0。
// 断言必须基于这个动态量，绝不能写死某个未来年份 —— 否则数据一到，CI 必红。
func firstEstimatedYear() int {
	for y := 2024; y <= 2100; y++ {
		if !HasOfficialData(y) {
			return y
		}
	}
	return 0
}

// TestEmbeddedHolidayData 校验 go:embed 进来的 holidays.json。
// 文件缺失或 JSON 坏了会在包 init 里 panic，测试根本跑不起来；这里只管内容对不对。
func TestEmbeddedHolidayData(t *testing.T) {
	for _, year := range []int{2024, 2025, 2026} {
		if !HasOfficialData(year) {
			t.Errorf("%d 年应有官方数据", year)
		}
		if len(OfficialPapers(year)) == 0 {
			t.Errorf("%d 年缺国务院原文链接，无法溯源", year)
		}
		if IsEstimated(year) {
			t.Errorf("%d 年已有官方数据，不应标记为推算", year)
		}
	}
	// 第一个未收录年份必须标记为推算（找得到才校验：全部收录是远期才会发生的事）。
	if y := firstEstimatedYear(); y != 0 && !IsEstimated(y) {
		t.Errorf("%d 年尚无官方数据，应标记为推算值", y)
	}
	// 每年的法定假日必须都被官方标成放假：法律写死放假，通知不可能安排上班。
	for _, year := range []int{2024, 2025, 2026} {
		for _, d := range StatutoryDays(year) {
			t0, _ := time.ParseInLocation("2006-01-02", d.Day, time.Local)
			if !IsRestDay(t0) {
				t.Errorf("%s（%s）是法定假日，却被判定为上班", d.Day, d.Name)
			}
		}
	}
	// 抽查几个补班日确实被标成上班。
	for _, d := range []string{"2024-04-07", "2025-09-28", "2026-09-20", "2026-10-10"} {
		if !IsMakeUpWorkday(day(d)) {
			t.Errorf("%s 应为调休上班日", d)
		}
		if IsRestDay(day(d)) {
			t.Errorf("%s 是补班日，不应判为休息", d)
		}
	}
}

// TestEstimatedYears 国务院尚未公布的年份，退回放假办法规定的 13 天法定假日。
// 「哪年已公布」逐年变化（每年 11 月发文 + CI 同步），凡与此相关的断言都必须用
// firstEstimatedYear() 这个动态量，写死未来年份会让数据一到 CI 就红。
func TestEstimatedYears(t *testing.T) {
	if !HasOfficialData(2026) {
		t.Fatal("2026 年安排已录入，HasOfficialData 应为 true")
	}
	// 推算算法的确定性快照：法定假日由《放假办法》+ 农历决定，与官方调休通知无关，
	// 即便 2027 官方数据日后录入，这组日期也不会变，断言依然成立。
	days := StatutoryDays(2027)
	if len(days) != 13 {
		t.Fatalf("2027 年法定假日 %d 天，应为 13 天：%v", len(days), days)
	}
	for _, d := range []string{
		"2027-01-01",                             // 元旦
		"2027-02-05",                             // 除夕
		"2027-02-06", "2027-02-07", "2027-02-08", // 正月初一至初三
		"2027-04-05",               // 清明
		"2027-05-01", "2027-05-02", // 劳动节
		"2027-06-09",                             // 端午
		"2027-09-15",                             // 中秋
		"2027-10-01", "2027-10-02", "2027-10-03", // 国庆
	} {
		if !IsStatutoryHoliday(day(d)) {
			t.Errorf("%s 应算法定节假日", d)
		}
		if !IsRestDay(day(d)) {
			t.Errorf("%s 应算休息日", d)
		}
	}
	// Estimated 标记跟着数据走：校验「第一个未收录年份」而非写死 2027。
	y := firstEstimatedYear()
	if y == 0 {
		t.Skip("官方数据已覆盖全部年份，无推算年份可校验")
	}
	if HasOfficialData(y) {
		t.Fatalf("%d 年尚无官方数据，HasOfficialData 应为 false", y)
	}
	if !DayInfoOf(day(fmt.Sprintf("%d-10-01", y))).Estimated {
		t.Errorf("%d 年应标记为推算值", y)
	}
	// 推算年份同样必须凑齐 13 天法定假日，且全部落在休息日。
	if n := len(StatutoryDays(y)); n != 13 {
		t.Errorf("%d 年推算法定假日 %d 天，应为 13 天", y, n)
	}
	for _, d := range StatutoryDays(y) {
		t0, _ := time.ParseInLocation("2006-01-02", d.Day, time.Local)
		if !IsRestDay(t0) {
			t.Errorf("%s（%s）是推算法定假日，却未被判定为休息日", d.Day, d.Name)
		}
	}
}

func TestNextQueries(t *testing.T) {
	// 中秋（2026-09-26，周日）之后的下一个法定工作日：假期到 9-27，9-28 周一上班。
	got, ok := NextLegalWorkday(day("2026-09-26"))
	if !ok || fmtDay(got) != "2026-09-28" {
		t.Errorf("下一个法定工作日 = %v，应为 2026-09-28", got)
	}
	// 之后的下一个法定节假日：9-26/27 只是中秋连休里的周末，不算法定假日，
	// 所以直接跳到国庆 10-01。
	got, ok = NextStatutoryHoliday(day("2026-09-26"))
	if !ok || fmtDay(got) != "2026-10-01" {
		t.Errorf("下一个法定节假日 = %v，应为 2026-10-01", got)
	}
	// 连休里调休凑出来的日子也不算，10-04 之后仍是 10-05 放假但非法定假日 → 跳过。
	got, ok = NextStatutoryHoliday(day("2026-10-03"))
	if !ok || fmtDay(got) != "2027-01-01" {
		t.Errorf("国庆法定假之后的下一个法定节假日 = %v，应为 2027-01-01", got)
	}
	// 9-28 周一之后的下一个周末：10-03 周六（10-01 起放假，但周末判定只看周六日）。
	got, ok = NextWeekendDay(day("2026-09-28"))
	if !ok || fmtDay(got) != "2026-10-03" {
		t.Errorf("下一个周末 = %v，应为 2026-10-03", got)
	}
	// 周六之后的下一个工作日（周一至周五）：周一。
	got, ok = NextWeekday(day("2026-10-03"))
	if !ok || fmtDay(got) != "2026-10-05" {
		t.Errorf("下一个工作日 = %v，应为 2026-10-05", got)
	}
}

func TestNextLunarMonth(t *testing.T) {
	// 农历八月初五（2026-09-15）的下一个「农历每月初五」是九月初五。
	got, ok := NextLunarMonthDay(day("2026-09-15"))
	if !ok {
		t.Fatal("农历每月推不出下一次")
	}
	if info := DayInfoOf(got); info.LunarFull != "农历九月初五" {
		t.Errorf("下一次 = %s（%s），应为农历九月初五", fmtDay(got), info.LunarFull)
	}
	// 农历三十遇上小月要退到月末：正月三十（2025-02-27）之后是二月三十？二月只有廿九 → 廿九。
	got, ok = NextLunarMonthDay(day("2025-02-27"))
	if !ok {
		t.Fatal("农历每月推不出下一次")
	}
	if info := DayInfoOf(got); info.LunarFull != "农历二月廿九" {
		t.Errorf("正月三十的下一次 = %s（%s），应为农历二月廿九", fmtDay(got), info.LunarFull)
	}
}

func TestNextLunarYear(t *testing.T) {
	// 农历八月十五（2026-09-25）的下一年中秋是 2027-09-15。
	got, ok := NextLunarYearDay(day("2026-09-25"))
	if !ok {
		t.Fatal("农历每年推不出下一次")
	}
	if fmtDay(got) != "2027-09-15" {
		t.Errorf("下一年中秋 = %s，应为 2027-09-15", fmtDay(got))
	}
	// 闰月生日在没有闰月的年份退回平月：闰六月初三 → 次年的六月初三。
	leapDay, ok := ToSolar(Date{Year: 2025, Month: LeapMonth(2025), Day: 3, IsLeap: true})
	if !ok {
		t.Fatal("闰六月初三换算失败")
	}
	if info := DayInfoOf(leapDay); !info.IsLeapMonth {
		t.Fatalf("%s 应为闰月，实为 %s", fmtDay(leapDay), info.LunarFull)
	}
	got, ok = NextLunarYearDay(leapDay)
	if !ok {
		t.Fatal("闰月生日推不出下一次")
	}
	if info := DayInfoOf(got); info.LunarFull != "农历六月初三" {
		t.Errorf("闰六月初三的下一年 = %s（%s），应为农历六月初三", fmtDay(got), info.LunarFull)
	}
}

func TestLeapMonthBasics(t *testing.T) {
	if leap := LeapMonth(2025); leap != 6 {
		t.Errorf("2025 年闰月 = %d，应为 6", leap)
	}
	if leap := LeapMonth(2026); leap != 0 {
		t.Errorf("2026 年闰月 = %d，应为 0", leap)
	}
	// 闰月紧跟在同月的平月之后：闰六月初一应在六月初一之后、七月初一之前。
	plain, _ := ToSolar(Date{Year: 2025, Month: 6, Day: 1})
	leap, _ := ToSolar(Date{Year: 2025, Month: 6, Day: 1, IsLeap: true})
	next, _ := ToSolar(Date{Year: 2025, Month: 7, Day: 1})
	if !leap.After(plain) || !leap.Before(next) {
		t.Errorf("闰六月初一 %s 应落在六月初一 %s 与七月初一 %s 之间", fmtDay(leap), fmtDay(plain), fmtDay(next))
	}
}

func TestDayInfosRange(t *testing.T) {
	got := DayInfos(day("2026-11-01"), day("2026-11-03"))
	if len(got) != 3 {
		t.Fatalf("区间天数 = %d，应为 3", len(got))
	}
	// 11-01 是农历九月廿三；初一那格要显示月名而不是「初一」。
	if got[0].Label != "廿三" || got[1].Label != "廿四" || got[2].Label != "廿五" {
		t.Errorf("区间标签 = %q / %q / %q", got[0].Label, got[1].Label, got[2].Label)
	}
	nov := DayInfoOf(day("2026-10-10"))
	if nov.LunarText != "九月" {
		t.Errorf("农历九月初一的短名 = %q，应为「九月」", nov.LunarText)
	}
	// 过长区间要被截断，避免接口被当负载测试用。
	long := DayInfos(day("2026-01-01"), day("2028-01-01"))
	if len(long) != 366 {
		t.Errorf("超长区间未截断，得到 %d 天", len(long))
	}
}
