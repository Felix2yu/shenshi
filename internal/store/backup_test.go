package store

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yufei/shendu/server/internal/model"
)

// TestExportImportRoundtrip 导出 → 另一库导入：任务、清单、标签、习惯完整往返。
func TestExportImportRoundtrip(t *testing.T) {
	src := newTestStore(t)
	list, err := src.CreateList(ListInput{Name: sp("往返清单")})
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}
	tag, err := src.CreateTag(TagInput{Name: sp("往返标签")})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	task := mustCreateTask(t, src, "往返任务", func(in *model.TaskInput) {
		in.ListID = optOf(list.ID)
		in.TagIDs = optOf([]int64{tag.ID})
		in.DueDate = optOfPtr(sp("2026-12-24"))
		in.Subtasks = optOf([]model.Subtask{{Title: "子项", SortOrder: 1024}})
	})
	h, err := src.CreateHabit(HabitInput{Name: sp("往返习惯")})
	if err != nil {
		t.Fatalf("CreateHabit: %v", err)
	}
	if _, err := src.CheckIn(h.ID, CheckInput{Day: time.Now().Format("2006-01-02")}); err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if err := src.SaveSettings(map[string]string{"theme": "dark"}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	bundle, err := src.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if bundle.Version != exportVersion || bundle.App != "慎始" {
		t.Errorf("bundle 头 = v%d app=%q", bundle.Version, bundle.App)
	}
	if bundle.InboxListID <= 0 {
		t.Error("导出应记录收集箱 id")
	}

	// 换库导入（replace 清空目标库后精确重建）。
	dst := newTestStore(t)
	res, err := dst.Import(bundle, ImportReplace, nil)
	if err != nil {
		t.Fatalf("Import(replace): %v", err)
	}
	if res.Mode != ImportReplace || res.Tasks < 2 || res.Lists < 1 || res.Tags < 1 || res.Habits < 1 {
		t.Errorf("导入计数 = %+v", res)
	}

	// 按标题找回来，逐项核对。
	found, err := dst.ListTasks(TaskFilter{Status: "all", Search: "往返任务", SortBy: "manual", IncludeTaskArchived: true})
	if err != nil || len(found) == 0 {
		t.Fatalf("导入后查找: n=%d err=%v", len(found), err)
	}
	got := found[0]
	if got.DueDate == nil || *got.DueDate != "2026-12-24" {
		t.Errorf("dueDate = %v", got.DueDate)
	}
	if len(got.Subtasks) != 1 || got.Subtasks[0].Title != "子项" {
		t.Errorf("子任务未往返: %+v", got.Subtasks)
	}
	if len(got.Tags) != 1 || got.Tags[0].Name != "往返标签" {
		t.Errorf("标签未往返: %+v", got.Tags)
	}
	// 清单名对上。
	lists, _ := dst.Lists()
	ok := false
	for _, l := range lists {
		if l.Name == "往返清单" && l.ID == got.ListID {
			ok = true
		}
	}
	if !ok {
		t.Error("清单未按原样重建")
	}
	// 习惯与打卡流水。
	habits, err := dst.Habits(true)
	if err != nil {
		t.Fatalf("Habits: %v", err)
	}
	ok = false
	for _, hb := range habits {
		if hb.Name == "往返习惯" {
			ok = true
		}
	}
	if !ok {
		t.Error("习惯未往返")
	}
	logs, err := dst.AllHabitLogs()
	if err != nil {
		t.Fatalf("AllHabitLogs: %v", err)
	}
	if len(logs) == 0 {
		t.Error("打卡流水未往返")
	}
	// 设置。
	kv, _ := dst.Settings()
	if kv["theme"] != "dark" {
		t.Errorf("设置未往返: %+v", kv)
	}
	_ = task

	// merge 模式：目标库现有任务数只增不减，且追加了副本。
	before, err := dst.ListTasks(TaskFilter{Status: "all", SortBy: "manual", IncludeArchived: true, IncludeTaskArchived: true})
	if err != nil {
		t.Fatalf("导入前计数: %v", err)
	}
	res2, err := dst.Import(bundle, ImportMerge, nil)
	if err != nil {
		t.Fatalf("Import(merge): %v", err)
	}
	after, err := dst.ListTasks(TaskFilter{Status: "all", SortBy: "manual", IncludeArchived: true, IncludeTaskArchived: true})
	if err != nil {
		t.Fatalf("导入后计数: %v", err)
	}
	if len(after) != len(before)+res2.Tasks {
		t.Errorf("merge 后任务数 = %d，期望 %d（原 %d + 新 %d）", len(after), len(before)+res2.Tasks, len(before), res2.Tasks)
	}
}

func TestImportValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Import(&ExportBundle{Version: 1}, "destroy", nil); err == nil {
		t.Error("非法导入模式应被拒绝")
	} else {
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("应为 ValidationError，得到 %T", err)
		}
	}
	if _, err := s.Import(&ExportBundle{Version: 1}, ImportMerge, nil); err == nil {
		t.Error("空备份应被拒绝")
	}
	if _, err := s.Import(nil, ImportReplace, nil); err == nil {
		t.Error("nil 备份应被拒绝")
	}
}

// TestExportJSONAndCSV JSON 头字段与 CSV 的 BOM/表头。
func TestExportJSONAndCSV(t *testing.T) {
	s := newTestStore(t)
	mustCreateTask(t, s, "导出用任务", nil)

	data, err := s.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var b ExportBundle
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("JSON 解析: %v", err)
	}
	if b.Version != exportVersion || b.App != "慎始" {
		t.Errorf("JSON 头 = v%d app=%q", b.Version, b.App)
	}
	if len(b.Tasks) == 0 || b.ExportedAt == "" {
		t.Errorf("JSON 内容不完整: tasks=%d at=%q", len(b.Tasks), b.ExportedAt)
	}

	csv, err := s.ExportCSV()
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	if !strings.HasPrefix(string(csv), "\xEF\xBB\xBF") {
		t.Error("CSV 应以 UTF-8 BOM 开头，否则 Excel 乱码")
	}
	lines := strings.SplitN(string(csv), "\n", 3)
	if len(lines) < 2 || !strings.Contains(lines[0], "标题") {
		t.Errorf("CSV 表头 = %q", lines[0])
	}
	if !strings.Contains(string(csv), "导出用任务") {
		t.Error("CSV 应含任务行")
	}
}

// TestBackupHelpers 备份相关的纯函数口径。
func TestBackupHelpers(t *testing.T) {
	if boolWord(true) != "是" || boolWord(false) != "否" {
		t.Error("boolWord 中文口径")
	}
	if stamp("  ") == "" || !strings.HasPrefix(stamp("2026-01-02T00:00:00Z"), "2026") {
		t.Error("stamp 空白回落当前时间，非空原样")
	}
	// statusOr 只认 done / in_progress，其余（含自造状态）回落 todo。
	if statusOr(model.StatusDone) != model.StatusDone || statusOr(model.StatusInProgress) != model.StatusInProgress || statusOr("bogus") != "todo" {
		t.Errorf("statusOr: done=%q inprogress=%q bogus=%q", statusOr(model.StatusDone), statusOr(model.StatusInProgress), statusOr("bogus"))
	}
	if habitCadenceOr("weekly") != "weekly" || habitCadenceOr("bogus") != "daily" {
		t.Error("habitCadenceOr 回落 daily")
	}
	if habitTargetOr(0) != 1 || habitTargetOr(-3) != 1 || habitTargetOr(5) != 5 {
		t.Error("habitTargetOr 下限 1")
	}
	today := time.Now().Format("2006-01-02")
	if habitDayOr("bad") != today {
		t.Errorf("habitDayOr 非法日期回落今天，得到 %s", habitDayOr("bad"))
	}
	if habitDayOr("2026-09-23") != "2026-09-23" {
		t.Error("合法日期原样")
	}
	// merge 导入必须换存储名，否则新旧记录共用一个文件。
	n1, n2 := newStoredName("a.png"), newStoredName("a.png")
	if n1 == "a.png" || n1 == n2 || !strings.HasSuffix(n1, ".png") {
		t.Errorf("newStoredName = %q / %q", n1, n2)
	}
	if nonNil(nil) == nil || len(nonNil(nil)) != 0 {
		t.Error("nonNil(nil) 应为空切片")
	}
}

