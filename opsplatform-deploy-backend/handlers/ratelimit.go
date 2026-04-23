package handlers

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
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

// 测试类接口：1 秒补 1 个 token，burst 10
// 调宽到 burst 10 是为了兼容 Ingress/LB 可能把一次点击重放 3-5 次的情况
// 真实恶意刷（>10 次/秒）仍能挡住
var testEndpointLimiter = newKeyedLimiter(rate.Every(1*time.Second), 10)

// 扫描类接口：每用户 1 分钟 1 次
var scanLimiter = newKeyedLimiter(rate.Every(60*time.Second), 1)

// logReject 记录被限流拒绝的请求细节（诊断 Ingress 重放等场景）
func logReject(r *http.Request, limiter, key string) {
	log.Printf("[ratelimit] REJECT limiter=%s key=%s path=%s method=%s remote=%s xff=%q ua=%q",
		limiter, key, r.URL.Path, r.Method,
		r.RemoteAddr,
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("User-Agent"))
}

// LoginRateLimit 基于 IP 的登录限流
func LoginRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !loginLimiter.get(ip).Allow() {
			logReject(r, "login", ip)
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
			logReject(r, "test", key)
			JSONError(w, 40300, "测试接口调用过于频繁，请稍后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ScanRateLimit 扫描接口限流。
// key = username + ":" + project_env_id（从 URL path 拿），让同一用户扫不同项目互不阻塞；
// 只限制"同一用户对同一项目"的重复扫描（1 次/分钟）。
func ScanRateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UsernameFromCtx(r)
		if user == "" {
			user = clientIP(r)
		}
		peID := mux.Vars(r)["id"]
		if peID == "" {
			peID = "unknown"
		}
		key := user + ":" + peID
		if !scanLimiter.get(key).Allow() {
			logReject(r, "scan", key)
			JSONError(w, 40300, "该项目扫描过于频繁，1 分钟后再试")
			return
		}
		next.ServeHTTP(w, r)
	})
}
