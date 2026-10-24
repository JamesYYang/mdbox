# 分享链接「登录后可编辑」实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把分享从「布尔只读」升级为「off / view / edit」三态，让已登录的用户凭可编辑链接在分享页就地改写原文档。

**Architecture:** 前端在分享页原地切换编辑态（textarea + `/api/preview` 实时预览），保存走新的 `PUT /api/share/{user}/{id}/{token}`；后端把 `publicPath` 从「路径前缀」改成「GET + 路径前缀」，使该 PUT 落到 `withAuth`（未登录 401），handler 用 `s.reg.Store(owner)` 拿到**作者**的 store 复用 `store.Update` 写入。已登录用户的登录流程在同一个 SPA 内完成，草稿存在 `shareCtx` 上跨登录保留。

**Tech Stack:** Go 1.22+（`net/http` 路由 PathValue）、原生 JS（无框架、无 CDN）。

## Global Constraints

- 代码注释与用户可见文案统一中文；面向访客的按钮/提示**中英双语都要加**（`web/i18n.js` 两个语言表各一条）。
- 不引入任何新依赖、新 CDN；前端全部原生 JS。
- `web/` 通过 `//go:embed` 打包，**每次改前端都要 `go build`** 才能生效。
- 分享签名算法不变：`HMAC-SHA256(secret, user+"/"+id)[:24]`，不换 secret，已发出的链接必须继续有效。
- `store` 的体积上限（单篇 2MB / 单用户 200MB）与 UTF-8/NUL 校验不改、不绕过。
- 每完成一个任务跑一次门禁：`gofmt -l .`（必须无输出）、`go vet ./...`、`go test ./...`、`go build -o mdbox .`。
- 提交直接在 `main`；**不要 `git push`**，推送由作者单独决定。

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/store/store.go` | 分享三态的唯一真相：`ShareMode`、兼容读、`SetShareMode` |
| `internal/store/share_mode_test.go` | **新建**：store 层的三态与兼容性测试 |
| `internal/api/share.go` | 分享读取、写入、开关三态的全部 handler |
| `internal/api/server.go` | 路由表与 `publicPath` 鉴权边界 |
| `internal/api/share_test.go` | **新建**：分享开关三态测试 |
| `internal/api/share_edit_test.go` | **新建**：可编辑链接的鉴权/状态码/落盘测试 |
| `internal/i18n/i18n.go` | 后端错误文案（新增 `KeyBadShareMode`） |
| `web/index.html` | 分享弹窗三态选择器、分享页编辑按钮与编辑面板 |
| `web/app.js` | 编辑态进入/保存/退出、草稿保留、登录分流 |
| `web/i18n.js` | 新增文案键（中英各一份） |
| `web/style.css` | 分享页编辑态的两栏布局与窄屏降级 |

---

### Task 1: store 三态分享 + 分享开关接口迁移

把 `shared bool` 换成 `share_mode` 三态，并一次性迁移所有调用点（旧 `SetShared` 直接删除，不留兼容壳）。

**Files:**
- Modify: `internal/store/store.go`（`Doc`、`frontmatter`、`loadFile`、`serialize`、`SetShared`）
- Modify: `internal/i18n/i18n.go`（新增 `KeyBadShareMode`）
- Modify: `internal/api/share.go`（`shareDoc`）
- Modify: `internal/api/clone_test.go`（3 处 `SetShared`）
- Test: `internal/store/share_mode_test.go`（新建）、`internal/api/share_test.go`（新建）

**Interfaces:**
- Produces:
  - `store.ShareOff = "off"`、`store.ShareView = "view"`、`store.ShareEdit = "edit"`
  - `store.ValidShareMode(mode string) bool`
  - `func (s *Store) SetShareMode(id, mode string) (*Doc, error)`
  - `store.ErrBadShareMode`
  - `Doc.ShareMode string`（json `share_mode`）、`Doc.Shared bool`（派生，json `shared`）
- Consumes: 既有 `Store.Get` / `serialize` / `readBody`

- [ ] **Step 1: 写 store 层失败测试**

新建 `internal/store/share_mode_test.go`：

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store -run Share -v`
Expected: 编译失败，`undefined: ShareView` / `ShareEdit` / `ErrBadShareMode` / `SetShareMode`。

- [ ] **Step 3: 加后端错误文案**

`internal/i18n/i18n.go` 里 `KeyShareGone` 旁边加常量：

```go
	KeyBadShareMode      Key = "share.bad_mode"
```

并在 en / zh 两张表里各加一条（与 `KeyShareGone` 同行位置）：

```go
	KeyBadShareMode: "Unknown share mode",
```

```go
	KeyBadShareMode: "未知的分享模式",
```

- [ ] **Step 4: 改 store**

`internal/store/store.go` 顶部常量区（`MaxDocBytes` 附近）加：

