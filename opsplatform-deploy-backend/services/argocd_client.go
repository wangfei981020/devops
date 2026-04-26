package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type ArgocdClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// argocdInsecureTLS 由 ARGOCD_INSECURE 环境变量控制是否跳过 TLS 校验
// 默认 false（校验）；ArgoCD 使用自签证书时设 true
func argocdInsecureTLS() bool {
	return os.Getenv("ARGOCD_INSECURE") == "true"
}

func NewArgocdClient(baseURL, token string) *ArgocdClient {
	return &ArgocdClient{
		BaseURL: baseURL,
		Token:   token,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: argocdInsecureTLS()},
			},
		},
	}
}

func (c *ArgocdClient) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return raw, resp.StatusCode, fmt.Errorf("argocd %s %s -> %d: %s", method, path, resp.StatusCode, string(raw))
	}
	return raw, resp.StatusCode, nil
}

// Ping 测试连通，返回 argocd 版本
func (c *ArgocdClient) Ping(ctx context.Context) (string, error) {
	raw, _, err := c.do(ctx, "GET", "/api/version", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"Version"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.Version, nil
}

type AppStatus struct {
	Name       string `json:"name"`
	SyncStatus string `json:"sync_status"` // Synced / OutOfSync / Unknown
	Health     string `json:"health"`      // Healthy / Progressing / Degraded / Missing / Suspended
	Message    string `json:"message"`
	// 当前/最近一次 sync 操作的状态。Sync API 是异步的，靠这两个字段判定"我们触发的
	// 这次 sync 是否已经被 argocd 处理完"，避免 PollUntilStable 第一次就误命中旧
	// Healthy 状态导致 0s "成功"。
	OperationPhase     string    `json:"operation_phase"`      // Running / Succeeded / Failed / Error / Terminating
	OperationStartedAt time.Time `json:"operation_started_at"` // 最近 sync 操作开始时间（零值表示无操作）
}

// GetAppStatus 读取 application 的 sync / health / 最近 sync 操作状态
func (c *ArgocdClient) GetAppStatus(ctx context.Context, name string) (*AppStatus, error) {
	raw, _, err := c.do(ctx, "GET", "/api/v1/applications/"+name, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Status struct {
			Sync struct {
				Status string `json:"status"`
			} `json:"sync"`
			Health struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"health"`
			OperationState *struct {
				Phase     string    `json:"phase"`
				StartedAt time.Time `json:"startedAt"`
			} `json:"operationState"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse argocd app: %w", err)
	}
	st := &AppStatus{
		Name:       name,
		SyncStatus: resp.Status.Sync.Status,
		Health:     resp.Status.Health.Status,
		Message:    resp.Status.Health.Message,
	}
	if resp.Status.OperationState != nil {
		st.OperationPhase = resp.Status.OperationState.Phase
		st.OperationStartedAt = resp.Status.OperationState.StartedAt
	}
	return st, nil
}

// ResourceNode 是 argocd 资源树里一个节点（Pod / Deployment / Service 等）
type ResourceNode struct {
	Kind      string
	Name      string
	Namespace string
	Health    string // Healthy / Progressing / Degraded / Missing / Suspended / Unknown
	HealthMsg string // 例: "back-off 5m0s restarting failed container=foo pod=..."
	// 来自 node.info[] 的关键字段。argocd UI 显示的 "Status Reason" / "Containers" / "Restart Count"
	StatusReason string // 例: "CrashLoopBackOff" / "ImagePullBackOff" / "OOMKilled" / "Pending"
	ContainersOK string // 例: "0/1"
	RestartCount string // 例: "3"
}

// GetAppResourceTree 拉 argocd application 的完整资源树。
// 失败排查用：失败的 deploy 调这个找出究竟哪个 Pod 起不来 + 为什么。
func (c *ArgocdClient) GetAppResourceTree(ctx context.Context, name string) ([]ResourceNode, error) {
	raw, _, err := c.do(ctx, "GET", "/api/v1/applications/"+name+"/resource-tree", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Nodes []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			Health    *struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"health"`
			Info []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"info"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse resource-tree: %w", err)
	}
	out := make([]ResourceNode, 0, len(resp.Nodes))
	for _, n := range resp.Nodes {
		rn := ResourceNode{
			Kind:      n.Kind,
			Name:      n.Name,
			Namespace: n.Namespace,
		}
		if n.Health != nil {
			rn.Health = n.Health.Status
			rn.HealthMsg = n.Health.Message
		}
		for _, kv := range n.Info {
			switch kv.Name {
			case "Status Reason":
				rn.StatusReason = kv.Value
			case "Containers":
				rn.ContainersOK = kv.Value
			case "Restart Count":
				rn.RestartCount = kv.Value
			}
		}
		out = append(out, rn)
	}
	return out, nil
}

// Sync 触发应用同步
func (c *ArgocdClient) Sync(ctx context.Context, name string) error {
	body := map[string]interface{}{
		"revision": "HEAD",
		"prune":    false,
		"dryRun":   false,
		"strategy": map[string]interface{}{"hook": map[string]interface{}{}},
	}
	_, _, err := c.do(ctx, "POST", "/api/v1/applications/"+name+"/sync", body)
	return err
}

// RestartDeployment 调用 argocd application resource action = restart
// 对 Deployment 做一次 rollout 重启
func (c *ArgocdClient) RestartDeployment(ctx context.Context, appName, namespace, deploymentName string) error {
	path := fmt.Sprintf(
		"/api/v1/applications/%s/resource/actions?resourceName=%s&namespace=%s&group=apps&version=v1&kind=Deployment",
		appName, deploymentName, namespace,
	)
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+path, bytes.NewReader([]byte(`"restart"`)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("restart %s/%s: %d %s", namespace, deploymentName, resp.StatusCode, string(raw))
	}
	return nil
}
