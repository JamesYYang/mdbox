# 归档与删除入口统一 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「归档 / 删除 / 恢复」收敛为列表卡片与文档页两处同构的「⋯」菜单，并补上归档的逆操作，让归档不再是单行道。

**Architecture:** 后端在 `internal/store` 新增与 `Archive` 对称的 `Unarchive`（文件 `archive/` → `docs/`），经 `internal/api` 暴露 `POST /api/docs/{id}/unarchive`、经 `internal/mcp` 暴露 `unarchive_doc`；前端删除编辑态的归档按钮，改为预览栏与列表卡片各一个「⋯」下拉菜单，菜单项由文档 `status` 决定（active → 归档·删除，archived → 恢复·删除），两处共用同一组动作函数与菜单开关函数。

**Tech Stack:** Go 标准库（net/http 路由、mux pattern）、mark3labs/mcp-go、原生 JS/CSS（`//go:embed` 打包）、goldmark 无关。

## Global Constraints

- 代码注释与用户可见文案用中文；英文只出现在 `web/i18n.js` 的 `en` 字典与 MCP 工具描述里。
- 前端改动必须 `go build` 才生效（`main.go` 用 `//go:embed web` 打包），不要用 `go run .` 之外的方式验证旧资源。
- 不新增依赖；不引入构建工具、不做前端组件抽象。
- 窄屏（<= 720px）保持只读：`#btn-new, #btn-upload, #p-edit, .card-actions, .doc-more-wrap { display: none; }`。
- store 层是唯一真相源，任何新能力先进 `store`，REST 与 MCP 都只是它的接口。
- 每个任务结束必须通过：`gofmt -l .`（无输出）、`go test ./...`、`go build -o mdbox .`。
- 前端交互不做自动化测试（仓库无浏览器测试基建），由作者本人浏览器验收；不要为此加依赖。

---

### Task 1: store.Unarchive + 测试

**Files:**
- Modify: `internal/store/store.go`（在 `Archive` 之后，约 395 行后新增）
- Test: `internal/store/store_test.go`（文件末尾追加）

**Interfaces:**
- Consumes: 已有的 `Store.docs map[string]*entry`、`entry.path`、`readBody(path) string`、`serialize(doc, content) []byte`、`ErrNotFound`
- Produces: `func (s *Store) Unarchive(id string) (*Doc, error)` —— 任务 2 的 api/mcp 直接调用；返回 `*Doc`，成功时 `Status == "active"`，`Content` 不填（与 `Archive` 保持一致）

- [ ] **Step 1: 写失败测试**

在 `internal/store/store_test.go` 末尾追加（先把 import 块补上 `os`、`path/filepath`）：

```go
// TestArchiveUnarchiveRoundTrip 归档是移到 archive/，恢复必须原样搬回 docs/，
// 正文与标题都不能丢——这正是「误归档只能真删重写」的痛点所在。
func TestArchiveUnarchiveRoundTrip(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "旧稿", Content: "# 旧稿\n\n正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("归档失败: %v", err)
	}

	time.Sleep(20 * time.Millisecond)
	got, err := st.Unarchive(d.ID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want active", got.Status)
	}
	if got.Title != "旧稿" {
		t.Errorf("title = %q, want 旧稿", got.Title)
	}
	if _, err := os.Stat(filepath.Join(st.root, "docs", d.ID+".md")); err != nil {
		t.Errorf("docs/ 下应存在该文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.root, "archive", d.ID+".md")); !os.IsNotExist(err) {
		t.Errorf("archive/ 下不应残留文件: err = %v", err)
	}
	full, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if full.Content != "# 旧稿\n\n正文" {
		t.Errorf("正文 = %q, want 原内容", full.Content)
	}
	active, err := st.List(ListFilter{Status: "active"})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(active) != 1 || active[0].ID != d.ID {
		t.Errorf("活动列表 = %+v, want 仅 %s", active, d.ID)
	}
}

// TestUnarchiveActiveDocIsNoop 对活动文档点「恢复」不该改任何东西，尤其不该刷新 updated。
func TestUnarchiveActiveDocIsNoop(t *testing.T) {
	st := newTestStore(t)
	d, err := st.Create(CreateInput{Title: "t", Content: "x"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	got, err := st.Unarchive(d.ID)
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if !got.Updated.Equal(d.Updated) {
		t.Errorf("updated 被改写: %v -> %v", d.Updated, got.Updated)
	}
}

func TestUnarchiveUnknownID(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.Unarchive("20260101-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store -run 'TestArchiveUnarchiveRoundTrip|TestUnarchiveActiveDocIsNoop|TestUnarchiveUnknownID' -v`
Expected: 编译失败 `st.Unarchive undefined (type *Store has no field or method Unarchive)`

