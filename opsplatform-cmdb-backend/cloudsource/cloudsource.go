// Package cloudsource 云资源数据源适配（每云厂商一个实现），一期支持 GCP 主机（只读）。
// 凭据 per-project：每个 adapter 持一个 project 的 service account 凭据，只列该 project。
package cloudsource

import (
	"context"
	"fmt"
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
}

// Adapter 云资源适配接口。ListInstances 只列该 adapter 所持凭据对应的单个 project。
type Adapter interface {
	ListInstances(ctx context.Context, projectID string) ([]Instance, error)
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
}

var limiters sync.Map // projectID -> *limiter

func limiterFor(projectID string) *limiter {
	if v, ok := limiters.Load(projectID); ok {
		return v.(*limiter)
	}
	l := &limiter{limit: defaultRatePerMin}
	actual, _ := limiters.LoadOrStore(projectID, l)
	return actual.(*limiter)
}

// wait 申请一次调用配额，撞线则 sleep 到下一分钟窗口（可被 ctx 取消）。
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
			return nil
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
