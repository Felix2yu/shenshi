package caldav

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCollectionOf 集合识别：带不带尾斜杠都认，未知路径报错。
func TestCollectionOf(t *testing.T) {
	if k, err := collectionOf(EventsPath); err != nil || k != CollectionEvents {
		t.Errorf("collectionOf(events) = %q, %v", k, err)
	}
	if k, err := collectionOf("/caldav/user/calendars/shenshi"); err != nil || k != CollectionEvents {
		t.Errorf("无尾斜杠的 events 应放行，得到 %q, %v", k, err)
	}
	if k, err := collectionOf(TasksPath); err != nil || k != CollectionTasks {
		t.Errorf("collectionOf(todos) = %q, %v", k, err)
	}
	if _, err := collectionOf("/caldav/user/calendars/other/"); err == nil {
		t.Error("未知集合应报错")
	}
	if _, err := collectionOf(PrincipalPath); err == nil {
		t.Error("主体路径不是集合")
	}
}

// TestSyncToken 生成与解析互为逆操作；坏令牌一律拒绝（不能因此炸 500）。
func TestSyncToken(t *testing.T) {
	for _, seq := range []int64{0, 1, 99999} {
		tok := formatSyncToken(seq)
		got, ok := parseSyncToken(tok)
		if !ok || got != seq {
			t.Errorf("往返 seq=%d → tok=%q → (%d,%v)", seq, tok, got, ok)
		}
	}
	if _, ok := parseSyncToken("  ss1-42  "); !ok {
		t.Error("前后空白应被 Trim 掉")
	}
	for _, bad := range []string{
		"",
		"ss1-",    // 缺序号
		"ss1-abc", // 非数字
		"ss1--5",  // 负序号
		"other-1", // 别家前缀
		"1",       // 裸序号
	} {
		if _, ok := parseSyncToken(bad); ok {
			t.Errorf("parseSyncToken(%q) 应失败", bad)
		}
	}
}

// TestParsePropNames 从 PROPPATCH 体里取出属性名：提取、去重、坏体返回 nil。
func TestParsePropNames(t *testing.T) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<D:propertyupdate xmlns:D="DAV:">
  <D:set><D:prop><D:displayname/><D:calendar-color/></D:prop></D:set>
  <D:set><D:prop><D:displayname/></D:prop></D:set>
</D:propertyupdate>`
	names := parsePropNames([]byte(body))
	if len(names) != 2 {
		t.Fatalf("应去重后剩 2 个属性名，得到 %v", names)
	}
	if names[0] != "displayname" || names[1] != "calendar-color" {
		t.Errorf("属性名 = %v", names)
	}

	if got := parsePropNames([]byte("not xml")); got != nil {
		t.Errorf("坏 XML 应返回 nil，得到 %v", got)
	}
}

// TestXmlEscape href / 属性值里的特殊字符不能破坏 multistatus 结构。
func TestXmlEscape(t *testing.T) {
	got := xmlEscape(`a<b>&"c"'`)
	want := "a&lt;b&gt;&amp;&#34;c&#34;&#39;"
	if got != want {
		t.Errorf("xmlEscape = %q，期望 %q", got, want)
	}
}

// TestIsCalDAVPath 最外层据此决定要不要打 DAV 头。
func TestIsCalDAVPath(t *testing.T) {
	yes := []string{"/.well-known/caldav", "/caldav", "/caldav/", "/caldav/user/calendars/shenshi/"}
	no := []string{"/api/tasks", "/caldavfoo", "/.well-known/webfinger", "/"}
	for _, p := range yes {
		if !IsCalDAVPath(p) {
			t.Errorf("IsCalDAVPath(%q) 应为 true", p)
		}
	}
	for _, p := range no {
		if IsCalDAVPath(p) {
			t.Errorf("IsCalDAVPath(%q) 应为 false", p)
		}
	}
}

// TestWellKnown 302（Apple 跟随得比 308 好）+ DAV 能力头。
func TestWellKnown(t *testing.T) {
	r := httptest.NewRequest("GET", "/.well-known/caldav", nil)
	w := httptest.NewRecorder()
	WellKnown(w, r)
	if w.Code != 302 {
		t.Errorf("well-known 状态码 = %d，库默认 308 对 Apple 更差", w.Code)
	}
	if got := w.Header().Get("DAV"); got != DavHeader {
		t.Errorf("DAV 头 = %q，期望 %q", got, DavHeader)
	}
	if loc := w.Header().Get("Location"); loc != PrincipalPath {
		t.Errorf("Location = %q", loc)
	}
}

// TestWritePropPatchOK 207 Multi-Status：请求里的属性逐个报 200，href 带 DAV 头。
func TestWritePropPatchOK(t *testing.T) {
	body := `<D:propertyupdate xmlns:D="DAV:"><D:set><D:prop><D:displayname/><X:color xmlns:X="x"/></D:prop></D:set></D:propertyupdate>`
	r := httptest.NewRequest("PROPPATCH", "/caldav/user/calendars/shenshi/", strings.NewReader(body))
	w := httptest.NewRecorder()
	writePropPatchOK(w, r)

	if w.Code != 207 {
		t.Fatalf("状态码 = %d，期望 207", w.Code)
	}
	out := w.Body.String()
	for _, want := range []string{"<D:response>", "<displayname/>", "<color/>", "HTTP/1.1 200 OK", "/caldav/user/calendars/shenshi/"} {
		if !strings.Contains(out, want) {
			t.Errorf("207 响应缺少 %q：\n%s", want, out)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/xml") {
		t.Errorf("Content-Type = %q", ct)
	}
}
