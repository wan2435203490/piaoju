package transaction

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

const (
	defaultLimit = 50 // 契约 §4：limit=50
	maxLimit     = 100
)

// Routes 契约 §4 /api/v1/transactions。主线程挂在 Auth 组下：
//
//	sec.Mount("/transactions", transaction.Routes(conn))
func Routes(db *sql.DB) chi.Router {
	h := &handler{svc: &service{db: db, now: time.Now}}
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Patch("/{id}", h.patch)
	r.Delete("/{id}", h.remove)
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

func pathID(r *http.Request) string {
	return strings.ToLower(strings.TrimSpace(chi.URLParam(r, "id")))
}

func decodeBody(r *http.Request) (body, error) {
	var in body
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return in, apperr.New(apperr.CodeInvalidParam, "invalid json body")
	}
	return in, nil
}

// parseMonth 解析 ?month=2026-07 为 UTC 月边界 [start, end)（月边界按 UTC 计，conventions §1）。
func parseMonth(s string) (start, end time.Time, err error) {
	t, perr := time.Parse("2006-01", s)
	if perr != nil {
		return time.Time{}, time.Time{}, badParam("month must be YYYY-MM (e.g. 2026-07)")
	}
	start = t.UTC()
	return start, start.AddDate(0, 1, 0), nil
}

// list GET /?month=&categoryId=&direction=&cursor=&limit=50（occurred_at DESC keyset 分页）。
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	f := listFilter{limit: defaultLimit}
	q := r.URL.Query()

	if ms := q.Get("month"); ms != "" {
		start, end, err := parseMonth(ms)
		if err != nil {
			httpx.Err(w, err)
			return
		}
		f.hasMonth, f.monthStart, f.monthEnd = true, start, end
	}
	if cs := q.Get("categoryId"); cs != "" {
		id, err := strconv.ParseInt(cs, 10, 64)
		if err != nil || id < 1 {
			httpx.Err(w, badParam("categoryId must be a positive id"))
			return
		}
		f.categoryID = id
	}
	if dir := q.Get("direction"); dir != "" {
		if !validDirections[dir] {
			httpx.Err(w, badEnum("unsupported direction %q", dir))
			return
		}
		f.direction = dir
	}
	if ls := q.Get("limit"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n < 1 || n > maxLimit {
			httpx.Err(w, badParam("limit must be within 1-%d", maxLimit))
			return
		}
		f.limit = n
	}
	if cs := q.Get("cursor"); cs != "" {
		t, id, err := decodeCursor(cs)
		if err != nil {
			httpx.Err(w, err)
			return
		}
		f.hasCursor, f.curTime, f.curID = true, t, id
	}

	res, err := h.svc.list(r.Context(), userID, f)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, res)
}

// create POST /（TransactionInput，id 客户端 UUID，幂等 upsert）→ data: 完整 Transaction（契约 v1.1）。
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	in, err := decodeBody(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	d, err := parseCreate(in)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	t, err := h.svc.create(r.Context(), userID, d)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, t)
}

// patch PATCH /{id}（Partial<TransactionInput>，direction 不可改）→ data: 完整 Transaction。
func (h *handler) patch(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	in, err := decodeBody(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	id := pathID(r)
	p, err := parsePatch(id, in)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	t, err := h.svc.patch(r.Context(), userID, id, p)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, t)
}

// remove DELETE /{id}（软删；票据关联 → 40001）→ data: null（契约 v1.1）。
func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	if err := h.svc.remove(r.Context(), userID, pathID(r)); err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, nil)
}
