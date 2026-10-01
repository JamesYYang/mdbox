# mdbox

一个自托管的 **Markdown 文档管理工具**：agent 产出的文档自动入库，人在任意浏览器里阅读、编辑、归类、打标签、上传、下载；同时通过 **MCP** 让任意 agent 读写这个库。

设计原则：**文档永远是纯 `.md` 文件 + YAML frontmatter，工具只是它的一个视图。** 存储层不用数据库，直接落在磁盘目录，git 负责版本与异地备份。

## 功能

- **Web UI**：列表 / 标签筛选 / 全文搜索 / 双栏编辑（源码 + 实时预览）/ 上传下载 / 归档
- **REST API**：文档 CRUD、标签聚合、搜索、multipart 上传、Markdown 渲染
- **MCP Server**：`list_docs` `search_docs` `read_doc` `write_doc` `update_doc` `list_tags` `archive_doc`，支持 HTTP（Streamable）与 stdio 两种传输
- **git 自动备份**：数据目录本身是 git 仓库，定时提交并推送远端
- **零前端依赖**：预览渲染在服务端（goldmark），不依赖任何 CDN

## 快速开始

构建要求：Go 1.23+

```bash
go build -o mdbox .
./mdbox -addr :8080 -data ./data
# 浏览器打开 http://<server>:8080
```

生产环境建议开启鉴权：

```bash
MDBOX_TOKEN=$(openssl rand -hex 16) ./mdbox -addr :8080 -data ./data
```

Web 页面首次访问会提示输入令牌，保存在浏览器 localStorage；API 用 `Authorization: Bearer <token>`。

## MCP 接入

HTTP 模式（远程 agent，如 CodeBuddy / Claude 等）：

```json
{
  "mcpServers": {
    "mdbox": {
      "url": "http://your-server:8080/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

stdio 模式（本机 agent 直连）：

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
| GET | `/api/docs?tag=&category=&status=&q=&limit=` | 列表 / 搜索 |
| POST | `/api/docs` | 创建 `{title, content, tags, category, source}` |
| GET | `/api/docs/{id}` | 读取（含正文与渲染 HTML） |
| PUT | `/api/docs/{id}` | 更新 `{title?, content?, tags?, category?}` |
| POST | `/api/docs/{id}/archive` | 归档（移入 archive，不真删） |
| DELETE | `/api/docs/{id}` | 彻底删除 |
| GET | `/api/docs/{id}/download` | 下载 .md |
| GET | `/api/tags` · `/api/categories` | 标签 / 分类聚合 |
| POST | `/api/upload` | multipart 上传多个 .md |
| POST | `/api/preview` | `{content}` → `{html}` |
| POST | `/mcp` | MCP Streamable HTTP 端点 |

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

## 目录结构

```
main.go              入口（HTTP 服务 / stdio 模式）
internal/store/      存储层：frontmatter 解析、内存索引、CRUD
internal/render/     Markdown → HTML（goldmark）
internal/api/        REST API
internal/mcp/        MCP 工具定义
web/                 前端（embed 进二进制，部署单文件）
scripts/git-backup.sh  数据目录 git 自动备份
```

## 已知取舍（MVP）

- 元数据索引在内存中启动时全量重建，**外部直接增删 data 目录文件后，服务会在下次请求时自动重建**；万篇以内无压力
- 预览渲染允许内联 HTML（`WithUnsafe`），仅建议个人或可信团队使用
- 归档是软删除；真正的「过期候选清单 + 自动清理」计划在下一阶段
