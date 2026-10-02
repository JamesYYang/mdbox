# 登录页宣传与品牌一致性 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 mdbox 加 slogan、把登录页升级为左右两栏的落地页、并让品牌可点击回首页/列表。

**Architecture:** 纯前端改动（vanilla HTML/CSS/JS，经 `//go:embed` 打包），不碰任何 Go 后端。三处改动：`web/index.html` 结构、`web/style.css` 样式、`web/app.js` 事件绑定。

**Tech Stack:** 原生 HTML/CSS/JS（单一 IIFE），无构建工具、无前端依赖。

## Global Constraints

- 不引入任何前端依赖或构建工具；保持 vanilla 与单一 IIFE 风格。
- 前端经 `//go:embed web` 打包，**改完必须重新编译**才生效（`go run` / `go build` 会重新编译）。
- 宣传文案中 `Agent` 一律首字母大写。
- 所有颜色一律用既有 CSS 变量（`--bg / --surface / --border / --text / --muted / --accent / --danger`），保证深浅色主题正确。
- 响应式沿用现有 `@media (max-width: 720px)` 断点。
- 不改 `main.go`、`internal/**`、路由、认证、API。
- 本项目**没有前端测试框架**，且「引入前端构建/测试工具」是 spec 明确非目标。因此每个任务以 **构建门禁 + 浏览器人工验收** 作为验证手段；浏览器验收由作者本人执行。

---

## 文件结构

| 文件 | 责任 | 本计划改动 |
|---|---|---|
| `web/index.html` | 三个视图的静态结构 | 登录区改两栏 + 宣传；分享/主应用品牌加 id 与 slogan |
| `web/style.css` | 全部样式 | 登录两栏 Grid、宣传区样式、`.brand-slogan`、窄屏堆叠、品牌可点样式 |
| `web/app.js` | 全部交互（单一 IIFE） | 新增 `goHome()`；绑定主应用/分享页品牌点击 |

---

### Task 1: 登录页改为左右两栏 + 宣传文案

**Files:**
- Modify: `web/index.html:12-24`（登录视图整段替换）
- Modify: `web/style.css:66-82`（登录样式块）
- Modify: `web/style.css:399-401`（`@media (max-width: 720px)` 内的登录规则）

**Interfaces:**
- Consumes: 无
- Produces:
  - `.login-panel`（两栏 Grid 容器）
  - `.login-promo` / `.login-promo-brand` / `.login-promo-lead` / `.login-promo-list` / `.login-promo-mcp`
  - `.brand-slogan`（Task 2 复用）
  - 登录卡片去掉了 `.login-brand`；`#login-form`、`#login-tip`、`#login-user`、`#login-pass`、`#login-pass2`、`#login-submit`、`#login-msg`、`#login-switch` 的 id 与层级**保持可用**（`app.js` 里 `login-*` 逻辑依赖它们）。

- [ ] **Step 1: 替换登录视图结构**

把 `web/index.html` 第 12–24 行整段替换为：

```html
<!-- 登录视图 -->
<section id="view-login" class="login-wrap" hidden>
  <div class="login-panel">
    <aside class="login-promo">
      <div class="login-promo-brand">
        <img class="logo logo-login" src="/mdbox.png" alt="MDBox">MDBox<span class="brand-slogan">· 人写，Agent 也写</span>
      </div>
      <p class="login-promo-lead">自托管的 Markdown 知识库</p>
      <ul class="login-promo-list">
        <li>纯 .md 文件，存在你自己的磁盘</li>
        <li>没有数据库，一个二进制就能跑</li>
        <li>人和 Agent 共用一个库</li>
        <li>一键只读分享</li>
      </ul>
      <p class="login-promo-mcp">让 Agent 通过 MCP 直接读写你的库</p>
    </aside>
    <form id="login-form" class="login-card">
      <p class="login-tip" id="login-tip">请登录后使用</p>
      <input id="login-user" placeholder="用户名" autocomplete="username">
      <input id="login-pass" type="password" placeholder="密码" autocomplete="current-password">
      <input id="login-pass2" type="password" placeholder="再次输入密码" autocomplete="new-password" hidden>
      <button class="primary" type="submit" id="login-submit">登录</button>
      <div id="login-msg" class="login-msg"></div>
      <button type="button" class="ghost" id="login-switch">没有账号？注册</button>
    </form>
  </div>
</section>
```

要点：登录卡片里**删掉了 `.login-brand`**（品牌由左栏承担）；卡片内其余元素的 id 与顺序不变。

