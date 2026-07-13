package ticket

import (
	"strings"
	"testing"

	"piaoju/internal/platform/apperr"
)

func strp(s string) *string     { return &s }
func intp(n int) *int           { return &n }
func i64p(n int64) *int64       { return &n }
func idsp(ns ...int64) *[]int64 { return &ns }

func validBody() body {
	return body{
		ID:          strp(tkID),
		Kind:        strp("movie"),
		Title:       strp("沙丘2"),
		EventTime:   strp("2026-07-10T12:30:00Z"),
		AmountCents: i64p(4500),
		CategoryID:  i64p(3),
		OccurredAt:  strp("2026-07-10T12:00:00Z"),
	}
}

func TestParseCreateDefaults(t *testing.T) {
	d, err := parseCreate(validBody())
	if err != nil {
		t.Fatalf("parseCreate: %v", err)
	}
	if d.PaymentMethod != "other" {
		t.Fatalf("paymentMethod = %q, want default other", d.PaymentMethod)
	}
	if d.Venue != "" || d.Seat != "" || d.Memo != "" || d.Rating != 0 {
		t.Fatalf("optional fields not zero: %+v", d)
	}
	if d.Extra == nil || d.Extra["cinema"] != "" {
		t.Fatalf("extra should be full shape with empty values, got %v", d.Extra)
	}
	if !d.EventTime.Equal(evtTime) || !d.OccurredAt.Equal(occTime) {
		t.Fatalf("times = %v / %v", d.EventTime, d.OccurredAt)
	}
}

func TestParseCreateNormalizesID(t *testing.T) {
	in := validBody()
	in.ID = strp("  " + strings.ToUpper(tkID) + " ")
	d, err := parseCreate(in)
	if err != nil {
		t.Fatalf("parseCreate: %v", err)
	}
	if d.ID != tkID {
		t.Fatalf("id = %q, want lowercased trimmed %q", d.ID, tkID)
	}
}

func TestParseCreateRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*body)
		code int
	}{
		{"missing id", func(b *body) { b.ID = nil }, apperr.CodeInvalidParam},
		{"bad uuid", func(b *body) { b.ID = strp("not-a-uuid") }, apperr.CodeInvalidParam},
		{"missing kind", func(b *body) { b.Kind = nil }, apperr.CodeInvalidParam},
		{"bad kind", func(b *body) { b.Kind = strp("concert") }, apperr.CodeUnsupportedEnum},
		{"missing title", func(b *body) { b.Title = nil }, apperr.CodeInvalidParam},
		{"blank title", func(b *body) { b.Title = strp("   ") }, apperr.CodeInvalidParam},
		{"title too long", func(b *body) { b.Title = strp(strings.Repeat("片", maxTitleLen+1)) }, apperr.CodeInvalidParam},
		{"missing eventTime", func(b *body) { b.EventTime = nil }, apperr.CodeInvalidParam},
		{"bad eventTime", func(b *body) { b.EventTime = strp("2026-07-10 12:30") }, apperr.CodeInvalidParam},
		{"rating out of range", func(b *body) { b.Rating = intp(6) }, apperr.CodeInvalidParam},
		{"negative amount", func(b *body) { b.AmountCents = i64p(-1) }, apperr.CodeInvalidParam},
		{"missing amount", func(b *body) { b.AmountCents = nil }, apperr.CodeInvalidParam},
		{"zero category", func(b *body) { b.CategoryID = i64p(0) }, apperr.CodeInvalidParam},
		{"bad payment", func(b *body) { b.PaymentMethod = strp("bitcoin") }, apperr.CodeUnsupportedEnum},
		{"missing occurredAt", func(b *body) { b.OccurredAt = nil }, apperr.CodeInvalidParam},
		{"bad updatedAt", func(b *body) { b.UpdatedAt = strp("yesterday") }, apperr.CodeInvalidParam},
		{"extra unknown field", func(b *body) { b.Extra = map[string]any{"director": "维伦纽瓦"} }, apperr.CodeInvalidParam},
		{"non-positive attachment id", func(b *body) { b.AttachmentIDs = idsp(0) }, apperr.CodeInvalidParam},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validBody()
			tc.mut(&in)
			_, err := parseCreate(in)
			wantCode(t, err, tc.code)
		})
	}
}

func TestParsePatchBodyIDMustMatchPath(t *testing.T) {
	_, err := parsePatch(tkID, body{ID: strp(otherT)})
	wantCode(t, err, apperr.CodeInvalidParam)

	// 大小写/空白归一化后一致 → 通过。
	if _, err := parsePatch(tkID, body{ID: strp(strings.ToUpper(tkID))}); err != nil {
		t.Fatalf("normalized id should match: %v", err)
	}
}

func TestParsePatchNilMeansAbsent(t *testing.T) {
	p, err := parsePatch(tkID, body{})
	if err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	if p.Kind != nil || p.Title != nil || p.AmountCents != nil || p.Extra != nil || p.AttachmentIDs != nil {
		t.Fatalf("all fields should be nil: %+v", p)
	}
}

func TestParsePatchValidates(t *testing.T) {
	if _, err := parsePatch(tkID, body{Kind: strp("gig")}); err == nil {
		t.Fatal("bad kind should fail")
	}
	if _, err := parsePatch(tkID, body{Title: strp(" ")}); err == nil {
		t.Fatal("blank title should fail")
	}
	if _, err := parsePatch(tkID, body{AmountCents: i64p(-5)}); err == nil {
		t.Fatal("negative amount should fail")
	}
	if _, err := parsePatch(tkID, body{Rating: intp(-1)}); err == nil {
		t.Fatal("negative rating should fail")
	}
}

func TestNormalizeAttachmentIDsDedupeKeepOrder(t *testing.T) {
	got, err := normalizeAttachmentIDs([]int64{3, 1, 3, 2, 1})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	want := []int64{3, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestNormalizeAttachmentIDsTooMany(t *testing.T) {
	ids := make([]int64, maxAttachments+1)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	_, err := normalizeAttachmentIDs(ids)
	wantCode(t, err, apperr.CodeInvalidParam)
}
