# 分享链接「登录后可编辑」 — 设计

日期：2026-10-24
状态：待评审

## 背景与动机

现状分享有两种能力：**只读链接**（匿名可读）与**clone**（登录后把内容复制到自己库，源文档不动）。

作者在真实使用中发现断点：一篇文档要交给同事看，同事同时需要就地补改几段。clone 会产生两份副本，改完还得手工合并，等于把编辑成本转移回了作者。

结论：把分享从「只读」升级为**可选的可编辑链接**——已登录的用户凭链接直接在分享页编辑，保存即写回原文档。

这是产品定位上的一次有意松动：v0.4 的分享 clone 设计明确写过「不做多人共编同一篇」。本次按作者判断推翻该结论，但严格限制在**单篇、整篇覆盖、无实时、无权限体系**的范围内，不引入协作者、角色、审计。

## 目标

- 文档分享模式由布尔扩展为三态：`off` / `view` / `edit`。
- 在 `edit` 模式下，**任何已登录用户**在分享页可直接编辑并保存，内容写回**原文档**。
- 撤销是即时的：把模式切回 `view` 或 `off` 后，别人已打开的编辑页下一次保存立刻失败。
- 不改变架构不变量：无数据库、文件即真相、内存索引、单实例。

## 非目标

- ❌ 不做实时协同：无 presence、无光标同步、无 WebSocket。
- ❌ 不做版本历史、快照、回滚（**作者已明确接受**，见「已知代价」）。
- ❌ 不做并发冲突检测（乐观锁 / ETag），后保存者静默覆盖。
- ❌ 不做匿名写入：写入必须登录。
- ❌ 不做协作者白名单、角色或权限分级（仍遵守 PRD §2.3）。
- ❌ 不做「谁改的」审计：`updated` 只记录时间，不记录身份。
- ❌ 不改 clone 的行为与语义。

## 决策摘要

| 项 | 结论 |
|---|---|
| 表达方式 | 文档级三态 `share_mode`，同一链接，权限由文档当前状态决定 |
| 谁能写 | 持有链接 **且** 已登录的任何用户（链接即能力凭证） |
| 写向何处 | 源文档本身（不是副本），走 `s.reg.Store(owner)` |
| store 层 | **零改动**：复用 `Update`，体积/文本/配额三道上限自动生效 |
| 撤回 | 切回 `view`/`off` 即时生效，无需等会话过期 |
| 冲突 | 不处理，last-write-wins |
| 前端位置 | 分享页就地编辑，不进入应用框架、看不到作者的库 |
| 登录流程 | 就地切登录视图，**不使用 `?next=`**；草稿在内存中跨登录保留 |
| 旧链接 | 全部继续有效，一律视为 `view` |

## 1. 数据模型

### frontmatter

```yaml
---
title: 某文档
share: view        # off 时整行省略；取值 view | edit
---
```

- 旧的 `shared: true` 在 `loadFile` 中被映射为 `view`；写回时一律写 `share:`，不再写 `shared`。
- 因此**已发出的分享链接全部继续有效**，且都不会凭空获得写权限。

### Doc 结构

```go
ShareMode string `json:"share_mode"` // off | view | edit，唯一真相
Shared    bool   `json:"shared"`      // 派生字段：ShareMode != "off"
```

保留 `Shared` 是刻意的：列表徽标、详情页 meta、`api.go:123` 的 shareUrl 分支共 5 处都在读 `shared`，派生后这些调用点一行不改。两个字段**从不各自赋值**，只在 `loadFile` / `Update` / `SetShareMode` 的同一处同时推出，不存在漂移。

### store API

`SetShared(id, bool)` 替换为：

```go
func (s *Store) SetShareMode(id, mode string) (*Doc, error) // mode ∈ {off, view, edit}
```

非法 mode 返回错误（`ErrBadShareMode`）。与 `SetShared` 一致：先改内存再落盘，`Updated` 前移。

## 2. 后端

### 2.1 鉴权边界：必须按方法判断

现在 `publicPath` 是路径前缀匹配：

```go
return strings.HasPrefix(p, "/api/share/")
```

它**连 POST/PUT 一起放行**。也就是说 `/api/share/` 下新增的任何写端点，默认是匿名的——只加路由不加判断，等于开了一个匿名写入口。这是本设计唯一的真安全点。

改为：

```go
func publicPath(r *http.Request) bool {
    switch r.URL.Path {
    case "/api/login", "/api/register", "/api/logout":
        return true
    }
    return r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/share/")
}
```

三个精确白名单不动；分享相关的**读取**（`GET /api/share/...`、`.../download`）行为完全不变；写入自然落进 `withAuth`，未登录 401。

### 2.2 路由与 handler

```go
mux.HandleFunc("PUT /api/share/{user}/{id}/{token}", s.editShared)
```

handler 的关键点：`stOf(r)` 是**调用者自己**的 store，写别人的文档必须改用源作者的 store。

