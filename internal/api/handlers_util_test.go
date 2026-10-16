package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/store"
)

// TestWriteErrorStatus 错误 → 状态码映射；内部错误不回传细节。
func TestWriteErrorStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		body string
	}{
		{"校验失败回 400", store.ValidationError{Msg: "标题不能为空"}, 400, "标题不能为空"},
		{"禁止操作回 403", store.ForbiddenError{Msg: "收集箱不可删"}, 403, "收集箱不可删"},
		{"不存在回 404", store.ErrNotFound, 404, "记录不存在"},
		{"未知错误回 500", io.ErrUnexpectedEOF, 500, "服务端错误"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, c.err)
			if w.Code != c.code {
				t.Errorf("code = %d，期望 %d", w.Code, c.code)
			}
			if !strings.Contains(w.Body.String(), c.body) {
				t.Errorf("body = %s，应含 %q", w.Body.String(), c.body)
			}
			// 500 不得泄漏内部错误文本。
			if c.code == 500 && strings.Contains(w.Body.String(), "unexpected") {
				t.Error("内部错误细节不应回传客户端")
			}
		})
	}
}

// TestDecodeAndOptional 请求体解析：必填 vs 允许空体。
func TestDecodeAndOptional(t *testing.T) {
	// 正常解析。
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"a":1}`))
	var v struct {
		A int `json:"a"`
	}
	if err := decode(httptest.NewRecorder(), r, &v); err != nil || v.A != 1 {
		t.Errorf("decode: v=%+v err=%v", v, err)
	}

	// 坏 JSON → ValidationError。
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{bad`))
	if err := decode(httptest.NewRecorder(), r, &v); err == nil {
		t.Error("坏 JSON 应报错")
	} else {
		var ve store.ValidationError
		if !asValidationError(err, &ve) {
			t.Errorf("应为 ValidationError，得到 %T", err)
		}
	}

	// 空体：decode 拒绝，decodeOptional 放行且保留零值。
	r = httptest.NewRequest("POST", "/", nil)
	if err := decode(httptest.NewRecorder(), r, &v); err == nil {
		t.Error("decode 空体应报错")
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader("  \n "))
	v.A = 0
	if err := decodeOptional(httptest.NewRecorder(), r, &v); err != nil || v.A != 0 {
		t.Errorf("decodeOptional 空体: v=%+v err=%v", v, err)
	}

	// decodeOptional 正常体照常解析。
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"a":7}`))
	if err := decodeOptional(httptest.NewRecorder(), r, &v); err != nil || v.A != 7 {
		t.Errorf("decodeOptional 有体: v=%+v err=%v", v, err)
	}

	// decodeOptional 坏 JSON 仍报错。
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{oops`))
	if err := decodeOptional(httptest.NewRecorder(), r, &v); err == nil {
		t.Error("坏 JSON 应报错")
	}
}

func asValidationError(err error, target *store.ValidationError) bool {
	if ve, ok := err.(store.ValidationError); ok {
		*target = ve
		return true
	}
	return false
}

// TestDecodeLimited 上限可调的备份体解析。
func TestDecodeLimited(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"version":1}`))
	var b store.ExportBundle
	if err := decodeLimited(w, r, &b, 1024); err != nil || b.Version != 1 {
		t.Errorf("decodeLimited: v=%d err=%v", b.Version, err)
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{bad`))
	if err := decodeLimited(w, r, &b, 1024); err == nil {
		t.Error("坏 JSON 应报错")
	}
}

// TestPathID 路径参数：非数字与非正整数一律拒绝。
func TestPathID(t *testing.T) {
	mk := func(v string) *http.Request {
		r := httptest.NewRequest("GET", "/api/x", nil)
		r.SetPathValue("id", v)
		return r
	}
	if id, err := pathID(mk("42"), "id"); err != nil || id != 42 {
		t.Errorf("pathID(42) = %d, %v", id, err)
	}
	for _, bad := range []string{"", "abc", "0", "-3", "1.5"} {
		if _, err := pathID(mk(bad), "id"); err == nil {
			t.Errorf("pathID(%q) 应报错", bad)
		}
	}
}

// TestTodayAndLocalOrigin 日期口径与本机 Origin 判定。
func TestTodayAndLocalOrigin(t *testing.T) {
	want := time.Now().Format("2006-01-02")
	if got := today(); got != want {
		t.Errorf("today = %s，期望 %s", got, want)
	}

	cases := map[string]bool{
		"":                               false,
		"http://localhost:5173":          true,
		"http://127.0.0.1:3000":          true,
		"http://[::1]:8080":              true,
		"https://evil.example.com":       false,
		"http://localhost.evil.com":      false, // 前缀必须含端口冒号
		"http://127.0.0.1":               false, // 无端口不算本机开发服务
		"http://localhost:5173.evil.com": true,  // 实现为前缀匹配：已知宽松口径
	}
	for origin, want := range cases {
		if got := isLocalOrigin(origin); got != want {
			t.Errorf("isLocalOrigin(%q) = %v，期望 %v", origin, got, want)
		}
	}
}