- [ ] **Step 3: 实现 Unarchive**

在 `internal/store/store.go` 的 `Archive` 函数之后新增：

```go
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
	content := readBody(e.path)
	e.Status = "active"
	e.Updated = time.Now()
	if err := os.WriteFile(dst, []byte(serialize(&e.Doc, content)), 0o644); err != nil {
		return nil, err
	}
	_ = os.Remove(e.path)
	e.path = dst
	out := e.Doc
	return &out, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/store -v`
Expected: 全部 PASS（含原有 `TestQuotaCountsArchived` 等）

- [ ] **Step 5: 提交**

```bash
gofmt -l internal/store
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): 新增 Unarchive，把归档文档搬回活动区"
```

---

### Task 2: REST 路由与 MCP 工具

**Files:**
- Modify: `internal/api/server.go`（`archive` 路由旁）
- Modify: `internal/api/api.go`（`archiveDoc` 之后）
- Modify: `internal/mcp/mcp.go`（`archive_doc` 之后）

**Interfaces:**
- Consumes: `(*store.Store).Unarchive(id string) (*store.Doc, error)`（Task 1）、既有 `stOf(r)`、`writeErr`、`writeJSON`、`toolErr`、`toolJSON`
- Produces: HTTP `POST /api/docs/{id}/unarchive` → `{"doc": {...}}`；MCP 工具 `unarchive_doc`（参数 `id`，结果 `{"id": ..., "message": "Unarchived"}`）

- [ ] **Step 1: 加路由**

`internal/api/server.go`，在 `mux.HandleFunc("POST /api/docs/{id}/archive", s.archiveDoc)` 下一行插入：

```go
mux.HandleFunc("POST /api/docs/{id}/unarchive", s.unarchiveDoc)
```

- [ ] **Step 2: 加 handler**

`internal/api/api.go`，紧跟 `archiveDoc` 函数之后新增（结构与 `archiveDoc` 完全对称）：

```go
func (s *Server) unarchiveDoc(w http.ResponseWriter, r *http.Request) {
	d, err := stOf(r).Unarchive(r.PathValue("id"))
	if err != nil {
		writeErr(w, r, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"doc": d})
}
```

- [ ] **Step 3: 加 MCP 工具**

`internal/mcp/mcp.go`，在 `archive_doc` 的 `s.AddTool(...)` 调用之后、`return s` 之前插入：

```go
s.AddTool(mcp.NewTool("unarchive_doc",
	mcp.WithDescription("Move a document out of the archive back to active documents"),
	mcp.WithString("id", mcp.Description("Document id"), mcp.Required()),
), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return toolErr(err)
	}
	if _, err := st.Unarchive(id); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"id": id, "message": "Unarchived"})
})
```

- [ ] **Step 4: 编译与全量测试**

Run: `gofmt -l . && go build -o mdbox . && go test ./...`
Expected: `gofmt` 无输出；build 成功；`go test` 全部 ok（`internal/mcp` 无测试，只看编译）

- [ ] **Step 5: 手工验证 REST（临时实例）**

