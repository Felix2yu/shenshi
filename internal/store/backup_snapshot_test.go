package store

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteSQLiteSnapshot 快照应是一份可直接独立打开的库，且包含 WAL 里已提交的数据。
func TestWriteSQLiteSnapshot(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("读 journal_mode: %v", err)
	}
	if strings.ToLower(mode) != "wal" {
		t.Fatalf("前置条件：源库应为 WAL 模式，实际 %q", mode)
	}
	mustCreateTask(t, s, "快照任务", nil)

	var buf bytes.Buffer
	if err := s.WriteSQLiteSnapshot(&buf); err != nil {
		t.Fatalf("WriteSQLiteSnapshot: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("SQLite format 3\x00")) {
		t.Fatal("快照缺少 SQLite 魔数")
	}

	// 写到独立位置后打开：数据必须都在，说明 WAL 内容被完整带进快照。
	out := filepath.Join(dir, "snapshot.db")
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("写快照文件: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+out+"?mode=ro")
	if err != nil {
		t.Fatalf("打开快照: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE title = '快照任务'`).Scan(&n); err != nil {
		t.Fatalf("快照不可读: %v", err)
	}
	if n != 1 {
		t.Errorf("快照内任务数 = %d, want 1", n)
	}
}

// TestOpenForExport 导出打开：不迁移 schema、不建目录，缺文件直接报错。
func TestOpenForExport(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// 数据文件不存在时报错，而不是凭空建一个空库。
	if _, err := OpenForExport(dbPath, Options{}); err == nil {
		t.Fatal("不存在的库应报错")
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Errorf("不应创建数据文件, stat err=%v", err)
	}

	// 只含无关表的「旧库」：导出打开不做迁移，业务表必须原样缺席。
	raw := filepath.Join(dir, "raw.db")
	rw, err := sql.Open("sqlite", "file:"+raw)
	if err != nil {
		t.Fatalf("建旧库: %v", err)
	}
	if _, err := rw.Exec("CREATE TABLE dummy(id INTEGER)"); err != nil {
		t.Fatalf("建旧表: %v", err)
	}
	if err := rw.Close(); err != nil {
		t.Fatalf("关闭旧库: %v", err)
	}
	st, err := OpenForExport(raw, Options{})
	if err != nil {
		t.Fatalf("OpenForExport: %v", err)
	}
	defer st.Close()
	var tables int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&tables); err != nil {
		t.Fatalf("读 sqlite_master: %v", err)
	}
	if tables != 0 {
		t.Error("导出打开不应执行迁移创建业务表")
	}

	// 正常库：导出打开可读出已有数据。
	good := filepath.Join(dir, "good.db")
	s, err := Open(good)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustCreateTask(t, s, "导出任务", nil)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	exp, err := OpenForExport(good, Options{})
	if err != nil {
		t.Fatalf("OpenForExport(good): %v", err)
	}
	defer exp.Close()
	bundle, err := exp.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	found := false
	for _, task := range bundle.Tasks {
		if task.Title == "导出任务" {
			found = true
		}
	}
	if !found {
		t.Errorf("导出结果缺少刚写入的任务（共 %d 条）", len(bundle.Tasks))
	}
}

// TestWriteZIPStream 流式导出与内存导出逐字节一致，附件计数一致。
func TestWriteZIPStream(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "流式任务", nil)
	if _, err := s.SaveAttachment(task.ID, "a.txt", "text/plain", strings.NewReader("附件内容")); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}

	mem, err := s.ExportZIP()
	if err != nil {
		t.Fatalf("ExportZIP: %v", err)
	}
	var buf bytes.Buffer
	stream, err := s.WriteZIP(&buf)
	if err != nil {
		t.Fatalf("WriteZIP: %v", err)
	}
	if stream.Files != mem.Files || stream.Missing != mem.Missing {
		t.Errorf("计数不一致: stream=%+v mem=%+v", stream, mem)
	}
	if !bytes.Equal(buf.Bytes(), mem.Data) {
		t.Errorf("流式输出与内存导出应逐字节一致: stream=%d bytes mem=%d bytes", buf.Len(), len(mem.Data))
	}
}
