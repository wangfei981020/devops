// Package cloudsource 云资源数据源适配（每云厂商一个实现），一期支持 GCP 主机（只读）。
// 凭据 per-project：每个 adapter 持一个 project 的 service account 凭据，只列该 project。
package cloudsource

import (
	"context"
	"fmt"
	"ops-cmdb-backend/logx"
	"sync"
	"time"
)

// Disk 主机的一块磁盘
type Disk struct {
	Name   string `json:"name"`
	SizeGB int    `json:"size_gb"`
	Type   string `json:"type"` // pd-ssd / pd-standard / pd-balanced
	IsBoot bool   `json:"is_boot"`
}

// Instance 一台主机（云实例）
type Instance struct {
	InstanceID  string
	Name        string
	Project     string // project id（不可变）
	Zone        string
	Region      string
	MachineType string
	VCPU        int
	MemMB       int
	InternalIP  string
	ExternalIP  string
	Status      string
	OS          string
	Labels      map[string]string
	SelfLink    string
	CreatedAt   *time.Time
	Disks       []Disk
	// GCP 同步自带的只读技术字段
	Hostname           string
	VPC                string
	Subnet             string
	NetworkTags        []string
	Preemptible        bool // 抢占式/Spot（会被回收）
	Image              string
	CPUPlatform        string
	DeletionProtection bool
	ServiceAccounts    []string
}

// ---- 网络资源（VPC/子网/防火墙/静态IP/负载均衡）----

type Network struct {
	Name     string
	Mode     string // auto/custom
	SelfLink string
}
type Subnet struct {
	Name     string
	Network  string // 所属 VPC 名
	Region   string
	CIDR     string
	Gateway  string
	SelfLink string
}
type Firewall struct {
	Name         string
	Network      string
	Direction    string // INGRESS/EGRESS
	Priority     int
	Action       string // allow/deny
	Protocols    string // "tcp:22,80;udp:53"
	SourceRanges string
	TargetTags   string
	Disabled     bool
	SelfLink     string
}
type Address struct {
	Name     string
	Address  string
	Type     string // EXTERNAL/INTERNAL
	Status   string // IN_USE/RESERVED
	Region   string // 'global' 或区域
	Users    string // 绑定的资源
	SelfLink string
}
type LoadBalancer struct {
	Name      string
	Scheme    string // EXTERNAL/INTERNAL/...
	VIP       string
	PortRange string
	Protocol  string
	Target    string
	Region    string // 'global' 或区域
	SelfLink  string
	Backends  []LBBackend // 后端成员（实例），best-effort 追溯
	// BackendState 追溯结果，用来区分「真的没有后端」和「没追溯到」。
	//
	//	  ok        —— 追到了，Backends 就是全部
	//	  none      —— 确实没有后端（LB 挂空了，这本身是个问题）
	//	  unresolved—— 追溯过程出错（权限/API 失败），**不知道有没有**
	//	  unsupported— 服务型 NEG 等当前不支持追溯的类型
	//
	//	必须区分：81 个 LB 全显示"无后端"时，人第一反应是"这些 LB 都是空的"，
	//	而实际可能只是拉后端的 API 全被拒了。把"没查到"渲染成"没有"，
	//	是这套系统最危险的失效模式。
	BackendState string
}

// LBBackend 负载均衡追溯到的一个后端实例
type LBBackend struct {
	Instance string // 实例名
	Group    string // 所属实例组名
	Zone     string // 实例组所在 zone/region
}

// NetworkResources 一个 project 的全部网络资源
type NetworkResources struct {
	Networks      []Network
	Subnets       []Subnet
	Firewalls     []Firewall
	Addresses     []Address
	LoadBalancers []LoadBalancer
}

// Adapter 云资源适配接口。各方法只处理该 adapter 所持凭据对应的单个 project。
type Adapter interface {
	ListInstances(ctx context.Context, projectID string) ([]Instance, error)
	ListNetwork(ctx context.Context, projectID string) (*NetworkResources, error)
}

// NewAdapter 按 provider + 该 project 的 service account JSON 凭据构造 adapter。
func NewAdapter(provider, credJSON string) (Adapter, error) {
	switch provider {
	case "gcp":
		return &GCP{credJSON: credJSON}, nil
		// 预留：aws / aliyun / tencent —— 后续各自实现
	}
	return nil, fmt.Errorf("云厂商 %q 暂不支持（当前仅 gcp）", provider)
}

