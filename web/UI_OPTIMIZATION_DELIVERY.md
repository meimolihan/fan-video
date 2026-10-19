# fan-video 前端 UI 优化 — 交付说明

用 ui-forge 技能对 `C:\Users\meimo\Desktop\GitHub\fan-video` 做存量 UI 改造的完整交付记录。

---

## 一、选定的方向与理由

技能要求先做**判断层**（认品类、定方向）再查规则库。本项目落定的方向：

**深色优先的影视资料库工作台，安静、信息密度高、键盘可达。**

- 品类是**资料浏览型工具**（影视库管理），不是营销页也不是沉浸式娱乐首页。判定依据：侧栏 + 分区货架 + 网格/列表双视图 + 分页，说明用户是反复来查找、比对、批量操作的人。
- 因此优先级是：**可读性 > 操作反馈 > 键盘可达 > 装饰动效**。这条优先级的实际后果是——本轮**主动放弃了**若干看起来"更精细"的改动（例如给悬停态加更明显的颜色位移、给卡片加更重的投影），因为它们对本场景的收益低于成本。
- 深色优先：项目默认主题就是暗色，且 `--nv-*` 令牌体系以暗色为基准定义、浅色为派生。改造必须顺着这个方向，不能反过来。

---

## 二、查了规则库的哪几条，命中了什么

用 `scripts/search.py` 查了三组，全部指向可执行的硬指标：

| 查询 | 命中规则 | 严重度 | 落到了哪 |
|---|---|---|---|
| `focus visible keyboard navigation ring --domain ux` | 每个可操作控件（含弹窗内）都要有可见焦点环 | High | 批次 2 的键盘等价；实测 69/69 全有焦点环 |
| `touch target size minimum icon only button --domain ux` | WCAG 2.2 AA「Target Size (Minimum)」：Web ≥24×24 CSS px | — | 已有 `@media (pointer: coarse)` 放大到 40px，本轮核对后无需改 |
| `color alone convey meaning status --domain ux` | 「Color Only」不得仅用颜色传达信息；「Color Contrast」正文 ≥4.5:1 | High | 批次 1 + 批次 3 的全部对比度修复 |

**规则库只提供阈值，判断仍是自己做的**：规则说"≥4.5:1"，但没说该把哪一级文字提到哪 —— 那是靠盘点界面层级（哪些文字承载信息、哪些纯装饰）决定的。

---

## 三、改了什么

### 批次 1 · 可读性（对比度 + 字号）

| 改动 | 位置 | 修前实测 | 修后实测 |
|---|---|---|---|
| 暗色三级文字 | `pages-theme.css` `:root:not([data-theme="light"])` | 3.84:1 | 5.00:1 |
| 浅色三级文字 | `app-ui.css` | 2.50:1 | 4.70:1 |
| 暗色主色三态 | `pages-theme.css` | 白字 3.77 / 3.21:1 | 4.53 / 4.58:1 |
| 侧栏分组标题字号 | `app-ui.css:9838`（`!important` 实际生效处） | 9px | 11px |
| 侧栏项文字 | `app-ui.css` ×3 | 11px | 12px |
| 首页 vw 缩放下限 | `app-ui.css` | 1440 视口 7.7px | `max(11px, .538vw)` |
| 登录页页脚说明 | `app-ui.css` | 11px | 12px |

### 批次 2 · 按压反馈 + 键盘等价

盘点发现 `.nv-media-card` / `.nv-rail-item` / `.nv-browse-list-item` 三个组件**有 hover 反馈但完全没有 `:active`** —— 鼠标按下时界面毫无反应，用户无法确认点击是否被接收。已补：

- 三个组件的 `:active` 按压态
- 每条 `:hover` 配套 `:focus-visible` / `:focus-within` 等价规则
- 媒体卡片的按压阴影刻意**比静止态更浅**（`0 4px 12px` vs 静止 `0 10px 24px`）

未动的：`.nv-filter-chip` 已有 `:active`；`.nv-pagination-*` 走 `.nv-button`，已有 `:active`；`.nv-collection-card` 这个类名在 CSS 里根本不存在（盘点时的命名假设有误）。

