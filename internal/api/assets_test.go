package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var pngHeader = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 64))

func postAsset(t *testing.T, h http.Handler, token string, data []byte, filename string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("构造 multipart: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("写 multipart: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/assets", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUploadAssetRequiresLogin(t *testing.T) {
	_, h, _ := newTestServer(t)
	if rec := postAsset(t, h, "", pngHeader, "a.png"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadAssetReturnsURL(t *testing.T) {
	reg, h, _ := newTestServer(t)
	alice, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册: %v", err)
	}
	rec := postAsset(t, h, alice.Token, pngHeader, "shot.png")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		URL   string `json:"url"`
		Name  string `json:"name"`
		Bytes int64  `json:"bytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if !strings.HasPrefix(got.URL, "/a/alice/") || !strings.HasSuffix(got.URL, ".png") {
		t.Fatalf("url = %q, want /a/alice/....png", got.URL)
	}
	if got.Bytes != int64(len(pngHeader)) {
		t.Fatalf("bytes = %d, want %d", got.Bytes, len(pngHeader))
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store: %v", err)
	}
	img, _, err := st.OpenImage(got.Name)
	if err != nil {
		t.Fatalf("图片没有真正落盘: %v", err)
	}
	// Windows 上不关句柄会让 t.TempDir 清理失败（文件仍被占用）。
	_ = img.Close()
}

func TestUploadAssetRejectsNonImage(t *testing.T) {
	reg, h, _ := newTestServer(t)
	alice, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册: %v", err)
	}
	svg := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><script>alert(1)</script></svg>")
	if rec := postAsset(t, h, alice.Token, svg, "x.svg"); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("状态码 = %d, want 415; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUploadAssetRejectsOversized(t *testing.T) {
	reg, h, _ := newTestServer(t)
	alice, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册: %v", err)
	}
	big := append([]byte(nil), pngHeader...)
	big = append(big, bytes.Repeat([]byte{0}, 5<<20)...) // > 5 MiB
	if rec := postAsset(t, h, alice.Token, big, "big.png"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAssetHandlerServesAnonymously(t *testing.T) {
	reg, h, _ := newTestServer(t)
	alice, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册: %v", err)
	}
	rec := postAsset(t, h, alice.Token, pngHeader, "shot.png")
	var up struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		t.Fatalf("解析上传响应: %v", err)
	}

	// 故意不带任何 cookie / token：分享页是匿名访客。
	req := httptest.NewRequest(http.MethodGet, up.URL, nil)
	got := httptest.NewRecorder()
	AssetHandler(reg).ServeHTTP(got, req)

	if got.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", got.Code)
	}
	if ct := got.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if cc := got.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control = %q, want immutable", cc)
	}
}

func TestAssetHandlerNotFound(t *testing.T) {
	reg, _, _ := newTestServer(t)
	if _, err := reg.Register("alice", "password123"); err != nil {
		t.Fatalf("注册: %v", err)
	}
	ah := AssetHandler(reg)
	for _, p := range []string{
		"/a/alice/" + strings.Repeat("a", 32) + ".png", // 格式对但文件不存在
		"/a/alice/notahash.png",
		"/a/bob/" + strings.Repeat("a", 32) + ".png", // 用户不存在
		"/a/alice/",
		"/a/alice/../x.png",
	} {
		rec := httptest.NewRecorder()
		ah.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s 状态码 = %d, want 404", p, rec.Code)
		}
	}
}
