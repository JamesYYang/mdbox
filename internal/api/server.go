package api

import (
	"context"
	"net/http"
	"strings"

	"mdbox/internal/auth"
	"mdbox/internal/config"
	"mdbox/internal/store"
	"mdbox/internal/users"
)

// Server 暴露 REST API。
type Server struct {
	reg      *users.Registry
	cfg      *config.Config
	sessions *auth.Sessions
	regLimit *limiter
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxStore
)

// stOf 取出当前请求所属用户的文档仓库（由 withAuth 注入）。
func stOf(r *http.Request) *store.Store { return r.Context().Value(ctxStore).(*store.Store) }

// userOf 取出当前请求的登录用户名（由 withAuth 注入）。
func userOf(r *http.Request) string { return r.Context().Value(ctxUser).(string) }

// New 返回挂载好路由的 http.Handler。
//
// 鉴权分两套：Web 端用登录会话（Cookie），agent/脚本用该用户自己的 token（Bearer）。
// 两者都会解析出用户，之后所有文档操作只作用于该用户自己的目录。
func New(reg *users.Registry, cfg *config.Config, sessions *auth.Sessions) http.Handler {
	s := &Server{reg: reg, cfg: cfg, sessions: sessions, regLimit: newLimiter(5, registerWindow)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/register", s.register)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("POST /api/me/token", s.resetToken)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/docs", s.listDocs)
	mux.HandleFunc("POST /api/docs", s.createDoc)
	mux.HandleFunc("GET /api/docs/{id}", s.getDoc)
	mux.HandleFunc("PUT /api/docs/{id}", s.updateDoc)
	mux.HandleFunc("DELETE /api/docs/{id}", s.deleteDoc)
	mux.HandleFunc("POST /api/docs/{id}/archive", s.archiveDoc)
	mux.HandleFunc("POST /api/docs/{id}/share", s.shareDoc)
	mux.HandleFunc("GET /api/docs/{id}/download", s.downloadDoc)
	mux.HandleFunc("GET /api/share/{user}/{id}/{token}", s.publicDoc)
	mux.HandleFunc("GET /api/share/{user}/{id}/{token}/download", s.publicDownload)
	mux.HandleFunc("GET /api/tags", s.tags)
	mux.HandleFunc("GET /api/categories", s.categories)
	mux.HandleFunc("POST /api/upload", s.upload)
	mux.HandleFunc("POST /api/preview", s.preview)
	return s.withAuth(mux)
}

// publicPath 判断某路径是否无需登录：登录/注册/登出接口、公开分享读取。
func publicPath(p string) bool {
	switch p {
	case "/api/login", "/api/register", "/api/logout":
		return true
	}
	return strings.HasPrefix(p, "/api/share/")
}

// Bearer 取出请求里的 token（Authorization 头优先，其次 ?token=）。
func Bearer(r *http.Request) string {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	return got
}

// identify 解析请求对应的用户名：先看登录会话，再看用户 token。
func (s *Server) identify(r *http.Request) (string, bool) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		if name, ok := s.sessions.Valid(c.Value); ok {
			if _, exists := s.reg.Get(name); exists {
				return name, true
			}
		}
	}
	if u, ok := s.reg.ByToken(Bearer(r)); ok {
		return u.Username, true
	}
	return "", false
}

func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		name, ok := s.identify(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		st, err := s.reg.Store(name)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, name)
		ctx = context.WithValue(ctx, ctxStore, st)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