### 批次 3 · WCAG AA 清零

用同一套对比度算法复检发现的剩余问题：

- `.nv-pagination-summary` 暗色 3.99:1 → 12px + 改走二级文字令牌
- 浅色侧栏选中项 `#6f55e9` on `#f0edff` = 4.40:1 → 压深到 `#5f45d6`（5.03:1）
- **主色令牌误用**：批次 1 把 `--nv-action-primary` 压暗是为了让"主色作**背景**配白字"达标，但"主色作**文字**配深底"是相反需求，导致「查看全部」掉到 4.41:1。改为深底上的紫文字走 `--nv-action-muted`（暗色 `#8795ff` = 8.4:1），浅色用 `#5f45d6`（5.99:1）

---

## 四、哪些没做，为什么

| 没做的事 | 原因 |
|---|---|
| CSS 迁入 `@layer` | 4 个 CSS 文件共 22,121 行、约 5,054 处 `!important`，全部未分层且恒压 Tailwind `utilities`。分层会一次性重排全局优先级，必须全站浏览器回归，超出本轮范围 |
| 收敛 4–5 代 `--nv-*` 令牌 | 同上。已发现的令牌分叉（同名令牌在 app-ui 与 pages-theme 各有一份，靠特异性决出胜负）属于这类结构债 |
| 大规模死代码删除 | 同上 |
| `npm run lint` | **本项目开箱即坏**：装了 eslint 9 但没有 `eslint.config.js`。既有问题，与 UI 改造无关，不在范围内 |
| 海报图相关的视觉问题 | 海报文件是 Linux 路径 `/media/...`，本机无文件，首页显示"暂无海报"占位态。占位态的样式已一并核对，但不能替代真实海报下的视觉判断 |

---

## 五、实测验证怎么做的

**先量后改，不靠读 CSS 猜。** 本轮新建 5 个探针（都在 `fanvideo-evidence/`）：

| 探针 | 作用 |
|---|---|
| `probe.mjs` | 读令牌值与 CSSOM 溯源，定位"改了不生效"的根因 |
| `probe-pseudo.mjs` | CDP `CSS.forcePseudoState` 强制伪状态，量 hover/active/focus 的真实 computed 差异 |
| `verify-focus-batch2.mjs` | **真实 Tab 键**遍历（`Input.dispatchKeyEvent`），不用 `element.focus()` |
| `check-contrast.mjs` | 遍历所有文字节点，按 WCAG 2.2 AA 算对比度 |
| `shoot-auth.mjs` | 带登录态截图，支持多视口 × 多主题 |

**最终数字：**

- 对比度：3 路由 × 双主题 = 6 组、共 **526 个文字节点，0 处低于 AA**
- 焦点可见性：真实 Tab 遍历 **69 个可聚焦控件，69 个有 `:focus-visible`，69 个有可见焦点环，0 缺失**
- 类型检查：`tsc -b` 通过
- 构建：`vite build` 通过（2149 modules，9.89s）
- 项目自带 `verify:retired-ui`：连续 3 轮全绿
- 截图：16 张（桌面 1440×900 / 手机 390×844 × 深浅双主题 × 4 个路由）

### 过程中被自己的测量工具骗到三次，都记下来了

1. **对比度脚本漏了 CSS Color 4 语法**：`color(srgb 0.61 0.55 1)` 的分量是 0–1 不是 0–255，脚本当 0–255 解析，量出侧栏选中项 1.10:1 的假报警。
2. **半透明背景合成顺序错**：徽标底色是 `rgba(25,25,28,.72)`，脚本把累积结果的 alpha 强行置 1，导致 14 个"480p"徽标全部误报 1.13:1。实际 CSS 完全正确。
3. **焦点环检测盲区**：搜索框的焦点环画在包裹层 `.nv-search` 上而非 `<input>` 自身，只查 `activeElement` 会误报缺失。补上向上遍历祖先后 69/69。

**另一个真实的静默失效**：我写的 `[data-theme="light"] html body ...` 规则进了 CSSOM、`selectorText` 也能读到，但 `matches()` 永远返回 false —— 因为 `data-theme` 挂在 `<html>` 自身上，而后代组合器要求它是真后代。必须写 `html[data-theme="light"] body`。这个坑不报错、不报警，只在实测对比度时才暴露。

