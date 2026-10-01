// mdbox —— 一个在线 Markdown 文档管理工具。
//
// 用法：
//
//	mdbox -addr :8080 -data ./data            # 启动 Web + REST + MCP(HTTP)
//	mdbox -stdio -data ./data                  # 以 MCP stdio 模式运行（本地 agent 直连）
//	MDBOX_TOKEN=xxx mdbox                      # 临时覆盖 config.yaml 里的 agent 令牌
//
// 首次启动会在 -config 指定的路径（默认 ./config.yaml）生成配置文件，
// 内含管理员账号（Web 登录用）与 agent 令牌 token（/api、/mcp 用）。
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"mdbox/internal/api"
	"mdbox/internal/auth"
	"mdbox/internal/config"
	mcpsrv "mdbox/internal/mcp"
	"mdbox/internal/store"
)

//go:embed web
var webFS embed.FS

const version = "0.2.0"

func main() {
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	dataDir := flag.String("data", "./data", "文档存储目录")
	configPath := flag.String("config", config.DefaultPath, "配置文件路径")
	token := flag.String("token", os.Getenv("MDBOX_TOKEN"), "agent/脚本用的访问令牌；留空则用 config.yaml 里的 token")
	stdioMode := flag.Bool("stdio", false, "以 MCP stdio 模式运行，供本地 agent 直连")
	flag.Parse()

	st, err := store.New(*dataDir)
	if err != nil {
		log.Fatalf("打开文档仓库失败: %v", err)
	}

	mcpServer := mcpsrv.New(st, "mdbox", version)

	if *stdioMode {
		if err := server.ServeStdio(mcpServer); err != nil && err != context.Canceled {
			log.Fatalf("stdio 模式异常: %v", err)
		}
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	// 令牌优先级：-token / MDBOX_TOKEN > config.yaml 的 token
	tok := *token
	if tok == "" {
		tok = cfg.Token
	}
	sessions := auth.New()

	webRoot, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("加载前端资源失败: %v", err)
	}
	indexHTML, err := fs.ReadFile(webRoot, "index.html")
	if err != nil {
		log.Fatalf("加载首页失败: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", api.New(st, cfg, sessions, tok))
	mux.Handle("/mcp", withToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.NewStreamableHTTPServer(mcpServer).ServeHTTP(w, r)
	}), tok))
	// 分享链接：匿名访问，交给前端按路径进入「分享模式」（只读）。
	mux.HandleFunc("GET /s/{id}/{sig}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.Handle("/", http.FileServer(http.FS(webRoot)))

	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		log.Printf("mdbox %s 启动于 %s，文档目录 %s，管理员 %s", version, *addr, mustAbs(*dataDir), cfg.Admin.Username)
		if tok == "" {
			log.Printf("提示：未设置访问令牌，agent/脚本无法访问 /api 与 /mcp")
		} else {
			log.Printf("agent/MCP 令牌已启用（配置见 %s 的 token 字段）", *configPath)
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务异常: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Print("已停止")
}

func withToken(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if got != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func mustAbs(p string) string {
	if a, err := fsPath(p); err == nil {
		return a
	}
	return p
}

func fsPath(p string) (string, error) {
	if len(p) > 0 && p[0] == '/' {
		return p, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return wd + "/" + p, nil
}
