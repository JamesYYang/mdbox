# mdbox

[English](../README.md) · 中文

一个自托管的 **Markdown 文档管理工具**：agent 产出的文档可通过 **MCP** 直接写入，人在任意浏览器里阅读、编辑、归类、打标签、上传、下载、分享；人和 agent 共用同一份存储。

设计原则：**文档永远是纯 `.md` 文件 + YAML frontmatter，工具只是它的一个视图。** 存储层不用数据库，直接落在磁盘目录，git 负责版本与异地备份。

## 功能

- **Web UI**
  - **登录 / 注册**：多用户，用户名自助注册（可在 `config.yaml` 用 `allow_register: false` 关闭）；每人的文档彼此隔离
  - **中英双语界面**：英文 / 中文，顶栏、登录页、分享页均可一键切换，默认跟随浏览器语言
  - **顶栏**：产品名 + Logo、MCP 接入（弹窗给出**你自己的** token 与接入配置，可重置）、浅色/深色主题切换、账号菜单（修改密码 / 退出）
  - **左侧栏**：分类 / 标签 / 归档，一键筛选
  - **卡片列表**：全文搜索、悬停显示删除按钮（二次确认后彻底删除）
  - **列表概览**：文档页顶部一条统计条，展示近 7 天的新增 / 更新 / 归档 / Agent 写入
  - **阅读与编辑**：默认**只读预览**，点「编辑」进入左源码右实时预览的双栏模式，可改标题/分类/标签/归档
  - **上传 / 下载**：拖拽或选择多个 `.md`；预览模式下可下载 Markdown 或**导出 PDF**
- **分享**：每篇文档可手动开启分享，生成**固定算法的稳定链接**，任何人凭链接免登录只读查看，可随时关闭撤销；登录用户可「复制到我的库」，把文档复制一份到自己库里
- **REST API**：登录/注册、文档 CRUD、搜索、标签/分类聚合、近 7 天概览、multipart 上传、Markdown 渲染、分享与 clone
- **MCP Server**：`list_docs` `search_docs` `read_doc` `write_doc` `update_doc` `list_tags` `archive_doc`，支持 HTTP（Streamable）与 stdio 两种传输
- **鉴权**：Web 用登录会话（Cookie），agent/脚本用**每个用户各自的 token**，两者都只能访问该用户自己的文档
- **git 自动备份**：数据目录本身是 git 仓库，定时提交并推送远端
- **零前端依赖**：预览渲染在服务端（goldmark），不依赖任何 CDN；唯一打包的第三方 JS 是本地内置的 html2pdf（用于 PDF 导出）

## 快速开始

构建要求：Go 1.23+

```bash
go build -o mdbox .
./mdbox -addr :8080 -data ./data
# 浏览器打开 http://<server>:8080，注册第一个用户即可使用（没有预置账号）
```

首次启动会在当前目录生成 **`config.yaml`** 与 **`users.yaml`**（均已在 `.gitignore` 中）：

```yaml
# config.yaml
secret: "<随机生成：分享链接签名，改动会让所有已分享链接失效>"
allow_register: true              # false 则关闭自助注册
```

- 没有预置/默认账号。若把 `allow_register` 设为 `false`，需先在开放注册时创建好用户，否则无人能登录；
- `users.yaml` 是用户注册表（不用数据库）：用户名、**bcrypt 密码哈希**、该用户的 MCP token、创建时间。它放在 `data/` 之外，不会被 git 备份带走；
- 用 `-config` / `-users` 指定两个文件的路径；模板见 `config.example.yaml`。

## 鉴权

两套机制，最终都解析成「某个用户」，之后只能读写 `data/users/<username>/` 下的文档：

- **Web 端**：用户名 / 密码登录（`POST /api/login`）或注册（`POST /api/register`，成功后直接登录），下发内存会话 Cookie `mdbox_session`。会话存在内存，**进程重启后需重新登录**。
- **agent / 脚本**：每个用户注册时自动生成一个 token，请求头 `Authorization: Bearer <token>`；`/api/*` 与 `/mcp` 都认它。token 可在 Web 的「MCP 接入」弹窗查看、重置（重置后旧 token 立即失效）。

用户名规则：小写字母、数字、`_`、`-`，3-32 位；密码 8-72 位。同一 IP 每小时最多注册 5 次。

## 分享

在文档预览模式点「分享」即可开启。链接形如：

```
https://<host>/s/<user>/<id>/<token>    # token = HMAC-SHA256(secret, "<user>/<id>")[:24]
```

链接末段（`token`）由 `secret` 用固定算法生成，所以同一篇文档每次都是**同一个地址**；更改 `secret` 会让所有已分享链接立即失效，关闭分享则该链接返回 404。文档可以分享为**只读**（`view`）或**可编辑**（`edit`）：可编辑链接允许已登录的访问者就地改写正文，标题、标签、分类不变。

已登录的访问者可以点「复制到我的库」，把文档复制一份到自己的库里（只带标题 + 正文，`source=clone`），源文档不受影响。这是**单向拷贝**，不是多人共编。

## MCP 接入

HTTP 模式（远程 agent，如 CodeBuddy / Claude 等）。token 是你自己账号的，在 Web 顶栏「MCP 接入」里查看；该 agent 只能访问你自己的文档：

```json
{
  "mcpServers": {
    "mdbox": {
      "url": "http://your-server:8080/mcp",
      "headers": { "Authorization": "Bearer <你的 token>" }
    }
  }
}
```