```bash
rm -rf /tmp/mdbox-u /tmp/mdbox-u-users.yaml /tmp/mdbox-u.jar
./mdbox -addr :18080 -data /tmp/mdbox-u -users /tmp/mdbox-u-users.yaml > /tmp/mdbox-u.log 2>&1 &
sleep 1
curl -s -c /tmp/mdbox-u.jar -X POST localhost:18080/api/register \
  -H 'Content-Type: application/json' -d '{"username":"alice","password":"pass12345"}' > /dev/null
TOKEN=$(curl -s -b /tmp/mdbox-u.jar localhost:18080/api/me | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
ID=$(curl -s -X POST localhost:18080/api/docs -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"t","content":"hi"}' \
  | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
curl -s -X POST localhost:18080/api/docs/$ID/archive -H "Authorization: Bearer $TOKEN"; echo
curl -s -X POST localhost:18080/api/docs/$ID/unarchive -H "Authorization: Bearer $TOKEN"; echo
ls /tmp/mdbox-u/users/alice/docs/$ID.md
kill %1
```
Expected: 两条 POST 都返回 `{"doc":{...}}`（第二次 `"status":"active"`）；`ls` 打印出该文件；恢复后 `/tmp/mdbox-u/users/alice/archive/` 里没有该文件。验完 `kill %1` 并删掉 `/tmp/mdbox-u*`。

- [ ] **Step 6: 提交**

```bash
git add internal/api/server.go internal/api/api.go internal/mcp/mcp.go
git commit -m "feat(api,mcp): 暴露 unarchive 接口与 unarchive_doc 工具"
```

---

### Task 3: i18n 文案

**Files:**
- Modify: `web/i18n.js`（`zh` 与 `en` 两个字典，都在 `'action.archive'` 附近）

**Interfaces:**
- Produces: 6 个 key，Task 4/5 直接 `I18N.t(...)` 使用：`action.more_aria`、`action.restore`、`doc.restore_title`、`doc.restore_confirm`、`doc.restore_failed`、`doc.archive_failed`（后两个带 `{msg}` 变量）

- [ ] **Step 1: 加中文条目**

在 `zh` 字典 `'action.archive': '归档',` 下一行插入：

```js
      'action.more_aria': '更多操作',
      'action.restore': '恢复',
```

再在中文 `'doc.archive_title': '归档文档',` 下一行插入（`doc.archive_failed` 是本次新补的，原来归档失败只写进编辑态消息栏，现在菜单不在编辑态了）：

```js
      'doc.archive_failed': '归档失败：{msg}',
      'doc.restore_title': '恢复文档',
      'doc.restore_confirm': '恢复到活动文档？',
      'doc.restore_failed': '恢复失败：{msg}',
```

- [ ] **Step 2: 加英文条目**

在 `en` 字典 `'action.archive': 'Archive',` 下一行插入：

```js
      'action.more_aria': 'More actions',
      'action.restore': 'Restore',
```

再在英文 `'doc.archive_title': 'Archive document',` 下一行插入：

```js
      'doc.archive_failed': 'Archive failed: {msg}',
      'doc.restore_title': 'Restore document',
      'doc.restore_confirm': 'Move back to active documents?',
      'doc.restore_failed': 'Restore failed: {msg}',
```

- [ ] **Step 3: 确认 key 完整且无重复**

Run: `grep -c "'action.more_aria'\|'action.restore'\|'doc.restore_title'\|'doc.restore_confirm'\|'doc.restore_failed'" web/i18n.js`
Expected: `10`（5 个 key × zh/en 各一次）

- [ ] **Step 4: 提交**

```bash
git add web/i18n.js
git commit -m "i18n: 补充归档恢复与「更多操作」文案"
```

---

### Task 4: 文档页「⋯」菜单 + 共享菜单开关

**Files:**
- Modify: `web/index.html`（`#preview-bar` 内、`#edit-bar` 内）
- Modify: `web/app.js`（`openDoc`、`archiveDoc`、新增 `unarchiveDoc`/`closeMenus`/`menuButton`、`bindEvents`、全局点击与 Escape）
- Modify: `web/style.css`（`.dropdown-menu` 附近新增 `.menu-danger`；窄屏规则）

**Interfaces:**
- Consumes: `I18N.t`、`uiConfirm`、`uiAlert`、`api`、`refresh`、`state.current`、`deleteDoc(id, title)`
- Produces: `closeMenus()`（Task 5 复用）、`menuButton(key, onClick, danger)` → `HTMLButtonElement`（Task 5 复用）、`archiveDoc(id)` 与 `unarchiveDoc(id)` 均可无参调用（取 `state.current`）

