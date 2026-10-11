package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mdbox/internal/auth"
	"mdbox/internal/config"
	"mdbox/internal/store"
	"mdbox/internal/users"
)

// newTestServer 建一个带真实注册表与临时目录的 api handler，并返回注册表与配置。
func newTestServer(t *testing.T) (*users.Registry, http.Handler, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	reg, err := users.Open(filepath.Join(dir, "users.yaml"), dir)
	if err != nil {
		t.Fatalf("打开用户表失败: %v", err)
	}
	cfg := &config.Config{Secret: "test-secret"}
	h := New(reg, cfg, auth.New())
	return reg, h, cfg
}

func TestCloneSharedDocCreatesCopyInCallerLibrary(t *testing.T) {
	reg, h, cfg := newTestServer(t)

	if _, err := reg.Register("alice", "password123"); err != nil {
		t.Fatalf("注册 alice 失败: %v", err)
	}
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}

	aliceStore, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 alice store 失败: %v", err)
	}
	src, err := aliceStore.Create(store.CreateInput{
		Title: "源标题", Content: "# 正文\n\n内容", Tags: []string{"x"}, Category: "c", Source: "web",
	})
	if err != nil {
		t.Fatalf("创建源文档失败: %v", err)
	}
	if _, err := aliceStore.SetShared(src.ID, true); err != nil {
		t.Fatalf("开启分享失败: %v", err)
	}
	srcDisk, err := aliceStore.Get(src.ID) // 从磁盘读回，正文含末尾换行，便于精确比较
	if err != nil {
		t.Fatalf("读取源文档失败: %v", err)
	}

	sig := (&Server{cfg: cfg}).shareSig("alice", src.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+src.ID+"/"+sig, nil)
	req.Header.Set("Authorization", "Bearer "+bob.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Doc store.Doc `json:"doc"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Doc.Source != "clone" {
		t.Errorf("source = %q, want clone", resp.Doc.Source)
	}
	if len(resp.Doc.Tags) != 0 {
		t.Errorf("tags = %v, want empty", resp.Doc.Tags)
	}
	if resp.Doc.Category != "" {
		t.Errorf("category = %q, want empty", resp.Doc.Category)
	}
	if resp.Doc.Shared {
		t.Errorf("shared = true, want false")
	}
	if resp.Doc.Title != "源标题" {
		t.Errorf("title = %q, want 源标题", resp.Doc.Title)
	}

	// 副本落在 bob 自己的库里，正文与源一致
	bobStore, err := reg.Store("bob")
	if err != nil {
		t.Fatalf("取 bob store 失败: %v", err)
	}
	got, err := bobStore.Get(resp.Doc.ID)
	if err != nil {
		t.Fatalf("bob 库中找不到副本: %v", err)
	}
	if got.Content != srcDisk.Content {
		t.Errorf("副本正文 = %q, want %q", got.Content, srcDisk.Content)
	}

	// 源文档元数据不变
	after, err := aliceStore.Get(src.ID)
	if err != nil {
		t.Fatalf("再次读取源文档失败: %v", err)
	}
	if len(after.Tags) != 1 || after.Tags[0] != "x" || after.Category != "c" {
		t.Errorf("源文档元数据被改动: tags=%v category=%q", after.Tags, after.Category)
	}
}

func TestCloneSharedDocUnauthorized(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	reg.Register("alice", "password123")
	aliceStore, _ := reg.Store("alice")
	src, _ := aliceStore.Create(store.CreateInput{Title: "T", Content: "b", Source: "web"})
	aliceStore.SetShared(src.ID, true)
	sig := (&Server{cfg: cfg}).shareSig("alice", src.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+src.ID+"/"+sig, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录状态码 = %d, want 401", rec.Code)
	}
}

func TestCloneUnsharedDocNotFound(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	reg.Register("alice", "password123")
	bob, _ := reg.Register("bob", "password123")
	aliceStore, _ := reg.Store("alice")
	src, _ := aliceStore.Create(store.CreateInput{Title: "T", Content: "b", Source: "web"}) // 未开启分享
	sig := (&Server{cfg: cfg}).shareSig("alice", src.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+src.ID+"/"+sig, nil)
	req.Header.Set("Authorization", "Bearer "+bob.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("未分享文档状态码 = %d, want 404", rec.Code)
	}
}

func TestCloneBadSignatureNotFound(t *testing.T) {
	reg, h, _ := newTestServer(t)
	reg.Register("alice", "password123")
	bob, _ := reg.Register("bob", "password123")
	aliceStore, _ := reg.Store("alice")
	src, _ := aliceStore.Create(store.CreateInput{Title: "T", Content: "b", Source: "web"})
	aliceStore.SetShared(src.ID, true)

	// bob 已登录，但签名伪造 → 应 404（端点的安全边界）
	req := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+src.ID+"/forged-signature", nil)
	req.Header.Set("Authorization", "Bearer "+bob.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("伪造签名状态码 = %d, want 404", rec.Code)
	}
}
