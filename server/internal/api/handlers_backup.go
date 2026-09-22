package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/yufei/shendu/server/internal/store"
)

// 导入用的请求体上限放宽到 64MB：备份文件里装的是全部任务与复盘，4MB 不够用。
const importBodyLimit = 64 << 20

// 上传备份文件的上限：带附件的压缩包可能很大，但不能无上限。
const importFileLimit = 512 << 20

// exportJSON 下载全量备份。
func (s *Server) exportJSON(w http.ResponseWriter, r *http.Request) error {
	data, err := s.st.ExportJSON()
	if err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="shenshi-backup-`+stamp+`.json"`)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(data)
	return err
}

// exportZIP 下载一份自洽的完整备份：JSON 在包根上，附件在 attachments/ 下。
//
// 这是推荐的备份方式 —— 单独一份 JSON 只带得走文字，附件留在磁盘上，
// 换机器、换数据目录就丢了。
func (s *Server) exportZIP(w http.ResponseWriter, r *http.Request) error {
	res, err := s.st.ExportZIP()
	if err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="shenshi-backup-`+stamp+`.zip"`)
	w.Header().Set("X-Shenshi-Attachments", strconv.Itoa(res.Files))
	w.Header().Set("X-Shenshi-Attachments-Missing", strconv.Itoa(res.Missing))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(res.Data)
	return err
}

// exportCSV 下载任务表格，供 Excel 等工具消费。
func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) error {
	data, err := s.st.ExportCSV()
	if err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="shenshi-tasks-`+stamp+`.csv"`)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(data)
	return err
}

// importBackup 导入备份，?mode=merge（默认，追加副本）或 replace（清空重建）。
// 请求体是裸 JSON，不带附件；要连附件一起还原请用 importBackupFile。
func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) error {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = store.ImportMerge
	}
	var body store.ExportBundle
	if err := decodeLimited(w, r, &body, importBodyLimit); err != nil {
		return err
	}
	// 裸 JSON 里没有文件，附件只有磁盘上原本就在的那些能挂回去。
	res, err := s.st.Import(&body, mode, nil)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

// importBackupFile 接收上传的备份文件：裸 JSON，或导出接口产出的压缩包（含附件）。
//
// 之所以另开一个接口而不在 /api/import 上兼容 multipart：压缩包是字节流，
// 塞进 JSON 字段要 base64 膨胀三分之一，也让「导入」的两种输入形态纠缠不清。
func (s *Server) importBackupFile(w http.ResponseWriter, r *http.Request) error {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = store.ImportMerge
	}
	r.Body = http.MaxBytesReader(w, r.Body, importFileLimit+uploadBodyLimit)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return store.ValidationError{Msg: "解析上传内容失败: " + err.Error()}
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	f, _, err := r.FormFile("file")
	if err != nil {
		return store.ValidationError{Msg: "缺少上传文件（字段名 file）"}
	}
	defer f.Close()

	// 先落临时文件：zip 的中央目录在末尾，只能随机访问，不能只拿一条流；
	// 而且带附件的备份可能比愿意给它预留的内存还大。
	tmp, err := os.CreateTemp("", "shenshi-import-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	n, err := io.Copy(tmp, io.LimitReader(f, importFileLimit+1))
	if err != nil {
		tmp.Close()
		return store.ValidationError{Msg: "读取上传文件失败: " + err.Error()}
	}
	if n > importFileLimit {
		tmp.Close()
		return store.ValidationError{Msg: "备份文件超过 512MB 上限"}
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	bf, err := store.OpenBackupFile(tmpName)
	if err != nil {
		return err
	}
	defer bf.Close()

	res, err := s.st.Import(bf.Bundle, mode, bf.Source)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

// decodeLimited 与 decode 相同，只是请求体上限可调。
func decodeLimited(w http.ResponseWriter, r *http.Request, v any, limit int64) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := dec.Decode(v); err != nil {
		return store.ValidationError{Msg: "备份解析失败: " + err.Error()}
	}
	return nil
}

// ---------- 自动备份 ----------
//
// 手动导出解决「我要一份备份」，自动备份解决「我忘了导出」。
// 两者共用同一份 ExportBundle，因此自动备份出来的文件可以直接喂给导入接口。

// backupStatus 返回自动备份的设置、上次结果与现存文件。
func (s *Server) backupStatus(w http.ResponseWriter, r *http.Request) error {
	st, err := s.st.Settings()
	if err != nil {
		return err
	}
	files, err := s.st.ListBackups()
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":   st[store.SetAutoBackup] == "1",
		"hour":      strWithDefault(st[store.SetAutoBackupHour], "3"),
		"keep":      strWithDefault(st[store.SetAutoBackupKeep], "14"),
		"lastAt":    st[store.SetBackupLastAt],
		"lastFile":  st[store.SetBackupLastFile],
		"lastError": st[store.SetBackupLastError],
		"dir":       s.st.BackupDir(),
		"files":     files,
	})
	return nil
}

// runBackup 立即执行一次备份，忽略「是否到了设定时点」。
func (s *Server) runBackup(w http.ResponseWriter, r *http.Request) error {
	keep := queryInt(r, "keep", 0)
	file, err := s.auto.BackupOnce(keep)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "file": file})
	return nil
}

func strWithDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
