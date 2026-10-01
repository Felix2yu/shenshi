// Package push 把到期提醒与每日概览经 Apprise 推到手机等外部渠道。
//
// 浏览器通知只在页面打开时可用，移动端不可能常开页面——这里是服务端兜底：
// 进程内轮询提醒台账，该推的经 apprise-go 送出去，独立台账去重，
// 与页面内的提醒中心互不干扰（见 store.push_log）。
//
// 渠道用 Apprise URL 声明（ntfy://、bark://、wxpusher://、smtp:// 等，
// 一套语法覆盖上百种服务），配置存设置键，随时可改、改完即生效。
package push

import (
	"log"
	"strconv"
	"strings"
	"time"

	apprise "github.com/unraid/apprise-go"
	"github.com/yufei/shendu/server/internal/store"
)

// 设置键，与前端「推送通知」设置区块一一对应。
const (
	SetEnabled      = "push.enabled"      // "1" 总开关：关掉则一切推送静默
	SetURLs         = "push.appriseUrls"  // 每行一条 Apprise URL，# 开头视为注释
	SetDailyEnabled = "push.dailyEnabled" // "1" 每日概览推送
	SetDailyTime    = "push.dailyTime"    // "HH:MM"，默认 09:00
)

const (
	tickEvery = 30 * time.Second
	// 只推「已到期」的提醒，不抢页面的活：lookahead 给 0，
	// 提前量提醒（如提前 30 分钟）仍由打开着的页面负责，服务端只兜底准点与逾期。
	lookahead = time.Duration(0)
	// 补推窗口：进程重启/停机期间到点的提醒，5 分钟内仍会补推。
	// 给得太宽，首次启用就会把一堆陈年旧账推到手机上。
	lookback = 5 * time.Minute
	// 台账保留期：推送键自带日期，过期条目不会再命中，定期清掉即可。
	logKeep = 30 * 24 * time.Hour
)

// Sender 是对 Apprise 发送动作的抽象，测试时可注入假实现。
type Sender func(urls []string, title, body string) error

func defaultSend(urls []string, title, body string) error {
	return apprise.Send(urls, body,
		apprise.WithTitle(title),
		apprise.WithNotifyType(apprise.NotifyInfo),
	)
}

// Pusher 周期巡检提醒与每日概览，经 Apprise 推送。
// 与 AutoBackup 同一套进程内定时器的思路：整个应用就一个二进制，
// 再让用户去配系统定时器就把开箱即用弄丢了。
type Pusher struct {
	st   *store.Store
	logf func(format string, v ...any)
	send Sender
	stop chan struct{}
	done chan struct{}
}

// New 构造推送器。logf 为空时使用标准日志。
func New(st *store.Store, logf func(format string, v ...any)) *Pusher {
	if logf == nil {
		logf = log.Printf
	}
	return &Pusher{st: st, logf: logf, send: defaultSend, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start 后台运行。可重复调用，只有第一次生效。
func (p *Pusher) Start() {
	go p.loop()
}

// Stop 结束后台循环并等待退出。
func (p *Pusher) Stop() {
	close(p.stop)
	<-p.done
}

func (p *Pusher) loop() {
	defer close(p.done)
	// 启动时先清一次旧台账，再立刻巡检一轮——重启期间到点的提醒能马上补上。
	if err := p.st.PrunePushLog(time.Now().Add(-logKeep)); err != nil {
		p.logf("push: 清理推送台账失败: %v", err)
	}
	p.tick(time.Now())
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case t := <-ticker.C:
			p.tick(t)
		}
	}
}

// Config 是从设置键读出的推送配置。
type Config struct {
	Enabled      bool
	URLs         []string
	DailyEnabled bool
	DailyTime    string // "HH:MM"
}

// LoadConfig 从设置键值里解析推送配置。URL 按行拆分，空行与 # 注释行忽略；
// 每日时间不合法时退回默认 09:00。
func LoadConfig(kv map[string]string) Config {
	cfg := Config{
		Enabled:      kv[SetEnabled] == "1",
		DailyEnabled: kv[SetDailyEnabled] == "1",
		DailyTime:    "09:00",
	}
	if t := kv[SetDailyTime]; len(t) == 5 && validHM(t) {
		cfg.DailyTime = t
	}
	for _, line := range strings.Split(kv[SetURLs], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cfg.URLs = append(cfg.URLs, line)
	}
	return cfg
}

