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
	"errors"
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
	// 站点对外地址。推送是服务端发出的，它无从知道自己被哪个域名访问；
	// 想让通知「点一下就回到这一条」，只能由使用者自己填一次。
	SetBaseURL = "push.baseUrl" // 形如 https://shenshi.example.com，结尾不带斜杠
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

// Pusher 周期巡检提醒与每日概览，经 Apprise 与 Web Push 推送。
// 与 AutoBackup 同一套进程内定时器的思路：整个应用就一个二进制，
// 再让用户去配系统定时器就把开箱即用弄丢了。
type Pusher struct {
	st   *store.Store
	logf func(format string, v ...any)
	send Sender
	// webSend 为 nil 表示 Web Push 通道不可用（没配密钥或没订阅）。
	// 刻意做成字段而不是每次现构造：密钥解析要验 ECDSA，每轮重做一次纯属浪费。
	webSend WebSender
	stop    chan struct{}
	done    chan struct{}
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
	BaseURL      string // 站点对外地址，用于拼「回到这一条」的深链
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
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(kv[SetBaseURL]), "/")
	return cfg
}

// deepLink 拼「点一下回到这一条」的链接。没有配站点地址就返回空 ——
// 宁可不给链接，也不给一个打不开的假地址。
func (c Config) deepLink(query string) string {
	if c.BaseURL == "" {
		return ""
	}
	return c.BaseURL + "/?" + query
}

// remindLink 提醒的深链：带上回执 id，页面据此立刻拉一次提醒，不必等下一个轮询周期。
func (c Config) remindLink(ackID int64) string {
	return c.deepLink("remind=" + strconv.FormatInt(ackID, 10))
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
	// 两条通道各投一遍，共用同一套台账去重：任一条送达就不再投另一条，
	// 否则用户会在手机和邮件上各收一遍同一件事。
	// 顺序上先 Apprise 后 Web Push：Apprise 是已经验证过的主通道，
	// 新加的 Web Push 排后面，不该抢在前面影响既有行为。
	appriseOK := false
	if kv[SetEnabled] == "1" {
		cfg := LoadConfig(kv)
		if len(cfg.URLs) > 0 {
			appriseOK = p.pushReminders(cfg, now)
			if cfg.DailyEnabled {
				p.pushDaily(cfg, now)
			}
		}
	}
	if !appriseOK {
		p.pushRemindersWeb(kv, now)
		p.pushDailyWeb(kv, now)
	}
}

// pushReminders 把补推窗口内该推的提醒逐条送出。
// 逐条而非合并：每条有独立台账键，某一条失败不影响其余，重试也只重试失败的。
// 返回值表示「这次是否真的送出去了」——已成功过的提醒不算，
// 否则一条到点提醒会把整轮判成已投递，Web Push 就再没机会补投。
func (p *Pusher) pushReminders(cfg Config, now time.Time) bool {
	hits, err := p.st.DueReminders(now, lookahead, lookback)
	if err != nil {
		p.logf("push: 读取到期提醒失败: %v", err)
		return false
	}
	sent := false
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
		body := h.DueLabel + "（已到时间）"
		if link := cfg.remindLink(h.AckID); link != "" {
			body += "\n" + link
		}
		if err := p.send(cfg.URLs, "慎始 · "+name, body); err != nil {
			_ = p.st.RecordPush(key, false, truncate(err.Error(), 300))
			p.logf("push: 推送提醒失败（第 %d 次）：%s", state.Attempts+1, err)
			continue
		}
		if err := p.st.RecordPush(key, true, ""); err != nil {
			p.logf("push: 记录推送台账失败: %v", err)
		}
		sent = true
	}
	return sent
}

// pushRemindersWeb 走 Web Push 通道投提醒。与 Apprise 共用台账键，
// 所以同一条提醒不会被两条通道各投一遍。
func (p *Pusher) pushRemindersWeb(kv map[string]string, now time.Time) {
	send, subs := p.webChannel(kv)
	if send == nil {
		return
	}
	hits, err := p.st.DueReminders(now, lookahead, lookback)
	if err != nil {
		p.logf("push: 读取到期提醒失败: %v", err)
		return
	}
	cfg := Config{BaseURL: strings.TrimRight(strings.TrimSpace(kv[SetBaseURL]), "/")}
	for _, h := range hits {
		key := ReminderKey(h.AckID, h.FireAt)
		state, err := p.st.PushState(key)
		if err != nil || state.Done {
			continue
		}
		name := h.Task.Title
		if h.Subtask != nil {
			name = h.Task.Title + " · " + h.Subtask.Title
		}
		payload := WebPushPayload{
			Title: "慎始 · " + name,
			Body:  h.DueLabel + "（已到时间）",
			Tag:   key,
			URL:   cfg.remindLink(h.AckID),
			AckID: h.AckID,
		}
		if p.broadcast(send, subs, key, payload) {
			_ = p.st.RecordPush(key, true, "")
		}
	}
}

