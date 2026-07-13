package category

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

// Routes 契约 §3 /api/v1/categories。主线程挂在 Auth 组下：
//
//	sec.Mount("/categories", category.Routes(conn))
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

var validKinds = map[string]bool{"expense": true, "income": true}

// input POST 请求体（契约 §3：{ name, icon, kind }）。
type input struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
	Kind string `json:"kind"`
}

// patchInput PATCH 请求体：字段可缺省（nil = 不改）。
type patchInput struct {
	Name *string `json:"name"`
	Icon *string `json:"icon"`
	Sort *int    `json:"sort"`
}

func uid(r *http.Request) (int64, error) {
	id := middleware.UID(r.Context())
	if id <= 0 {
		return 0, apperr.New(apperr.CodeTokenExpired, "unauthorized")
	}
	return id, nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		return 0, apperr.New(apperr.CodeInvalidParam, "id must be a positive integer")
	}
	return id, nil
}

func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 32 {
		return "", apperr.New(apperr.CodeInvalidParam, "name is required, at most 32 chars")
	}
	return name, nil
}

func validateIcon(icon string) (string, error) {
	icon = strings.TrimSpace(icon)
	if len(icon) > 16 {
		return "", apperr.New(apperr.CodeInvalidParam, "icon at most 16 bytes (single emoji)")
	}
	return icon, nil
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	items, err := h.svc.list(r.Context(), userID)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, map[string]any{"items": items})
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	var in input
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.Err(w, apperr.New(apperr.CodeInvalidParam, "invalid json body"))
		return
	}
	if in.Name, err = validateName(in.Name); err != nil {
		httpx.Err(w, err)
		return
	}
	if in.Icon, err = validateIcon(in.Icon); err != nil {
		httpx.Err(w, err)
		return
	}
	if !validKinds[in.Kind] {
		httpx.Err(w, apperr.New(apperr.CodeUnsupportedEnum, "kind must be expense or income"))
		return
	}
	c, err := h.svc.create(r.Context(), userID, in)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, c)
}

func (h *handler) patch(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	var p patchInput
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		httpx.Err(w, apperr.New(apperr.CodeInvalidParam, "invalid json body"))
		return
	}
	if p.Name != nil {
		v, err := validateName(*p.Name)
		if err != nil {
			httpx.Err(w, err)
			return
		}
		p.Name = &v
	}
	if p.Icon != nil {
		v, err := validateIcon(*p.Icon)
		if err != nil {
			httpx.Err(w, err)
			return
		}
		p.Icon = &v
	}
	c, err := h.svc.patch(r.Context(), userID, id, p)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, c)
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request) {
	userID, err := uid(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	if err := h.svc.remove(r.Context(), userID, id); err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, nil)
}
