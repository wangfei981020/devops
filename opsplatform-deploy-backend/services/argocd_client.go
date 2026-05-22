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
	"sort"
	"strings"
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

// ListAppNames 拉某 ArgoCD 实例下所有 application 名字列表（批量缓存用）
//
//	可选 selector 过滤（如 "project=g50-uat"），空则拉全量
//	相比逐个 GetApp，此方法发 1 个请求即可，适合做"批量校验前刷新缓存"
func (c *ArgocdClient) ListAppNames(ctx context.Context, selector string) ([]string, error) {
	path := "/api/v1/applications"
	if selector != "" {
		path += "?selector=" + urlQ(selector)
	}
	raw, _, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse argocd app list: %w", err)
	}
	out := make([]string, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item.Metadata.Name != "" {
			out = append(out, item.Metadata.Name)
		}
	}
	return out, nil
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
	UID       string // k8s resource uid，调 events 接口必填（按 involvedObject.uid 过滤）
	Health    string // Healthy / Progressing / Degraded / Missing / Suspended / Unknown
	HealthMsg string // 例: "back-off 5m0s restarting failed container=foo pod=..."
	// 来自 node.info[] 的关键字段。argocd UI 显示的 "Status Reason" / "Containers" / "Restart Count"
	StatusReason string // 例: "CrashLoopBackOff" / "ImagePullBackOff" / "OOMKilled" / "Pending"
	ContainersOK string // 例: "0/1"
	RestartCount string // 例: "3"
	// v127 起：用来把"本次发布产生的新 RS pod"跟"老 RS 残留的 pod"区分开
	// pod 的 parentRefs 指向 ReplicaSet；RS 的 parentRefs 指向 Deployment。
	ParentRefs []ResourceRef
	// pod / RS / 等 K8s 资源的 creationTimestamp。多个 RS 共存时按这个挑最新的 = 本次发布的 RS。
	CreatedAt time.Time
}

// ResourceRef 是 ArgoCD resource-tree 里 parentRefs 数组的元素。
// pod 视角看 parentRefs[0].Kind=ReplicaSet；RS 视角看 parentRefs[0].Kind=Deployment。
type ResourceRef struct {
	Kind      string
	Name      string
	Namespace string
	UID       string
}

// PodEvent 是从 ArgoCD events 接口透传的 k8s Event 关键字段
type PodEvent struct {
	Type      string    `json:"type"`       // Normal / Warning
	Reason    string    `json:"reason"`     // FailedScheduling / ImagePullBackOff / BackOff / Pulled ...
	Message   string    `json:"message"`
	Count     int       `json:"count"`
	First     time.Time `json:"first_at"`
	Last      time.Time `json:"last_at"`
	FieldPath string    `json:"field_path"` // 例: spec.containers{frontend}
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
			UID       string `json:"uid"`
			Health    *struct {
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"health"`
			Info []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"info"`
			ParentRefs []struct {
				Kind      string `json:"kind"`
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
				UID       string `json:"uid"`
			} `json:"parentRefs"`
			CreatedAt time.Time `json:"createdAt"`
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
			UID:       n.UID,
			CreatedAt: n.CreatedAt,
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
		for _, p := range n.ParentRefs {
			rn.ParentRefs = append(rn.ParentRefs, ResourceRef{
				Kind: p.Kind, Name: p.Name, Namespace: p.Namespace, UID: p.UID,
			})
		}
		out = append(out, rn)
	}
	return out, nil
}

