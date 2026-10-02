# 移动端阅读体验优化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 mdbox 在手机浏览器上达到正常可读水平，同时保证桌面端表现零变化。

**Architecture:** 不改后端、不改桌面样式。在 `web/style.css` 里整体重写现有的 `@media (max-width: 720px)` 块（并允许新增少量基础规则），把侧栏在窄屏改为可唤出的左侧抽屉（新增 `#side-toggle` / `#side-backdrop` / `.side-foot` 三个元素 + `body.nav-open` 状态类），在 `web/app.js` 里补约 20 行抽屉逻辑。编辑/新建/上传/删除入口在窄屏用纯 CSS 隐藏。

**Tech Stack:** 原生 HTML/CSS/JS（零依赖，无构建链、无前端测试框架）。`web/` 由 `main.go` 的 `//go:embed web` 嵌入单二进制。

**设计依据：** `docs/superpowers/specs/2026-10-02-mobile-reading-design.md`

## Global Constraints

- 断点固定为 **720px**，与现有媒体查询一致。
- **桌面端（>720px）表现必须零变化**：不改任何现有基础规则的选择器语义，只做「追加」。
- **不引入任何 CSS 框架、JS 库或 CDN 资源**（目标网络环境在国内）。
- 沿用项目约定：**代码注释与用户可见文案一律用中文**。
- `web/` 由 `//go:embed` 嵌入，**任何前端改动都必须 `go build` 后才生效**。
- **不修改 `main.go` 和 `internal/` 下的任何文件**，不改任何 REST / MCP 接口。
- 本仓库**没有前端自动化测试框架**（`CODEBUDDY.md`：自动化测试仅覆盖 `internal/users`）。因此每个任务的验证 = `go build` / `go test ./...` / `gofmt -l .` 通过 + 浏览器定点检查。**不要试图为 CSS/JS 发明测试框架。**

---

## 验证环境准备（每个任务复用）

所有浏览器检查都通过本地跑起来的实例进行。每个任务开始前确保它已在运行：

```bash
cd d:/code/mdbox
go build -o mdbox.exe . && ./mdbox.exe -addr 127.0.0.1:8080 -data ./data
```

然后浏览器打开 `http://127.0.0.1:8080`，用 DevTools（F12）的 **Toggle device toolbar** 切到：
- **375 × 667**（iPhone SE / 8）
- **390 × 844**（iPhone 14）

桌面回归检查用 **1440 × 900**。首次使用需在登录页注册一个账号。

---

## 文件结构

| 文件 | 职责 | 本次改动 |
| --- | --- | --- |
| `web/index.html` | 页面骨架 | 新增 `#side-toggle`、`#side-backdrop`、`.side-foot`；viewport 补 `viewport-fit=cover` |
| `web/style.css` | 全部样式 | 重写 `@media (max-width: 720px)` 块；新增若干基础规则（dvh 回退、元素默认隐藏） |
| `web/app.js` | 全部交互 | 抽屉开合、`.side-foot` 按钮事件、抽出 `doLogout` / `renderUser` |
| `docs/superpowers/specs/2026-10-02-mobile-reading-design.md` | 设计依据 | 仅修正「matchMedia 复位」一节（见 Task 1 说明），不改实现 |

不改动：`main.go`、`internal/**`、`web/vendor/**`、`web/app.js` 的后端调用逻辑。

---

## Task 1: 顶栏开关 + 左侧抽屉骨架

交付物：手机上顶栏变成 `☰ MDBox … ☾`，点 ☰ 抽屉从左侧滑出并可关闭；桌面端 1440px 下与改动前**完全一致**。

**Files:**
- Modify: `web/index.html`（`#app` 的 `.topbar`、`.shell` 与 `#app` 之间、`.sidebar` 末尾）
- Modify: `web/style.css:279-286`（替换整个窄屏媒体查询块）
- Modify: `web/app.js`（新增抽屉函数、事件绑定、`resetSession`）
- Test: 手动浏览器验证（见步骤）

**Interfaces:**
- Produces:
  - CSS 状态类：`body.nav-open`（存在 = 抽屉打开）
  - DOM id：`#side-toggle`（开关按钮）、`#side-backdrop`（遮罩）
  - JS 函数：`openNav(): void`、`closeNav(): void`（后续 Task 2 复用）

- [ ] **Step 1: 在顶栏最左侧插入抽屉开关按钮**

在 `web/index.html` 中，把 `#app` 里 `.topbar` 的第一行改成下面这样（在原 `<div class="brand">` 之前加按钮）：

```html
  <header class="topbar">
    <button id="side-toggle" class="icon-btn" title="目录" aria-label="打开目录">
      <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
        <path d="M3 6h18"/><path d="M3 12h18"/><path d="M3 18h18"/>
      </svg>
    </button>
    <div class="brand"><img class="logo" src="/mdbox.png" alt="MDBox">MDBox</div>
```

- [ ] **Step 2: 在 `.shell` 之后插入遮罩元素**

