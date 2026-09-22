package api

import (
	"net/http"
	"time"
)

// authStatus 汇报是否需要口令、以及当前请求是否已通过。两者都只是布尔值，
// 因此可以在未登录时安全返回。
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, map[string]any{
		"required":      s.gate.Enabled(),
		"authenticated": s.gate.accept(r),
	})
	return nil
}

// login 用口令换取 Cookie。口令错误返回 401（而非 400），并计入失败节流。
func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeOptional(w, r, &body); err != nil {
		return err
	}
	ip := clientIP(r)
	if s.gate.blocked(ip) {
		w.Header().Set("Retry-After", "300")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "口令错误次数过多，请稍后再试"})
		return nil
	}
	if !s.gate.match(body.Token) {
		s.gate.noteFail(ip)
		time.Sleep(authFailWait)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "口令不正确"})
		return nil
	}
	s.gate.resetFails(ip)
	setAuthCookie(w, r, s.gate.token)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

// logout 清掉 Cookie。未启用口令时也允许调用，方便前端统一处理。
func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	clearAuthCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}
