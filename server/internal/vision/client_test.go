package vision

// client 层：只测错误映射与 schema 构造，不打真 API。

import (
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"piaoju/internal/platform/apperr"
)

func rateLimitErr() error { return &anthropic.Error{StatusCode: 429} }

func TestMapLLMError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int // 0 = 不是 apperr（走 50000 兜底）
	}{
		{"rate limited", &anthropic.Error{StatusCode: 429}, codeRateLimited},
		{"overloaded", &anthropic.Error{StatusCode: 529}, codeRateLimited},
		{"bad request", &anthropic.Error{StatusCode: 400}, 0},
		{"transport", errors.New("dial tcp: timeout"), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapLLMError(c.err)
			var ae *apperr.Error
			if c.want == 0 {
				if errors.As(got, &ae) {
					t.Fatalf("err = %v, want plain error (→50000)", got)
				}
				return
			}
			if !errors.As(got, &ae) || ae.Code != c.want {
				t.Fatalf("err = %v, want apperr %d", got, c.want)
			}
		})
	}
}
