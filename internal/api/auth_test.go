package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/store"
)

// TestGateDisabled 未启用口令：全放行、Enabled 为假。
func TestGateDisabled(t *testing.T) {
	g := newGate("  ")
	if g.Enabled() {
		t.Error("空 token 不应启用")
	}
	if g.Enabled() != (&gate{}).Enabled() {
		t.Error("零值与空 token 口径应一致")
	}
	// 未启用时 wrap 原样返回。
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("inner"))
	})
	h := g.wrap(inner)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/anything", nil))
	if w.Code != http.StatusOK || w.Body.String() != "inner" {
		t.Errorf("未启用口令应直通: %d %q", w.Code, w.Body.String())
	}
}

// TestGateMatchAndAccept 口令比对与凭据提取优先级。
func TestGateMatchAndAccept(t *testing.T) {
	g := newGate("right-pw")
	if !g.Enabled() {
		t.Fatal("应启用")
	}
	if !g.match("right-pw") {
		t.Error("正确口令应通过")
	}
	if g.match("") || g.match("wrong") {
		t.Error("空/错误口令应拒绝")
	}
	// 零值 gate 永远拒绝。
	var zero *gate
	if zero.match("x") {
		t.Error("nil gate 应拒绝")
	}

	// credential 优先级：query > cookie > Basic(password) > Bearer。
	r := httptest.NewRequest("GET", "/api/x?token=q", nil)
	if got := credential(r); got != "q" {
		t.Errorf("query token = %q", got)
	}
	r.AddCookie(&http.Cookie{Name: authCookie, Value: "cookie-pw"})
	if got := credential(r); got != "q" {
		t.Errorf("query 应优先于 cookie, got %q", got)
	}
	r = httptest.NewRequest("GET", "/api/x", nil)
	r.AddCookie(&http.Cookie{Name: authCookie, Value: "cookie-pw"})
	if got := credential(r); got != "cookie-pw" {
		t.Errorf("cookie token = %q", got)
	}
	// Basic：cookie 优先于 Basic，因此要用干净请求验证 Basic 分支。
	r = httptest.NewRequest("GET", "/api/x", nil)
	r.SetBasicAuth("user", "basic-pw")
	if got := credential(r); got != "basic-pw" {
		t.Errorf("Basic 应取 password 字段, got %q", got)
	}
	// 有 cookie 时 cookie 优先于 Basic。
	r.AddCookie(&http.Cookie{Name: authCookie, Value: "cookie-pw"})
	if got := credential(r); got != "cookie-pw" {
		t.Errorf("cookie 应优先于 Basic, got %q", got)
	}
	r = httptest.NewRequest("GET", "/api/x", nil)
	r.Header.Set("Authorization", "Bearer bear")
	if got := credential(r); got != "bear" {
		t.Errorf("Bearer = %q", got)
	}
	r = httptest.NewRequest("GET", "/api/x", nil)
	if got := credential(r); got != "" {
		t.Errorf("无凭据 = %q", got)
	}

	// accept 组合。
	r = httptest.NewRequest("GET", "/api/x?token=right-pw", nil)
	if !g.accept(r) {
		t.Error("正确 query token 应 accept")
	}
	r = httptest.NewRequest("GET", "/api/x?token=wrong", nil)
	if g.accept(r) {
		t.Error("错误 token 应拒绝")
	}
}