// broadcast 把一条消息投给全部订阅，返回是否至少成功一个。
// 订阅作废（410）就删掉：留着每一轮都要白等一次超时。
func (p *Pusher) broadcast(send WebSender, subs []store.PushSubscription, key string, payload WebPushPayload) bool {
	anyOK := false
	for _, sub := range subs {
		err := send(sub, payload)
		switch {
		case err == nil:
			anyOK = true
			_ = p.st.MarkSubscriptionSent(sub.Endpoint)
		case errors.Is(err, store.ErrSubscriptionGone):
			p.logf("push: 订阅已失效，已移除：%s", sub.Endpoint)
			_ = p.st.DeleteSubscription(sub.Endpoint)
			// 成功台账落在这里而不是调用方：全挂了才是真失败，
			// 落账与否取决于「用户有没有收到」，与哪条订阅成功无关。
			anyOK = false
		default:
			p.logf("push: Web Push 投递失败：%v", err)
		}
	}
	return anyOK
}

// webChannel 取出 Web Push 投递器与订阅列表。不可用时返回 (nil, nil)。
func (p *Pusher) webChannel(kv map[string]string) (WebSender, []store.PushSubscription) {
	subs, err := p.st.Subscriptions()
	if err != nil {
		p.logf("push: 读取订阅失败: %v", err)
		return nil, nil
	}
	if len(subs) == 0 {
		return nil, nil
	}
	if p.webSend == nil {
		send, err := NewWebSender(kv)
		if err != nil {
			return nil, nil
		}
		p.webSend = send
	}
	return p.webSend, subs
}

// pushDailyWeb 走 Web Push 通道投每日概览。
func (p *Pusher) pushDailyWeb(kv map[string]string, now time.Time) {
	if kv[SetDailyEnabled] != "1" {
		return
	}
	send, subs := p.webChannel(kv)
	if send == nil {
		return
	}
	fire, err := time.ParseInLocation("15:04", LoadConfig(kv).DailyTime, time.Local)
	if err != nil || now.Before(time.Date(now.Year(), now.Month(), now.Day(), fire.Hour(), fire.Minute(), 0, 0, time.Local)) {
		return
	}
	key := DailyKey(now.Format("2006-01-02"))
	state, err := p.st.PushState(key)
	if err != nil || state.Done {
		return
	}
	sum, err := p.st.DailySummary(now)
	if err != nil || (sum.Today == 0 && sum.Overdue == 0) {
		_ = p.st.RecordPush(key, true, "")
		return
	}
	var b strings.Builder
	b.WriteString("今日待办 " + strconv.Itoa(sum.Today) + " 项")
	if sum.Overdue > 0 {
		b.WriteString("，已逾期 " + strconv.Itoa(sum.Overdue) + " 项")
	}
	if len(sum.Sample) > 0 {
		b.WriteString("：" + strings.Join(sum.Sample, "、"))
	}
	base := strings.TrimRight(strings.TrimSpace(kv[SetBaseURL]), "/")
	payload := WebPushPayload{Title: "慎始 · 今日概览", Body: b.String(), Tag: key}
	if base != "" {
		payload.URL = base + "/?view=today"
	}
	if p.broadcast(send, subs, key, payload) {
		_ = p.st.RecordPush(key, true, "")
	}
}

// pushDaily 在配置的时点之后把当日概览推出去，一天至多一条。
func (p *Pusher) pushDaily(cfg Config, now time.Time) bool {
	fire, err := time.ParseInLocation("15:04", cfg.DailyTime, time.Local)
	if err != nil {
		p.logf("push: 每日推送时间不合法: %q", cfg.DailyTime)
		return false
	}
	fireAt := time.Date(now.Year(), now.Month(), now.Day(), fire.Hour(), fire.Minute(), 0, 0, time.Local)
	if now.Before(fireAt) {
		return false
	}
	key := DailyKey(now.Format("2006-01-02"))
	state, err := p.st.PushState(key)
	if err != nil {
		p.logf("push: 查询推送台账失败: %v", err)
		return false
	}
	if state.Done {
		return false
	}
	sum, err := p.st.DailySummary(now)
	if err != nil {
		p.logf("push: 统计每日概览失败: %v", err)
		return false
	}
	if sum.Today == 0 && sum.Overdue == 0 {
		// 无事一身轻：空概览不推，但照常落账，免得每个周期都白算一遍。
		_ = p.st.RecordPush(key, true, "")
		return false
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
	if link := cfg.deepLink("view=today"); link != "" {
		b.WriteString("\n")
		b.WriteString(link)
	}
	if err := p.send(cfg.URLs, "慎始 · 今日概览", b.String()); err != nil {
		_ = p.st.RecordPush(key, false, truncate(err.Error(), 300))
		p.logf("push: 推送每日概览失败（第 %d 次）：%s", state.Attempts+1, err)
		return false
	}
	_ = p.st.RecordPush(key, true, "")
	return true
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