```go
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

// ErrBadShareMode 表示传入的分享模式不在取值域内。
var ErrBadShareMode = errors.New(string(i18n.KeyBadShareMode))
```

`Doc` 结构体把 `Shared` 那行换成两行（顺序保持与其它字段一致）：

```go
	ShareMode string    `json:"share_mode"` // off | view | edit，唯一真相
	Shared    bool      `json:"shared"`    // 派生：ShareMode != ShareOff
```

`frontmatter` 增加：

```go
	Share  string `yaml:"share,omitempty"`  // view | edit
	Shared bool   `yaml:"shared,omitempty"` // 旧的只读开关，仅用于兼容读取
```

`loadFile` 里，构造 `Doc` 之前算一次模式：

```go
	mode := m.Share
	if mode == "" && m.Shared {
		mode = ShareView // 旧文件只有 shared: true
	}
	if !ValidShareMode(mode) {
		mode = ShareOff
	}
```

并把 `Doc` 字面量里的 `Shared: m.Shared` 换成：

```go
		ShareMode: mode,
		Shared:    mode != ShareOff,
```

`serialize` 里 `frontmatter` 字面量中把 `Shared: d.Shared` 换成：

```go
		Share:    shareField(d.ShareMode),
```

并在 `serialize` 上方加一个小工具（`off` 时不写该字段）：

```go
// shareField 把模式转成 frontmatter 的取值；off 写空串，靠 omitempty 省略整行。
func shareField(mode string) string {
	if mode == ShareOff || mode == "" {
		return ""
	}
	return mode
}
```

`SetShared` 整个函数替换为：

```go
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
```

- [ ] **Step 5: 迁移 api 调用点**

`internal/api/share.go` 的 `shareDoc` 整体替换：

```go
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
```

`internal/api/clone_test.go` 三处 `aliceStore.SetShared(src.ID, true)` 改为 `aliceStore.SetShareMode(src.ID, store.ShareView)`。

- [ ] **Step 6: 补分享开关的 API 测试**

新建 `internal/api/share_test.go`：

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mdbox/internal/store"
)

// postShareMode 以给定 token 调用分享开关接口。
func postShareMode(t *testing.T, h http.Handler, token, id, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/docs/"+id+"/share", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	out["_status"] = float64(rec.Code)
	return out
}

