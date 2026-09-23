// Package api 提供「慎始」的 HTTP 接口层。
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	shenshicaldav "github.com/yufei/shendu/server/internal/caldav"
	"github.com/yufei/shendu/server/internal/store"
)

// Server 持有依赖并对外暴露 http.Handler。
type Server struct {
	st     *store.Store
	mux    *http.ServeMux
	gate   *gate
	root   http.Handler
	hooks  *dispatcher
	auto   *store.AutoBackup
	caldav *shenshicaldav.Handler
}

// New 构造路由表。token 非空时启用访问口令鉴权（见 auth.go）。
func New(st *store.Store, token string) *Server {
	dav := shenshicaldav.NewHandler(st)
	s := &Server{
		st:     st,
		mux:    http.NewServeMux(),
		gate:   newGate(token),
		auto:   store.NewAutoBackup(st, log.Printf),
		caldav: dav,
	}
	s.routes()
	s.registerWebhooks()
	// 任务一变就记进 CalDAV 变更日志，增量同步才有得可查。
	st.OnEvent(dav.Backend().SyncHook())
	// root 在最外层套上鉴权，因此 API 与前端静态资源走同一道门。
	s.root = s.gate.wrap(s.mux)
	return s
}

// AutoBackup 返回自动备份器，供 main 启动与停止。
func (s *Server) AutoBackup() *store.AutoBackup { return s.auto }

// CalDAV 返回 CalDAV 处理器，供 main 在启动时补齐变更日志。
func (s *Server) CalDAV() *shenshicaldav.Handler { return s.caldav }

