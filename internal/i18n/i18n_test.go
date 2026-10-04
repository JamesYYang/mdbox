package i18n

import "testing"

func TestFromAcceptLanguage(t *testing.T) {
	cases := []struct {
		in   string
		want Lang
	}{
		{"zh-CN,zh;q=0.9,en;q=0.8", ZH},
		{"zh", ZH},
		{"zh-Hant-TW", ZH},
		{"en-US,en;q=0.9", EN},
		{"en;q=0.5,zh;q=0.9", EN}, // 按出现顺序取首个可识别语言
		{"fr-FR,de;q=0.9,en;q=0.8", EN},
		{"", EN},
	}
	for _, c := range cases {
		if got := FromAcceptLanguage(c.in); got != c.want {
			t.Errorf("FromAcceptLanguage(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCatalogParity(t *testing.T) {
	en, zh := catalog[EN], catalog[ZH]
	for k := range en {
		if _, ok := zh[k]; !ok {
			t.Errorf("中文目录缺少键 %q", k)
		}
	}
	for k := range zh {
		if _, ok := en[k]; !ok {
			t.Errorf("英文目录缺少键 %q", k)
		}
	}
}

func TestKnown(t *testing.T) {
	if !Known(string(KeyUserExists)) {
		t.Error("KeyUserExists 应被识别为已知键")
	}
	if Known("这不是键") {
		t.Error("未知字符串不应被识别")
	}
}
