// Package cloudsource 云资源数据源适配（每云厂商一个实现），一期支持 GCP 主机（只读）。
package cloudsource

import (
	"context"
	"fmt"
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
	ProjectName string // project 显示名（GCP 可重命名；取不到回退 project id）
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

// Adapter 云资源适配接口
type Adapter interface {
	ListInstances(ctx context.Context, projects []string) ([]Instance, error)
}

// NewAdapter 按 provider + service account JSON 凭据构造 adapter。
func NewAdapter(provider, credJSON string) (Adapter, error) {
	switch provider {
	case "gcp":
		return &GCP{credJSON: credJSON}, nil
	// 预留：aws / aliyun / tencent —— 后续各自实现
	}
	return nil, fmt.Errorf("云厂商 %q 暂不支持（当前仅 gcp）", provider)
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
