# 分享页「复制到我的库」（clone） Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让已登录的 mdbox 用户在分享页一键把该文档复制一份到自己库（只带标题 + 正文），源文档不受影响。

**Architecture:** 新增一个**需鉴权**的 REST 端点 `POST /api/clone/{user}/{id}/{token}`。它复用现有 `sharedDoc()`（校验分享签名 + 取出源文档）读取源文档，再用 `stOf(r)`（当前登录用户的 store）调用现有 `store.Create` 建一份独立副本。**store 层零改动**，不引入协作/权限模型。

**Tech Stack:** Go 1.23（标准库 `net/http` + `httptest`）、vanilla JS（`web/app.js`）、`web/i18n.js` 中英字典；前端经 `//go:embed` 打包。

## Global Constraints

- **语言**：后端代码注释 / 面向用户的后端文案用中文；前端新增文案必须**中英双语**（`web/i18n.js` 的 zh 与 en 都要加键）。
- **提交**：直接在 `main` 上提交，不建分支、不用 worktree；**不要 `git push`**（推送由作者确认）。
- **前端改动需重新构建**才生效（`//go:embed web`）；`.gitattributes` 把 `*.js` 标为 binary，`git diff` 看不到内容属正常，需 `git diff --text -- web/app.js`。
- **验收边界**：subagent 只跑 `gofmt -l .` / `go vet ./...` / `go test ./...` / `go build` 这类门禁；**浏览器验收由作者本人做**，不要声称"已视觉验证"。
- 只在 Go 1.23+ 构建，无新增第三方依赖。

---

### Task 1: 后端 clone 端点

**Files:**
- Modify: `internal/api/server.go`（在分享路由后加一行）
- Modify: `internal/api/share.go`（新增 `cloneShared` handler）
- Create: `internal/api/clone_test.go`
- Modify: `docs/PRD.md`（§6.2 `source` 取值域、§6.3 REST API 表）

**Interfaces:**
- Consumes（均已在 `internal/api` 内）：
  - `func (s *Server) sharedDoc(r *http.Request) (*store.Doc, bool)` — 校验 `{user}/{id}/{token}` 签名 + `Shared` 标志，返回含正文的源文档（`internal/api/share.go:53`）
  - `func stOf(r *http.Request) *store.Store` — 当前登录用户的 store（`internal/api/server.go:40`）
  - `func writeErr(w http.ResponseWriter, r *http.Request, status int, msg string)`、`func writeJSON(w http.ResponseWriter, status int, v any)`
  - `store.CreateInput{Title, Content, Tags, Category, Source string/slice}`、`(*store.Store).Create(in) (*store.Doc, error)`
  - `i18n.KeyShareGone`
- Produces：
  - 路由 `POST /api/clone/{user}/{id}/{token}`
  - `func (s *Server) cloneShared(w http.ResponseWriter, r *http.Request)` — 成功返回 `201 {"doc": Doc}`

- [ ] **Step 1: 写失败的测试**

创建 `internal/api/clone_test.go`：

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mdbox/internal/auth"
	"mdbox/internal/config"
	"mdbox/internal/store"
	"mdbox/internal/users"
)

// newTestServer 建一个带真实注册表与临时目录的 api handler，并返回注册表与配置。
func newTestServer(t *testing.T) (*users.Registry, http.Handler, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	reg, err := users.Open(filepath.Join(dir, "users.yaml"), dir)
	if err != nil {
		t.Fatalf("打开用户表失败: %v", err)
	}
	cfg := &config.Config{Secret: "test-secret"}
	h := New(reg, cfg, auth.New())
	return reg, h, cfg
}

