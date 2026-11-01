# 归档与删除入口统一 — 设计

日期：2026-11-01
状态：待评审

## 背景与动机

当前「归档」（软删除）与「删除」（真删）是两个性质和后果都接近的动作，但入口散布在两个界面：

| 动作 | 入口 | 代码位置 |
|---|---|---|
| 删除 | 列表卡片右上角 hover 出现的垃圾桶 | `web/app.js:583`、`web/style.css:185` |
| 归档 | 仅编辑态工具条 | `web/index.html:205`、`web/app.js:835` |

由此产生三个问题：

1. **同族动作分散**：清理一篇文档时，若想「先归档看看」，必须先进编辑态；若想真删，则要在列表上 hover。两套心智。
2. **危险等级与可见度倒挂**：软删除（可挽回）藏得更深，硬删除（不可恢复）反而一步可达。
3. **归档是单行道**：`store` 只有 `Archive`，没有 `Unarchive`；进入「已归档文档」视图后只能删，无法恢复。误归档等于只能真删重写。

## 目标

- 归档 / 删除 / 恢复收敛成**同一组动作**，在列表卡片和文档页两处以相同形态出现。
- 归档不再是单向操作：新增恢复能力，store 先行，REST 与 MCP 同构。
- 不改变现有权限、配额、体积上限与文件即真相的架构不变量。

## 非目标

- ❌ 不做批量选择 / 批量归档 / 批量删除。
- ❌ 不做回收站、撤销 toast、延迟删除（恢复 <=> 从归档列表捞回，就够了）。
- ❌ 不把分享、下载塞进这个菜单（它们留在文档页工具条，菜单只装「危险动作」）。
- ❌ 不改窄屏（<= 720px）的只读策略：移动端继续不出现任何编辑/归档/删除入口。
- ❌ 不改归档的语义：文件仍在 `data/users/{user}/archive/`，仍计入配额。

## 决策摘要

| 项 | 结论 |
|---|---|
| 入口形态 | 纯图标「⋯」按钮 + 下拉菜单，复用现有 `.dropdown / .dropdown-menu` |
| 列表卡片 | 现有垃圾桶按钮改为「⋯」菜单，位置/悬停规则不变 |
| 文档页 | 预览栏加「⋯」菜单；**编辑态不再有归档按钮** |
| 菜单内容 | 仅两项：归档/恢复、删除；删除用 danger 色 |
| 归档视图 | 同一菜单自动变「恢复 / 删除」 |
| 恢复能力 | 新增 `store.Unarchive`、`POST /api/docs/{id}/unarchive`、MCP `unarchive_doc` |
| 完成后行为 | 归档/恢复成功后回列表并刷新（沿用现有 `archiveDoc` 的行为） |

## 1. 交互细节

### 入口与菜单项

菜单项由文档当前状态决定，三处入口共用同一映射：

| 当前视图 / 文档状态 | 菜单项 |
|---|---|
| 活动列表、活动文档的预览栏 | 归档 · 删除 |
| 已归档视图、已归档文档的预览栏 | 恢复 · 删除 |

- 「⋯」按钮：`aria-label` / `title` 用同一个 key `action.more_aria`，带 `aria-haspopup="menu"`。
- 菜单结构沿用 `index.html` 中 `#p-download` 的 `.dropdown > button + .dropdown-menu > button`。
- 删除项加 `danger` 类（贴近现有 `uiConfirm({danger: true})` 的视觉语言），归档/恢复为默认样式。
- 列表卡片的「⋯」在 `hover` / `:focus-visible` 时出现；触屏（`@media (hover:none)`）常显；窄屏（<= 720px）`display:none`。这条链与现在 `.card-del` 完全一致，只改类名。

### 点击与键盘

- 卡片内「⋯」与菜单项都必须 `stopPropagation()`，否则冒泡触发 `card.onclick → openDoc`。
- 打开菜单时点击空白处收起：扩展现有 document 级点击处理（`app.js:1196` 附近），关掉所有打开的 `.card-menu` / `#doc-menu`。
- `Escape` 收起菜单，沿用现有 `window.keydown` 分支（`app.js:1201`）。
- 一次只允许一个菜单打开（打开新的先关旧的）。
- 菜单项点击后先收起菜单，再执行动作。

### 编辑态

编辑工具条删除 `#e-archive` 按钮及其监听（`app.js:1160`）。理由：归档与未保存草稿会互相干扰（现在归档会把磁盘上的旧内容搬走，草稿丢失）。要归档必须先退出编辑态（取消或保存），此时处于预览栏，菜单位置显而易见。

### 完成后的跳转

沿用 `archiveDoc` 现有行为：成功后 `state.current = null` + `refresh()` 回列表。恢复同样处理——用户会看到文档从「已归档」消失、在活动列表出现（overview 数字同步变化），反馈明确。

## 2. 后端改动

### store

```go
// Unarchive 把文档从 archive 移回 docs（Archive 的逆操作）。
func (s *Store) Unarchive(id string) (*Doc, error)
```