- [ ] **Step 1: 改 index.html 工具条**

`#preview-bar` 内，把 `<button id="p-edit" class="primary" data-i18n="action.edit">编辑</button>` 替换为：

```html
          <button id="p-edit" class="primary" data-i18n="action.edit">编辑</button>
          <div class="dropdown doc-more-wrap">
            <button id="doc-more" class="ghost" data-i18n-aria="action.more_aria" aria-haspopup="menu">
              <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="19" cy="12" r="1.7"/></svg>
            </button>
            <div id="doc-menu" class="dropdown-menu" hidden>
              <button id="doc-archive" data-i18n="action.archive">归档</button>
              <button id="doc-restore" data-i18n="action.restore" hidden>恢复</button>
              <button id="doc-delete" class="menu-danger" data-i18n="action.delete">删除</button>
            </div>
          </div>
```

`#edit-bar` 内删除这一行（归档不再出现在编辑态）：

```html
          <button id="e-archive" class="ghost" data-i18n="action.archive">归档</button>
```

- [ ] **Step 2: 加共享菜单函数**

`web/app.js` 中，紧跟 `function deleteDoc(id, title) { ... }` 之前插入：

```js
  // 关闭所有「⋯」菜单。列表卡片菜单与文档页菜单共用，保证同时只开一个。
  function closeMenus() {
    var menus = document.querySelectorAll('.dropdown-menu');
    Array.prototype.forEach.call(menus, function (m) { m.hidden = true; });
    var wraps = document.querySelectorAll('.card-actions.open');
    Array.prototype.forEach.call(wraps, function (w) { w.classList.remove('open'); });
  }

  // menuButton 生成一个菜单项，danger 为真时用红字（删除这类不可恢复的动作）。
  function menuButton(key, onClick, danger) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = danger ? 'menu-danger' : '';
    b.textContent = I18N.t(key);
    b.addEventListener('click', function (e) { e.stopPropagation(); onClick(); });
    return b;
  }
```

（`closeMenus` 会把 `#download-menu` 一起关掉，这是想要的行为：一次只留一个菜单。）

- [ ] **Step 3: 让 archiveDoc 支持 id 参数，新增 unarchiveDoc**

把现有 `function archiveDoc() {...}` 整体替换为：

```js
  // archiveDoc 归档（软删除）。传参用于列表卡片，不传则取当前打开的文档。
  function archiveDoc(id) {
    if (!id && state.current) { id = state.current.id; }
    if (!id) { return; }
    uiConfirm(I18N.t('doc.archive_confirm'), { title: I18N.t('doc.archive_title'), confirmText: I18N.t('action.archive') })
      .then(function (ok) {
        if (!ok) { return; }
        api('/api/docs/' + id + '/archive', { method: 'POST' })
          .then(function () { state.current = null; state.dirty = false; refresh(); })
          .catch(function (e) { uiAlert(I18N.t('doc.archive_failed', { msg: e.message }), { title: I18N.t('doc.archive_title') }); });
      });
  }

  // unarchiveDoc 恢复（Archive 的逆操作）。成功后回列表，用户能看到它重新出现。
  function unarchiveDoc(id) {
    if (!id && state.current) { id = state.current.id; }
    if (!id) { return; }
    api('/api/docs/' + id + '/unarchive', { method: 'POST' })
      .then(function () { state.current = null; state.dirty = false; refresh(); })
      .catch(function (e) { uiAlert(I18N.t('doc.restore_failed', { msg: e.message }), { title: I18N.t('doc.restore_title') }); });
  }
```

注意：`doc.archive_confirm` 现在的文案是否带 `{title}`？先确认再定：

Run: `grep -n "doc.archive_confirm\|doc.archive_failed" web/i18n.js`
- 若 `doc.archive_confirm` 不带 `{title}`，把上面 `{ title: title || '' }` 去掉，写成 `I18N.t('doc.archive_confirm')`。
- 若不存在 `doc.archive_failed`，改用现有的 `doc.delete_failed` 风格新增一条：中文 `'doc.archive_failed': '归档失败：{msg}',`、英文 `'doc.archive_failed': 'Archive failed: {msg}',`（放进 Task 3 同样的位置，一并提交到本任务）。

