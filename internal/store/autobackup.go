package store

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// 自动备份的设置项与默认值。
const (
	SetAutoBackup      = "autoBackup"      // "1" 启用
	SetAutoBackupHour  = "autoBackupHour"  // 一天中几点备份，0-23
	SetAutoBackupKeep  = "autoBackupKeep"  // 保留份数
	SetBackupLastAt    = "autoBackupLastAt"
	SetBackupLastFile  = "autoBackupLastFile"
	SetBackupLastError = "autoBackupLastError"

	defaultBackupHour = 3
	defaultBackupKeep = 14
	// 连续两次备份的最小间隔：防止进程重启后重复触发。
	minBackupGap = 18 * time.Hour
)

// AutoBackup 在进程内定时导出一份全量备份（含附件的 zip）。
//
// 之所以放在进程里而不是依赖外部 cron：整个应用就一个二进制、一个数据目录，
// 再要求用户配一遍系统定时器就把「开箱即用」弄丢了。代价是关掉进程就不备份，
// 这对本地自用足够——真要异地容灾，还是把数据目录交给外部备份工具更稳。
type AutoBackup struct {
	s     *Store
	logf  func(format string, v ...any)
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
	start sync.Once
}

// NewAutoBackup 构造一个自动备份器。logf 为空时使用标准日志。
func NewAutoBackup(s *Store, logf func(format string, v ...any)) *AutoBackup {
	if logf == nil {
		logf = log.Printf
	}
	return &AutoBackup{s: s, logf: logf, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start 后台运行。可重复调用，只有第一次生效。
func (a *AutoBackup) Start() {
	a.start.Do(func() {
		go a.loop()
	})
}

// Stop 结束后台循环并等待退出。
func (a *AutoBackup) Stop() {
	a.once.Do(func() { close(a.stop) })
	<-a.done
}

// loop 每 10 分钟看一眼是否到了该备份的时点。
// 用轮询而非精确计时：配置可能随时被改，且进程可能被挂起，醒来后补一次即可。
func (a *AutoBackup) loop() {
	defer close(a.done)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-ticker.C:
			a.tick()
		}
	}
}

func (a *AutoBackup) tick() {
	on, hour, keep, err := a.config()
	if err != nil || !on {
		return
	}
	now := time.Now()
	if now.Hour() != hour {
		return
	}
	if last, ok := a.lastAt(); ok && time.Since(last) < minBackupGap {
		return
	}
	file, err := a.BackupOnce(keep)
	if err != nil {
		a.logf("自动备份失败: %v", err)
		_ = a.s.SaveSettings(map[string]string{SetBackupLastError: truncate(err.Error(), 300)})
		return
	}
	a.logf("自动备份完成: %s", file)
}

// config 读取自动备份设置，缺失项按默认值补齐。
func (a *AutoBackup) config() (on bool, hour int, keep int, err error) {
	st, err := a.s.Settings()
	if err != nil {
		return false, 0, 0, err
	}
	on = strings.TrimSpace(st[SetAutoBackup]) == "1"
	hour = defaultBackupHour
	if n, e := strconv.Atoi(strings.TrimSpace(st[SetAutoBackupHour])); e == nil && n >= 0 && n <= 23 {
		hour = n
	}
	keep = defaultBackupKeep
	if n, e := strconv.Atoi(strings.TrimSpace(st[SetAutoBackupKeep])); e == nil && n > 0 {
		keep = n
	}
	return on, hour, keep, nil
}

func (a *AutoBackup) lastAt() (time.Time, bool) {
	st, err := a.s.Settings()
	if err != nil {
		return time.Time{}, false
	}
	s := strings.TrimSpace(st[SetBackupLastAt])
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// BackupOnce 立即导出一份备份并裁掉多余的旧文件，返回写入的路径。
//
// 产出的是 zip：附件一并打包，这样每一份备份都能独立还原，
// 不必再惦记「还要把 attachments 目录一起拷走」。
func (a *AutoBackup) BackupOnce(keep int) (string, error) {
	if keep <= 0 {
		keep = defaultBackupKeep
	}
	res, err := a.s.ExportZIP()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(a.s.BackupDir(), 0o755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	name := "shenshi-backup-" + stamp + ".zip"
	target := filepath.Join(a.s.BackupDir(), name)

	// 先写临时文件再改名：中途失败不会留下半截的「备份」骗人。
	tmp, err := os.CreateTemp(a.s.BackupDir(), ".backup-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(res.Data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if res.Missing > 0 {
		a.logf("自动备份：有 %d 个附件记录的文件已不在磁盘上，未打包", res.Missing)
	}

	removed, err := pruneBackups(a.s.BackupDir(), keep)
	if err != nil {
		a.logf("清理旧备份失败: %v", err)
	}
	_ = a.s.SaveSettings(map[string]string{
		SetBackupLastAt:    model.Now(),
		SetBackupLastFile:  name,
		SetBackupLastError: "",
	})
	if removed > 0 {
		a.logf("已清理 %d 份超出保留数量的旧备份", removed)
	}
	return target, nil
}

// backupNameOK 判断一个文件是不是本应用产出的备份。
// .json 是改用压缩包之前留下的旧备份，一并纳入清理，免得它们永远占着份数。
func backupNameOK(name string) bool {
	if !strings.HasPrefix(name, "shenshi-backup-") {
		return false
	}
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".json")
}

// pruneBackups 只保留最新的 keep 份备份，返回删除数量。
func pruneBackups(dir string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	files := []string{}
	for _, e := range entries {
		if e.IsDir() || !backupNameOK(e.Name()) {
			continue
		}
		files = append(files, e.Name())
	}
	if len(files) <= keep {
		return 0, nil
	}
	// 名字里带时间戳，字典序即时间序。
	sort.Strings(files)
	drop := files[:len(files)-keep]
	removed := 0
	for _, n := range drop {
		if err := os.Remove(filepath.Join(dir, n)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// ListBackups 返回备份目录里现存的文件名，新的在前。
func (s *Store) ListBackups() ([]string, error) {
	entries, err := os.ReadDir(s.BackupDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	files := []string{}
	for _, e := range entries {
		if e.IsDir() || !backupNameOK(e.Name()) {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files, nil
}
