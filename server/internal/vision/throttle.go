package vision

import (
	"strconv"
	"sync"
	"time"
)

// 识票准入控制（security/high）。
//
// 识票每次调用都会发起一次付费 Opus 多模态请求（client.go：Opus4_8、60s 超时），
// 且全体用户共享同一个 PIAOJU_LLM_API_KEY。若不在服务端按用户计量，任一登录用户即可
// 拿到一个合法 attachmentId 后循环重放，烧光共享额度、触发上游 429，使所有用户识票
// 都返回 42901——契约 §6.1 的「超额/限流」由此沦为可被单个攻击者触发的全局故障。
//
// 故在发起上游调用前做两道服务端准入（超限一律 42901，绝不发起上游调用）：
//   - per-uid 滑动窗口配额：每分钟 visionRateMax 次 + 每日 visionDailyMax 次；
//   - 全局并发信号量：最多 visionMaxConcurrent 个在途上游调用，防几百并发各占一个
//     goroutine/连接最长 60s 把服务打满。
//
// auth 包已有同款滑动窗口（auth.rateLimiter），但为其私有类型、跨包不可复用，此处按
// 识票自身配额独立实例化。进程内实现，单实例部署够用；多实例水平扩容时需换共享存储。
const (
	visionRateMax     = 10
	visionRateWindow  = time.Minute
	visionDailyMax    = 100
	visionDailyWindow = 24 * time.Hour

	visionMaxConcurrent = 8

	// visionMaxTrackedUsers 超过即触发一次全表清扫，防海量 uid 撑爆内存（同 auth.maxTrackedKeys）。
	visionMaxTrackedUsers = 10000
)

// uidKey 限流 key：按用户隔离（契约 §2「所有接口按 user 隔离」）。
func uidKey(uid int64) string { return strconv.FormatInt(uid, 10) }

// slidingLimiter 按 key 滑动窗口计数，并发安全（算法同 auth.rateLimiter）。
type slidingLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time // 测试注入固定时钟
	hits   map[string][]time.Time
}

func newSlidingLimiter(max int, window time.Duration, now func() time.Time) *slidingLimiter {
	return &slidingLimiter{max: max, window: window, now: now, hits: make(map[string][]time.Time)}
}

// allow 窗口内不足 max 次则记账放行；超限拒绝（被拒不记账，窗口过后自然恢复）。
func (l *slidingLimiter) allow(key string) bool {
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

	if len(l.hits) > visionMaxTrackedUsers {
		l.sweep(cutoff)
	}
	return true
}

// sweep 移除窗口内已无记录的 key（调用方持锁）。
func (l *slidingLimiter) sweep(cutoff time.Time) {
	for k, h := range l.hits {
		if len(h) == 0 || !h[len(h)-1].After(cutoff) {
			delete(l.hits, k)
		}
	}
}

// admit 懒初始化准入组件（零值 service 可用，直接构造 &service{} 的单测无需改动）。
func (s *service) admit() (perMin, perDay *slidingLimiter, sem chan struct{}) {
	s.limiterOnce.Do(func() {
		s.perMin = newSlidingLimiter(visionRateMax, visionRateWindow, time.Now)
		s.perDay = newSlidingLimiter(visionDailyMax, visionDailyWindow, time.Now)
		s.sem = make(chan struct{}, visionMaxConcurrent)
	})
	return s.perMin, s.perDay, s.sem
}