- [ ] **Step 4: deleteDoc 成功后回列表**

把 `deleteDoc` 里成功回调 `.then(function () { loadSidebar(); loadList(); })` 改为：

```js
          .then(function () { state.current = null; state.dirty = false; refresh(); })
```

（`refresh()` 本身就是 `showContent('list') + leaveDocUrl() + loadSidebar() + loadList()`，从文档页删完也能正确回列表。）

- [ ] **Step 5: openDoc 按状态切换菜单项**

在 `openDoc` 里 `$('p-share').classList.toggle('primary', !!data.doc.shared);` 下一行插入：

```js
      var isArchived = data.doc.status === 'archived';
      $('doc-archive').hidden = isArchived;
      $('doc-restore').hidden = !isArchived;
      $('doc-menu').hidden = true;
```

- [ ] **Step 6: 绑定事件**

`bindEvents` 里删掉 `$('e-archive').addEventListener('click', archiveDoc);`，并在 `$('p-share').addEventListener('click', openShare);` 之后插入：

```js
    $('doc-more').addEventListener('click', function (e) {
      e.stopPropagation();
      var wasOpen = !$('doc-menu').hidden;
      closeMenus();
      $('doc-menu').hidden = wasOpen;
    });
    $('doc-archive').addEventListener('click', function () { closeMenus(); archiveDoc(); });
    $('doc-restore').addEventListener('click', function () { closeMenus(); unarchiveDoc(); });
    $('doc-delete').addEventListener('click', function () {
      closeMenus();
      if (state.current) { deleteDoc(state.current.id, state.current.title || state.current.id); }
    });
```

- [ ] **Step 7: 空白处点击与 Escape 收起菜单**

把 document 级点击处理里的这两行：

```js
      var menu = $('download-menu');
      if (menu && !menu.hidden) { menu.hidden = true; }
```

替换为：

```js
      closeMenus();
```

在 `window.keydown` 的 `Escape` 分支最前面（`if (!$('user-menu').hidden)...` 之前）插入：

```js
        if (document.querySelector('.dropdown-menu:not([hidden])')) { closeMenus(); return; }
```

- [ ] **Step 8: CSS**

`web/style.css` 的 `.dropdown-menu button:hover { background: var(--accent-soft); }` 之后新增：

```css
.dropdown-menu button.menu-danger { color: var(--danger); }
.dropdown-menu button.menu-danger:hover { background: var(--danger-soft, rgba(192, 57, 43, .1)); }
```

（没有 `--danger-soft` 变量，回退值即生效，不新增变量。）

窄屏规则里把 `#e-archive` 换成 `.doc-more-wrap`：

```css
  #btn-new, #btn-upload, #p-edit, .doc-more-wrap, .card-actions { display: none; }
```

- [ ] **Step 9: 构建并自检**

Run: `go build -o mdbox . && go test ./...`
Expected: build 成功、测试全绿。

Run: `node --check web/app.js && node --check web/i18n.js`
Expected: 无输出（语法通过）。若本机无 node，跳过并在浏览器控制台确认无报错。

- [ ] **Step 10: 提交**

```bash
git add web/index.html web/app.js web/style.css web/i18n.js
git commit -m "feat(web): 文档页改用「⋯」菜单，归档/恢复不再绑在编辑态"
```

---

### Task 5: 列表卡片「⋯」菜单

**Files:**
- Modify: `web/app.js`（`renderList` 内卡片渲染）
- Modify: `web/style.css`（`.card-del` 块整体替换，以及 `@media (hover:none)` 与窄屏两条引用）

**Interfaces:**
- Consumes: `closeMenus()`、`menuButton(key, onClick, danger)`、`archiveDoc(id)`、`unarchiveDoc(id)`、`deleteDoc(id, title)`（均来自 Task 4）
- Produces: 每张卡片右上角的 `.card-actions` 包裹层（含 `.card-more` 按钮与 `.dropdown-menu.card-menu`）

- [ ] **Step 1: 替换卡片上的删除按钮**

`web/app.js` 的 `renderList` 里，把这一整段：

