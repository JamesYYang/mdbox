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
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"mdbox/internal/i18n"
)

// 写入上限。文档是纯文本，这些值刻意留得很宽松，只为挡住「传二进制/撑爆磁盘」这类滥用。
// 改动时记得同步 internal/i18n 里 KeyDocTooLarge / KeyQuotaExceeded 的文案。
const (
	MaxDocBytes  = 2 << 20   // 单篇正文上限 2 MiB
	MaxUserBytes = 200 << 20 // 单用户总量上限 200 MiB（含归档）
)

// 分享模式三态。Shared 是它的派生视图（ShareMode != ShareOff）。
const (
	ShareOff  = "off"
	ShareView = "view"
	ShareEdit = "edit"
)

// ValidShareMode 判断 mode 是否属于三态取值域。
func ValidShareMode(mode string) bool {
	return mode == ShareOff || mode == ShareView || mode == ShareEdit
}

var (
	ErrNotFound = errors.New(string(i18n.KeyDocNotFound))
	// ErrNotText 表示正文不是合法 UTF-8 文本（含 NUL 字节），典型来源是把二进制改名成 .md。
	ErrNotText = errors.New(string(i18n.KeyNotText))
	// ErrDocTooLarge 表示单篇正文超过 MaxDocBytes。
	ErrDocTooLarge = errors.New(string(i18n.KeyDocTooLarge))
	// ErrQuotaExceeded 表示该用户已用总量加本次写入会超过 MaxUserBytes。
	ErrQuotaExceeded = errors.New(string(i18n.KeyQuotaExceeded))
	// ErrBadShareMode 表示传入的分享模式不在取值域内。
	ErrBadShareMode = errors.New(string(i18n.KeyBadShareMode))
)

// Doc 是一篇文档的元数据（Content 仅在单篇读取时填充）。
type Doc struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Tags      []string  `json:"tags"`
	Category  string    `json:"category"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`     // active | archived
	ShareMode string    `json:"share_mode"` // off | view | edit，唯一真相
	Shared    bool      `json:"shared"`     // 派生：ShareMode != ShareOff
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Size      int       `json:"size"`
	Content   string    `json:"content,omitempty"`
}

// frontmatter 是文件头部的 YAML 结构。
type frontmatter struct {
	Title    string   `yaml:"title"`
	Tags     []string `yaml:"tags"`
	Category string   `yaml:"category"`
	Source   string   `yaml:"source"`
	Status   string   `yaml:"status"`
	Share    string   `yaml:"share,omitempty"`  // view | edit
	Shared   bool     `yaml:"shared,omitempty"` // 旧的只读开关，仅用于兼容读取
	Created  string   `yaml:"created"`
	Updated  string   `yaml:"updated"`
}

type entry struct {
	Doc
	path string
}

// Store 是文档仓库。索引常驻内存，启动时从磁盘全量重建。
type Store struct {
	root  string
	quota int64 // 单用户总量上限（字节），默认 MaxUserBytes
	mu    sync.RWMutex
	docs  map[string]*entry
}