func TestShareModeThreeStates(t *testing.T) {
	reg, h, _ := newTestServer(t)
	alice, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	d, err := st.Create(store.CreateInput{Title: "文档", Content: "正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	out := postShareMode(t, h, alice.Token, d.ID, `{"mode":"edit"}`)
	if out["_status"] != float64(http.StatusOK) {
		t.Fatalf("状态码 = %v, want 200; body=%v", out["_status"], out)
	}
	if out["mode"] != "edit" {
		t.Errorf("mode = %v, want edit", out["mode"])
	}
	if out["url"] == nil || out["url"] == "" {
		t.Error("edit 模式应返回分享链接")
	}

	out = postShareMode(t, h, alice.Token, d.ID, `{"mode":"off"}`)
	if out["_status"] != float64(http.StatusOK) {
		t.Fatalf("状态码 = %v, want 200", out["_status"])
	}
	if _, has := out["url"]; has {
		t.Error("off 模式不应返回分享链接")
	}

	out = postShareMode(t, h, alice.Token, d.ID, `{"mode":"public"}`)
	if out["_status"] != float64(http.StatusBadRequest) {
		t.Errorf("非法模式状态码 = %v, want 400", out["_status"])
	}
}
```

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/store ./internal/api -run 'Share|Clone' -v`
Expected: 全部 PASS。

- [ ] **Step 8: 门禁与提交**

```bash
gofmt -l . && go vet ./... && go test ./... && go build -o mdbox .
git add internal/store internal/api internal/i18n
git commit -m "feat(store): 分享改为 off/view/edit 三态"
```

---

### Task 2: 鉴权边界 + 可编辑链接写入端点

**Files:**
- Modify: `internal/api/server.go`（`New` 加路由、`publicPath` 改签名）
- Modify: `internal/api/share.go`（新增 `editableDoc`、`editShared`）
- Test: `internal/api/share_edit_test.go`（新建）

**Interfaces:**
- Consumes: `store.ShareEdit`、`store.ValidShareMode`、`store.UpdateInput{Content *string}`、`s.reg.Store(user)`
- Produces: `PUT /api/share/{user}/{id}/{token}` → `{doc, html}`；`func (s *Server) editableDoc(r *http.Request) (*store.Doc, *store.Store, bool)`

- [ ] **Step 1: 写失败测试**

新建 `internal/api/share_edit_test.go`：

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mdbox/internal/config"
	"mdbox/internal/store"
	"mdbox/internal/users"
)

// seedEditShared 造一篇 share_mode=edit 的文档，返回 id、签名与作者 store。
func seedEditShared(t *testing.T, reg *users.Registry, cfg *config.Config) (string, string, *store.Store) {
	t.Helper()
	if _, err := reg.Register("alice", "password123"); err != nil {
		t.Fatalf("注册 alice 失败: %v", err)
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	d, err := st.Create(store.CreateInput{
		Title: "源标题", Content: "# 正文\n\n内容",
		Tags: []string{"x"}, Category: "c", Source: "web",
	})
	if err != nil {
		t.Fatalf("创建文档失败: %v", err)
	}
	if _, err := st.SetShareMode(d.ID, store.ShareEdit); err != nil {
		t.Fatalf("设置可编辑失败: %v", err)
	}
	return d.ID, (&Server{cfg: cfg}).shareSig("alice", d.ID), st
}

func putShare(t *testing.T, h http.Handler, token, id, sig, content string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content})
	req := httptest.NewRequest(http.MethodPut, "/api/share/alice/"+id+"/"+sig, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestEditSharedRequiresLogin(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, _ := seedEditShared(t, reg, cfg)
	rec := putShare(t, h, "", id, sig, "游客写入")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEditSharedRejectsViewMode(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, st := seedEditShared(t, reg, cfg)
	if _, err := st.SetShareMode(id, store.ShareView); err != nil {
		t.Fatalf("切只读失败: %v", err)
	}
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}
	rec := putShare(t, h, bob.Token, id, sig, "同事写入")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEditSharedWritesOriginal(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, st := seedEditShared(t, reg, cfg)
	before, err := st.Get(id)
	if err != nil {
		t.Fatalf("读取源文档失败: %v", err)
	}
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}
	rec := putShare(t, h, bob.Token, id, sig, "同事改过的正文")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	after, err := st.Get(id)
	if err != nil {
		t.Fatalf("读取源文档失败: %v", err)
	}
	if strings.TrimSpace(after.Content) != "同事改过的正文" {
		t.Errorf("正文 = %q, want 同事改过的正文", after.Content)
	}
	if after.Title != before.Title || after.Category != before.Category || after.Source != before.Source {
		t.Errorf("元数据被改动: before=%+v after=%+v", before, after)
	}
	if len(after.Tags) != 1 || after.Tags[0] != "x" {
		t.Errorf("标签被改动: %v", after.Tags)
	}
	if !after.Updated.After(before.Updated) {
		t.Errorf("updated 未前移: %v -> %v", before.Updated, after.Updated)
	}
}

func TestEditSharedRejectsBinaryAndOversize(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, _ := seedEditShared(t, reg, cfg)
	alice, ok := reg.Get("alice")
	if !ok {
		t.Fatal("alice 不存在")
	}
	bin := putShare(t, h, alice.Token, id, sig, "a\x00b")
	if bin.Code != http.StatusUnsupportedMediaType {
		t.Errorf("含 NUL 状态码 = %d, want 415", bin.Code)
	}
	big := putShare(t, h, alice.Token, id, sig, strings.Repeat("a", store.MaxDocBytes+1))
	if big.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("超限状态码 = %d, want 413", big.Code)
	}
}

func TestEditSharedRevokedImmediately(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	id, sig, st := seedEditShared(t, reg, cfg)
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}
	if _, err := st.SetShareMode(id, store.ShareOff); err != nil {
		t.Fatalf("关闭分享失败: %v", err)
	}
	rec := putShare(t, h, bob.Token, id, sig, "撤回后写入")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/api -run EditShared -v`
Expected: FAIL。`TestEditSharedRequiresLogin` 目前返回 404 或 405（路径仍被当作公开，或路由不存在）。

- [ ] **Step 3: 改 `publicPath` 与路由**

`internal/api/server.go`：

```go
// publicPath 判断某请求是否无需登录：登录/注册/登出接口，以及分享的只读 GET。
// 必须按方法判断——否则 /api/share/ 下的写端点会被当成匿名接口。
func publicPath(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/login", "/api/register", "/api/logout":
		return true
	}
	return r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/share/")
}
```

`withAuth` 里的调用改成 `if publicPath(r) {`。

`New` 的路由表里，紧跟 `GET /api/share/{user}/{id}/{token}/download` 加一行：

```go
	mux.HandleFunc("PUT /api/share/{user}/{id}/{token}", s.editShared)
```

- [ ] **Step 4: 实现 `editableDoc` 与 `editShared`**

追加到 `internal/api/share.go` 末尾：

```go
// editableDoc 校验签名与「可编辑」状态，并返回源文档与**作者**的 store。
// 与 sharedDoc 的区别：要求 share_mode == edit，且必须由调用方自己写盘。
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
	if err != nil || d.ShareMode != store.ShareEdit {
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
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeBadReq(w, r, i18n.KeyBadJSON, err)
		return
	}
	d, err := st.Update(src.ID, store.UpdateInput{Content: &in.Content})
	if err != nil {
		writeErr(w, r, storeErrStatus(err), err.Error())
		return
	}
	html, _ := render.Markdown(in.Content)
	writeJSON(w, http.StatusOK, map[string]any{"doc": d, "html": html})
}
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/api -run 'EditShared|Clone' -v`
Expected: 全部 PASS。

- [ ] **Step 6: 回归公开读取**

Run: `go test ./internal/api -run Share -v`
Expected: PASS（`GET /api/share/...` 仍匿名可读）。

- [ ] **Step 7: 门禁与提交**

```bash
gofmt -l . && go vet ./... && go test ./... && go build -o mdbox .
git add internal/api
git commit -m "feat(api): 可编辑分享链接写入端点，publicPath 按方法判断"
```

---

### Task 3: 分享弹窗三态

**Files:**
- Modify: `web/index.html`（分享弹窗）
- Modify: `web/app.js`（`openShare` / `updateShareLink` / `onToggleShare` 一带）
- Modify: `web/i18n.js`（新增键）

**Interfaces:**
- Consumes: `POST /api/docs/{id}/share` `{mode}` → `{mode, url}`（Task 1）
- Produces: DOM `#share-mode`（`select`）、`#share-mode-hint`；`state.current.share_mode`