`web/index.html` 中 `.shell` 的收尾是 `</aside>` → `</main>` → `</div>`（关 `.shell`）→ `</div>`（关 `#app`）。在两者之间插入遮罩，改成：

```html
    </main>
  </div>
  <div id="side-backdrop" class="side-backdrop"></div>
</div>
```

> 注意：遮罩**不要**加 `hidden` 属性，它的显隐由 CSS 的 `opacity` 控制（加 `hidden` 会被 `[hidden] { display: none !important }` 永久盖住）。

- [ ] **Step 3: 替换窄屏媒体查询块为抽屉实现**

把 `web/style.css` 末尾现有的整块（当前 279-286 行）替换掉：

```css
/* ---------- 窄屏 / 移动端 ---------- */
/* 这三个元素只在窄屏出现，桌面端一律隐藏 */
.side-toggle, .side-backdrop, .side-foot { display: none; }

@media (max-width: 720px) {
  /* —— 顶栏：☰ MDBox … ☾ —— */
  .topbar { height: 54px; padding: 0 12px; gap: 6px; }
  .side-toggle { display: inline-flex; }
  .user-name, #pwd-btn, #logout-btn, #mcp-btn { display: none; }

  /* —— 侧栏 → 左侧抽屉 —— */
  .sidebar {
    position: fixed; z-index: 40;
    left: 0; top: 54px; bottom: 0;
    width: min(78vw, 300px);
    padding: 12px 8px 0;
    transform: translateX(-100%);
    transition: transform .22s ease;
    box-shadow: 0 0 24px rgba(0, 0, 0, .18);
  }
  body.nav-open .sidebar { transform: translateX(0); }
  /* 锁滚动。规则写在媒体查询内，桌面端天然不受影响 */
  body.nav-open { overflow: hidden; }

  .side-backdrop {
    display: block; position: fixed; inset: 54px 0 0 0; z-index: 39;
    background: rgba(0, 0, 0, .4);
    opacity: 0; pointer-events: none;
    transition: opacity .22s ease;
  }
  body.nav-open .side-backdrop { opacity: 1; pointer-events: auto; }

  /* —— 编辑相关：编辑不可达，以下规则仅为「万一面板被恢复」兜底 —— */
  .panes { flex-direction: column; }
  .source { border-right: none; border-bottom: 1px solid var(--border); min-height: 40vh; }
  .preview { min-height: 40vh; }
  .meta-input { width: 110px; }
}
```

- [ ] **Step 4: 在 `web/app.js` 里加入抽屉函数并绑定事件**

在 `/* ---------- 侧边栏 ---------- */` 小节（`function loadSidebar()` 之前）插入：

```js
  /* ---------- 移动端侧栏抽屉 ---------- */
  // 桌面端没有开关按钮，这两个函数只会被窄屏 UI 调用。
  // 相关 CSS 全部写在 @media (max-width:720px) 内，所以不需要在窗口变宽时额外复位。
  function openNav() { document.body.classList.add('nav-open'); }
  function closeNav() { document.body.classList.remove('nav-open'); }
```

在 `bindEvents()` 里，把这行：

```js
    $('theme-btn').addEventListener('click', toggleTheme);
```

改成：

```js
    $('side-toggle').addEventListener('click', function () {
      if (document.body.classList.contains('nav-open')) { closeNav(); } else { openNav(); }
    });
    $('side-backdrop').addEventListener('click', closeNav);
    $('theme-btn').addEventListener('click', toggleTheme);
```

- [ ] **Step 5: 让抽屉在「选中分类/标签」和「切换账号」后自动收起**

在 `onSideClick` 函数末尾（`refresh();` 那一行之前）加 `closeNav();`：

```js
  function onSideClick(e) {
    var li = e.target.closest ? e.target.closest('.side-item') : null;
    if (!li) { return; }
    var kind = li.getAttribute('data-kind');
    var v = li.getAttribute('data-value');
    if (kind === 'all') { state.category = ''; state.tag = ''; state.status = 'active'; }
    else if (kind === 'category') { state.category = (state.category === v ? '' : v); state.tag = ''; state.status = 'active'; }
    else if (kind === 'tag') { state.tag = (state.tag === v ? '' : v); state.category = ''; state.status = 'active'; }
    closeNav();
    refresh();
  }
```

在 `bindEvents()` 里给「已归档文档」也加上收起：

```js
    $('side-archived').addEventListener('click', function () {
      state.status = 'archived'; state.category = ''; state.tag = '';
      closeNav();
      refresh();
    });
```

在 `resetSession()` 里加一行（防止退出登录后抽屉保持打开状态带进下一个账号）：

```js
    state.pushed = false;
    closeNav();
```

- [ ] **Step 6: 给 Esc 键加关闭抽屉的处理**

在 `bindEvents()` 的 `keydown` 监听里，找到 Escape 分支，改成：

