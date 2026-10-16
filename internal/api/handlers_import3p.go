package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/yufei/shendu/server/internal/store"
)

// thirdPartyLimit 是第三方导入文件的大小上限。8MB 足够装下几万条任务，
// 再大就该怀疑是传错了文件而不是真要导入这么多。
const thirdPartyLimit = 8 << 20

// previewThirdParty 只解析不落库，让用户先看清会进来多少东西、丢些什么。
func (s *Server) previewThirdParty(w http.ResponseWriter, r *http.Request) error {
	name, data, err := readUploadedFile(w, r, "导入预览失败")
	if err != nil {
		return err
	}
	prev, err := s.st.PreviewThirdParty(name, data)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, prev)
	return nil
}

// importThirdParty 执行导入。只增不改——第三方文件不是慎始的备份，
// 拿它去「替换」现有数据等于替用户做删除决定。
func (s *Server) importThirdParty(w http.ResponseWriter, r *http.Request) error {
	name, data, err := readUploadedFile(w, r, "导入失败")
	if err != nil {
		return err
	}
	res, prev, err := s.st.ImportThirdParty(name, data)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "preview": prev})
	return nil
}

// readUploadedFile 读上传的文件。
//
// multipart 与裸 body 两条路都收：前者给界面用，后者给脚本用（curl 一个文件就能导，
// 不必先拼 multipart）。**靠 Content-Type 判断走哪条**，而不是先试解析再回退——
// ParseMultipartForm 失败时流已经被读掉，回退分支拿到的会是残缺数据。
func readUploadedFile(w http.ResponseWriter, r *http.Request, failMsg string) (string, []byte, error) {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, thirdPartyLimit)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return "", nil, store.ValidationError{Msg: failMsg + "：读取内容失败"}
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			return "", nil, store.ValidationError{Msg: failMsg + "：请上传 CSV 或 .ics 文件"}
		}
		name := r.Header.Get("X-File-Name")
		if name == "" {
			name = "upload"
		}
		return name, data, nil
	}

	r.Body = http.MaxBytesReader(w, r.Body, thirdPartyLimit)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return "", nil, store.ValidationError{Msg: failMsg + "：解析上传内容失败"}
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	f, header, err := r.FormFile("file")
	if err != nil {
		return "", nil, store.ValidationError{Msg: failMsg + "：缺少上传文件（字段名 file）"}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, thirdPartyLimit))
	if err != nil {
		return "", nil, store.ValidationError{Msg: failMsg + "：读取文件失败"}
	}
	return header.Filename, data, nil
}