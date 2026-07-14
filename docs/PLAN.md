# 拾光票局 开发 Plan v2 — 多 Agent 细分版

> 产品定位、选型理由见 v1（`../../拾光票局-PLAN.md`）。本文是执行计划：任务拆到 agent，契约先行。
> 契约：`docs/PROTOCOL.md`。约定：`.claude/skills/piaoju-conventions`。UI 规范：`.claude/skills/piaoju-design`。

## 执行模型

- 每个任务卡 = 一个 agent 一次交付。并行波次内 agent 用 worktree 隔离。
- 所有 agent 提示词固定头部：**先读 piaoju-conventions；UI 任务加读 piaoju-design + dataviz；契约以 docs/PROTOCOL.md 为准；只改所有权目录**。
- 每波结束：主线程集成 + `/code-review` + verify，绿了才开下一波。

## 目录所有权表（防冲突的硬边界）

| 目录 | Owner |
|---|---|
| `server/cmd,internal/{platform,middleware},migrations 0001-0003,Makefile,docker-compose` | S1 |
| `server/internal/auth` + auth 相关迁移 | S2 |
| `server/internal/{transaction,stats}` | S3 |
| `server/internal/{ticket,upload}` | S4 |
| `server/internal/sync` | S5 |
| `web/`（scaffold、app.css tokens、components 基础件、api/client+types+fixtures、布局路由骨架） | W1 |
| `web/src/routes/(app)/ledger`、`stats` + 记账专用组件 | W2 |
| `web/src/routes/(app)/tickets` + TicketCard 家族 | W3 |
| `web/src/routes/auth`、`(app)/me` | W4 |
| `web/src/lib/db`（Dexie/outbox/同步引擎） | W5 |
| `mobile/`（Capacitor） | P1 |
| `docs/`、契约、集成粘合 | 主线程 only |

基础组件（Button/Amount/Sheet 等）W1 建成后冻结；W2/W3/W4 要改基础件 → 报主线程，不许直接动。

## Wave 0 — 地基（主线程，串行，0.5 天）

- [x] 契约 PROTOCOL.md、两个项目 skill
- [x] T0.1 repo init：目录骨架、Makefile（dev/lint/test/build）、docker-compose（mysql8 + api）、.env.example、CI（lint+test+bundle-size 门禁）
- [x] T0.2 迁移 0001-0003（users/refresh_tokens/categories+seed、transactions、tickets/attachments）——迁移先行，S2-S4 只消费不建表
- [ ] T0.3 UI mockup 3 版（Artifact 预览）→ 用户定稿 → 回写 design skill tokens（tokens 初版已在 design skill；mockup 定稿待用户）

## Wave 1 — 双骨架（2 agent 并行，≈1 天）

### S1 server-core
- chi 骨架、config(env)、slog、响应信封 + apperr、JWT 中间件（验签 + userID 注入 context，签发逻辑留给 S2 的接口）、panic recover、CORS、healthz、sqlc 初始化、迁移 runner、Dockerfile
- DoD：`make dev` 起服，healthz 200；信封/错误码与契约 §1 一致；中间件有单测

### W1 web-shell
- SvelteKit(static, ssr=false) + TS strict + Tailwind v4；app.css 全量 tokens（design skill §1）；SafeAreaLayout + TabBar + 路由骨架（各页占位）；基础组件：Button/Amount/Sheet/EmptyState/Skeleton/NumPad/CategoryPicker 壳
- `api/types.ts`（契约全量类型）、`api/client.ts`（信封解包、401 refresh 重试、VITE_MOCK 开关）、fixtures（每个接口 ≥1 组逼真假数据：中文片名、真实车次样式）
- outbox 接口壳：`db/outbox.ts`（M2 前实现 = 透传 client）
- DoD：`pnpm dev` mock 模式全路由可点；暗色/亮色 token 生效；`pnpm check` 绿

## Wave 2 — 模块并行（6 agent，worktree，≈2-3 天）

### S2 auth
契约 §2。argon2id、JWT 签发、refresh 旋转+吊销（token_hash 落库）、限速（登录 5次/分/IP）
DoD：注册→登录→refresh→logout 全链路集成测试；错误码 40101/40102/40103/40901 精确

### S3 transactions + stats
契约 §4 §7。UUID 幂等 upsert、cursor 分页、month 过滤、软删；stats 两接口 SQL 聚合（不许内存算大数据集）
DoD：幂等重放测试（同 id 重发不重复）；分页边界测试；stats 数字与手算对账

### S4 tickets + upload
契约 §5 §6。建票事务内联动建交易（金额/删除同步）；extra JSON 按 kind 校验白名单字段；upload 压缩+缩略图（`disintegration/imaging`）、签名 URL
DoD：票↔交易联动一致性测试；HEIC/超限/非图片拒收测试

