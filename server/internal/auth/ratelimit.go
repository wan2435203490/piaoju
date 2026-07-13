package auth

import (
	"sync"
	"time"
)

// 认证端点限流：5 次/分钟/IP（任务卡 S2，login/register/refresh 各自独立配额）。
// 进程内滑动窗口，单实例部署够用；多实例水平扩容时需换共享存储（Redis）——见交付说明。
const (
	authRateMax    = 5
	authRateWindow = time.Minute

	// maxTrackedKeys 超过即触发一次全表清扫，防恶意海量 IP 撑爆内存。
	maxTrackedKeys = 10000
)

// rateLimiter 按 key（客户端 IP）滑动窗口计数。并发安全。
type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time // 测试注入固定时钟
	hits   map[string][]time.Time
}

func newRateLimiter(max int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{max: max, window: window, now: now, hits: make(map[string][]time.Time)}
}

// allow 窗口内不足 max 次则记账放行；超限拒绝（被拒请求不记账，窗口过后自然恢复）。
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)

	h := l.hits[key]
	i := 0
	for i < len(h) && !h[i].After(cutoff) {
		i++
	}
	h = h[i:]

	if len(h) >= l.max {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)

	if len(l.hits) > maxTrackedKeys {
		l.sweep(cutoff)
	}
	return true
}

// sweep 移除窗口内已无记录的 key（调用方持锁）。
func (l *rateLimiter) sweep(cutoff time.Time) {
	for k, h := range l.hits {
		if len(h) == 0 || !h[len(h)-1].After(cutoff) {
			delete(l.hits, k)
		}
	}
}
