// Package dnsource 域名数据源适配层：从域名厂商(GoDaddy/阿里云/腾讯…)同步域名与 DNS 解析。
// adapter 可扩展，当前实现 GoDaddy；每个数据源独立客户端限流(50/分钟)，主动挡在撞厂商真限制(GoDaddy 60)之前。
package dnsource

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ops-cmdb-backend/logx"
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
//
// credKey 取 API key/secret，兼容早期前端写入的 key/secret 两个键名。
//
// 🔴 为什么要兼容而不是直接改掉：早期版本的注册商弹窗把凭据存成了
// {key, secret}，而这里读的是 {api_key, api_secret}。取到空串后认证头拼成
// "sso-key :"，表现是"密钥是对的却一直不通"，且**全程没有任何报错**——
// 保存成功、界面显示"已配置"。
//
// 直接改前端的话，库里已有的那些记录仍然是坏的，而且坏得同样安静。
// 所以这里认旧键名，但**打 WARN**：让"我在用兼容路径"这件事可见，
// 而不是把它变成一个永远没人知道的隐式契约。
func credKey(cred map[string]string) (key, secret string) {
	key, secret = cred["api_key"], cred["api_secret"]
	if key != "" || secret != "" {
		return key, secret
	}
	if cred["key"] != "" || cred["secret"] != "" {
		logx.J("dnsource", "legacy_cred_keys", map[string]any{
			"warn": "凭据用的是旧键名 key/secret（应为 api_key/api_secret）。" +
				"本次已兼容读取；重新保存一次该注册商即可写成新键名",
		})
		return cred["key"], cred["secret"]
	}
	return "", ""
}

