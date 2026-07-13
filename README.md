# 拾光票局 (piaoju)

票据档案馆 + 轻记账。把电影票、火车票、演出票这些「用完即弃」的票根收进档案馆，顺手记一笔账。

- **后端**：Go + chi + MySQL 8（sqlc、golang-migrate）
- **前端**：SvelteKit SPA（static adapter, ssr=false）+ TypeScript + Tailwind v4
- **移动端**：Capacitor 打包 Android / iOS（规划中）

## 仓库结构

```
server/     Go API（cmd/api 入口，internal 按模块划分）
web/        SvelteKit 前端（VITE_MOCK=1 走本地 fixtures）
docs/       PROTOCOL.md（API 契约，唯一事实源）、PLAN.md（执行计划）
.claude/    项目 skill（工程约定、设计规范）
```

## 快速开始

```bash
# 后端 + 数据库
cp .env.example .env
docker compose up -d          # mysql8 + api
make dev                      # 或本地起后端 (localhost:8080)

# 前端
cd web
pnpm install
pnpm dev                      # VITE_MOCK=1 时无需后端

# 测试 / 静态检查
make test                     # go test ./...
make lint
```

## 核心约定

- 金额一律整数「分」，时间 RFC3339 UTC，业务主键为客户端生成的 UUID，删除走软删墓碑
- API 响应统一信封 `{code, message, data}`，全部查询按 user_id 隔离
- 契约唯一来源：[`docs/PROTOCOL.md`](docs/PROTOCOL.md)；改动流程见 [`docs/PLAN.md`](docs/PLAN.md)

## 当前状态

开发中。已完成：项目地基（迁移 0001–0003、CI、docker-compose）、后端核心骨架（中间件 / 配置 / 错误信封 / JWT 校验）、票据与上传模块、前端壳（设计 tokens、基础组件、路由骨架）。进行中：auth、记账与统计模块及各页面 UI，详见 [`docs/PLAN.md`](docs/PLAN.md)。
