package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteSQLiteSnapshot 把数据库以 SQLite 自身的一致性快照写到 w。
//
// 用 VACUUM INTO 而不是复制文件：热拷贝正在被写入的 db/-wal/-shm 不是原子操作，
// 可能拷出损坏的副本或漏掉 WAL 里已提交的数据；VACUUM INTO 由 SQLite 在事务里
// 生成一份自洽的快照，WAL 模式下也不阻塞写入。
//
// 快照先落在临时目录再流式输出：VACUUM INTO 要求目标不存在，且这样无论
// 调用方读到一半断开，都不会在数据目录里留下半成品。
func (s *Store) WriteSQLiteSnapshot(w io.Writer) error {
	dir, err := os.MkdirTemp("", "shenshi-snapshot-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)

	target := filepath.Join(dir, "snapshot.db")
	if _, err := s.db.Exec("VACUUM INTO ?", target); err != nil {
		return fmt.Errorf("导出数据库快照失败: %w", err)
	}

	f, err := os.Open(target)
	if err != nil {
		return fmt.Errorf("读取快照失败: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("写出快照失败: %w", err)
	}
	return nil
}
