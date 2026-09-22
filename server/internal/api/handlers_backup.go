package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yufei/shendu/server/internal/store"
)

// 导入用的请求体上限放宽到 64MB：备份文件里装的是全部任务与复盘，4MB 不够用。
const importBodyLimit = 64 << 20

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
func (s *Server) importBackup(w http.ResponseWriter, r *http.Request) error {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = store.ImportMerge
	}
	var body store.ExportBundle
	if err := decodeLimited(w, r, &body, importBodyLimit); err != nil {
		return err
	}
	res, err := s.st.Import(&body, mode)
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
