package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	container "cloud.google.com/go/container/apiv1"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

type GCPClient struct {
	cm      *container.ClusterManagerClient
	compute *compute.Service
}

// NewGCPClient: 使用 Application Default Credentials（ADC）— 主要给本地开发用。
func NewGCPClient(ctx context.Context) (*GCPClient, error) {
	c, err := container.NewClusterManagerClient(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := compute.NewService(ctx)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("compute client: %w", err)
	}
	return &GCPClient{cm: c, compute: cs}, nil
}

// NewGCPClientWithJSON: 直接用内存里的 service account JSON 创建 client。
// 每个被监控的集群在 DB 里存自己的 SA key，scrape 时按需创建。
func NewGCPClientWithJSON(ctx context.Context, saKeyJSON []byte) (*GCPClient, error) {
	c, err := container.NewClusterManagerClient(ctx, option.WithCredentialsJSON(saKeyJSON))
	if err != nil {
		return nil, err
	}
	cs, err := compute.NewService(ctx, option.WithCredentialsJSON(saKeyJSON))
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("compute client: %w", err)
	}
	return &GCPClient{cm: c, compute: cs}, nil
}

func (g *GCPClient) Close() error { return g.cm.Close() }

type ClusterInfo struct {
	CurrentMasterVersion string
	NodePools            []*containerpb.NodePool
	Location             string
}

// GetCluster: 只取 currentMasterVersion 和 nodePools。
// "可升级范围" 由调用方结合 ServerConfig.validMasterVersions 推导（Cluster 资源本身不暴露该字段）。
func (g *GCPClient) GetCluster(ctx context.Context, project, location, name string) (*ClusterInfo, error) {
	req := &containerpb.GetClusterRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, name),
	}
	c, err := g.cm.GetCluster(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ClusterInfo{
		CurrentMasterVersion: c.GetCurrentMasterVersion(),
		NodePools:            c.GetNodePools(),
		Location:             location,
	}, nil
}

type ServerConfig struct {
	ValidMasterVersions []string
	ValidNodeVersions   []string
	DefaultClusterVer   string
	// EOL：GCP API 在部分 channel 信息中带 endOfStandardSupportTimestamp，map: version -> {std, ext}
	EOL map[string]struct{ Std, Ext string }
}

// NodeInstance：从 GCP Compute API 拿到的单台 VM 实例信息
type NodeInstance struct {
	Name      string    // gke-xxx-xxx 这种 VM 实例名（= kubernetes node name）
	Zone      string    // asia-east2-a
	CreatedAt time.Time // GCP 真实创建时间（kubectl get node 里的 AGE 来源）
}

// ListNodepoolInstances：给定一个节点池的 InstanceGroupManager URL 列表，
// 拉取这些 IGM 下所有 VM 实例的创建时间。
//
// 流程：
//  1. 每个 IGM URL 解析出 project/zone/igmName
//  2. ListManagedInstances 拿到该 IGM 下所有 instance 引用 URL
//  3. Instances.Get 拿单台 VM 的 creationTimestamp
//
// 性能：N 个节点池 × M 个 VM = N+N*M 次 API 调用。
// 比 aggregatedList 慢但实现简单，节点池 VM 数量级一般 < 100，可接受。
func (g *GCPClient) ListNodepoolInstances(ctx context.Context, instanceGroupUrls []string) ([]NodeInstance, error) {
	out := []NodeInstance{}
	for _, igmURL := range instanceGroupUrls {
		project, zone, igmName, err := parseIGMURL(igmURL)
		if err != nil {
			return nil, fmt.Errorf("parse IGM URL %q: %w", igmURL, err)
		}

		// 1. 列出 IGM 下托管的实例（只拿到实例 URL 引用，没有 createTime）
		listResp, err := g.compute.InstanceGroupManagers.
			ListManagedInstances(project, zone, igmName).
			Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("list managed instances (%s/%s): %w", zone, igmName, err)
		}

		// 2. 每个实例独立 Get 一次拿真实创建时间
		for _, mi := range listResp.ManagedInstances {
			// mi.Instance 是完整 URL：.../projects/X/zones/Y/instances/NAME
			instName := lastSeg(mi.Instance)
			if instName == "" {
				continue
			}
			inst, err := g.compute.Instances.Get(project, zone, instName).Context(ctx).Do()
			if err != nil {
				// 单台拉不到不要把整个节点池都拖死，跳过这台
				// （常见原因：刚被删/正在 delete 中）
				continue
			}
			created, _ := time.Parse(time.RFC3339, inst.CreationTimestamp)
			out = append(out, NodeInstance{
				Name:      inst.Name,
				Zone:      zone,
				CreatedAt: created,
			})
		}
	}
	return out, nil
}

// parseIGMURL: 把 IGM 的 selfLink URL 拆出 project/zone/name。
// URL 示例：
//
//	https://www.googleapis.com/compute/v1/projects/csc5002-public-uat/zones/asia-east2-a/instanceGroupManagers/gke-uat-k8s-cluster-01-infra-...
func parseIGMURL(u string) (project, zone, name string, err error) {
	parts := strings.Split(u, "/")
	for i, p := range parts {
		switch p {
		case "projects":
			if i+1 < len(parts) {
				project = parts[i+1]
			}
		case "zones":
			if i+1 < len(parts) {
				zone = parts[i+1]
			}
		case "instanceGroupManagers":
			if i+1 < len(parts) {
				name = parts[i+1]
			}
		}
	}
	if project == "" || zone == "" || name == "" {
		return "", "", "", fmt.Errorf("incomplete IGM URL")
	}
	return
}

// lastSeg：从 URL 取最后一段（/projects/X/zones/Y/instances/NAME -> NAME）
func lastSeg(u string) string {
	idx := strings.LastIndex(u, "/")
	if idx < 0 || idx == len(u)-1 {
		return ""
	}
	return u[idx+1:]
}

func (g *GCPClient) GetServerConfig(ctx context.Context, project, location string) (*ServerConfig, error) {
	req := &containerpb.GetServerConfigRequest{
		Name: fmt.Sprintf("projects/%s/locations/%s/serverConfig", project, location),
	}
	c, err := g.cm.GetServerConfig(ctx, req)
	if err != nil {
		return nil, err
	}
	cfg := &ServerConfig{
		ValidMasterVersions: c.GetValidMasterVersions(),
		ValidNodeVersions:   c.GetValidNodeVersions(),
		DefaultClusterVer:   c.GetDefaultClusterVersion(),
		EOL:                 map[string]struct{ Std, Ext string }{},
	}
	return cfg, nil
}
