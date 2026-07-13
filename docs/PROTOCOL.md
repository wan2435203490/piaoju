# 拾光票局 API & 数据契约 v1

> 唯一契约源。改动需主线程批准并广播。规范细则（信封/分页/时间/金额）见 piaoju-conventions skill，此处不重复。

## 1. 错误码表

| code | 含义 |
|---|---|
| 0 | ok |
| 40001 | 参数校验失败 |
| 40002 | 不支持的枚举值 |
| 40101 | access token 过期 |
| 40102 | refresh token 无效/已吊销 |
| 40103 | 邮箱或密码错误 |
| 40901 | 邮箱已注册 |
| 40902 | 幂等冲突（同 id 不同内容且 updatedAt 更旧） |
| 40401 | 资源不存在或无权访问 |
| 41301 | 上传文件超限（>10MB）或不支持的图片格式 |
| 50000 | 服务端错误 |

## 2. Auth

```
POST /api/v1/auth/register   { email, password(≥8), nickname }
  → data: { user: User, accessToken, refreshToken }
POST /api/v1/auth/login      { email, password }
  → data: { user: User, accessToken, refreshToken }
POST /api/v1/auth/refresh    { refreshToken }
  → data: { accessToken, refreshToken }   // 旋转：旧 refresh 立即吊销
POST /api/v1/auth/logout     { refreshToken } → data: null

User = { id: number, email: string, nickname: string, createdAt: string }
```

## 3. Categories

系统预设（user_id NULL，seed 迁移写入）：餐饮🍜 奶茶🧋 交通🚇 购物🛍 娱乐🎮 日用🧻 医疗💊 其他📦（expense）；工资💰 红包🧧 其他（income）。

```
GET  /api/v1/categories                → data: { items: Category[] }   // 系统 + 本人自定义
POST /api/v1/categories                { name, icon, kind }
PATCH/DELETE /api/v1/categories/{id}   // 仅自定义分类可改删；删除后其交易归入「其他」

Category = { id: number, name: string, icon: string(emoji),
             kind: "expense"|"income", isSystem: boolean, sort: number }
```

> 写接口响应约定（v1.1 补）：POST/PATCH 成功返回 data = 完整实体（Transaction/Ticket/Category）；DELETE 成功返回 data = null。

## 4. Transactions

```
GET    /api/v1/transactions?month=2026-07&categoryId=&direction=&cursor=&limit=50
       → data: { items: Transaction[], nextCursor }   // occurredAt desc
POST   /api/v1/transactions            TransactionInput   // id 客户端 UUID，幂等 upsert
PATCH  /api/v1/transactions/{id}       Partial<TransactionInput>
DELETE /api/v1/transactions/{id}       → 软删
       // v1.1 补：若该交易关联票据（ticketId 非空）→ 40001「请从票夹删除该票据」；PATCH 不允许改 direction

Transaction = {
  id: string(uuid), amountCents: number, direction: "expense"|"income",
  categoryId: number, note: string, occurredAt: string(RFC3339),
  paymentMethod: "wechat"|"alipay"|"cash"|"card"|"other",
  ticketId: string|null,            // 反向关联，只读
  createdAt: string, updatedAt: string
}
TransactionInput = 同上去掉 ticketId/createdAt/updatedAt
```

## 5. Tickets

```
GET    /api/v1/tickets?kind=&year=&cursor=&limit=20   → data: { items: Ticket[], nextCursor }
GET    /api/v1/tickets/{id}
POST   /api/v1/tickets            TicketInput    // 服务端事务内同时建 Transaction，其 note = title（建票/重放时快照）
PATCH  /api/v1/tickets/{id}       Partial<TicketInput>（amountCents 变更同步改交易；title 变更不回写交易 note）
DELETE /api/v1/tickets/{id}       // 软删票 + 关联交易

Ticket = {
  id: string(uuid), kind: Kind, title: string, venue: string,
  eventTime: string(RFC3339), seat: string,
  extra: Extra, rating: number(0-5, 0=未评), memo: string,
  transaction: { id, amountCents, categoryId, paymentMethod },  // 内嵌只读
  attachments: Attachment[],
  createdAt, updatedAt
}
TicketInput = { id, transactionId, kind, title, venue, eventTime, seat, extra, rating, memo,
                amountCents, categoryId, paymentMethod, occurredAt,
                attachmentIds: number[] }
  // transactionId（v1.2 补）：联动交易的主键，与 id 一样由客户端生成 UUIDv4
  // （conventions §1；离线建票时本地须立刻能把这笔交易写进账本，故不能等服务端生成）。
  // 幂等重放：以库中已存在的 transaction_id 为准，不被 payload 覆盖（防交易分裂）。
Attachment = { id: number, url: string, thumbUrl: string, w: number, h: number }
```