```js
      if (e.key === 'Escape') {
        if (!$('modal-dialog').hidden) { closeDialog(false); return; }
        var modalIds = ['modal-mcp', 'modal-share'];
        var openModalId = modalIds.filter(function (id) { return !$(id).hidden; })[0];
        if (openModalId) { $(openModalId).hidden = true; return; }
        closeNav();
        return;
      }
```

> 顺序是刻意的：弹窗 z-index 50 高于抽屉 40，Esc 必须先关弹窗再关抽屉。

- [ ] **Step 7: 编译并跑既有检查**

```bash
cd d:/code/mdbox
gofmt -l .
go build -o mdbox.exe .
go test ./...
```

Expected：`gofmt -l .` 无输出；`go build` 无输出（成功）；`go test ./...` 全部 `ok` 或 `no test files`。

> `gofmt -l .` 期望无输出。若报 `web/` 相关错误说明路径写错了，本次未改动任何 `.go` 文件。

- [ ] **Step 8: 浏览器验证 —— 窄屏**

重新启动 `./mdbox.exe -addr 127.0.0.1:8080 -data ./data`，DevTools 切到 **375 × 667**，然后：

1. 顶栏显示为 `☰ MDBox` + 右侧主题按钮，**无横向滚动条**（页面底部不应出现左右滚动）。
2. 点 ☰ → 侧栏从左侧滑入，正文被半透明遮罩盖住。
3. 点遮罩 → 抽屉收起。
4. 点 ☰ 打开后按 Esc → 抽屉收起。
5. 点 ☰ 打开后点「全部文档」或任一分类 → 抽屉收起且列表已刷新。
6. 抽屉打开时，在抽屉区域内上下滑动，背景列表**不应跟着滚动**。

- [ ] **Step 9: 浏览器验证 —— 桌面回归（关键）**

DevTools 切到 **1440 × 900**：

1. 顶栏**看不到 ☰ 按钮**，与改动前一致。
2. 侧栏是常驻的 232px 左栏，不是浮层；无遮罩。
3. 检查 `<body>` 不含 `nav-open` 类（Elements 面板确认）。若含有，说明 `resetSession` 的清理或事件绑定有问题。
4. 页面整体与原版本逐像素对比：`git stash` 前后各截一张 1440px 全页截图对照，**应无任何差异**。

- [ ] **Step 10: 修正 spec 里已过时的一节**

本任务实现后，`docs/superpowers/specs/2026-10-02-mobile-reading-design.md` 第 3 节「侧栏抽屉行为」的最后一条「**复位**：监听 `matchMedia('(max-width:720px)')` 的 `change` 事件…」已不成立 —— 因为 `body.nav-open` 的锁滚动规则写在媒体查询**内部**，窗口变宽时天然失效，不需要 JS 复位。

把该条替换为：

```markdown
- **复位**：不需要。`body.nav-open` 与 `.sidebar` 的相关规则全部写在 `@media (max-width: 720px)` 内部，窗口变宽时自动失效，桌面端不会被锁滚动。仅在 `resetSession()`（退出/切换账号）时移除 `nav-open`。
```

- [ ] **Step 11: 提交**

```bash
cd d:/code/mdbox
git add web/index.html web/style.css web/app.js docs/superpowers/specs/2026-10-02-mobile-reading-design.md
git commit -m "feat(web): 窄屏顶栏加入抽屉开关，侧栏改为可唤出的左侧抽屉"
```

---

## Task 2: 抽屉底部的账号操作区

交付物：手机上抽屉底部可见用户名 + `MCP 接入` / `修改密码` / `退出登录` 三个按钮并可用；桌面端无任何变化。

**Files:**
- Modify: `web/index.html`（`.sidebar` 末尾新增 `.side-foot`）
- Modify: `web/style.css`（窄屏媒体查询块内追加 `.side-foot` 规则；基础区追加默认隐藏）
- Modify: `web/app.js`（`doLogout` / `renderUser` 抽取、新按钮绑定）

**Interfaces:**
- Consumes: Task 1 的 `closeNav()`
- Produces:
  - DOM id：`#side-mcp`、`#side-pwd`、`#side-logout`、`#side-user-name`、`#side-avatar`
  - JS 函数：`doLogout(): void`、`renderUser(user: string): void`

- [ ] **Step 1: 在侧栏末尾插入 `.side-foot`**

`web/index.html` 中 `.sidebar` 的末尾是「归档」的 `.side-section` 然后 `</aside>`。在 `</aside>` 之前插入：

```html
      <div class="side-foot">
        <div class="side-foot-user">
          <span class="avatar" id="side-avatar">A</span>
          <span id="side-user-name">admin</span>
        </div>
        <button type="button" class="ghost side-foot-btn" id="side-mcp">MCP 接入</button>
        <button type="button" class="ghost side-foot-btn" id="side-pwd">修改密码</button>
        <button type="button" class="ghost side-foot-btn" id="side-logout">退出登录</button>
      </div>
    </aside>
```

- [ ] **Step 2: 加 `.side-foot` 样式**

在 `web/style.css` 窄屏媒体查询块**内部**的最后一行 `}` 之前追加：