- [ ] **Step 1: 改 HTML**

`web/index.html` 中把分享弹窗里那段 `<label class="switch-row">…</label>`（含 `#share-toggle`）整体替换为：

```html
      <label class="switch-row">
        <span data-i18n="sharemodal.label">分享方式</span>
        <select id="share-mode" class="share-mode">
          <option value="off" data-i18n="sharemodal.mode_off">关闭分享</option>
          <option value="view" data-i18n="sharemodal.mode_view">只读链接（无需登录）</option>
          <option value="edit" data-i18n="sharemodal.mode_edit">可编辑链接（需登录）</option>
        </select>
      </label>
      <p id="share-mode-hint" class="msg"></p>
```

- [ ] **Step 2: 加 i18n 键**

`web/i18n.js` 中文表里 `sharemodal.title` 附近加：

```js
      'sharemodal.label': '分享方式',
      'sharemodal.mode_off': '关闭分享',
      'sharemodal.mode_view': '只读链接（无需登录）',
      'sharemodal.mode_edit': '可编辑链接（需登录）',
      'sharemodal.hint_off': '关闭后链接立即失效。',
      'sharemodal.hint_view': '任何人凭链接可只读查看，无需登录，无法编辑。',
      'sharemodal.hint_edit': '链接不变；任何登录用户凭链接可在分享页直接编辑，保存即改写本文档。无版本历史。',
```

英文表同位置加：

```js
      'sharemodal.label': 'Sharing',
      'sharemodal.mode_off': 'Off',
      'sharemodal.mode_view': 'Read-only link (no login)',
      'sharemodal.mode_edit': 'Editable link (login required)',
      'sharemodal.hint_off': 'The link stops working immediately.',
      'sharemodal.hint_view': 'Anyone with the link can read, without logging in. No editing.',
      'sharemodal.hint_edit': 'Same link; any logged-in user can edit in place and save over this document. No version history.',
```

删掉不再使用的 `sharemodal.toggle`（中文表与英文表各一行）。

- [ ] **Step 3: 改 JS**

`web/app.js` 里 `openShare` / `updateShareLink` / `onToggleShare` 三个函数整体替换为：

```js
  var SHARE_HINTS = {
    off: 'sharemodal.hint_off',
    view: 'sharemodal.hint_view',
    edit: 'sharemodal.hint_edit'
  };

  function openShare() {
    if (!state.current) { return; }
    $('share-mode').value = state.current.share_mode || 'off';
    updateShareModeHint();
    updateShareLink($('share-mode').value);
    openModal('modal-share');
  }

  function updateShareModeHint() {
    var m = $('share-mode').value;
    $('share-mode-hint').textContent = I18N.t(SHARE_HINTS[m] || SHARE_HINTS.off);
  }

  function updateShareLink(mode) {
    if (mode !== 'off' && state.current && state.current.shareUrl) {
      $('share-link').value = location.origin + state.current.shareUrl;
      $('share-link-wrap').hidden = false;
    } else {
      $('share-link-wrap').hidden = true;
    }
  }

  function onChangeShareMode() {
    if (!state.current) { return; }
    var mode = $('share-mode').value;
    var prev = state.current.share_mode || 'off';
    updateShareModeHint();
    api('/api/docs/' + state.current.id + '/share', {
      method: 'POST',
      body: JSON.stringify({ mode: mode }),
      headers: { 'Content-Type': 'application/json' }
    }).then(function (resp) {
      state.current.share_mode = resp.mode;
      state.current.shared = resp.mode !== 'off';
      state.current.shareUrl = resp.url || '';
      $('p-share').classList.toggle('primary', resp.mode !== 'off');
      renderDocMeta(state.current);
      updateShareLink(resp.mode);
      loadList();
    }).catch(function (e) {
      $('share-mode').value = prev;
      updateShareModeHint();
      uiAlert(I18N.t('error.op_failed', { msg: e.message }), { title: I18N.t('error.op_failed_title') });
    });
  }
```