```js
      // 删除按钮：默认隐藏，鼠标悬停卡片时出现
      var del = document.createElement('button');
      del.type = 'button';
      del.className = 'card-del';
      del.title = I18N.t('list.delete_title');
      del.setAttribute('aria-label', I18N.t('list.delete_aria'));
      del.innerHTML = '<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' +
        '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/></svg>';
      del.onclick = function (e) { e.stopPropagation(); deleteDoc(d.id, d.title || d.id); };
      card.appendChild(del);
```

替换为：

```js
      // 「⋯」菜单：归档/恢复 + 删除。默认隐藏，悬停卡片或键盘聚焦时出现。
      var wrap = document.createElement('div');
      wrap.className = 'card-actions dropdown';
      wrap.addEventListener('click', function (e) { e.stopPropagation(); });

      var more = document.createElement('button');
      more.type = 'button';
      more.className = 'card-more';
      more.title = I18N.t('action.more_aria');
      more.setAttribute('aria-label', I18N.t('action.more_aria'));
      more.setAttribute('aria-haspopup', 'menu');
      more.innerHTML = '<svg viewBox="0 0 24 24" width="15" height="15" fill="currentColor" aria-hidden="true">' +
        '<circle cx="5" cy="12" r="1.7"/><circle cx="12" cy="12" r="1.7"/><circle cx="19" cy="12" r="1.7"/></svg>';
      wrap.appendChild(more);

      var menu = document.createElement('div');
      menu.className = 'dropdown-menu card-menu';
      menu.hidden = true;
      var archived = d.status === 'archived';
      menu.appendChild(menuButton(archived ? 'action.restore' : 'action.archive', function () {
        closeMenus();
        if (archived) { unarchiveDoc(d.id); } else { archiveDoc(d.id); }
      }));
      menu.appendChild(menuButton('action.delete', function () {
        closeMenus();
        deleteDoc(d.id, d.title || d.id);
      }, true));
      wrap.appendChild(menu);

      more.addEventListener('click', function (e) {
        e.stopPropagation();
        var wasOpen = !menu.hidden;
        closeMenus();
        if (!wasOpen) { menu.hidden = false; wrap.classList.add('open'); }
      });
      card.appendChild(wrap);
```

- [ ] **Step 2: 替换 CSS**

`web/style.css` 里把 `.card-del` 相关的整块（注释 + `.card-del` + hover + `@media (hover:none)`）替换为：

```css
/* 卡片操作：⋯ 菜单，默认隐藏，悬停或键盘聚焦时出现 */
.card-actions {
  position: absolute; top: 8px; right: 8px;
  opacity: 0; pointer-events: none;
  transition: opacity .12s ease;
}
.card:hover .card-actions,
.card-actions:focus-within,
.card-actions.open { opacity: 1; pointer-events: auto; }
.card-more {
  width: 26px; height: 26px; padding: 0;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 6px; background: var(--surface); color: var(--muted);
}
.card-more:hover { color: var(--text); border-color: var(--accent); }
.card-menu { min-width: 132px; }
/* 触屏没有 hover：直接常显，否则菜单点不到 */
@media (hover: none) {
  .card-actions { opacity: 1; pointer-events: auto; }
}
```

- [ ] **Step 3: 确认没有残留引用**

Run: `grep -rn 'card-del' web/`
Expected: 无输出。

- [ ] **Step 4: 构建与语法检查**

Run: `go build -o mdbox . && go test ./... && node --check web/app.js`
Expected: build 成功、测试绿、语法通过。

- [ ] **Step 5: 提交**

```bash
git add web/app.js web/style.css
git commit -m "feat(web): 列表卡片改用「⋯」菜单，归档视图可恢复"
```

---

### Task 6: 文档同步（PRD / README / CLAUDE 约束）

**Files:**
- Modify: `docs/PRD.md`（API 表、MCP 工具清单、V2-6 删除描述、手工验收项）
- Modify: `README.md`（MCP server 工具列表）
- Modify: `docs/README-ZH.md`（同）
- Modify: `CODEBUDDY.md`（Sharing 段之前无需改；仅在「Archive is a soft delete」处补一句恢复能力）

