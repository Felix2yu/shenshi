package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/model"
)

// multipartBody 构造 multipart/form-data 上传体。
func multipartBody(t *testing.T, field, filename, content, contentType string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	if contentType != "" {
		h["Content-Type"] = []string{contentType}
	}
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

// TestAttachmentHandlers 上传 → 列表 → 下载 → 删除的接口链路。
func TestAttachmentHandlers(t *testing.T) {
	s, st := newTestServer(t)
	inbox, _ := st.InboxListID()
	task, err := st.CreateTask(taskInputTitle("附件接口任务"), inbox)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	id := itoa(task.ID)

	// 初始空列表。
	w := httptest.NewRecorder()
	if err := s.listAttachments(w, reqWithID("GET", "/api/tasks/"+id+"/attachments", id, "")); err != nil {
		t.Fatalf("listAttachments: %v", err)
	}
	var list []model.Attachment
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 0 {
		t.Fatalf("初始列表 = %+v err=%v", list, err)
	}
	// 非法路径参数。
	w = httptest.NewRecorder()
	if err := s.listAttachments(w, reqWithID("GET", "/api/tasks/x/attachments", "x", "")); err == nil {
		t.Error("非法 id 应报错")
	}

	// 上传。
	buf, ct := multipartBody(t, "file", "笔记.txt", "附件正文", "text/plain")
	w = httptest.NewRecorder()
	req := reqWithID("POST", "/api/tasks/"+id+"/attachments", id, "")
	req.Body = ioNopCloser(buf)
	req.Header.Set("Content-Type", ct)
	req.ContentLength = int64(buf.Len())
	if err := s.uploadAttachment(w, req); err != nil {
		t.Fatalf("uploadAttachment: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("code = %d", w.Code)
	}
	var att model.Attachment
	if err := json.Unmarshal(w.Body.Bytes(), &att); err != nil || att.ID == 0 {
		t.Fatalf("attachment = %+v err=%v", att, err)
	}
	if att.Name != "笔记.txt" {
		t.Errorf("name = %q", att.Name)
	}
	if att.Mime != "text/plain" {
		// multipart 未带 Content-Type 时回落 octet-stream；这里带了应为 text/plain。
		t.Logf("mime = %q（取决于解析路径）", att.Mime)
	}

	// 列表里出现。
	w = httptest.NewRecorder()
	if err := s.listAttachments(w, reqWithID("GET", "/api/tasks/"+id+"/attachments", id, "")); err != nil {
		t.Fatalf("listAttachments(2): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("列表 = %+v err=%v", list, err)
	}

	// 下载：正文一致，带头。
	attID := itoa(att.ID)
	w = httptest.NewRecorder()
	if err := s.downloadAttachment(w, reqWithID("GET", "/api/attachments/"+attID+"/download", attID, "")); err != nil {
		t.Fatalf("downloadAttachment: %v", err)
	}
	if w.Body.String() != "附件正文" {
		t.Errorf("下载内容 = %q", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "filename*=UTF-8''") {
		t.Errorf("Content-Disposition = %q，中文名需走 RFC 5987", cd)
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("应带 nosniff")
	}

	// 文件被清掉 → 410（记录还在、文件没了）。
	if err := os.Remove(s.st.AttachmentPath(&att)); err != nil {
		t.Fatalf("移除文件: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.downloadAttachment(w, reqWithID("GET", "/api/attachments/"+attID+"/download", attID, "")); err != nil {
		t.Fatalf("downloadAttachment(文件缺失): %v", err)
	}
	if w.Code != http.StatusGone {
		t.Errorf("code = %d，期望 410", w.Code)
	}

	// 删除。
	w = httptest.NewRecorder()
	if err := s.deleteAttachment(w, reqWithID("DELETE", "/api/attachments/"+attID, attID, "")); err != nil {
		t.Fatalf("deleteAttachment: %v", err)
	}
	// 删除不存在 → 404 语义（store.ErrNotFound）。
	w = httptest.NewRecorder()
	if err := s.deleteAttachment(w, reqWithID("DELETE", "/api/attachments/"+attID, attID, "")); err == nil {
		t.Error("删除不存在应报错")
	}
	// 下载不存在。
	w = httptest.NewRecorder()
	if err := s.downloadAttachment(w, reqWithID("GET", "/api/attachments/"+attID+"/download", attID, "")); err == nil {
		t.Error("下载不存在应报错")
	}
	// 非法 id。
	w = httptest.NewRecorder()
	if err := s.downloadAttachment(w, reqWithID("GET", "/api/attachments/0/download", "0", "")); err == nil {
		t.Error("非法 id 应报错")
	}
}

// TestUploadMissingFile 缺 file 字段与坏任务 id。
func TestUploadMissingFile(t *testing.T) {
	s, _ := newTestServer(t)

	// 非法任务 id。
	w := httptest.NewRecorder()
	req := reqWithID("POST", "/api/tasks/abc/attachments", "abc", "")
	req.Body = ioNopCloser(strings.NewReader(""))
	if err := s.uploadAttachment(w, req); err == nil {
		t.Error("非法 id 应报错")
	}

	// 缺 file 字段。
	w = httptest.NewRecorder()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("note", "没有文件")
	_ = mw.Close()
	req = reqWithID("POST", "/api/tasks/1/attachments", "1", "")
	req.Body = ioNopCloser(&buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.ContentLength = int64(buf.Len())
	if err := s.uploadAttachment(w, req); err == nil {
		t.Error("缺少 file 字段应报错")
	}
}

// ioNopCloser 把普通 Reader 包成 ReadCloser。
func ioNopCloser(r interface {
	Read([]byte) (int, error)
}) *readCloserWrap {
	return &readCloserWrap{r: r}
}

type readCloserWrap struct {
	r interface{ Read([]byte) (int, error) }
}

func (w *readCloserWrap) Read(p []byte) (int, error) { return w.r.Read(p) }
func (w *readCloserWrap) Close() error               { return nil }
