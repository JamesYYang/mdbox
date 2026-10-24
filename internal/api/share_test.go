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
