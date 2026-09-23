package caldav

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	emcaldav "github.com/emersion/go-webdav/caldav"

	"github.com/yufei/shendu/server/internal/store"
)

// DavHeader 必须出现在每一个 CalDAV 响应上，包括 401 挑战。
// Apple 日历在账户设置阶段会先发 OPTIONS 探测，看不到 calendar-access 就直接
// 报「calendar not found」，连口令都不会问。
const DavHeader = "1, 3, calendar-access, sync-collection"

// syncTokenPrefix 是不透明同步令牌的前缀：客户端不应解析它，
// 我们也可以在将来换内部表示而不破坏客户端。
const syncTokenPrefix = "ss1-"

// maxSyncBatch 单次增量同步最多返回的条目数。超了就截断，返回的 sync-token
// 停在最后一条已返回的变更上，客户端会带着它再来一轮接着取（不丢中间的变更）。
const maxSyncBatch = 500

// Handler 把 go-webdav 的 CalDAV 处理器包一层，补上 Apple 客户端要求的那些
// 「不该这么麻烦但必须这么麻烦」的细节。
type Handler struct {
	backend *Backend
	inner   *emcaldav.Handler
}

// NewHandler 构造 CalDAV 处理器。
func NewHandler(st *store.Store) *Handler {
	b := NewBackend(st)
	return &Handler{
		backend: b,
		inner:   &emcaldav.Handler{Backend: b, Prefix: "/caldav"},
	}
}

// Backend 暴露底层后端，便于注册变更钩子。
func (h *Handler) Backend() *Backend { return h.backend }

// ServeHTTP 先处理库里缺失或行为不合适的几个方法，其余交给 go-webdav。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("DAV", DavHeader)
	w.Header().Set("Allow", AllowHeader)

	switch r.Method {
	case http.MethodOptions:
		// 能力探测无需认证，否则 Apple 拿不到 Allow 与 Dav 就放弃了。
		w.WriteHeader(http.StatusNoContent)
		return
	case "PROPPATCH":
		// 库对 PROPPATCH 返回 501，Apple 见到就报「位置不支持此请求」。
		// 这里吞掉改动并返回 207 + 200：属性是服务端权威的，改了也不认。
		writePropPatchOK(w, r)
		return
	case "COPY", "MOVE", "MKCOL", "MKCALENDAR", "LOCK", "UNLOCK":
		// 一律 403：501 会让客户端停止重试并把整个账户标红。
		w.WriteHeader(http.StatusForbidden)
		return
	case "REPORT":
		if h.trySyncCollection(w, r) {
			return
		}
	}
	h.inner.ServeHTTP(w, r)
}

// WellKnown 处理 /.well-known/caldav。
// 库默认用 308 永久重定向，Apple 客户端跟随 302 更可靠，所以自己来。
func WellKnown(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("DAV", DavHeader)
	http.Redirect(w, r, PrincipalPath, http.StatusFound)
}

