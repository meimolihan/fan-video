# Fan-Video UI / CSS 美化审查报告

**审查范围**：`web/src/styles/*.css`（约 22,000 行）+ `web/src/**/*.tsx`（121 个）中的 UI 实现。
**审查方法**：并行多路分析设计令牌、CSS 质量、响应式/无障碍、组件层四方面。
**结论**：视觉语言本身已相当惊艳（neo-aurora 深空渐变 + 玻璃拟态 + View Transition 圆形扩散主题切换），但 CSS 是"方案一 · CSS 大扫除"式多版本历史文件拼接物，存在严重的令牌冲突、`!important` 泛滥、重复与死代码，以及零星但明确的无障碍缺口。

客观数据（4 个 CSS 文件总计）：
- 原始 `#hex` 380 处、`rgba()/rgb()` 1074 处、裸 `px` 4825 处、裸 `rem` 370 处
- `!important`：app-ui 3,550 / pages-theme 1,137 / player 198 / base 169（**合计 5,054**）
- 160 个选择器在多个文件重复定义（749 条规则）；244 个选择器重复声明
- 至少 5 代同一 `--nv-*` 配色层叠互相覆盖；近 90 个已死 class（约 214 条规则 / 1,074 行 / 238 个 `!important`）

---

## 一、优先级最高的"选择性优化"清单（按用户可见收益排序）

### 🔴 A1. 修复管理后台用户启用开关丢失键盘焦点指示（无障碍，明确 Bug）
`src/components/admin/DashboardTab.tsx:768`
```tsx
className="relative inline-flex h-6 w-11 ... focus:outline-none"
```
`focus:outline-none` 且**没有** `focus-visible:shadow-[var(--nv-shadow-focus)]` 替代，`role="switch"` 在键盘导航下完全无焦点环。同一个开关在 `admin/storage/StorageUIKit.tsx:87`、`CreateLibraryModal.tsx:80`、`EditLibraryModal.tsx:70` 都有正确的 focus-visible，唯独这里漏了。违反 WCAG 2.4.7。**修法**：对齐其它实现，补 `focus-visible:shadow-[var(--nv-shadow-focus)]`。

### 🔴 A2. 放开移动端缩放锁定（无障碍，明确 Bug）
`web/index.html:8`
```html
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover" />
```
`maximum-scale=1.0` + `user-scalable=no` 禁止捏合缩放，叠加下面的小字号会进一步放大无障碍问题（WCAG 1.4.4）。**修法**：删除这两个属性。

### 🔴 A3. 高风险：完全没有高对比/强制色彩支持
4 个 CSS + TSX 中 `prefers-contrast` / `forced-colors` 均为 **0 条**。整套界面依赖 `color-mix()`、`rgba()` 透明叠加和 `backdrop-filter` 区分层次；Windows 高对比模式下这些计算色会塌陷，层次与焦点环（`--nv-focus-ring`）全部消失。**建议起步**：先给焦点环加 `CanvasText` 兜底，再补强制色彩下的边框。

### 🟠 B1. 触摸目标偏小（480–1023px 平板 / 粗指针设备）
- `.nv-button` 最终生效 `min-height: 32px`（`pages-theme.css:1459` 覆盖了 `base.css:988` 的 34px）；`sm`=30px、`lg`=40px、**`md` 无规则**（空洞）。
- 分页箭头/页码 `size="sm"` → 30px（`Pagination.tsx:83,100,113`）。
- `.nv-filter-chip` 30px（`app-ui.css:574-578`）。
- 开关 24px 高（`h-6`）。
- `base.css:189-216` 的移动端 44px 兜底**只在 <480px**，且**显式排除 `[role='switch']`**；`.touch-target` 48px 兜底未套用到按钮/分页/芯片。