// New 打开（必要时创建）一个文档仓库。
func New(root string) (*Store, error) {
	s := &Store{root: root, quota: MaxUserBytes, docs: map[string]*entry{}}
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
	mode := m.Share
	if mode == "" && m.Shared {
		mode = ShareView // 旧文件只有 shared: true
	}
	if !ValidShareMode(mode) {
		mode = ShareOff
	}
	e := &entry{
		Doc: Doc{
			ID:        id,
			Title:     m.Title,
			Tags:      m.Tags,
			Category:  m.Category,
			Source:    m.Source,
			Status:    status,
			ShareMode: mode,
			Shared:    mode != ShareOff,
			Created:   parseTime(m.Created, fi),
			Updated:   parseTime(m.Updated, fi),
			Size:      len(body),
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

// shareField 把模式转成 frontmatter 的取值；off 写空串，靠 omitempty 省略整行。
func shareField(mode string) string {
	if mode == ShareOff || mode == "" {
		return ""
	}
	return mode
}

// serialize 把文档序列化为带 frontmatter 的 Markdown 文本。
func serialize(d *Doc, content string) string {
	m := frontmatter{
		Title:    d.Title,
		Tags:     d.Tags,
		Category: d.Category,
		Source:   d.Source,
		Status:   d.Status,
		Share:    shareField(d.ShareMode),
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

// checkContent 校验一段正文能否入库：必须是合法 UTF-8 文本（Markdown 是文本格式，
// 二进制改名成 .md 会在这里被挡下），且不超过单篇上限。
func checkContent(content string) error {
	if !utf8.ValidString(content) || strings.IndexByte(content, 0) >= 0 {
		return ErrNotText
	}
	if len(content) > MaxDocBytes {
		return ErrDocTooLarge
	}
	return nil
}

// usedBytes 返回该用户已占用的正文总量（含归档文档，它们同样占磁盘）。调用方须持锁。
func (s *Store) usedBytes() int64 {
	var total int64
	for _, e := range s.docs {
		total += int64(e.Size)
	}
	return total
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

	if err := checkContent(in.Content); err != nil {
		return nil, err
	}
	if s.usedBytes()+int64(len(in.Content)) > s.quota {
		return nil, ErrQuotaExceeded
	}

	id := newID()
	now := time.Now()
	d := &Doc{
		ID:        id,
		Title:     orDefault(in.Title, "Untitled"),
		Tags:      in.Tags,
		Category:  in.Category,
		Source:    orDefault(in.Source, "manual"),
		Status:    "active",
		ShareMode: ShareOff,
		Created:   now,
		Updated:   now,
		Size:      len(in.Content),
		Content:   in.Content,
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
	// 校验放在改动内存状态之前：失败时索引与磁盘都保持原样。
	content := readBody(e.path)
	if in.Content != nil {
		if err := checkContent(*in.Content); err != nil {
			return nil, err
		}
		content = *in.Content
	}
	// 换掉自己的旧版本，所以先减去自身当前体积再比配额。
	if s.usedBytes()-int64(e.Size)+int64(len(content)) > s.quota {
		return nil, ErrQuotaExceeded
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
	dst := filepath.Join(s.root, "archive", id+".md")
	// 已经归档过：不写盘、不更新 updated，保持幂等。
	if e.Status == "archived" && e.path == dst {
		out := e.Doc
		return &out, nil
	}
	// 先写盘成功再改内存索引，写失败时索引不会与磁盘脱节。
	doc := e.Doc
	doc.Status = "archived"
	doc.Updated = time.Now()
	if err := os.WriteFile(dst, []byte(serialize(&doc, content)), 0o644); err != nil {
		return nil, err
	}
	// 源可能就是目标本身（文档已在 archive/），那种情况要就地重写，
	// 删源会连刚写好的文件一起删掉。
	if e.path != dst {
		_ = os.Remove(e.path)
	}
	e.Doc = doc
	e.path = dst
	out := e.Doc
	return &out, nil
}

// Unarchive 把文档从 archive 移回 docs，是 Archive 的逆操作。
func (s *Store) Unarchive(id string) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	dst := filepath.Join(s.root, "docs", id+".md")
	// 已经在活动区：不写盘、不更新 updated，保持幂等。
	if e.Status == "active" && e.path == dst {
		out := e.Doc
		return &out, nil
	}
	// 显式读源文件，不用 readBody：后者吞掉读错误返回空串，源文件被外部删掉而索引陈旧时
	// 会把空正文写进 docs/ 并返回成功，等于静默毁掉内容。
	raw, err := os.ReadFile(e.path)
	if err != nil {
		return nil, err
	}
	// 先写盘成功再改内存索引，写失败时索引不会与磁盘脱节。
	doc := e.Doc
	doc.Status = "active"
	doc.Updated = time.Now()
	if err := os.WriteFile(dst, []byte(serialize(&doc, stripFrontmatter(string(raw)))), 0o644); err != nil {
		return nil, err
	}
	// 源可能就是目标本身（文件在 docs/ 但 frontmatter 写着 archived），那种情况要就地重写，
	// 删源会连刚写好的文件一起删掉。
	if e.path != dst {
		_ = os.Remove(e.path)
	}
	e.Doc = doc
	e.path = dst
	out := e.Doc
	return &out, nil
}

// SetShareMode 设置文档的分享模式（off / view / edit）。
func (s *Store) SetShareMode(id, mode string) (*Doc, error) {
	if !ValidShareMode(mode) {
		return nil, ErrBadShareMode
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.docs[id]
	if !ok {
		return nil, ErrNotFound
	}
	e.ShareMode = mode
	e.Shared = mode != ShareOff
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

// Overview 是列表页「近 N 天」概览的计数结果。
type Overview struct {
	New      int `json:"new"`
	Updated  int `json:"updated"`
	Archived int `json:"archived"`
	Agent    int `json:"agent"`
}

// nonAgentSources 是不算作「agent 写入」的来源；其余（agent、codebuddy 等）都归为 agent。
var nonAgentSources = map[string]bool{"": true, "web": true, "manual": true, "upload": true, "clone": true}

// Overview 统计 since 以来的新增、更新、归档与 agent 写入数量。
// 更新只算窗口前创建、窗口内修改的活跃文档，避免与新增重复计数。
func (s *Store) Overview(since time.Time) Overview {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var o Overview
	for _, e := range s.docs {
		if !e.Created.Before(since) {
			o.New++
			if !nonAgentSources[e.Source] {
				o.Agent++
			}
		}
		if e.Status == "archived" {
			if !e.Updated.Before(since) {
				o.Archived++
			}
			continue
		}
		if e.Created.Before(since) && !e.Updated.Before(since) {
			o.Updated++
		}
	}
	return o
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
	return stripFrontmatter(string(raw))
}

// stripFrontmatter 去掉文件开头的 --- YAML 块，返回正文。
func stripFrontmatter(body string) string {
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
