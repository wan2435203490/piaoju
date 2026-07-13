package auth

import (
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	cur := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(5, time.Minute, func() time.Time { return cur })

	for i := range 5 {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d must pass", i+1)
		}
	}
	if l.allow("1.2.3.4") {
		t.Fatal("6th attempt within window must be blocked")
	}
	// 其他 IP 不受影响。
	if !l.allow("5.6.7.8") {
		t.Fatal("different key must not be affected")
	}
	// 窗口滑过后恢复。
	cur = cur.Add(61 * time.Second)
	if !l.allow("1.2.3.4") {
		t.Fatal("attempt after window must pass again")
	}
}

// TestRateLimiterBlockedNotCounted 被拒请求不记账：窗口过后立即恢复满额度。
func TestRateLimiterBlockedNotCounted(t *testing.T) {
	cur := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(2, time.Minute, func() time.Time { return cur })

	l.allow("k")
	l.allow("k")
	for range 10 { // 连续被拒不延长封禁
		if l.allow("k") {
			t.Fatal("must stay blocked within window")
		}
	}
	cur = cur.Add(time.Minute + time.Second)
	if !l.allow("k") {
		t.Fatal("must recover right after window despite blocked attempts")
	}
}

// TestRateLimiterSweep 全表清扫剔除窗口外的 key，防内存无界增长。
func TestRateLimiterSweep(t *testing.T) {
	cur := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	l := newRateLimiter(5, time.Minute, func() time.Time { return cur })
	l.allow("stale")
	l.allow("fresh")
	cur = cur.Add(2 * time.Minute)
	l.allow("fresh") // fresh 有窗口内新记录

	l.mu.Lock()
	l.sweep(cur.Add(-time.Minute))
	if _, ok := l.hits["stale"]; ok {
		t.Fatal("stale key must be swept")
	}
	if _, ok := l.hits["fresh"]; !ok {
		t.Fatal("fresh key must survive sweep")
	}
	l.mu.Unlock()
}