### 🟠 B2. 断点体系碎片化（76 种不同 `@media`、768px 有 4 种拼法）
- 768px 边界拼法并存：`max-width:47.999rem`(22×)、`47.99rem`(6×)、`768px`(3×)、`767px`(12×)，767–769px 带留缝隙或重叠。
- `--nv-grid-column` 自相矛盾：`pages-theme.css:338-339` 190px vs `:361-363` 192px（同区间后者覆盖前者，前段意图失效）。
- TSX 里另一套 Tailwind `sm/md/lg/xl`（约 305 处）与纯 CSS 的桌面优先断点并存，且 tailwind.config 未自定义断点，两套语义不对齐。

### 🟠 B3. hover 有反馈、按压缩放稀缺
`:active` 规则全项目仅 16 处；TSX 几乎无 `active:`（仅 `VideoPlayer.tsx:1155`）。卡片/芯片/浏览行/分页按下无"按压确认"。部分 hover 行缺键盘等价（`.nv-browse-list-item:hover` `pages-theme.css:4118` / `:1626` 无 `:focus-within/:focus-visible`，而后卡类都有）。

### 🟡 C1. 字体过小（叠加 A2 使问题更严重）
- `.nv-rail-label` 10px（`pages-theme.css:152`）
- `.nv-filter-chip` 11px（`app-ui.css:578`）、播放器时间 11px、详情 tab 11.5px
- TSX 内 `text-[10px]`×49、`text-[11px]`×64（应映射到 `--nv-type-caption/meta`）

---

## 二、CSS 架构与维护性（影响后续所有美化的"地基"）

### 🔴 M1. `!important` 战争与过特化选择器
- `app-ui.css` 66% 声明带 `!important`；74% 规则以 `html body` 前缀抬特异度（1,127/1,520 条）。
- `.nv-app-shell.nv-app-shell` 选择器翻倍 ×48；`.nv-topbar` 定义 20+ 次、高度 48/50/52/56/60/62/64/72px 各用 `!important` 互踩。
- **后果**：改一个按钮/顶栏/分页的视觉，等于跟整条 `!important` 链打架。这是重设计前的头号障碍。

### 🔴 M2. 令牌没有"单一事实源"，主题随 import 顺序漂移
同一角色名 `--nv-bg-canvas`、`--nv-action-primary`、`--nv-radius-*` 各被 4-5 处重定义，注释自承"那些值早已不生效"（`pages-theme.css:1150-1151,1929-1931`）。**改 `main.tsx:9-12` 的 import 顺序就会改主题外观**。
- 暗色胜者：`pages-theme.css:3403-3463`；浅色胜者：`app-ui.css:2576-2597`。
- 现有"获胜"动作色是**平铺** `--nv-action-primary`（约 `#6875ff`），所有渐变 primary 定义（`base.css:1806`、`pages-theme.css:3576`、`app-ui.css:326`）都是死代码。

### 🔴 M3. 已死 class / 死代码可安全删除（零视觉变化）
- `nv-detail-sidebar`+`nv-detail-side-card*`(8 个兄弟 class) 整块右侧详情布局已死（`app-ui.css:5776,6217,6371-6452`）。
- `.nv-button--primary/--secondary` BEM 变体定义两次但标记用的是 `data-variant`（`app-ui.css:326,532`）；`.nv-switch`（`app-ui.css:107-141`）孤儿无用法。
- 约 90 个死 class / 214 条规则 / 1,074 行 / 238 个 `!important` 可清除。
- `pages-theme.css` 6,104 行中 **99.8% 是 UI-2.0 之前的旧主题层**；`base.css` 2,682 行中同样大部分是旧基石。文件头部已预留的"legacy 隔离桩"（`base.css:2685`、`pages-theme.css:6096`）表明团队原本就在计划废弃。

### 🔴 M4. 未分层 + 攻击 Tailwind 工具类
- `@layer` 只在 `base.css` 用过；因自定义 CSS 全部**未分层**，永远压过 Tailwind 的 `@layer utilities`。
- 137 条规则直接选中 Tailwind 工具类（如 `.group\/player .player-controls > .flex...`）；JSX 里改类名即静默破坏 CSS。

