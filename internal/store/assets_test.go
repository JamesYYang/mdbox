package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 只要求前 8 字节能通过 http.DetectContentType 的嗅探，SaveImage 不解码图片。
var pngBytes = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 100))

func TestSaveImageDedupesIdenticalContent(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	name1, size1, err := st.SaveImage(pngBytes)
	if err != nil {
		t.Fatalf("第一次 SaveImage: %v", err)
	}
	name2, size2, err := st.SaveImage(append([]byte(nil), pngBytes...))
	if err != nil {
		t.Fatalf("第二次 SaveImage: %v", err)
	}
	if name1 != name2 || size1 != size2 || size1 != int64(len(pngBytes)) {
		t.Fatalf("同名复用失败: %q/%d vs %q/%d", name1, size1, name2, size2)
	}
	if !strings.HasSuffix(name1, ".png") || len(strings.TrimSuffix(name1, ".png")) != 32 {
		t.Fatalf("文件名不是 32 位 hex + .png: %q", name1)
	}
	files, _ := os.ReadDir(filepath.Join(st.Root(), "assets"))
	if len(files) != 1 {
		t.Fatalf("assets 下应只有 1 个文件，实际 %d", len(files))
	}
}

func TestSaveImageRejectsOversized(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	big := bytes.Repeat([]byte{0}, MaxImageBytes+1)
	if _, _, err := st.SaveImage(big); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("err = %v, want ErrImageTooLarge", err)
	}
}

func TestSaveImageRejectsNonImage(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	svg := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><script>alert(1)</script></svg>")
	if _, _, err := st.SaveImage(svg); !errors.Is(err, ErrUnsupportedImage) {
		t.Fatalf("err = %v, want ErrUnsupportedImage", err)
	}
}

func TestQuotaCountsImages(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st.quota = 1000
	if _, _, err := st.SaveImage(pngBytes); err != nil { // 约 108 字节
		t.Fatalf("SaveImage: %v", err)
	}
	// 图片占了配额，文档写入必须被挡下。
	if _, err := st.Create(CreateInput{Title: "t", Content: strings.Repeat("a", 1000)}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("Create err = %v, want ErrQuotaExceeded", err)
	}
}

func TestOpenImageRejectsBadNames(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	name, _, err := st.SaveImage(pngBytes)
	if err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	ok, _, err := st.OpenImage(name)
	if err != nil {
		t.Fatalf("合法文件名打不开: %v", err)
	}
	// Windows 上未关闭的句柄会让 t.TempDir 的清理失败。
	_ = ok.Close()
	for _, bad := range []string{"../" + name, strings.ToUpper(name), "short.png", name + ".bak", "x/" + name} {
		if _, _, err := st.OpenImage(bad); !errors.Is(err, ErrNotFound) {
			t.Fatalf("OpenImage(%q) err = %v, want ErrNotFound", bad, err)
		}
	}
}

func TestReloadPicksUpImages(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	name, size, err := st.SaveImage(pngBytes)
	if err != nil {
		t.Fatalf("SaveImage: %v", err)
	}
	again, err := New(dir)
	if err != nil {
		t.Fatalf("重新打开: %v", err)
	}
	againFile, _, err := again.OpenImage(name)
	if err != nil {
		t.Fatalf("重启后图片丢失: %v", err)
	}
	_ = againFile.Close()
	if got := again.usedBytes(); got != size {
		t.Fatalf("重启后 usedBytes = %d, want %d", got, size)
	}
}
