// Package api 提供 mdbox 的 REST 接口，Web UI、CLI 与第三方都走这一层。
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"mdbox/internal/render"
	"mdbox/internal/store"
)

/* ---------- 基础 ---------- */

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) listDocs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if v := q.Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	docs, err := stOf(r).List(store.ListFilter{
		Tag:      q.Get("tag"),
		Category: q.Get("category"),
		Status:   q.Get("status"),
		Query:    q.Get("q"),
		Limit:    limit,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"docs": docs, "total": len(docs)})
}

func (s *Server) getDoc(w http.ResponseWriter, r *http.Request) {
	d, err := stOf(r).Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	html, _ := render.Markdown(d.Content)
	resp := map[string]any{"doc": d, "html": html}
	if d.Shared {
		resp["shareUrl"] = s.shareURL(userOf(r), d.ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

type createReq struct {
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Tags     []string `json:"tags"`
	Category string   `json:"category"`
	Source   string   `json:"source"`
}

func (s *Server) createDoc(w http.ResponseWriter, r *http.Request) {
	var in createReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	d, err := stOf(r).Create(store.CreateInput{
		Title:    in.Title,
		Content:  in.Content,
		Tags:     in.Tags,
		Category: in.Category,
		Source:   in.Source,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"doc": d})
}

func (s *Server) updateDoc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    *string  `json:"title"`
		Content  *string  `json:"content"`
		Tags     []string `json:"tags"`
		Category *string  `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	d, err := stOf(r).Update(r.PathValue("id"), store.UpdateInput{
		Title:    in.Title,
		Content:  in.Content,
		Tags:     in.Tags,
		Category: in.Category,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"doc": d})
}

func (s *Server) archiveDoc(w http.ResponseWriter, r *http.Request) {
	d, err := stOf(r).Archive(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"doc": d})
}

func (s *Server) deleteDoc(w http.ResponseWriter, r *http.Request) {
	if err := stOf(r).Delete(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) downloadDoc(w http.ResponseWriter, r *http.Request) {
	d, err := stOf(r).Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeMarkdown(w, d)
}

// writeMarkdown 以附件形式输出一篇文档的 Markdown 正文。
func writeMarkdown(w http.ResponseWriter, d *store.Doc) {
	name := strings.TrimSpace(d.Title)
	if name == "" {
		name = d.ID
	}
	name = strings.ReplaceAll(name, "/", "_")
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s.md"; filename*=UTF-8''%s.md`,
			fallbackName(name), url.QueryEscape(name)))
	_, _ = io.WriteString(w, d.Content)
}

// fallbackName 生成纯 ASCII 文件名，兼容不支持 RFC 5987 的客户端。
func fallbackName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 128 && r != '"' && r != '\\':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "doc"
	}
	return b.String()
}

func (s *Server) tags(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tags": stOf(r).Tags()})
}

func (s *Server) categories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"categories": stOf(r).Categories()})
}

// upload 接收 multipart 上传的 .md 文件，可一次多篇。
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "parse form: "+err.Error())
		return
	}
	created := []*store.Doc{}
	for _, fh := range r.MultipartForm.File {
		for _, h := range fh {
			doc, err := saveUpload(stOf(r), h)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			created = append(created, doc)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"docs": created, "total": len(created)})
}

func saveUpload(st *store.Store, h *multipart.FileHeader) (*store.Doc, error) {
	f, err := h.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// 文件自带 frontmatter 时用其中的 title/tags/category，否则用文件名兜底
	return st.CreateFromRaw(raw, strings.TrimSuffix(h.Filename, ".md"), "upload")
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "bad json: "+err.Error())
		return
	}
	html, err := render.Markdown(in.Content)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": html})
}
