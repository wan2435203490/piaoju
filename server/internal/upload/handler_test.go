package upload

// handler 层测试：真 HTTP（chi + middleware.Auth + JWT）+ sqlmock + t.TempDir 写盘，
// 覆盖 S4 DoD 的超限/非图片拒收与落盘落库链路。

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/token"
)

const httpUID int64 = 7

func newTestServer(t *testing.T, maxMB int) (*httptest.Server, sqlmock.Sqlmock, string, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tm := token.NewManager("upload-test-secret")
	access, err := tm.Sign(httpUID, time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	dir := t.TempDir()
	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))
			sec.Mount("/uploads", Routes(db, dir, maxMB, "upload-test-secret")) // 与主线程挂载方式一致
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, mock, access, dir
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func postMultipart(t *testing.T, srv *httptest.Server, access, field string, payload []byte) (int, envelope) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, "photo.bin")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	mw.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/uploads/", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return resp.StatusCode, env
}

func TestUploadUnauthorized(t *testing.T) {
	srv, _, _, _ := newTestServer(t, 10)
	status, env := postMultipart(t, srv, "", "file", []byte("x"))
	if status != http.StatusUnauthorized || env.Code != 40101 {
		t.Fatalf("status/code = %d/%d, want 401/40101", status, env.Code)
	}
}

// 超过 PIAOJU_UPLOAD_MAX_MB → 41301（maxMB=1，发 3MB）。
func TestUploadRejectsOversize(t *testing.T) {
	srv, _, access, _ := newTestServer(t, 1)
	status, env := postMultipart(t, srv, access, "file", bytes.Repeat([]byte{0xCC}, 3<<20))
	if status != http.StatusRequestEntityTooLarge || env.Code != 41301 {
		t.Fatalf("status/code = %d/%d, want 413/41301", status, env.Code)
	}
}

func TestUploadRejectsNonImage(t *testing.T) {
	srv, _, access, _ := newTestServer(t, 10)
	_, env := postMultipart(t, srv, access, "file", []byte("plain text pretending to be a photo"))
	if env.Code != 41301 {
		t.Fatalf("code = %d, want 41301", env.Code)
	}
}

func TestUploadMissingFileField(t *testing.T) {
	srv, _, access, _ := newTestServer(t, 10)
	_, env := postMultipart(t, srv, access, "not_file", []byte("x"))
	if env.Code != 40001 {
		t.Fatalf("code = %d, want 40001", env.Code)
	}
}

// 合法 webp → 压缩写盘（原图+缩略图）→ attachments 落库 → 返回签名 URL。
func TestUploadHappyPath(t *testing.T) {
	srv, mock, access, dir := newTestServer(t, 10)

	mock.ExpectExec(regexp.QuoteMeta(sqlInsertAttachment)).
		WithArgs(httpUID, sqlmock.AnyArg(), sqlmock.AnyArg(), 100, 50, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(42, 1))

	status, env := postMultipart(t, srv, access, "file", genWebP(100, 50, 200, 120, 40, 255))
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("status/code = %d/%d (msg %q), want 200/0", status, env.Code, env.Message)
	}
	var att Attachment
	if err := json.Unmarshal(env.Data, &att); err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	if att.ID != 42 || att.W != 100 || att.H != 50 {
		t.Fatalf("attachment = %+v", att)
	}
	for _, u := range []string{att.URL, att.ThumbURL} {
		if !regexp.MustCompile(`^/uploads/7/[0-9a-f-]+(_thumb)?\.jpg\?exp=\d+&sig=[0-9a-f]{64}$`).MatchString(u) {
			t.Fatalf("url %q not a signed upload path", u)
		}
	}

	files, err := os.ReadDir(filepath.Join(dir, "7"))
	if err != nil || len(files) != 2 {
		t.Fatalf("user dir should contain original + thumbnail, got %v (err %v)", files, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
