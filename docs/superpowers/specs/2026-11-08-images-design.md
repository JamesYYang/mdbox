# 图片支持：内容寻址图片库 — 设计

日期：2026-11-08
状态：已确认，待实施

## 背景与动机

现状：Markdown 里只能写外链图片，写文档时要先把截图传到别处。

排查后发现**渲染层不是瓶颈**：

- `internal/render` 用 bluemonday 的 `UGCPolicy`，它包含 `AllowImages()`（`img` 放行 `src/alt/title/width/height/loading`），并且 `AllowStandardURLs()` 打开了 `AllowRelativeURLs(true)` 与 `http/https/mailto` 三种 scheme。
- `main.go` 的 CSP 已经是 `img-src 'self' data: https: http:`。

真正的卡点是**没有地方存图**：`store` 的写入通道只接受合法 UTF-8 文本（`checkContent`），二进制改名成 `.md` 会被 `ErrNotText` 挡下；单篇 2 MB、单用户 200 MB 的限额也是按纯文本来设的。

作者的真实场景（已确认）：

1. 截图后想直接粘进文档；
2. 手上一批本地图片要导入并插入；
3. agent 通过 MCP 写文档时需要配图。

## 已确认的约束

| 项 | 结论 | 来源 |
|---|---|---|
| 存储位置 | `data/users/{用户名}/assets/`，随 `data/` 一起被 git 备份 | 作者选择 |
| 匿名访问 | 图片地址不可猜即视为公开，不跟分享状态联动 | 作者选择 |
| 配额 | 沿用 200 MB 总配额，单图上限 5 MB，不做压缩 | 作者选择 |
| 方案形态 | 内容寻址图片库（方案 A），不做按文档绑定、不做 data URI | 作者选择 |

## 目标

- 文档正文可以引用本站自己的图片，Web 与 MCP 两条写入通道都支持。
- 分享页（匿名）能正常显示这些图片。
- 图片文件与 `.md` 同处 `data/` 下，备份、迁移、换机器仍然只需搬一个目录。

## 非目标

- ❌ 不做压缩、缩略图、EXIF 处理、图片编辑。
- ❌ 不做图库管理页（列出/删除/重命名图片）。
- ❌ 不做孤儿图回收：删文档或覆盖正文不会清理没人引用的图片。
- ❌ 不做「导出含图 zip」：下载/克隆出去的 `.md` 里图片仍指向本站。
- ❌ 不改渲染层与 CSP（已满足需求）。

## 决策摘要

| 项 | 结论 |
|---|---|
| 目录 | `data/users/{用户名}/assets/`，启动时 `MkdirAll` |
| 文件名 | `{sha256 前 16 字节 hex}.{png\|jpg\|gif\|webp}`，128 位内容哈希 |
| 去重 | 内容寻址天然去重：同名已存在直接复用，不重复写盘/占配额 |
| 读取路径 | `GET /a/{用户名}/{文件名}`，匿名、无签名、长缓存 |
| 上传接口 | `POST /api/assets`（需登录，multipart，单张） |
| 单图上限 | `store.MaxImageBytes = 5 << 20` |
| 配额 | 图片计入 `usedBytes()`，与文档共用 200 MB |
| Markdown 引用 | 根绝对路径 `![](/a/{user}/{hash}.png)` |
| MCP | 新增 `upload_image`，`data_base64` 或 stdio 下的 `source_path` 二选一 |
| 格式 | 只收 png/jpeg/gif/webp；**SVG 拒收** |

## 1. 存储（`internal/store`）

```
data/users/{用户名}/
├── docs/*.md
├── archive/*.md
└── assets/{32位hex}.{png|jpg|gif|webp}
```

- `New()` 的 `MkdirAll` 列表加 `assets`。
- 新增内存索引 `assets map[string]int64`（文件名 → 字节数），在 `reload()` 里用 `glob(root/assets/*)` 重建，和文档索引同一把锁。
- `usedBytes()` 把 `assets` 一起算进去。这样文档写入的配额检查也会感知图片占用，语义变成「该用户的全部内容」。
- 常量：`MaxImageBytes = 5 << 20`。`MaxUserBytes` 的注释从「含归档」改为「含归档与图片」。
- 新错误：`ErrImageTooLarge`、`ErrUnsupportedImage`（SVG 也走后者），由 `api.storeErrStatus` 映射到 413 / 415。
- **大小与类型校验都放在 store 里**，不在 api / mcp 各写一份：Web 上传与 `upload_image` 走同一个入口，错误语义天然一致。
- 新方法（签名以实施为准，语义如下）：
  - `SaveImage(data []byte) (name string, size int64, err error)` —— 顺序：`len(data) > MaxImageBytes` → `ErrImageTooLarge`；`http.DetectContentType(data[:512])` 映射扩展名，不在白名单 → `ErrUnsupportedImage`；算 SHA-256 拼文件名；已存在则直接返回（**不做配额检查**，因为没有新增磁盘占用）；不存在则持锁校验配额后落盘，成功后再更新 `assets` 索引。
  - `OpenImage(name string) (*os.File, os.FileInfo, error)` —— 先校验文件名匹配 `^[0-9a-f]{32}\.(png|jpg|gif|webp)$`，不匹配按不存在处理（不返回「参数非法」，避免探测）。
