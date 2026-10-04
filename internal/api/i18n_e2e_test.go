package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mdbox/internal/auth"
	"mdbox/internal/config"
	"mdbox/internal/i18n"
	"mdbox/internal/users"
)

// 走完整链路：Accept-Language 头 → FromAcceptLanguage → context → 鉴权失败 → writeErr。
func TestWithAuthUnauthorizedLocalizedFromHeader(t *testing.T) {
	dir := t.TempDir()
	reg, err := users.Open(filepath.Join(dir, "users.yaml"), dir)
	if err != nil {
		t.Fatalf("打开用户表失败: %v", err)
	}
	srv := &Server{reg: reg, cfg: &config.Config{}, sessions: auth.New(), regLimit: newLimiter(5, registerWindow)}
	h := srv.withAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("未鉴权请求不应到达受保护 handler")
	}))

	cases := []struct {
		accept string
		want   string
	}{
		{"zh-CN,zh;q=0.9,en;q=0.8", i18n.T(i18n.ZH, i18n.KeyUnauthorized)},
		{"en-US,en;q=0.9", i18n.T(i18n.EN, i18n.KeyUnauthorized)},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
		req.Header.Set("Accept-Language", c.accept)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Accept-Language=%q: 状态码 = %d, want 401", c.accept, rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		if body["error"] != c.want {
			t.Errorf("Accept-Language=%q: error = %q, want %q", c.accept, body["error"], c.want)
		}
	}
}
