// mdbox —— 一个在线 Markdown 文档管理工具。
//
// 用法：
//
//	mdbox -addr :8080 -data ./data            # 启动 Web + REST + MCP(HTTP)
//	mdbox -stdio -user alice -data ./data      # 以 MCP stdio 模式为 alice 运行（本地 agent 直连）
//
// 首次启动会在 -config 指定的路径（默认 ./config.yaml）生成配置文件，
// 不预置任何账号，用户通过 Web 自助注册（可用 allow_register 关闭）；每个用户的文档在 {data}/users/{username}/ 下，
// 并各自拥有一个 MCP/API token（见 users.yaml 或 Web 的「MCP 接入」弹窗）。
package main

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
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
	mux.Handle("/", staticFiles(webRoot))

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

// staticFiles 提供内嵌的 web/ 静态资源：gzip 压缩 + 基于 ETag 的协商缓存。
// 浏览器里唯一的大文件 html2pdf 已改为按需加载，这里再兜住传输体积与重复下载：
// 文本资源经 gzip 通常只剩约 1/4，重复访问走 304 不再重传。
func staticFiles(root fs.FS) http.Handler {
	etags := map[string]string{}
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(root, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		etags["/"+p] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})

	fileServer := http.FileServer(http.FS(root))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		comp := compressible(p)
		if comp {
			w.Header().Add("Vary", "Accept-Encoding")
		}
		// 首页（"/"，以及会被 301 到 "/" 的显式 /index.html）不参与协商缓存，交给 FileServer。
		if p != "/" && p != "/index.html" {
			if et, ok := etags[p]; ok {
				w.Header().Set("ETag", et)
				w.Header().Set("Cache-Control", "no-cache")
				if r.Header.Get("If-None-Match") == et {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}
		if !comp || r.Method != http.MethodGet || r.Header.Get("Range") != "" ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			fileServer.ServeHTTP(w, r)
			return
		}
		gz := gzip.NewWriter(w)
		gw := &gzipWriter{ResponseWriter: w, gz: gz}
		fileServer.ServeHTTP(gw, r)
		if gw.compressed {
			_ = gz.Close()
		}
	})
}

// gzipWriter 仅在响应为 200 时改写为 gzip 输出；其余状态（304/404/301 等）原样透传。
type gzipWriter struct {
	http.ResponseWriter
	gz         *gzip.Writer
	wroteHead  bool
	compressed bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if g.wroteHead {
		return
	}
	g.wroteHead = true
	if code == http.StatusOK {
		g.compressed = true
		g.Header().Del("Content-Length") // 压缩后长度未知，交给分块编码
		g.Header().Set("Content-Encoding", "gzip")
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHead {
		g.WriteHeader(http.StatusOK)
	}
	if g.compressed {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// compressible 判断按扩展名是否值得 gzip（文本类；png 等已压缩格式跳过）。
func compressible(p string) bool {
	if p == "/" { // 首页实际返回 index.html
		return true
	}
	switch path.Ext(p) {
	case ".js", ".css", ".html", ".htm", ".svg", ".json", ".txt", ".map", ".xml":
		return true
	}
	return false
}
