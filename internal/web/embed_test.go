package web

import (
	"io/fs"
	"testing"
)

// HasBuild 与 FS() 必须自洽：有真实产物时 HasBuild 为真且 index.html 读得到；
// 只有占位 .gitkeep 时为假且读不到。两种状态都要成立——开发者本地跑过
// scripts/build.sh 后再跑测试，与 CI 上的干净 checkout 结果必须一致。
func TestHasBuildAndFSAgree(t *testing.T) {
	has := HasBuild()
	_, err := fs.Stat(FS(), "index.html")
	if has != (err == nil) {
		t.Fatalf("HasBuild()=%v 与 index.html 可读性不一致（stat err=%v）", has, err)
	}
}

// 无论有没有真实产物，FS() 都必须返回可用的文件系统：
// 有产物时 index.html 必然可读，只有占位时 .gitkeep 必然可读
// （CI 的 test job 会 rm -rf 后摆真实产物，两种状态都会出现）。
func TestFSAlwaysUsable(t *testing.T) {
	want := ".gitkeep"
	if HasBuild() {
		want = "index.html"
	}
	if _, err := fs.ReadFile(FS(), want); err != nil {
		t.Fatalf("FS() 读不到 %s: %v", want, err)
	}
}
