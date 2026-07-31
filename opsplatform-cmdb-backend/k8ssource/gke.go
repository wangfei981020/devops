// GKE 经 GCP SA key 自动发现 + 连接（不用逐集群 kubeconfig）。
// container.googleapis.com 是公网 Google API，列集群不受集群授权网络限制；连各集群 apiserver 仍需网络可达。
package k8ssource

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"
	"k8s.io/client-go/rest"
)

// GKECluster 发现结果。
type GKECluster struct {
	Name      string `json:"name"`
	Location  string `json:"location"`
	Endpoint  string `json:"endpoint"`
	Version   string `json:"version"`
	NodeCount int    `json:"node_count"`
	Status    string `json:"status"`
	CA        string `json:"ca"` // base64 PEM
	// ReleaseChannel 决定这个集群的自动升级排期取官网表的哪一列（空=未入通道，规则上取 STABLE 列）。
	// 纳管时就显示出来，免得选完还要回 GCP 控制台确认。
	ReleaseChannel string `json:"release_channel"`
}

// DiscoverGKE 用 SA key 列出某 project 下所有 GKE 集群。
func DiscoverGKE(ctx context.Context, saJSON []byte, projectID string) ([]GKECluster, error) {
	svc, err := container.NewService(ctx, option.WithCredentialsJSON(saJSON), option.WithScopes(container.CloudPlatformScope))
	if err != nil {
		return nil, fmt.Errorf("GKE 客户端: %w", err)
	}
	resp, err := svc.Projects.Locations.Clusters.List("projects/" + projectID + "/locations/-").Do()
	if err != nil {
		return nil, fmt.Errorf("列集群: %w", err)
	}
	out := []GKECluster{}
	for _, c := range resp.Clusters {
		ca := ""
		if c.MasterAuth != nil {
			ca = c.MasterAuth.ClusterCaCertificate
		}
		channel := ""
		if c.ReleaseChannel != nil {
			channel = c.ReleaseChannel.Channel
		}
		out = append(out, GKECluster{
			Name: c.Name, Location: c.Location, Endpoint: c.Endpoint,
			Version: c.CurrentMasterVersion, NodeCount: int(c.CurrentNodeCount), Status: c.Status, CA: ca,
			ReleaseChannel: channel,
		})
	}
	return out, nil
}

// gkeRestConfig 用 SA key 换 OAuth token 连 GKE apiserver（endpoint + base64 CA）。
func gkeRestConfig(ctx context.Context, saJSON []byte, endpoint, caB64 string) (*rest.Config, error) {
	creds, err := google.CredentialsFromJSON(ctx, saJSON, container.CloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("SA 凭据: %w", err)
	}
	ca, err := base64.StdEncoding.DecodeString(caB64)
	if err != nil {
		return nil, fmt.Errorf("解码 CA: %w", err)
	}
	cfg := &rest.Config{Host: "https://" + endpoint, TLSClientConfig: rest.TLSClientConfig{CAData: ca}}
	ts := creds.TokenSource
	cfg.Wrap(func(rt http.RoundTripper) http.RoundTripper { return &oauth2.Transport{Source: ts, Base: rt} })
	cfg.Timeout = 15 * time.Second
	return cfg, nil
}