`bindEvents` 里把 `$('share-toggle').addEventListener('change', onToggleShare);` 换成：

```js
    $('share-mode').addEventListener('change', onChangeShareMode);
```

- [ ] **Step 4: 构建并自检**

Run: `go build -o mdbox . && grep -n "share-toggle" web/index.html web/app.js`
Expected: 构建成功；grep **无输出**（旧复选框引用已清干净）。

- [ ] **Step 5: 提交**

```bash
git add web/index.html web/app.js web/i18n.js
git commit -m "feat(web): 分享弹窗改为三态选择"
```

---

### Task 4: 分享页编辑态

**Files:**
- Modify: `web/index.html`（`#view-share`）
- Modify: `web/app.js`（`setupShare` 一带、新增函数）
- Modify: `web/style.css`
- Modify: `web/i18n.js`

**Interfaces:**
- Consumes: `GET /api/share/...` 返回的 `doc.share_mode` / `doc.content`；`PUT /api/share/...`；`POST /api/preview`
- Produces: `enterShareEdit()` / `exitShareEdit()` / `cancelShareEdit()` / `saveShareEdit()` / `updateShareChrome()`；`shareCtx = {user,id,sig,title,mode,content,editing,draft}`

- [ ] **Step 1: 改 HTML**

`#view-share` 顶栏里，`#share-clone` 之前插入：

```html
    <span id="share-msg" class="msg"></span>
    <button id="share-edit" class="ghost" data-i18n="share.edit" hidden>编辑</button>
```

把 `<span class="share-brand-tip" data-i18n="share.brand_tip">只读分享</span>` 改成（去掉 `data-i18n`，改由 JS 按模式设置）：

```html
    <span id="share-brand-tip" class="share-brand-tip">只读分享</span>
```

`.share-body` 内部改成：

```html
  <div class="share-body">
    <div class="share-inner">
      <article id="share-content" class="markdown"></article>
      <nav id="share-toc" class="toc" hidden data-i18n-aria="toc.title"></nav>
    </div>
    <div id="share-editor" class="share-editor" hidden>
      <div class="share-editbar">
        <span id="share-edit-hint" class="msg" data-i18n="share.edit_hint">保存后对方立即看到，无版本历史</span>
        <span class="spacer"></span>
        <button id="share-edit-cancel" class="ghost" data-i18n="share.cancel">取消</button>
        <button id="share-edit-save" class="primary" data-i18n="share.save">保存</button>
      </div>
      <div class="share-panes">
        <textarea id="share-source" class="source" spellcheck="false"></textarea>
        <div id="share-preview" class="preview markdown"></div>
      </div>
    </div>
  </div>
```

- [ ] **Step 2: 加 i18n 键**

中文表 `share.clone` 附近加：

```js
      'share.brand_tip_edit': '可共同编辑',
      'share.edit': '编辑',
      'share.save': '保存',
      'share.cancel': '取消',
      'share.save_ok': '已保存',
      'share.save_failed': '保存失败',
      'share.gone_on_save': '分享已失效或已关闭编辑，内容未保存。',
      'share.edit_hint': '保存后对方立即看到，无版本历史',
      'share.preview_need_login': '登录后才有实时预览',
```

英文表同位置加：

```js
      'share.brand_tip_edit': 'Editable',
      'share.edit': 'Edit',
      'share.save': 'Save',
      'share.cancel': 'Cancel',
      'share.save_ok': 'Saved',
      'share.save_failed': 'Save failed',
      'share.gone_on_save': 'This share link is invalid or editing was turned off. Nothing was saved.',
      'share.edit_hint': 'Changes are visible immediately. No version history.',
      'share.preview_need_login': 'Log in to see the live preview',
```

- [ ] **Step 3: 加样式**

`web/style.css` 里 `.share-inner` 那段后面追加：

