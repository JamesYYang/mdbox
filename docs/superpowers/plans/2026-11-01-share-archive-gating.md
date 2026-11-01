# 归档即暂停分享 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 文档处于 `archived` 时，分享链接（匿名读 / 下载 / clone / 可编辑写入）一律失效；恢复后自动复效，并在归档前与分享弹窗里告知作者。

**Architecture:** 只在 `internal/api` 的 `sharedDoc` / `editableDoc` 两个鉴权函数里加归档判定，四条分享路径自动收敛；不改 `share_mode`、不改签名算法，因此恢复即复效。前端只加文案，不动分享弹窗结构。

**Tech Stack:** Go 标准库（net/http）、原生 JS（`web/app.js` + `web/i18n.js`，`//go:embed` 打包）。

## Global Constraints

- 代码注释与用户可见文案用中文；英文文案只出现在 `web/i18n.js` 的 `en` 字典里。
- 前端改动必须 `go build` 才生效。
- 不新增依赖、不改签名算法、不改 `share_mode` 的存储格式。
- 分享失效统一走既有的 `i18n.KeyShareGone`（404），不新增错误文案、不区分「已归档」与「未分享」。
- 每个任务结束必须通过：`go build -o mdbox .`、`go test ./...`；Go 文件用「忽略 CR 的 gofmt」核对：
  ```bash
  gofmt <file> | tr -d '\r' > /tmp/g.txt; tr -d '\r' < <file> > /tmp/o.txt; diff -q /tmp/g.txt /tmp/o.txt
  ```
  （本机工作区是 CRLF，`gofmt -l .` 永远非空，不要为此改行尾。）

---

### Task 1: 后端 gating + 行为测试

**Files:**
- Modify: `internal/store/store.go`（新增导出常量 `StatusArchived`，替换 `Archive` 与 `loadFile` 里的字面量）
- Modify: `internal/api/share.go`（`sharedDoc`、`editableDoc`）
- Test: `internal/api/share_test.go`

**Interfaces:**
- Consumes: `store.Doc.Status`（`"active"` / `"archived"`）、`(*store.Store).Archive`、`(*store.Store).Unarchive`、测试脚手架 `newTestServer(t) (reg *users.Registry, h http.Handler, cfg *config.Config)`
- Produces: `const store.StatusArchived = "archived"` —— Task 2 不使用它，但后续任何判断归档状态的地方都该用它

- [ ] **Step 1: 写失败测试**

在 `internal/api/share_test.go` 末尾追加（该文件已 import `net/http`、`net/http/httptest`、`strings`、`encoding/json`、`testing`、`mdbox/internal/store`）：

```go
// TestShareGoneWhenArchived 归档是软删除，但分享入口必须一起关掉：
// 否则已收起的文档仍能被匿名读到，edit 链接甚至能写进归档区。
func TestShareGoneWhenArchived(t *testing.T) {
	reg, h, cfg := newTestServer(t)
	alice, err := reg.Register("alice", "password123")
	if err != nil {
		t.Fatalf("注册 alice 失败: %v", err)
	}
	st, err := reg.Store("alice")
	if err != nil {
		t.Fatalf("取 store 失败: %v", err)
	}
	d, err := st.Create(store.CreateInput{Title: "文档", Content: "# 正文"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := st.SetShareMode(d.ID, store.ShareEdit); err != nil {
		t.Fatalf("设置可编辑失败: %v", err)
	}
	sig := (&Server{cfg: cfg}).shareSig("alice", d.ID)
	bob, err := reg.Register("bob", "password123")
	if err != nil {
		t.Fatalf("注册 bob 失败: %v", err)
	}

	base := "/api/share/alice/" + d.ID + "/" + sig
	get := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}

	if code := get(base); code != http.StatusOK {
		t.Fatalf("归档前读取 = %d, want 200", code)
	}

	if _, err := st.Archive(d.ID); err != nil {
		t.Fatalf("归档失败: %v", err)
	}

	if code := get(base); code != http.StatusNotFound {
		t.Errorf("归档后读取 = %d, want 404", code)
	}
	if code := get(base + "/download"); code != http.StatusNotFound {
		t.Errorf("归档后下载 = %d, want 404", code)
	}

	cl := httptest.NewRequest(http.MethodPost, "/api/clone/alice/"+d.ID+"/"+sig, strings.NewReader(""))
	cl.Header.Set("Authorization", "Bearer "+bob.Token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, cl)
	if rec.Code != http.StatusNotFound {
		t.Errorf("归档后 clone = %d, want 404", rec.Code)
	}

	if code := putShare(t, h, bob.Token, d.ID, sig, "偷偷改").Code; code != http.StatusNotFound {
		t.Errorf("归档后可编辑写入 = %d, want 404", code)
	}

	if _, err := st.Unarchive(d.ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if code := get(base); code != http.StatusOK {
		t.Errorf("恢复后读取 = %d, want 200", code)
	}
	got, err := st.Get(d.ID)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if strings.Contains(got.Content, "偷偷改") {
		t.Errorf("归档期间的写入居然生效了: %q", got.Content)
	}
	if got.ShareMode != store.ShareEdit {
		t.Errorf("share_mode = %q, want edit（归档不该改动分享模式）", got.ShareMode)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/api -run TestShareGoneWhenArchived -v`