// TestBackupNameAndPrune 备份文件名白名单与按保留数裁剪。
func TestBackupNameAndPrune(t *testing.T) {
	okCases := []struct {
		name string
		want bool
	}{
		{"shenshi-backup-20260101-000000.zip", true},
		{"shenshi-backup-20260101-000000.json", true},
		{"other-backup.zip", false},
		{"shenshi-backup-20260101.tar", false},
		{"shenshi-backup-", false},
		{"notes.txt", false},
		{".backup-abc", false},
	}
	for _, c := range okCases {
		if got := backupNameOK(c.name); got != c.want {
			t.Errorf("backupNameOK(%q) = %v，期望 %v", c.name, got, c.want)
		}
	}

	dir := t.TempDir()
	// 5 份合法备份 + 1 份无关文件（不许被清理波及）。
	names := []string{
		"shenshi-backup-20260101-000000.zip",
		"shenshi-backup-20260102-000000.zip",
		"shenshi-backup-20260103-000000.zip",
		"shenshi-backup-20260104-000000.zip",
		"shenshi-backup-20260105-000000.zip",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("写备份文件: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("别删我"), 0o644); err != nil {
		t.Fatalf("写无关文件: %v", err)
	}

	// keep 足够时不裁。
	if removed, err := pruneBackups(dir, 10); err != nil || removed != 0 {
		t.Errorf("keep=10: removed=%d err=%v", removed, err)
	}
	// keep=2 → 删 3 份最旧的，字典序即时间序。
	removed, err := pruneBackups(dir, 2)
	if err != nil || removed != 3 {
		t.Fatalf("keep=2: removed=%d err=%v", removed, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) != 3 {
		t.Fatalf("剩余 = %v，应为 2 备份 + 1 无关", left)
	}
	hasNotes := false
	for _, n := range left {
		if n == "notes.txt" {
			hasNotes = true
		}
		if n == "shenshi-backup-20260101-000000.zip" || n == "shenshi-backup-20260103-000000.zip" {
			t.Errorf("旧备份应被裁掉，但 %s 还在", n)
		}
	}
	if !hasNotes {
		t.Error("无关文件不应被 prune 波及")
	}

	// keep=0 的语义是裁光（调用方 BackupOnce 会先把 0 回落成默认值，这里直测边界）。
	if removed, err := pruneBackups(dir, 0); err != nil || removed != 2 {
		t.Errorf("keep=0 应裁光: removed=%d err=%v", removed, err)
	}
	// 全部删完后再裁也不报错。
	if removed, err := pruneBackups(dir, 1); err != nil || removed != 0 {
		t.Errorf("空目录裁剪: removed=%d err=%v", removed, err)
	}
	// 目录不存在直接报错。
	if _, err := pruneBackups(filepath.Join(dir, "nope"), 1); err == nil {
		t.Error("目录不存在应报错")
	}
}

// TestListBackupsOnlyOwn 只认本应用的备份名，新的在前。
func TestListBackupsOnlyOwn(t *testing.T) {
	s := newTestStore(t)
	dir := s.BackupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// 目录不存在路径下先应为空。
	empty, err := s.ListBackups()
	if err != nil || len(empty) != 0 {
		t.Fatalf("初始应为空: %v %v", empty, err)
	}

	for _, n := range []string{
		"shenshi-backup-20260101-000000.zip",
		"shenshi-backup-20260103-000000.zip",
		"shenshi-backup-20260102-000000.json",
		"random.zip",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o644); err != nil {
			t.Fatalf("写文件: %v", err)
		}
	}
	got, err := s.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("只应含 3 份合法备份，得到 %v", got)
	}
	if got[0] != "shenshi-backup-20260103-000000.zip" {
		t.Errorf("新的在前，首条 = %q", got[0])
	}
}