```css
/* 分享页编辑态：占满 .share-body，两栏等宽，窄屏上下堆叠。 */
.share-body.editing { overflow: hidden; }
.share-editor { display: flex; flex-direction: column; height: 100%; }
.share-editbar { display: flex; align-items: center; gap: 8px; padding: 8px 14px; border-bottom: 1px solid var(--border); }
.share-panes { flex: 1; min-height: 0; display: grid; grid-template-columns: 1fr 1fr; }
.share-panes .source { border: 0; border-right: 1px solid var(--border); border-radius: 0; padding: 16px 14px; resize: none; }
.share-panes .preview { overflow: auto; padding: 16px 20px; }
@media (max-width: 860px) {
  .share-panes { grid-template-columns: 1fr; grid-template-rows: 1fr 1fr; }
  .share-panes .source { border-right: 0; border-bottom: 1px solid var(--border); }
}
```

- [ ] **Step 4: 改 `setupShare` 并新增编辑函数**

`shareCtx` 声明行改为：

```js
  var shareCtx = null; // { user, id, sig, title, mode, content, editing, draft }
  var sharePreviewTimer = null;
```

`setupShare` 整体替换为：

```js
  function setupShare() {
    showView('share');
    var parts = location.pathname.split('/').filter(Boolean); // ['s', user, id, sig]
    var content = $('share-content');
    if (parts.length < 4) {
      content.innerHTML = '<p>' + I18N.t('share.invalid') + '</p>';
      clearToc($('share-toc'));
      return;
    }
    var draft = shareCtx ? shareCtx.draft : null;
    shareCtx = { user: parts[1], id: parts[2], sig: parts[3], title: '', mode: 'off', content: '', editing: false, draft: draft };
    fetch(shareApi(), { headers: { 'Accept-Language': I18N.lang() } })
      .then(function (r) { return r.json().then(function (d) { return { ok: r.ok, data: d }; }); })
      .then(function (res) {
        if (!res.ok || res.data.error) {
          content.innerHTML = '<p>' + I18N.t('share.gone') + '</p>';
          clearToc($('share-toc'));
          return;
        }
        var doc = res.data.doc || {};
        shareCtx.title = doc.title || '';
        shareCtx.mode = doc.share_mode || 'off';
        shareCtx.content = doc.content || '';
        content.innerHTML = res.data.html || '';
        buildToc(content, $('share-toc'), document.querySelector('.share-body'));
        updateShareChrome();
        refreshTitle();
        if (shareCtx.draft != null) { enterShareEdit(); }
      })
      .catch(function () {
        content.innerHTML = '<p>' + I18N.t('share.load_failed') + '</p>';
        clearToc($('share-toc'));
      });
  }

  // 按分享模式刷新顶栏：可编辑时显示「编辑」按钮并改文案。
  function updateShareChrome() {
    if (!shareCtx) { return; }
    var editable = shareCtx.mode === 'edit';
    $('share-edit').hidden = !editable;
    $('share-brand-tip').textContent = I18N.t(editable ? 'share.brand_tip_edit' : 'share.brand_tip');
  }

  function enterShareEdit() {
    if (!shareCtx || shareCtx.mode !== 'edit') { return; }
    shareCtx.editing = true;
    $('share-source').value = shareCtx.draft != null ? shareCtx.draft : shareCtx.content;
    $('share-preview').innerHTML = $('share-content').innerHTML;
    $('share-content').hidden = true;
    $('share-toc').hidden = true;
    $('share-editor').hidden = false;
    document.querySelector('.share-body').classList.add('editing');
    $('share-source').focus();
  }

  function exitShareEdit() {
    if (!shareCtx) { return; }
    shareCtx.editing = false;
    shareCtx.draft = null;
    $('share-editor').hidden = true;
    $('share-content').hidden = false;
    document.querySelector('.share-body').classList.remove('editing');
    buildToc($('share-content'), $('share-toc'), document.querySelector('.share-body'));
  }

  function cancelShareEdit() {
    if (!shareCtx) { return; }
    if ($('share-source').value === shareCtx.content) { exitShareEdit(); return; }
    uiConfirm(I18N.t('doc.discard_confirm'), {
      title: I18N.t('doc.discard_title'), confirmText: I18N.t('doc.discard_ok'), danger: true
    }).then(function (ok) { if (ok) { exitShareEdit(); } });
  }

  function saveShareEdit() {
    if (!shareCtx) { return; }
    var content = $('share-source').value;
    fetch(shareApi(), {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'Accept-Language': I18N.lang() },
      body: JSON.stringify({ content: content })
    }).then(function (r) {
      return r.json().catch(function () { return {}; }).then(function (d) { return { status: r.status, data: d }; });
    }).then(function (res) {
      if (res.status === 401) {
        shareCtx.draft = content; // 放在 shareCtx 上，showLogin 里的 resetSession 不会清掉
        showLogin();
        return;
      }
      if (res.status === 404) {
        exitShareEdit();
        uiAlert(I18N.t('share.gone_on_save'), { title: I18N.t('action.dialog_title') });
        return;
      }
      if (res.status !== 200) { throw new Error(res.data.error || I18N.t('share.save_failed')); }
      shareCtx.content = content;
      $('share-content').innerHTML = res.data.html || '';
      exitShareEdit();
      msg('share-msg', I18N.t('share.save_ok'));
    }).catch(function (e) {
      uiAlert(e.message || I18N.t('share.save_failed'), { title: I18N.t('action.dialog_title') });
    });
  }
```

