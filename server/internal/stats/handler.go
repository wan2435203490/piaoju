package stats

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

// Routes 契约 §7 /api/v1/stats。主线程挂在 Auth 组下：
//
//	sec.Mount("/stats", stats.Routes(conn))
func Routes(db *sql.DB) chi.Router {
	h := &handler{svc: &service{db: db}}
	r := chi.NewRouter()
	r.Get("/monthly", h.monthly)
	r.Get("/tickets", h.tickets)
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

func badParam(format string, a ...any) error {
	return apperr.New(apperr.CodeInvalidParam, fmt.Sprintf(format, a...))
}

// monthly GET /monthly?month=2026-07（必填；月边界按 UTC 计）。
func (h *handler) monthly(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	ms := r.URL.Query().Get("month")
	if ms == "" {
		httpx.Err(w, badParam("month is required (YYYY-MM)"))
		return
	}
	t, err := time.Parse("2006-01", ms)
	if err != nil {
		httpx.Err(w, badParam("month must be YYYY-MM (e.g. 2026-07)"))
		return
	}
	start := t.UTC()
	res, err := h.svc.monthly(r.Context(), userID, start, start.AddDate(0, 1, 0))
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, res)
}

// tickets GET /tickets?year=2026（必填；年边界按 UTC 计，口径同 GET /tickets?year=）。
func (h *handler) tickets(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	ys := r.URL.Query().Get("year")
	if ys == "" {
		httpx.Err(w, badParam("year is required"))
		return
	}
	y, err := strconv.Atoi(ys)
	if err != nil || y < 1 || y > 9999 {
		httpx.Err(w, badParam("year must be a 4-digit year"))
		return
	}
	start := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
	res, err := h.svc.tickets(r.Context(), userID, start, start.AddDate(1, 0, 0))
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, res)
}
