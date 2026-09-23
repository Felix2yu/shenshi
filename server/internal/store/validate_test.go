package store

import (
	"errors"
	"testing"
)

// TestCheckDay 覆盖日期白名单：真实日历放行、越界与格式拒绝。
func TestCheckDay(t *testing.T) {
	valid := []string{"2026-09-23", "2024-02-29", "1999-12-31", "2000-01-01"}
	for _, d := range valid {
		if err := CheckDay(d); err != nil {
			t.Errorf("CheckDay(%q) 应放行，却报错: %v", d, err)
		}
	}
	invalid := []string{
		"",
		"   ",
		"not-a-date",
		"2026-13-45", // 月、日都越界
		"2026-13-01", // 月越界
		"2026-02-31", // 2 月没有 31 日
		"26-01-02",   // 缺世纪
		"20260923",   // 缺分隔符
	}
	for _, d := range invalid {
		err := CheckDay(d)
		if err == nil {
			t.Errorf("CheckDay(%q) 应被拒绝", d)
			continue
		}
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("CheckDay(%q) 应返回 ValidationError，得到 %T", d, err)
		}
	}
}

// TestCheckDatePtr 校验可空日期：nil / 空白是「清空」语义必须放行，非空走日历校验。
func TestCheckDatePtr(t *testing.T) {
	if err := checkDatePtr(nil); err != nil {
		t.Errorf("nil 应放行（清空日期），却报错: %v", err)
	}
	blank := "   "
	if err := checkDatePtr(&blank); err != nil {
		t.Errorf("空白串应放行（清空日期），却报错: %v", err)
	}
	ok := "2026-09-23"
	if err := checkDatePtr(&ok); err != nil {
		t.Errorf("合法日期应放行，却报错: %v", err)
	}
	bad := "2026-02-31"
	if err := checkDatePtr(&bad); err == nil {
		t.Error("非法日期应被拒绝")
	}
}

// TestCheckTime 校验可空时刻：nil / 空白放行，HH:MM 与 HH:MM:SS 接受，其余拒绝。
func TestCheckTime(t *testing.T) {
	if err := checkTime(nil); err != nil {
		t.Errorf("nil 应放行，却报错: %v", err)
	}
	blank := "  "
	if err := checkTime(&blank); err != nil {
		t.Errorf("空白串应放行（清空时间），却报错: %v", err)
	}
	for _, v := range []string{"00:00", "08:30", "8:30", "23:59", "08:30:00"} {
		v := v
		if err := checkTime(&v); err != nil {
			t.Errorf("checkTime(%q) 应放行，却报错: %v", v, err)
		}
	}
	// 注：Go 布局 15 对小时只取 1~2 位，"8:30" 合法；此处只拦真正越界与错格式。
	for _, v := range []string{"25:00", "08:60", "0830", "08:30:99", "tomorrow"} {
		v := v
		if err := checkTime(&v); err == nil {
			t.Errorf("checkTime(%q) 应被拒绝", v)
		}
	}
}

// TestCheckPriority 优先级仅 0..3 四档。
func TestCheckPriority(t *testing.T) {
	for p := 0; p <= 3; p++ {
		if err := checkPriority(p); err != nil {
			t.Errorf("checkPriority(%d) 应放行，却报错: %v", p, err)
		}
	}
	for _, p := range []int{-1, 4, 99} {
		if err := checkPriority(p); err == nil {
			t.Errorf("checkPriority(%d) 应被拒绝", p)
		}
	}
}

// TestCheckReminders 提醒偏移 0..43200（30 天），含边界。
func TestCheckReminders(t *testing.T) {
	if err := checkReminders(nil); err != nil {
		t.Errorf("空提醒列表应放行，却报错: %v", err)
	}
	if err := checkReminders([]int{0, 15, 43200}); err != nil {
		t.Errorf("边界内提醒应放行，却报错: %v", err)
	}
	for _, r := range [][]int{{-1}, {43201}, {0, -5}} {
		if err := checkReminders(r); err == nil {
			t.Errorf("提醒 %v 应被拒绝", r)
		}
	}
}
