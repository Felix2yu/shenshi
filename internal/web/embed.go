// Package web 提供前端产物托管：优先内嵌产物，否则回退磁盘目录。
// 与 guanfu/huozhi 的范式一致：SPA 静态资源 + 回退逻辑由调用方（main）组装。
package web

import (
	"embed"
	"io/fs"
)

// 构建脚本会把前端产物复制到 internal/web/dist 后再编译，
// 从而单二进制自带前端。未构建时该目录只有占位文件，HasBuild 返回 false。
//
// 注意必须用 all: 前缀：前端产物里可能存在 _ 和 . 开头的条目，
// 而 go:embed 默认排除它们（经典陷阱）。
//
//go:embed all:dist
var embedded embed.FS

// HasBuild 判断是否内嵌了真实前端产物（dist 下存在 index.html）。
func HasBuild() bool {
	b, err := embedded.ReadFile("dist/index.html")
	return err == nil && len(b) > 0
}

// FS 返回内嵌产物的文件系统（调用方需先确认 HasBuild）。
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return embedded
	}
	return sub
}
