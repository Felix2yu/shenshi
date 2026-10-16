package api

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/yufei/shendu/server/internal/store"
)

// 上传请求的整体上限：比单个附件的 32MB 略大，留给 multipart 的边界与表单字段。
const uploadBodyLimit = 40 << 20

// listAttachments 返回一个任务上的附件。
func (s *Server) listAttachments(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	list, err := s.st.ListAttachments(id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

// uploadAttachment 接收 multipart 上传并挂到任务上。
func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	// 先套一层整体限额，否则超大请求体会把内存吃光。
	r.Body = http.MaxBytesReader(w, r.Body, uploadBodyLimit)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return store.ValidationError{Msg: "解析上传内容失败: " + err.Error()}
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	f, header, err := r.FormFile("file")
	if err != nil {
		return store.ValidationError{Msg: "缺少上传文件（字段名 file）"}
	}
	defer f.Close()

	mime := strings.TrimSpace(header.Header.Get("Content-Type"))
	if mime == "" {
		mime = "application/octet-stream"
	}
	a, err := s.st.SaveAttachment(id, header.Filename, mime, f)
	if err != nil {
		return err
	}
	// 附件不进 Webhook / CalDAV 事件：它不改变任务本身的含义，
	// 只是给详情面板添一份材料。
	writeJSON(w, http.StatusCreated, a)
	return nil
}

// downloadAttachment 下载附件内容。走 ServeContent 以支持断点续传。
func (s *Server) downloadAttachment(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.st.GetAttachment(id)
	if err != nil {
		return err
	}
	f, err := os.Open(s.st.AttachmentPath(a))
	if err != nil {
		// 记录还在、文件没了：说明数据目录被手工清理过，如实报 410 而不是 500。
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusGone, map[string]string{"error": "附件文件已不在磁盘上"})
			return nil
		}
		return err
	}
	defer f.Close()

	if a.Mime != "" {
		w.Header().Set("Content-Type", a.Mime)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	// 中文文件名必须走 RFC 5987 的 filename*，否则各浏览器行为不一。
	w.Header().Set("Content-Disposition",
		`attachment; filename*=UTF-8''`+url.PathEscape(a.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	mod := time.Time{}
	if t, err := time.Parse(time.RFC3339, a.CreatedAt); err == nil {
		mod = t
	}
	http.ServeContent(w, r, a.Name, mod, f)
	return nil
}

// deleteAttachment 删除一个附件。
func (s *Server) deleteAttachment(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "id")
	if err != nil {
		return err
	}
	// 先确认存在：删除不存在的附件应当报 404，而不是假装成功。
	if _, err := s.st.GetAttachment(id); err != nil {
		return err
	}
	if err := s.st.DeleteAttachment(id); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	return nil
}
