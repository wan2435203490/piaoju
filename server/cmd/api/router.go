package main

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/auth"
	"piaoju/internal/category"
	"piaoju/internal/importer"
	"piaoju/internal/middleware"
	"piaoju/internal/platform/config"
	"piaoju/internal/platform/httpx"
	"piaoju/internal/platform/token"
	"piaoju/internal/stats"
	syncmod "piaoju/internal/sync" // alias：包名 sync 与 stdlib 撞名
	"piaoju/internal/ticket"
	"piaoju/internal/transaction"
	"piaoju/internal/upload"
	"piaoju/internal/vision"
)

// newRouter 组装全局中间件与业务模块路由。
// 各模块构造器签名见其 handler.go 的 Routes 文档注释；
// ticket/upload/Serve 必须共用同一签名密钥（cfg.JWTSecret）。
func newRouter(conn *sql.DB, tm *token.Manager, cfg config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestLog)
	r.Use(middleware.Recover)
	r.Use(middleware.CORS)

	// 健康检查：不查 DB，起服即 up（K8s/compose 探活用）。
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.OK(w, map[string]string{"status": "up"})
	})

	r.Route("/api/v1", func(api chi.Router) {
		// 公开路由（不挂 Auth）
		api.Mount("/auth", auth.Routes(conn, tm, cfg.AccessTTL, cfg.RefreshTTL))

		// 认证路由：统一挂 Auth，handler 内用 middleware.UID(ctx) 取 userID
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))

			// Auth 链路自检端点（S1 交付验证用，之后保留无妨）。
			sec.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
				httpx.OK(w, map[string]any{"pong": true, "uid": middleware.UID(r.Context())})
			})

			sec.Mount("/categories", category.Routes(conn))
			sec.Mount("/transactions", transaction.Routes(conn))
			sec.Mount("/stats", stats.Routes(conn))
			// 识票（契约 §6.1）：静态路由必须先于 /tickets 挂载，否则被 /tickets/{id} 抢走。
			// uploadDir 与 upload.Routes 同一个（读的是同一批落盘图片）。
			sec.Mount("/tickets/recognize", vision.Routes(conn, cfg.UploadDir))
			sec.Mount("/tickets", ticket.Routes(conn, cfg.JWTSecret))
			sec.Mount("/uploads", upload.Routes(conn, cfg.UploadDir, cfg.UploadMaxMB, cfg.JWTSecret))
			sec.Mount("/imports", importer.Routes(conn)) // 账单导入（契约 §6.2）
			// sync 的 secret 必须与 ticket/upload 同一把（pull 下发附件签名 URL）
			sec.Mount("/sync", syncmod.Routes(conn, cfg.JWTSecret))
		})
	})

	// 契约 §6：签名 URL 静态文件，不在 /api/v1 下、不挂 Auth。
	r.Get("/uploads/*", upload.Serve(cfg.UploadDir, cfg.JWTSecret))

	return r
}