```css
  /* —— 抽屉底部账号区（桌面端隐藏）—— */
  .side-foot {
    position: sticky; bottom: 0;
    margin: 12px -8px 0;
    padding: 8px 8px 8px;
    background: var(--surface);
    border-top: 1px solid var(--border);
  }
  .side-foot-user {
    display: flex; align-items: center; gap: 8px;
    padding: 4px 8px 10px; font-size: 14px; color: var(--text);
  }
  .side-foot-btn { display: block; width: 100%; text-align: left; margin-bottom: 6px; }
```

> `.side-foot` 用 `position: sticky; bottom: 0` 固定在抽屉底部，这样分类/标签再长也不会把账号区挤出屏幕；负 `margin` 让上边框铺满抽屉宽度。

- [ ] **Step 3: 抽出 `doLogout` 与 `renderUser`**

`web/app.js` 中，把 `enterApp` 里的用户渲染替换为一个具名函数。原代码：

```js
  function enterApp(user) {
    state.user = user;
    $('user-name').textContent = user;
    $('user-avatar').textContent = (user || 'A').charAt(0);
    showView('app');
```

改为：

```js
  // renderUser 同时刷新顶栏与抽屉底部的用户名/头像
  function renderUser(user) {
    var initial = (user || 'A').charAt(0);
    $('user-name').textContent = user;
    $('user-avatar').textContent = initial;
    $('side-user-name').textContent = user;
    $('side-avatar').textContent = initial;
  }

  function enterApp(user) {
    state.user = user;
    renderUser(user);
    showView('app');
```

在 `bindEvents()` 之前新增登出函数（与 `openPwd` 放在同一个小节，即 `/* ---------- 上传 ---------- */` 之前）：

```js
  function doLogout() {
    api('/api/logout', { method: 'POST', noAuthRedirect: true }).catch(function () {}).then(showLogin);
  }
```

- [ ] **Step 4: 绑定两组按钮**

把 `bindEvents()` 里这三行：

```js
    $('mcp-btn').addEventListener('click', openMcp);
    $('pwd-btn').addEventListener('click', openPwd);
    $('logout-btn').addEventListener('click', function () {
      api('/api/logout', { method: 'POST', noAuthRedirect: true }).catch(function () {}).then(showLogin);
    });
```

替换为：

```js
    $('mcp-btn').addEventListener('click', openMcp);
    $('pwd-btn').addEventListener('click', openPwd);
    $('logout-btn').addEventListener('click', doLogout);
    // 窄屏：同样的操作放在抽屉底部，先收起抽屉再动作，避免弹窗盖在打开的抽屉上
    $('side-mcp').addEventListener('click', function () { closeNav(); openMcp(); });
    $('side-pwd').addEventListener('click', function () { closeNav(); openPwd(); });
    $('side-logout').addEventListener('click', function () { closeNav(); doLogout(); });
```

- [ ] **Step 5: 编译并跑既有检查**

```bash
cd d:/code/mdbox
gofmt -l .
go build -o mdbox.exe .
go test ./...
```

Expected：同 Task 1 Step 7。

- [ ] **Step 6: 浏览器验证 —— 窄屏（375 × 667）**

1. 点 ☰ 打开抽屉，底部能看到头像 + 用户名 + 三个按钮，且三个按钮**不被下方截断**（抽屉内滚到底再看一次）。
2. 点 `修改密码` → 抽屉收起，密码弹窗出现，且弹窗**盖在**抽屉之上。
3. 关掉密码弹窗，再点 `MCP 接入` → 抽屉收起，MCP 弹窗出现，里面的 JSON 代码块可横向滚动。
4. 点 `退出登录` → 回到登录页。
5. **重新登录**后，抽屉应是**关闭**状态（`resetSession` 清了 `nav-open`）。
6. 登录页用另一个账号登录，抽屉底部的用户名/头像应更新为新账号。

- [ ] **Step 7: 浏览器验证 —— 桌面回归（1440 × 900）**

1. `.side-foot` **完全不可见**，侧栏底部与改动前一致。
2. 顶栏的 `修改密码` / `退出` / `MCP` 按钮仍正常工作。
3. 1440px 全页截图与 Task 1 后对比无差异。

- [ ] **Step 8: 提交**

```bash
cd d:/code/mdbox
git add web/index.html web/style.css web/app.js
git commit -m "feat(web): 窄屏把账号操作移入抽屉底部（修改密码/退出/MCP）"
```

---

## Task 3: 窄屏隐藏编辑入口 + 工具条改单行

交付物：手机上列表页只剩搜索框，文档页工具条单行不溢出，卡片上无删除按钮；桌面端无变化。

**Files:**
- Modify: `web/style.css`（窄屏媒体查询块内追加）

**Interfaces:**
- Consumes: 无
- Produces: 无（纯样式）

- [ ] **Step 1: 追加隐藏规则与工具条规则**

在 `web/style.css` 窄屏媒体查询块**内部**的最后一行 `}` 之前追加：

