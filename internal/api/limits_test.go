package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mdbox/internal/i18n"
	"mdbox/internal/store"
)

func TestStoreErrStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"不存在", store.ErrNotFound, http.StatusNotFound},
		{"非文本", store.ErrNotText, http.StatusUnsupportedMediaType},
		{"超单篇上限", store.ErrDocTooLarge, http.StatusRequestEntityTooLarge},
		{"超配额", store.ErrQuotaExceeded, http.StatusRequestEntityTooLarge},
		{"未知错误", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		if got := storeErrStatus(c.err); got != c.want {
			t.Errorf("%s: storeErrStatus = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestWriteBadReqReportsBodyTooLarge 请求体被 http.MaxBytesReader 截断时要回 413 而不是笼统的 400。
func TestWriteBadReqReportsBodyTooLarge(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/docs", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxLang, i18n.ZH))

	writeBadReq(rec, req, i18n.KeyBadJSON, &http.MaxBytesError{Limit: 32 << 20})

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, want 413", rec.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if want := i18n.T(i18n.ZH, i18n.KeyBodyTooLarge); body.Error != want {
		t.Errorf("error = %q, want %q", body.Error, want)
	}
}

// TestCreateDocRejectsOversizedBody 端到端：超单篇上限的正文经 createDoc 回 413。
func TestCreateDocRejectsOversizedBody(t *testing.T) {
	reg, h, _ := newTestServer(t)
	u, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	payload, err := json.Marshal(map[string]string{
		"title":   "太大",
		"content": strings.Repeat("a", store.MaxDocBytes+1),
	})
	if err != nil {
		t.Fatalf("编码请求失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/docs", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+u.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态码 = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
}

// TestUploadRejectsBinaryRenamedToMd 上传一个内容不是文本的文件（哪怕叫 .md）要回 415。
func TestUploadRejectsBinaryRenamedToMd(t *testing.T) {
	reg, h, _ := newTestServer(t)
	u, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	// PNG 头 + NUL 字节，整体不是合法 UTF-8 文本
	binary := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0x00, 0xff, 0xfe}, 512)...)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "evil.md")
	if err != nil {
		t.Fatalf("构造 multipart 失败: %v", err)
	}
	if _, err := fw.Write(binary); err != nil {
		t.Fatalf("写入 multipart 失败: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("关闭 multipart 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+u.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("状态码 = %d, want 415; body=%s", rec.Code, rec.Body.String())
	}

	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	if docs, _ := st.List(store.ListFilter{Status: "all"}); len(docs) != 0 {
		t.Errorf("二进制被写入: %d 篇文档", len(docs))
	}
}

// TestUploadAcceptsUtf8Markdown 正常上传不受影响（回归：别把好文件一起挡了）。
func TestUploadAcceptsUtf8Markdown(t *testing.T) {
	reg, h, _ := newTestServer(t)
	u, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "note.md")
	_, _ = fw.Write([]byte("# 标题\n\n中文正文，带 emoji 👋\n"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+u.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
}
