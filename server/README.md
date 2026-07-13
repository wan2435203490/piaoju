# server

Go 后端（module `piaoju`）。启动：仓库根 `make dev`，或 `cd server && go run ./cmd/api`。
配置走环境变量（`PIAOJU_` 前缀，见根目录 `.env.example`；`PIAOJU_JWT_SECRET` 必填）。

- 迁移：`migrations/` 已内嵌进二进制（embed + golang-migrate），进程启动自动执行，无需手动 migrate。
- sqlc：各模块在 `sqlc.yaml` 的 `sql:` 列表追加条目（模板见该文件注释）后执行 `sqlc generate`；无本地二进制用 `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0 generate`。
- 模块路由挂载点：`cmd/api/router.go`（公开路由 / 认证路由两处注释区）。
- 验证：`go build ./... && go vet ./... && go test ./...`。