```go
// editableDoc 在 sharedDoc 的基础上要求 share_mode == "edit"，并返回源作者的 store。
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
    if err != nil || d.ShareMode != "edit" {
        return nil, nil, false
    }
    return d, st, true
}

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

- `Title` / `Tags` / `Category` / `Source` 一律不动，只覆盖正文。
- 分享签名只依赖 `user + id`，与正文无关 → 保存后链接不变。
- 归档文档同样可写（与现有「归档仍可匿名读」一致），不额外加规则。

### 2.3 状态码

| 情况 | 状态码 |
|---|---|
| 未登录 / 无有效会话 | 401（`withAuth`） |
| 签名错误 / 未分享 / `share_mode != edit` | 404（统一按「不存在」处理） |
| 正文超 2MB | 413 |
| 非 UTF-8 或含 NUL | 415 |
| 撑爆源作者 200MB 配额 | 与 `storeErrStatus` 一致 |

### 2.4 分享开关接口

`POST /api/docs/{id}/share` 请求体由 `{shared: bool}` 改为 `{mode: "off"|"view"|"edit"}`，响应 `{mode, url}`（`off` 时无 `url`）。

mode 不在取值域内时在 **api 层**校验并返回 400（`i18n.KeyBadJSON` 同级的新键 `KeyBadShareMode`），不要让它落到 `storeErrStatus`——那里会把它误映射成 404。

MCP 未暴露分享能力，无影响；PRD §6.3 同步更新。

## 3. 前端

### 3.1 分享弹窗

「开启分享」复选框换成三态 `select`：关闭 / 只读链接 / 可编辑链接，下方提示文案随选择变化。`onToggleShare` 改为读 `select.value` 提交 `{mode}`。

### 3.2 分享页

- 文档 `share_mode === "edit"` 时，顶栏多一个「编辑」按钮（`#share-edit`），`brand_tip` 由「只读分享」变「可共同编辑」。
- **点「编辑」不做鉴权探测**，直接进编辑态。理由：进编辑器不需要权限，只有保存需要；省一次 `/api/me`，也让未登录的人先写起来。
- 编辑态：隐藏 `#share-content`，显示 `#share-editor`（textarea + 预览面板 + 保存/取消）。实时预览复用 `POST /api/preview`，300ms 防抖。
- 取消：有改动时走 `uiConfirm` 确认放弃，然后回只读视图。

### 3.3 保存

```
PUT /api/share/{user}/{id}/{sig}   { "content": "..." }
```

- `200` → 用返回的 `html` 回只读视图，重建目录（`buildToc`）。
- `404` → 「分享已失效或已关闭编辑，内容未保存」，退出编辑态。
- `401` → 把当前正文存进 `shareCtx.draft`，`showLogin()` 就地切登录视图。

### 3.4 登录后回到分享页

SPA 在 `/s/...` 下也是同一份 `index.html`，`view-login` 与 `view-share` 是同一页面的两个视图，所以**不需要 `?next=`**，也就没有开放跳转白名单的问题。

但登录成功回调现在无条件调用 `enterApp()`（`showView('app')` + `loadList()`），在 `/s/...` 下会把应用渲染在分享页地址上。改为：

```js
if (location.pathname.indexOf('/s/') === 0) { resumeShare(d.user); }
else { enterApp(d.user); }
```

`resumeShare(user)`：设置 `state.user`、重新 `setupShare()` 拉取文档；若 `shareCtx.draft != null` 则直接回到编辑态并把草稿填回去。

注意 `api()` 的 401 分支会调 `showLogin()`，而 `showLogin()` 内部会 `resetSession()`（清空 `state.current` 等）。因此草稿必须存在 `shareCtx`（独立于 `state`）上，否则会在登录前被清掉。

### 3.5 i18n（中英都要加）

`share.brand_tip_edit`、`share.edit`、`share.save`、`share.cancel`、`share.save_ok`、`share.save_failed`、`share.gone_on_save`、`share.login_to_save`、`sharemodal.mode_off`、`sharemodal.mode_view`、`sharemodal.mode_edit`、`sharemodal.hint_edit`。

既有 `sharemodal.toggle` / `share.brand_tip` 文案改写为对应新语义。

### 3.6 样式

编辑态在 `.share-wrap` 内，无法直接复用应用内双栏布局（那套依赖 `.app` 的 grid）。新增 `.share-editor` 的独立样式：`display:grid` 两栏（窄屏单栏），`textarea` 等宽字体、可滚动，预览区沿用 `.markdown`。

## 4. 边界与安全

- **链接即能力凭证**：拿到可编辑链接的登录用户都能写，无法区分「这张链接给谁」。这是「不做权限体系」的直接代价，作者已知悉。
- **撤销即时**：每次保存都重新校验 `share_mode == "edit"`，源作者切回只读后立刻失效。
- **无法审计**：只记录 `updated` 时间，不知道是谁改的。
- **配额归属**：写入计入**源作者**的配额与单篇上限；持有链接的人可以撑爆作者的 200MB。上限本身仍在，只是归属无法在无权限体系下区分。
- **暴露面**：内容本来就能被匿名下载 `.md`，可编辑链接只是把「下载 → 手改 → 回传」这三步收敛成一步，未扩大信息暴露面。
- **XSS**：保存返回的 `html` 与 `POST /api/preview` 同源（`internal/render` + bluemonday 白名单），前端继续走 `innerHTML` 是安全的。
- **`//go:embed web`**：前端改动必须重新构建才生效。

