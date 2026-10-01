// Package store 管理 Markdown 文档的持久化与元数据索引。
//
// 磁盘布局：
//
//	data/docs/*.md      活跃文档
//	data/archive/*.md   已归档文档
//
// 每篇文档是纯 Markdown 文件，元数据写在 YAML frontmatter 里。
// 工具只是这些文件的一个视图——文件永远可以脱离工具单独存在。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

var ErrNotFound = errors.New("doc not found")

// Doc 是一篇文档的元数据（Content 仅在单篇读取时填充）。
type Doc struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Tags     []string  `json:"tags"`
	Category string    `json:"category"`
	Source   string    `json:"source"`
	Status   string    `json:"status"` // active | archived
	Shared   bool      `json:"shared"` // 是否开启匿名分享
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Size     int       `json:"size"`
	Content  string    `json:"content,omitempty"`
}

// frontmatter 是文件头部的 YAML 结构。
type frontmatter struct {
	Title    string   `yaml:"title"`
	Tags     []string `yaml:"tags"`
	Category string   `yaml:"category"`
	Source   string   `yaml:"source"`
	Status   string   `yaml:"status"`
	Shared   bool     `yaml:"shared,omitempty"`
	Created  string   `yaml:"created"`
	Updated  string   `yaml:"updated"`
}

type entry struct {
	Doc
	path string
}

// Store 是文档仓库。索引常驻内存，启动时从磁盘全量重建。
type Store struct {
	root string
	mu   sync.RWMutex
	docs map[string]*entry
}

// New 打开（必要时创建）一个文档仓库。
func New(root string) (*Store, error) {
	s := &Store{root: root, docs: map[string]*entry{}}
	for _, d := range []string{"docs", "archive"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return nil, err
		}
	}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

// Root 返回仓库根目录，供 git 备份等外部逻辑使用。
func (s *Store) Root() string { return s.root }

func (s *Store) reload() error {
	next := map[string]*entry{}
	for _, sub := range []string{"docs", "archive"} {
		files, _ := filepath.Glob(filepath.Join(s.root, sub, "*.md"))
		for _, f := range files {
			e, err := loadFile(f, sub)
			if err != nil {
				continue
			}
			next[e.ID] = e
		}
	}
	s.docs = next
	return nil
}

func loadFile(path string, bucket string) (*entry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body := string(raw)
	m := frontmatter{}
	if strings.HasPrefix(body, "---\n") {
		if parts := strings.SplitN(body[4:], "\n---", 2); len(parts) == 2 {
			_ = yaml.Unmarshal([]byte(parts[0]), &m)
			body = strings.TrimLeft(parts[1], "\n")
		}
	}
	id := strings.TrimSuffix(filepath.Base(path), ".md")
	fi, _ := os.Stat(path)

	status := m.Status
	if status == "" {
		status = "active"
	}
	if bucket == "archive" {
		status = "archived"
	}
	e := &entry{
		Doc: Doc{
			ID:       id,
			Title:    m.Title,
			Tags:     m.Tags,
			Category: m.Category,
			Source:   m.Source,
			Status:   status,
			Shared:   m.Shared,
			Created:  parseTime(m.Created, fi),
			Updated:  parseTime(m.Updated, fi),
			Size:     len(body),
		},
		path: path,
	}
	if e.Title == "" {
		e.Title = id
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	return e, nil
}

func parseTime(v string, fi os.FileInfo) time.Time {
	if v != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
	}
	if fi != nil {
		return fi.ModTime()
	}
	return time.Now()
}

// serialize 把文档序列化为带 frontmatter 的 Markdown 文本。
func serialize(d *Doc, content string) string {
	m := frontmatter{
		Title:    d.Title,
		Tags:     d.Tags,
		Category: d.Category,
		Source:   d.Source,
		Status:   d.Status,
		Shared:   d.Shared,
		Created:  d.Created.Format(time.RFC3339),
		Updated:  d.Updated.Format(time.RFC3339),
	}
	b, _ := yaml.Marshal(&m)
	body := content
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return "---\n" + string(b) + "---\n\n" + body
}

// CreateInput 是新建/上传文档的入参。
type CreateInput struct {
	Title    string
	Content  string
	Tags     []string
	Category string
	Source   string
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102") + "-" + hex.EncodeToString(b)
}

// Create 写入一篇新文档。
func (s *Store) Create(in CreateInput) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := newID()
	now := time.Now()
	d := &Doc{
		ID:       id,
		Title:    orDefault(in.Title, "未命名文档"),
		Tags:     in.Tags,
		Category: in.Category,
		Source:   orDefault(in.Source, "manual"),
		Status:   "active",
		Created:  now,
		Updated:  now,
		Size:     len(in.Content),
		Content:  in.Content,
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	path := filepath.Join(s.root, "docs", id+".md")
	if err := os.WriteFile(path, []byte(serialize(d, in.Content)), 0o644); err != nil {
		return nil, err
	}
	e := &entry{Doc: *d, path: path}
	s.docs[id] = e
	out := e.Doc
	out.Content = in.Content
	return &out, nil
}