### 🟡 M5. 令牌体系空转
- `--nv-bp-*`（0 引用）、`--nv-space-*`（约 90% 被裸 px 绕过）、`--nv-z-overlay/toast`、`--nv-ease-emphasized` 只定义未用；取而代之是裸 z-index（`player.css:383` 的 `2147483000`、`app-ui.css:12014` 的 `9998`）。

---

## 三、组件层：重复实现与"值得加料的点"

### 🟡 D1. 开关无共享原语，5 处各做各的
`CreateLibraryModal.tsx:65`、`EditLibraryModal.tsx:55`、`admin/DashboardTab.tsx:761`、`admin/STRMConfigSection.tsx:222`、`admin/storage/StorageUIKit.tsx:67` 尺寸/结构/焦点处理各不相同。**抽一个统一 `Switch` 是最划算的组件整合**（顺带修掉 A1）。

### 🟡 D2. 一张"视觉/交互升级菜单"（自包含、可用现有令牌落地）
1. **Button 无内置 loading 微调**：`design-system/index.tsx:40-62` 只设 `aria-busy`+disabled，调用方各自 `Loader2`。可在 Button 内统一加 spinner。
2. **Modal / Toast 无进出场动画**：`Modal.tsx`/`Dialog.tsx`/`Toast.tsx` 均无 `transition/animation`（现有 keyframes 只有 `nv-detail-tab-in`/`nv-admin-content-in`）。
3. **评论区翻页与主分页组件分裂**：`CommentSection.tsx:246` 手搓 Button 行 vs 正式 `nv-pagination` 组件。
4. **空态/占位一致性**：多数页面用 `nv-empty-state`（`base.css:2105`），但 `SubtitleContentSearch` 自做 `.player-overlay-empty`；`BrowsePage.tsx:689` 有 9px 内联"暂无海报"兜底。
5. **文件浏览器文件夹**：`FileListView.tsx:261-283` 复杂 `nth-child` 边框逻辑 + 朴素图标。
6. **管理端迷你层**：`admin/STRMConfigSection`、`StorageUIKit`（自带 Toast/Toggle/StatusBadge）与共享 kit 并存。

---

## 四、明显的布局/文案风险（翻译/超长文案时才暴露）

- 硬编码 LTR 绝对定位：顶栏搜索 `left:50%`（`pages-theme.css:133-137`）、tab 下划线 `left/right:10px`（`app-ui.css:2025-2028`）、播放器 `left-4 top-4`。
- 英文标题在 `13–14ch` 处会破词截断（`pages-theme.css:181,1888`）。
- `Pagination.tsx:76-116` 是唯一有"第…页"硬编码中文、未走 `t()` 的组件。

---

## 五、做得好的地方（勿动）
- `hero-section-backdrop.css` 干净；核心 app-ui 层能正确用 `var(--nv-*)` + `color-mix`。
- `prefers-reduced-motion` 覆盖广（26 处），全局硬上限。**这是维护得最好的区**。
- 大部分 TSX 令牌化良好；`--nv-detail-hero-layer-*` 作用域 z-index 链是正确范式。
- 主题切换的 View Transition 圆形扩散 + 玻璃拟态是亮点，保留。

---

## 六、可执行的优化路径（按"性价比"排序建议）

> 选 1–3 项即可快速见效；选 4–8 属"地基翻修"，收益大但需回归。

