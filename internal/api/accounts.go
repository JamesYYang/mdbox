package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"mdbox/internal/auth"
	"mdbox/internal/users"
)

// registerWindow 是注册限速的统计窗口：同一 IP 每个窗口内最多注册 5 次。
const registerWindow = time.Hour

// limiter 是按 key（IP）计数的滑动窗口限速器，仅存内存。
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

// allow 记一次请求，超限返回 false。
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var kept []time.Time
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) startSession(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    s.sessions.Create(name),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.TTL.Seconds()),
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	u, ok := s.reg.Authenticate(strings.TrimSpace(in.Username), in.Password)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	s.startSession(w, u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"user": u.Username})
}

// register 自助注册：校验用户名/密码，生成该用户的 MCP token，并直接登录。
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.RegisterOpen() {
		writeErr(w, http.StatusForbidden, "注册已关闭")
		return
	}
	var in credentials
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	if !s.regLimit.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "注册过于频繁，请稍后再试")
		return
	}
	u, err := s.reg.Register(strings.TrimSpace(in.Username), in.Password)
	switch {
	case errors.Is(err, users.ErrInvalidName), errors.Is(err, users.ErrWeakPassword):
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, users.ErrExists):
		writeErr(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.startSession(w, u.Username)
	writeJSON(w, http.StatusCreated, map[string]any{"user": u.Username})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		s.sessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// me 返回当前用户及其 MCP token，供 Web 的「MCP 接入」弹窗使用。
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, _ := s.reg.Get(userOf(r))
	writeJSON(w, http.StatusOK, map[string]any{"user": u.Username, "token": u.Token})
}

// changePassword 修改当前用户密码：需要旧密码；成功后注销该用户的其他登录会话。
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	name := userOf(r)
	switch err := s.reg.ChangePassword(name, in.Old, in.New); {
	case errors.Is(err, users.ErrWeakPassword), errors.Is(err, users.ErrWrongPassword):
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	keep := ""
	if c, err := r.Cookie(auth.CookieName); err == nil {
		keep = c.Value
	}
	s.sessions.DeleteUserExcept(name, keep)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// resetToken 重新生成当前用户的 token，旧 token 立即失效。
func (s *Server) resetToken(w http.ResponseWriter, r *http.Request) {
	tok, err := s.reg.ResetToken(userOf(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok})
}