```css
  /* —— 窄屏只读：隐藏一切编辑/新建/上传/删除入口 —— */
  /* 注意 .card-del 在基础区有一条 @media (hover:none) 规则把它设为常显，
     那条只改 opacity/pointer-events，不会覆盖这里的 display:none。 */
  #btn-new, #btn-upload, #p-edit, #e-archive, .card-del { display: none; }

  /* —— 列表页 —— */
  #view-list { padding: 12px 14px; }
  .listbar { gap: 8px; }
  .listbar .search { max-width: none; }
  /* 基础规则是 repeat(auto-fill, minmax(280px,1fr))，在 600-720px 区间会出两列；
     手机上锁定单列，避免卡片被压得过窄。 */
  .list { grid-template-columns: 1fr; }

  /* —— 文档工具条：预览条压成单行，标题占满剩余宽度并截断 —— */
  #preview-bar { flex-wrap: nowrap; padding: 8px 12px; gap: 6px; }
  #preview-bar .spacer { display: none; }
  .doc-title { flex: 1; min-width: 0; max-width: none; font-size: 14px; }
  .docbar .msg { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  /* 编辑条在窄屏不可达，保留换行兜底 */
  #edit-bar { flex-wrap: wrap; }
```

- [ ] **Step 2: 编译并跑既有检查**

```bash
cd d:/code/mdbox
gofmt -l .
go build -o mdbox.exe .
go test ./...
```

Expected：同 Task 1 Step 7。

- [ ] **Step 3: 浏览器验证 —— 窄屏（375 × 667）**

1. 列表页：搜索框**独占一行且铺满宽度**；看不到 `新建`、`上传 .md` 按钮。
2. 列表页：卡片**右上角没有垃圾桶图标**；点卡片仍能正常打开文档。
3. 文档页工具条：**只有一行**，从左到右是 `返回` + 标题 + `分享` + `下载 ▾`；看不到 `编辑` 按钮。
4. 打开一篇标题很长的文档，标题应显示为省略号截断，工具条**高度不变、不换行**。
5. 点 `下载 ▾` → 下拉菜单弹出且完整可见（含 `导出 PDF`）。
6. 点 `分享` → 分享弹窗出现。
7. 以上操作中页面**始终没有横向滚动条**。

- [ ] **Step 4: 浏览器验证 —— 桌面回归（1440 × 900）**

1. 列表页有 `新建` / `上传 .md`，卡片悬停时右上角出现删除按钮。
2. 文档工具条换行行为、`编辑` 按钮、标题宽度均与改动前一致。
3. 1440px 全页截图与此前对比无差异。

- [ ] **Step 5: 提交**

```bash
cd d:/code/mdbox
git add web/style.css
git commit -m "feat(web): 窄屏隐藏编辑/新建/上传/删除入口，文档工具条改单行"
```

---

## Task 4: 正文阅读排版

交付物：手机上正文 16px/1.75、左右留白收窄、宽表格改为容器内横向滚动（不再撑破页面）。

**Files:**
- Modify: `web/style.css`（窄屏媒体查询块内追加）

- [ ] **Step 1: 追加排版规则**

在 `web/style.css` 窄屏媒体查询块**内部**的最后一行 `}` 之前追加：

```css
  /* —— 正文阅读排版 —— */
  /* 注意：这里的选择器是「阅读容器」而不是裸 .markdown。
     原因是 .pdf-export 的包裹层也带 markdown 类（见 web/style.css 末尾），
     若写成 .markdown 会连带影响手机端导出 PDF —— 尤其 table 会被加上
     display:block + overflow-x:auto，html2canvas 会按容器边缘裁切，正好
     重新引入 web/CODEBUDDY.md 里明确警告过的「右侧被静默裁掉」问题。 */
  .pane-preview .markdown, .preview, .share-inner .markdown { font-size: 16px; line-height: 1.75; }
  .pane-preview { padding: 16px 14px; }
  .preview { padding: 14px; }

  .pane-preview pre, .preview pre, .share-inner pre { padding: 12px; border-radius: 8px; }
  .pane-preview blockquote, .preview blockquote, .share-inner blockquote { padding: 2px 10px; }

  /* 宽表格在正文容器内横向滚动，不再把页面撑宽。
     多列时靠内在最小宽度触发滚动；少列长文本仍照常换行。 */
  .pane-preview table, .preview table, .share-inner table {
    display: block; overflow-x: auto; max-width: 100%;
  }
  .pane-preview th, .pane-preview td,
  .preview th, .preview td,
  .share-inner th, .share-inner td { padding: 6px 8px; }
```

> `table` 用 `display: block` 会让表格宽度按内容收缩、不再撑满容器。这是刻意的取舍：撑满需要 JS 包一层 `<div>`（约 6 行），收益不足以抵消复杂度。
>
> 选择器刻意不写裸 `.markdown`：必须避开 `.pdf-export`（它同时带 `pdf-export` 和 `markdown` 两个类）。详见上面注释。