func validHM(s string) bool {
	if s[2] != ':' {
		return false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	return err1 == nil && err2 == nil && h >= 0 && h < 24 && m >= 0 && m < 60
}

// tick 巡检一轮。读设置失败只记日志：下一轮 30 秒后再来，不必惊动调用方。
func (p *Pusher) tick(now time.Time) {
	kv, err := p.st.Settings()
	if err != nil {
		p.logf("push: 读取设置失败: %v", err)
		return
	}
	cfg := LoadConfig(kv)
	if !cfg.Enabled || len(cfg.URLs) == 0 {
		return
	}
	p.pushReminders(cfg, now)
	if cfg.DailyEnabled {
		p.pushDaily(cfg, now)
	}
}

// pushReminders 把补推窗口内该推的提醒逐条送出。
// 逐条而非合并：每条有独立台账键，某一条失败不影响其余，重试也只重试失败的。
func (p *Pusher) pushReminders(cfg Config, now time.Time) {
	hits, err := p.st.DueReminders(now, lookahead, lookback)
	if err != nil {
		p.logf("push: 读取到期提醒失败: %v", err)
		return
	}
	for _, h := range hits {
		key := ReminderKey(h.AckID, h.FireAt)
		state, err := p.st.PushState(key)
		if err != nil {
			p.logf("push: 查询推送台账失败: %v", err)
			continue
		}
		if state.Done {
			continue
		}
		name := h.Task.Title
		if h.Subtask != nil {
			name = h.Task.Title + " · " + h.Subtask.Title
		}
		if err := p.send(cfg.URLs, "慎始 · "+name, h.DueLabel+"（已到时间）"); err != nil {
			_ = p.st.RecordPush(key, false, truncate(err.Error(), 300))
			p.logf("push: 推送提醒失败（第 %d 次）：%s", state.Attempts+1, err)
			continue
		}
		if err := p.st.RecordPush(key, true, ""); err != nil {
			p.logf("push: 记录推送台账失败: %v", err)
		}
	}
}

// pushDaily 在配置的时点之后把当日概览推出去，一天至多一条。
func (p *Pusher) pushDaily(cfg Config, now time.Time) {
	fire, err := time.ParseInLocation("15:04", cfg.DailyTime, time.Local)
	if err != nil {
		p.logf("push: 每日推送时间不合法: %q", cfg.DailyTime)
		return
	}
	fireAt := time.Date(now.Year(), now.Month(), now.Day(), fire.Hour(), fire.Minute(), 0, 0, time.Local)
	if now.Before(fireAt) {
		return
	}
	key := DailyKey(now.Format("2006-01-02"))
	state, err := p.st.PushState(key)
	if err != nil {
		p.logf("push: 查询推送台账失败: %v", err)
		return
	}
	if state.Done {
		return
	}
	sum, err := p.st.DailySummary(now)
	if err != nil {
		p.logf("push: 统计每日概览失败: %v", err)
		return
	}
	if sum.Today == 0 && sum.Overdue == 0 {
		// 无事一身轻：空概览不推，但照常落账，免得每个周期都白算一遍。
		_ = p.st.RecordPush(key, true, "")
		return
	}
	var b strings.Builder
	b.WriteString("今日待办 ")
	b.WriteString(strconv.Itoa(sum.Today))
	b.WriteString(" 项")
	if sum.Overdue > 0 {
		b.WriteString("，已逾期 ")
		b.WriteString(strconv.Itoa(sum.Overdue))
		b.WriteString(" 项")
	}
	if len(sum.Sample) > 0 {
		b.WriteString("：")
		b.WriteString(strings.Join(sum.Sample, "、"))
	}
	if err := p.send(cfg.URLs, "慎始 · 今日概览", b.String()); err != nil {
		_ = p.st.RecordPush(key, false, truncate(err.Error(), 300))
		p.logf("push: 推送每日概览失败（第 %d 次）：%s", state.Attempts+1, err)
		return
	}
	_ = p.st.RecordPush(key, true, "")
}

// ReminderKey 拼提醒推送的台账键。ackId 与页面提醒共用约定：任务为正 id，子任务为负。
func ReminderKey(ackID int64, fireAt string) string {
	return "R|" + strconv.FormatInt(ackID, 10) + "|" + fireAt
}

// DailyKey 拼每日概览的台账键。
func DailyKey(day string) string {
	return "D|" + day
}

// TestResult 是测试发送的单条结果，逐地址返回，方便排错哪一条配错了。
type TestResult struct {
	URL   string `json:"url"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SendTest 向单个地址发一条测试消息。地址不合法也归入结果而不是整体报错，
// 设置面板里多条地址可以逐条看出哪条没配好。
func SendTest(rawURL, title, body string) TestResult {
	cl := apprise.New()
	if err := cl.Add(rawURL); err != nil {
		return TestResult{URL: rawURL, Error: err.Error()}
	}
	if err := cl.Send(body, apprise.WithTitle(title), apprise.WithNotifyType(apprise.NotifyInfo)); err != nil {
		return TestResult{URL: rawURL, Error: truncate(err.Error(), 300)}
	}
	return TestResult{URL: rawURL, OK: true}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
