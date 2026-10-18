package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/store"
)

// staticHandler 是 go:embed 链的最后一环，必须两种状态都成立：
//   - 内嵌有产物（跑过 scripts/build.sh）：交出处理器，不能是 nil；
//   - 什么都没有（CI 的 test job 不构建前端，internal/web/dist 只有占位
//     .gitkeep，磁盘上也没有 web/dist）：必须失败得明明白白，
//     而不是返回 (nil, nil) 让调用方在 nil handler 上才崩。
func TestStaticHandlerEmbedWiring(t *testing.T) {
	h, err := staticHandler("")
	if err != nil {
		if h != nil {
			t.Fatal("返回错误时不应同时给出非空 handler")
		}
		if !strings.Contains(err.Error(), "未找到前端资源") {
			t.Fatalf("缺少前端资源时的错误信息不可读: %v", err)
		}
		return
	}
	if h == nil {
		t.Fatal("无错误时 handler 不应为 nil")
	}
}

// TestRunBackup 覆盖 `shenshi backup` 子命令：两种产物格式、未知格式与缺失数据文件。
// 该子命令是外部备份工具（docker-db-auto-backup）的入口，产物形态即接口契约：
// db 以 SQLite 魔数开头，zip 以 PK\x03\x04 开头，stdout 只有数据、日志走 stderr。
func TestRunBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var dbOut bytes.Buffer
	if err := runBackup([]string{"--db", dbPath, "--format", "db"}, &dbOut); err != nil {
		t.Fatalf("runBackup(db): %v", err)
	}
	if !bytes.HasPrefix(dbOut.Bytes(), []byte("SQLite format 3\x00")) {
		t.Error("db 产物应以 SQLite 魔数开头")
	}

	var zipOut bytes.Buffer
	if err := runBackup([]string{"--db", dbPath}, &zipOut); err != nil {
		t.Fatalf("runBackup(默认 zip): %v", err)
	}
	if !bytes.HasPrefix(zipOut.Bytes(), []byte{'P', 'K', 0x03, 0x04}) {
		t.Error("zip 产物应以 PK\\x03\\x04 开头")
	}

	if err := runBackup([]string{"--db", dbPath, "--format", "tar"}, &dbOut); err == nil {
		t.Error("未知格式应报错")
	}

	// 数据文件不存在时报错，且不得顺手新建一个空库。
	missing := filepath.Join(dir, "missing.db")
	if err := runBackup([]string{"--db", missing}, &zipOut); err == nil {
		t.Error("缺失数据文件应报错")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("不应创建空数据文件, stat err=%v", err)
	}
}
