package lunar

import (
	"embed"
	"encoding/json"
	"sort"
	"strconv"
	"sync"
	"time"
)

// 法定节假日。
//
// 国务院办公厅每年 11 月前后发布次年安排，因此「放假几天、哪天补班」是**逐年公布**的，
// 只有前一年年底之后才能拿到确定值。本文件的口径是：
//
//  1. 已公布的年份，用 holidays.json 里的官方逐日数据（本文件随二进制 embed）；
//  2. 尚未公布的年份，退回《全国年节及纪念日放假办法》规定的法定假日
//     （2024 及以前 11 天，2025 年起 13 天）
//     —— 这些天由法律固定，不随通知变动，能一直算准；连休与补班则不给，
//     并在接口里标 estimated=true，提醒用户「调休安排待公布」。
//
// 宁可少标一天调休，也不要凭想象编一份放假表。
//
// ## 每年怎么更新
//
// 国务院发文（通常 11 月）后跑一条命令即可，**不要手改 holidays.json**：
//
//	node scripts/holiday-cn.mjs --sync 2028            # 直接写文件
//	./bin/shenshi -addr :8787                          # 校验走本地接口
//	node scripts/holiday-cn.mjs --check 2028           # 应输出「0 处不一致」
//	cd server && go test ./internal/lunar/
//
// 脚本对接 NateScarlet/holiday-cn（每日抓取国务院公告、CI 自动更新），
// 生成的文件里带国务院原文链接，可随时回溯。

//go:embed holidays.json
var holidayFS embed.FS

// holidayDay 是官方数据里的**一天**：isOffDay 为真表示这天放假，
// 为假表示这天是周末但要补班。name 是所属节日名，不区分是否法定假日当天。
//
// 注意官方数据不含「与周末连休的纯周末」—— 那些天靠 IsWeekend 判定即可。
type holidayDay struct {
	Date     string `json:"date"`
	Name     string `json:"name"`
	IsOffDay bool   `json:"isOffDay"`
}

type holidayYear struct {
	Papers []string     `json:"papers"`
	Days   []holidayDay `json:"days"`
}

type holidayFile struct {
	Source string                 `json:"source"`
	Years  map[string]holidayYear `json:"years"`
}

var (
	// dayMarks 是「日期 → 官方标记」的平表，查询 O(1)，不必再算区间。
	dayMarks      map[string]holidayDay
	officialYears map[int]bool
	// papersByYear 保存每年所依据的国务院文件链接，供溯源。
	papersByYear map[int][]string
)

func init() {
	raw, err := holidayFS.ReadFile("holidays.json")
	if err != nil {
		// 文件由 go:embed 打进二进制，读不到只可能是构建坏了，fail fast 好过静默降级。
		panic("lunar: 读取 holidays.json 失败：" + err.Error())
	}
	var f holidayFile
	if err := json.Unmarshal(raw, &f); err != nil {
		panic("lunar: 解析 holidays.json 失败：" + err.Error())
	}
	dayMarks = make(map[string]holidayDay, 512)
	officialYears = make(map[int]bool, len(f.Years))
	papersByYear = make(map[int][]string, len(f.Years))
	for ys, y := range f.Years {
		year, err := strconv.Atoi(ys)
		if err != nil || len(y.Days) == 0 {
			continue
		}
		officialYears[year] = true
		papersByYear[year] = y.Papers
		for _, d := range y.Days {
			dayMarks[d.Date] = d
		}
	}
}

// HasOfficialData 报告某年是否已录入官方放假安排。
func HasOfficialData(year int) bool { return officialYears[year] }

// OfficialPapers 返回某年放假安排所依据的国务院文件链接，便于溯源核对。
func OfficialPapers(year int) []string { return papersByYear[year] }

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// ---------- 法定假日（放假办法规定的 13 天） ----------

var statutoryCache sync.Map // year -> []StatutoryDay

// StatutoryDay 是放假办法规定的一天法定假日。
type StatutoryDay struct {
	Day  string // YYYY-MM-DD
	Name string // 元旦 / 春节 / 清明节 / 劳动节 / 端午节 / 中秋节 / 国庆节
}