- 与 `Archive`（`internal/store/store.go:381`）对称：读正文 → `Status = "active"` → `Updated = now` → 写到 `docs/{id}.md` → 删除 `archive/{id}.md` → 更新 `e.path`。
- 幂等：文档已是 `active` 且文件已在 `docs/` 时直接返回，不写盘、不改 `Updated`。
- 未知 id 返回 `ErrNotFound`。
- 不加冲突检测：ID 在索引里唯一，`docs/{id}.md` 已存在属病态情况，按覆盖处理（与 `Archive` 的写行为一致）。

### api

`internal/api/server.go:65` 旁新增：

```go
mux.HandleFunc("POST /api/docs/{id}/unarchive", s.unarchiveDoc)
```

handler 与 `archiveDoc` 同构（`internal/api/api.go:181`），错误一律 404。不需要动 `publicPath` 与分享路径。

### mcp

在 `internal/mcp/mcp.go:131` 的 `archive_doc` 旁新增 `unarchive_doc`，描述对称（"Move a document out of the archive back to active"），同样走 `st.Unarchive`。

## 3. 前端改动清单

| 文件 | 改动 |
|---|---|
| `web/index.html` | 预览栏加 `#doc-more`（⋯ + `#doc-menu`）；删除 `#e-archive` |
| `web/app.js` | `renderList` 中 `.card-del` 换为 `.card-more` + 菜单；新增 `docMenuItems()` 决定菜单项、`openCardMenu()`、`closeMenus()`；新增 `unarchiveDoc()`；`archiveDoc` / `deleteDoc` 改为被菜单调用（`deleteDoc` 增加归档视图下的调用方）；删除 `e-archive` 监听 |
| `web/style.css` | `.card-del` 更名 `.card-more`（含 `@media (hover:none)` 与窄屏 `display:none` 两条引用）；卡片内菜单的 `position: absolute` / `z-index` / `min-width`；菜单项 danger 色 |
| `web/i18n.js` | 新增 6 条（中英各一），含原先只写进编辑态消息栏的 `doc.archive_failed` |

前端所有 DOM 仍由 `renderList` 动态生成，菜单不做抽象组件，两处各自拼装、共用同一组动作函数。

## 4. 文案（i18n）

| key | 中文 | English |
|---|---|---|
| `action.more_aria` | 更多操作 | More actions |
| `action.restore` | 恢复 | Restore |
| `doc.archive_failed` | 归档失败：{msg} | Archive failed: {msg} |
| `doc.restore_title` | 恢复文档 | Restore document |
| `doc.restore_confirm` | 恢复到活动文档？ | Move back to active documents? |
| `doc.restore_failed` | 恢复失败：{msg} | Restore failed: {msg} |

## 5. 测试与验收

自动化：

- `internal/store` 单测：`Archive` → `Unarchive` 往返后，文件在 `docs/`、`archive/` 下均无残留、`Status == "active"`、`Updated` 已变；对已活动的文档调用 `Unarchive` 为幂等无副作用；未知 id 返回 `ErrNotFound`。
- `internal/api`：现有测试未覆盖 archive 路由，本次不为 unarchive 单独加路由测试，与现状保持一致；路由正确性由浏览器验收第 2、4 条覆盖。
- 门禁：`go build -o mdbox .`、`go test ./...`、`gofmt -l .`。

浏览器验收（作者本人）：

1. 活动列表 hover 卡片 → 「⋯」→ 归档 → 文档从列表消失、出现在「已归档文档」。
2. 已归档视图 → 「⋯」→ 恢复 → 回到活动列表，overview 数字变化。
3. 打开活动文档 → 预览栏「⋯」→ 删除 → 确认框 → 回列表且文档消失。
4. 打开已归档文档 → 预览栏菜单显示「恢复 / 删除」。
5. 编辑态确认没有归档按钮，且编辑中无法归档。
6. 菜单：点空白处收起、`Escape` 收起、点菜单项不会误打开文档、窄屏无任何入口。
7. 中英切换后三处菜单文案正确。

## 6. 已知代价与风险

- **卡片内弹层的层叠**：`.card` 与 `.list` 若存在 `overflow` 裁剪或 `z-index` 上下文，菜单可能被相邻卡片遮住。实现时优先用 `position:absolute` + 足够 `z-index`；若网格容器裁剪，则在打开菜单时为卡片加临时高层级。这是本方案最需要通过浏览器验收确认的一点。
- **两处 DOM 重复**：列表菜单每次 `renderList` 重建（随卡片走），文档页菜单是静态 DOM。两份结构、同一组动作函数，接受这点重复，不为此抽组件。
- **归档不再出现在编辑态**：习惯「边改边归档」的用户多一步（退出编辑）。作者已确认接受。
- **恢复不计入任何审计**：与归档一样只改 `updated`，不记录来源。符合现有「无版本历史」的取舍。