- 原子性：落盘先写临时文件再 rename（与 `users.yaml` 的写法一致），避免半张图被索引到；`usedBytes()` 要求调用方持锁，`SaveImage` 自己加锁。

## 2. HTTP（`internal/api` + `main.go`）

### 上传 `POST /api/assets`

- 需登录（挂在现有 `/api/` 的 `withAuth` 之下）。
- `multipart/form-data`，字段名 `file`；一次一张。批量由前端串行多次调用，因此不改 32 MB 的全局请求体上限。
- handler 只做「读请求体 + 调 `store.SaveImage` + 把错误映射成状态码」，不在 api 层重复校验大小与类型。
- 类型白名单到扩展名的映射（在 store 内）：`image/png→png`、`image/jpeg→jpg`、`image/gif→gif`、`image/webp→webp`；其余一律 415（含 `image/svg+xml`）。
- **不信任客户端文件名与扩展名**，落盘名完全由内容决定。
- 响应 `200` + `{"url":"/a/alice/3f2a...png","name":"3f2a....png","bytes":12345}`。去重时同样 200 —— 这是幂等的「按内容入库」，不是每次新建资源。

### 读取 `GET /a/{user}/{name}`

- 在 `main.go` 注册 `mux.Handle("GET /a/", ...)`，必须排在 `mux.Handle("/", staticFiles(...))` 之前。
- **匿名**：不经过 `withAuth`，因为分享页要读图；也不进 `/api/` 前缀，避免再次动 `publicPath` 的放行规则（v0.5 已经在这一点上踩过坑）。
- 校验：用户名必须存在于 `users.Registry`（`Registry.Store` 会拒绝未知用户），文件名必须匹配上面的正则；任一不满足 → 404。两个字段都校验，`filepath.Join` 没有穿越空间。
- 用 `http.ServeContent` 输出（自带 Range 与 `If-Modified-Since`），并设：
  - `Content-Type`：按扩展名映射（png→`image/png`，jpg→`image/jpeg`，gif→`image/gif`，webp→`image/webp`）
  - `Cache-Control: public, max-age=31536000, immutable`（内容哈希命名，永不变更）
  - `X-Content-Type-Options: nosniff` 已有全局中间件，不重复
- 不列目录、不回退到 SPA。

### 为什么 Markdown 里写根绝对路径

同一段正文会在三个位置渲染：文档页（hash 路由 `/#/doc/{id}`）、分享页（`/s/{user}/{id}/{sig}`）、编辑器预览（`POST /api/preview` 返回片段）。只有 `/a/...` 这种根绝对路径在三处都解析到同一地址；相对路径 `assets/x.png` 在分享页会变成 `/s/{user}/{id}/assets/x.png` 而 404。

## 3. 前端（`web/app.js` + `web/index.html` + `web/i18n.js`）

两个编辑入口共用一份 helper：文档编辑 `#d-source` 与分享页编辑 `#share-source`。