**还有一个测量工具的静默失败**：截图脚本用 `dark:desktop` 这种带冒号的标签做文件名，而 Windows 不允许 `:`，`writeFileSync` 直接抛错被我漏接，四轮截图**一张都没落盘**却仍然打印了"已保存"。现在脚本会在写盘后校验文件大小，小于 1KB 即视为失败。

---

## 六、独立评审

`web/UI_AUDIT_REPORT.md` 里上一轮列出的 A1–A3 / B1–B3 / C1 / M1–M5 / D1–D2 逐条复核：

- **A1**（卡片焦点环）→ 已落地，`DashboardTab.tsx:1167` 有 `focus-visible:shadow-[var(--nv-shadow-focus)]`
- **A2**（viewport 阻止缩放）→ 已移除，`index.html:8` 不再有 `maximum-scale` / `user-scalable=no`
- **A3**（强制色/高对比）→ 已落地，`base.css:2748` 有 `forced-colors`、2769 有 `prefers-contrast`
- **B3**（`:active` 反馈稀缺）→ **本轮批次 2 处理完毕**
- **M1 / M2 / M3**（CSS 架构、令牌分叉、死代码）→ 明确不做，理由见上表

---

## 七、删掉了哪些文字

**界面上一处文字都没删。**

审视过是否该删的文案，没有找到该删的。本轮改动全部是字号、颜色、反馈态、对比度 —— 没有任何一处需要靠删字来解决。

如果后续要做，第一批候选是：首页 Hero 的副标题（与卡片标题信息重复）、播放器的"点击或按空格播放"提示（对多数用户是冗余的）。但这两处都需要真实海报内容才能判断，留待后续。

---

## ⚠️ 必须披露：一次真实数据库事故

搭建实测环境时，我犯了一个严重错误，如实记录：

**经过**：为在本机跑起后端，我从项目目录直接启动服务。`config/app.yaml` 里的 `data_dir: ./data` 是扁平相对路径，我尝试用 `NOWEN_APP_DATA_DIR` 环境变量覆盖但**没有生效**，服务端仍然打开了项目**真实的数据库**。服务启动时的 `media_cleanup.go` 以"残留清理"名义执行，把 `media` 表**清空了**。

- 真实库 `media` 表从 **617 条变成 0 条**，同时 `login_logs` 混入了我的失败登录尝试。

**止损与还原**：
- 12:09 我恰好留了一份事前快照 `fanvideo-evidence/rundata/nowen.db`（校验含 617 条），已完整复制回 `data/nowen.db`
- 受损的 `nowen.db` / `-wal` / `-shm` 移入 `fanvideo-evidence/damaged-db-backup/` 留档，未删除
- 还原后校验：`media: 617` 条、`login_logs` 回到 2026-09-13

**根治**：改为**独立工作目录**运行（`fanvideo-evidence/apprun/`，自带 `config/` 与 `data/`），配置路径相对 CWD 解析，物理隔离。已验证隔离库收到登录记录而真实库零改动。本轮所有实测都在隔离环境进行。

---

## 九、证据文件位置

全部在 `C:\Users\meimo\WorkBuddy\2026-10-19-10-04-57\fanvideo-evidence\`：

```
baseline/          改造前基线截图
after-batch1/      批次 1 后复验截图
after-batch2/      批次 2 后复验截图（含此前缺失的手机视口）
final/             最终 16 张（桌面 + 手机 × 深浅双主题 × 4 路由）
apprun/            隔离运行环境（config/ + data/）
damaged-db-backup/ 受损数据库留档
rundata/           12:09 事前快照（还原源）
probe.mjs / probe-pseudo.mjs / verify-focus-batch2.mjs
check-contrast.mjs / shoot-auth.mjs
```

改动的源文件只有两个：

- `web/src/styles/app-ui.css`（+批次 2/3 追加的规则块，含逐条原因注释）
- `web/src/styles/pages-theme.css`（令牌修正）
