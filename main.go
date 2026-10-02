// mdbox —— 一个在线 Markdown 文档管理工具。
//
// 用法：
//
//	mdbox -addr :8080 -data ./data            # 启动 Web + REST + MCP(HTTP)
//	mdbox -stdio -user alice -data ./data      # 以 MCP stdio 模式为 alice 运行（本地 agent 直连）
//
// 首次启动会在 -config 指定的路径（默认 ./config.yaml）生成配置文件，
// 并用其中的 admin 账号在 -users 指定的 users.yaml 里初始化第一个用户。
// 其他用户可自助注册；每个用户的文档在 {data}/users/{username}/ 下，
// 并各自拥有一个 MCP/API token（见 users.yaml 或 Web 的「MCP 接入」弹窗）。
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
	"sync"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"mdbox/internal/api"
	"mdbox/internal/auth"
	"mdbox/internal/config"
	mcpsrv "mdbox/internal/mcp"
	"mdbox/internal/users"
)

//go:embed web
var webFS embed.FS

const version = "0.3.0"

func main() {
	addr := flag.String("addr", ":8080", "HTTP 监听地址")
	dataDir := flag.String("data", "./data", "文档存储目录")
	configPath := flag.String("config", config.DefaultPath, "配置文件路径")
	usersPath := flag.String("users", "users.yaml", "用户注册表路径（含密码哈希与 token，不要放进 data/ 备份仓库）")
	stdioMode := flag.Bool("stdio", false, "以 MCP stdio 模式运行，供本地 agent 直连")
	stdioUser := flag.String("user", "", "stdio 模式下为哪个用户服务")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	reg, err := users.Open(*usersPath, *dataDir)
	if err != nil {
		log.Fatalf("加载用户表失败: %v", err)
	}
	// 用 config.yaml 的 admin 初始化第一个用户；已存在则不动。
	if err := users.ValidName(cfg.Admin.Username); err != nil {
		log.Fatalf("config.yaml 的 admin.username 不合法: %v", err)
	}
	if err := reg.EnsureUser(cfg.Admin.Username, cfg.Admin.Password); err != nil {
		log.Fatalf("初始化 %s 失败: %v", cfg.Admin.Username, err)
	}

	mcps := &mcpServers{reg: reg, m: map[string]*server.MCPServer{}}

	if *stdioMode {
		if *stdioUser == "" {
			log.Fatalf("stdio 模式需要用 -user 指定用户")
		}
		srv, err := mcps.get(*stdioUser)
		if err != nil {
			log.Fatalf("stdio 模式无法为用户 %q 服务: %v", *stdioUser, err)
		}
		if err := server.ServeStdio(srv); err != nil && err != context.Canceled {
			log.Fatalf("stdio 模式异常: %v", err)
		}
		return
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
	mux.Handle("/api/", api.New(reg, cfg, sessions))
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		u, ok := reg.ByToken(api.Bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		srv, err := mcps.get(u.Username)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		server.NewStreamableHTTPServer(srv).ServeHTTP(w, r)
	})
	// 分享链接：匿名访问，交给前端按路径进入「分享模式」（只读）。
	mux.HandleFunc("GET /s/{user}/{id}/{sig}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.Handle("/", http.FileServer(http.FS(webRoot)))

	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		log.Printf("mdbox %s 启动于 %s，文档目录 %s，用户表 %s，开放注册: %v",
			version, *addr, mustAbs(*dataDir), *usersPath, cfg.RegisterOpen())
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

// mcpServers 按用户缓存 MCP server：每个 server 只绑定该用户自己的文档仓库。
type mcpServers struct {
	reg *users.Registry
	mu  sync.Mutex
	m   map[string]*server.MCPServer
}

func (c *mcpServers) get(name string) (*server.MCPServer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.m[name]; ok {
		return s, nil
	}
	st, err := c.reg.Store(name)
	if err != nil {
		return nil, err
	}
	s := mcpsrv.New(st, "mdbox", version)
	c.m[name] = s
	return s, nil
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