// ---------- 客户端限流（防撞 GCP 读配额）----------
// GCP Compute 读配额很宽松（每分钟上万），我们同步请求量本就小，这里只做保守节流保底：
// 每 project 一个令牌桶，默认 300/分钟；撞线则阻塞等待下一窗口，不直接失败。
const defaultRatePerMin = 300

type limiter struct {
	mu     sync.Mutex
	limit  int
	count  int
	window int64 // 当前窗口起点（unix 分钟）
	// scope 跨副本记账的键。必须带 projectID —— 配额是按 GCP 项目算的，
	// 所有项目共用一个 scope 会让它们互相挤占。
	scope string
}

// takeShared 向跨副本后端申请一次配额。没注入后端时直接放行。
func (l *limiter) takeShared(ctx context.Context) (bool, error) {
	q := quotaBackend()
	if q == nil || l.scope == "" {
		return true, nil
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, ok, err := q.Take(c, l.scope, l.limit)
	return ok, err
}

var limiters sync.Map // projectID -> *limiter

// crossReplicaQuota 跨副本配额后端。为 nil 时退化成纯进程内计数。
//
// 🔴 为什么必须有它
//
// 上面那个 limiter 是**进程内**的。多副本下每个副本各算各的 300/分钟：
//
//	2 副本 = 600/分   7 副本（HPA 上限）= 2100/分
//
// dnsource 早就改成跨副本记账了（GoDaddy 那次撞了限流才改的），
// 而这一条一直漏着 —— 同一个坑在两个地方，只填了一个。
//
// ⚠️ 撞上厂商限流的表现**不是一句报错**，而是同步结果里
// "莫名其妙少了几台机器"：部分请求被 429 掉，而整体流程照常走完。
// GoDaddy 那次就是这么发现的（批量续费里几个没续上）。
//
// ⚠️ 这个变量为 nil 意味着**保护只在单副本下成立**。由 main 启动时注入。
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

func limiterFor(projectID string) *limiter {
	if v, ok := limiters.Load(projectID); ok {
		return v.(*limiter)
	}
	l := &limiter{limit: defaultRatePerMin, scope: "cloudsource:" + projectID}
	actual, _ := limiters.LoadOrStore(projectID, l)
	return actual.(*limiter)
}

// wait 申请一次调用配额，撞线则 sleep 到下一分钟窗口（可被 ctx 取消）。
//
// 两层都要过：进程内那层挡住本副本的突发，跨副本那层守住厂商真限制。
// ⚠️ 顺序不能反 —— 先过本地再打 DB，否则每次调用都要一次 DB 往返。
func (l *limiter) wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		now := time.Now().Unix() / 60
		if now != l.window {
			l.window = now
			l.count = 0
		}
		if l.count < l.limit {
			l.count++
			l.mu.Unlock()
			// ⚠️ 绝不能在持有 l.mu 时调用：这里有一次 DB 往返，
			//	持锁会把该项目的所有调用串行化到 DB 延迟上。
			ok, err := l.takeShared(ctx)
			if err != nil {
				// 🔴 配额后端不可用时**放行**，但必须留痕。
				//	拒绝的代价是同步做不了；放行的代价只是可能撞一次 429（看得见）。
				//	与互斥锁的选择相反 —— 那个守的是扣钱，必须拒绝。
				logx.J("cloudsource", "quota_backend_fail", map[string]any{
					"level": "WARN", "scope": l.scope, "error": err.Error(),
					"msg": "跨副本配额不可用，本次只按进程内计数放行；多副本下可能越过厂商限制",
				})
				return nil
			}
			if ok {
				return nil
			}
			// 跨副本层撞线：等下一窗口再来（和本地撞线走同一条路）
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(60-time.Now().Unix()%60) * time.Second):
			}
			continue
		}
		l.mu.Unlock()
		// 等到下一分钟窗口
		sleep := time.Duration(60-time.Now().Unix()%60) * time.Second
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

// lastSeg 取 URL / 路径最后一段（GCP 很多字段是完整 URL）
func lastSeg(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}

// regionOfZone asia-east1-a -> asia-east1
func regionOfZone(zone string) string {
	if i := lastIndexByte(zone, '-'); i > 0 {
		return zone[:i]
	}
	return zone
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