func TestCloneSharedDocCreatesCopyInCallerLibrary(t *testing.T) {
	reg, h, cfg := newTestServer(t)

	if _, err := reg.Register("alice", "password123"); err != nil {
		t.Fatalf("注册 alice 失败: %v", err)
	}
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}

	aliceStore, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 alice store 失败: %v", err)
	}
	src, err := aliceStore.Create(store.CreateInput{
		Title: "源标题", Content: "# 正文\n\n内容", Tags: []string{"x"}, Category: "c", Source: "web",
	})
	if err != nil {
		t.Fatalf("创建源文档失败: %v", err)
	}
	if _, err := aliceStore.SetShared(src.ID, true); err != nil {
		t.Fatalf("开启分享失败: %v", err)
	}
	srcDisk, err := aliceStore.Get(src.ID) // 从磁盘读回，正文含末尾换行，便于精确比较
	if err != nil {
		t.Fatalf("读取源文档失败: %v", err)
	}

	sig := (&Server{cfg: cfg}).shareSig("alice", src.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+src.ID+"/"+sig, nil)
	req.Header.Set("Authorization", "Bearer "+bob.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("状态码 = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Doc store.Doc `json:"doc"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Doc.Source != "clone" {
		t.Errorf("source = %q, want clone", resp.Doc.Source)
	}
	if len(resp.Doc.Tags) != 0 {
		t.Errorf("tags = %v, want empty", resp.Doc.Tags)
	}
	if resp.Doc.Category != "" {
		t.Errorf("category = %q, want empty", resp.Doc.Category)
	}
	if resp.Doc.Shared {
		t.Errorf("shared = true, want false")
	}
	if resp.Doc.Title != "源标题" {
		t.Errorf("title = %q, want 源标题", resp.Doc.Title)
	}

	// 副本落在 bob 自己的库里，正文与源一致
	bobStore, err := reg.Store("bob")
	if err != nil {
		t.Fatalf("取 bob store 失败: %v", err)
	}
	got, err := bobStore.Get(resp.Doc.ID)
	if err != nil {
		t.Fatalf("bob 库中找不到副本: %v", err)
	}
	if got.Content != srcDisk.Content {
		t.Errorf("副本正文 = %q, want %q", got.Content, srcDisk.Content)
	}

	// 源文档元数据不变
	after, err := aliceStore.Get(src.ID)
	if err != nil {
		t.Fatalf("再次读取源文档失败: %v", err)
	}
	if len(after.Tags) != 1 || after.Tags[0] != "x" || after.Category != "c" {
		t.Errorf("源文档元数据被改动: tags=%v category=%q", after.Tags, after.Category)
	}
}

func TestCloneSharedDocUnauthorized(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	reg.Register("alice", "password123")
	aliceStore, _ := reg.Store("alice")
	src, _ := aliceStore.Create(store.CreateInput{Title: "T", Content: "b", Source: "web"})
	aliceStore.SetShared(src.ID, true)
	sig := (&Server{cfg: cfg}).shareSig("alice", src.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+src.ID+"/"+sig, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录状态码 = %d, want 401", rec.Code)
	}
}

