package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"mdbox/internal/i18n"
	"mdbox/internal/render"
	"mdbox/internal/store"
)

// shareSig 用固定算法为「用户 + 文档 id」生成分享签名，同一文档永远得到同一签名。
func (s *Server) shareSig(user, id string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
	mac.Write([]byte(user + "/" + id))
	return hex.EncodeToString(mac.Sum(nil))[:24]
}

// shareURL 返回文档的分享路径（不含域名）。
func (s *Server) shareURL(user, id string) string {
	return "/s/" + user + "/" + id + "/" + s.shareSig(user, id)
}

func (s *Server) validSig(user, id, got string) bool {
	return hmac.Equal([]byte(got), []byte(s.shareSig(user, id)))
}

func (s *Server) shareDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Shared bool `json:"shared"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeBadReq(w, r, i18n.KeyBadJSON, err)
		return
	}
	id := r.PathValue("id")
	d, err := stOf(r).SetShared(id, in.Shared)
	if err != nil {
		writeErr(w, r, http.StatusNotFound, err.Error())
		return
	}
	resp := map[string]any{"shared": d.Shared}
	if d.Shared {
		resp["url"] = s.shareURL(userOf(r), id)
	}
	writeJSON(w, http.StatusOK, resp)
}

// sharedDoc 校验分享链接并取出文档；任何一步失败都统一按「不存在」处理。
func (s *Server) sharedDoc(r *http.Request) (*store.Doc, bool) {
	user, id := r.PathValue("user"), r.PathValue("id")
	if !s.validSig(user, id, r.PathValue("token")) {
		return nil, false
	}
	st, err := s.reg.Store(user)
	if err != nil {
		return nil, false
	}
	d, err := st.Get(id)
	if err != nil || !d.Shared {
		return nil, false
	}
	return d, true
}

// publicDoc 是匿名可访问的分享读取接口，只读、不校验登录。
func (s *Server) publicDoc(w http.ResponseWriter, r *http.Request) {
	d, ok := s.sharedDoc(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, string(i18n.KeyShareGone))
		return
	}
	html, _ := render.Markdown(d.Content)
	writeJSON(w, http.StatusOK, map[string]any{"doc": d, "html": html})
}

// publicDownload 是匿名可访问的分享下载接口，同样只读、不校验登录。
func (s *Server) publicDownload(w http.ResponseWriter, r *http.Request) {
	d, ok := s.sharedDoc(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, string(i18n.KeyShareGone))
		return
	}
	writeMarkdown(w, d)
}

// cloneShared 把一篇「已分享」的文档复制到当前登录用户自己的库里（只带标题与正文）。
// 源文档不受影响；副本是独立文档（source=clone）。
func (s *Server) cloneShared(w http.ResponseWriter, r *http.Request) {
	src, ok := s.sharedDoc(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, string(i18n.KeyShareGone))
		return
	}
	d, err := stOf(r).Create(store.CreateInput{
		Title:   src.Title,
		Content: src.Content,
		Source:  "clone",
	})
	if err != nil {
		writeErr(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"doc": d})
}