// TestGateWrapEnforced 启用口令后的分流：直通、401、429、登录页、Cookie 补种。
func TestGateWrapEnforced(t *testing.T) {
	g := newGate("secret-pw")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h := g.wrap(inner)

	// 未登录访问 API → 401 JSON。
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("API 未登录 code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "需要访问口令") {
		t.Errorf("body = %s", w.Body.String())
	}

	// 健康检查免鉴权。
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Errorf("health code = %d", w.Code)
	}
	// 登录接口免鉴权。
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`)))
	if w.Code == http.StatusUnauthorized && strings.Contains(w.Body.String(), "需要访问口令") {
		t.Errorf("登录接口不应被门挡住: %d %s", w.Code, w.Body.String())
	}

	// 带正确 query token → 放行并补种 Cookie。
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/tasks?token=secret-pw", nil))
	if w.Code != http.StatusOK {
		t.Errorf("正确 token code = %d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	var planted bool
	for _, c := range cookies {
		if c.Name == authCookie && c.Value == "secret-pw" {
			planted = true
		}
	}
	if !planted {
		t.Error("query 通过后应补种 Cookie")
	}

	// 静态资源未登录 → 登录页 HTML 401。
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("静态未登录 code = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q，应为登录页", ct)
	}
	if !strings.Contains(w.Body.String(), "慎始") {
		t.Error("应返回登录页内容")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("登录页不应被缓存")
	}

	// 累积失败到上限 → 429。限速 key 取 RemoteAddr 的直连地址，必须对上。
	ip := "10.0.0.1"
	for i := 0; i < authFailMax; i++ {
		g.noteFail(ip)
	}
	w = httptest.NewRecorder()
	req429 := httptest.NewRequest("GET", "/api/tasks", nil)
	req429.RemoteAddr = ip + ":54321"
	h.ServeHTTP(w, req429)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("超限 code = %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("429 应带 Retry-After")
	}
	g.resetFails(ip)
}

// TestFailWindow 失败计数的窗口与过期。
func TestFailWindow(t *testing.T) {
	g := newGate("pw")
	ip := "9.9.9.9"
	if g.blocked(ip) {
		t.Error("无记录不应 blocked")
	}
	// 过期窗口的旧记录不计数。
	g.fails[ip] = &failBucket{count: 99, first: time.Now().Add(-authFailWin - time.Minute)}
	if g.blocked(ip) {
		t.Error("过期窗口应清掉并放行")
	}
	if _, ok := g.fails[ip]; ok {
		t.Error("过期记录应被删除")
	}
	// 窗口内累计。
	for i := 0; i < authFailMax-1; i++ {
		g.noteFail(ip)
	}
	if g.blocked(ip) {
		t.Error("差一次上限不应 blocked")
	}
	g.noteFail(ip)
	if !g.blocked(ip) {
		t.Error("达到上限应 blocked")
	}
	g.resetFails(ip)
	if g.blocked(ip) {
		t.Error("reset 后应放行")
	}
	// 重新开窗：距首次超窗后重新记为 1。
	g.fails[ip] = &failBucket{count: 5, first: time.Now().Add(-authFailWin - time.Second)}
	g.noteFail(ip)
	if g.fails[ip].count != 1 {
		t.Errorf("超窗后应重开计数, got %d", g.fails[ip].count)
	}
}

// TestAuthHandlers 状态、登录、登出接口。
func TestAuthHandlers(t *testing.T) {
	// 未启用口令的服务。
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	if err := s.authStatus(w, httptest.NewRequest("GET", "/api/auth/status", nil)); err != nil {
		t.Fatalf("authStatus: %v", err)
	}
	var st struct {
		Required      bool `json:"required"`
		Authenticated bool `json:"authenticated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if st.Required {
		t.Error("未启用口令 required 应为假")
	}

	// logout 未启用也成功，且清 Cookie。
	w = httptest.NewRecorder()
	if err := s.logout(w, httptest.NewRequest("POST", "/api/auth/logout", nil)); err != nil {
		t.Fatalf("logout: %v", err)
	}
	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == authCookie && c.MaxAge < 0 {
			found = true
		}
	}
	if !found {
		t.Error("logout 应清 Cookie（MaxAge<0）")
	}

	// 启用口令的服务：登录失败 → 401；成功 → Set-Cookie；节流 → 429。
	s2 := New(newTestStoreServer(t), "login-pw")
	w = httptest.NewRecorder()
	if err := s2.login(w, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"token":"wrong"}`))); err != nil {
		t.Fatalf("login(错): %v", err)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("错口令 code = %d", w.Code)
	}

	w = httptest.NewRecorder()
	if err := s2.login(w, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"token":"login-pw"}`))); err != nil {
		t.Fatalf("login(对): %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("正确口令 code = %d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	okCookie := false
	for _, c := range cookies {
		if c.Name == authCookie && c.Value == "login-pw" && c.HttpOnly {
			okCookie = true
		}
	}
	if !okCookie {
		t.Error("登录成功应下发 HttpOnly Cookie")
	}

	// authStatus 在启用后：required=true，带正确凭据 authenticated=true。
	w = httptest.NewRecorder()
	if err := s2.authStatus(w, httptest.NewRequest("GET", "/api/auth/status?token=login-pw", nil)); err != nil {
		t.Fatalf("authStatus(2): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("解析(2): %v", err)
	}
	if !st.Required || !st.Authenticated {
		t.Errorf("status = %+v", st)
	}

	// 打满失败 → 登录 429。
	ip := "203.0.113.7"
	for i := 0; i < authFailMax; i++ {
		s2.gate.noteFail(ip)
	}
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"token":"login-pw"}`))
	req.RemoteAddr = "203.0.113.7:54321"
	if err := s2.login(w, req); err != nil {
		t.Fatalf("login(节流): %v", err)
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("节流 code = %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("429 应带 Retry-After")
	}

	// 坏 JSON 登录体。
	s3 := New(newTestStoreServer(t), "pw3")
	w = httptest.NewRecorder()
	if err := s3.login(w, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{`))); err == nil {
		t.Error("坏 JSON 应报错")
	}
}

// TestClientIPAndTLS 直连地址解析与 TLS 判定。
func TestClientIPAndTLS(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.9:40000"
	if got := clientIP(r); got != "192.168.1.9" {
		t.Errorf("clientIP = %q", got)
	}
	// 无端口时原样返回（SplitHostPort 失败分支）。
	r.RemoteAddr = "192.168.1.9"
	if got := clientIP(r); got != "192.168.1.9" {
		t.Errorf("clientIP(无端口) = %q", got)
	}
	// 伪造头不采信。
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	if got := clientIP(r); got != "192.168.1.9" {
		t.Errorf("clientIP 不应采信伪造头, got %q", got)
	}

	// TLS：直连或反代头。
	if requestIsTLS(httptest.NewRequest("GET", "/", nil)) {
		t.Error("无 TLS 应为假")
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Proto", "HTTPS")
	if !requestIsTLS(r) {
		t.Error("X-Forwarded-Proto=https 应为真（大小写不敏感）")
	}

	// setAuthCookie 的 Secure 随 TLS 切换。
	w := httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	setAuthCookie(w, r, "tok")
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Secure {
		t.Error("非 TLS 下 Secure 应为假，否则浏览器不存")
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	setAuthCookie(w, r, "tok")
	cookies = w.Result().Cookies()
	if len(cookies) == 0 || !cookies[0].Secure {
		t.Error("TLS 下 Secure 应为真")
	}
	if cookies[0].MaxAge != 30*24*3600 {
		t.Errorf("Cookie 有效期 = %d", cookies[0].MaxAge)
	}
}

// TestWriteLoginPage 登录页头。
func TestWriteLoginPage(t *testing.T) {
	w := httptest.NewRecorder()
	writeLoginPage(w)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<!DOCTYPE html>") {
		t.Error("应返回完整 HTML 页")
	}
}

// newTestStoreServer 再开一个独立临时库，供需要不同口令配置的服务复用。
func newTestStoreServer(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	return st
}
