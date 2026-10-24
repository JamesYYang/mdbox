package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 旧文档只写了 shared: true，应被读成 view，且共享标志仍为真。
func TestLegacySharedReadsAsView(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	id := "20260101-aaaaaaaaaaaa"
	raw := "---\ntitle: 旧文档\nshared: true\n---\n\n正文\n"
	if err := os.WriteFile(filepath.Join(root, "docs", id+".md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatalf("打开仓库失败: %v", err)
	}
	d, err := s.Get(id)
	if err != nil {
		t.Fatalf("读取文档失败: %v", err)
	}
	if d.ShareMode != ShareView {
		t.Errorf("ShareMode = %q, want %q", d.ShareMode, ShareView)
	}
	if !d.Shared {
		t.Error("Shared = false, want true")
	}
}

// 设置三态后落盘应写 share:，且不再写旧的 shared:。
func TestSetShareModeWritesShareField(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("打开仓库失败: %v", err)
	}
	d, err := s.Create(CreateInput{Title: "文档", Content: "正文"})
	if err != nil {
		t.Fatalf("创建文档失败: %v", err)
	}
	if _, err := s.SetShareMode(d.ID, ShareEdit); err != nil {
		t.Fatalf("设置分享模式失败: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root(), "docs", d.ID+".md"))
	if err != nil {
		t.Fatalf("读取落盘文件失败: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "share: edit") {
		t.Errorf("落盘缺少共享模式，实际内容:\n%s", text)
	}
	if strings.Contains(text, "shared:") {
		t.Errorf("不应再写旧的 shared 字段，实际内容:\n%s", text)
	}
	again, err := s.Get(d.ID)
	if err != nil {
		t.Fatalf("重新读取失败: %v", err)
	}
	if again.ShareMode != ShareEdit || !again.Shared {
		t.Errorf("重新读取后 ShareMode=%q Shared=%v, want edit/true", again.ShareMode, again.Shared)
	}
	if _, err := s.SetShareMode(d.ID, ShareOff); err != nil {
		t.Fatalf("关闭分享失败: %v", err)
	}
	off, err := s.Get(d.ID)
	if err != nil {
		t.Fatalf("重新读取失败: %v", err)
	}
	if off.ShareMode != ShareOff || off.Shared {
		t.Errorf("关闭后 ShareMode=%q Shared=%v, want off/false", off.ShareMode, off.Shared)
	}
}

// 非法取值必须报错，且不动磁盘。
func TestSetShareModeRejectsUnknown(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("打开仓库失败: %v", err)
	}
	d, err := s.Create(CreateInput{Title: "文档", Content: "正文"})
	if err != nil {
		t.Fatalf("创建文档失败: %v", err)
	}
	if _, err := s.SetShareMode(d.ID, "public"); err != ErrBadShareMode {
		t.Fatalf("err = %v, want ErrBadShareMode", err)
	}
	got, err := s.Get(d.ID)
	if err != nil {
		t.Fatalf("读取文档失败: %v", err)
	}
	if got.ShareMode != ShareOff {
		t.Errorf("ShareMode = %q, want off", got.ShareMode)
	}
}
