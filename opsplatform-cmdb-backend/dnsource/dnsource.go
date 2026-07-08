// Package dnsource 域名数据源适配层：从域名厂商(GoDaddy/阿里云/腾讯…)同步域名与 DNS 解析。
// adapter 可扩展，当前实现 GoDaddy；每个数据源独立客户端限流(50/分钟)，主动挡在撞厂商真限制(GoDaddy 60)之前。
package dnsource

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Domain 厂商账户下的一个域名
type Domain struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
	Status    string     `json:"status"`
}

// DNSRecord 厂商的一条 DNS 记录
type DNSRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Data     string `json:"data"`
	TTL      int    `json:"ttl"`
	Priority *int   `json:"priority,omitempty"`
}

// Adapter 域名数据源适配接口（每厂商一个实现）
type Adapter interface {
	ListDomains(ctx context.Context) ([]Domain, error)
	ListRecords(ctx context.Context, domain string) ([]DNSRecord, error)
}

// NewAdapter 按 provider + 凭据 + 该源的限流器构造 adapter。
func NewAdapter(provider string, cred map[string]string, lim *Limiter) (Adapter, error) {
	switch provider {
	case "godaddy":
		return &GoDaddy{key: cred["api_key"], secret: cred["api_secret"], lim: lim}, nil
	// 预留：aliyun / tencent / dnspod / cloudflare —— 后续实现各自 adapter
	}
	return nil, fmt.Errorf("数据源 provider %q 暂不支持同步（已支持 godaddy，其余待接入）", provider)
}

// ---------- 客户端限流器（每数据源一个，50/分钟固定窗口） ----------

type RateLimitInfo struct {
	Window        string `json:"window"`
	Used          int    `json:"used"`
	Limit         int    `json:"limit"`
	RetryAt       string `json:"retry_at"`
	RetryAfterSec int    `json:"retry_after_seconds"`
}

// RateLimitError 触发客户端限流（未真正打厂商）
type RateLimitError struct{ Info RateLimitInfo }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("已达数据源客户端限流（%d/分钟），%d 秒后可重试", e.Info.Limit, e.Info.RetryAfterSec)
}

type Limiter struct {
	mu         sync.Mutex
	limit      int
	curMinute  int64
	count      int
	todayDate  string
	todayCount int
	lastLimit  string
}

const defaultLimit = 50 // 客户端阈值，GoDaddy 真限制 60，留 10 缓冲

// Allow 申请一次调用配额；返回 nil 放行，*RateLimitError 表示被限流。
func (l *Limiter) Allow() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	min := now.Unix() / 60
	if min != l.curMinute {
		l.curMinute = min
		l.count = 0
	}
	if d := now.Format("2006-01-02"); d != l.todayDate {
		l.todayDate = d
		l.todayCount = 0
	}
	if l.count >= l.limit {
		retryAt := time.Unix((min+1)*60, 0)
		l.lastLimit = now.Format("2006-01-02 15:04:05")
		return &RateLimitError{Info: RateLimitInfo{
			Window:        time.Unix(min*60, 0).Format("15:04:05") + " ~ " + time.Unix(min*60+59, 0).Format("15:04:05"),
			Used:          l.count, Limit: l.limit,
			RetryAt:       retryAt.Format("2006-01-02 15:04:05"),
			RetryAfterSec: int(time.Until(retryAt).Seconds()) + 1,
		}}
	}
	l.count++
	l.todayCount++
	return nil
}

// Wait 申请一次调用配额；撞限则**阻塞等到下一分钟窗口**再放行（节流不失败，用于后台全量同步）。
// 仍不超过 limit/分钟（守住 GoDaddy 真限制），可被 ctx 取消。
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now()
		min := now.Unix() / 60
		if min != l.curMinute {
			l.curMinute = min
			l.count = 0
		}
		if d := now.Format("2006-01-02"); d != l.todayDate {
			l.todayDate = d
			l.todayCount = 0
		}
		if l.count < l.limit {
			l.count++
			l.todayCount++
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		sleep := time.Until(time.Unix((min+1)*60, 0)) + 100*time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

// Stats 当前用量（给 API 用量卡片）
type Stats struct {
	MinuteUsed int    `json:"minute_used"`
	Limit      int    `json:"limit"`
	TodayTotal int    `json:"today_total"`
	LastLimit  string `json:"last_limited_at"`
}

func (l *Limiter) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Unix()/60 != l.curMinute {
		return Stats{MinuteUsed: 0, Limit: l.limit, TodayTotal: l.todayCount, LastLimit: l.lastLimit}
	}
	return Stats{MinuteUsed: l.count, Limit: l.limit, TodayTotal: l.todayCount, LastLimit: l.lastLimit}
}

// 每数据源一个限流器实例（按 source/registrar id 复用）
var limiters sync.Map

func LimiterFor(sourceID int) *Limiter {
	if v, ok := limiters.Load(sourceID); ok {
		return v.(*Limiter)
	}
	l := &Limiter{limit: defaultLimit}
	actual, _ := limiters.LoadOrStore(sourceID, l)
	return actual.(*Limiter)
}
