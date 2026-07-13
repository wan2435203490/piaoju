package ticket

import (
	"strings"
	"testing"

	"piaoju/internal/platform/apperr"
)

func TestNormalizeExtraFullShape(t *testing.T) {
	got, err := normalizeExtra("train", map[string]any{"trainNo": "G102", "fromStation": "北京南"})
	if err != nil {
		t.Fatalf("normalizeExtra: %v", err)
	}
	want := []string{"trainNo", "fromStation", "toStation", "departTime", "arriveTime", "seatClass"}
	if len(got) != len(want) {
		t.Fatalf("got %d keys %v, want full shape %v", len(got), got, want)
	}
	if got["trainNo"] != "G102" || got["toStation"] != "" {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeExtraRejects(t *testing.T) {
	cases := []struct {
		name string
		kind string
		raw  map[string]any
	}{
		{"unknown field", "movie", map[string]any{"director": "x"}},
		{"cross-kind field", "movie", map[string]any{"trainNo": "G102"}},
		{"non-string value", "movie", map[string]any{"cinema": 42}},
		{"value too long", "movie", map[string]any{"cinema": strings.Repeat("长", maxExtraValueLen+1)}},
		{"other kind allows nothing", "other", map[string]any{"cinema": "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeExtra(tc.kind, tc.raw)
			wantCode(t, err, apperr.CodeInvalidParam)
		})
	}
}

func TestReconcileExtraDropsEmptyAndRevalidates(t *testing.T) {
	// movie → show：旧 extra 只剩空值 → 清空重建为 show 形状。
	got, err := reconcileExtra("show", map[string]string{"cinema": "", "hall": "", "filmFormat": ""})
	if err != nil {
		t.Fatalf("reconcileExtra: %v", err)
	}
	if got["tour"] != "" || got["session"] != "" || got["zone"] != "" || len(got) != 3 {
		t.Fatalf("got %v, want empty show shape", got)
	}

	// 非空的旧字段塞不进新 kind → 40001，要求显式传 extra。
	_, err = reconcileExtra("show", map[string]string{"cinema": "万达"})
	wantCode(t, err, apperr.CodeInvalidParam)
}

func TestFillExtraDefaults(t *testing.T) {
	got := fillExtraDefaults("flight", map[string]string{"flightNo": "CA1234"})
	if got["flightNo"] != "CA1234" {
		t.Fatalf("existing value lost: %v", got)
	}
	for _, k := range []string{"airline", "fromAirport", "toAirport", "departTime", "arriveTime", "cabin"} {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing default key %q: %v", k, got)
		}
	}
	if got := fillExtraDefaults("movie", nil); got == nil || len(got) != 3 {
		t.Fatalf("nil map should become full movie shape, got %v", got)
	}
}
