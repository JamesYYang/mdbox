package api

import (
	"io"
	"net/http"
	"strings"

	"mdbox/internal/i18n"
	"mdbox/internal/users"
)

// uploadAsset 接收一张图片（multipart 字段 file），按内容入库后返回访问地址。
// 大小与类型校验都在 store.SaveImage 里，这里只负责搬运与错误映射。
func (s *Server) uploadAsset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeBadReq(w, r, i18n.KeyBadForm, err)
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeBadReq(w, r, i18n.KeyBadForm, err)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		writeBadReq(w, r, i18n.KeyBadForm, err)
		return
	}
	name, size, err := stOf(r).SaveImage(data)
	if err != nil {
		writeErr(w, r, storeErrStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":   "/a/" + userOf(r) + "/" + name,
		"name":  name,
		"bytes": size,
	})
}

// imageContentType 按扩展名返回响应类型；文件名已由 OpenImage 校验过。
func imageContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".jpg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".gif"):
		return "image/gif"
	}
	return "image/webp"
}

// AssetHandler 提供匿名图片读取，挂在 /a/ 前缀下、不经过 withAuth。
// 用户名必须在注册表里、文件名必须是内容哈希格式，否则一律 404。
func AssetHandler(reg *users.Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/a/")
		user, name, ok := strings.Cut(rest, "/")
		if !ok || user == "" || strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}
		st, err := reg.Store(user)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f, fi, err := st.OpenImage(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", imageContentType(name))
		// 文件名就是内容哈希，内容永不变，可以长缓存。
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, name, fi.ModTime(), f)
	})
}