// writePropPatchOK 返回 207 Multi-Status，把请求里的属性原样报成 200。
func writePropPatchOK(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	href := r.URL.Path
	props := parsePropNames(raw)

	var buf bytes.Buffer
	buf.WriteString(xmlHeader)
	buf.WriteString(`<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`)
	buf.WriteString(`<D:response>`)
	buf.WriteString(`<D:href>` + xmlEscape(href) + `</D:href>`)
	buf.WriteString(`<D:propstat><D:prop>`)
	for _, p := range props {
		buf.WriteString(`<` + p + `/>`)
	}
	buf.WriteString(`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>`)
	buf.WriteString(`</D:response></D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write(buf.Bytes())
}

// propPatchReq 用于取出请求里出现的属性名，好把它们逐个报成 200。
type propPatchReq struct {
	Prop []struct {
		XMLName xml.Name
		Inner   []struct {
			XMLName xml.Name `xml:",any"`
		} `xml:",any"`
	} `xml:"propertyupdate>set>prop"`
}

func parsePropNames(raw []byte) []string {
	var req propPatchReq
	if err := xml.Unmarshal(raw, &req); err != nil {
		return nil
	}
	out := []string{}
	seen := map[string]bool{}
	for _, p := range req.Prop {
		for _, inner := range p.Inner {
			name := inner.XMLName.Local
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// ---------- RFC 6578 增量同步 ----------

type syncRequest struct {
	XMLName   xml.Name `xml:"sync-collection"`
	SyncToken string   `xml:"sync-token"`
	SyncLevel string   `xml:"sync-level"`
}

// trySyncCollection 处理 sync-collection 报告；不是这类请求时返回 false，
// 并把请求体还原，交给库继续处理。
func (h *Handler) trySyncCollection(w http.ResponseWriter, r *http.Request) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return false
	}
	// 无论是否命中都要把 body 放回去：库还要再读一次。
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if !bytes.Contains(raw, []byte("sync-collection")) {
		return false
	}

	var req syncRequest
	if err := xml.Unmarshal(raw, &req); err != nil {
		writeSyncError(w, http.StatusBadRequest, "")
		return true
	}

	kind, err := collectionOf(r.URL.Path)
	if err != nil {
		writeSyncError(w, http.StatusNotFound, "")
		return true
	}

	seq := int64(0)
	if tok := strings.TrimSpace(req.SyncToken); tok != "" {
		parsed, ok := parseSyncToken(tok)
		if !ok {
			// 令牌看不懂：按 RFC 6578 回 403 + valid-sync-token，客户端会自行全量重同步。
			writeSyncError(w, http.StatusForbidden, "valid-sync-token")
			return true
		}
		seq = parsed
	}

	changes, maxSeq, err := h.backend.st.CalDAVChangesSince(kind, seq, maxSyncBatch)
	if err != nil {
		writeSyncError(w, http.StatusServiceUnavailable, "")
		return true
	}

	// 同一个 uid 可能被改了多次，只保留最后一次的结果。
	type entry struct {
		uid     string
		id      int64
		deleted bool
	}
	order := []string{}
	byUID := map[string]entry{}
	for _, c := range changes {
		e := entry{uid: c.UID, deleted: c.Deleted}
		if c.TaskID != nil {
			e.id = *c.TaskID
		}
		if _, dup := byUID[c.UID]; !dup {
			order = append(order, c.UID)
		} else if e.deleted {
			// 已存在且本次是删除：删除优先，客户端不该再渲染它。
		}
		if prev, ok := byUID[c.UID]; ok && !e.deleted && prev.deleted {
			// 删了又建（理论上不会），以最新为准。
		}
		byUID[c.UID] = e
	}

	var buf bytes.Buffer
	buf.WriteString(xmlHeader)
	buf.WriteString(`<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`)
	for _, uid := range order {
		e := byUID[uid]
		if e.deleted || e.id <= 0 {
			buf.WriteString(`<D:response><D:href>` + xmlEscape(objectPath(kind, e.id)) + `</D:href>`)
			buf.WriteString(`<D:status>HTTP/1.1 404 Not Found</D:status></D:response>`)
			continue
		}
		t, err := h.backend.st.GetTask(e.id)
		if err != nil {
			buf.WriteString(`<D:response><D:href>` + xmlEscape(objectPath(kind, e.id)) + `</D:href>`)
			buf.WriteString(`<D:status>HTTP/1.1 404 Not Found</D:status></D:response>`)
			continue
		}
		obj, err := renderObject(kind, t)
		if err != nil {
			continue
		}
		text, err := encodeCalendar(obj.Data)
		if err != nil {
			continue
		}
		buf.WriteString(`<D:response><D:href>` + xmlEscape(obj.Path) + `</D:href>`)
		buf.WriteString(`<D:propstat><D:prop>`)
		buf.WriteString(`<D:getetag>` + xmlEscape(obj.ETag) + `</D:getetag>`)
		buf.WriteString(`<C:calendar-data>` + xmlEscape(text) + `</C:calendar-data>`)
		buf.WriteString(`</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>`)
		buf.WriteString(`</D:response>`)
	}
	buf.WriteString(`<D:sync-token>` + formatSyncToken(maxSeq) + `</D:sync-token>`)
	buf.WriteString(`</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write(buf.Bytes())
	return true
}

func collectionOf(p string) (string, error) {
	switch normalizePath(p) {
	case normalizePath(EventsPath):
		return CollectionEvents, nil
	case normalizePath(TasksPath):
		return CollectionTasks, nil
	}
	return "", fmt.Errorf("caldav: 不是日历集合: %s", p)
}

func formatSyncToken(seq int64) string { return syncTokenPrefix + strconv.FormatInt(seq, 10) }

func parseSyncToken(tok string) (int64, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(tok), syncTokenPrefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// writeSyncError 返回带 DAV 错误体的响应。elem 为空时不带具体错误元素。
func writeSyncError(w http.ResponseWriter, code int, elem string) {
	body := xmlHeader + `<D:error xmlns:D="DAV:">`
	if elem != "" {
		body += `<D:` + elem + `/>`
	}
	body += `</D:error>`
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>`

func xmlEscape(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}

// IsCalDAVPath 判断路径是否属于 CalDAV 端点，供最外层的头处理使用。
func IsCalDAVPath(p string) bool {
	return p == "/.well-known/caldav" || strings.HasPrefix(p, "/caldav/") || p == "/caldav"
}

// EnsureChangelog 为已有数据铺一条变更日志的基线。
//
// 客户端第一次同步会带一个空令牌，服务端于是把「序号 0 之后的所有变更」都推过去——
// 若基线为空，那等于全量推送。这里在基线为空时把现存任务记成「已同步过」，
// 之后客户端只会拿到真正的新变化。只在基线为空时做，避免每次启动都灌一遍日志。
func (h *Handler) EnsureChangelog() error {
	tasks, err := h.backend.st.ListTasks(store.TaskFilter{Status: "all", SortBy: "created"})
	if err != nil {
		return err
	}
	for _, kind := range []string{CollectionEvents, CollectionTasks} {
		seq, err := h.backend.st.CalDAVMaxSeq(kind)
		if err != nil {
			return err
		}
		if seq > 0 {
			continue
		}
		for i := range tasks {
			t := tasks[i]
			if kind == CollectionEvents && t.DueDate == nil {
				continue
			}
			if err := h.backend.st.NoteCalDAVChange(kind, uidFor(kind, t.ID), t.ID, false); err != nil {
				return err
			}
		}
	}
	return nil
}
