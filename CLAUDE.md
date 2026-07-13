# 拾光票局 (piaoju)

票据档案馆 + 轻记账。Go + MySQL 后端，SvelteKit SPA 前端，Capacitor 打包 Android/iOS。

## 开工必读（agent 与主线程同样适用）

1. 写任何代码前：读 `piaoju-conventions` skill（工程约定、契约规则、目录所有权）
2. 写任何 UI 前：加读 `piaoju-design` skill（tokens、组件、交互规范）；写图表再加读 `dataviz` skill
3. API/数据结构唯一契约：`docs/PROTOCOL.md`（只有主线程可改）
4. 执行计划与任务卡：`docs/PLAN.md`

## 常用命令

```bash
make dev      # 起后端 (localhost:8080)
make test     # go test ./...
make lint
cd web && pnpm dev          # 前端 (VITE_MOCK=1 走 fixtures)
docker compose up -d        # mysql8 + api
```

## 硬规则速记

- 金额整数分、时间 RFC3339 UTC、业务主键客户端 UUID、软删墓碑
- 响应信封 `{code,message,data}`；查询必须带 user_id 隔离
- 前端新依赖 gzip >10KB 默认拒；首屏 JS gzip <200KB
- 只改自己任务卡声明的目录；契约疑义停下问主线程