// GetPodLogs 拉指定 pod 的容器日志（默认主 container）。
//
// argocd 该接口返回 NDJSON 流（每行 `{"result":{"content":"...","timeStampStr":"..."}}`），
// 我们解析出 content 拼回纯文本。若返回不是 NDJSON（旧版本 / 错误），原样回退。
//
// previous=true 时拿「上一次崩溃前」容器的日志（kubectl logs --previous）——
// 这是 crash-loop 排查关键：当前容器才起来几秒没什么日志，上次容器才记着 panic。
func (c *ArgocdClient) GetPodLogs(
	ctx context.Context,
	appName, namespace, podName, container string,
	tailLines int, previous bool,
) (string, error) {
	if tailLines <= 0 {
		tailLines = 200
	}
	if tailLines > 2000 {
		tailLines = 2000
	}
	q := fmt.Sprintf("namespace=%s&podName=%s&tailLines=%d&previous=%t",
		urlQ(namespace), urlQ(podName), tailLines, previous)
	if container != "" {
		q += "&container=" + urlQ(container)
	}
	path := "/api/v1/applications/" + appName + "/pods/" + podName + "/logs?" + q

	// 单独 30s timeout（不影响 client 默认 timeout 给其他接口用）
	lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, _, err := c.do(lctx, "GET", path, nil)
	if err != nil {
		return "", err
	}
	// 尝试 NDJSON 解析；失败则原样返回（兼容旧版本 / 错误页）
	lines := bytes.Split(raw, []byte("\n"))
	var out bytes.Buffer
	parsed := 0
	for _, ln := range lines {
		ln = bytes.TrimSpace(ln)
		if len(ln) == 0 {
			continue
		}
		var entry struct {
			Result struct {
				Content      string `json:"content"`
				TimeStampStr string `json:"timeStampStr"`
			} `json:"result"`
		}
		if err := json.Unmarshal(ln, &entry); err != nil {
			// 不是 JSON，整个 raw 当纯文本回退
			return string(raw), nil
		}
		out.WriteString(entry.Result.Content)
		out.WriteByte('\n')
		parsed++
	}
	if parsed == 0 {
		return string(raw), nil
	}
	result := out.String()

	// kubectl stderr 兜底检测：ArgoCD 在 pod 已 GC / container 还没起 / previous 不存在等场景
	// 会把 kubectl 报错通过 NDJSON 200 包装返回（content 字段塞 "pods xxx not found" 之类）。
	// 上面解析逻辑会把这段 stderr 当真日志返回，最终被归档进 MinIO 误导用户。
	//
	// 判定：单行短文本 + 已知 kubectl stderr pattern → 返回 err 让上游跳过写入。
	// 真日志几乎都有多行换行 + 长度远大于 500B，所以这个 filter 误伤率极低。
	trimmed := strings.TrimSpace(result)
	if len(trimmed) > 0 && len(trimmed) < 500 && strings.Count(trimmed, "\n") == 0 {
		knownStderrPatterns := []string{
			"not found",                     // pods "xxx" not found
			"previous terminated container", // previous terminated container "xxx" in pod "yyy" not found
			"is waiting to start",           // container "xxx" in pod "yyy" is waiting to start
		}
		for _, p := range knownStderrPatterns {
			if strings.Contains(trimmed, p) {
				return "", fmt.Errorf("argocd returned kubectl stderr (not real log): %s", trimmed)
			}
		}
	}
	return result, nil
}

// GetPodEvents 拉指定 pod 的 k8s events（FailedScheduling / ImagePullBackOff / BackOff 等）
//
//	走 ArgoCD `/applications/{name}/events?resourceUID=&resourceNamespace=&resourceName=`，
//	后端转 kubectl get events --field-selector involvedObject.uid=<uid> 拿数据。
//	uid 是必填的：不传 ArgoCD 会返回该 application 的所有 sync 事件，不是 pod 事件。
//	返回按 lastTimestamp 倒序（最近的事件在前）。
func (c *ArgocdClient) GetPodEvents(
	ctx context.Context,
	appName, podUID, podNamespace, podName string,
) ([]PodEvent, error) {
	if podUID == "" {
		return nil, nil // 没 uid 拉不出 events，静默返回空（旧 pod 已被收走的常见情况）
	}
	q := fmt.Sprintf("resourceUID=%s&resourceNamespace=%s&resourceName=%s",
		urlQ(podUID), urlQ(podNamespace), urlQ(podName))
	path := "/api/v1/applications/" + appName + "/events?" + q

	ectx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	raw, _, err := c.do(ectx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Items []struct {
			Type           string    `json:"type"`
			Reason         string    `json:"reason"`
			Message        string    `json:"message"`
			Count          int       `json:"count"`
			FirstTimestamp time.Time `json:"firstTimestamp"`
			LastTimestamp  time.Time `json:"lastTimestamp"`
			InvolvedObject struct {
				FieldPath string `json:"fieldPath"`
			} `json:"involvedObject"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse events: %w", err)
	}
	out := make([]PodEvent, 0, len(resp.Items))
	for _, it := range resp.Items {
		out = append(out, PodEvent{
			Type:      it.Type,
			Reason:    it.Reason,
			Message:   it.Message,
			Count:     it.Count,
			First:     it.FirstTimestamp,
			Last:      it.LastTimestamp,
			FieldPath: it.InvolvedObject.FieldPath,
		})
	}
	// 按 Last 倒序（最近事件在前）
	sort.Slice(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out, nil
}

// urlQ 简单 URL query 转义；只处理 = & 空格 等基础字符
func urlQ(s string) string {
	r := bytes.NewBuffer(nil)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9',
			ch == '-' || ch == '_' || ch == '.' || ch == '~':
			r.WriteByte(ch)
		default:
			fmt.Fprintf(r, "%%%02X", ch)
		}
	}
	return r.String()
}

// Sync 触发应用同步
//
//	body 不传 revision：让 ArgoCD 用 Application 自己的 spec.source.targetRevision。
//	历史教训：写死 "revision": "HEAD" 时，如果 Application 的 targetRevision 是 "main"
//	（字符串不等），且 syncPolicy.automated 开着，ArgoCD 会拒绝：
//	  Cannot sync to HEAD: auto-sync currently set to main
//	省掉 revision 字段后，ArgoCD 永远跟自己的 targetRevision 一致，
//	不论用户配的是 main / HEAD / pre-uat 都能正确触发同步。
func (c *ArgocdClient) Sync(ctx context.Context, name string) error {
	body := map[string]interface{}{
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
