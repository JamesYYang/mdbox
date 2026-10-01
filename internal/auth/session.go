// Package auth 提供最简单的登录会话管理：登录后签发一个随机 token，
// 服务端在内存里记住它对应的用户。进程重启后所有会话失效，需要重新登录。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// CookieName 是存放会话 token 的 Cookie 名。
const CookieName = "mdbox_session"

// TTL 是会话有效期。
const TTL = 30 * 24 * time.Hour

type session struct {
	user string
	exp  time.Time
}

// Sessions 是内存中的会话表，并发安全。
type Sessions struct {
	mu sync.Mutex
	m  map[string]session
}

// New 创建一个空的会话表。
func New() *Sessions {
	return &Sessions{m: map[string]session{}}
}

// Create 为指定用户签发一个新的会话 token。
func (s *Sessions) Create(user string) string {
	tok := randomHex(32)
	s.mu.Lock()
	s.m[tok] = session{user: user, exp: time.Now().Add(TTL)}
	s.mu.Unlock()
	return tok
}

// Valid 校验 token，返回其对应的用户名。
func (s *Sessions) Valid(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.m[tok]
	if !ok {
		return "", false
	}
	if time.Now().After(sess.exp) {
		delete(s.m, tok)
		return "", false
	}
	return sess.user, true
}

// Delete 注销一个会话 token。
func (s *Sessions) Delete(tok string) {
	s.mu.Lock()
	delete(s.m, tok)
	s.mu.Unlock()
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