**Interfaces:**
- Consumes: Task 1-5 的实际行为与命名
- Produces: 文档与实现一致

- [ ] **Step 1: PRD 的 API 表**

在 `docs/PRD.md` 的 `| POST | /api/docs/{id}/archive | 归档（软删除） |` 下一行插入：

```markdown
| POST | `/api/docs/{id}/unarchive` | 恢复（从归档移回活动区） |
```

（保持该表原有的反引号风格，与相邻行一致。）

- [ ] **Step 2: PRD 的 MCP 工具清单**

把 `docs/PRD.md:244` 那行行尾的 ``archive_doc`` 改为 ``archive_doc` · `unarchive_doc``。

- [ ] **Step 3: PRD 的 V2-6 描述**

把 `| V2-6 | **文档删除** | 列表卡片悬停显示删除按钮，二次确认后**彻底删除**（不可恢复，区别于归档的软删除） |` 改为：

```markdown
| V2-6 | **文档删除** | 列表卡片与文档页统一用「⋯」菜单承载归档 / 删除；删除二次确认后**彻底删除**（不可恢复，区别于归档的软删除），归档可在「已归档文档」视图恢复 | 
```

- [ ] **Step 4: PRD 手工验收项**

在 `| 归档 / 删除：删除后文件从磁盘移除、列表数减一、再访问 404 | ✓ |` 之后插入：

```markdown
| 归档可恢复：归档后从「已归档文档」恢复，回到活动列表且正文未丢 | ✓ |
```

- [ ] **Step 5: 两份 README 的工具列表**

`README.md:22` 与 `docs/README-ZH.md:22` 里，把 `archive_doc` 后面补上 `, unarchive_doc`（英文逗号，与各自句式一致），并把句式改成
「`list_docs`, …, `archive_doc`, `unarchive_doc`」。

- [ ] **Step 6: CODEBUDDY.md**

把 `- **Archive is a soft delete** (file moves to `data/archive/`); `Delete` is the only real removal.` 改为：

```markdown
- **Archive is a soft delete** (file moves to `data/archive/`; `Unarchive` moves it back); `Delete` is the only real removal.
```

- [ ] **Step 7: 提交**

```bash
git add docs/PRD.md README.md docs/README-ZH.md CODEBUDDY.md
git commit -m "docs: 补充 unarchive 接口、unarchive_doc 工具与统一的归档/删除入口"
```

---

### Task 7: 门禁与交付

**Files:** 无代码改动（只跑命令、整理验收清单）

**Interfaces:**
- Consumes: 全部前序任务
- Produces: 可通过的构建门禁 + 给作者的浏览器验收清单

- [ ] **Step 1: 全量门禁**

```bash
gofmt -l .
go vet ./...
go build -o mdbox .
go test ./...
node --check web/app.js
```
Expected: `gofmt` 无输出、`vet` 无输出、build 成功、测试全绿、node 语法通过。

- [ ] **Step 2: 交付给作者的验收清单（复制进回复，不写进仓库）**

1. 活动列表 hover 卡片 → 「⋯」→ 归档 → 清空确认 → 文档从列表消失，「已归档文档」里出现。
2. 「已归档文档」→ 「⋯」→ 恢复 → 回到活动列表，正文完整。
3. 打开活动文档 → 预览栏「⋯」→ 删除 → 确认 → 回列表且文档消失，再访问其 URL 404。
4. 打开已归档文档 → 预览栏「⋯」只有「恢复 / 删除」。
5. 编辑态：没有归档按钮；编辑中不会误归档。
6. 菜单交互：点空白及 `Escape` 都收起；点菜单项不会打开文档；同时只开一个菜单；相邻卡片不遮挡菜单（重点看最后一行卡片的菜单是否被裁掉）。
7. 窄屏（<=720px）：卡片和文档页都看不到「⋯」。
8. 中英切换后三处菜单文案都正确。

- [ ] **Step 3: 不提交二进制**

Run: `git status --short`
Expected: 无 `mdbox` 二进制、无 `data/`、`config.yaml`、`users.yaml` 出现在待提交列表（这些都在 .gitignore 里）。
