# mdbox

一个自托管的 **Markdown 文档管理工具**：agent 产出的文档自动入库，人在任意浏览器里阅读、编辑、归类、打标签、上传、下载、分享；同时通过 **MCP** 让任意 agent 读写这个库。

设计原则：**文档永远是纯 `.md` 文件 + YAML frontmatter，工具只是它的一个视图。** 存储层不用数据库，直接落在磁盘目录，git 负责版本与异地备份。

## 功能

- **Web UI**
  - **登录**：预置管理员账号，无注册流程；配置来自 `config.yaml`
  - **顶栏**：产品名 + Logo、MCP 接入（弹窗给出接入配置）、浅色/深色主题切换、用户信息与退出
  - **左侧栏**：分类 / 标签 / 归档，一键筛选
  - **卡片列表**：全文搜索、悬停显示删除按钮（二次确认后彻底删除）
  - **阅读与编辑**：默认**只读预览**，点「编辑」进入左源码右实时预览的双栏模式，可改标题/分类/标签/归档
  - **上传 / 下载**：拖拽或选择多个 `.md`；预览模式下可下载 Markdown 或**导出 PDF**
- **分享**：每篇文档可手动开启分享，生成**固定算法的稳定链接**，任何人凭链接免登录只读查看，可随时关闭撤销
- **REST API**：登录、文档 CRUD、搜索、标签/分类聚合、multipart 上传、Markdown 渲染、分享
- **MCP Server**：`list_docs` `search_docs` `read_doc` `write_doc` `update_doc` `list_tags` `archive_doc`，支持 HTTP（Streamable）与 stdio 两种传输
- **鉴权**：Web 用登录会话（Cookie），agent/脚本用共享令牌，两套相互独立
- **git 自动备份**：数据目录本身是 git 仓库，定时提交并推送远端
- **零前端依赖**：预览渲染在服务端（goldmark），不依赖任何 CDN；唯一打包的第三方 JS 是本地内置的 html2pdf（用于 PDF 导出）

## 快速开始

构建要求：Go 1.23+

```bash
go build -o mdbox .
./mdbox -addr :8080 -data ./data
# 浏览器打开 http://<server>:8080，用 admin / mdbox@111!!! 登录
```

首次启动会在当前目录生成 **`config.yaml`**（已在 `.gitignore` 中）：

```yaml
admin:
  username: admin
  password: "mdbox@111!!!"        # 请及时修改
secret: "<随机生成：分享链接签名，改动会让所有已分享链接失效>"
token:  "<随机生成：agent/脚本访问 /api 与 /mcp 的令牌>"
```

- 用 `-config ./config.yaml` 指定配置路径；
- `-token xxx` 或 `MDBOX_TOKEN=xxx` 可**临时覆盖**配置里的 `token`；
- 模板见 `config.example.yaml`。

## 鉴权

两套独立机制，共用同一个文档仓库：

- **Web 端**：用户名 / 密码登录（`POST /api/login`），下发内存会话 Cookie `mdbox_session`；`/api/*` 接受「有效会话 **或** 有效令牌」。会话存在内存，**进程重启后需重新登录**。
- **agent / 脚本**：单个共享令牌（`config.yaml` 的 `token`，或 `-token` / `MDBOX_TOKEN` 覆盖），请求头 `Authorization: Bearer <token>`。`/mcp` 只认这个令牌。

## 分享

在文档预览模式点「分享」即可开启。链接形如：

```
https://<host>/s/<id>/<sig>      # sig = HMAC-SHA256(secret, id)[:24]
```

`sig` 由 `secret` 用固定算法生成，所以同一篇文档每次都是**同一个地址**；更改 `secret` 会让所有已分享链接立即失效，关闭分享则该链接返回 404。分享页只读，不能编辑。

## MCP 接入

HTTP 模式（远程 agent，如 CodeBuddy / Claude 等）。令牌见 `config.yaml` 的 `token`：

```json
{
  "mcpServers": {
    "mdbox": {
      "url": "http://your-server:8080/mcp",
      "headers": { "Authorization": "Bearer <config.yaml 中的 token>" }
    }
  }
}
```

stdio 模式（本机 agent 直连，无需令牌）：

```json
{
  "mcpServers": {
    "mdbox": {
      "command": "/path/to/mdbox",
      "args": ["-stdio", "-data", "/path/to/data"]
    }
  }
}
```

## REST API 一览

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/login` | `{username,password}` 登录，下发会话 Cookie |
| POST | `/api/logout` | 退出登录 |
| GET | `/api/me` | 当前登录用户 |
| GET | `/api/health` | 健康检查 |
| GET | `/api/docs?tag=&category=&status=&q=&limit=` | 列表 / 搜索 |
| POST | `/api/docs` | 创建 `{title, content, tags, category, source}` |
| GET | `/api/docs/{id}` | 读取（含正文与渲染 HTML；已分享时附带 shareUrl） |
| PUT | `/api/docs/{id}` | 更新 `{title?, content?, tags?, category?}` |
| POST | `/api/docs/{id}/archive` | 归档（移入 archive，软删除） |
| DELETE | `/api/docs/{id}` | 彻底删除（不可恢复） |
| POST | `/api/docs/{id}/share` | `{shared:bool}` 开启/关闭分享，返回固定链接 |
| GET | `/api/docs/{id}/download` | 下载 .md |
| GET | `/api/share/{id}/{sig}` | **匿名**只读读取（分享） |
| GET | `/api/share/{id}/{sig}/download` | **匿名**下载 .md（分享） |
| GET | `/api/tags` · `/api/categories` | 标签 / 分类聚合 |
| POST | `/api/upload` | multipart 上传多个 .md |
| POST | `/api/preview` | `{content}` → `{html}` |
| POST | `/mcp` | MCP Streamable HTTP 端点（仅令牌） |

## 数据与备份

```
data/
├── docs/*.md      活跃文档
└── archive/*.md   已归档文档
```

初始化远端并加入 crontab：

```bash
cd data && git init && git remote add origin git@github.com:<you>/<repo>.git
crontab: */10 * * * * /path/to/mdbox/scripts/git-backup.sh /path/to/mdbox/data
```

> 部署提示：mdbox 是**有状态、单实例**服务（内存索引 + 内存会话 + 磁盘文件），需要挂一块持久磁盘当 `data/`。文档**不适合**直接放在对象存储（S3/OSS）上——它是普通文件系统 I/O。

## 目录结构

```
main.go                入口（HTTP 服务 / stdio 模式）、路由、embed web/
internal/store/        存储层：frontmatter 解析、内存索引、CRUD
internal/render/       Markdown → HTML（goldmark）
internal/api/          REST API 与鉴权
internal/mcp/          MCP 工具定义
internal/config/       config.yaml（管理员账号 / 分享 secret / agent token）
internal/auth/         内存登录会话
web/                   前端（embed 进二进制，部署单文件）
web/vendor/            本地内置的 html2pdf（PDF 导出用）
scripts/git-backup.sh  数据目录 git 自动备份
```

## 已知取舍（MVP）

- 元数据索引在内存中启动时全量重建，**外部直接增删 data 目录文件后，服务会在下次请求时自动重建**；万篇以内无压力
- 登录会话存在内存，进程重启后所有人需重新登录；请以**单实例**运行
- 预览渲染允许内联 HTML（`WithUnsafe`），仅建议个人或可信团队使用
- 归档是软删除；**删除则是真删**，不可恢复
- PDF 由浏览器端 html2pdf 把整页截成图片再切片分页，超长代码块或超大表格仍可能有分页瑕疵
- 「过期候选清单 + 自动清理」计划在下一阶段