- [ ] **Step 2: 替换登录样式块**

把 `web/style.css` 第 66–82 行（`/* ---------- 登录 ---------- */` 到 `.login-msg` 那一段）整段替换为：

```css
/* ---------- 登录 ---------- */
.login-wrap {
  min-height: 100vh; min-height: 100dvh; display: flex; align-items: center; justify-content: center;
  background: var(--bg); padding: 20px;
}
.login-panel {
  display: grid; grid-template-columns: minmax(0, 1fr) 320px; gap: 40px; align-items: center;
  width: 100%; max-width: 780px;
}
.login-promo { min-width: 0; }
.login-promo-brand {
  display: flex; align-items: center; gap: 10px;
  font-size: 24px; font-weight: 700; letter-spacing: .5px;
}
.login-promo-lead { margin: 10px 0 16px; color: var(--muted); font-size: 14px; }
.login-promo-list { margin: 0 0 16px; padding-left: 18px; font-size: 14px; line-height: 1.9; }
.login-promo-mcp { margin: 0; color: var(--accent); font-size: 13px; }
.login-card {
  width: 320px; display: flex; flex-direction: column; gap: 12px;
  padding: 28px 26px; border: 1px solid var(--border); border-radius: 14px;
  background: var(--surface);
}
.login-tip { margin: 0 0 6px; text-align: center; color: var(--muted); font-size: 13px; }
.pwd-form { display: flex; flex-direction: column; gap: 12px; }
.login-msg { min-height: 18px; font-size: 13px; color: var(--danger); text-align: center; }
.brand-slogan { font-size: 13px; font-weight: 400; color: var(--muted); letter-spacing: 0; }
```

注意：原来的 `.login-brand` 规则被删除（对应元素已移除），新增 `.brand-slogan` 供本任务与 Task 2 共用。

- [ ] **Step 3: 更新窄屏堆叠规则**

在 `web/style.css` 的 `@media (max-width: 720px)` 块内，把这两行：

```css
  .login-wrap { padding: 16px; }
  .login-card { width: 100%; max-width: 340px; padding: 22px 18px; }
```

替换为：

```css
  .login-wrap { padding: 16px; }
  .login-panel { grid-template-columns: 1fr; gap: 20px; max-width: 340px; }
  .login-card { width: 100%; padding: 22px 18px; }
```

- [ ] **Step 4: 构建门禁**

Run:
```bash
gofmt -l . && go vet ./... && go test ./... && go build -o /tmp/mdbox_check . && rm -f /tmp/mdbox_check
```
Expected: `go vet` / `go test` / `go build` 全部退出码 0（`gofmt -l .` 因仓库 CRLF 检出会列出文件，属既有现象，不是本任务引入）。

- [ ] **Step 5: 浏览器人工验收（作者本人）**

Run: `go run . -addr :8080 -data ./data`，然后：

- 桌面宽度打开 `/` → 左宣传、右登录卡两栏并排；左栏显示 `MDBox · 人写，Agent 也写`、定位句、4 条要点、绿色 MCP 一行。
- 登录卡里**只有一行**品牌缺失（即看不到第二个 MDBox）；提示语、用户名/密码、登录按钮正常。
- 缩窄到 <720px → 变上下堆叠：宣传在上、卡片在下，均不被挤出。
- 切换深浅色主题 → 两栏配色正确。
- 点"没有账号？注册" → `login-pass2` 出现、按钮文案变化（确认注册流程未坏）。

- [ ] **Step 6: 提交**

```bash
git add web/index.html web/style.css
git commit -m "feat(web): 登录页改左右两栏并加产品宣传"
```

---

### Task 2: 分享页 slogan 与品牌点击

**Files:**
- Modify: `web/index.html:29`（分享页 `.brand`）
- Modify: `web/index.html:58`（主应用 `.brand`）
- Modify: `web/style.css`（在 `.brand` 规则附近新增可点样式）
- Modify: `web/app.js`（新增 `goHome()`；`bindEvents()` 第 748 行；`bindShareEvents()`）

**Interfaces:**
- Consumes: Task 1 产出的 `.brand-slogan`
- Produces:
  - `#share-brand` / `#app-brand` 元素 id（供 `app.js` 绑定）
  - `.brand.clickable` 样式类
  - `goHome()`（`app.js` 内部函数）

- [ ] **Step 1: 分享页品牌加 id 与 slogan**

把 `web/index.html` 第 29 行：

```html
    <div class="brand"><img class="logo" src="/mdbox.png" alt="MDBox">MDBox</div>
```

