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

// Adapter 域名数据源适配接口（每厂商一个实现，只读同步）
type Adapter interface {
	ListDomains(ctx context.Context) ([]Domain, error)
	ListRecords(ctx context.Context, domain string) ([]DNSRecord, error)
}

// DomainDetail 厂商侧域名详情（续费页展示用）。
type DomainDetail struct {
	Expires   *time.Time `json:"expires"`
	RenewAuto bool       `json:"renew_auto"`
	Privacy   bool       `json:"privacy"` // 隐私保护是否已开启（只读展示）
	Status    string     `json:"status"`
}

// RenewalPrice 续费价（估算，来自厂商挂牌价；真实扣费以厂商结算为准）。
type RenewalPrice struct {
	AmountMicro int64  `json:"amount_micro"` // 微单位（÷1_000_000 = 货币金额）
	Currency    string `json:"currency"`
}

// RenewResult 续费厂商返回（订单号等；精确扣费以厂商账单为准，金额字段厂商不一定给）。
type RenewResult struct {
	OrderID     string `json:"order_id"`
	AmountMicro int64  `json:"amount_micro"` // 厂商若返回则填，否则 0
	Currency    string `json:"currency"`
	RawBody     string `json:"raw_body"` // 厂商原始响应，留档
}

// WriteAdapter 可选写回接口：支持把 CMDB 的解析变更同步回厂商，以及域名续费/自动续费。
// 只有实现了它的 adapter 才允许写回；handler 通过类型断言判断。
// 语义对齐 GoDaddy：解析按「类型+主机名」整组操作（读改写），不是单条。
type WriteAdapter interface {
	// GetGroup 取某域名下某 (type,name) 的当前记录组（写回前读，做读改写）。
	GetGroup(ctx context.Context, domain, rtype, name string) ([]DNSRecord, error)
	// ReplaceGroup 用给定记录整组替换该 (type,name)（新增/编辑都走它）。
	ReplaceGroup(ctx context.Context, domain, rtype, name string, recs []DNSRecord) error
	// DeleteGroup 删除该 (type,name) 整组。
	DeleteGroup(ctx context.Context, domain, rtype, name string) error
	// AddRecords 批量追加多条记录（一次调用，用于批量新增）。
	AddRecords(ctx context.Context, domain string, recs []DNSRecord) error
	// GetDomainDetail 取域名当前到期/自动续费/隐私状态（续费前展示）。
	GetDomainDetail(ctx context.Context, domain string) (DomainDetail, error)
	// GetRenewalPrice 取续费挂牌价（估算展示；查不到返回零值不报错）。
	GetRenewalPrice(ctx context.Context, domain string) (RenewalPrice, error)
	// RenewDomain 续费 period 年（⚠️会真实扣费；dry_run 时只打日志不真扣）。返回厂商订单信息。
	RenewDomain(ctx context.Context, domain string, period int) (RenewResult, error)
	// SetAutoRenew 开/关自动续费（不扣费）。
	SetAutoRenew(ctx context.Context, domain string, enabled bool) error
	// DryRun 是否预演模式（只打日志不真发），用于生产写回/续费护栏。
	DryRun() bool
	// EnvLabel 环境标识（生产 / OTE 测试），用于日志/审计区分。
	EnvLabel() string
}

// NewAdapter 按 provider + 凭据 + 该源的限流器构造 adapter。
// 凭据 map 除 api_key/api_secret 外，可选：
//
//	base_url  厂商 API 根地址（GoDaddy 默认生产 https://api.godaddy.com；测试填 https://api.ote-godaddy.com）
//	dry_run   "1" 表示写回只打日志不真发（生产护栏）
func NewAdapter(provider string, cred map[string]string, lim *Limiter) (Adapter, error) {
	switch provider {
	case "godaddy":
		return &GoDaddy{
			key:    cred["api_key"],
			secret: cred["api_secret"],
			base:   cred["base_url"],
			dryRun: cred["dry_run"] == "1",
			lim:    lim,
		}, nil
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
