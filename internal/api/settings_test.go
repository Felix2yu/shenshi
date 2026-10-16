package api

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yufei/shendu/server/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	return New(st, ""), st
}

// TestPutSettingsSkipsInternalKeys 外部请求夹带服务端内部键时静默跳过、
// 其余键照常保存 —— 前端 saveSettings 是全量合并回传，回 400 会把改主题
// 一起打挂；同时内部键不能经此落库（那是 store 内部路径的事）。
func TestPutSettingsSkipsInternalKeys(t *testing.T) {
	s, st := newTestServer(t)

	body := `{"theme":"light","autoBackupLastAt":"2099-01-01T00:00:00Z","autoBackupLastFile":"evil.zip","autoBackupLastError":"x"}`
	req := httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body))
	w := httptest.NewRecorder()
	if err := s.putSettings(w, req); err != nil {
		t.Fatalf("putSettings 返回错误: %v", err)
	}

	kv, err := st.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if kv["theme"] != "light" {
		t.Errorf("正常键应保存，theme = %q", kv["theme"])
	}
	for _, k := range []string{"autoBackupLastAt", "autoBackupLastFile", "autoBackupLastError"} {
		if v, ok := kv[k]; ok {
			t.Errorf("内部键 %s 不应被外部请求写入，现有值 %q", k, v)
		}
	}
}

// TestQueryIntClamped 数值入参钳制：防超大 days 打爆趋势循环。
func TestQueryIntClamped(t *testing.T) {
	cases := []struct {
		qs   string
		def  int
		min  int
		max  int
		want int
	}{
		{"?days=9999", 30, 0, 366, 366}, // 超上限
		{"?days=-5", 30, 0, 366, 0},     // 低于下限
		{"?days=14", 30, 0, 366, 14},    // 区间内原样
		{"", 30, 0, 366, 30},            // 缺省给默认值
		{"?days=abc", 30, 0, 366, 30},   // 非数字回落默认
		{"?days=0", 30, 1, 366, 1},      // 压在下限上
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/api/stats"+c.qs, nil)
		if got := queryIntClamped(r, "days", c.def, c.min, c.max); got != c.want {
			t.Errorf("queryIntClamped(%q) = %d，期望 %d", c.qs, got, c.want)
		}
	}
}

// TestQueryHelpers queryInt / queryStr / queryInt64 的缺省与容错。
func TestQueryHelpers(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/x?a=42&bad=xx&s=hi&n=123", nil)

	if got := queryInt(r, "a", 7); got != 42 {
		t.Errorf("queryInt(a) = %d，期望 42", got)
	}
	if got := queryInt(r, "bad", 7); got != 7 {
		t.Errorf("queryInt(非数字) = %d，应回落默认 7", got)
	}
	if got := queryInt(r, "missing", 7); got != 7 {
		t.Errorf("queryInt(缺省) = %d，应为 7", got)
	}

	if v := queryStr(r, "s"); v == nil || *v != "hi" {
		t.Errorf("queryStr(s) = %v", v)
	}
	if v := queryStr(r, "missing"); v != nil {
		t.Errorf("queryStr(缺省) 应为 nil，得到 %v", *v)
	}

	if v := queryInt64(r, "n"); v == nil || *v != 123 {
		t.Errorf("queryInt64(n) = %v", v)
	}
	if v := queryInt64(r, "bad"); v != nil {
		t.Errorf("queryInt64(非数字) 应为 nil")
	}
	if v := queryInt64(r, "missing"); v != nil {
		t.Errorf("queryInt64(缺省) 应为 nil")
	}
}
