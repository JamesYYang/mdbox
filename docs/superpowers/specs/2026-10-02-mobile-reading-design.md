# 移动端阅读体验优化 — 设计

日期：2026-10-02
状态：已定稿，待实施

## 背景与问题

在手机浏览器上打开 mdbox，可读性很差。当前前端只有：

1. `<meta name="viewport" content="width=device-width, initial-scale=1">`
2. 一个 `@media (max-width: 720px)` 块（`web/style.css:280`），只做了两件事：把侧栏从 232px 收窄到 170px、把编辑区的源码/预览从左右并排改成上下堆叠。

也就是说，除编辑区外没有任何真正的窄屏适配。具体问题：

| 位置 | 问题 |
| --- | --- |
| `.topbar`（`web/style.css:86`） | 品牌 + MCP + 主题 + 头像 + 用户名 + 修改密码 + 退出挤在一行，`flex-wrap` 未设置，窄屏必然溢出 |
| `.sidebar`（`web/style.css:114`） | 常驻占宽。375px 屏上只剩约 205px 给正文 |
| `.listbar`（`web/style.css:140`） | 搜索框 + `新建` + `上传 .md` 一行放不下 |
| `.pane-preview`（`web/style.css:185`） | 左右各 32px 内边距，正文有效宽度被进一步压缩 |
| `body`（`web/style.css:45`） | 字号 15px / 行高 1.6，手机上偏小偏挤 |
| `.markdown table`（`web/style.css:273`） | 无横向滚动容器，宽表格会撑破页面 |
| 按钮 | 普通按钮高约 32px、`.icon-btn` 34px、`.card-del` 26px，低于 44px 的推荐触控尺寸 |
| `#app`（`web/style.css:85`） | `height: 100vh`，iOS Safari 上会被地址栏遮挡底部内容 |
| `<meta viewport>` | 缺 `viewport-fit=cover`，`env(safe-area-inset-*)` 不生效 |

## 目标与非目标

### 目标

- 手机浏览器上的**阅读体验**达到正常可读水平：正文不挤、不横向溢出、触控不误点。
- 分类/标签筛选在手机上仍然可用。
- 桌面端表现**零变化**。

### 非目标（明确不做）

- 不做手机端编辑体验。窄屏下编辑、新建、上传、归档、删除入口全部隐藏。
- 不做底部标签栏、手势滑动返回、字号调节控件。
- 不引入任何 CSS 框架或 CDN（沿用项目现有的零依赖取向）。
- 不改任何后端 / REST / MCP 逻辑。

### 前提（已与作者确认）

- 手机上基本只用来**读**。
- 手机上会**按分类/标签筛选**，因此侧栏的分类/标签必须能随时唤出。
- 手机上**隐藏编辑入口**。

## 方案选择

考虑过三个方案：

- **A. 新增窄屏样式层 + 侧栏抽屉**：桌面 CSS/JS 不动，只扩写 `@media (max-width:720px)`，外加约 30 行 JS 管抽屉。**采用此方案。**
- **B. 全端统一响应式重构**：桌面也走同一套弹性规则。视觉更一致，但要改动桌面端当前稳定的代码，回归面大、收益不明显。否决。
- **C. 独立移动端页面（`/m/...`）**：两套 HTML/CSS 分开维护。隔离彻底但代码翻倍，与项目「窄范围」取向相悖。否决。

## 设计

### 1. 断点

沿用 **720px**，与现有媒体查询一致。CSS 全部为**新增**，桌面端现有规则一行不动；现有 `@media (max-width: 720px)` 块整体替换为新的实现。

### 2. DOM 改动

只加元素，不改既有元素的结构：

| 新增元素 | 位置 | 说明 |
| --- | --- | --- |
| `<button id="side-toggle" class="icon-btn">` | `.topbar` 内、`.brand` 之前 | 抽屉开关，桌面端 `display:none` |
| `<div id="side-backdrop" class="side-backdrop">` | `.shell` 之后 | 抽屉遮罩，桌面端 `display:none` |
| `<div class="side-foot">` | `.sidebar` 末尾 | 抽屉底部区，桌面端 `display:none` |

`.side-foot` 内包含：用户名行、`MCP 接入`、`修改密码`、`退出登录` 四个按钮。它们在窄屏下从顶栏隐藏，改为在此处显示。**功能完全复用现有实现**——共用的处理函数（`openMcp` / `openPwd` / 登出）抽成具名函数后绑到两组按钮上，不新增业务逻辑。

窄屏下从顶栏隐藏的既有元素：`.user-name`、`#pwd-btn`、`#logout-btn`、`#mcp-btn`。

### 3. 侧栏抽屉行为

- 窄屏：`.sidebar` 变为 `position: fixed; top: 54px; bottom: 0; width: min(78vw, 300px); overflow: auto`，默认 `transform: translateX(-100%)`。
- `body.nav-open` 时：`.sidebar` → `translateX(0)`，同时显示 `#side-backdrop` 并 `body { overflow: hidden }` 锁滚动。
- 关闭时机：点遮罩、按 Esc、选中任意分类/标签后（`onSideClick` 末尾追加关闭）。
- **复位**：不需要。`body.nav-open` 与 `.sidebar` 的相关规则全部写在 `@media (max-width: 720px)` 内部，窗口变宽时自动失效，桌面端不会被锁滚动。仅在 `resetSession()`（退出/切换账号）时移除 `nav-open`。

