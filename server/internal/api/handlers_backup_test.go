package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/store"
)

// TestExportHandlers 三种导出的头与内容。
func TestExportHandlers(t *testing.T) {
	s, st := newTestServer(t)
	inbox, _ := st.InboxListID()
	if _, err := st.CreateTask(taskInputTitle("导出接口任务"), inbox); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// JSON 备份：Content-Disposition 带 shenshi-backup 前缀。
	w := httptest.NewRecorder()
	if err := s.exportJSON(w, httptest.NewRequest("GET", "/api/export", nil)); err != nil {
		t.Fatalf("exportJSON: %v", err)
	}
	if w.Code != http.StatusOK {
		t.Errorf("code = %d", w.Code)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "shenshi-backup-") || !strings.Contains(cd, ".json") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	var bundle store.ExportBundle
	if err := json.Unmarshal(w.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("导出内容应为合法 bundle: %v", err)
	}
	if bundle.App != "慎始" {
		t.Errorf("bundle.App = %q", bundle.App)
	}

	// ZIP。
	w = httptest.NewRecorder()
	if err := s.exportZIP(w, httptest.NewRequest("GET", "/api/export/zip", nil)); err != nil {
		t.Fatalf("exportZIP: %v", err)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	if w.Header().Get("X-Shenshi-Attachments") == "" {
		t.Error("应带附件计数头")
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
		t.Error("ZIP 应以 PK 魔数开头")
	}

	// CSV：BOM + 表头。
	w = httptest.NewRecorder()
	if err := s.exportCSV(w, httptest.NewRequest("GET", "/api/export/csv", nil)); err != nil {
		t.Fatalf("exportCSV: %v", err)
	}
	if !strings.HasPrefix(w.Body.String(), "\xEF\xBB\xBF") {
		t.Error("CSV 应带 BOM")
	}
	if !strings.Contains(w.Body.String(), "导出接口任务") {
		t.Error("CSV 应含任务行")
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, ".csv") {
		t.Errorf("CSV Disposition = %q", cd)
	}
}

// TestImportBackupHandlers 裸 JSON 与文件上传两种导入路径。
func TestImportBackupHandlers(t *testing.T) {
	s, st := newTestServer(t)

	// 先导出一份。
	data, err := st.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}

	// 裸 JSON 导入（merge 默认）。
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/import", bytes.NewReader(data))
	if err := s.importBackup(w, req); err != nil {
		t.Fatalf("importBackup: %v", err)
	}
	var res store.ImportResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析导入结果: %v", err)
	}
	if res.Mode != store.ImportMerge || res.Tasks < 1 {
		t.Errorf("导入结果 = %+v", res)
	}

	// replace 模式。
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/import?mode=replace", bytes.NewReader(data))
	if err := s.importBackup(w, req); err != nil {
		t.Fatalf("importBackup(replace): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Mode != store.ImportReplace {
		t.Errorf("replace 结果 = %+v err=%v", res, err)
	}

	// 坏 JSON。
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/import", strings.NewReader(`{bad`))
	if err := s.importBackup(w, req); err == nil {
		t.Error("坏 JSON 应报错")
	}
	// 空备份 → store 校验拒绝。
	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/import", strings.NewReader(`{"version":1}`))
	if err := s.importBackup(w, req); err == nil {
		t.Error("空备份应被拒绝")
	}

	// 文件上传路径：先打 ZIP 包。
	zexp, err := st.ExportZIP()
	if err != nil {
		t.Fatalf("ExportZIP: %v", err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "backup.zip")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(zexp.Data); err != nil {
		t.Fatalf("写文件: %v", err)
	}
	_ = mw.Close()

	w = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/import/file", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := s.importBackupFile(w, req); err != nil {
		t.Fatalf("importBackupFile: %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("解析文件导入结果: %v", err)
	}
	if res.Tasks < 1 {
		t.Errorf("文件导入 tasks = %d", res.Tasks)
	}

	// 缺 file 字段。
	w = httptest.NewRecorder()
	buf2 := bytes.NewBufferString("not-multipart")
	req = httptest.NewRequest("POST", "/api/import/file", buf2)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := s.importBackupFile(w, req); err == nil {
		t.Error("非 multipart 应报错")
	}
	// 坏文件内容。
	w = httptest.NewRecorder()
	var buf3 bytes.Buffer
	mw3 := multipart.NewWriter(&buf3)
	f3, _ := mw3.CreateFormFile("file", "bad.zip")
	_, _ = f3.Write([]byte("not a backup"))
	_ = mw3.Close()
	req = httptest.NewRequest("POST", "/api/import/file", &buf3)
	req.Header.Set("Content-Type", mw3.FormDataContentType())
	if err := s.importBackupFile(w, req); err == nil {
		t.Error("坏备份文件应报错")
	}
}

// TestBackupStatusAndRun 自动备份状态与手动触发。
func TestBackupStatusAndRun(t *testing.T) {
	s, st := newTestServer(t)

	// 默认状态：未启用、hour=3、keep=14。
	w := httptest.NewRecorder()
	if err := s.backupStatus(w, httptest.NewRequest("GET", "/api/backup/status", nil)); err != nil {
		t.Fatalf("backupStatus: %v", err)
	}
	var status struct {
		Enabled bool     `json:"enabled"`
		Hour    string   `json:"hour"`
		Keep    string   `json:"keep"`
		Dir     string   `json:"dir"`
		Files   []string `json:"files"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("解析状态: %v", err)
	}
	if status.Enabled || status.Hour != "3" || status.Keep != "14" {
		t.Errorf("默认状态 = %+v", status)
	}
	if status.Dir == "" || status.Files == nil {
		t.Errorf("dir/files = %+v", status)
	}

	// 配置后再读：自定义值生效。
	if err := st.SaveSettings(map[string]string{
		store.SetAutoBackup:     "1",
		store.SetAutoBackupHour: "4",
		store.SetAutoBackupKeep: "5",
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	w = httptest.NewRecorder()
	if err := s.backupStatus(w, httptest.NewRequest("GET", "/api/backup/status", nil)); err != nil {
		t.Fatalf("backupStatus(2): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("解析状态(2): %v", err)
	}
	if !status.Enabled || status.Hour != "4" || status.Keep != "5" {
		t.Errorf("配置后状态 = %+v", status)
	}

	// 手动触发一次备份。
	w = httptest.NewRecorder()
	if err := s.runBackup(w, httptest.NewRequest("POST", "/api/backup/run", nil)); err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	var run struct {
		OK   bool   `json:"ok"`
		File string `json:"file"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil || !run.OK {
		t.Fatalf("run = %+v err=%v", run, err)
	}
	if _, err := os.Stat(run.File); err != nil {
		t.Errorf("备份文件应存在: %v", err)
	}
	// 状态里出现该文件。
	w = httptest.NewRecorder()
	if err := s.backupStatus(w, httptest.NewRequest("GET", "/api/backup/status", nil)); err != nil {
		t.Fatalf("backupStatus(3): %v", err)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("解析状态(3): %v", err)
	}
	found := false
	for _, f := range status.Files {
		if strings.HasSuffix(f, filepath.Base(run.File)) {
			found = true
		}
	}
	if !found {
		t.Errorf("备份列表 %v 应含 %s", status.Files, filepath.Base(run.File))
	}
}