- [ ] **Step 5: 绑事件**

`bindShareEvents` 里 `$('share-clone').addEventListener(...)` 之后加：

```js
    $('share-edit').addEventListener('click', enterShareEdit);
    $('share-edit-save').addEventListener('click', saveShareEdit);
    $('share-edit-cancel').addEventListener('click', cancelShareEdit);
    $('share-source').addEventListener('input', function () {
      var v = this.value;
      clearTimeout(sharePreviewTimer);
      sharePreviewTimer = setTimeout(function () {
        api('/api/preview', {
          method: 'POST',
          body: JSON.stringify({ content: v }),
          headers: { 'Content-Type': 'application/json' },
          noAuthRedirect: true
        }).then(function (r) { $('share-preview').innerHTML = r.html || ''; })
          .catch(function () {
            $('share-preview').innerHTML = '<p class="msg">' + I18N.t('share.preview_need_login') + '</p>';
          });
      }, 300);
    });
```

注意 `noAuthRedirect: true` 不能省：`/api/preview` 需要登录，未登录时默认的 401 分支会把同事直接踢到登录页。

- [ ] **Step 6: 构建自检**

Run: `go build -o mdbox . && gofmt -l . && go vet ./...`
Expected: 构建成功，gofmt / vet 无输出。

- [ ] **Step 7: 提交**

```bash
git add web/index.html web/app.js web/i18n.js web/style.css
git commit -m "feat(web): 分享页可编辑态（就地编辑 + 实时预览 + 保存）"
```

---

### Task 5: 登录分流与草稿恢复

分享页现在根本不绑登录表单（`boot()` 在 `/s/` 分支直接 `return`），必须补上；同时 `setupShare` 会被二次调用，事件绑定要移出去，否则监听器翻倍。

**Files:**
- Modify: `web/app.js`（`boot`、`bindLogin` 成功回调、新增 `resumeShare`、`applyLang`）

**Interfaces:**
- Consumes: Task 4 的 `shareCtx.draft` 与 `setupShare`
- Produces: `resumeShare(user)`

- [ ] **Step 1: `boot` 的分享分支**

把

```js
    if (location.pathname.indexOf('/s/') === 0) { setupShare(); return; }
```

改成

```js
    if (location.pathname.indexOf('/s/') === 0) {
      bindShareEvents(); // 只绑一次：setupShare 会被 resumeShare 二次调用
      bindLogin();
      setupShare();
      return;
    }
```

同时删掉 `setupShare` 里的 `bindShareEvents();` 这一行。

- [ ] **Step 2: 登录成功后分流**

`bindLogin` 的成功回调：

```js
      }).then(function (d) {
        $('login-pass').value = '';
        $('login-pass2').value = '';
        setMode(false);
        enterApp(d.user);
      })
```

替换为：

```js
      }).then(function (d) {
        $('login-pass').value = '';
        $('login-pass2').value = '';
        setMode(false);
        if (location.pathname.indexOf('/s/') === 0) { resumeShare(d.user); }
        else { enterApp(d.user); }
      })
```

- [ ] **Step 3: 新增 `resumeShare`**

放在 `setupShare` 之前：

```js
  // 登录成功后回到分享页：重取文档，并把草稿填回编辑态。
  function resumeShare(user) {
    state.user = user;
    renderUser(user);
    setupShare();
  }
```

- [ ] **Step 4: 语言切换不要踩到应用视图**

`applyLang` 里插在**登录视图判断之后、`if (state.user)` 之前**（不能插在登录判断前面，否则分享流程里切语言时登录按钮的文案不会刷新）：

```js
    if (shareCtx) { updateShareChrome(); return; }
```

（`resumeShare` 会设置 `state.user`，不加这行的话在分享页切语言会触发 `loadList()`。）

- [ ] **Step 5: 构建自检**

Run: `go build -o mdbox .`
Expected: 成功。

- [ ] **Step 6: 提交**

```bash
git add web/app.js
git commit -m "feat(web): 分享页就地登录并恢复编辑草稿"
```

---

### Task 6: 文档同步

**Files:**
- Modify: `docs/PRD.md`（§2.3 非目标、§6.2 数据模型、§6.3 REST API、§12 变更记录）
- Modify: `CODEBUDDY.md`（`### Sharing` 一节）

- [ ] **Step 1: PRD §6.2 数据模型**