// StatutoryDays 返回某年《全国年节及纪念日放假办法》规定的法定假日。
//
// 天数分两个时代，别混用：
//   - 2024 年及以前：共 11 天 —— 元旦 1；春节 3（正月初一至初三，**不含除夕**）；
//     清明 1；劳动节 1（5 月 1 日）；端午 1；中秋 1；国庆 3。
//   - 2025 年起：修订后的办法施行，除夕与 5 月 2 日纳入法定假日，共 13 天。
//
// 这些天由法律固定，与逐年发布的调休通知无关 —— 连休区间会变，法定假日不会。
func StatutoryDays(year int) []StatutoryDay {
	if !Supported(year) {
		return nil
	}
	if v, ok := statutoryCache.Load(year); ok {
		return v.([]StatutoryDay)
	}
	// 修订后的《放假办法》自 2025-01-01 施行，此前除夕不算法定假日。
	since2025 := year >= 2025
	var out []StatutoryDay
	add := func(t time.Time, ok bool, name string) {
		if ok {
			out = append(out, StatutoryDay{Day: dayKey(t), Name: name})
		}
	}
	add(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC), true, "元旦")
	if since2025 {
		// 除夕是农历年 year-1 的腊月最后一天。
		eve, okEve := ToSolar(Date{Year: year - 1, Month: 12, Day: MonthDays(year-1, 12)})
		add(eve, okEve, "春节")
	}
	for _, d := range []Date{{Year: year, Month: 1, Day: 1}, {Year: year, Month: 1, Day: 2}, {Year: year, Month: 1, Day: 3}} {
		t, ok := ToSolar(d)
		add(t, ok, "春节")
	}
	if qingming, ok := SolarTermDate(year, 6); ok { // 6 = 清明
		add(qingming, true, "清明节")
	}
	add(time.Date(year, 5, 1, 0, 0, 0, 0, time.UTC), true, "劳动节")
	if since2025 {
		add(time.Date(year, 5, 2, 0, 0, 0, 0, time.UTC), true, "劳动节")
	}
	{
		t, ok := ToSolar(Date{Year: year, Month: 5, Day: 5})
		add(t, ok, "端午节")
	}
	{
		t, ok := ToSolar(Date{Year: year, Month: 8, Day: 15})
		add(t, ok, "中秋节")
	}
	add(time.Date(year, 10, 1, 0, 0, 0, 0, time.UTC), true, "国庆节")
	add(time.Date(year, 10, 2, 0, 0, 0, 0, time.UTC), true, "国庆节")
	add(time.Date(year, 10, 3, 0, 0, 0, 0, time.UTC), true, "国庆节")
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	statutoryCache.Store(year, out)
	return out
}

// statutoryMap 返回「日期 → 法定节假日名」，用于按天查名。
func statutoryMap(year int) map[string]string {
	m := map[string]string{}
	for _, d := range StatutoryDays(year) {
		m[d.Day] = d.Name
	}
	return m
}

// ---------- 对外查询 ----------

// HolidaySpanName 返回这一天所属官方假期的节日名（如「国庆节」）。
// 连休 7 天里只有 10 月 1 至 3 日是法定假日，其余几天靠调休凑出来，
// 但它们同样放假 —— 这个方法回答的是「这天放的是哪个假」。
// 调休上班日虽然名义上属于某个假期，但那天要上班，不算。
func HolidaySpanName(t time.Time) string {
	if d, ok := dayMarks[dayKey(t)]; ok && d.IsOffDay {
		return d.Name
	}
	return ""
}

// IsMakeUpWorkday 报告这天是否是调休上班日（周末但需补班）。
func IsMakeUpWorkday(t time.Time) bool {
	d, ok := dayMarks[dayKey(t)]
	return ok && !d.IsOffDay
}

// StatutoryName 返回这天若是法定节假日的名称（如「中秋节」），否则 ""。
//
// 判据是《放假办法》规定的 13 天，与「官方连休区间」不是一回事：
// 2026 年国庆连休 10 月 1 至 7 日，但其中只有 1 至 3 日是法定假日。
// 日历上标名字、以及「每个法定节假日」重复规则，都以这份口径为准。
func StatutoryName(t time.Time) string {
	if !Supported(t.Year()) {
		return ""
	}
	return statutoryMap(t.Year())[dayKey(t)]
}

// IsStatutoryHoliday 报告这天是否是法定节假日。
func IsStatutoryHoliday(t time.Time) bool { return StatutoryName(t) != "" }

// IsWeekend 报告这天是否是周六或周日。
func IsWeekend(t time.Time) bool {
	w := t.Weekday()
	return w == time.Saturday || w == time.Sunday
}

// IsRestDay 报告这天是否是法定休息日：
// 法定节假日、官方连休区间内的日子，或未被调休占用的周末。
func IsRestDay(t time.Time) bool {
	// 法定假日永远休息，且绝不会被安排补班，先判最稳。
	if IsStatutoryHoliday(t) {
		return true
	}
	if IsMakeUpWorkday(t) {
		return false
	}
	return HolidaySpanName(t) != "" || IsWeekend(t)
}

// IsLegalWorkday 报告这天是否是法定工作日（需要上班的日子）。
// 与 IsRestDay 互补：周一至周五且非法定假日，或周末调休补班。
func IsLegalWorkday(t time.Time) bool { return !IsRestDay(t) }

// IsEstimated 报告某年的放假安排是否为推算值（国务院尚未公布）。
func IsEstimated(year int) bool { return !HasOfficialData(year) }