### 4. 窄屏隐藏的入口

纯 CSS `display: none`：

- `#btn-new`、`#btn-upload`
- `#p-edit`、`#e-archive`
- `.card-del`（卡片删除按钮）

隐藏删除按钮是刻意的取舍：它 26px 见方、位于卡片右上角、且是不可恢复的永久删除（`web/app.js:429`），误触代价高于「能在手机上删文档」的收益。相关 JS 逻辑保留不动，桌面端行为不变。

### 5. 顶栏与工具条

- 顶栏窄屏内容变为：`☰ MDBox … ☾`。图标按钮提升到 44px 触控尺寸（顶栏 54px 高足够容纳）。
- `#preview-bar` 窄屏改为单行：`← 标题(自动截断) … 分享 下载▾`。标题 `flex: 1; min-width: 0` 并截断。

### 6. 阅读排版

| 项目 | 现在 | 窄屏改为 |
| --- | --- | --- |
| `.markdown` 字号/行高 | 15px / 1.6 | **16px / 1.75** |
| `.pane-preview` 内边距 | `24px 32px` | **`16px 14px`** |
| `.share-inner` 内边距 | `40px 20px` | **`20px 14px`** |
| `.markdown table` | 无滚动容器 | `display: block; overflow-x: auto; max-width: 100%` |
| `.markdown pre` | `overflow: auto` | 不变（已可横向滚动） |

表格的取舍：`display:block` 让表格宽度按内容收缩、不再撑满容器。撑满需要 JS 包一层 `<div>`（约 6 行），先用纯 CSS 这版。

> **选择器必须避开 `.pdf-export`。** PDF 导出的临时包裹层同时带 `pdf-export` 和 `markdown` 两个类（`web/style.css` 末尾），所以窄屏规则**不能写成裸 `.markdown`**，否则手机上导出 PDF 时表格会被加上 `display:block + overflow-x:auto`，html2canvas 按容器边缘裁切，正好重新引入 `CODEBUDDY.md` 里明确警告过的「右侧被静默裁掉」问题。实现时改用「阅读容器」限定：`.pane-preview .markdown` / `.preview` / `.share-inner .markdown`。

### 7. 触控尺寸与安全区

- 窄屏 `button`、`.icon-btn`、`.side-item` 最小高度 **44px**。
- `#app { height: 100vh; height: 100dvh; }`；`.modal-card` 的 `max-height: 88vh` 同样补 `88dvh`。
- `<meta name="viewport">` 补 `viewport-fit=cover`。
- 底部区域加 `padding-bottom: env(safe-area-inset-bottom)`。

### 8. 覆盖的页面

- **列表页 / 文档页**：主体改动。
- **分享页 `/s/...`**：独立于 `#app` 的另一套 `.topbar`，共用 `.markdown` 排版规则；顶栏窄屏隐藏 `.share-brand-tip`。
- **登录页**：已基本自适应（`width: 320px` 且有 `padding: 20px`），仅顺带把输入框和按钮提到 44px。

## 受影响文件

- `web/style.css` — 主要改动，替换并扩写窄屏媒体查询
- `web/index.html` — 新增 `#side-toggle`、`#side-backdrop`、`.side-foot`；viewport meta 补 `viewport-fit=cover`
- `web/app.js` — 抽屉开合、遮罩/Esc/选中后关闭、`matchMedia` 复位、`.side-foot` 按钮事件绑定
- 不改 `main.go`、`internal/` 下任何文件

## 验证方式

1. `go build -o mdbox .` 通过（`web/` 由 `//go:embed` 嵌入，**改完必须重新 build 才生效**）。
2. `gofmt -l .` 无输出（本次不涉及 Go 代码，仅确认无回归）。
3. DevTools 设备模拟 **375×667** 与 **390×844**，逐项走查：
   - 列表页：搜索框铺满一行，无横向滚动条
   - 抽屉：☰ 打开、遮罩关闭、Esc 关闭、选中分类后自动关闭
   - 文档页：工具条单行、表格可横向滚动、代码块可横向滚动
   - 分享弹窗、MCP 弹窗（内容可滚动、代码块可滚动）
   - 分享页 `/s/{user}/{id}/{sig}`
   - 登录页
4. 桌面端 **1440px** 宽度截图对比改动前后，确认零差异。
5. 部署到 Azure 后用真机（iPhone Safari）确认 `dvh` 与 safe-area 生效、地址栏不再遮挡底部。

## 风险

- **仅 CSS 层面的回归风险低**，但 `body.nav-open` 的锁滚动若忘记复位会导致桌面端异常——已用 `matchMedia` 的 `change` 监听覆盖。
- 真机验证依赖部署（本地开发机在 Windows，无法直接用真机访问 localhost），需在 Azure 上重新部署后再验。
