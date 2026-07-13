---
name: piaoju-conventions
description: 拾光票局（piaoju）工程约定与多 agent 协作协议。凡是在本项目中写任何 Go 后端代码、Svelte 前端逻辑、数据库迁移、API 接口，或作为并行开发 agent 领取模块任务时，必须先读本 skill。包含 API 契约位置、错误格式、金额/时间/ID 规范、目录所有权、mock 约定、DoD 验收标准。违反契约的代码将在 review 阶段被整体打回。
---

# 拾光票局 工程约定

## 0. 契约优先（多 agent 协作的根规则）

- **`docs/PROTOCOL.md` 是唯一契约源**：所有 API 路径、请求/响应 JSON、错误码、DB schema 以它为准。
- 前后端 agent **并行开发互不等待**：后端按契约实现，前端按契约写 typed client + 本地 fixtures mock。集成阶段只对契约，不对实现。
- 发现契约有缺陷：**停下，报告主线程**，由主线程改契约后各方跟进。任何 agent 不得私改 PROTOCOL.md。
- 每个 agent 只允许改动任务卡里声明的目录（见 docs/PLAN.md 所有权表）。跨目录需求 → 报告主线程。

## 1. 通用数据规范（前后端一致，写错即 bug）

| 事项 | 规范 |
|---|---|
| 金额 | 一律整数「分」：`amount_cents int64` / `amountCents: number`。渲染层才除 100 |
| 时间 | API 传输一律 RFC3339 UTC（`2026-07-12T11:30:00Z`）；展示层转本地时区。DB 用 `DATETIME(3)` 存 UTC |
| 业务主键 | `transactions.id` / `tickets.id` 为 **客户端生成 UUIDv4**（离线创建不冲突），服务端校验格式 + 幂等 upsert |
| 删除 | 软删 `deleted_at`（同步墓碑），查询默认过滤 |
| JSON 字段名 | API 一律 camelCase；DB 一律 snake_case |
| 枚举 | ticket kind: `movie/show/attraction/train/flight/other`；direction: `expense/income` |

## 2. API 约定

- 前缀 `/api/v1`，认证 `Authorization: Bearer <access_token>`。
- 响应信封，成功与失败结构固定：

```json
{ "code": 0, "message": "ok", "data": { ... } }
{ "code": 40101, "message": "token expired", "data": null }
```

- code 段位：0 成功；400xx 参数/校验；401xx 认证；403xx 权限；404xx 不存在；409xx 冲突；500xx 服务端。具体码表在 PROTOCOL.md。
- 列表分页：`?cursor=<opaque>&limit=50`，响应 `data: { items: [], nextCursor: "" | null }`。
- 所有接口按 user 隔离：handler 从 JWT context 取 userID，**任何查询不带 user_id 条件即为安全 bug**。

## 3. Go 后端约定（`server/`）

- Go 1.22+，router 用 `chi`，DB 访问用 `sqlc`（查询写在 `internal/<mod>/queries.sql`），迁移用 `golang-migrate`（`server/migrations/`，只增不改已合并迁移）。
- 目录即模块：`internal/{auth,transaction,ticket,stats,sync,upload}/`，每模块 `handler.go / service.go / queries.sql`。模块间只许调 service 接口，不许互查对方的表。
- 错误处理：service 返回 `apperr.New(code, msg)`；middleware 统一转响应信封。handler 里禁止手写错误 JSON。
- 密码 argon2id；JWT：access 15min + refresh 30d（DB 存 refresh 哈希，可吊销）。
- 配置走 env（`PIAOJU_DB_DSN` 等），提供 `.env.example`。
- 测试：service 层必须有单测（sqlite 内存或 testcontainers-mysql 均可，模块内保持一致）；`make test` 全绿才算完成。
- 日志用 `slog`，禁止 `fmt.Println`。

## 4. 前端约定（`web/`）

- SvelteKit static adapter（SPA 模式，`ssr = false`），TypeScript strict，Tailwind v4（token 见 design skill）。
- 类型唯一源：`web/src/lib/api/types.ts` 手工对齐 PROTOCOL.md（契约变更时同步改此文件）。
- API 只许经 `web/src/lib/api/client.ts`（统一信封解包、401 自动 refresh 重试、错误 toast）。组件内禁止裸 fetch。
- Mock：`web/src/lib/api/fixtures/*.json` + client 的 `VITE_MOCK=1` 开关。UI agent 开发一律跑 mock 模式，不依赖后端起服。
- 状态：Svelte 5 runes + 少量 store；不引入额外状态库。
- 离线（M3 前预留）：写操作统一走 `web/src/lib/db/outbox.ts` 接口（M1/M2 阶段其实现 = 直接调 client，M3 换成 Dexie 队列，调用方无感知）。
- 路由：`/ledger`（默认）`/tickets` `/stats` `/me` `/auth/login` `/auth/register`。

## 5. 包体红线（打包成 App 的硬约束）

- 新增 npm 依赖需主线程批准；gzip 影响 > 10KB 原则上拒。图表自绘 SVG。
- CI 检查：首屏 JS gzip < 200KB。图标用 unplugged 单个 SVG 导入，禁止整包 icon 库。

## 6. Git 与交付

- 分支 `feat/<module>`，Conventional Commits（`feat(auth): ...`）。
- 每个 agent 任务的 DoD：
  1. 契约对齐（接口路径/字段与 PROTOCOL.md 完全一致）
  2. `make lint && make test` 通过（web: `pnpm check && pnpm test`）
  3. UI 任务附带 design skill 第 6 节自查清单结果
  4. 只动了所有权表内目录
  5. 交付说明：改了什么、契约覆盖了哪些接口、已知未做项
