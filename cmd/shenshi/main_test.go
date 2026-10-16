package main

import (
	"strings"
	"testing"
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
