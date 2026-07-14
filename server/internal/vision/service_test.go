package vision

// service 层单测：与 ticket/transaction 同款 go-sqlmock 方案；LLM 走 fake（不打真 API）。
// 覆盖：附件不属于本人→40401、未配 key→50001、上游限流→42901、
// 正常识别字段映射、模型返回非法 kind / 负数金额→拒绝、user_id 隔离（SQL 参数含 uid）。

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"piaoju/internal/platform/apperr"
)

const (
	uidA int64 = 7
	attA int64 = 42
)

// fakeLLM 注入式假客户端。
type fakeLLM struct {
	out    *modelOutput
	err    error
	called int
}

func (f *fakeLLM) recognize(_ context.Context, _ string, img []byte) (*modelOutput, error) {
	f.called++
	if len(img) == 0 {
		return nil, errors.New("fakeLLM: empty image")
	}
	return f.out, f.err
}

// newTestService 建服务 + 一张落盘的假图片（内容不重要，fake 不解码）。
func newTestService(t *testing.T, llm recognizer) (*service, sqlmock.Sqlmock, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	rel := "7/abc.jpg"
	if err := os.MkdirAll(filepath.Join(dir, "7"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "7", "abc.jpg"), []byte("\xff\xd8\xff jpeg bytes"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return &service{db: db, dir: dir, llm: llm}, mock, rel
}

func expectAttachment(mock sqlmock.Sqlmock, rel string) {
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectAttachment)).
		WithArgs(attA, uidA). // user_id 隔离：查询必带 uid
		WillReturnRows(sqlmock.NewRows([]string{"file_path"}).AddRow(rel))
}

func wantCode(t *testing.T, err error, code int) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *apperr.Error with code %d", err, code)
	}
	if ae.Code != code {
		t.Fatalf("code = %d, want %d", ae.Code, code)
	}
}

func goodOutput() *modelOutput {
	return &modelOutput{
		Kind:      "movie",
		Title:     "沙丘3",
		Venue:     "万达影城",
		EventTime: "2026-07-12T19:30:00+08:00",
		Seat:      "5排8座",
		Extra: map[string]string{
			"cinema": "万达影城(五角场店)", "hall": "3号厅", "filmFormat": "IMAX",
			// 非本 kind 的并集字段：应被丢弃
			"trainNo": "G1234", "airline": "CA",
		},
		AmountCents: 6850,
		Confidence:  0.92,
	}
}