// ServeHTTP 实现 http.Handler，并附加日志与 CORS。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if origin := r.Header.Get("Origin"); isLocalOrigin(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	isDAV := shenshicaldav.IsCalDAVPath(r.URL.Path)
	if isDAV {
		// Dav 头要在最外层就设好：鉴权失败时（401）客户端也必须能看到能力集，
		// 否则 Apple 日历在账户设置阶段就判定「不支持 CalDAV」。
		w.Header().Set("DAV", shenshicaldav.DavHeader)
	}
	if r.Method == http.MethodOptions {
		if isDAV {
			w.Header().Set("Allow", shenshicaldav.AllowHeader)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	sw := &statusWriter{ResponseWriter: w, code: 200}
	out := http.ResponseWriter(sw)
	if isDAV {
		// 401 时补 Basic 挑战：Apple 客户端只认这一种认证方式，
		// 没有 WWW-Authenticate 它连口令框都不会弹。
		out = &hookWriter{ResponseWriter: sw, hook: func(code int) {
			if code == http.StatusUnauthorized {
				sw.Header().Set("WWW-Authenticate", `Basic realm="shenshi", charset="UTF-8"`)
			}
		}}
	}
	s.root.ServeHTTP(out, r)
	if strings.HasPrefix(r.URL.Path, "/api/") {
		log.Printf("%s %s -> %d (%s)", r.Method, redactURI(r.URL.RequestURI()), sw.code, time.Since(start).Round(time.Millisecond))
	}
}

// redactURI 把查询串里的口令类参数抹掉再进日志，避免 ?token= 落盘。
func redactURI(uri string) string {
	if !strings.Contains(uri, "token=") && !strings.Contains(uri, "password=") && !strings.Contains(uri, "secret=") {
		return uri
	}
	qi := strings.IndexByte(uri, '?')
	if qi < 0 {
		return uri
	}
	base, query := uri[:qi], uri[qi+1:]
	parts := strings.Split(query, "&")
	for i, p := range parts {
		if k, _, ok := strings.Cut(p, "="); ok {
			switch strings.ToLower(k) {
			case "token", "password", "secret", "key":
				parts[i] = k + "=***"
			}
		}
	}
	return base + "?" + strings.Join(parts, "&")
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// hookWriter 在真正写出状态码之前回调一次，用于补上「只有看到状态码才知道该不该加」的响应头。
type hookWriter struct {
	http.ResponseWriter
	hook func(code int)
}

func (w *hookWriter) WriteHeader(code int) {
	w.hook(code)
	w.ResponseWriter.WriteHeader(code)
}

// HandleStatic 挂载前端静态资源（由 main 注入，避免 api 包耦合 embed）。
func (s *Server) HandleStatic(h http.Handler) {
	s.mux.Handle("/", h)
}

func (s *Server) routes() {
	h := func(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if err := fn(w, r); err != nil {
				writeError(w, err)
			}
		}
	}

	s.mux.HandleFunc("GET /api/health", h(s.health))
	s.mux.HandleFunc("GET /api/bootstrap", h(s.bootstrap))

	s.mux.HandleFunc("GET /api/auth/status", h(s.authStatus))
	s.mux.HandleFunc("POST /api/auth/login", h(s.login))
	s.mux.HandleFunc("POST /api/auth/logout", h(s.logout))

	s.mux.HandleFunc("GET /api/tasks", h(s.listTasks))
	s.mux.HandleFunc("POST /api/tasks", h(s.createTask))
	s.mux.HandleFunc("POST /api/tasks/batch", h(s.batchTasks))
	s.mux.HandleFunc("POST /api/tasks/reorder", h(s.reorderTasks))
	s.mux.HandleFunc("POST /api/tasks/purge", h(s.purgeCompleted))
	s.mux.HandleFunc("GET /api/tasks/{id}", h(s.getTask))
	s.mux.HandleFunc("PATCH /api/tasks/{id}", h(s.updateTask))
	s.mux.HandleFunc("DELETE /api/tasks/{id}", h(s.deleteTask))
	s.mux.HandleFunc("POST /api/tasks/{id}/toggle", h(s.toggleTask))
	s.mux.HandleFunc("POST /api/tasks/{id}/skip", h(s.skipTask))
	s.mux.HandleFunc("POST /api/tasks/{id}/move", h(s.moveTask))
	s.mux.HandleFunc("POST /api/tasks/{id}/duplicate", h(s.duplicateTask))
	s.mux.HandleFunc("POST /api/tasks/{id}/subtasks", h(s.addSubtask))
	s.mux.HandleFunc("PATCH /api/subtasks/{id}", h(s.updateSubtask))
	s.mux.HandleFunc("DELETE /api/subtasks/{id}", h(s.deleteSubtask))
	s.mux.HandleFunc("POST /api/tasks/{id}/links", h(s.addTaskLink))
	s.mux.HandleFunc("GET /api/tasks/{id}/blocked", h(s.taskBlocked))
	s.mux.HandleFunc("DELETE /api/task-links/{id}", h(s.deleteTaskLink))

	// 撤销最近一次删除与操作历史
	s.mux.HandleFunc("GET /api/undo", h(s.undoState))
	s.mux.HandleFunc("POST /api/undo", h(s.undo))
	s.mux.HandleFunc("DELETE /api/undo", h(s.dropUndo))
	s.mux.HandleFunc("GET /api/activities", h(s.listActivities))
	s.mux.HandleFunc("DELETE /api/activities", h(s.clearActivities))

	// 保存的筛选条件
	s.mux.HandleFunc("GET /api/saved-filters", h(s.listSavedFilters))
	s.mux.HandleFunc("POST /api/saved-filters", h(s.createSavedFilter))
	s.mux.HandleFunc("PATCH /api/saved-filters/{id}", h(s.updateSavedFilter))
	s.mux.HandleFunc("DELETE /api/saved-filters/{id}", h(s.deleteSavedFilter))

	s.mux.HandleFunc("GET /api/folders", h(s.listFolders))
	s.mux.HandleFunc("POST /api/folders", h(s.createFolder))
	s.mux.HandleFunc("PATCH /api/folders/{id}", h(s.updateFolder))
	s.mux.HandleFunc("DELETE /api/folders/{id}", h(s.deleteFolder))
	s.mux.HandleFunc("PUT /api/folders/reorder", h(s.reorderFolders))

	s.mux.HandleFunc("GET /api/lists", h(s.listLists))
	s.mux.HandleFunc("POST /api/lists", h(s.createList))
	s.mux.HandleFunc("PATCH /api/lists/{id}", h(s.updateList))
	s.mux.HandleFunc("DELETE /api/lists/{id}", h(s.deleteList))
	s.mux.HandleFunc("PUT /api/lists/reorder", h(s.reorderLists))

	s.mux.HandleFunc("GET /api/tags", h(s.listTags))
	s.mux.HandleFunc("POST /api/tags", h(s.createTag))
	s.mux.HandleFunc("POST /api/tags/ensure", h(s.ensureTags))
	s.mux.HandleFunc("PATCH /api/tags/{id}", h(s.updateTag))
	s.mux.HandleFunc("DELETE /api/tags/{id}", h(s.deleteTag))

	s.mux.HandleFunc("GET /api/settings", h(s.getSettings))
	s.mux.HandleFunc("PUT /api/settings", h(s.putSettings))

	s.mux.HandleFunc("GET /api/habits", h(s.listHabits))
	s.mux.HandleFunc("POST /api/habits", h(s.createHabit))
	s.mux.HandleFunc("PUT /api/habits/reorder", h(s.reorderHabits))
	s.mux.HandleFunc("PATCH /api/habits/{id}", h(s.updateHabit))
	s.mux.HandleFunc("DELETE /api/habits/{id}", h(s.deleteHabit))
	s.mux.HandleFunc("POST /api/habits/{id}/check", h(s.checkHabit))
	s.mux.HandleFunc("DELETE /api/habits/{id}/check", h(s.uncheckHabit))

	s.mux.HandleFunc("GET /api/export", h(s.exportJSON))
	s.mux.HandleFunc("GET /api/export/csv", h(s.exportCSV))
	s.mux.HandleFunc("GET /api/export/zip", h(s.exportZIP))
	s.mux.HandleFunc("POST /api/import", h(s.importBackup))
	s.mux.HandleFunc("POST /api/import/file", h(s.importBackupFile))

	s.mux.HandleFunc("GET /api/stats", h(s.stats))
	s.mux.HandleFunc("GET /api/reviews", h(s.listReviews))
	s.mux.HandleFunc("GET /api/reviews/{date}", h(s.getReview))
	s.mux.HandleFunc("PUT /api/reviews", h(s.putReview))
	s.mux.HandleFunc("GET /api/focus", h(s.listFocus))
	s.mux.HandleFunc("POST /api/focus", h(s.addFocus))

	s.mux.HandleFunc("GET /api/reminders/due", h(s.dueReminders))
	s.mux.HandleFunc("POST /api/reminders/ack", h(s.ackReminder))
	s.mux.HandleFunc("POST /api/reminders/snooze", h(s.snoozeReminder))
	s.mux.HandleFunc("POST /api/reminders/reset", h(s.resetReminders))
	s.mux.HandleFunc("GET /api/meta/repeat", h(s.repeatMeta))

	// 附件
	s.mux.HandleFunc("GET /api/tasks/{id}/attachments", h(s.listAttachments))
	s.mux.HandleFunc("POST /api/tasks/{id}/attachments", h(s.uploadAttachment))
	s.mux.HandleFunc("GET /api/attachments/{id}", h(s.downloadAttachment))
	s.mux.HandleFunc("DELETE /api/attachments/{id}", h(s.deleteAttachment))

	// 出站 Webhook
	s.mux.HandleFunc("GET /api/webhooks", h(s.listWebhooks))
	s.mux.HandleFunc("POST /api/webhooks", h(s.createWebhook))
	s.mux.HandleFunc("PATCH /api/webhooks/{id}", h(s.updateWebhook))
	s.mux.HandleFunc("DELETE /api/webhooks/{id}", h(s.deleteWebhook))
	s.mux.HandleFunc("POST /api/webhooks/{id}/test", h(s.testWebhookHandler))
	s.mux.HandleFunc("GET /api/webhooks/{id}/deliveries", h(s.listDeliveries))

	// 模板任务
	s.mux.HandleFunc("GET /api/templates", h(s.listTemplates))
	s.mux.HandleFunc("POST /api/templates", h(s.createTemplate))
	s.mux.HandleFunc("PATCH /api/templates/{id}", h(s.updateTemplate))
	s.mux.HandleFunc("DELETE /api/templates/{id}", h(s.deleteTemplate))
	s.mux.HandleFunc("POST /api/templates/{id}/instantiate", h(s.instantiateTemplate))

	// 自动备份
	s.mux.HandleFunc("GET /api/backups", h(s.backupStatus))
	s.mux.HandleFunc("POST /api/backups/run", h(s.runBackup))

	// CalDAV：交给独立处理器，错误格式是 XML 而不是 JSON，不走 h() 包装。
	s.mux.HandleFunc("/.well-known/caldav", shenshicaldav.WellKnown)
	s.mux.Handle("/caldav/", s.caldav)
}

// ---------- 响应辅助 ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("写入响应失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	var ve store.ValidationError
	var fe store.ForbiddenError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Msg})
	case errors.As(err, &fe):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": fe.Msg})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "记录不存在"})
	default:
		// 内部错误只记日志，不回传给客户端：err.Error() 可能带 SQL/路径等敏感细节。
		log.Printf("服务端错误: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "服务端错误"})
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		return store.ValidationError{Msg: "请求体解析失败: " + err.Error()}
	}
	return nil
}

