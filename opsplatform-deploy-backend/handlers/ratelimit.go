package handlers

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// keyedLimiter 按 key（IP 或 username）分桶限流
type keyedLimiter struct {
	mu       sync.Mutex
	limits   map[string]*rate.Limiter
	r        rate.Limit
	burst    int
	lastSeen map[string]time.Time
}

func newKeyedLimiter(r rate.Limit, burst int) *keyedLimiter {
	l := &keyedLimiter{
		limits:   make(map[string]*rate.Limiter),
		r:        r,
		burst:    burst,
		lastSeen: make(map[string]time.Time),
	}
	// 定期清理 1h 未活跃的 key，避免无限增长
	go func() {
		t := time.NewTicker(10 * time.Minute)
		defer t.Stop()
		for range t.C {
			l.gc(time.Hour)
		}
	}()
	return l
}

func (l *keyedLimiter) get(key string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.limits[key]
	if !ok {
		lim = rate.NewLimiter(l.r, l.burst)
		l.limits[key] = lim
	}
	l.lastSeen[key] = time.Now()
	return lim
}

func (l *keyedLimiter) gc(maxAge time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for k, ts := range l.lastSeen {
		if ts.Before(cutoff) {
			delete(l.limits, k)
			delete(l.lastSeen, k)
		}
	}
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		// 取第一个
		if i := strings.IndexByte(xf, ','); i > 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	if xr := r.Header.Get("X-Real-IP"); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// 登录：每 IP 每分钟 5 次
var loginLimiter = newKeyedLimiter(rate.Every(12*time.Second), 5)

// 测试类接口：每用户 3 秒 1 次，burst 3（允许连续点 3 下，然后补充）
// 运维调测时节奏合理，又能挡住 Lark 刷屏 / PAT 探测
var testEndpointLimiter = newKeyedLimiter(rate.Every(3*time.Second), 3)

// 扫描类接口：每用户 1 分钟 1 次
var scanLimiter = newKeyedLimiter(rate.Every(60*time.Second), 1)

// LoginRateLimit 基于 IP 的登录限流
func LoginRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !loginLimiter.get(ip).Allow() {
			JSONError(w, 40300, "登录尝试过于频繁，请稍后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TestRateLimit 基于用户的测试接口限流（用于 /lark-bots/{id}/test、/*/test-git、/*/test-argocd、/global-config/test-gitlab）
func TestRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := UsernameFromCtx(r)
		if key == "" {
			key = clientIP(r)
		}
		if !testEndpointLimiter.get(key).Allow() {
			JSONError(w, 40300, "测试接口调用过于频繁，请稍后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ScanRateLimit 扫描接口限流
func ScanRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := UsernameFromCtx(r)
		if key == "" {
			key = clientIP(r)
		}
		if !scanLimiter.get(key).Allow() {
			JSONError(w, 40300, "扫描操作过于频繁，1 分钟后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}
