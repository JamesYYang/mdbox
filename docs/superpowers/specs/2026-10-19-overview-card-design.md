# 列表页「近 7 天」产出概览 — 设计

日期：2026-10-19
状态：已确认（作者选定方案 A）

## 背景与动机

列表页目前只有「N 篇」这一行静态计数，用户看不到自己的库最近有没有在长。GitHub 贡献热力图被评估过，但样本量（几个用户、每周个位数文档）撑不起逐日格子，且文档多由 agent 写入，streak 的心理学不成立（见 2026-10-19 讨论）。

改为一行**聚合概览**：新增 / 更新 / 归档 / Agent 写入。同样的数据，信息量更大，还能直接观察「agent 是否真的在写」。

## 目标

- 列表页搜索栏下方展示一行：`近 7 天 · 新增 5 · 更新 3 · 归档 2 · Agent 写入 4`
- 中英双语，随语言切换即时重渲染
- 口径明确、可测试；不读正文、不扫盘

## 非目标

- ❌ 不做 streak / 连续天数 / 打卡提醒
- ❌ 不做逐日数组、图表、热力图
- ❌ 不做跨用户对比、排行、通知
- ❌ 不引入新的存储字段

## 决策摘要

| 项 | 结论 |
|---|---|
| 窗口 | 滚动 `now - N 天`（默认 7），不做自然周，避免周一归零与时区边界 |
| 位置 | `#view-list` 内、`.listbar` 与 `.list-head` 之间，纯文本一行 |
| 全 0 | 整行隐藏（新用户不看到一排零） |
| 接口 | `GET /api/overview?days=7`，需登录，`days` 限制 1–90 |
| 实现位置 | Store 内存索引上聚合；不读文件、不改 frontmatter |
| 前端 | 一次请求，结果缓存到 `state.overview`，切语言只重渲染不重请求 |

## 统计口径

- **新增**：`created >= since`（含窗口内新建后又归档的）
- **更新**：`status == "active"` 且 `created < since <= updated`（不重复计新增）
- **归档**：`status == "archived"` 且 `updated >= since`（`Archive` 会刷新 `updated`，见 `store.go:359`）
- **Agent 写入**：上述「新增」里 `source` 不属于 `web` / `manual` / `upload` / `clone` / 空（MCP 固定写 `agent`，见 `mcp.go:87`）

边界用闭区间：时间恰好等于 `since` 计入窗口。

## 接口

```
GET /api/overview?days=7
Authorization: Bearer <token> 或会话 Cookie

200 {"days":7,"new":5,"updated":3,"archived":2,"agent":4}
```

- `days` 缺省 7；非法值回退 7；小于 1 取 1，大于 90 取 90。
- 未登录 401（与其它 `/api/docs` 一致）。

## 前端

- `web/index.html`：`<div id="overview" class="overview" hidden></div>`
- `web/app.js`：`loadList()` 顺带调用 `loadOverview()`（不阻塞列表）；`renderOverview()` 读取 `state.overview`；`applyLang()` 在列表态调用 `renderOverview()`。
- 文案键：`overview.range/new/updated/archived/agent`，中英各一套。
- `agent === 0` 时省略该段；三项业务计数全 0 时整行 `hidden`。
- CSS：`.overview { font-size: 13px; color: var(--muted); margin-bottom: 8px; line-height: 1.6; }`，窄屏自然换行。

## 错误处理与降级

- 概览请求失败：静默隐藏，不影响列表（列表本身已有错误处理）。
- 时间戳来自 frontmatter，外部手改文件可能造成偏差；这是既有数据模型的事实，不在本功能内修正。

## 测试

- `store`：新增 / 更新（创建后改时间窗口）/ 归档 / source 分组 / 窗口外为 0。
- `api`：带 token 200 且字段完整；无凭据 401；`days` 越界被 clamp。
- 手动：列表页出现一行；切英文变成 `Last 7 days`；全部为 0 时隐藏。