Expected: FAIL —— 归档后读取仍是 200（`归档后读取 = 200, want 404`），后续几条同样报 200。

- [ ] **Step 3: store 加状态常量**

`internal/store/store.go` 的 `ShareOff/ShareView/ShareEdit` 常量组旁新增：

```go
// StatusArchived 是归档状态的取值；活动文档恒为 "active"。
const StatusArchived = "archived"
```

把 `Archive`（`e.Status = "archived"`）与 `loadFile`（`status = "archived"` 那一处）的字面量替换为 `StatusArchived`。只替换这两处，不改其它字符串。

- [ ] **Step 4: api 加判定**

`internal/api/share.go`：

`sharedDoc`：

```go
	d, err := st.Get(id)
	if err != nil || !d.Shared || d.Status == store.StatusArchived {
		return nil, false
	}
```

`editableDoc`（先读现有实现，在它取到文档、判断 `share_mode` 的地方并列补一个条件），形如：

```go
	if d.Status == store.StatusArchived || d.ShareMode != store.ShareEdit {
		return nil, nil, false
	}
```

保持它原有的返回值形状（`(*store.Doc, *store.Store, bool)`）与注释风格；注释补一句「归档期间分享视为失效，恢复后自动复效」。

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/api -v -run 'Share|Clone' && go test ./...`
Expected: 全绿；原有的 `TestShareModeThreeStates`、`TestEditShared*`、clone 测试不受影响。

- [ ] **Step 6: 提交**

```bash
gofmt internal/store/store.go | tr -d '\r' > /tmp/g.txt; tr -d '\r' < internal/store/store.go > /tmp/o.txt; diff -q /tmp/g.txt /tmp/o.txt
git add internal/store/store.go internal/api/share.go internal/api/share_test.go
git commit -m "feat(api): 归档期间分享链接失效，恢复后自动复效"
```

---

### Task 2: 前端两处告知

**Files:**
- Modify: `web/i18n.js`（`doc.archive_confirm` 改写 + 新增 `share.archived_hint`）
- Modify: `web/app.js`（`updateShareModeHint`）

**Interfaces:**
- Consumes: `state.current.status`（`GET /api/docs/{id}` 返回的 doc 带 `status`）、`I18N.t`、`SHARE_HINTS`
- Produces: 无新函数

- [ ] **Step 1: 改文案**

`web/i18n.js` 中文：

```js
      'doc.archive_confirm': '归档后可在左侧「归档」中找回；分享链接会暂时失效，恢复后自动恢复。确定归档？',
```

英文：

```js
      'doc.archive_confirm': 'Archived documents can be found later under Archive. Any share link stops working until you restore it. Archive this one?',