把 `shared` 那一行改成 `share`（取值 `off | view | edit`）+ 派生的 `shared`，并补一句：只写了 `shared: true` 的旧文件按 `view` 读取。

- [ ] **Step 2: PRD §6.3 REST API**

`POST /api/docs/{id}/share` 的请求体改为 `{mode}`、响应 `{mode, url}`；并新增一行：

```
PUT /api/share/{user}/{id}/{sig}   {content}  -> {doc, html}   需登录；仅 share_mode=edit 时可用
```

- [ ] **Step 3: PRD §2.3 非目标**

「不做权限体系与跨用户协作」保留，补一句限定：`edit` 分享是**整篇覆盖的单向写权限**，不做实时协同、版本历史与协作者白名单。

- [ ] **Step 4: PRD §12 变更记录**

加一行 v0.5（2026-10-24）：分享三态 + 可编辑链接；说明这是对 v0.4「不做多人共编」结论的有意修正，边界是单篇整篇覆盖、无历史、无审计。

- [ ] **Step 5: CODEBUDDY.md 的 `### Sharing`**

整节替换为：

```markdown
### Sharing

Per-document `share` has three states (`off | view | edit`); `Doc.Shared` is derived (`mode != off`). A shared doc is readable anonymously at `GET /api/share/{user}/{id}/{sig}` and `/s/{user}/{id}/{sig}` (serves `index.html`; the SPA enters read-only share mode by pathname). `sig = HMAC-SHA256(secret, user+"/"+id)[:24]` is deterministic, so links are stable; changing `secret` invalidates every link, and unsharing makes it 404.

In `edit` mode any **logged-in** user holding the link can rewrite the body in place via `PUT /api/share/{user}/{id}/{token}`, which targets the **owner's** store (`s.reg.Store(owner)`), not `stOf(r)`. `publicPath` must therefore stay method-aware: only `GET` under `/api/share/` is anonymous — a bare path-prefix check would silently expose the write endpoint. Writes reuse `store.Update`, so size/text/quota limits still apply. There is no version history, no conflict detection, no per-person attribution, and the write counts against the owner's quota.
```

- [ ] **Step 6: 提交**

```bash
git add docs/PRD.md CODEBUDDY.md
git commit -m "docs: 同步分享三态与可编辑链接"
```

---

### Task 7: 构建门禁与人工验收

**Files:** 无代码改动

- [ ] **Step 1: 全量门禁**

```bash
gofmt -l .        # 必须无输出
go vet ./...
go test ./...
go build -o mdbox .
```

- [ ] **Step 2: curl 冒烟（后端）**

```bash
./mdbox -addr :18080 -data ./tmpdata -users ./tmpusers.yaml &
# 注册两个账号，响应里带 token
curl -s -X POST localhost:18080/api/register -d '{"username":"alice","password":"password123"}'
curl -s -X POST localhost:18080/api/register -d '{"username":"bob","password":"password123"}'
```

依次验证：未带 token 的 PUT → 401；`{"mode":"view"}` 后 PUT → 404；`{"mode":"edit"}` 后 PUT → 200 且 `GET /api/share/...` 正文已变。跑完删掉 `tmpdata/` 与 `tmpusers.yaml`，并结束该进程。

- [ ] **Step 3: 交给作者的人工验收（不由 agent 执行）**

1. A 把文档设为「可编辑链接」，把链接发给 B。
2. B 登录后打开链接：顶栏显示「可共同编辑」+「编辑」按钮，改两段并保存，内容立即更新、目录正常。
3. A 刷新看到 B 的改动。
4. A 切回「只读链接」，B 页面再次保存 → 提示分享已失效。
5. B 退出登录后打开链接 → 点编辑 → 写一段 → 保存 → 被送到登录 → 登录后回到编辑态且草稿还在。
6. 中英两种界面语言下按钮与提示正确。
7. 回归：v0.4 之前开的分享链接仍能只读打开；clone 行为不变。

- [ ] **Step 4: 收尾**

确认 `git status` 干净、`tmpdata/` 已删除。**不要 push**，等作者决定。

---

## 自查结果

- 规格覆盖：三态模型（Task 1）、`publicPath` 方法化 + 写入端点（Task 2）、分享弹窗（Task 3）、分享页编辑态（Task 4）、就地登录与草稿（Task 5）、文档同步（Task 6）、验收（Task 7）——设计稿的每个小节都有对应任务。
- 类型一致性：`store.ShareOff/ShareView/ShareEdit`、`ValidShareMode`、`SetShareMode`、`ErrBadShareMode`、`Doc.ShareMode` 在任务间名称一致；前端统一用 `share_mode` 字段与 `shareCtx.mode`。
- 已知缺口（设计稿明确接受，不在本计划中处理）：无版本历史、无冲突检测、无编辑者审计、写入计入作者配额。