- **粘贴**：监听 `paste`，若 `clipboardData.files`（或 `items`）里有 `image/*`，`preventDefault()` 后逐张上传，成功后在光标处插入 `![alt](/a/{user}/{hash}.png)` 并补一个换行。alt：有原始文件名就用文件名（去扩展名），剪贴板截图没有文件名则用 `图片`/`image`。
- **拖拽**：只在 textarea 上接管 `dragover`（`preventDefault`）与 `drop`，取 `dataTransfer.files` 走同一条链路；不接管整页拖拽，避免和浏览器默认行为打架。
- **按钮**：编辑器工具栏加「插入图片」按钮 + 隐藏 `<input type="file" accept="image/*" multiple>`，多选串行上传、逐张插入。
- 插入用的 URL 一律取响应里的 `url` 字段，前端不自己拼用户名/哈希（分享页尤其不假设当前登录人就是文档所属人）。
- 插入后手动派发一次 `input` 事件，复用现有的脏标记与 300 ms 实时预览逻辑，不新增状态。
- 上传中在现有 `msg()` 提示区显示进度（第 n/总数）；单张失败不中断其余，最后汇总失败数量。
- **分享页不需要额外鉴权判断**：能进编辑态即已登录，同源 `fetch` 自带会话；会话过期返回 401 时沿用现有路径（`shareCtx.draft` 保住草稿并切登录视图）。图片上传本身不改草稿结构。
- **PDF 导出补等待**：`exportPdf()` 克隆完立即抓 canvas，同源图片可能还没加载完。导出前对 `wrap.querySelectorAll('img')` 等待 `complete` 或 `load`/`error`，带 5 秒总超时兜底，然后再走 `html2pdf()`。`useCORS` 已开启，不改其它参数。

## 4. MCP（`internal/mcp`）

新增工具 `upload_image`，描述里说明「返回可直接粘进 `write_doc` 的 Markdown 片段」。

参数（二选一，都给或都不给返回错误说明）：

| 参数 | 适用 | 说明 |
|---|---|---|
| `source_path` | 仅 stdio | agent 本机文件路径，服务端直接读，避免 base64 膨胀。HTTP 模式下明确报错并提示改用 `data_base64` |
| `data_base64` | HTTP（stdio 也可） | base64 编码的图片内容 |

- 解码后走与 Web 上传**同一个** `store.SaveImage`，不写第二份校验。
- 返回：`![name](/a/{user}/{hash}.ext)` 与图片 URL 两行文本。
- 与 `write_doc` 的关系：agent 先调 `upload_image` 拿到片段，再把片段拼进正文。`write_doc` / `update_doc` 的 schema 不变。
- `source_path` 只在 stdio 下有效，因为 stdio 进程本来就运行在 agent 的机器上、已有文件系统权限；这是一条明确的信任边界，写进工具描述。

## 5. 错误与文案（`internal/i18n`）

新增键，中英各一份：

- `KeyImageTooLarge`：图片超过 5 MB 上限
- `KeyUnsupportedImage`：只支持 PNG / JPEG / GIF / WebP（SVG 单独一句更好，若合并成同一句也接受）
- `KeyImageUploadFailed`：前端批量上传的失败汇总

同时把 `KeyQuotaExceeded` 的措辞从「文档总量」改成能涵盖图片的说法（例如「文档与图片总量」），中英同步。`api.storeErrStatus` 新增两条映射：`ErrImageTooLarge → 413`、`ErrUnsupportedImage → 415`。

## 6. 测试

- `internal/store`：同内容两次 `SaveImage` 只落一份且返回同名；配额把图片算进 `usedBytes`；超 `MaxImageBytes` 被拒；`OpenImage` 对 `../`、大写 hex、非法扩展名一律按不存在返回。
- `internal/api`：`POST /api/assets` 的 200 / 413 / 415（含 SVG）；未登录 401；`GET /a/...` 匿名 200 且带 `immutable` 缓存头；未知文件名 404；分享页引用图片可读。
- `internal/render`：`![](/a/alice/3f2a....png)` 渲染后 `<img src>` 保留（sanitizer 回归）。
- `internal/mcp`：`data_base64` 成功返回片段；HTTP 模式传 `source_path` 返回明确错误；两个都给报错。
- 前端无自动化测试框架，粘贴/拖拽/插入位置与 PDF 含图导出由作者在浏览器验收。

## 7. 文档更新

- `docs/PRD.md`：新增功能条目（图片库 / 粘贴上传 / `upload_image`），变更记录加 v0.6；§9 的「数据不锁定」补一句图片 URL 指向本站的连带影响。
- `README.md` / `README-ZH.md`：能力列表补图片；数据目录说明加 `assets/`。
- `CODEBUDDY.md` / `CLAUDE.md`：数据布局加 `assets/`；架构要点补「`/a/` 匿名、不进 `withAuth`」与配额含图片。

## 8. 已知限制

- 图片 URL 指向本站：把 `.md` 下载到本地或克隆到别处后，图片仍可访问（因为是公开地址），但离线/换站即裂。不做 zip 导出。
- 孤儿图不回收，会长期占用配额；真到需要时再做一次「扫描正文引用 → 删除未引用文件」的离线脚本，不影响本设计。
- 取消分享不回收图片访问权（按已确认的「不可猜即公开」语义）。
