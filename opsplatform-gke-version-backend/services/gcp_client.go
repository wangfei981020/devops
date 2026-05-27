package services

import (
	"context"
	"fmt"

	container "cloud.google.com/go/container/apiv1"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	"google.golang.org/api/option"
)

type GCPClient struct {
	cm *container.ClusterManagerClient
}

// NewGCPClient: 使用 Application Default Credentials（ADC）— 主要给本地开发用。
func NewGCPClient(ctx context.Context) (*GCPClient, error) {
	c, err := container.NewClusterManagerClient(ctx)
	if err != nil {
		return nil, err
	}
	return &GCPClient{cm: c}, nil
}

// NewGCPClientWithJSON: 直接用内存里的 service account JSON 创建 client。
// 每个被监控的集群在 DB 里存自己的 SA key，scrape 时按需创建。
func NewGCPClientWithJSON(ctx context.Context, saKeyJSON []byte) (*GCPClient, error) {
	c, err := container.NewClusterManagerClient(ctx, option.WithCredentialsJSON(saKeyJSON))
	if err != nil {
		return nil, err
	}
	return &GCPClient{cm: c}, nil
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
