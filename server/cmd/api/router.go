package main

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/httpx"
	"piaoju/internal/platform/token"
)

// newRouter 组装全局中间件与路由骨架。
//
// S2-S5 模块挂载方式：在下方两个「挂载点」注释处各加一行 Mount/Get 即可，
// 模块构造器需要的 *sql.DB / *token.Manager 从本函数入参接线。
func newRouter(conn *sql.DB, tm *token.Manager) http.Handler {
	_ = conn // 现阶段仅透传给后续模块构造器（S2-S5）使用

	r := chi.NewRouter()
	r.Use(middleware.RequestLog)
	r.Use(middleware.Recover)
	r.Use(middleware.CORS)

	// 健康检查：不查 DB，起服即 up（K8s/compose 探活用）。
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		httpx.OK(w, map[string]string{"status": "up"})
	})

	r.Route("/api/v1", func(api chi.Router) {
		// ── 公开路由挂载点（不挂 Auth；仅 S2 的 /auth/* 允许放这里）─────────
		// S2 示例: api.Mount("/auth", auth.Routes(authSvc))

		// ── 认证路由：统一挂 Auth，handler 内用 middleware.UID(ctx) 取 userID ──
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))

			// Auth 链路自检端点（S1 交付验证用，之后保留无妨）。
			sec.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
				httpx.OK(w, map[string]any{"pong": true, "uid": middleware.UID(r.Context())})
			})

			// ── 业务模块挂载点（S3/S4/S5 在此追加）────────────────────────
			// S3 示例: sec.Mount("/transactions", transaction.Routes(txSvc))
			//          sec.Mount("/stats", stats.Routes(statsSvc))
			// S4 示例: sec.Mount("/tickets", ticket.Routes(ticketSvc))
			//          sec.Mount("/uploads", upload.Routes(uploadSvc))
			// S5 示例: sec.Mount("/sync", syncmod.Routes(syncSvc))
		})
	})

	// S4 注意：契约 §6 的静态文件 GET /uploads/{path}（带签名参数）挂在根路由，
	// 不在 /api/v1 下、不挂 Auth：r.Get("/uploads/*", upload.Serve(...))

	return r
}
