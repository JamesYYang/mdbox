package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"mdbox/internal/i18n"
)

// MaxImageBytes 是单张图片上限。图片与文档共用 MaxUserBytes 配额。
const MaxImageBytes = 5 << 20

var (
	// ErrImageTooLarge 表示图片超过 MaxImageBytes。
	ErrImageTooLarge = errors.New(string(i18n.KeyImageTooLarge))
	// ErrUnsupportedImage 表示内容不是允许的图片格式（SVG 也走这里）。
	ErrUnsupportedImage = errors.New(string(i18n.KeyUnsupportedImage))
)

// assetNameRe 限定可读取的文件名：32 位小写 hex + 已知扩展名。
// 这个正则同时是路径穿越的防线，OpenImage 之外不要用它做别的判断。
var assetNameRe = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|gif|webp)$`)

// imageExt 把嗅探出的 MIME 映射成扩展名；不在白名单返回空串。
func imageExt(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}

// SaveImage 按内容存一张图片并返回文件名。
// 校验顺序：大小 → 类型；同名（同内容）已存在时直接复用，不再占配额。
func (s *Store) SaveImage(data []byte) (string, int64, error) {
	if len(data) > MaxImageBytes {
		return "", 0, ErrImageTooLarge
	}
	probe := data
	if len(probe) > 512 {
		probe = probe[:512]
	}
	ext := imageExt(http.DetectContentType(probe))
	if ext == "" {
		return "", 0, ErrUnsupportedImage
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:16]) + ext

	s.mu.Lock()
	defer s.mu.Unlock()
	if n, ok := s.assets[name]; ok {
		return name, n, nil
	}
	if s.usedBytes()+int64(len(data)) > s.quota {
		return "", 0, ErrQuotaExceeded
	}
	dir := filepath.Join(s.root, "assets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", 0, err
	}
	// 先落盘再进索引：写失败时内存与磁盘不会脱节。
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpName)
		return "", 0, err
	}
	s.assets[name] = int64(len(data))
	return name, int64(len(data)), nil
}

// OpenImage 按名打开图片。名字不合法或文件不存在都返回 ErrNotFound，
// 不区分「非法」与「不存在」，避免被用来探测。
func (s *Store) OpenImage(name string) (*os.File, os.FileInfo, error) {
	if !assetNameRe.MatchString(name) {
		return nil, nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(s.root, "assets", name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, fi, nil
}
