package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mdbox/internal/config"
	"mdbox/internal/store"
	"mdbox/internal/users"
)

// seedEditShared 造一篇 share_mode=edit 的文档，返回 id、签名与作者 store。
func seedEditShared(t *testing.T, reg *users.Registry, cfg *config.Config) (string, string, *store.Store) {
	t.Helper()
	if _, err := reg.Register("alice", "password123"); err != nil {
		t.Fatalf("注册 alice 失败: %v", err)
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	d, err := st.Create(store.CreateInput{
		Title: "源标题", Content: "# 正文\n\n内容",
		Tags: []string{"x"}, Category: "c", Source: "web",
	})
	if err != nil {
		t.Fatalf("创建文档失败: %v", err)
	}
	if _, err := st.SetShareMode(d.ID, store.ShareEdit); err != nil {
		t.Fatalf("设置可编辑失败: %v", err)
	}
	return d.ID, (&Server{cfg: cfg}).shareSig("alice", d.ID), st
}

func putShare(t *testing.T, h http.Handler, token, id, sig, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content})
	req := httptest.NewRequest(http.MethodPut, "/api/share/alice/"+id+"/"+sig, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEditSharedRequiresLogin(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, _ := seedEditShared(t, reg, cfg)
	rec := putShare(t, h, "", id, sig, "游客写入")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEditSharedRejectsViewMode(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, st := seedEditShared(t, reg, cfg)
	if _, err := st.SetShareMode(id, store.ShareView); err != nil {
		t.Fatalf("切只读失败: %v", err)
	}
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}
	rec := putShare(t, h, bob.Token, id, sig, "同事写入")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEditSharedWritesOriginal(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, st := seedEditShared(t, reg, cfg)
	before, err := st.Get(id)
	if err != nil {
		t.Fatalf("读取源文档失败: %v", err)
	}
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}
	rec := putShare(t, h, bob.Token, id, sig, "同事改过的正文")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	after, err := st.Get(id)
	if err != nil {
		t.Fatalf("读取源文档失败: %v", err)
	}
	if strings.TrimSpace(after.Content) != "同事改过的正文" {
		t.Errorf("正文 = %q, want 同事改过的正文", after.Content)
	}
	if after.Title != before.Title || after.Category != before.Category || after.Source != before.Source {
		t.Errorf("元数据被改动: before=%+v after=%+v", before, after)
	}
	if len(after.Tags) != 1 || after.Tags[0] != "x" {
		t.Errorf("标签被改动: %v", after.Tags)
	}
	if !after.Updated.After(before.Updated) {
		t.Errorf("updated 未前移: %v -> %v", before.Updated, after.Updated)
	}
}

func TestEditSharedRejectsBinaryAndOversize(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, _ := seedEditShared(t, reg, cfg)
	alice, ok := reg.Get("alice")
	if !ok {
		t.Fatal("alice 不存在")
	}
	bin := putShare(t, h, alice.Token, id, sig, "a\x00b")
	if bin.Code != http.StatusUnsupportedMediaType {
		t.Errorf("含 NUL 状态码 = %d, want 415", bin.Code)
	}
	big := putShare(t, h, alice.Token, id, sig, strings.Repeat("a", store.MaxDocBytes+1))
	if big.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超限状态码 = %d, want 413", big.Code)
	}
}

func TestEditSharedRevokedImmediately(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, st := seedEditShared(t, reg, cfg)
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}
	if _, err := st.SetShareMode(id, store.ShareOff); err != nil {
		t.Fatalf("关闭分享失败: %v", err)
	}
	rec := putShare(t, h, bob.Token, id, sig, "撤回后写入")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", rec.Code)
	}
}
