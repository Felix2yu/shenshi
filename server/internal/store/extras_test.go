package store

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// TestAttachmentLifecycle 上传 → 列表 → 读取 → 删除 的全链路与不存在路径。
func TestAttachmentLifecycle(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "带附件的任务", nil)

	// 上传不存在的任务。
	if _, err := s.SaveAttachment(999999, "x.txt", "text/plain", strings.NewReader("x")); !errors.Is(err, ErrNotFound) {
		t.Errorf("任务不存在应 ErrNotFound，得到 %v", err)
	}

	att, err := s.SaveAttachment(task.ID, "../../evil 名字.txt", "text/plain", strings.NewReader("内容"))
	if err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	// 展示名取 basename 并去掉控制字符；存储名由服务端随机生成。
	if strings.Contains(att.Name, "/") || strings.Contains(att.Name, "..") {
		t.Errorf("展示名应已净化: %q", att.Name)
	}
	if att.Size != int64(len("内容")) || att.Mime != "text/plain" {
		t.Errorf("size/mime = %d/%q", att.Size, att.Mime)
	}
	// 磁盘上真实存在。
	if _, err := s.GetAttachment(att.ID); err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if _, err := os.Stat(s.AttachmentPath(att)); err != nil {
		t.Errorf("附件文件应已落盘: %v", err)
	}

	// 任务装配出附件。
	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Attachments) != 1 {
		t.Fatalf("装配附件数 = %d，期望 1", len(got.Attachments))
	}

	// 超过 32MB 上限：多读一个字节的逻辑必须拦住，不允许静默截断。
	huge := bytes.NewReader(make([]byte, maxAttachmentSize+1))
	if _, err := s.SaveAttachment(task.ID, "big.bin", "application/octet-stream", huge); err == nil {
		t.Error("超限附件应被拒绝")
	} else {
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("应为 ValidationError，得到 %T: %v", err, err)
		}
	}

	if err := s.DeleteAttachment(att.ID); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}
	if err := s.DeleteAttachment(att.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除应 ErrNotFound，得到 %v", err)
	}
	if _, err := s.GetAttachment(att.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后 GetAttachment 应 ErrNotFound，得到 %v", err)
	}
}

// TestAttachmentPathSafety 存储名非法时指向必然不存在的占位，杜绝目录穿越。
func TestAttachmentPathSafety(t *testing.T) {
	s := newTestStore(t)
	good := &model.Attachment{File: "ok.png"}
	bad := &model.Attachment{File: "../escape.png"}
	if p := s.AttachmentPath(good); !strings.HasSuffix(p, "ok.png") {
		t.Errorf("合法路径 = %q", p)
	}
	if p := s.AttachmentPath(bad); !strings.HasSuffix(p, ".invalid-stored-name") {
		t.Errorf("穿越名应落占位路径，得到 %q", p)
	}
}

