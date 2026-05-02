package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VmAgentClient 调 deploy-vm-agent 的 HTTP 接口（agent 协议见 opsplatform-deploy-vm-agent/）
//
//	BaseURL 形如 https://10.x.x.x:8443，不带尾 /
//	Token 透传给 agent 的 Authorization: Bearer <token>
//	内网通常证书为自签或 host 不匹配 → InsecureSkipVerify 默认 true（依赖 token 鉴权保证安全）
type VmAgentClient struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewVmAgentClient(baseURL, token string) *VmAgentClient {
	return &VmAgentClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP: &http.Client{
			// 60s：覆盖 ListServices 时 agent 先 git pull (~7s 最坏含重试) + 扫盘 (~1s) 链路；
			// 单个调用真要超过 60s 应该报错而不是憋着。流式 SSE 日志请求自己造 client 不受这里限制。
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

func (c *VmAgentClient) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
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
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return raw, resp.StatusCode, fmt.Errorf("agent %s %s -> %d: %s", method, path, resp.StatusCode, string(raw))
	}
	return raw, resp.StatusCode, nil
}

// AgentHealth /v1/health 返回值
type AgentHealth struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	AnsibleRoot   string `json:"ansible_root"`
	VersionRoot   string `json:"version_root"`
	MaxConcurrent int    `json:"max_concurrent"`
}

func (c *VmAgentClient) Health(ctx context.Context) (*AgentHealth, error) {
	raw, _, err := c.do(ctx, "GET", "/v1/health", nil)
	if err != nil {
		return nil, err
	}
	var h AgentHealth
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// AgentService 服务发现返回的单个服务
type AgentService struct {
	Name         string   `json:"name"`
	AnsibleGroup string   `json:"ansible_group"`
	Hosts        []string `json:"hosts"`
	AppName      string   `json:"app_name"`
	SourcePath   string   `json:"source_path"`
}

// ListServices /v1/services?project=&env=&git_sync=true
//
//	默认带 git_sync=true：让 agent 先 git pull 拿到 ansible 仓库最新 playbook 再扫盘。
//	对齐 K8s 那边「同步模块」按钮的语义（git pull + 扫盘 一气呵成），用户在 UI 点
//	"🔄 同步服务列表"就不用再 ssh 到 agent 主机手动 git pull 了。
//	受 agent.manager.gitMu 保护，跟批量发布共享同一把锁。
func (c *VmAgentClient) ListServices(ctx context.Context, project, env string) ([]AgentService, error) {
	path := fmt.Sprintf("/v1/services?project=%s&env=%s&git_sync=true", urlQ(project), urlQ(env))
	raw, _, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Services []AgentService `json:"services"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Services, nil
}

// AgentTaskSpec 触发任务参数（agent 那边的 Spec）
type AgentTaskSpec struct {
	Action  string `json:"action"`            // rsync / update_version / git_sync
	Project string `json:"project"`
	Env     string `json:"env,omitempty"`
	Service string `json:"service"`
	Version string `json:"version,omitempty"`
}

// SubmitTask POST /v1/tasks → { task_id, status }
func (c *VmAgentClient) SubmitTask(ctx context.Context, spec AgentTaskSpec) (string, error) {
	raw, _, err := c.do(ctx, "POST", "/v1/tasks", spec)
	if err != nil {
		return "", err
	}
	var resp struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	return resp.TaskID, nil
}

// AgentBatchItem 批量请求里的单个 service 项
type AgentBatchItem struct {
	Service string `json:"service"`
	Version string `json:"version,omitempty"`
}

// AgentBatchSpec 批量提交参数（agent /v1/tasks/batch 入参）
type AgentBatchSpec struct {
	Action   string           `json:"action"`
	Project  string           `json:"project"`
	Env      string           `json:"env,omitempty"`
	Services []AgentBatchItem `json:"services"`
}

// AgentBatchResult 批量提交返回的单条 task
type AgentBatchResult struct {
	Service string `json:"service"`
	TaskID  string `json:"task_id"`
}

// SubmitBatch POST /v1/tasks/batch → { tasks: [{service, task_id}, ...] }
//
//	agent 内部：1 次 git sync（gitMu 串行 + 智能重试）→ 成功后并行起 N 个 work；
//	git 失败 → 返回的 task 都已创建但 status=failed，调用方 poll 时能看到统一错误。
//	agent 整体拒收（spec 校验失败 / service 锁占用）才返 error。
func (c *VmAgentClient) SubmitBatch(ctx context.Context, spec AgentBatchSpec) ([]AgentBatchResult, error) {
	raw, _, err := c.do(ctx, "POST", "/v1/tasks/batch", spec)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Tasks []AgentBatchResult `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

// AgentTask 任务状态完整字段（agent 那边的 Task struct）
type AgentTask struct {
	ID         string    `json:"id"`
	Spec       AgentTaskSpec `json:"spec"`
	Status     string    `json:"status"` // pending/running/success/failed/canceled
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	ErrMsg     string    `json:"err_msg,omitempty"`
}

// GetTask GET /v1/tasks/<id>
func (c *VmAgentClient) GetTask(ctx context.Context, taskID string) (*AgentTask, error) {
	raw, _, err := c.do(ctx, "GET", "/v1/tasks/"+taskID, nil)
	if err != nil {
		return nil, err
	}
	var t AgentTask
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTaskLogs 一次性拉日志（不流式），since=offset
//
//	返回 (logs, total_bytes, error)
func (c *VmAgentClient) GetTaskLogs(ctx context.Context, taskID string, since int) (string, int, error) {
	path := fmt.Sprintf("/v1/tasks/%s/logs?since=%d", taskID, since)
	raw, _, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return "", 0, err
	}
	return string(raw), len(raw) + since, nil
}

// CancelTask POST /v1/tasks/<id>/cancel
func (c *VmAgentClient) CancelTask(ctx context.Context, taskID string) error {
	_, _, err := c.do(ctx, "POST", "/v1/tasks/"+taskID+"/cancel", nil)
	return err
}

// StreamTaskLogs 把 agent SSE 流转发给 caller 的 io.Writer（前端 SSE pipe 用）
//
//	阻塞直到 ctx.Done 或 agent 关连接
func (c *VmAgentClient) StreamTaskLogs(ctx context.Context, taskID string, since int, w io.Writer) error {
	path := fmt.Sprintf("/v1/tasks/%s/logs?since=%d&stream=true", taskID, since)
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	// 流式请求不能用默认 30s timeout，自己造一个 client
	streamClient := &http.Client{
		Transport: c.HTTP.Transport,
		// Timeout: 0 → 不超时，靠 ctx 控制
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("agent stream returned %d", resp.StatusCode)
	}
	// 直接 io.Copy，前端拿到的就是 SSE 协议（event:/data:/\n\n）
	_, err = io.Copy(w, resp.Body)
	return err
}