### extra 按 kind（全部字段可空字符串）

| kind | extra 字段 |
|---|---|
| movie | `{ cinema hall filmFormat }`（IMAX/杜比…） |
| show | `{ tour session zone }` |
| attraction | `{ city ticketType }`（成人/学生…） |
| train | `{ trainNo fromStation toStation departTime arriveTime seatClass }` |
| flight | `{ flightNo airline fromAirport toAirport departTime arriveTime cabin }` |
| other | `{}` |

## 6. Uploads

```
POST /api/v1/uploads   multipart form: file（jpeg/png/webp ≤10MB；heic 由客户端转 jpeg 后上传——Capacitor Camera 默认输出 jpeg，纯 Go 无法解码 heic；服务端遇不支持格式回 41301）
  → data: Attachment    // 服务端：长边>2000 压缩、质量80、生成 480px 缩略图
GET  /uploads/{path}    // 静态服务，URL 即 Attachment.url（带签名参数防越权）
```

## 7. Stats

```
GET /api/v1/stats/monthly?month=2026-07
  → data: { expenseCents, incomeCents, byCategory: [{ categoryId, cents, count }],
            byDay: [{ date: "2026-07-01", expenseCents }] }
  // 口径（v1.1 补）：byCategory / byDay 仅统计 expense；expenseCents/incomeCents 为当月两向总额
GET /api/v1/stats/tickets?year=2026
  → data: { total, byKind: [{ kind, count, cents }] }
```

## 8. Sync（M3 启用；M1/M2 后端即按此实现幂等，前端 outbox 后接）

```
POST /api/v1/sync/push
  { changes: [{ entity: "transaction"|"ticket", op: "upsert"|"delete",
                payload: TransactionInput|TicketInput|{id}, clientUpdatedAt }] }
  → data: { results: [{ id, status: "applied"|"stale"|"error", code }] }
  // LWW：clientUpdatedAt < 服务端 updated_at → stale（服务端版本随 pull 下发）
  //
  // clientUpdatedAt 必须带毫秒（"2026-07-13T13:20:58.421Z"）——服务端 updated_at 是
  // DATETIME(3)，秒级时间戳（.000）恒早于同秒内的服务端版本，会被误判 stale。
  // JS `new Date().toISOString()` 天然满足；shell `date -u +%...Z` 不满足。
  //
  // 时钟：服务端 updated_at 一律由服务端时钟写入，与 clientUpdatedAt 不同源。
  // 客户端时钟慢于服务端 → 自己的改动恒被判 stale、永远推不上去，故客户端必须
  // 用 pull 下发的 updatedAt 反推偏移并校正（web: lib/db/clock.ts）。

GET /api/v1/sync/pull?since=<serverCursor>&limit=200
  → data: { transactions: [...], tickets: [...], categories: [...],
            nextCursor, hasMore }    // 含 deleted_at 墓碑；cursor 为服务端单调游标
```

## 9. DB Schema（迁移唯一实现于 server/migrations/，此处为契约摘要）

```sql
users(id BIGINT PK AI, email VARCHAR(191) UNIQUE, password_hash, nickname,
      created_at DATETIME(3))
refresh_tokens(id, user_id, token_hash CHAR(64), expires_at, revoked_at NULL)
categories(id BIGINT PK AI, user_id BIGINT NULL, name, icon, kind ENUM, sort,
           deleted_at NULL)
transactions(id CHAR(36) PK, user_id, amount_cents BIGINT, direction ENUM,
             category_id, note VARCHAR(500), occurred_at DATETIME(3),
             payment_method ENUM, created_at, updated_at DATETIME(3), deleted_at NULL,
             INDEX(user_id, occurred_at), INDEX(user_id, updated_at))
tickets(id CHAR(36) PK, user_id, transaction_id CHAR(36) UNIQUE,
        kind ENUM, title, venue, event_time DATETIME(3), seat,
        extra JSON, rating TINYINT, memo TEXT,
        created_at, updated_at, deleted_at NULL,
        INDEX(user_id, kind, event_time), INDEX(user_id, updated_at))
attachments(id BIGINT PK AI, user_id, ticket_id CHAR(36) NULL,
            file_path, thumb_path, w, h, size, created_at)
```