// TestRedactURI 日志脱敏：早退条件只认小写 token=/password=/secret=，
// 进入循环后才按小写 key 名脱敏（含 key）。PASSWORD/key 单独出现时因早退原样返回。
func TestRedactURI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api/tasks?limit=10", "/api/tasks?limit=10"},
		{"/api/x?token=abc", "/api/x?token=***"},
		{"/api/x?secret=abc", "/api/x?secret=***"},
		{"/api/x", "/api/x"}, // 无查询串
		{"/api/x?token=", "/api/x?token=***"},
		// 早退条件大小写敏感：大写 PASSWORD 单独出现不触发脱敏。
		{"/api/x?PASSWORD=abc&limit=1", "/api/x?PASSWORD=abc&limit=1"},
		// key 单独出现不触发早退条件。
		{"/api/x?key=abc", "/api/x?key=abc"},
		// 已进入脱敏路径后，key 名按小写比对，PASSWORD/key 一并抹掉。
		{"/api/x?token=1&PASSWORD=2", "/api/x?token=***&PASSWORD=***"},
		{"/api/x?token=1&key=2", "/api/x?token=***&key=***"},
	}
	for _, c := range cases {
		if got := redactURI(c.in); got != c.want {
			t.Errorf("redactURI(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestWriteJSONNil nil 载荷只发状态头不发体。
func TestWriteJSONNil(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusNoContent, nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("code = %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("nil 载荷不应有 body: %q", w.Body.String())
	}

	w2 := httptest.NewRecorder()
	writeJSON(w2, http.StatusOK, map[string]int{"a": 1})
	if !strings.Contains(w2.Body.String(), `"a":1`) {
		t.Errorf("body = %s", w2.Body.String())
	}
	if ct := w2.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

// TestStrWithDefault 空串回落。
func TestStrWithDefault(t *testing.T) {
	if strWithDefault("", "3") != "3" || strWithDefault("7", "3") != "7" {
		t.Error("strWithDefault 口径错误")
	}
}

// TestStatusWriterAndHookWriter statusWriter.code 记录最后一次调用（无条件覆盖）；
// hookWriter 每次 WriteHeader 都回调。
func TestStatusWriterAndHookWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec, code: 200}
	sw.WriteHeader(404)
	if sw.code != 404 {
		t.Errorf("code = %d，期望 404", sw.code)
	}
	if rec.Code != 404 {
		t.Errorf("底层 code = %d", rec.Code)
	}
	// 第二次调用会覆盖记录值；底层响应头仍以第一次为准（HTTP 语义）。
	sw.WriteHeader(500)
	if sw.code != 500 {
		t.Errorf("后写应覆盖记录值, got %d", sw.code)
	}
	if rec.Code != 404 {
		t.Errorf("底层首次 404 不应被顶掉, got %d", rec.Code)
	}

	// hookWriter：每次 WriteHeader 触发 hook，401 时补挑战头。
	rec2 := httptest.NewRecorder()
	sw2 := &statusWriter{ResponseWriter: rec2, code: 200}
	seen := 0
	hw := &hookWriter{ResponseWriter: sw2, hook: func(code int) {
		seen++
		if code == http.StatusUnauthorized {
			sw2.Header().Set("WWW-Authenticate", "Basic")
		}
	}}
	hw.WriteHeader(200)
	hw.WriteHeader(401)
	if seen != 2 {
		t.Errorf("hook 触发次数 = %d", seen)
	}
	if rec2.Header().Get("WWW-Authenticate") == "" {
		t.Error("401 应补 WWW-Authenticate")
	}
}

// TestQueryHelpersMisc queryStr/queryInt64 的空值与坏值。
func TestQueryHelpersMisc(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/x?n=notanumber", nil)
	if queryInt64(r, "n") != nil {
		t.Error("坏数字应得 nil")
	}
	if queryStr(r, "missing") != nil {
		t.Error("缺参应得 nil")
	}
	s := queryStr(r, "missing")
	if s != nil {
		t.Errorf("queryStr = %v", s)
	}
}

// TestServeHTTPCORSAndDAVServeHTTP 外壳：本机 Origin 补 CORS，CalDAV 路径补 DAV 头。
func TestServeHTTPCORSAndDAV(t *testing.T) {
	s, _ := newTestServer(t)

	// 本机 Origin → CORS 头。
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	s.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("应允许携带凭据")
	}

	// 外部 Origin → 不补 CORS。
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	s.ServeHTTP(w, req)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("外部 Origin 不应拿到 CORS")
	}

	// OPTIONS 预检 → 204。
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("OPTIONS", "/api/health", nil))
	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS code = %d", w.Code)
	}

	// CalDAV 路径 OPTIONS → 带 Allow 头（Apple 靠它判断能力）。
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("OPTIONS", "/caldav/user/", nil))
	if w.Code != http.StatusNoContent {
		t.Errorf("DAV OPTIONS code = %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); !strings.Contains(allow, "PROPFIND") {
		t.Errorf("Allow = %q，应含 PROPFIND", allow)
	}

	// CalDAV 路径普通请求 → DAV 头常在（401 时客户端也看得到能力集）。
	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/caldav/user/", nil)
	req.SetBasicAuth("u", "pw")
	s.ServeHTTP(w, req)
	if w.Header().Get("DAV") == "" {
		t.Error("CalDAV 路径应带 DAV 头")
	}
}

// TestHealth 登录门未启用时健康检查直通。
func TestHealth(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/health", nil)
	// 经 root 包装，覆盖 gate.wrap 未启用分支。
	s.root.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("health code = %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("health 应有响应体")
	}
}
