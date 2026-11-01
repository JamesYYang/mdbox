package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mdbox/internal/store"
)

// postShareMode 以给定 token 调用分享开关接口。
func postShareMode(t *testing.T, h http.Handler, token, id, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/docs/"+id+"/share", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	out["_status"] = float64(rec.Code)
	return out
}

func TestShareModeThreeStates(t *testing.T) {
	reg, h, _ := newTestServer(t)
	alice, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	d, err := st.Create(store.CreateInput{Title: "文档", Content: "正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	out := postShareMode(t, h, alice.Token, d.ID, `{"mode":"edit"}`)
	if out["_status"] != float64(http.StatusOK) {
		t.Fatalf("状态码 = %v, want 200; body=%v", out["_status"], out)
	}
	if out["mode"] != "edit" {
		t.Errorf("mode = %v, want edit", out["mode"])
	}
	if out["url"] == nil || out["url"] == "" {
		t.Error("edit 模式应返回分享链接")
	}

	out = postShareMode(t, h, alice.Token, d.ID, `{"mode":"off"}`)
	if out["_status"] != float64(http.StatusOK) {
		t.Fatalf("状态码 = %v, want 200", out["_status"])
	}
	if _, has := out["url"]; has {
		t.Error("off 模式不应返回分享链接")
	}

	out = postShareMode(t, h, alice.Token, d.ID, `{"mode":"public"}`)
	if out["_status"] != float64(http.StatusBadRequest) {
		t.Errorf("非法模式状态码 = %v, want 400", out["_status"])
	}
}

// TestShareGoneWhenArchived 归档是软删除，但分享入口必须一起关掉：
// 否则已收起的文档仍能被匿名读到，edit 链接甚至能写进归档区。
func TestShareGoneWhenArchived(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	if _, err := reg.Register("alice", "password123"); err != nil {
		t.Fatalf("注册 alice 失败: %v", err)
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	d, err := st.Create(store.CreateInput{Title: "文档", Content: "# 正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := st.SetShareMode(d.ID, store.ShareEdit); err != nil {
		t.Fatalf("设置可编辑失败: %v", err)
	}
	sig := (&Server{cfg: cfg}).shareSig("alice", d.ID)
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}

	base := "/api/share/alice/" + d.ID + "/" + sig
	get := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}

	if code := get(base); code != http.StatusOK {
		t.Fatalf("归档前读取 = %d, want 200", code)
	}

	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("归档失败: %v", err)
	}

	if code := get(base); code != http.StatusNotFound {
		t.Errorf("归档后读取 = %d, want 404", code)
	}
	if code := get(base + "/download"); code != http.StatusNotFound {
		t.Errorf("归档后下载 = %d, want 404", code)
	}

	cl := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+d.ID+"/"+sig, strings.NewReader(""))
	cl.Header.Set("Authorization", "Bearer "+bob.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, cl)
	if rec.Code != http.StatusNotFound {
		t.Errorf("归档后 clone = %d, want 404", rec.Code)
	}

	if code := putShare(t, h, bob.Token, d.ID, sig, "偷偷改").Code; code != http.StatusNotFound {
		t.Errorf("归档后可编辑写入 = %d, want 404", code)
	}

	if _, err := st.Unarchive(d.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if code := get(base); code != http.StatusOK {
		t.Errorf("恢复后读取 = %d, want 200", code)
	}
	got, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if strings.Contains(got.Content, "偷偷改") {
		t.Errorf("归档期间的写入居然生效了: %q", got.Content)
	}
	if got.ShareMode != store.ShareEdit {
		t.Errorf("share_mode = %q, want edit（归档不该改动分享模式）", got.ShareMode)
	}
}
