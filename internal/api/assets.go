package api

import (
	"io"
	"net/http"

	"mdbox/internal/i18n"
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