// CreateFromRaw 从一段可能带 frontmatter 的原始 Markdown 创建文档。
// 文件自带的 title/tags/category 优先，缺失时用 fallbackTitle 兜底。
func (s *Store) CreateFromRaw(raw []byte, fallbackTitle, source string) (*Doc, error) {
	body := string(raw)
	m := frontmatter{}
	if strings.HasPrefix(body, "---\n") {
		if parts := strings.SplitN(body[4:], "\n---", 2); len(parts) == 2 {
			_ = yaml.Unmarshal([]byte(parts[0]), &m)
			body = strings.TrimLeft(parts[1], "\n")
		}
	}
	title := m.Title
	if strings.TrimSpace(title) == "" {
		title = fallbackTitle
	}
	return s.Create(CreateInput{
		Title:    title,
		Content:  body,
		Tags:     m.Tags,
		Category: m.Category,
		Source:   source,
	})
}

// UpdateInput 是更新文档的入参，零值表示不改。
type UpdateInput struct {
	Title    *string
	Content  *string
	Tags     []string
	Category *string
}

// Update 更新一篇已有文档。
func (s *Store) Update(id string, in UpdateInput) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	if in.Title != nil {
		e.Title = *in.Title
	}
	if in.Category != nil {
		e.Category = *in.Category
	}
	if in.Tags != nil {
		e.Tags = in.Tags
	}
	content := readBody(e.path)
	if in.Content != nil {
		content = *in.Content
	}
	e.Updated = time.Now()
	e.Size = len(content)
	if err := os.WriteFile(e.path, []byte(serialize(&e.Doc, content)), 0o644); err != nil {
		return nil, err
	}
	out := e.Doc
	out.Content = content
	return &out, nil
}

// Archive 把文档移入 archive 目录（不真删）。
func (s *Store) Archive(id string) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	content := readBody(e.path)
	e.Status = "archived"
	e.Updated = time.Now()
	dst := filepath.Join(s.root, "archive", id+".md")
	if err := os.WriteFile(dst, []byte(serialize(&e.Doc, content)), 0o644); err != nil {
		return nil, err
	}
	_ = os.Remove(e.path)
	e.path = dst
	out := e.Doc
	return &out, nil
}

// SetShared 开启或关闭文档的匿名分享。
func (s *Store) SetShared(id string, shared bool) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	e.Shared = shared
	e.Updated = time.Now()
	content := readBody(e.path)
	if err := os.WriteFile(e.path, []byte(serialize(&e.Doc, content)), 0o644); err != nil {
		return nil, err
	}
	out := e.Doc
	return &out, nil
}

// Delete 彻底删除文档文件。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.docs[id]
	if !ok {
		return ErrNotFound
	}
	if err := os.Remove(e.path); err != nil {
		return err
	}
	delete(s.docs, id)
	return nil
}

// Get 读取单篇文档（含正文）。文件被外部删除时会自动重建索引。
func (s *Store) Get(id string) (*Doc, error) {
	s.mu.RLock()
	e, ok := s.docs[id]
	s.mu.RUnlock()
	if !ok {
		s.mu.Lock()
		_ = s.reload()
		s.mu.Unlock()
		s.mu.RLock()
		e, ok = s.docs[id]
		s.mu.RUnlock()
		if !ok {
			return nil, ErrNotFound
		}
	}
	if _, err := os.Stat(e.path); err != nil {
		s.mu.Lock()
		_ = s.reload()
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	out := e.Doc
	out.Content = readBody(e.path)
	if e.Status == "active" {
		_ = os.Chtimes(e.path, time.Now(), time.Now()) // 记录一次访问
	}
	return &out, nil
}

// ListFilter 控制列表与搜索。
type ListFilter struct {
	Tag      string
	Category string
	Status   string // 默认 active
	Query    string
	Limit    int
}

// List 返回文档列表（不含正文）。Query 为空时按更新时间倒序。
func (s *Store) List(f ListFilter) ([]Doc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := f.Status
	if status == "" {
		status = "active"
	}
	q := strings.ToLower(strings.TrimSpace(f.Query))

	var hits []Doc
	for _, e := range s.docs {
		if status != "all" && e.Status != status {
			continue
		}
		if f.Tag != "" && !hasTag(e.Tags, f.Tag) {
			continue
		}
		if f.Category != "" && e.Category != f.Category {
			continue
		}
		if q != "" {
			body := readBody(e.path)
			if !contains(body, q) && !contains(e.Title, q) && !contains(strings.Join(e.Tags, " "), q) {
				continue
			}
		}
		d := e.Doc
		d.Content = ""
		hits = append(hits, d)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Updated.After(hits[j].Updated) })
	if f.Limit > 0 && len(hits) > f.Limit {
		hits = hits[:f.Limit]
	}
	return hits, nil
}

// Tags 返回标签及其文档数。
func (s *Store) Tags() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for _, e := range s.docs {
		if e.Status != "active" {
			continue
		}
		for _, t := range e.Tags {
			out[t]++
		}
	}
	return out
}

// Categories 返回分类及其文档数。
func (s *Store) Categories() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for _, e := range s.docs {
		if e.Status != "active" {
			continue
		}
		out[e.Category]++
	}
	return out
}

func readBody(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	body := string(raw)
	if strings.HasPrefix(body, "---\n") {
		if parts := strings.SplitN(body[4:], "\n---", 2); len(parts) == 2 {
			return strings.TrimLeft(parts[1], "\n")
		}
	}
	return body
}

func contains(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), sub)
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
