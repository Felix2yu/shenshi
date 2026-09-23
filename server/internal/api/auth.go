package api

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 访问口令鉴权。
//
// 「慎始」默认是单用户、本地自用的服务；一旦放到公网服务器上供多端访问，
// 就必须有一道门。这里刻意只做一件事：一个长期口令（`SHENSHI_TOKEN`），
// 凭口令换取一个 HttpOnly Cookie，之后所有请求凭 Cookie 放行。
//
// 设计取舍：
//   - 不引入账号体系与 JWT：单用户场景下账号表、密码哈希、令牌轮换都是负担，
//     一把足够长的口令 + 常量时间比较 + 失败节流，安全性已经够用。
//   - Cookie 而非每次带 Header：这样前端静态资源也能被同一道门保护，
//     未登录时直接看到登录页，而不是加载了整个应用再满屏报错。
//   - `/api/health` 与 `/api/auth/*` 免鉴权：前者供容器健康检查，后者是登录本身。

const (
	authCookie   = "shenshi_token"
	authFailMax  = 8               // 窗口内允许的失败次数
	authFailWin  = 5 * time.Minute // 失败计数窗口
	authFailWait = 200 * time.Millisecond
)

// gate 持有口令与失败计数。zero 值（token 为空）表示未启用鉴权。
type gate struct {
	token string

	mu    sync.Mutex
	fails map[string]*failBucket
}

type failBucket struct {
	count int
	first time.Time
}

func newGate(token string) *gate {
	return &gate{token: strings.TrimSpace(token), fails: map[string]*failBucket{}}
}

// Enabled 表示是否配置了口令。
func (g *gate) Enabled() bool { return g != nil && g.token != "" }

// wrap 返回带鉴权的处理器。未启用口令时原样返回 next。
func (g *gate) wrap(next http.Handler) http.Handler {
	if !g.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// 健康检查供容器探活；登录接口本身当然不能要求先登录。
		if p == "/api/health" || strings.HasPrefix(p, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		if g.accept(r) {
			g.markCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}

		ip := clientIP(r)
		if g.blocked(ip) {
			w.Header().Set("Retry-After", "300")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "口令错误次数过多，请稍后再试"})
			return
		}
		// 有凭据但不对 → 记为一次失败；限速减缓暴力猜测。
		if credential(r) != "" {
			g.noteFail(ip)
			time.Sleep(authFailWait)
		}

		if strings.HasPrefix(p, "/api/") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "需要访问口令"})
			return
		}
		writeLoginPage(w)
	})
}

// accept 判断请求是否携带了正确口令。
func (g *gate) accept(r *http.Request) bool {
	return g.match(credential(r))
}

// match 常量时间比对口令，避免通过响应时间逐字节试探。
func (g *gate) match(token string) bool {
	if !g.Enabled() || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(g.token)) == 1
}

// credential 依次从查询串、Cookie、Authorization 头里取口令。
// 查询串优先，便于用 `curl '...?token=xxx'` 或浏览器直接贴上带口令的链接；
// 走查询串的请求会顺带补一个 Cookie（见 markCookie）。
func credential(r *http.Request) string {
	if v := r.URL.Query().Get("token"); v != "" {
		return v
	}
	if c, err := r.Cookie(authCookie); err == nil && c.Value != "" {
		return c.Value
	}
	// HTTP Basic：Apple 的日历与提醒事项只认这一种，账户设置界面里根本没有
	// 填自定义请求头的地方，因此把 password 字段当口令、忽略 username。
	if _, pass, ok := r.BasicAuth(); ok && pass != "" {
		return pass
	}
	if h := r.Header.Get("Authorization"); h != "" {
		if after, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// markCookie 在凭查询串通过校验后补种 Cookie，让后续请求与静态资源也能放行。
func (g *gate) markCookie(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") == "" {
		return
	}
	if _, err := r.Cookie(authCookie); err == nil {
		return
	}
	setAuthCookie(w, r, g.token)
}

func setAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsTLS(r),
		MaxAge:   30 * 24 * 3600, // 一个月；单用户自用，不必频繁重输
	})
}

func clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     authCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// requestIsTLS 判断外部访问是否走 HTTPS。容器里通常在反向代理后终止 TLS，
// 因此同时看 X-Forwarded-Proto。
func requestIsTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	// 限速 key 只认直连地址：X-Forwarded-For 可以被客户端随意伪造，
	// 用它做 key 等于把限速开关交给攻击者。真实来源需要时看访问日志。
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// blocked 判断该来源是否已超出失败上限。顺带清理过期窗口。
func (g *gate) blocked(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, ok := g.fails[ip]
	if !ok {
		return false
	}
	if time.Since(b.first) > authFailWin {
		delete(g.fails, ip)
		return false
	}
	return b.count >= authFailMax
}

func (g *gate) noteFail(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	b, ok := g.fails[ip]
	if !ok || now.Sub(b.first) > authFailWin {
		g.fails[ip] = &failBucket{count: 1, first: now}
		return
	}
	b.count++
}

// resetFails 登录成功后清空该来源的失败计数。
func (g *gate) resetFails(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, ip)
}

// ---------- 登录页 ----------
//
// 未登录时访问静态资源，直接返回一张内联的登录页，而不是把整个前端加载进来再让接口报错。
// 这张页面不依赖前端构建产物，因此不会因为资源加载而泄露应用结构。

func writeLoginPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprint(w, loginPageHTML)
}

const loginPageHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>慎始 · 入门</title>
<style>
  :root { color-scheme: light dark; --ink:#2b2926; --ink2:#6f6a62; --line:#e0dcd4; --paper:#f7f4ee; --seal:#b4553d; }
  @media (prefers-color-scheme: dark) {
    :root { --ink:#eae6de; --ink2:#9c968c; --line:#3a3733; --paper:#1c1a18; --seal:#c9644a; }
  }
  * { box-sizing: border-box; }
  body { margin:0; min-height:100dvh; display:grid; place-items:center; background:var(--paper); color:var(--ink);
         font: 14px/1.6 "Songti SC", "Noto Serif SC", system-ui, -apple-system, "PingFang SC", sans-serif; }
  main { width: min(92vw, 22rem); }
  .seal { width:44px; height:44px; border-radius:10px; background:var(--seal); color:#fff; display:grid; place-items:center;
          font-size:20px; margin:0 auto 1rem; }
  h1 { font-size:17px; font-weight:600; letter-spacing:.14em; text-align:center; margin:0 0 .35rem; }
  p.hint { text-align:center; color:var(--ink2); font-size:12px; margin:0 0 1.4rem; }
  label { display:block; font-size:11.5px; color:var(--ink2); margin-bottom:.4rem; }
  input { width:100%; padding:.6rem .7rem; font-size:14px; color:var(--ink); background:transparent;
          border:1px solid var(--line); border-radius:10px; outline:none; }
  input:focus { border-color:var(--seal); }
  button { width:100%; margin-top:.9rem; padding:.62rem; font-size:14px; font-family:inherit; cursor:pointer;
           color:#fff; background:var(--seal); border:0; border-radius:10px; }
  button:disabled { opacity:.55; cursor: default; }
  .err { min-height:1.2rem; margin-top:.7rem; text-align:center; font-size:12px; color:#b4553d; }
  footer { margin-top:1.6rem; text-align:center; font-size:11px; color:var(--ink2); letter-spacing:.08em; }
</style>
</head>
<body>
<main>
  <div class="seal">慎</div>
  <h1>慎始而敬终</h1>
  <p class="hint">请输入访问口令</p>
  <form id="f">
    <label for="t">访问口令</label>
    <input id="t" type="password" autocomplete="current-password" autofocus required>
    <button type="submit" id="b">进入</button>
    <div class="err" id="e"></div>
  </form>
  <footer>口令由服务端环境变量 SHENSHI_TOKEN 设定</footer>
</main>
<script>
  const form = document.getElementById('f'), input = document.getElementById('t');
  const btn = document.getElementById('b'), err = document.getElementById('e');
  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    err.textContent = '';
    btn.disabled = true;
    try {
      const resp = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: input.value }),
      });
      if (resp.ok) { location.replace('/'); return; }
      const data = await resp.json().catch(() => ({}));
      err.textContent = data.error || '口令不正确';
    } catch (e) {
      err.textContent = '无法连接到服务';
    }
    btn.disabled = false;
    input.select();
  });
</script>
</body>
</html>
`