| # | 动作 | 收益 | 风险 | 工作量 |
|---|---|---|---|---|
| 1 | A1 补 DashboardTab 开关 focus-visible | 修 a11y bug | 极低 | 1 行 |
| 2 | A2 放开 viewport 缩放 | 修 a11y bug | 极低 | 1 行 |
| 3 | M3 删除已死 class（~1,074 行） | 瘦身/去重 | 低（验证 0 引用） | 中 |
| 4 | B1 触摸目标：统一按钮基础高 + 补齐 `md`、覆盖粗指针 | 移动端体验 | 低 | 中 |
| 5 | B2 统一 768px 断点拼法 + 修 `--nv-grid-column` 矛盾 | 稳定响应式 | 中 | 中 |
| 6 | C1 小字号提升到 token（10/11px→caption） | 可读性 | 低 | 中 |
| 7 | M1 把自定义 CSS 迁入 `@layer components` + `.nv-button` 收敛为单一定义 | 消除 `!important` 战争 | 高（需回归全页面） | 高 |
| 8 | M2 收敛 4–5 代令牌到单一 `:root`/theme 块 | 主题可控、可再设计 | 高 | 高 |

> 7、8 是"再美化的地基"。若只想先出效果，推荐按 **1 → 2 → 4 → 6** 顺序（低风险高可见收益）。

*注：本报告为审查交付物，未改动任何代码。*

---

## 二、执行结论（本会话已落地的落地项 / 刻意延后项）

> 本会话按"安全、可验证、非破坏式"原则执行；涉及结构性/删除性 CSS 改造（M1/M2/大规模 M3）因**无浏览器回归验证**而刻意延后，交由带浏览器回归的一轮再处理。

### ✅ 已落地（全部通过 `tsc` + `vite build` + 完整脚本回归）

- **A1**：`DashboardTab.tsx:768` 开关补 `focus-visible:shadow-[var(--nv-shadow-focus)]`，对齐 `StorageUIKit.tsx:87`。
- **A2**：`web/index.html:8` viewport 移除 `maximum-scale=1.0` / `user-scalable=no`，放开捏合缩放。
- **A3**：`base.css` 追加 `@media (forced-colors)`（焦点环用 `CanvasText`、表面加边框）+ `@media (prefers-contrast: more)`（令牌/边框增强）；已去掉错误包裹的 `@layer base`，改为非分层以正确胜出。
- **B1**：`@media (hover:none) and (pointer:coarse)` 下将 `.nv-button / .nv-filter-chip / .nv-pagination-page / .nv-pagination-arrow / .nv-rail-action` 提到 `min-height/min-width:40px`（仅触屏，不影响桌面）。
- **C1**：`.nv-filter-chip` 字号 11px→12px（可读性）。
- **B2**：统一 768px 断点拼法——`max-width:767px` / `768px` / `47.99rem` 全部归一为 `47.999rem`（消除 ~0-2px 缝）；修正 `--nv-grid-column` 在 80–95rem 的矛盾（190 vs 192px），把 `max-width:95rem`(192) 收窄为 `(min-width:72rem) and (max-width:79.999rem)` 专属带，消除覆盖冲突。
- **D 系列**：`Button` 内置 `loading` spinner（`Loader2` + `animate-spin motion-reduce:animate-none`，`aria-busy` 复用）；`Modal`（backdrop 淡入 + sheet 上移缩放淡入）与 `Toast`（右侧滑入）补入场动画，全部受 `prefers-reduced-motion` 保护。

### ⏸ 刻意延后（需浏览器回归验证后再做）

- **M3（大规模死代码删除）**：核查确认当前 CSS 里真正"已定义且 0 引用"的死 class 只有 **`nv-detail-sidebar`** 与 **`nv-switch`** 两类（分布 20+ 处，含共享选择器列表）；报告的"近 90 个死 class"多数为旧版命名，在当前 CSS 中根本不存在。死规则不匹配任何元素、无行为影响，删除收益低且需触碰共享选择器，故交由带浏览器的回归轮删除 `.nv-card`、`nv-detail-sidebar`、`nv-switch` 等。
- **M2（收敛 4–5 代令牌）**：涉及删除层叠里的"败者"，直接影响主题结果，未在无回归下执行。
- **M1（全 CSS 迁入 `@layer components`）**：会改变 ~137 条刻意覆盖 Tailwind utility 的不分层规则的层叠优先级，属高风险全面重构，明确**不在此无浏览器会话执行**。
