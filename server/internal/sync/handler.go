package sync

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

const (
	defaultLimit = 200 // 契约 §8：limit=200
	maxLimit     = 500
)

// Routes 契约 §8 /api/v1/sync。主线程挂在 Auth 组下（secret 与 ticket/upload 同一把 JWTSecret，
// pull 下发附件签名 URL 用）：
//
//	import syncmod "piaoju/internal/sync"
//	sec.Mount("/sync", syncmod.Routes(conn, cfg.JWTSecret))
func Routes(db *sql.DB, secret string) chi.Router {
	h := &handler{svc: &service{db: db, secret: secret, now: time.Now}}
	r := chi.NewRouter()
	r.Post("/push", h.push)
	r.Get("/pull", h.pull)
	return r
}

type handler struct {
	svc *service
}

// uid 取 Auth 注入的 userID；拿不到说明路由没挂 Auth（防御，不 panic）。
func uid(r *http.Request) (int64, error) {
	id := middleware.UID(r.Context())
	if id <= 0 {
		return 0, apperr.New(apperr.CodeTokenExpired, "unauthorized")
	}
	return id, nil
}

// push POST /push：批量应用离线变更。整请求只在 body 不可解析 / changes 超量时失败；
// 单条变更的校验或写入失败只体现在自己的 result（status=error + code），不影响其余条目。
func (h *handler) push(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	var in pushBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.Err(w, apperr.New(apperr.CodeInvalidParam, "invalid json body"))
		return
	}
	if len(in.Changes) > maxChanges {
		httpx.Err(w, badParam("too many changes (max %d)", maxChanges))
		return
	}
	httpx.OK(w, h.svc.push(r.Context(), userID, in.Changes))
}

// pull GET /pull?since=<serverCursor>&limit=200：按 (updated_at, id) 单调游标增量下发（含墓碑）。
func (h *handler) pull(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	f := pullFilter{limit: defaultLimit}
	q := r.URL.Query()

	if ls := q.Get("limit"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n < 1 || n > maxLimit {
			httpx.Err(w, badParam("limit must be within 1-%d", maxLimit))
			return
		}
		f.limit = n
	}
	if cs := q.Get("since"); cs != "" {
		t, id, err := decodeCursor(cs)
		if err != nil {
			httpx.Err(w, err)
			return
		}
		f.hasCursor, f.curTime, f.curID, f.rawCursor = true, t, id, cs
	}

	res, err := h.svc.pull(r.Context(), userID, f)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, res)
}