- [ ] **Step 2: 编译并跑既有检查**

```bash
cd d:/code/mdbox
gofmt -l .
go build -o mdbox.exe .
go test ./...
```

Expected：同 Task 1 Step 7。

- [ ] **Step 3: 准备一篇测试文档**

在桌面端浏览器（1440px）登录，新建一篇文档并把下面内容粘进源码框保存（新建入口在手机上被隐藏，必须用桌面宽度操作）：

````markdown
# 移动端排版测试

## 二级标题

这是一个用于验证手机端正文排版的段落。需要在窄屏下检查字号是否足够大、左右留白是否合理、行高是否舒适，以及整段文字是否会自动换行而不是产生横向滚动。

### 三级标题

- 列表项一，包含一段足够长的文字用来观察换行行为，例如这串内容一直写下去看看会不会溢出容器边界导致页面被撑宽。
- 列表项二

> 引用块测试，同样写一段足够长的文字来观察左侧竖线和左内边距在窄屏下是否合适。

行内代码 `some_function_name_that_is_quite_long()` 与一个很长的链接 <https://example.com/a/very/long/path/that/should/not/break/the/mobile/layout/at/all/ok>。

```go
func aVeryLongFunctionNameThatShouldScrollHorizontally(param1 string, param2 string) (string, error) {
	return "这是一个很长的代码行，用来验证代码块在手机上是横向滚动而不是撑破页面", nil
}
```

| 列一 | 列二 | 列三 | 列四 | 列五 | 列六 |
| --- | --- | --- | --- | --- | --- |
| 甲 | 乙 | 丙 | 丁 | 戊 | 己 |
| 较长的单元格内容一 | 较长的单元格内容二 | 较长的单元格内容三 | 较长的单元格内容四 | 较长的单元格内容五 | 较长的单元格内容六 |