// 附件不属于本人（或不存在）→ 40401，且不调 LLM。
func TestRecognizeAttachmentNotOwned(t *testing.T) {
	llm := &fakeLLM{out: goodOutput()}
	s, mock, _ := newTestService(t, llm)
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectAttachment)).
		WithArgs(attA, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"file_path"})) // 他人附件 → 无行

	_, err := s.Recognize(context.Background(), uidA, attA)
	wantCode(t, err, apperr.CodeNotFound)
	if llm.called != 0 {
		t.Fatalf("llm called %d times, want 0", llm.called)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 未配置 PIAOJU_LLM_API_KEY（llm == nil）→ 50001，服务其余部分不受影响。
func TestRecognizeNotConfigured(t *testing.T) {
	s, mock, rel := newTestService(t, nil)
	expectAttachment(mock, rel)

	_, err := s.Recognize(context.Background(), uidA, attA)
	wantCode(t, err, codeVisionUnready)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Routes 在未配 key 时仍可挂载（识票是可选增强，绝不能让服务起不来）。
func TestRoutesMountsWithoutAPIKey(t *testing.T) {
	t.Setenv(EnvAPIKey, "")
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	if r := Routes(db, t.TempDir()); r == nil {
		t.Fatal("Routes returned nil without API key")
	}
}

// 上游限流 → 42901（HTTPStatus 由 code/100 推导 = 429）。
func TestRecognizeRateLimited(t *testing.T) {
	llm := &fakeLLM{err: apperr.New(codeRateLimited, "recognize service is rate limited, retry later")}
	s, mock, rel := newTestService(t, llm)
	expectAttachment(mock, rel)

	_, err := s.Recognize(context.Background(), uidA, attA)
	wantCode(t, err, codeRateLimited)
	var ae *apperr.Error
	_ = errors.As(err, &ae)
	if ae.HTTPStatus() != http.StatusTooManyRequests {
		t.Fatalf("HTTPStatus = %d, want 429", ae.HTTPStatus())
	}
}

// 正常识别：字段映射 + extra 按 kind 白名单过滤补全 + 时间归一化 UTC。
func TestRecognizeSuccess(t *testing.T) {
	llm := &fakeLLM{out: goodOutput()}
	s, mock, rel := newTestService(t, llm)
	expectAttachment(mock, rel)

	d, err := s.Recognize(context.Background(), uidA, attA)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if d.Kind != "movie" || d.Title != "沙丘3" || d.Venue != "万达影城" || d.Seat != "5排8座" {
		t.Fatalf("draft = %+v", d)
	}
	if d.AmountCents != 6850 || d.Confidence != 0.92 {
		t.Fatalf("amount/confidence = %d/%v", d.AmountCents, d.Confidence)
	}
	if d.EventTime != "2026-07-12T11:30:00Z" { // RFC3339 UTC
		t.Fatalf("eventTime = %q", d.EventTime)
	}
	want := map[string]string{"cinema": "万达影城(五角场店)", "hall": "3号厅", "filmFormat": "IMAX"}
	if len(d.Extra) != len(want) {
		t.Fatalf("extra = %v, want only movie fields", d.Extra)
	}
	for k, v := range want {
		if d.Extra[k] != v {
			t.Fatalf("extra[%q] = %q, want %q", k, d.Extra[k], v)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 识别不出的字段回零值，不猜：extra 补全为该 kind 的完整空串形状。
func TestRecognizeZeroValues(t *testing.T) {
	llm := &fakeLLM{out: &modelOutput{Kind: "train", EventTime: "看不清", Confidence: 1.5}}
	s, mock, rel := newTestService(t, llm)
	expectAttachment(mock, rel)

	d, err := s.Recognize(context.Background(), uidA, attA)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if d.Title != "" || d.Venue != "" || d.Seat != "" || d.EventTime != "" || d.AmountCents != 0 {
		t.Fatalf("draft not zeroed: %+v", d)
	}
	if d.Confidence != 1 { // 越界置信度夹紧到 [0,1]
		t.Fatalf("confidence = %v, want 1", d.Confidence)
	}
	for _, f := range extraWhitelist["train"] {
		if _, ok := d.Extra[f]; !ok {
			t.Fatalf("extra missing field %q: %v", f, d.Extra)
		}
	}
	if len(d.Extra) != len(extraWhitelist["train"]) {
		t.Fatalf("extra = %v", d.Extra)
	}
}

// 模型返回非法 kind → 拒绝（不落到客户端）。
func TestRecognizeRejectsInvalidKind(t *testing.T) {
	llm := &fakeLLM{out: &modelOutput{Kind: "concert", Confidence: 0.9}}
	s, mock, rel := newTestService(t, llm)
	expectAttachment(mock, rel)

	_, err := s.Recognize(context.Background(), uidA, attA)
	wantCode(t, err, apperr.CodeInternal)
}

// 模型返回负数金额 → 拒绝。
func TestRecognizeRejectsNegativeAmount(t *testing.T) {
	llm := &fakeLLM{out: &modelOutput{Kind: "movie", AmountCents: -1}}
	s, mock, rel := newTestService(t, llm)
	expectAttachment(mock, rel)

	_, err := s.Recognize(context.Background(), uidA, attA)
	wantCode(t, err, apperr.CodeInternal)
}

// attachmentId 缺失/非法 → 40001，不查库。
func TestRecognizeMissingAttachmentID(t *testing.T) {
	s, _, _ := newTestService(t, &fakeLLM{out: goodOutput()})
	_, err := s.Recognize(context.Background(), uidA, 0)
	wantCode(t, err, apperr.CodeInvalidParam)
}

// outputSchema 必须覆盖五种 kind 的 extra 白名单并集。
func TestOutputSchemaCoversWhitelist(t *testing.T) {
	schema := outputSchema()
	props := schema["properties"].(map[string]any)
	extra := props["extra"].(map[string]any)
	fields := extra["properties"].(map[string]any)
	for kind, ks := range extraWhitelist {
		for _, f := range ks {
			if _, ok := fields[f]; !ok {
				t.Fatalf("schema extra missing %q (kind %s)", f, kind)
			}
		}
	}
}
