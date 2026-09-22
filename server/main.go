// 「慎始」服务端入口。
//
// 单进程同时承担 REST API 与前端静态资源托管；前端构建产物通过 go:embed 打进二进制，
// 因此交付物就是一个可执行文件（外加一个 SQLite 数据文件）。
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yufei/shendu/server/internal/api"
	"github.com/yufei/shendu/server/internal/store"
)

//go:embed all:dist
var embedded embed.FS

func main() {
	addr := flag.String("addr", ":8787", "监听地址")
	dbPath := flag.String("db", defaultDBPath(), "SQLite 数据文件路径")
	devDir := flag.String("web", "", "使用磁盘上的前端目录（开发模式），留空则使用内嵌资源")
	token := flag.String("token", os.Getenv("SHENSHI_TOKEN"), "访问口令，留空则不启用鉴权（亦可用环境变量 SHENSHI_TOKEN）")
	flag.Parse()

	if err := run(*addr, *dbPath, *devDir, *token); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func defaultDBPath() string {
	if v := os.Getenv("SHENSHI_DB"); v != "" {
		return v
	}
	return filepath.Join("data", "shenshi.db")
}

func run(addr, dbPath, devDir, token string) error {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := api.New(st, token)
	handler, err := staticHandler(devDir)
	if err != nil {
		return err
	}
	srv.HandleStatic(handler)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("慎始 · 慎始而敬终，行稳致远")
		log.Printf("服务已启动: http://localhost%s", normalizeAddr(addr))
		log.Printf("数据文件: %s", dbPath)
		if strings.TrimSpace(token) == "" {
			log.Printf("访问口令: 未设置（任何能访问该端口的人都可以读写数据，公网部署请设置 SHENSHI_TOKEN）")
		} else {
			log.Printf("访问口令: 已启用（来自 -token 或 SHENSHI_TOKEN）")
		}
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("监听失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭服务…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpSrv.Shutdown(ctx)
}

func normalizeAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		return addr[len("0.0.0.0"):]
	}
	return ":" + strings.TrimPrefix(addr, "localhost:")
}

// staticHandler 返回前端资源处理器。
// 优先使用 -web 指定的磁盘目录；否则使用内嵌资源；若内嵌资源为空（未执行前端构建），
// 则回退到 ./web/dist，保证 `go run ./server` 在开发机上也能直接用。
func staticHandler(devDir string) (http.Handler, error) {
	if devDir != "" {
		abs, err := filepath.Abs(devDir)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
			return nil, errors.New("指定的前端目录缺少 index.html: " + abs)
		}
		log.Printf("前端资源: %s（开发模式）", abs)
		return spaHandler(os.DirFS(abs)), nil
	}

	dist, err := fs.Sub(embedded, "dist")
	if err == nil {
		if _, err := fs.Stat(dist, "index.html"); err == nil {
			log.Printf("前端资源: 内嵌资源")
			return spaHandler(dist), nil
		}
	}
	if _, err := os.Stat(filepath.Join("web", "dist", "index.html")); err == nil {
		log.Printf("前端资源: web/dist（回退）")
		return spaHandler(os.DirFS(filepath.Join("web", "dist"))), nil
	}
	return nil, errors.New("未找到前端资源，请先执行 `npm run build`（或使用 -web 指定目录）")
}

// spaHandler 托管静态文件，并对未知路径回退到 index.html，以支持前端路由。
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if clean == "." || clean == "/" || clean == "" {
			clean = "index.html"
		}
		if st, err := fs.Stat(fsys, clean); err != nil || st.IsDir() {
			// 构建产物缺失应当如实报 404，否则浏览器会把 HTML 当 JS/CSS 缓存下来。
			if strings.HasPrefix(clean, "assets/") {
				http.NotFound(w, r)
				return
			}
			data, err := fs.ReadFile(fsys, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		// 带内容哈希的构建产物可以长期缓存。
		if strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}
