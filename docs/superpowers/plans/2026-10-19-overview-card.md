# 列表页「近 7 天」产出概览 — 实现计划

日期：2026-10-19
设计：docs/superpowers/specs/2026-10-19-overview-card-design.md

## 任务

- [x] 1. `internal/store`：新增 `Overview(since time.Time) Overview`，在内存索引上聚合新增/更新/归档/agent；补单测。
- [x] 2. `internal/api`：新增 `GET /api/overview` handler，clamp `days` 1–90；补 200 / 401 测试。
- [x] 3. `web/`：index.html 加 `#overview`；app.js 加 `loadOverview/renderOverview` 并接入 `loadList` 与 `applyLang`；style.css 加样式；i18n.js 加中英文案。
- [x] 4. PRD：§4.2 增 V4-6、§6.3 增接口行、§12 变更记录；版本 0.4.2。
- [x] 5. 验收：`go build ./...`、`go test ./...`、`gofmt` 检查；手动核对中英与全 0 隐藏。

## 验收记录

- `go build ./...`、`go vet ./...`、`go test ./...` 全部通过（2026-10-19）
- `node --check web/app.js web/i18n.js` 通过
