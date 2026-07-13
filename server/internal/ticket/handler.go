package ticket

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
	defaultLimit = 20 // 契约 §5：limit=20
	maxLimit     = 100
)

// Routes 契约 §5 /api/v1/tickets。主线程挂在 Auth 组下：
//
//	sec.Mount("/tickets", ticket.Routes(conn, cfg.JWTSecret))
//
// uploadSecret 用于生成附件签名 URL，必须与 upload.Routes / upload.Serve 传同一个 secret。
func Routes(db *sql.DB, uploadSecret string) chi.Router {
	h := &handler{svc: &service{db: db, secret: uploadSecret, now: time.Now}}
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Get("/{id}", h.get)
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

// list GET /?kind=&year=&cursor=&limit=20（event_time DESC keyset 分页）。
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	f := listFilter{limit: defaultLimit}
	q := r.URL.Query()

	if kind := q.Get("kind"); kind != "" {
		if !validKinds[kind] {
			httpx.Err(w, badEnum("unsupported kind %q", kind))
			return
		}
		f.kind = kind
	}
	if ys := q.Get("year"); ys != "" {
		y, err := strconv.Atoi(ys)
		if err != nil || y < 1 || y > 9999 {
			httpx.Err(w, badParam("year must be a 4-digit year"))
			return
		}
		f.year = y
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

// get GET /{id}。
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	t, err := h.svc.get(r.Context(), userID, pathID(r))
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, t)
}

// create POST /（TicketInput，id 客户端 UUID，幂等 upsert）→ data: 完整 Ticket（契约 v1.1）。
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

// patch PATCH /{id}（Partial<TicketInput>）→ data: 完整 Ticket。
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

// remove DELETE /{id}（软删票 + 关联交易）→ data: null（契约 v1.1）。
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
