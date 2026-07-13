# web —— 拾光票局前端（SvelteKit SPA）

SvelteKit(adapter-static, ssr=false) + Svelte 5 runes + TS strict + Tailwind v4。
契约：`docs/PROTOCOL.md`；约定：`piaoju-conventions` skill；UI 规范：`piaoju-design` skill。

## 命令（pnpm）

```sh
pnpm install
VITE_MOCK=1 pnpm dev   # UI 开发一律 mock 模式（fixtures + 150ms 延迟，不依赖后端）
pnpm dev               # 真后端联调（vite 已代理 /api、/uploads → localhost:8080）
pnpm check             # svelte-check（TS strict）
pnpm test              # vitest
pnpm build && pnpm preview
```

## 给 W2/W3/W4/W5 的速查

- **设计 tokens**：`src/app.css`（唯一来源，双模式）。组件 CSS 用 `var(--brand)` 等；
  模板可用 Tailwind 工具类 `bg-surface text-ink text-ink-2 border-line bg-kind-movie …`、`tnum`/`tabular-nums`。
- **类型**：全部从 `$lib/api/types`（PROTOCOL 全量 + `ERR` 错误码 + `ApiError`）。
- **读数据**：`import { api } from '$lib/api/client'`（信封解包、40101 自动 refresh 重试）。
- **写数据**：一律 `import { outbox } from '$lib/db/outbox'`（M3 换 Dexie 队列无感知）。
  业务 id 用 `import { uuid } from '$lib/utils/uuid'` 客户端生成。
- **金额渲染**：`<Amount cents={…} direction="expense|income" size="md|lg|xl" />`；
  纯字符串用 `$lib/utils/money` 的 `formatCents` / `signedAmount`。
- **基础组件**（已冻结，改动需报主线程）：`$lib/components/` 下
  Button / Amount / Sheet / EmptyState / Skeleton / NumPad / CategoryPicker / TabBar。

## 目录

```
src/lib/api/        types.ts client.ts mock.ts tokens.ts fixtures/*.json
src/lib/db/         outbox.ts（写操作统一入口）
src/lib/components/ 基础组件
src/lib/utils/      money.ts uuid.ts
src/routes/         (app)/ledger|tickets|stats|me + auth/login|register
```