// TestAttachmentNameHelpers 展示名/存储名/后缀白名单的净化口径。
func TestAttachmentNameHelpers(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"a.png", "a.png", true},
		{" spaced.txt ", "spaced.txt", true},
		{"", "", false},
		{"  ", "", false},
		{".", "", false},
		{"..", "", false},
		{"a/b.png", "", false},
		{`a\b.png`, "", false},
		{"a\x00b", "", false},
	}
	for _, c := range cases {
		got, ok := safeStoredName(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("safeStoredName(%q) = (%q, %v)，期望 (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}

	disp := []struct{ in, want string }{
		{"/etc/passwd", "passwd"},
		{"正常文件.pdf", "正常文件.pdf"},
		{"a\nb\r\tc.txt", "abc.txt"},
		{"..", "附件"},
		{".", "附件"},
		{"", "附件"},
		{strings.Repeat("长", 200), strings.Repeat("长", 120)},
	}
	for _, c := range disp {
		if got := safeDisplayName(c.in); got != c.want {
			t.Errorf("safeDisplayName(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}

	exts := []struct {
		in, want string
	}{
		{"a.PNG", ".png"},
		{"a.tar.gz", ".bin"}, // 只取最后一段扩展名，.gz 不在白名单
		{"a.exe", ".bin"},
		{"a.html", ".bin"},
		{"a.pdf", ".pdf"},
		{"a", ".bin"},
		{"noext.", ".bin"},
	}
	for _, c := range exts {
		if got := safeExt(c.in); got != c.want {
			t.Errorf("safeExt(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestWebhookLifecycle 回调的建改删、URL/事件校验与密钥脱敏。
func TestWebhookLifecycle(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateWebhook(model.WebhookInput{}); err == nil {
		t.Error("空 URL 应被拒绝")
	}
	if _, err := s.CreateWebhook(model.WebhookInput{URL: optOf("ftp://x")}); err == nil {
		t.Error("非 http(s) 应被拒绝")
	}

	w, err := s.CreateWebhook(model.WebhookInput{
		Name:   optOf("测试回调"),
		URL:    optOf("https://example.com/hook"),
		Secret: optOf("topsecret"),
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	// 密钥不回传，只带 HasSecret 标记。
	if w.Secret != "" || !w.HasSecret {
		t.Errorf("密钥应被脱敏: secret=%q has=%v", w.Secret, w.HasSecret)
	}
	// 未指定事件 → 默认订阅创建与完成。
	if len(w.Events) != 2 {
		t.Errorf("默认事件 = %v，应含创建与完成", w.Events)
	}

	// 事件过滤后为空 → 拒绝。
	if _, err := s.UpdateWebhook(w.ID, model.WebhookInput{Events: optOf([]string{"nonsense"})}); err == nil {
		t.Error("全未知事件应被拒绝")
	}
	// URL 置空 / 非法协议 → 拒绝。
	if _, err := s.UpdateWebhook(w.ID, model.WebhookInput{URL: optOf("  ")}); err == nil {
		t.Error("更新为空 URL 应被拒绝")
	}
	if _, err := s.UpdateWebhook(w.ID, model.WebhookInput{URL: optOf("javascript:alert(1)")}); err == nil {
		t.Error("非法协议应被拒绝")
	}

	upd, err := s.UpdateWebhook(w.ID, model.WebhookInput{
		Events:  optOf([]string{model.EventTaskUpdated, "bogus", model.EventTaskUpdated}),
		Enabled: optOf(false),
	})
	if err != nil {
		t.Fatalf("UpdateWebhook: %v", err)
	}
	if len(upd.Events) != 1 || upd.Events[0] != model.EventTaskUpdated {
		t.Errorf("事件应过滤去重: %v", upd.Events)
	}
	if upd.Enabled {
		t.Error("enabled 应已关闭")
	}
	// 空 PATCH 直接回读。
	if _, err := s.UpdateWebhook(w.ID, model.WebhookInput{}); err != nil {
		t.Errorf("空更新应成功: %v", err)
	}
	if _, err := s.UpdateWebhook(999999, model.WebhookInput{Name: optOf("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("更新不存在应 ErrNotFound，得到 %v", err)
	}

	// 事件索引：关闭的不参与投递。
	hookForCreated, err := s.WebhooksForEvent(model.EventTaskCreated)
	if err != nil {
		t.Fatalf("WebhooksForEvent: %v", err)
	}
	for _, item := range hookForCreated {
		if item.ID == w.ID {
			t.Error("已停用回调不应参与事件投递")
		}
	}
	hookForUpdated, _ := s.WebhooksForEvent(model.EventTaskUpdated)
	found := false
	for _, item := range hookForUpdated {
		if item.ID == w.ID {
			found = true
		}
	}
	if found {
		t.Error("已停用回调即使事件匹配也不应出现")
	}

	if err := s.DeleteWebhook(w.ID); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	if err := s.DeleteWebhook(w.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除应 ErrNotFound，得到 %v", err)
	}
	if _, err := s.GetWebhook(w.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("删除后 GetWebhook 应 ErrNotFound，得到 %v", err)
	}
}

// TestWebhookHelpers URL/事件/截断纯函数。
func TestWebhookHelpers(t *testing.T) {
	if !isHTTPURL("http://a") || !isHTTPURL("https://a") || isHTTPURL("ws://a") {
		t.Error("isHTTPURL 只认 http(s)")
	}

	// 保留已知事件、去重、去空白；未知事件丢弃。
	got := normalizeEvents([]string{model.EventTaskCreated, "", "unknown.event", model.EventTaskCreated, " " + model.EventTaskDeleted})
	if len(got) != 2 || got[0] != model.EventTaskCreated || got[1] != model.EventTaskDeleted {
		t.Errorf("normalizeEvents = %v", got)
	}
	if out := normalizeEvents(nil); out == nil || len(out) != 0 {
		t.Errorf("空输入应得空切片，得到 %v", out)
	}

	if truncate("abc", 10) != "abc" {
		t.Error("不超限原样返回")
	}
	if got := truncate("中文四个字", 2); got != "中文…" {
		t.Errorf("超限按 rune 截断 = %q", got)
	}

	// 脱敏幂等。
	w := &model.Webhook{Secret: "s"}
	redactWebhook(w)
	if w.Secret != "" || !w.HasSecret {
		t.Errorf("redactWebhook = %+v", w)
	}
	w2 := &model.Webhook{}
	redactWebhook(w2)
	if w2.HasSecret {
		t.Error("无密钥时 HasSecret 应为假")
	}
}

// TestWebhookDeliveries 投递台账：超 20 条裁剪，limit 出界回落默认。
func TestWebhookDeliveries(t *testing.T) {
	s := newTestStore(t)
	w, err := s.CreateWebhook(model.WebhookInput{URL: optOf("https://example.com/hook")})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}

	// 25 条投递，只留最近 20 条。
	for i := 0; i < 25; i++ {
		if err := s.RecordDelivery(w.ID, model.EventTaskCreated, 200, i%2 == 0, strings.Repeat("e", 400)); err != nil {
			t.Fatalf("RecordDelivery(%d): %v", i, err)
		}
	}
	all, err := s.RecentDeliveries(w.ID, 100)
	if err != nil {
		t.Fatalf("RecentDeliveries: %v", err)
	}
	if len(all) != 20 {
		t.Errorf("台账条数 = %d，应裁剪到 20", len(all))
	}
	// 最新的在前；error 字段截断在 300 字内带省略号。
	if all[0].Code != 200 {
		t.Errorf("倒序首条 code = %d", all[0].Code)
	}
	if len([]rune(all[0].Error)) > 301 {
		t.Errorf("error 未截断: %d 字", len([]rune(all[0].Error)))
	}
	// 非法 limit 回落 20。
	lim, err := s.RecentDeliveries(w.ID, -5)
	if err != nil || len(lim) != 20 {
		t.Errorf("limit 出界回落: n=%d err=%v", len(lim), err)
	}
}

// TestTemplateLifecycle 模板建改删，校验与排序位次。
func TestTemplateLifecycle(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateTemplate(model.TemplateInput{}); err == nil {
		t.Error("空名称应被拒绝")
	}
	if _, err := s.CreateTemplate(model.TemplateInput{Name: optOf("模板")}); err == nil {
		t.Error("空标题应被拒绝")
	}

	inbox, _ := s.InboxListID()
	tpl, err := s.CreateTemplate(model.TemplateInput{
		Name:      optOf("周报模板"),
		Title:     optOf("写周报"),
		Notes:     optOf("同步进展"),
		ListID:    optOfPtr(&inbox),
		Priority:  optOf(model.PriorityHigh),
		DueOffset: optOfPtr(intp(2)),
		DueTime:   optOfPtr(sp("17:00")),
		Reminders: optOf([]int{30}),
		Subtasks:  optOf([]string{"整理本周进展", "写风险", "  ", "列下周计划"}),
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if tpl.SortOrder <= 0 {
		t.Errorf("sortOrder 应自动递增，得到 %v", tpl.SortOrder)
	}
	if len(tpl.Subtasks) != 4 {
		t.Errorf("subtasks = %v", tpl.Subtasks)
	}

	// PATCH：改名 + 显式排序。
	if _, err := s.UpdateTemplate(tpl.ID, model.TemplateInput{Name: optOf("周报模板v2")}); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	// 空白名/标题拒绝。
	if _, err := s.UpdateTemplate(tpl.ID, model.TemplateInput{Name: optOf("  ")}); err == nil {
		t.Error("空白改名应被拒绝")
	}
	if _, err := s.UpdateTemplate(tpl.ID, model.TemplateInput{Title: optOf("")}); err == nil {
		t.Error("空白标题应被拒绝")
	}
	// 空 PATCH 回读。
	if _, err := s.UpdateTemplate(tpl.ID, model.TemplateInput{}); err != nil {
		t.Errorf("空更新应成功: %v", err)
	}
	if _, err := s.UpdateTemplate(999999, model.TemplateInput{Name: optOf("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("更新不存在应 ErrNotFound，得到 %v", err)
	}

	if err := s.DeleteTemplate(tpl.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if err := s.DeleteTemplate(tpl.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除应 ErrNotFound，得到 %v", err)
	}
}

// TestInstantiateTemplate 按模板落任务：偏移日期、覆盖清单与显式日期优先。
func TestInstantiateTemplate(t *testing.T) {
	s := newTestStore(t)
	inbox, _ := s.InboxListID()
	other, err := s.CreateList(ListInput{Name: sp("模板目标清单")})
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	tpl, err := s.CreateTemplate(model.TemplateInput{
		Name:      optOf("日常模板"),
		Title:     optOf("日常任务"),
		ListID:    optOfPtr(&inbox),
		DueOffset: optOfPtr(intp(1)),
		Priority:  optOf(model.PriorityMedium),
		Subtasks:  optOf([]string{"步骤一", "步骤二"}),
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// 不覆盖：按模板清单 + 今天+1。
	task, err := s.InstantiateTemplate(tpl.ID, nil, nil)
	if err != nil {
		t.Fatalf("InstantiateTemplate: %v", err)
	}
	if task.Title != "日常任务" || task.ListID != inbox {
		t.Errorf("任务 = %q list=%d", task.Title, task.ListID)
	}
	want := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if task.DueDate == nil || *task.DueDate != want {
		t.Errorf("dueDate = %v，期望 %s（偏移 1）", task.DueDate, want)
	}
	if !task.Urgent {
		t.Error("明天到期应派生紧急")
	}
	if len(task.Subtasks) != 2 || task.Subtasks[0].Title != "步骤一" {
		t.Errorf("子任务 = %+v", task.Subtasks)
	}
	if task.Priority != model.PriorityMedium {
		t.Errorf("priority = %d", task.Priority)
	}

	// 显式清单与日期覆盖模板。
	forced := "2026-11-11"
	otherID := other.ID
	task2, err := s.InstantiateTemplate(tpl.ID, &otherID, &forced)
	if err != nil {
		t.Fatalf("InstantiateTemplate(覆盖): %v", err)
	}
	if task2.ListID != other.ID || task2.DueDate == nil || *task2.DueDate != forced {
		t.Errorf("覆盖失败: list=%d due=%v", task2.ListID, task2.DueDate)
	}

	// 显式空日期 = 不带日期（覆盖掉模板偏移）。
	blank := ""
	task3, err := s.InstantiateTemplate(tpl.ID, nil, &blank)
	if err != nil {
		t.Fatalf("InstantiateTemplate(空日期): %v", err)
	}
	if task3.DueDate != nil {
		t.Errorf("显式空串应清空日期，得到 %v", *task3.DueDate)
	}
	if _, err := s.InstantiateTemplate(999999, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("模板不存在应 ErrNotFound，得到 %v", err)
	}
}

// TestCalDAVChangelog 变更日志按集合隔离；limit 截断时水位停在最后一条已返回变更。
func TestCalDAVChangelog(t *testing.T) {
	s := newTestStore(t)

	// 初始为空。
	changes, next, err := s.CalDAVChangesSince("todos", 0, 0)
	if err != nil {
		t.Fatalf("CalDAVChangesSince: %v", err)
	}
	if len(changes) != 0 || next != 0 {
		t.Errorf("空库 = %v next=%d", changes, next)
	}
	if maxSeq, err := s.CalDAVMaxSeq("todos"); err != nil || maxSeq != 0 {
		t.Errorf("空库 MaxSeq = %d err=%v", maxSeq, err)
	}

	// todos 三条、events 一条：跨集合不串。
	for i := 1; i <= 3; i++ {
		if err := s.NoteCalDAVChange("todos", "uid-"+itoa(i), int64(i), false); err != nil {
			t.Fatalf("NoteCalDAVChange: %v", err)
		}
	}
	if err := s.NoteCalDAVChange("events", "ev-1", 1, true); err != nil {
		t.Fatalf("NoteCalDAVChange(events): %v", err)
	}

	// 全量：水位 = 全库最大序号。
	all, next, err := s.CalDAVChangesSince("todos", 0, 0)
	if err != nil {
		t.Fatalf("全量: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("todos 变更 = %d，期望 3", len(all))
	}
	if next != all[2].Seq {
		t.Errorf("水位 = %d，应为最大 seq %d", next, all[2].Seq)
	}
	// 集合隔离：events 只看到自己那条，且是删除标记。
	ev, _, err := s.CalDAVChangesSince("events", 0, 0)
	if err != nil || len(ev) != 1 || !ev[0].Deleted {
		t.Errorf("events = %+v err=%v", ev, err)
	}

	// 截断：limit=2 只回 2 条，水位停在第 2 条 —— 若给全局 maxSeq，
	// 第 3 条变更会被客户端误判为已收齐而永远丢失。
	page, pageNext, err := s.CalDAVChangesSince("todos", 0, 2)
	if err != nil {
		t.Fatalf("分页: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("分页条数 = %d", len(page))
	}
	if pageNext != page[1].Seq {
		t.Errorf("截断水位 = %d，应停在最后一条已返回的 %d", pageNext, page[1].Seq)
	}
	// 拿水位续拉，能取回剩余。
	rest, restNext, err := s.CalDAVChangesSince("todos", pageNext, 2)
	if err != nil {
		t.Fatalf("续拉: %v", err)
	}
	if len(rest) != 1 {
		t.Errorf("续拉条数 = %d，期望 1", len(rest))
	}
	if restNext != all[2].Seq {
		t.Errorf("未截断续拉水位 = %d，期望全局最大 %d", restNext, all[2].Seq)
	}

	// 删除标记（task_id 传 0 落 NULL）。
	del, _, err := s.CalDAVChangesSince("events", 0, 0)
	if err != nil || len(del) != 1 || !del[0].Deleted {
		t.Errorf("删除标记 = %+v err=%v", del, err)
	}
}

// TestExtrasJSONHelpers nullable*/json*/nonNil* 的边界口径。
func TestExtrasJSONHelpers(t *testing.T) {
	zero := int64(0)
	pos := int64(7)
	if nullableTaskID(0) != nil {
		t.Error("task_id<=0 应落 NULL")
	}
	if nullableTaskID(5) != any(int64(5)) {
		t.Error("task_id>0 原样")
	}
	if nullableInt64(nil) != nil || nullableInt64(&zero) != nil {
		t.Error("nullableInt64 nil/非正 应 NULL")
	}
	if nullableInt64(&pos) != any(int64(7)) {
		t.Error("nullableInt64 正数原样")
	}
	ni := 0
	if nullableInt(nil) != nil {
		t.Error("nullableInt(nil)")
	}
	if nullableInt(&ni) != any(0) {
		t.Error("nullableInt 指针为 0 也保留（偏移 0 合法）")
	}
	blank := "  "
	if nullableStrPtr(nil) != nil || nullableStrPtr(&blank) != nil {
		t.Error("nullableStrPtr 空白应 NULL")
	}
	ok := " x "
	if nullableStrPtr(&ok) != any("x") {
		t.Errorf("nullableStrPtr 应 trim，得到 %v", nullableStrPtr(&ok))
	}

	if got := jsonStrings(`["a","b"]`); len(got) != 2 || got[1] != "b" {
		t.Errorf("jsonStrings = %v", got)
	}
	if got := jsonStrings("  "); got == nil || len(got) != 0 {
		t.Error("空白应得空切片")
	}
	if got := jsonStrings("not-json"); got == nil || len(got) != 0 {
		t.Error("坏 JSON 应兜底空切片")
	}
	if got := jsonInts("[1,2]"); len(got) != 2 || got[0] != 1 {
		t.Errorf("jsonInts = %v", got)
	}
	if got := jsonInt64s("[3]"); len(got) != 1 || got[0] != 3 {
		t.Errorf("jsonInt64s = %v", got)
	}
	if got := jsonInt64s(""); got == nil || len(got) != 0 {
		t.Error("jsonInt64s 空白")
	}

	if nonNilInts(nil) == nil || len(nonNilInts(nil)) != 0 {
		t.Error("nonNilInts(nil) 应为空切片")
	}
	if nonNilInt64s(nil) == nil || len(nonNilInt64s(nil)) != 0 {
		t.Error("nonNilInt64s(nil) 应为空切片")
	}
	if nonNilStrings(nil) == nil || len(nonNilStrings(nil)) != 0 {
		t.Error("nonNilStrings(nil) 应为空切片")
	}
	v := []int{1}
	if nonNilInts(v)[0] != 1 {
		t.Error("非空原样返回")
	}
}