```

新增 key（zh 与 en 各一条，放进 `share.` 分组里，例如 `'share.edit_hint'` 附近）：

```js
      'share.archived_hint': '文档已归档，链接暂时不可访问；恢复后自动恢复。',
      'share.archived_hint': 'This document is archived, so the link is unavailable until you restore it.',
```

- [ ] **Step 2: 分享弹窗提示**

`web/app.js` 的 `updateShareModeHint()` 改为：

```js
  function updateShareModeHint() {
    var m = $('share-mode').value;
    var text = I18N.t(SHARE_HINTS[m] || SHARE_HINTS.off);
    // 归档期间链接是 404，但模式仍可改（恢复后按当前模式生效），所以只提示不阻拦。
    if (m !== 'off' && state.current && state.current.status === 'archived') {
      text += ' ' + I18N.t('share.archived_hint');
    }
    $('share-mode-hint').textContent = text;
  }
```

- [ ] **Step 3: 校验**

```bash
node --check web/app.js && node --check web/i18n.js
go build -o mdbox . && go test ./...
grep -c "share.archived_hint" web/i18n.js   # 期望 2
```

- [ ] **Step 4: 提交**

```bash
git add web/i18n.js web/app.js
git commit -m "feat(web): 归档前与分享弹窗告知链接会暂时失效"
```

---

### Task 3: 文档同步与门禁

**Files:**
- Modify: `docs/PRD.md`（分享规则、已知取舍）
- Modify: `CODEBUDDY.md`（Sharing 段）

**Interfaces:**
- Consumes: Task 1/2 的实际行为
- Produces: 无

- [ ] **Step 1: PRD 分享规则**

在 `docs/PRD.md` 讲分享的三态或分享链接的那一小节里补一句（找到描述 `/s/<user>/<id>/<token>` 或 share_mode 的段落，紧跟其后）：

```markdown
归档会暂时让分享链接失效（统一 404，与「关闭分享」对访客无区别）；恢复文档后同一链接自动复效，分享模式保持归档前的取值。
```

- [ ] **Step 2: PRD 已知代价**

在 `docs/PRD.md` 的风险/取舍表里补一行：

```markdown
| 归档会静默中断已发出去的外部链接 | 归档语义是「这篇内容不该再被主动消费」；确认框已告知，恢复即复效 |
```

（若该表结构不同，插成相邻格式一致的表格行。）

- [ ] **Step 3: CODEBUDDY.md**

在 `### Sharing` 段落里 `sig = HMAC-SHA256(...)` 那句之后补一句：

```markdown
An archived document's links are dead: `sharedDoc` / `editableDoc` reject `Status == store.StatusArchived`, so read/download/clone/edit all 404 while archived and come back automatically after `Unarchive` (the `share` field is untouched).
```

- [ ] **Step 4: 门禁**

```bash
go build -o mdbox .
go test ./...
go vet ./...
node --check web/app.js && node --check web/i18n.js
```

- [ ] **Step 5: 提交**

```bash
git add docs/PRD.md CODEBUDDY.md
git commit -m "docs: 说明归档期间分享链接失效与恢复后复效"
```

---

### Task 4: 交付

**Files:** 无

- [ ] **Step 1: 门禁复查**

```bash
go build -o mdbox . && go test ./... && go vet ./... && git status --short
```
Expected: 全绿；工作区无未提交改动（`mdbox` 二进制在 .gitignore 里）。

- [ ] **Step 2: 交给作者的浏览器验收清单**

1. 分享一篇文档为「只读」→ 归档 → 打开分享链接 = 404；下载链接 404
2. 分享为「可编辑」→ 用另一个账号在分享页改并保存 → 归档 → 再尝试保存 = 提示失效/404
3. 打开已归档文档的分享弹窗 → 提示语出现「文档已归档，链接暂时不可访问」
4. 恢复文档 → 同一个旧链接直接可读，分享模式仍是原来那个
5. 中英切换后归档确认框与分享提示文案都正确
