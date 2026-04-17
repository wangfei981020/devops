package handlers

import (
	"net/http"
	"sync"
	"time"
)

// slidingWindow is a simple in-memory sliding-window counter keyed by string.
// Not distributed but sufficient for single-instance deployments and DoS throttling.
type slidingWindow struct {
	mu      sync.Mutex
	window  time.Duration
	limit   int
	entries map[string][]time.Time
}

func newSlidingWindow(window time.Duration, limit int) *slidingWindow {
	sw := &slidingWindow{
		window:  window,
		limit:   limit,
		entries: make(map[string][]time.Time),
	}
	go sw.gc()
	return sw
}

func (s *slidingWindow) allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-s.window)
	list := s.entries[key]
	// drop expired
	idx := 0
	for i, t := range list {
		if t.After(cutoff) {
			idx = i
			break
		}
		idx = i + 1
	}
	list = list[idx:]
	if len(list) >= s.limit {
		s.entries[key] = list
		return false
	}
	list = append(list, now)
	s.entries[key] = list
	return true
}

func (s *slidingWindow) gc() {
	for {
		time.Sleep(s.window)
		s.mu.Lock()
		cutoff := time.Now().Add(-s.window)
		for k, list := range s.entries {
			idx := 0
			for i, t := range list {
				if t.After(cutoff) {
					idx = i
					break
				}
				idx = i + 1
			}
			list = list[idx:]
			if len(list) == 0 {
				delete(s.entries, k)
			} else {
				s.entries[k] = list
			}
		}
		s.mu.Unlock()
	}
}

var (
	registerLimiter  *slidingWindow
	reportLimiter    *slidingWindow
	loginLimiter     *slidingWindow // per IP, anti brute-force
	heartbeatLimiter *slidingWindow // per agent_id
	pullLimiter      *slidingWindow // per agent_id

	// Per-username failed login counter, locks user after threshold
	loginFailMu     sync.Mutex
	loginFailCount  = map[string]int{}
	loginLockUntil  = map[string]time.Time{}
	loginFailWindow = 15 * time.Minute
	loginFailMax    = 10
)

// InitRateLimiters should be called once after config is loaded.
func InitRateLimiters() {
	registerLimiter = newSlidingWindow(time.Minute, cfg.RLRegisterPerMin)
	reportLimiter = newSlidingWindow(time.Second, cfg.RLReportPerSec)
	loginLimiter = newSlidingWindow(time.Minute, 20)    // 每 IP 每分钟最多 20 次登录尝试
	heartbeatLimiter = newSlidingWindow(time.Second, 5) // 每 Agent 每秒最多 5 次心跳
	pullLimiter = newSlidingWindow(time.Second, 10)     // 每 Agent 每秒最多 10 次拉任务
}

func checkLoginLimit(w http.ResponseWriter, r *http.Request) bool {
	if loginLimiter == nil {
		return true
	}
	if !loginLimiter.allow(getClientIP(r)) {
		jsonError(w, http.StatusTooManyRequests, "登录频率过高，请稍后再试")
		return false
	}
	return true
}

// markLoginFailure increments per-username failure count and locks if exceeded.
func markLoginFailure(username string) {
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	loginFailCount[username]++
	if loginFailCount[username] >= loginFailMax {
		loginLockUntil[username] = time.Now().Add(loginFailWindow)
		loginFailCount[username] = 0
	}
}

func clearLoginFailure(username string) {
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	delete(loginFailCount, username)
	delete(loginLockUntil, username)
}

func isLoginLocked(username string) bool {
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	if t, ok := loginLockUntil[username]; ok {
		if time.Now().Before(t) {
			return true
		}
		delete(loginLockUntil, username)
	}
	return false
}

func checkHeartbeatLimit(w http.ResponseWriter, agentID string) bool {
	if heartbeatLimiter == nil {
		return true
	}
	if !heartbeatLimiter.allow(agentID) {
		jsonError(w, http.StatusTooManyRequests, "心跳频率过高")
		return false
	}
	return true
}

func checkPullLimit(w http.ResponseWriter, agentID string) bool {
	if pullLimiter == nil {
		return true
	}
	if !pullLimiter.allow(agentID) {
		jsonError(w, http.StatusTooManyRequests, "拉任务频率过高")
		return false
	}
	return true
}

func checkRegisterLimit(w http.ResponseWriter, r *http.Request) bool {
	if registerLimiter == nil {
		return true
	}
	ip := getClientIP(r)
	if !registerLimiter.allow(ip) {
		jsonError(w, http.StatusTooManyRequests, "注册频率过高，请稍后再试")
		return false
	}
	return true
}

func checkReportLimit(w http.ResponseWriter, agentID string) bool {
	if reportLimiter == nil {
		return true
	}
	if !reportLimiter.allow(agentID) {
		jsonError(w, http.StatusTooManyRequests, "上报频率过高")
		return false
	}
	return true
}
