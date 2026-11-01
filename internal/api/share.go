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
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeBadReq(w, r, i18n.KeyBadJSON, err)
		return
	}
	if !store.ValidShareMode(in.Mode) {
		writeErr(w, r, http.StatusBadRequest, string(i18n.KeyBadShareMode))
		return
	}
	id := r.PathValue("id")
	d, err := stOf(r).SetShareMode(id, in.Mode)
	if err != nil {
		writeErr(w, r, http.StatusNotFound, err.Error())
		return
	}
	resp := map[string]any{"mode": d.ShareMode}
	if d.ShareMode != store.ShareOff {
		resp["url"] = s.shareURL(userOf(r), id)
	}
	writeJSON(w, http.StatusOK, resp)
}

// sharedDoc 校验分享链接并取出文档；任何一步失败都统一按「不存在」处理。
// 归档期间分享视为失效，恢复后自动复效。
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
	if err != nil || !d.Shared || d.Status == store.StatusArchived {
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
		writeErr(w, r, storeErrStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"doc": d})
}

// editableDoc 校验签名与「可编辑」状态，并返回源文档与**作者**的 store。
// 与 sharedDoc 的区别：要求 share_mode == edit，且必须由调用方自己写盘。
// 归档期间分享视为失效，恢复后自动复效。
// 已知窄竞态（接受，不改结构）：模式与归档状态在这里判定，st.Update 在 store 自己的锁里写盘，
// 作者在这两步之间把分享改成 off 或归档时，这次写入仍会落盘（会写进 archive/）。要强一致得把判定下推进 store 锁内。
func (s *Server) editableDoc(r *http.Request) (*store.Doc, *store.Store, bool) {
	user, id := r.PathValue("user"), r.PathValue("id")
	if !s.validSig(user, id, r.PathValue("token")) {
		return nil, nil, false
	}
	st, err := s.reg.Store(user)
	if err != nil {
		return nil, nil, false
	}
	d, err := st.Get(id)
	if err != nil || d.Status == store.StatusArchived || d.ShareMode != store.ShareEdit {
		return nil, nil, false
	}
	return d, st, true
}

// editShared 让已登录用户凭可编辑分享链接改写源文档正文。
// 标题、标签、分类、来源一律不动；分享签名与正文无关，保存后链接不变。
func (s *Server) editShared(w http.ResponseWriter, r *http.Request) {
	src, st, ok := s.editableDoc(r)
	if !ok {
		writeErr(w, r, http.StatusNotFound, string(i18n.KeyShareGone))
		return
	}
	var in struct {
		Content *string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeBadReq(w, r, i18n.KeyBadJSON, err)
		return
	}
	// 缺 content（如 {}）与显式 null 都视为参数错误：与 PUT /api/docs/{id} 一致，
	// 不能让一次空请求把源文档正文清空。
	if in.Content == nil {
		writeErr(w, r, http.StatusBadRequest, string(i18n.KeyBadJSON))
		return
	}
	d, err := st.Update(src.ID, store.UpdateInput{Content: in.Content})
	if err != nil {
		writeErr(w, r, storeErrStatus(err), err.Error())
		return
	}
	html, _ := render.Markdown(*in.Content)
	writeJSON(w, http.StatusOK, map[string]any{"doc": d, "html": html})
}
