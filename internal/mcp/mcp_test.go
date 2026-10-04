package mcp

import (
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
