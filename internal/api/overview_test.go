package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mdbox/internal/store"
)

type overviewResp struct {
	Days     int `json:"days"`
	New      int `json:"new"`
	Updated  int `json:"updated"`
	Archived int `json:"archived"`
	Agent    int `json:"agent"`
}

func TestOverviewEndpoint(t *testing.T) {
	reg, h, _ := newTestServer(t)
	u, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	if _, err := st.Create(store.CreateInput{Title: "a", Content: "a", Source: "agent"}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/overview?days=7", nil)
	req.Header.Set("Authorization", "Bearer "+u.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got overviewResp
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if got.Days != 7 || got.New != 1 || got.Agent != 1 || got.Updated != 0 || got.Archived != 0 {
		t.Errorf("overview = %+v, want days=7 new=1 agent=1 updated=0 archived=0", got)
	}
}

func TestOverviewEndpointRequiresAuth(t *testing.T) {
	_, h, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录状态码 = %d, want 401", rec.Code)
	}
}

func TestOverviewDaysClamped(t *testing.T) {
	reg, h, _ := newTestServer(t)
	u, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	cases := []struct {
		query string
		want  int
	}{
		{"", 7},
		{"?days=0", 1},
		{"?days=999", 90},
		{"?days=abc", 7},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/api/overview"+c.query, nil)
		req.Header.Set("Authorization", "Bearer "+u.Token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: 状态码 = %d, want 200", c.query, rec.Code)
		}
		var got overviewResp
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%q: 解析失败: %v", c.query, err)
		}
		if got.Days != c.want {
			t.Errorf("%q: days = %d, want %d", c.query, got.Days, c.want)
		}
	}
}