stdio 模式（本机 agent 直连，无需令牌，用 `-user` 指定为哪个用户服务，该用户须已注册）：

```json
{
  "mcpServers": {
    "mdbox": {
      "command": "/path/to/mdbox",
      "args": ["-stdio", "-user", "alice", "-data", "/path/to/data"]
    }
  }
}
```

## REST API 一览

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/login` | `{username,password}` 登录，下发会话 Cookie |
| POST | `/api/register` | `{username,password}` 注册并登录，生成该用户的 token（可被 `allow_register` 关闭） |
| POST | `/api/logout` | 退出登录 |
| GET | `/api/me` | 当前用户与其 token |
| POST | `/api/me/token` | 重置当前用户的 token |
| POST | `/api/me/password` | 修改当前用户密码，body `{"old","new"}`；成功后其他登录会话失效 |
| GET | `/api/health` | 健康检查 |
| GET | `/api/overview?days=7` | 近 N 天的新增 / 更新 / 归档 / Agent 写入计数（1–90 天） |
| GET | `/api/docs?tag=&category=&status=&q=&limit=` | 列表 / 搜索 |
| POST | `/api/docs` | 创建 `{title, content, tags, category, source}` |
| GET | `/api/docs/{id}` | 读取（含正文与渲染 HTML；已分享时附带 shareUrl） |
| PUT | `/api/docs/{id}` | 更新 `{title?, content?, tags?, category?}` |
| POST | `/api/docs/{id}/archive` | 归档（移入 archive，软删除） |
| DELETE | `/api/docs/{id}` | 彻底删除（不可恢复） |
| POST | `/api/docs/{id}/share` | `{mode}`（`off` / `view` / `edit`）设置分享，返回 `{mode, url}`（`off` 时无 `url`） |
| GET | `/api/docs/{id}/download` | 下载 .md |
| GET | `/api/share/{user}/{id}/{token}` | **匿名**只读读取（分享） |
| GET | `/api/share/{user}/{id}/{token}/download` | **匿名**下载 .md（分享） |
| POST | `/api/clone/{user}/{id}/{token}` | **需登录**：把已分享文档复制到自己的库（只带标题 + 正文，`source=clone`） |
| GET | `/api/tags` · `/api/categories` | 标签 / 分类聚合 |
| POST | `/api/upload` | multipart 上传多个 .md（仅 UTF-8 文本，单篇 ≤2MB） |
| POST | `/api/preview` | `{content}` → `{html}` |
| POST | `/mcp` | MCP Streamable HTTP 端点（仅用户 token，只暴露该用户自己的文档） |

## 数据与备份

```
data/users/<username>/
├── docs/*.md      活跃文档
└── archive/*.md   已归档文档
```

每个用户一个目录，整个 `data/` 仍是一个 git 仓库，备份方式不变。

初始化远端并加入 crontab：

```bash
cd data && git init && git remote add origin git@github.com:<you>/<repo>.git
crontab: */10 * * * * /path/to/mdbox/scripts/git-backup.sh /path/to/mdbox/data
```

> 部署提示：mdbox 是**有状态、单实例**服务（内存索引 + 内存会话 + 磁盘文件），需要挂一块持久磁盘当 `data/`。文档**不适合**直接放在对象存储（S3/OSS）上——它是普通文件系统 I/O。

在 Azure 上的完整部署步骤（GitHub Actions 构建 → ACR → Container Apps + Azure Files 持久卷）见 [`DEPLOY.md`](DEPLOY.md)。

## 目录结构

```
main.go                入口（HTTP 服务 / stdio 模式）、路由、embed web/
internal/store/        存储层：frontmatter 解析、内存索引、CRUD
internal/render/       Markdown → HTML（goldmark）
internal/api/          REST API 与鉴权
internal/mcp/          MCP 工具定义
internal/config/       config.yaml（分享 secret / 是否开放注册；无预置账号）
internal/users/        用户注册表 users.yaml、密码哈希、token、按用户的 Store
internal/auth/         内存登录会话
web/                   前端（embed 进二进制，部署单文件）
web/vendor/            本地内置的 html2pdf（PDF 导出用）
scripts/git-backup.sh  数据目录 git 自动备份
```

## 已知取舍

- 元数据索引在内存中启动时全量重建，**外部直接增删 data 目录文件后，服务会在下次请求时自动重建**；万篇以内无压力
- 登录会话存在内存，进程重启后所有人需重新登录；请以**单实例**运行
- 多用户只做到「文档隔离」：没有角色/权限、没有用户间共享、没有找回密码流程（修改密码需校验旧密码；忘记密码只能人工处理）
- 预览渲染允许内联 HTML（`WithUnsafe`），但输出会经白名单清洗（bluemonday UGC）：`<script>`、内联事件属性、`<iframe>`、`javascript:` 链接等一律剔除，`<details>`、`<br>`、表格这类安全 HTML 保留
- 归档是软删除；**删除则是真删**，不可恢复
- 写入有上限：正文必须是 UTF-8 文本（把二进制改名成 `.md` 会被拒，回 415），单篇 2MB、每人 200MB（归档也算）、单次请求 32MB；阈值是 `internal/store/store.go` 里的常量，改完记得同步 i18n 文案
- PDF 由浏览器端 html2pdf 把整页截成图片再切片分页，超长代码块或超大表格仍可能有分页瑕疵
