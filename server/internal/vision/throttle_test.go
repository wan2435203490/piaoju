package vision

// 准入控制单测：滑动窗口配额算法（注入时钟）+ 服务层超额直接 42901 不发起上游调用。

import (
	"context"
	"testing"
	"time"
)

// 滑动窗口：窗口内放行 max 次，第 max+1 次拒绝；窗口滑过后自然恢复。
func TestSlidingLimiter(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newSlidingLimiter(3, time.Minute, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !l.allow("7") {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	if l.allow("7") {
		t.Fatal("4th call within window should be denied")
	}
	// 不同 key 独立配额。
	if !l.allow("8") {
		t.Fatal("other user should have its own budget")
	}
	// 窗口滑过 → 恢复。
	now = now.Add(time.Minute + time.Second)
	if !l.allow("7") {
		t.Fatal("call after window should be allowed again")
	}
}

// 超过 per-uid 配额 → 42901，且在读图/调用上游 LLM 前拦截（不消耗付费额度）。
func TestRecognizeQuotaExceeded(t *testing.T) {
	llm := &fakeLLM{out: goodOutput()}
	s, mock, rel := newTestService(t, llm)
	expectAttachment(mock, rel) // 归属校验仍先跑；配额闸在其后、上游调用之前

	// 耗尽本分钟配额（触发懒初始化后直接打满 perMin）。
	s.admit()
	key := uidKey(uidA)
	for i := 0; i < visionRateMax; i++ {
		s.perMin.allow(key)
	}

	_, err := s.Recognize(context.Background(), uidA, attA)
	wantCode(t, err, codeRateLimited)
	if llm.called != 0 {
		t.Fatalf("llm called %d times, want 0", llm.called)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