func NewAdapter(provider string, cred map[string]string, lim *Limiter) (Adapter, error) {
	switch provider {
	case "godaddy":
		k, sec := credKey(cred)
		return &GoDaddy{
			key:    k,
			secret: sec,
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

// crossReplicaQuota 跨副本配额后端。为 nil 时退化成纯进程内计数。
//
// ⚠️ 这个变量为 nil 意味着**保护只在单副本下成立**。
// 它由 main 在启动时注入；注入失败会打 WARN 而不是静默继续，
// 因为"少了一层保护"和"没有这层保护"在日志里看起来一模一样。
type crossReplicaQuota interface {
	Take(ctx context.Context, scope string, limit int) (int, bool, error)
}

var (
	globalQuota   crossReplicaQuota
	globalQuotaMu sync.RWMutex
)

// SetQuota 注入跨副本配额后端（main 启动时调一次）。
func SetQuota(q crossReplicaQuota) {
	globalQuotaMu.Lock()
	globalQuota = q
	globalQuotaMu.Unlock()
}

func quotaBackend() crossReplicaQuota {
	globalQuotaMu.RLock()
	defer globalQuotaMu.RUnlock()
	return globalQuota
}

// Limiter 单个数据源的调用限流。
//
// ⚠️ 两层计数，缺一不可：
//
//	进程内（count）：省掉大部分 DB 往返，也是 DB 不可用时的兜底；
//	跨副本（globalQuota）：**权威**。
//
// 只有进程内那层的话，2 副本各算 50/分钟 = 100，
// 直接越过 GoDaddy 的 60——保护形同虚设，而现象只是
// 批量续费里"莫名其妙有几个没续上"。
type Limiter struct {
	mu         sync.Mutex
	limit      int
	curMinute  int64
	count      int
	todayDate  string
	todayCount int
	lastLimit  string
	scope      string // 跨副本配额的作用域键，如 dnsource:12
}

const defaultLimit = 50 // 客户端阈值，GoDaddy 真限制 60，留 10 缓冲

// Allow 申请一次调用配额；返回 nil 放行，*RateLimitError 表示被限流。
//
// 两层依次通过才放行：先进程内（便宜、DB 挂了时的兜底），再跨副本（权威）。
func (l *Limiter) Allow() error {
	min, used, ok := l.takeLocal()
	if !ok {
		return l.limitErr(min, used)
	}
	shUsed, shOK, err := l.takeShared()
	if err != nil {
		// 保护失效了必须有人知道。静默继续的话，真撞上厂商限流时
		// 没有任何线索指向"跨副本配额没生效"
		logx.J("dnsource", "quota_backend_fail", map[string]any{
			"level": "WARN", "scope": l.scopeKey(), "error": err.Error(),
			"msg": "跨副本配额不可用，本次只按进程内计数放行；多副本下可能越过厂商限制",
		})
		return nil
	}
	if !shOK {
		l.markLimited()
		return l.limitErr(min, shUsed)
	}
	return nil
}

// takeLocal 进程内计数 +1。返回当前分钟窗口、用量、是否放行。
func (l *Limiter) takeLocal() (min int64, used int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	min = now.Unix() / 60
	if min != l.curMinute {
		l.curMinute = min
		l.count = 0
	}
	if d := now.Format("2006-01-02"); d != l.todayDate {
		l.todayDate = d
		l.todayCount = 0
	}
	if l.count >= l.limit {
		l.lastLimit = now.Format("2006-01-02 15:04:05")
		return min, l.count, false
	}
	l.count++
	l.todayCount++
	return min, l.count, true
}

func (l *Limiter) markLimited() {
	l.mu.Lock()
	l.lastLimit = time.Now().Format("2006-01-02 15:04:05")
	l.mu.Unlock()
}

func (l *Limiter) scopeKey() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.scope
}

// takeShared 向跨副本后端申请一次配额。没注入后端时直接放行。
//
// ⚠️ 绝不能在持有 l.mu 时调用：这里有一次 DB 往返，
// 持锁会把该数据源的所有调用串行化到 DB 延迟上。
func (l *Limiter) takeShared() (int, bool, error) {
	q := quotaBackend()
	scope := l.scopeKey()
	if q == nil || scope == "" {
		return 0, true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return q.Take(ctx, scope, l.limit)
}

func (l *Limiter) limitErr(min int64, used int) error {
	retryAt := time.Unix((min+1)*60, 0)
	return &RateLimitError{Info: RateLimitInfo{
		Window: time.Unix(min*60, 0).Format("15:04:05") + " ~ " + time.Unix(min*60+59, 0).Format("15:04:05"),
		Used:   used, Limit: l.limit,
		RetryAt:       retryAt.Format("2006-01-02 15:04:05"),
		RetryAfterSec: int(time.Until(retryAt).Seconds()) + 1,
	}}
}

// Wait 申请一次调用配额；撞限则**阻塞等到下一分钟窗口**再放行（节流不失败，用于后台全量同步）。
// 仍不超过 limit/分钟（守住 GoDaddy 真限制），可被 ctx 取消。
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		min, _, ok := l.takeLocal()
		if ok {
			// ⚠️ 跨副本这一层同样要走，否则两个副本各自跑全量同步时
			// 每个都以为自己只用了 50/分钟，合起来照样打爆厂商配额。
			// 后台任务现在归 leader 跑，但手动触发的同步落在哪个副本不确定。
			shOK, err := l.waitShared()
			if err != nil {
				logx.J("dnsource", "quota_backend_fail", map[string]any{
					"level": "WARN", "scope": l.scopeKey(), "error": err.Error(),
					"msg": "跨副本配额不可用，本次只按进程内计数放行；多副本下可能越过厂商限制",
				})
				return nil
			}
			if shOK {
				return nil
			}
			l.markLimited()
			// 跨副本配额满了：和进程内满了一样，等到下一分钟窗口。
			// 注意进程内计数已经 +1 了——那一次是"预留但没用上"，
			// 下个窗口会清零，不会长期偏移
		}
		sleep := time.Until(time.Unix((min+1)*60, 0)) + 100*time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

func (l *Limiter) waitShared() (bool, error) {
	_, ok, err := l.takeShared()
	return ok, err
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
	// scope 必须带上 sourceID：一个数据源对应一个厂商账号，
	// 配额是按账号算的。所有源共用一个 scope 会让多账号互相挤占
	l := &Limiter{limit: defaultLimit, scope: fmt.Sprintf("dnsource:%d", sourceID)}
	actual, _ := limiters.LoadOrStore(sourceID, l)
	return actual.(*Limiter)
}

// SyncSupported 这个厂商有没有真正的同步实现。
//
// 🔴 存在的理由：注册商的 provider 白名单（handler 里的 Providers）
// 一度有 5 个厂商，而本文件的 NewAdapter 只实现了 godaddy 一个。
// 选了其余几个会**保存成功、界面显示「已启用」**，但同步时才找不到实现——
// 表现为"这个注册商下的域名到期日一直不更新"，没有任何报错。
//
// 那段风险在 handler 的注释里被明确警告过，但**白名单与实现集合没有做对齐校验**，
// 于是它照样发生了。⚠️ 警告不能替代校验。
//
// 新增 adapter 时必须同时在这里登记，zz_provider_parity_test.go 会卡住不一致。
func SyncSupported(provider string) bool {
	switch provider {
	case "godaddy":
		return true
	default:
		return false
	}
}
