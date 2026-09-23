package store

import (
	"strings"
	"time"
)

// 本文件收拢「入库前」的校验口径：api 层与 store 层共用一套规则，
// 避免再长出第三套只认格式不认日历的 isDate。

// CheckDay 导出给 api 层复用（GET /api/reviews/{date} 等）。
func CheckDay(day string) error { return checkDay(day) }

// checkDatePtr 校验可空日期：nil 与空白串表示「清空」，放行；
// 非空必须是真实存在的 YYYY-MM-DD（time.Parse 会拒绝 2026-99-99）。
func checkDatePtr(p *string) error {
	if p == nil {
		return nil
	}
	if strings.TrimSpace(*p) == "" {
		return nil
	}
	return checkDay(*p)
}

// checkTime 校验可空时刻：nil 与空白串表示「清空」，放行；
// 接受 HH:MM 与 HH:MM:SS（前端 input[type=time] 产出前者）。
func checkTime(p *string) error {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	if _, err := time.Parse("15:04", v); err == nil {
		return nil
	}
	if _, err := time.Parse("15:04:05", v); err == nil {
		return nil
	}
	return ValidationError{Msg: "时间格式应为 HH:MM"}
}

// checkPriority 校验优先级档位：0-3（无/低/中/高）。
func checkPriority(p int) error {
	if p < 0 || p > 3 {
		return ValidationError{Msg: "优先级应在 0 ~ 3 之间"}
	}
	return nil
}

// checkReminders 校验提醒偏移（分钟）：0 表示到点提醒，上限 30 天。
func checkReminders(r []int) error {
	for _, v := range r {
		if v < 0 || v > 43200 {
			return ValidationError{Msg: "提醒偏移应在 0 ~ 43200 分钟之间"}
		}
	}
	return nil
}
