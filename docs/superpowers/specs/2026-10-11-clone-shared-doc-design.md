# 分享页「复制到我的库」（clone） — 设计

日期：2026-10-11
状态：设计中（待评审）

## 背景与动机

当前分享是**只读**的：一篇文档开启分享后，任何人拿到固定签名链接 `/s/<user>/<id>/<sig>` 都能匿名阅读，并可下载 `.md` / PDF，但**无法编辑**。

随着产品转为**公开在线服务**（v0.4），出现了一个真实诉求：**拿到分享链接的人想把内容接手、继续写自己的版本**。现在的断点是——他只能下载 `.md`，再手动上传到自己的库，才能编辑。

目标场景（已与作者确认）：**接手开新篇**——别人把内容复制走，在自己的库里继续写，源文档不被改动。**不是**多人共编同一篇。

## 目标

- 分享页为**已登录**的 mdbox 用户提供一个入口：把该文档**复制一份到自己的库**。
- 复制出的副本是**完全独立的文档**，之后可自由编辑、归档、删除、再分享。
- **源文档分毫不改**，作者不受影响。
- 保持产品一贯的克制风格：不引入协作 / 权限 / 实时同步，不改架构不变量。

## 非目标

- ❌ 不做跨用户协作、多人共编同一篇、实时同步、presence。
- ❌ 不做角色 / 权限分级、用户间共享所有权。
- ❌ 不改 PRD §2.3 的「不做权限体系与跨用户协作」——本设计是**单向拷贝**，不是共享所有权，**不违反该非目标**。
- ❌ 不复制源文档的标签 / 分类（作者已定：只带标题 + 正文）。
- ❌ 不复制 `shared` 状态、`created` 时间、来源回链（`cloned_from` 留给 P1-4「文档间关联」）。

## 决策摘要

| 项 | 结论 |
|---|---|
| 触发条件 | 源文档 `shared=true` + 分享签名有效 + 调用者已登录 |
| 端点 | `POST /api/clone/{user}/{id}/{token}`（**需鉴权**，区别于匿名的 `/api/share/`） |
| 复制内容 | 标题 + 正文；标签 / 分类留空，由调用者自己归类 |
| 副本元数据 | 新 id、新 `created`/`updated`、`shared=false`、`source=clone` |
| 源文档 | 不读锁外写、不改动；反复 clone 得到多份独立副本（允许） |
| store 层 | **无需改动**，复用 `Create` |
| 前端 | 分享页加「复制到我的库」按钮；未登录点击跳登录；成功跳到副本 |

## 详细设计

### 1. 概念

- **分享链接 = 能力凭证**（证明"你能读到这篇"）；**登录 = 身份**（证明"你有地方放"）。二者齐备才允许 clone。
- clone 之后，副本与源文档再无关系（不联动、不回链），彻底落在调用者自己的库里。

### 2. 后端

**路由（`internal/api/server.go`）**

```go
mux.HandleFunc("POST /api/clone/{user}/{id}/{token}", s.cloneShared)
```

- 路径值名与现有分享路由**一致**（`{user}` / `{id}` / `{token}`），因此可直接复用 `s.sharedDoc(r)`。
- 该路径**不在** `publicPath` 的 `/api/share/` 前缀内 → 自然落入 `withAuth`，未登录返回 401，且 handler 里 `stOf(r)` 就是**调用者自己**的 store。

**handler（`internal/api/share.go`）**

```go
func (s *Server) cloneShared(w http.ResponseWriter, r *http.Request) {
    src, ok := s.sharedDoc(r) // 校验签名 + shared + 取出源文档（含正文）
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

- `sharedDoc` 已做 `Get`（填充 `Content`）且校验 `Shared`，**无需改 store**。
- `Source: "clone"`：`source` 取值域新增一项（PRD §6.2 原为 `agent | codebuddy | upload | web`），用于统计"从分享复制来的文档"。

### 3. 前端

**分享页（`web/app.js` 的 `setupShare` / `bindShareEvents`、`web/index.html`）**

- 分享页顶栏（主题 / 语言按钮旁）加一个按钮 `#share-clone`：文案「复制到我的库 / Copy to my library」。
- 点击：

