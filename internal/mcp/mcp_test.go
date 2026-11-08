package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mdbox/internal/i18n"
	"mdbox/internal/users"
)

func TestErrTextResolvesKeysToEnglish(t *testing.T) {
	want := i18n.T(i18n.EN, i18n.KeyUserNotFound)
	if got := errText(users.ErrNotFound); got != want {
		t.Errorf("errText(ErrNotFound) = %q, want %q", got, want)
	}
}

func TestErrTextPassesThroughUnknown(t *testing.T) {
	if got := errText(errString("boom")); got != "boom" {
		t.Errorf("未知错误应原样返回，got %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestImageBytesRejectsBothSources(t *testing.T) {
	if _, err := imageBytes("a.png", "AAAA", true); err == nil || !strings.Contains(err.Error(), "either") {
		t.Fatalf("err = %v, want 提示二选一", err)
	}
}

func TestImageBytesRejectsEmpty(t *testing.T) {
	if _, err := imageBytes("", "", false); err == nil || !strings.Contains(err.Error(), "data_base64") {
		t.Fatalf("err = %v, want 提示提供 data_base64", err)
	}
}

func TestImageBytesRejectsPathOverHTTP(t *testing.T) {
	if _, err := imageBytes("a.png", "", false); err == nil || !strings.Contains(err.Error(), "stdio") {
		t.Fatalf("err = %v, want 提示仅 stdio 可用", err)
	}
}

func TestImageBytesReadsLocalPathOnStdio(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.png")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatalf("准备文件: %v", err)
	}
	got, err := imageBytes(p, "", true)
	if err != nil {
		t.Fatalf("imageBytes: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("读出 = %q, want hello", got)
	}
}

func TestImageBytesAcceptsDataURIBase64(t *testing.T) {
	got, err := imageBytes("", "data:image/png;base64,aGVsbG8=", false)
	if err != nil {
		t.Fatalf("imageBytes: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("解码 = %q, want hello", got)
	}
}

func TestImageBytesRejectsBadBase64(t *testing.T) {
	if _, err := imageBytes("", "not-base64!!", false); err == nil {
		t.Fatal("非法 base64 应报错")
	}
}
