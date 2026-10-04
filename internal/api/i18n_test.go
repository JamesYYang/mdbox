package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mdbox/internal/i18n"
	"mdbox/internal/users"
)

func TestWriteErrLocalizesByRequestLanguage(t *testing.T) {
	cases := []struct {
		lang i18n.Lang
		want string
	}{
		{i18n.EN, i18n.T(i18n.EN, i18n.KeyUserExists)},
		{i18n.ZH, i18n.T(i18n.ZH, i18n.KeyUserExists)},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
		r = r.WithContext(context.WithValue(r.Context(), ctxLang, c.lang))
		writeErr(rec, r, http.StatusConflict, users.ErrExists.Error())

		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		if body["error"] != c.want {
			t.Errorf("语言 %s: error = %q, want %q", c.lang, body["error"], c.want)
		}
	}
}

func TestWriteErrPassesThroughUnknown(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	writeErr(rec, r, http.StatusInternalServerError, "boom")

	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "boom" {
		t.Errorf("未知文案应原样返回，got %q", body["error"])
	}
}
