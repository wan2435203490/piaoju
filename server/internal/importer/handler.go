// Package importer 契约 §6.2：微信/支付宝账单 CSV 导入预览。
//
// **只有 preview，没有 commit**：解析 + 规则分类 + 查重的结果原样交给客户端，
// 用户勾选后由客户端走 §8 sync/push 写入（离线安全、幂等、复用 LWW）。
// 因此本模块对 transactions 表只读（查重），不写任何数据。
package importer

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

// maxBytes 契约 §6.2：CSV ≤5MB，超限 → 41301。
const maxBytes int64 = 5 << 20

// Routes 契约 §6.2 POST /api/v1/imports/preview。主线程挂在 Auth 组下：
//
//	sec.Mount("/imports", importer.Routes(conn))
func Routes(db *sql.DB) chi.Router {
	h := &handler{svc: &service{db: db}}
	r := chi.NewRouter()
	r.Post("/preview", h.preview)
	return r
}

type handler struct {
	svc *service
}

func errTooLarge() error {
	return apperr.New(apperr.CodeUploadTooLarge, fmt.Sprintf("file exceeds %dMB limit", maxBytes>>20))
}

// preview POST /preview（multipart: file=CSV, source=wechat|alipay）→ data: {items,total,duplicates}。
func (h *handler) preview(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UID(r.Context())
	if uid <= 0 { // 防御：路由未挂 Auth 时不做无主查重
		httpx.Err(w, apperr.New(apperr.CodeTokenExpired, "unauthorized"))
		return
	}

	// 请求体总上限 = 文件上限 + 1MB multipart 头部余量；超限在 FormFile 解析时触发 MaxBytesError。
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1<<20)

	file, _, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Err(w, errTooLarge())
			return
		}
		httpx.Err(w, apperr.New(apperr.CodeInvalidParam, `multipart field "file" is required`))
		return
	}
	defer file.Close()

	source := r.FormValue("source")
	if source == "" {
		httpx.Err(w, apperr.New(apperr.CodeInvalidParam, `multipart field "source" is required`))
		return
	}
	if _, ok := columnAliases[source]; !ok {
		httpx.Err(w, apperr.New(apperr.CodeUnsupportedEnum,
			fmt.Sprintf(`unsupported source %q (want "wechat" or "alipay")`, source)))
		return
	}

	data, err := readAllLimit(file, maxBytes)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Err(w, errTooLarge())
			return
		}
		httpx.Err(w, fmt.Errorf("importer: read multipart file: %w", err))
		return
	}
	if int64(len(data)) > maxBytes {
		httpx.Err(w, errTooLarge())
		return
	}

	res, err := h.svc.preview(r.Context(), uid, source, data)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, res)
}
