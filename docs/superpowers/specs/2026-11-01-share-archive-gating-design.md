# 归档即暂停分享 — 设计

日期：2026-11-01
状态：已确认，待实施

## 背景与动机

现状：分享只看 `share_mode`，不看文档状态。

- `sharedDoc`（`internal/api/share.go:57`）只校验签名与 `d.Shared`，不看 `Status` → 已归档文档的只读链接、下载、clone 全部照常 200。
- `editableDoc`（`share.go:118`）同样不看 `Status` → **持有 edit 链接的登录用户仍可改写一篇已归档的文档**，内容写进 `archive/{id}.md`，作者在活动列表里看不到。
- `Archive` 重写 frontmatter 时保留 `share:` 字段（`internal/store/store.go:220`），所以归档后分享状态一直在。

「收起了文档却开着入口」与归档的语义冲突（MCP 工具描述：`Move a document to the archive … Good for outdated content`），而 edit 链接能写进归档区是明显的怪状态。

## 目标

- 文档处于 `archived` 时，分享链接**视为失效**：匿名读、下载、clone、可编辑写入四条路径统一不可用。
- 恢复（`Unarchive`）后链接**自动复效**，无需重新开启分享、无需重发链接。
- 让作者在归档前就知道会发生什么。

## 非目标

- ❌ 不改 `share_mode` 字段：归档不动分享开关，恢复后原模式原样生效。
- ❌ 不在归档时自动把 `share` 置 `off`（那是不可逆的副作用）。
- ❌ 不给匿名访客单独的「已归档」提示，也不区分「未分享」与「已归档」——统一沿用 `KeyShareGone` 的 404。
- ❌ 不做「归档前自动询问是否关闭分享」的二次流程。

## 决策摘要

| 项 | 结论 |
|---|---|
| 判定位置 | `sharedDoc` 与 `editableDoc` 各加一个 `Status` 判断，覆盖全部四条路径 |
| 响应 | 统一 404 + 既有 `i18n.KeyShareGone`，不新增错误文案 |
| 恢复 | 零代码：签名确定性 + `share:` 保留，`Status` 回 `active` 即复效 |
| 作者提示 | 归档确认框补一句；分享弹窗在文档已归档时提示链接暂不可访问 |
| 测试 | `internal/api` 一条行为测试，覆盖归档前 200 / 归档后四路 404 / 恢复后 200 |

## 1. 后端

`internal/api/share.go`：

```go
// sharedDoc 里
if err != nil || !d.Shared || d.Status == store.StatusArchived {
	return nil, false
}
```

`editableDoc` 同样在取到文档后补 `d.Status == store.StatusArchived` 的判定（与它现有的 `share_mode != edit` 判断并列）。

注意：store 里归档状态目前是字符串字面量 `"archived"`（`store.go:155`、`Archive`），没有导出常量。实施时任选其一——沿用字面量 `"archived"`，或在 `internal/store` 增加 `const StatusArchived = "archived"` 并替换 `Archive`/`loadFile` 里的字面量。**推荐加常量**（本仓库已有 `ShareOff/ShareView/ShareEdit"` 的先例），但改动只允许触及这一处语义，不顺手重构其它字面量。

## 2. 语义细节

- 归档 → 链接 404，与「分享已关闭」对访客完全一致。
- 恢复 → 链接立即 200，且 `share_mode` 与归档前相同（`edit` 仍是 `edit`）。
- 已归档文档在 Web 端仍可打开、可编辑、可在分享弹窗里改模式——只是链接当前不可访问。这样作者可以先归档再决定是否关闭分享，不需要来回操作。

## 3. 前端提示

- `doc.archive_confirm`（`web/i18n.js`）中文改为「归档后可在左侧「归档」中找回；分享链接会暂时失效，恢复后自动恢复。确定归档？」，英文对应改写。
- `updateShareModeHint()`（`web/app.js:1005`）在 `state.current.status === 'archived'` 且模式非 `off` 时，在提示语后追加 `share.archived_hint`：
  - 中文：`文档已归档，链接暂时不可访问；恢复后自动恢复。`
  - 英文：`This document is archived, so the link is unavailable until you restore it.`
- 不改分享弹窗的结构（复用现有的 `#share-mode-hint`）。

## 4. 测试

`internal/api/share_test.go` 新增 `TestShareGoneWhenArchived`：

1. 建文档 → `share_mode = edit` → 生成签名 → **GET `/api/share/alice/{id}/{sig}` = 200**
2. `st.Archive(id)` →
   - GET 分享读 = 404
   - GET `/api/share/.../download` = 404
   - POST `/api/clone/alice/{id}/{sig}`（带 bob 的 token）= 404
   - PUT `/api/share/alice/{id}/{sig}`（带 bob 的 token + `{"content":"x"}`）= 404
3. `st.Unarchive(id)` → GET 分享读 = 200，且正文未被那次失败的写入改动

## 5. 文档

- `docs/PRD.md` 分享章节补一句规则：归档期间分享链接失效，恢复自动复效。
- `CODEBUDDY.md` 的 Sharing 段补一句。
- 已知取舍：归档会暂时中断已经发给外部的链接；这是有意为之（归档意味着这篇内容不该再被主动消费），但确认框必须告知。

## 6. 验收

- `go test ./internal/api -run TestShareGoneWhenArchived -v` 通过
- 浏览器：分享一篇 → 归档 → 链接 404 → 分享弹窗显示「已归档」提示 → 恢复 → 链接恢复可读，模式未变

## 已知代价

- 归档会**静默打断**已发出去的外部链接（访客侧就是 404）。缓解手段只有归档确认框里那句话——作者已确认接受。
- 与「分享状态与生命周期正交」的干净模型相比，这是刻意引入的耦合，理由写在「背景与动机」里。