## 涉及文件

| 文件 | 改动 |
|---|---|
| `internal/store/store.go` | `Doc.ShareMode` + `frontmatter.Share`；`loadFile` 兼容 `shared: true`；`serialize` 写 `share:`；`SetShared` → `SetShareMode`；`ErrBadShareMode` |
| `internal/api/server.go` | `publicPath` 改按方法判断；注册 `PUT /api/share/{user}/{id}/{token}` |
| `internal/api/share.go` | 新增 `editableDoc`、`editShared`；`shareDoc` 改收 `{mode}`、返回 `{mode, url}`；`sharedDoc` 要求 `Shared`（派生，等价于 `ShareMode != off`） |
| `internal/api/share_edit_test.go` | **新增**：未登录 401 / `view` 模式 404 / `edit` 模式 200 且正文落盘、标题标签不变 / 超限 413 / 含 NUL 415 / 切回只读后保存 404 |
| `internal/api/clone_test.go` 等 | 跟随 `SetShared` → `SetShareMode` 的签名变更 |
| `internal/i18n` | 新增 `KeyBadShareMode`（400 文案，中英各一条） |
| `web/index.html` | 分享页加 `#share-edit` 与 `#share-editor`；分享弹窗复选框改 `select` |
| `web/app.js` | 编辑态进入/退出/保存、草稿保留、`resumeShare`、登录回调分流、分享弹窗三态 |
| `web/i18n.js` | 新增 12 个键（中英），改写 2 个既有键 |
| `web/style.css` | `.share-editor` 两栏布局与窄屏降级 |
| `docs/PRD.md` | §6.3 分享接口体改 `{mode}` + 新增 PUT；§6.2 `shared` → `share`；§2.3 非目标补一句「不做实时协同与版本历史」 |
| `CODEBUDDY.md` | Sharing 一节改写：三态、按方法判断 `publicPath`、写入走源 store |

**不改**：`internal/users`、`internal/auth`、`internal/render`、`internal/mcp`、`internal/config`、clone 逻辑。

## 实现顺序

1. **store**：字段、兼容读、`SetShareMode`，跑 `go test ./internal/store` 与现有 api 测试（会有编译错误，顺手改断言）。
2. **后端**：`publicPath` 方法化 + `editableDoc` + `editShared` + `shareDoc` 三态；补 `share_edit_test.go`；curl 跑一遍状态码矩阵。
3. **前端**：弹窗三态 → 分享页编辑态 → 登录分流与草稿恢复 → i18n → 样式；`go build` 重新打包。
4. **收尾**：PRD / CODEBUDDY.md 同步；人工冒烟。

## 验证方式

**构建门禁**：`gofmt -l .`、`go vet ./...`、`go test ./...`、`go build -o mdbox .` 全过。

**单测**（`share_edit_test.go`，沿用 `httptest` 风格）：
- 无会话 `PUT` → 401
- `share_mode=view` → 404
- `share_mode=edit` → 200；读磁盘确认正文已变、`title/tags/category/source` 未变、`updated` 前移
- 正文 > 2MB → 413；含 NUL → 415
- 保存前把模式切回 `view` → 404
- 回归：`GET /api/share/...` 在 `view` / `edit` 下都 200

**curl**：上述矩阵在同一环境复跑一遍，确认 `publicPath` 改动没有波及 `/api/login`、`/api/register`、`/api/logout` 与 clone。

**人工验收**（作者本人，两个账号）：
1. A 把文档设为「可编辑链接」，把链接发给 B。
2. B 登录后打开链接 → 顶栏显示「可共同编辑」+「编辑」按钮 → 改两段 → 保存 → 内容更新，目录正常。
3. A 刷新看到 B 的改动。
4. A 切回「只读链接」→ B 页面再次保存 → 提示分享已失效。
5. B 退出登录后打开链接 → 点编辑 → 写一段 → 保存 → 被送到登录 → 登录后回到编辑态，草稿还在。
6. 中英两种界面语言下按钮与提示正确。

**回归**：旧 frontmatter `shared: true` 的文档仍可只读分享；clone 行为不变。

## 已知代价（作者已确认接受）

- **无版本、无快照**：两个人同时改，后保存者静默覆盖；同事手滑清空也找不回来。第一篇真实协作文档出这类事故时，大概率会回来要快照——届时在 `data/users/{u}/.history/{id}/<ts>.md` 加即可，字段与目录结构都留得住。
- **无法区分编辑者**：只知道「被改过」，不知道「被谁改」。
- **配额可被撑爆**：写入计入源作者。