### W2 ledger + stats UI（读 design skill 全文，重点 §4 快记）
账本页（月切换、日分组流水、月收支头卡）、QuickAddSheet 完整交互（NumPad 算式、3 秒路径）、stats 页（Donut 分类占比 + 日趋势条图，先读 dataviz skill）
DoD：mock 模式录屏级走查；design skill §6 清单全过

### W3 tickets UI
票夹页（卡片墙/时间线双视图切换、kind/年份筛选）、TicketCard 五种 kind 特化布局（撕票线/打孔/火车横排），票详情页、五套票型表单（拍照占位调 outbox 上传）
DoD：五种票型 fixtures 全渲染正确；空态/骨架齐

### W4 auth UI + me
登录/注册页（token 持久化、client 对接）、我的页（昵称、导出入口占位、深色模式指示、退出）
DoD：与 mock 的 401 refresh 流程联调通过

## Wave 3 — 集成收口（主线程 + review workflow，≈1 天）

- T3.1 主线程：合并、关 VITE_MOCK 接真后端、docker-compose 全栈冒烟（注册→记账→建票→看统计）
- T3.2 review workflow：按维度 fan-out（安全:越权/user_id 泄漏、契约一致性、UI 规范符合度、测试盲区）→ 每 finding 对抗验证 → 修复小 agent 逐个修
- T3.3 verify skill 真机流程验证
- **⬅ M1+M2 完成线：web 端可日常使用**

## Wave 4 — 离线同步（M3）✅

- [x] S5 sync 后端：契约 §8，游标单调性（updated_at,id keyset）+ LWW + 墓碑。24 个 sqlmock 单测
- [x] W5 离线引擎：Dexie schema、outbox 队列（重试/指数退避/在线探测/队列合并）、pull 合并（本地 pending 不被覆盖）、待同步小圆点、Service Worker（app shell 缓存；数据离线归 Dexie）
- [x] 测试卡：双端并发改同条记录、离线建票带照片、时钟漂移（`web/src/lib/db/offline*.test.ts`）
- [x] 端到端冒烟：`scripts/sync-smoke.sh`

**契约在本波补了两个只有离线才暴露的洞（详见 PROTOCOL v1.2）**：
1. §5 `TicketInput.transactionId` —— 联动交易主键改由客户端生成。原先服务端自己 `newUUID()`，
   离线建的票不知道交易 id，本地账本/统计就缺这一笔，直到联网 pull 回来才出现。
2. §8 `clientUpdatedAt` 必须带毫秒 —— 服务端 `updated_at` 是 DATETIME(3)，秒级 `.000`
   恒早于同秒内的服务端版本，会被 LWW 误判 stale。并明确两个时间戳来自不同时钟：
   客户端时钟慢会导致自己的改动永远推不上去（`web/src/lib/db/clock.ts` 据 pull 下发的
   服务端时间戳反推偏移并校正）。

## Wave 5 — 打包 App（M4，≈1 周）

- P1 Capacitor：android/ios 工程、Camera/Filesystem/StatusBar/SplashScreen 插件接入（拍票走原生相机）、图标启动图、签名、包体检查（APK < 6MB）
- 主线程：真机走查 iOS 安全区/键盘遮挡/暗色，TestFlight
- 卡点提醒：Apple 开发者账号 $99/年需提前申请

## Wave 6 — 增强（M5）✅ 四项已交付（`faeda7a` + review 修复 `39d2e22`）

- [x] LLM 识票：拍照→结构化 JSON（json_schema 约束五票型 extra 白名单，一个 prompt 自判 kind）。
      契约 §6.1；key 走 `PIAOJU_LLM_API_KEY`，未配则端点回 50001、前端隐藏入口，主流程不受影响。
      `server/internal/vision/`。**待办：配 key + 真票照片人工验 prompt 效果（自动化测不了）**
- [x] 微信/支付宝 CSV 导入+规则分类：契约 §6.2，只 preview 不 commit，写入走 sync/push 复用幂等/LWW。
      `server/internal/importer/`（GBK/BOM/表头名映射/查重）+ `web/src/routes/(app)/import/`
- [x] 票根分享图：Canvas 2D 自绘（html2canvas 超包体红线，禁用）；污染/加载失败降级无照片版式。
      `web/src/lib/components/share/`
- [x] 年度报告：票型概览/月度趋势/年度之最/城市足迹。**足迹是条形图不是地图**——凭记忆画简化中国
      轮廓易错国界/岛屿，不值得为好看担风险。`/year`（「我的」页入口）
- [ ] 预算提醒：未做（Wave 6 原始清单里的第五项，按需再开卡）

## 风险登记

| 风险 | 对策 |
|---|---|
| 并行 agent 改基础组件打架 | 所有权表 + W1 后基础件冻结 |
| 契约中途变更扩散 | 只有主线程改 PROTOCOL.md，变更即广播受影响 agent |
| 包体膨胀 | conventions §5 红线 + CI 门禁 |
| App Store 4.2 拒审 | 原生相机+离线+推送凑原生价值，上架前评估 |
| 同步数据错乱 | Wave 4 压缩并行度，主线程主写 |