// TestAutoBackupOnce 自动备份落盘、写入台账设置并按保留数裁剪。
func TestAutoBackupOnce(t *testing.T) {
	s := newTestStore(t)
	mustCreateTask(t, s, "自动备份任务", nil)

	ab := NewAutoBackup(s, func(format string, v ...any) {})
	// 先埋一份更旧的合法备份。
	old := filepath.Join(s.BackupDir(), "shenshi-backup-20200101-000000.zip")
	if err := os.MkdirAll(s.BackupDir(), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(old, []byte("stale"), 0o644); err != nil {
		t.Fatalf("写旧备份: %v", err)
	}

	path, err := ab.BackupOnce(1)
	if err != nil {
		t.Fatalf("BackupOnce: %v", err)
	}
	name := filepath.Base(path)
	if !backupNameOK(name) {
		t.Errorf("产出名 = %q", name)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("备份文件应存在: %v", err)
	}
	// keep=1：只留最新这份，旧的被裁掉。
	files, err := s.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(files) != 1 || files[0] != name {
		t.Errorf("保留结果 = %v，应只剩 %q", files, name)
	}
	// 台账写入设置。
	kv, _ := s.Settings()
	if kv[SetBackupLastFile] != name || kv[SetBackupLastError] != "" || kv[SetBackupLastAt] == "" {
		t.Errorf("备份台账设置 = file:%q err:%q at:%q", kv[SetBackupLastFile], kv[SetBackupLastError], kv[SetBackupLastAt])
	}
	// 非法 keep 回落默认：不报错即可（仍产出文件）。
	if _, err := ab.BackupOnce(0); err != nil {
		t.Errorf("keep=0 应回落默认而非报错: %v", err)
	}
}

// TestExportZIPAndOpenBackupFile 压缩包导出 → 落盘 → 读回；裸 JSON 与坏文件的识别。
func TestExportZIPAndOpenBackupFile(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "压缩包任务", nil)
	if _, err := s.SaveAttachment(task.ID, "note.txt", "text/plain", strings.NewReader("附件内容")); err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}

	zexp, err := s.ExportZIP()
	if err != nil {
		t.Fatalf("ExportZIP: %v", err)
	}
	if zexp.Files != 1 || zexp.Missing != 0 || len(zexp.Data) == 0 {
		t.Errorf("ZIP 导出 = files:%d missing:%d bytes:%d", zexp.Files, zexp.Missing, len(zexp.Data))
	}

	// 落临时文件再读回：包里应有清单与那 1 个附件。
	tmp := filepath.Join(t.TempDir(), "backup.zip")
	if err := os.WriteFile(tmp, zexp.Data, 0o644); err != nil {
		t.Fatalf("写临时文件: %v", err)
	}
	bf, err := OpenBackupFile(tmp)
	if err != nil {
		t.Fatalf("OpenBackupFile(zip): %v", err)
	}
	defer bf.Close()
	if bf.Bundle == nil || bf.Bundle.Version != exportVersion {
		t.Fatalf("zip 内 bundle = %+v", bf.Bundle)
	}
	if bf.Source == nil {
		t.Fatal("zip 应带附件源")
	}
	found := false
	for _, b := range bf.Bundle.Tasks {
		if b.Title == "压缩包任务" && len(b.Attachments) == 1 {
			found = true
		}
	}
	if !found {
		t.Error("zip 内任务/附件记录不全")
	}
	// 附件源能按存储名打开。
	var stored string
	for _, b := range bf.Bundle.Tasks {
		for _, a := range b.Attachments {
			stored = a.File
		}
	}
	rc, err := bf.Source.Open(stored)
	if err != nil {
		t.Fatalf("Source.Open: %v", err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "附件内容" {
		t.Errorf("附件内容 = %q", string(data))
	}

	// 裸 JSON：Source 为 nil。
	raw, err := s.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	jsonPath := filepath.Join(t.TempDir(), "backup.json")
	if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
		t.Fatalf("写 JSON: %v", err)
	}
	bf2, err := OpenBackupFile(jsonPath)
	if err != nil {
		t.Fatalf("OpenBackupFile(json): %v", err)
	}
	defer bf2.Close()
	if bf2.Bundle == nil || bf2.Bundle.App != "慎始" {
		t.Errorf("JSON bundle = %+v", bf2.Bundle)
	}
	if bf2.Source != nil {
		t.Error("裸 JSON 不带附件源")
	}

	// 不存在 / 坏文件。
	if _, err := OpenBackupFile(filepath.Join(t.TempDir(), "nope.zip")); err == nil {
		t.Error("不存在文件应报错")
	}
	bad := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(bad, []byte("这不是压缩包也不是 JSON"), 0o644); err != nil {
		t.Fatalf("写坏文件: %v", err)
	}
	if _, err := OpenBackupFile(bad); err == nil {
		t.Error("坏文件应报错")
	} else {
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("坏文件应为 ValidationError，得到 %T: %v", err, err)
		}
	}
}

// TestUndoExpire 撤销槽位过期后读取即清空（惰性清理，无后台协程）。
func TestUndoExpire(t *testing.T) {
	s := newTestStore(t)
	task := mustCreateTask(t, s, "过期撤销", nil)
	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	us, _ := s.UndoState()
	if !us.Available {
		t.Fatal("刚删除时槽位应可用")
	}

	// 把创建时间拨到一小时前（TTL 10 分钟）。
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE undo_slot SET created_at = ? WHERE id = 1`, past); err != nil {
		t.Fatalf("改时间: %v", err)
	}
	us, err := s.UndoState()
	if err != nil {
		t.Fatalf("UndoState: %v", err)
	}
	if us.Available {
		t.Error("过期槽位应已失效")
	}
	// 槽位行被清掉。
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM undo_slot`).Scan(&n); err != nil {
		t.Fatalf("查槽位: %v", err)
	}
	if n != 0 {
		t.Errorf("过期后应清空槽位行，残留 %d", n)
	}
	if _, err := s.Undo(); err == nil {
		t.Error("过期后撤销应报「没有可撤销的操作」")
	}
}
