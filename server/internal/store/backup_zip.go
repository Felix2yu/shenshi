package store

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 压缩包内部的固定布局。导入时按同样的名字把两者对上，改这里要同步改那边。
const (
	// ZipManifest 是压缩包根上的 JSON 备份。
	ZipManifest = "shenshi-backup.json"
	// zipAttachmentPrefix 是附件在包内的目录前缀。
	zipAttachmentPrefix = "attachments/"
)

// ZipExport 是一次压缩包导出的结果。
// Missing 让调用方有机会告诉用户「有几个文件没带走」——静默漏掉附件，
// 等真要恢复时才发现，是备份功能最不能犯的错。
type ZipExport struct {
	Data    []byte
	Files   int // 打包进去的附件数
	Missing int // 记录存在但磁盘上已找不到文件的附件数
}

// ExportZIP 把 JSON 备份与全部附件打包成一个 zip。
//
// 附件是字节流，塞进 JSON 只会变成一堆 base64；但备份又必须自洽 ——
// 于是 JSON 留在根上，附件按存储名放进 attachments/，导入时按名字重新对上。
// 包内用存储名而不是原始文件名：它是库里的主键，同名文件、非法字符都干扰不到。
func (s *Store) ExportZIP() (*ZipExport, error) {
	bundle, err := s.Export()
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	w, err := zw.Create(ZipManifest)
	if err != nil {
		return nil, fmt.Errorf("创建压缩包失败: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("写入备份内容失败: %w", err)
	}

	out := &ZipExport{}
	seen := map[string]bool{}
	for _, t := range bundle.Tasks {
		for i := range t.Attachments {
			a := t.Attachments[i]
			if a.File == "" || seen[a.File] {
				continue // 同一存储名只打包一次
			}
			seen[a.File] = true
			ok, err := zipOneAttachment(zw, s.attachmentDir, a.File)
			if err != nil {
				_ = zw.Close()
				return nil, err
			}
			if ok {
				out.Files++
			} else {
				// 记录还在、文件没了（数据目录被手工清理过）：跳过它，别让整份备份失败。
				out.Missing++
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("完成压缩包失败: %w", err)
	}
	out.Data = buf.Bytes()
	return out, nil
}

// zipOneAttachment 把一个附件写进压缩包。文件不存在时返回 (false, nil)。
func zipOneAttachment(zw *zip.Writer, dir, stored string) (bool, error) {
	f, err := os.Open(filepath.Join(dir, stored))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("读取附件失败: %w", err)
	}
	defer f.Close()

	w, err := zw.Create(zipAttachmentPrefix + stored)
	if err != nil {
		return false, fmt.Errorf("创建附件条目失败: %w", err)
	}
	if _, err := io.Copy(w, f); err != nil {
		return false, fmt.Errorf("写入附件失败: %w", err)
	}
	return true, nil
}

// ---------- 读回备份文件 ----------

// AttachmentSource 为导入提供附件内容。
// Open 的 key 是备份里记录的存储名（attachment.File）；
// 不存在时返回 fs.ErrNotExist，导入会据此判定「这次没带上该文件」。
type AttachmentSource interface {
	Open(stored string) (io.ReadCloser, error)
}

// BackupFile 是磁盘上的一份备份：可能是裸 JSON，也可能是导出的压缩包。
type BackupFile struct {
	Bundle *ExportBundle
	Source AttachmentSource // 压缩包里的附件；裸 JSON 时为 nil
	closer io.Closer
}

// Close 释放底层文件（压缩包需要一直开着读完）。可重复调用。
func (b *BackupFile) Close() error {
	if b == nil || b.closer == nil {
		return nil
	}
	c := b.closer
	b.closer = nil
	return c.Close()
}

// OpenBackupFile 打开一个备份文件并判断它的形态。
//
// 之所以要求真实文件路径而不是 io.Reader：zip 的中央目录在文件末尾，
// 必须随机访问，只给一条流读不了。所以上传先落临时文件再交给这里。
func OpenBackupFile(name string) (*BackupFile, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("打开备份文件失败: %w", err)
	}
	out := &BackupFile{closer: f}

	magic := make([]byte, 4)
	n, _ := io.ReadFull(f, magic)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	if n == 4 && string(magic) == "PK\x03\x04" {
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		zr, err := zip.NewReader(f, info.Size())
		if err != nil {
			f.Close()
			return nil, ValidationError{Msg: "压缩包解析失败: " + err.Error()}
		}
		out.Bundle, out.Source, err = readZipBundle(zr)
		if err != nil {
			f.Close()
			return nil, err
		}
		return out, nil
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	var b ExportBundle
	if err := json.Unmarshal(data, &b); err != nil {
		f.Close()
		return nil, ValidationError{Msg: "备份解析失败: " + err.Error()}
	}
	out.Bundle = &b
	return out, nil
}

// readZipBundle 从压缩包里取出 JSON 备份与附件目录索引。
func readZipBundle(zr *zip.Reader) (*ExportBundle, AttachmentSource, error) {
	var manifest *zip.File
	files := map[string]*zip.File{}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.TrimPrefix(filepathToSlash(zf.Name), "./"))
		if strings.HasPrefix(name, zipAttachmentPrefix) {
			files[name[len(zipAttachmentPrefix):]] = zf
			continue
		}
		// 备份内容认固定名字，其次退让给根上的第一个 json ——
		// 手写打包的目录结构不该让一份本来可用的备份废掉。
		if manifest == nil && (name == ZipManifest || (!strings.Contains(name, "/") && strings.HasSuffix(strings.ToLower(name), ".json"))) {
			manifest = zf
		}
	}
	if manifest == nil {
		return nil, nil, ValidationError{Msg: "压缩包里没有找到 JSON 备份（应为 " + ZipManifest + "）"}
	}
	rc, err := manifest.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("读取压缩包内的备份失败: %w", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, importJSONLimit))
	if err != nil {
		return nil, nil, err
	}
	var b ExportBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, nil, ValidationError{Msg: "备份解析失败: " + err.Error()}
	}
	return &b, &zipSource{files: files}, nil
}

// importJSONLimit 压缩包内 JSON 的读取上限，防止畸形包把内存撑爆。
const importJSONLimit = 64 << 20

type zipSource struct {
	files map[string]*zip.File
}

func (z *zipSource) Open(stored string) (io.ReadCloser, error) {
	zf, ok := z.files[stored]
	if !ok {
		return nil, os.ErrNotExist
	}
	return zf.Open()
}

// filepathToSlash 把包内路径统一成斜杠，Windows 上打的包也能读。
func filepathToSlash(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

// 判定「附件没随备份带来」统一走这一个口径。
func notProvided(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