![测试图片](https://example.com/not-found.png)
````

- [ ] **Step 4: 浏览器验证 —— 窄屏（375 × 667）**

打开上面这篇文档：

1. 正文字号明显大于 UI 文字（16px），行高舒朗。
2. 正文左右留白约 14px，不再有明显的大块空白。
3. **整个页面没有横向滚动条**（关键）。用 DevTools 的 Elements 面板选中 `<body>`，确认 `scrollWidth <= clientWidth`。
4. **宽表格**（6 列那张）：在表格区域内左右拖动可以横向滚动，表格本身不越出正文容器边界。
5. **代码块**：横向可滚动，不撑破页面。
6. 三级/二级标题层级清晰，引用块有左侧竖线。
7. 长链接/长行内代码没有把页面撑宽（`.markdown` 已带 `overflow-wrap: break-word`）。

- [ ] **Step 5: 验证 PDF 导出未被波及（关键）**

`.pdf-export` 包裹层同时带 `pdf-export` 和 `markdown` 两个类，是本任务最容易误伤的地方。在 **375 × 667** 下打开那篇测试文档 → 点 `下载 ▾` → `导出 PDF`：

1. 生成的 PDF 里，那张 6 列表格**不能出现右侧被裁切**（这正是 `web/CODEBUDDY.md` 警告过的 html2canvas 裁切问题）。
2. PDF 里表格边框正常（说明 `markdown` 类仍生效）。
3. 用 DevTools 选中 `.pdf-export`（导出瞬间它会被临时插入 `document.body`，可在导出前于 Console 里执行一次 `document.body.insertAdjacentHTML('beforeend','<div class="pdf-export markdown"><table><tr><td>a</td></tr></table></div>')` 再检查），确认其内部 `table` 的 computed `display` 是 `table` 而不是 `block`。

> 若第 3 条显示 `display: block`，说明选择器没避开 `.pdf-export`，必须回到 Step 1 修正。

- [ ] **Step 6: 浏览器验证 —— 编辑模式的实时预览（次要）**

窄屏下编辑入口已隐藏，本步骤只在桌面端做：切换 1440px → 打开文档 → 点 `编辑` → 右侧预览区排版不变。确认 Task 4 的 `.preview` 规则确实没有影响桌面。

- [ ] **Step 7: 提交**

```bash
cd d:/code/mdbox
git add web/style.css
git commit -m "feat(web): 窄屏正文 16px/1.75、收紧留白、宽表格改为容器内横滚"
```

---

## Task 5: 触控尺寸、视口高度与安全区

交付物：手机上所有可点元素 ≥44px，输入框 ≥16px（消除 iOS 聚焦自动放大），`100dvh` 修掉 iOS 地址栏遮挡底部的问题。

**Files:**
- Modify: `web/index.html`（viewport meta）
- Modify: `web/style.css`（新增基础规则 + 窄屏媒体查询块内追加）

- [ ] **Step 1: 给 viewport 补 `viewport-fit=cover`**

`web/index.html` 第 5 行：

```html
<meta name="viewport" content="width=device-width, initial-scale=1">
```

改为：

```html
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
```

> `env(safe-area-inset-*)` 只有在有 `viewport-fit=cover` 时才返回非 0 值。

- [ ] **Step 2: 加 dvh 回退（放在基础区，不是媒体查询里）**

`web/style.css` 基础区里，把这三处的 `100vh` 各补一行 `dvh` 回退。

`#app`：

```css
#app { height: 100vh; height: 100dvh; display: flex; flex-direction: column; }
```

`.login-wrap`：

```css
.login-wrap {
  min-height: 100vh; min-height: 100dvh; display: flex; align-items: center; justify-content: center;
  background: var(--bg); padding: 20px;
}
```

`.share-wrap`：

```css
.share-wrap { min-height: 100vh; min-height: 100dvh; display: flex; flex-direction: column; }
```

> `dvh` 在桌面端与 `vh` 等价，支持它的浏览器才用；不支持的浏览器忽略第二行、沿用 `vh`。这是标准的两行回退写法。

- [ ] **Step 3: 加触控尺寸与安全区规则**

在 `web/style.css` 窄屏媒体查询块**内部**的最后一行 `}` 之前追加：

```css
  /* —— 触控尺寸：可点元素至少 44px —— */
  button { min-height: 44px; padding: 8px 14px; }
  .icon-btn { width: 44px; height: 44px; }
  .side-item { min-height: 44px; padding: 10px 12px; }
  .modal-close { min-height: 44px; padding: 0 10px; }

  /* 输入框字号 <16px 时 iOS Safari 会在聚焦时自动放大整页，这里统一提到 16px */
  input[type="search"], input[type="text"], input[type="password"], input:not([type]), textarea {
    font-size: 16px; min-height: 44px;
  }

  /* —— 弹窗与安全区 —— */
  .modal { padding: 14px; }
  .modal-card { max-height: 88vh; max-height: 88dvh; }
  .modal-body { padding: 14px; }
  .detailfoot { padding: 6px 12px calc(6px + env(safe-area-inset-bottom)); }
```

- [ ] **Step 4: 编译并跑既有检查**

```bash
cd d:/code/mdbox
gofmt -l .
go build -o mdbox.exe .
go test ./...
```

Expected：同 Task 1 Step 7。

- [ ] **Step 5: 浏览器验证 —— 窄屏尺寸（375 × 667）**

在 DevTools 里用 Elements 面板逐个选中并确认渲染盒高度（Computed 面板的 `height`）：

1. 顶栏的 ☰ 与主题按钮：均为 44px 高。
2. 抽屉里的「全部文档」和任一分类/标签项：均 ≥44px 高。
3. 搜索框：44px 高。
4. 文档页的 `返回` / `分享` / `下载 ▾`：均 ≥44px 高。
5. 点搜索框聚焦：**页面不应整体放大**（DevTools 不模拟该行为，需在 Task 6 的真机上确认）。
6. 所有弹窗（MCP / 分享 / 密码 / 通用确认框）内容较小时不出现内部滚动条；内容较长时内部可滚动，且**关闭按钮 × 可见可点**。
7. 页面底部内容**不被裁掉**：在文档页滚到底，`detailfoot` 的元信息文字完整可见。

- [ ] **Step 6: 浏览器验证 —— 桌面回归（1440 × 900）**

1. 按钮高度、内边距与改动前一致（基础 `button` 规则未改）。
2. `#app` 高度正常（`dvh` 在桌面等于 `vh`）。
3. 1440px 全页截图与此前对比无差异。

- [ ] **Step 7: 提交**

```bash
cd d:/code/mdbox
git add web/index.html web/style.css
git commit -m "feat(web): 窄屏触控尺寸提到 44px、输入框 16px 防缩放、dvh 与安全区适配"
```

---

## Task 6: 分享页与登录页收尾 + 全量回归

交付物：匿名分享页在手机上同样可读；登录页触控舒适；桌面端完整回归通过；真机确认 `dvh` 与 safe-area。

**Files:**
- Modify: `web/style.css`（窄屏媒体查询块内追加）

- [ ] **Step 1: 追加分享页与登录页规则**

在 `web/style.css` 窄屏媒体查询块**内部**的最后一行 `}` 之前追加：

```css
  /* —— 分享页 —— */
  .share-brand-tip { display: none; }
  .share-inner { padding: 20px 14px calc(20px + env(safe-area-inset-bottom)); }
  .code-block { font-size: 12px; }

  /* —— 登录页 —— */
  .login-wrap { padding: 16px; }
  .login-card { width: 100%; max-width: 340px; padding: 22px 18px; }
```

> `.share-inner` 的窄屏内边距只在这里定义一次（Task 4 未涉及它），带底部安全区。

- [ ] **Step 2: 编译并跑既有检查**

```bash
cd d:/code/mdbox
gofmt -l .
go build -o mdbox.exe .
go test ./...
```

Expected：同 Task 1 Step 7。

- [ ] **Step 3: 取得一个分享链接**

在桌面宽度（1440px）下登录 → 打开任一文档 → 点 `分享` → 勾选开启分享 → 复制 `分享链接`，形如 `http://127.0.0.1:8080/s/{user}/{id}/{sig}`。

- [ ] **Step 4: 浏览器验证 —— 分享页（375 × 667，关键）**

把上一步的链接粘到同一个已切换到 375×667 的标签页打开：

1. 顶栏是 `MDBox` + 主题按钮 + `下载 ▾`，**看不到「只读分享」小字**，无横向滚动。
2. 正文排版与登录后的文档页一致（16px / 1.75 / 左右 14px）。
3. `下载 ▾` 下拉菜单完整可见。
4. 页面**无横向滚动条**；宽表格可在容器内横向滚动。
5. 滚到底部，最后一行内容不被裁掉（有底部安全区内边距）。

- [ ] **Step 5: 浏览器验证 —— 登录页（375 × 667）**

1. 退出登录（在抽屉底部点 `退出登录`）。
2. 登录卡片宽度自适应屏幕，左右各留 16px；不出现横向滚动。
3. 用户名/密码输入框高 44px。
4. 点输入框聚焦**不应触发整页放大**（DevTools 不模拟；若在真机上发现放大，检查 `.login-card input` 的 `font-size` 是否被其它规则覆盖）。
5. 点 `没有账号？注册`，多出的「再次输入密码」输入框正常显示。

- [ ] **Step 6: 浏览器验证 —— 桌面端完整回归（1440 × 900）**

逐项确认与改动前一致：

1. 顶栏：`MDBox` + MCP + 主题 + 头像 + 用户名 + `修改密码` + `退出`，全部正常。
2. 侧栏常驻 232px，没有 `.side-foot`。
3. 列表页网格为多列（`repeat(auto-fill, minmax(280px,1fr))`），卡片悬停出现删除按钮。
4. 文档页：`编辑` 可用，进入编辑后左右两栏并排，实时预览正常，`Ctrl+S` 保存正常。
5. 分享弹窗、MCP 弹窗、修改密码弹窗、通用确认框外观不变。
6. 桌面端与改动前的全页截图**零差异**。

- [ ] **Step 7: 部署到 Azure 并用真机验证**

改完必须重新构建镜像才生效（`web/` 是 `//go:embed` 进去的）。按 `docs/DEPLOY.md` 重新构建并推送到 ACR、更新 Container Apps 后，用 iPhone Safari 打开线上地址：

1. 顶部与底部**不被地址栏/工具栏遮挡**（`dvh` 生效）。
2. 点搜索框聚焦**不放大整页**（16px 生效）。
3. 抽屉在 iPhone 上从左侧滑出流畅，滑动时不带动背景。
4. 底部最后一行内容不被 iPhone 的 Home 指示条压住（safe-area 生效）。
5. 分享链接在手机上打开同样可读。

- [ ] **Step 8: 提交**

```bash
cd d:/code/mdbox
git add web/style.css
git commit -m "feat(web): 窄屏分享页与登录页收尾适配"
```

---

## 自检记录

**Spec 覆盖检查**（对照 `docs/superpowers/specs/2026-10-02-mobile-reading-design.md`）：

| Spec 章节 | 对应任务 |
| --- | --- |
| 1. 断点 720px | Task 1 Step 3（替换媒体查询块） |
| 2. DOM 改动（三个新元素） | Task 1 Step 1-2（`#side-toggle` / `#side-backdrop`）、Task 2 Step 1（`.side-foot`） |
| 3. 侧栏抽屉行为 | Task 1 Step 3-6 |
| 4. 窄屏隐藏的入口 | Task 3 Step 1 |
| 5. 顶栏与工具条 | Task 1 Step 3（顶栏）、Task 3 Step 1（工具条） |
| 6. 阅读排版 | Task 4 Step 1 |
| 7. 触控尺寸与安全区 | Task 5 Step 1-3 |
| 8. 覆盖的页面（分享页/登录页） | Task 6 Step 1、Step 4-5 |
| 验证方式 1-5 | Task 1/3/4/5/6 的验证步骤 + Task 6 Step 6-7 |
| 受影响文件 | 全部任务均只动 `web/index.html`、`web/style.css`、`web/app.js`（+ spec 自身修正） |

已识别的 spec 偏差：spec 第 3 节的「matchMedia 复位」被判定为**不必要**（媒体查询内的规则在桌面端自动失效），已在 Task 1 Step 10 中改为修正 spec。

**占位符扫描**：无 TBD / TODO / 「类似 Task N」；每个代码步骤均含可直接粘贴的完整代码。

**命名一致性**：`openNav` / `closeNav` / `doLogout` / `renderUser` / `body.nav-open` / `#side-toggle` / `#side-backdrop` / `#side-mcp` / `#side-pwd` / `#side-logout` / `#side-user-name` / `#side-avatar` 在定义与引用处拼写一致。

**额外发现（已并入计划）**：iOS Safari 在输入框字号 <16px 时聚焦会自动放大整页 —— 原 spec 未覆盖，已加入 Task 5 Step 3 与 Task 6 真机验证项。
