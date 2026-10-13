package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLimitBodyCapsRequestSize 请求体超限时，读取必须失败并给出 *http.MaxBytesError
// （api 层据此回 413），否则超大上传会一路写进磁盘。
func TestLimitBodyCapsRequestSize(t *testing.T) {
	var readErr error
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/upload",
		strings.NewReader(strings.Repeat("a", maxRequestBytes+1)))
	h.ServeHTTP(httptest.NewRecorder(), req)

	var mbe *http.MaxBytesError
	if !errors.As(readErr, &mbe) {
		t.Fatalf("读取错误 = %v, want *http.MaxBytesError", readErr)
	}
}

func TestLimitBodyAllowsBodyAtLimit(t *testing.T) {
	var (
		readErr error
		n       int
	)
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b []byte
		b, readErr = io.ReadAll(r.Body)
		n = len(b)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/docs",
		strings.NewReader(strings.Repeat("a", maxRequestBytes)))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if readErr != nil {
		t.Fatalf("正好等于上限不应报错: %v", readErr)
	}
	if n != maxRequestBytes {
		t.Errorf("读到 %d 字节, want %d", n, maxRequestBytes)
	}
}

// TestSecurityHeadersPresent CSP 是渲染层之外的第二道防线，别在重构里被摘掉。
func TestSecurityHeadersPresent(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self'", "object-src 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP 缺少 %q；当前值 %q", want, csp)
		}
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
