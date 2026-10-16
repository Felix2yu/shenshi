package store

import (
	"path/filepath"
	"testing"
)

// newTestStore 开一个临时库（含迁移与种子数据），测试结束随 TempDir 一起清理。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	return s
}

// TestStatsClampsDays 统计区间必须被钳进 [1, 366]：
// 趋势按天展开，超大入参会把请求打爆（审查缺陷之一）。
func TestStatsClampsDays(t *testing.T) {
	s := newTestStore(t)

	st, err := s.Stats(9999)
	if err != nil {
		t.Fatalf("Stats(9999): %v", err)
	}
	if len(st.Trend) != 366 {
		t.Errorf("超上限应钳到 366 天，得到 %d", len(st.Trend))
	}

	st, err = s.Stats(0)
	if err != nil {
		t.Fatalf("Stats(0): %v", err)
	}
	if len(st.Trend) != 30 {
		t.Errorf("非正数应回落默认 30 天，得到 %d", len(st.Trend))
	}

	st, err = s.Stats(-5)
	if err != nil {
		t.Fatalf("Stats(-5): %v", err)
	}
	if len(st.Trend) != 30 {
		t.Errorf("负数应回落默认 30 天，得到 %d", len(st.Trend))
	}

	st, err = s.Stats(14)
	if err != nil {
		t.Fatalf("Stats(14): %v", err)
	}
	if len(st.Trend) != 14 {
		t.Errorf("区间内应原样 14 天，得到 %d", len(st.Trend))
	}
}

// TestSaveSettingsInternalKeys 店内写入不设白名单：自动备份等内部路径
// 经 SaveSettings 落 autoBackupLastAt 等键（拦截外部请求在 API 层做）。
// 这条钉住「settingsDenied 曾误放进 store 层导致备份时间戳写不进去」的回归。
func TestSaveSettingsInternalKeys(t *testing.T) {
	s := newTestStore(t)

	err := s.SaveSettings(map[string]string{
		"theme":               "dark",
		"autoBackupLastAt":    "2099-01-01T00:00:00Z",
		"autoBackupLastFile":  "shenshi-backup-20990101-000000.zip",
		"autoBackupLastError": "",
	})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	kv, err := s.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if kv["theme"] != "dark" {
		t.Errorf("theme = %q", kv["theme"])
	}
	if kv["autoBackupLastAt"] != "2099-01-01T00:00:00Z" {
		t.Errorf("内部键 autoBackupLastAt 未落库: %q", kv["autoBackupLastAt"])
	}
	if v, ok := kv["autoBackupLastFile"]; !ok || v == "" {
		t.Errorf("内部键 autoBackupLastFile 未落库: %q", v)
	}
}