```js
fetch('/api/clone/' + encodeURIComponent(shareCtx.user) + '/' +
      encodeURIComponent(shareCtx.id) + '/' + encodeURIComponent(shareCtx.sig),
      { method: 'POST', headers: { 'Accept-Language': I18N.lang() } })
  .then(function (r) {
    if (r.status === 401) { location.href = '/'; return null; } // 跳登录页
    if (!r.ok) { throw new Error(I18N.t('share.clone_failed')); }
    return r.json();
  })
  .then(function (data) {
    if (!data) { return; }
    location.href = '/#/doc/' + encodeURIComponent(data.doc.id); // 进自己的库并打开副本
  })
  .catch(function (e) { uiAlert(e.message || I18N.t('share.clone_failed')); });
```

- **不额外探测登录态**：按钮始终显示；未登录时后端返回 401，前端跳登录页。少一次请求、逻辑更短。（可选增强，本期不做：带 `?next=` 登录后回跳分享页。）
- 成功跳转 `/#/doc/<id>`：`enterApp` 在 `hashDocId()` 有值时即 `route()`，会打开新副本（见 `web/app.js:228`）。

**i18n（`web/i18n.js`）**

- 新增键（中英都要加）：`share.clone`（按钮）、`share.clone_failed`（失败提示）。`share.gone` 已存在。

### 4. 边界与安全

- 未登录 clone → 401；源文档取消分享 / 签名无效 → 404（沿用 `sharedDoc` 校验）。
- **暴露面 ≈ 0**：内容本来就能被匿名下载 `.md`，clone 只是把"下载 + 手传"自动化。
- **无跨 store 写**：源文档只读，写入落在调用者自己的 store；无锁竞争、无并发冲突。
- 反复 clone → 多份副本，各自独立，**这是允许的**（不做去重，保持简单）。

## 涉及文件

| 文件 | 改动 |
|---|---|
| `internal/api/server.go` | 注册 `POST /api/clone/{user}/{id}/{token}` 路由（`publicPath` 不变） |
| `internal/api/share.go` | **新增** `cloneShared` handler（复用 `sharedDoc`） |
| `internal/api/clone_test.go` | **新增**：clone 成功（`source=clone`、tags 空、源文档不变）/ 未登录 401 / 取消分享 404 的单测（`internal/api` 已有测试，沿用 `httptest` 风格） |
| `web/index.html` | 分享页顶栏加 `#share-clone` 按钮 |
| `web/app.js` | `setupShare` / `bindShareEvents` 绑定 clone 行为与跳转 |
| `web/i18n.js` | 加 `share.clone` / `share.clone_failed` 中英键 |
| `web/style.css` | 分享页按钮沿用现有按钮类，必要时补细节 |
| `docs/PRD.md` | §6.2 `source` 取值域补 `clone`（可选，随实现一并改） |

**不改**：`internal/store`（复用 `Create`）、`internal/auth`、`internal/users`、`internal/config`、`internal/render`、MCP、`publicPath` 逻辑。

## 实现顺序（建议）

1. **后端**：加路由 + `cloneShared`；用 curl 验证 401 / 201 / 404 与 `source=clone`、tags 为空。
2. **前端**：分享页按钮 + 跳转 + i18n 键；`go build` 重新构建（`//go:embed web`）。
3. **收尾**：PRD §6.2 补 `source: clone`；人工冒烟。

## 边界与注意事项

- 前端经 `//go:embed web` 打包，**改完必须重新构建**才生效；`.gitattributes` 把 `*.js` 标为 `binary`，`git diff` 看不到内容属正常，用 `git diff --text -- web/app.js`。
- 路由路径值名必须与 `sharedDoc` 里读取的一致（`user` / `id` / `token`），否则 `PathValue` 取空导致签名校验失败。
- 新增 `source: "clone"` 值不影响既有逻辑（`source` 仅作展示 / 统计）。
- 面向访客的按钮文案必须**中英双语**（公开服务定位，见 `project_positioning`）。

## 验证方式

- 构建门禁：`gofmt -l .`、`go vet ./...`、`go test ./...`、`go build` 全过。
- 后端 curl：
  - 登录用户 + 有效分享链接 → `201`，新文档 `source=clone`、`tags=[]`、`shared=false`，正文与标题同源；源文档内容与 `updated` 不变。
  - 未登录 → `401`；对未分享 / 错误签名文档 → `404`。
  - 反复调用 → 产生多份独立副本。
- 浏览器人工验收（作者本人）：
  - 已登录访客在分享页点「复制到我的库」→ 跳到自己的库并打开副本，可直接编辑。
  - 未登录访客点击 → 跳到登录页。
  - 中 / 英两种界面语言下按钮文案正确。
