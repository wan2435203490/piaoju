---
name: piaoju-design
description: 拾光票局（piaoju）设计系统与 UI/UX 规范。凡是在本项目中编写或修改任何前端代码（Svelte 组件、页面、样式、图表、动效、空状态、表单），或评审 UI 相关 diff 时，必须先读本 skill。包含设计 tokens、组件规范、票根视觉语言、交互模式、暗色模式与可访问性要求。不读本 skill 直接写 UI 属于违规产出。
---

# 拾光票局 设计系统

品牌气质：**纸质票根的怀旧感 × 现代工具的干净利落**。像一本随身携带的票据收藏册，不像一个理财软件。

> 方向已定稿（2026-07-12，用户确认）：主体 = 方案 A「纸感票局」（即本文全部 token）；唯一例外：**movie 票根卡左缘用胶片齿孔条**（见 §2）。不再讨论其他风格方向。

## 1. 设计 Tokens（唯一来源：`web/src/app.css` 中的 CSS variables）

写任何样式前先确认 token 已存在；缺 token 先补 token，禁止在组件里写裸色值/裸尺寸。

### 色彩

| Token | Light | Dark | 用途 |
|---|---|---|---|
| `--bg` | `#FAF6EF` 米白纸 | `#171412` | 页面底色（纸感，不用纯白/纯黑） |
| `--surface` | `#FFFFFF` | `#211D1A` | 卡片/弹层 |
| `--ink` | `#292524` | `#E7E0D8` | 主文字 |
| `--ink-2` | `#78716C` | `#A8A29E` | 次要文字 |
| `--line` | `#E7E0D3` | `#3B3532` | 分隔线/描边 |
| `--brand` | `#C2410C` 赭橙 | `#F97316` | 主操作、选中态、金额支出 |
| `--accent` | `#0F766E` 深青 | `#2DD4BF` | 收入、成功、次强调 |
| `--danger` | `#DC2626` | `#F87171` | 删除/错误 |

票型色标（票夹卡片左侧色条 + 图标底色）：
movie `#E11D48` / show `#9333EA` / attraction `#EA580C` / train `#16A34A` / flight `#2563EB` / other `#64748B`。

对比度要求：正文对底色 ≥ 4.5:1，大字号 ≥ 3:1。改色先验证再提交。

### 字体与数字

- 字体栈：`system-ui, -apple-system, "PingFang SC", "Noto Sans SC", sans-serif`。不引入 webfont（包体红线）。
- 金额一律 `font-variant-numeric: tabular-nums`，组件 `<Amount>` 统一渲染：支出 `--brand` 前缀 `-`，收入 `--accent` 前缀 `+`，两位小数，千分位。
- 字号阶梯（rem）：12 辅助 / 14 正文 / 16 强调 / 20 标题 / 28 金额大数 / 34 月度汇总。行高 1.5，标题 1.25。

### 间距·圆角·投影·动效

- 间距 4px 网格：4/8/12/16/24/32。页面左右留白 16px。
- 圆角：卡片 12px，按钮/输入 10px，票根卡 16px，全屏 sheet 顶部 20px。
- 投影只用一档：`0 1px 3px rgb(0 0 0 / 0.08)`；暗色模式用描边 `--line` 替代投影。
- 动效：150ms（hover/按压）、250ms（sheet/页面切换），easing `cubic-bezier(0.2, 0, 0, 1)`。列表增删用 Svelte transition（fly/fade）。尊重 `prefers-reduced-motion`。

## 2. 票根视觉语言（本产品的识别度所在）

票夹里的每张票渲染为「票根卡」：

```
┌──────────────────────────────┐
│▌流浪地球 3           ¥45.00  │   ▌= 票型色条 4px
│▌万达影城·IMAX厅              │
│▌2026-07-12 19:30  9排12座    │
├╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌┤   ← 撕票线：dashed border +
│▌★★★★☆  "比第二部好"          │      两侧半圆打孔缺口
└──────────────────────────────┘
```

- 撕票线用 `border-top: 1px dashed var(--line)` + 左右两个 `radial-gradient` 打孔缺口（伪元素实现，不用图片）。
- **movie 专属**：左缘色条替换为 14px 宽胶片齿孔条——`background-color` 取 movie 色标，`background-image: radial-gradient(circle, var(--bg) 2.5px, transparent 3px)`，`background-size: 14px 12px`，正文区左 padding 相应加大。其他票型保持 4px 纯色条。
- 有票面照片时照片区在上（16:9 裁切，`object-fit: cover`），无照片时展示票型图标 + 色块底纹，不留空白灰框。
- 高铁/飞机票卡布局特化：出发地 → 到达地 大字横排，中间箭头/虚线飞机线，车次/航班号居中小字。

## 3. 组件清单（`web/src/lib/components/`，所有页面从这里取，禁止页面内重复造）

Button（primary/ghost/danger, loading 态）· Amount · TicketCard（按 kind 分派布局）· QuickAddSheet（快记面板）· CategoryPicker（网格图标选择）· NumPad（自绘数字键盘）· StatCard · DonutChart / TrendChart（自绘 SVG，遵循 dataviz skill 原则：先读 dataviz skill 再写图表）· EmptyState · Skeleton · SafeAreaLayout · TabBar。

## 4. 核心交互规范

### 3 秒快记（产品生命线，体验优先级最高）
1. 首页右下角 FAB（56px，`--brand`）→ 弹出底部 QuickAddSheet
2. Sheet 内自上而下：金额大数显示 → 分类网格（预设 8 个常用，横滑更多）→ 自绘 NumPad → 备注（可跳过）
3. 键盘即算式：支持 `12+8` 直接求和
4. 保存即关，列表顶部新条目 fly-in；失败不弹错——静默入离线队列，条目带「待同步」小圆点
5. 全程无必填校验拦截：只有金额 > 0 一条

### 通用
- 页面结构：TabBar 三主页（账本 / 票夹 / 我的）+ 各自栈内导航。
- 删除一律二次确认（ActionSheet），删除后 5 秒 toast 可撤销（软删本来就支持）。
- 列表页必配：空状态（插画级 EmptyState + 引导按钮）、骨架屏（首屏 > 300ms 时）、下拉刷新。
- 表单日期默认今天，时间默认现在；票型表单按 kind 显示专属字段（见 conventions skill 的 extra 字段表）。
- 触控目标 ≥ 44×44px。iOS 安全区：所有固定元素用 `env(safe-area-inset-*)`。

## 5. 暗色模式

- 必须支持，跟随系统 `prefers-color-scheme`，token 双值已定义，组件只引用 token 即自动适配。
- 图片/照片在暗色下加 `filter: brightness(0.9)` 防刺眼。
- 提交 UI 前两种模式都必须自查截图。

## 6. 验收自查清单（每个 UI agent 交付前逐条过）

- [ ] 无裸色值/裸 px 魔数（token 化）
- [ ] 暗色模式正常
- [ ] 空状态、加载态、错误态齐备
- [ ] 金额走 `<Amount>`，数字 tabular-nums
- [ ] 安全区、44px 触控、reduced-motion
- [ ] 无新增 npm 重依赖（图表自绘；新依赖需主线批准，gzip 影响 > 10KB 一律拒）
