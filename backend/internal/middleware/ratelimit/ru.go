package ratelimit

import (
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	readRU         = 1
	writeRU        = 6
	shortWindow    = 5 * time.Second
	longWindow     = 10 * time.Minute
	defaultRURate  = 1000
	defaultRUBurst = 2000
)

// requestSample 保存一次已放行请求的耗时和完成时间。
type requestSample struct {
	cost     time.Duration
	finished time.Time
}

// RULimiter 在服务入口用令牌桶按 RU 限流。
type RULimiter struct {
	mu       sync.Mutex
	tokens   float64
	last     time.Time
	rate     float64
	capacity float64
	samples  []requestSample
}

func NewRULimiter(ratePerSec, capacity float64) *RULimiter {
	return &RULimiter{
		tokens:   capacity,
		last:     time.Now(),
		rate:     ratePerSec,
		capacity: capacity,
	}
}

// ServerRU 挂在路由最外层，保护整个服务。
func ServerRU() gin.HandlerFunc {
	return NewRULimiter(defaultRURate, defaultRUBurst).Middleware()
}

func (l *RULimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cost := writeRU
		if isRead(c) {
			cost = readRU
		}
		if !l.allow(cost) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests",
			})
			return
		}
		start := time.Now()
		c.Next()
		elapsed := time.Since(start)
		finished := time.Now()
		l.record(elapsed, finished)
	}
}

func isRead(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func (l *RULimiter) allow(base int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	cost := float64(base)
	if l.delayRatio(time.Now()) > 1 {
		cost *= 2
	}
	if l.tokens < cost {
		return false
	}
	l.tokens -= cost
	return true
}

func (l *RULimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	if elapsed > 0 {
		l.tokens += elapsed * l.rate
		if l.tokens > l.capacity {
			l.tokens = l.capacity
		}
	}
	l.last = now
}

func (l *RULimiter) delayRatio(now time.Time) float64 {
	short := make([]time.Duration, 0)
	long := make([]time.Duration, 0)
	for _, sample := range l.samples {
		age := now.Sub(sample.finished)
		if age < 0 || age > longWindow {
			continue
		}
		long = append(long, sample.cost)
		if age <= shortWindow {
			short = append(short, sample.cost)
		}
	}
	longP95 := percentile95(long)
	shortP95 := percentile95(short)
	if longP95 == 0 {
		return 0
	}
	return shortP95 / longP95
}

func percentile95(costs []time.Duration) float64 {
	if len(costs) == 0 {
		return 0
	}
	sort.Slice(costs, func(i, j int) bool { return costs[i] < costs[j] })
	idx := int(math.Ceil(0.95*float64(len(costs)))) - 1
	if idx < 0 {
		idx = 0
	}
	return float64(costs[idx])
}

func (l *RULimiter) record(cost time.Duration, finished time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := finished.Add(-longWindow)
	dst := 0
	for src := range l.samples {
		if !l.samples[src].finished.Before(cutoff) {
			l.samples[dst] = l.samples[src]
			dst++
		}
	}
	l.samples = append(l.samples[:dst], requestSample{cost: cost, finished: finished})
}