// decodeOptional 与 decode 相同，但允许空请求体并保留 v 的零值。
// 用于「不传即取默认」的接口，例如打卡不传 body 即视为今天加一次。
func decodeOptional(w http.ResponseWriter, r *http.Request, v any) error {
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		return store.ValidationError{Msg: "读取请求体失败: " + err.Error()}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return store.ValidationError{Msg: "请求体解析失败: " + err.Error()}
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, store.ValidationError{Msg: "路径参数 " + name + " 不合法"}
	}
	return id, nil
}

func queryInt64(r *http.Request, name string) *int64 {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func queryInt(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// queryIntClamped 同 queryInt，但把结果收进 [min, max]，
// 供「数值会驱动循环次数」的端点使用，防止超大入参打爆请求。
func queryIntClamped(r *http.Request, name string, def, min, max int) int {
	n := queryInt(r, name, def)
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func queryStr(r *http.Request, name string) *string {
	v := r.URL.Query().Get(name)
	if v == "" {
		return nil
	}
	return &v
}

// today 返回本地时区的今天（YYYY-MM-DD）。与库内 day 字段口径一致。
func today() string { return time.Now().Format("2006-01-02") }

func isLocalOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	return strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "http://127.0.0.1:") ||
		strings.HasPrefix(origin, "http://[::1]:")
}