替换为：

```html
    <div id="share-brand" class="brand clickable"><img class="logo" src="/mdbox.png" alt="MDBox">MDBox<span class="brand-slogan">· 人写，Agent 也写</span></div>
```

- [ ] **Step 2: 主应用品牌加 id 与可点类**

把 `web/index.html` 第 58 行：

```html
    <div class="brand"><img class="logo" src="/mdbox.png" alt="MDBox">MDBox</div>
```

替换为：

```html
    <div id="app-brand" class="brand clickable"><img class="logo" src="/mdbox.png" alt="MDBox">MDBox</div>
```

- [ ] **Step 3: 加可点品牌样式**

在 `web/style.css` 的 `.brand { ... }` 规则（第 92–95 行）**紧后面**插入：

```css
.brand.clickable { cursor: pointer; }
.brand.clickable:hover { color: var(--accent); }
```

- [ ] **Step 4: 新增 goHome()**

在 `web/app.js` 的 `leaveDocUrl()` 函数（第 186–190 行）之后插入：

```js
  // 点品牌回文档列表：等价「返回」，但在编辑态有未保存修改时先确认。
  function goHome() {
    if (state.mode === 'edit' && state.dirty && state.current) {
      uiConfirm('有未保存的修改，确定放弃？', { title: '放弃修改', confirmText: '放弃', danger: true })
        .then(function (ok) {
          if (!ok) { return; }
          state.dirty = false;
          showContent('list'); leaveDocUrl(); loadList();
        });
      return;
    }
    showContent('list'); leaveDocUrl(); loadList();
  }
```

- [ ] **Step 5: 绑定主应用品牌点击**

把 `web/app.js` 第 748 行：

```js
    $('p-back').addEventListener('click', function () { showContent('list'); leaveDocUrl(); loadList(); });
```

替换为：

```js
    $('p-back').addEventListener('click', goHome);
    $('app-brand').addEventListener('click', goHome);
```

（`p-back` 只在预览态可见，`goHome()` 的编辑守卫不会触发，行为与原来一致。）

- [ ] **Step 6: 绑定分享页品牌点击**

在 `web/app.js` 的 `bindShareEvents()`（第 886–898 行）里，`$('share-theme-btn')` 那行之后插入：

```js
    $('share-brand').addEventListener('click', function () { location.href = '/'; });
```

- [ ] **Step 7: 构建门禁**

Run:
```bash
go vet ./... && go test ./... && go build -o /tmp/mdbox_check . && rm -f /tmp/mdbox_check
```
Expected: 全部退出码 0。

- [ ] **Step 8: 浏览器人工验收（作者本人）**

Run: `go run . -addr :8080 -data ./data`，然后：

- 分享页：品牌显示 `MDBox · 人写，Agent 也写`，点它跳到 `/`（登录页）。
- 主应用：在文档列表点品牌 → 无变化（已在列表）；打开某文档后点品牌 → 回到列表，且浏览器后退不会多出多余历史。
- 在编辑态改点内容（产生未保存修改）后点品牌 → 弹确认框；点"放弃"才回列表，点"取消"留在编辑页。
- 悬停主应用/分享页品牌 → 出现指针与强调色反馈；登录页品牌不可点。

- [ ] **Step 9: 提交**

```bash
git add web/index.html web/style.css web/app.js
git commit -m "feat(web): 分享页加 slogan，品牌可点回列表/首页"
```

---

## Self-Review

**Spec coverage:**
- slogan「人写，Agent 也写」→ Task 1（登录页）、Task 2（分享页）✅
- 登录页左右两栏 + 定位句 + 4 条要点 + MCP 说明 → Task 1 ✅
- 分享页 slogan → Task 2 ✅
- 品牌点击三视图分流（应用回列表 / 分享跳 `/` / 登录不响应）→ Task 2 ✅
- 去掉登录卡品牌 → Task 1 Step 1 ✅
- 720px 堆叠 → Task 1 Step 3 ✅
- 深浅色变量 → Task 1 Step 2 ✅
- 不改后端 → 所有任务均未触碰 Go 文件 ✅

**Placeholder scan:** 无 TBD/TODO；每个代码步骤均为可直接粘贴的完整片段。

**Type consistency:** `.brand-slogan` 在 Task 1 定义、Task 2 复用；`goHome()` 在 Task 2 Step 4 定义、Step 5 绑定；`#share-brand` / `#app-brand` 在 Step 1/2 定义、Step 5/6 使用——名称一致。