func TestCloneUnsharedDocNotFound(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	reg.Register("alice", "password123")
	bob, _ := reg.Register("bob", "password123")
	aliceStore, _ := reg.Store("alice")
	src, _ := aliceStore.Create(store.CreateInput{Title: "T", Content: "b", Source: "web"}) // 未开启分享
	sig := (&Server{cfg: cfg}).shareSig("alice", src.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+src.ID+"/"+sig, nil)
	req.Header.Set("Authorization", "Bearer "+bob.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("未分享文档状态码 = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/api/ -run TestClone -v`
Expected: FAIL（`cloneShared` 未定义 / 路由未注册，请求落到 404 或 405；且 `newTestServer` 若与实现冲突会编译失败）

- [ ] **Step 3: 注册路由**

在 `internal/api/server.go` 中，紧跟现有分享路由之后（第 68 行 `publicDownload` 那行后）插入：

```go
	mux.HandleFunc("POST /api/clone/{user}/{id}/{token}", s.cloneShared)
```

> 路径值名 `{user}` / `{id}` / `{token}` 必须与 `sharedDoc` 里读取的名字一致。该路径**不在** `publicPath` 的 `/api/share/` 前缀内，因此自动落入 `withAuth`（未登录 401），且 handler 内 `stOf(r)` 就是调用者自己的 store。

- [ ] **Step 4: 实现 handler**

在 `internal/api/share.go` 末尾追加：

```go
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
```

> `share.go` 已 import `net/http`、`mdbox/internal/i18n`、`mdbox/internal/store`，无需改 import。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/api/ -run TestClone -v`
Expected: PASS（`TestCloneSharedDocCreatesCopyInCallerLibrary` / `TestCloneSharedDocUnauthorized` / `TestCloneUnsharedDocNotFound` 全绿）

- [ ] **Step 6: 更新 PRD**

`docs/PRD.md` §6.2 数据模型里的 `source` 行：

```yaml
source: agent | codebuddy | upload | web | clone
```

`docs/PRD.md` §6.3 REST API 表，在 `POST /api/docs/{id}/share` 行后加一行：

```
| POST | `/api/clone/{user}/{id}/{token}` | **需登录**：把已分享的文档复制一份到当前用户自己的库（只带标题 + 正文，`source=clone`） |
```

- [ ] **Step 7: 提交**

```bash
git add internal/api/server.go internal/api/share.go internal/api/clone_test.go docs/PRD.md
git commit -m "feat(api): 分享页 clone 端点——已分享文档复制到自己的库"
```

---

### Task 2: 前端分享页「复制到我的库」按钮

**Files:**
- Modify: `web/i18n.js`（zh 与 en 各加 2 个键）
- Modify: `web/index.html`（分享页顶栏加按钮）
- Modify: `web/app.js`（`bindShareEvents` 绑定 + 新增 `cloneShared()`）

**Interfaces:**
- Consumes：`shareCtx`（`{user,id,sig,title}`，`web/app.js:957`）、`$`（`getElementById` 包装）、`I18N.t`、`uiAlert(msg, {title})`
- Produces：按钮 `#share-clone`；`function cloneShared()`（POST `/api/clone/...` → 401 跳登录 / 201 跳 `/#/doc/<id>`）

- [ ] **Step 1: 加 i18n 键**

`web/i18n.js` 的 `zh` 块，紧跟 `'share.download_failed': '下载失败',` 之后加：

```js
      'share.clone': '复制到我的库',
      'share.clone_failed': '复制失败',
```

`web/i18n.js` 的 `en` 块，紧跟 `'share.download_failed': 'Download failed',` 之后加：

```js
      'share.clone': 'Copy to my library',
      'share.clone_failed': 'Copy failed',
```

- [ ] **Step 2: 加按钮**

`web/index.html` 中，在分享页语言按钮 `</button>`（第 58 行）与下载下拉 `<div class="dropdown">`（第 59 行）之间插入：

```html
    <button id="share-clone" class="ghost" data-i18n="share.clone">复制到我的库</button>
```

> 复用现有 `.ghost` 按钮样式（与 `#share-download` 同款），无需改 `web/style.css`。

- [ ] **Step 3: 绑定事件 + 实现 clone**

在 `web/app.js` 的 `bindShareEvents()` 内，`$('share-brand').addEventListener(...)` 那行后加：

```js
    $('share-clone').addEventListener('click', cloneShared);
```

在 `shareDownloadPdf()` 函数后（第 1006 行之后）新增：

```js
  // 复制当前分享的文档到自己登录账号的库里，成功后打开副本。
  function cloneShared() {
    if (!shareCtx) { return; }
    fetch('/api/clone/' + encodeURIComponent(shareCtx.user) + '/' +
          encodeURIComponent(shareCtx.id) + '/' + encodeURIComponent(shareCtx.sig), {
      method: 'POST',
      headers: { 'Accept-Language': I18N.lang() }
    }).then(function (r) {
      if (r.status === 401) { location.href = '/'; return null; } // 未登录 → 跳登录页
      if (!r.ok) { throw new Error(I18N.t('share.clone_failed')); }
      return r.json();
    }).then(function (data) {
      if (!data || !data.doc) { return; }
      location.href = '/#/doc/' + encodeURIComponent(data.doc.id); // 进自己的库并打开副本
    }).catch(function (e) {
      uiAlert(e.message || I18N.t('share.clone_failed'), { title: I18N.t('action.dialog_title') });
    });
  }
```

> `enterApp` 在 `hashDocId()` 有值时即 `route()`（`web/app.js:228`），因此跳 `/#/doc/<id>` 会打开新副本。

- [ ] **Step 4: 重新构建（前端 embed 生效）**

Run: `go build -o mdbox .`
Expected: 构建成功（无输出即成功）

- [ ] **Step 5: 提交**

```bash
git add web/i18n.js web/index.html web/app.js
git commit -m "feat(web): 分享页加「复制到我的库」按钮"
```

---

### Task 3: 门禁与人工验收

**Files:** 无代码改动（仅验证；如验收发现问题，回到对应 Task 修复）

- [ ] **Step 1: 跑全部构建门禁**

```bash
gofmt -l .          # 期望：无输出（新增 Go 代码已格式化）
go vet ./...        # 期望：无输出
go test ./...       # 期望：全部 PASS
go build -o mdbox . # 期望：成功
```

- [ ] **Step 2: 后端 curl 冒烟（可选，作者可做）**

起服务后：用一个账号 A 建一篇文档并开启分享拿到链接，用账号 B 的 token 调 clone：

```bash
# 期望 201，且返回 doc.source=clone、doc.tags=[]、doc.shared=false
curl -s -X POST http://localhost:8080/api/clone/<A>/<id>/<sig> \
  -H "Authorization: Bearer <B的token>"
# 期望 401
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8080/api/clone/<A>/<id>/<sig>
```

- [ ] **Step 3: 作者浏览器人工验收（DEFERRED 给作者，subagent 不执行）**

- [ ] 以**已登录**用户打开别人分享的页面 → 点「复制到我的库」→ 跳到自己的库并打开副本，可编辑。
- [ ] **未登录**访客在分享页点击 → 跳到登录页。
- [ ] 在分享页把界面切到英文 → 按钮显示 `Copy to my library`；中文 → `复制到我的库`。
- [ ] 确认**源文档**未被改动（内容、标签、更新时间不变）。
- [ ] 反复点击 → 产生多份独立副本（预期行为）。

- [ ] **Step 4: 通过后报告**

报告：门禁四条命令结果、curl 状态码、人工验收勾选情况。**不要**声称浏览器已验证（那是作者的活）。

---

## 收尾提醒

- 两个提交（Task 1、Task 2）留在本地 `main`，**不要 push**；由作者确认后另行推送。
- 本设计守 PRD §2.3 非目标（不做协作），`source` 仅新增 `clone` 取值，不改变既有语义。
