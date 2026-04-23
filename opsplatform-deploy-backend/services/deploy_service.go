package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"opsplatform-deploy-backend/models"
)

type DeployService struct {
	Git *GitService
}

func NewDeployService(git *GitService) *DeployService {
	return &DeployService{Git: git}
}

// Module 简化视图（handler 把 models.Module 映射过来）
type Module struct {
	Name         string
	CurrentTag   string
	ChartRelPath string // e.g. charts/g32-uat/atmosphere-frontend/values.yaml
	ArgocdApp    string
	Namespace    string // 该模块实际部署的 K8s namespace（多 ns 项目必填）
}

// DiffEntry preview/update 共用的一条 diff 记录
type DiffEntry struct {
	Module  string `json:"module"`
	FromTag string `json:"from_tag"`
	ToTag   string `json:"to_tag"`
	Path    string `json:"path"`
	IsNew   bool   `json:"is_new"`
	Skip    bool   `json:"skip,omitempty"`
}

// ParseBatchInput 解析多行 "模块:tag"，忽略空行和注释
func ParseBatchInput(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 || idx >= len(line)-1 {
			continue
		}
		mod := strings.TrimSpace(line[:idx])
		tag := strings.TrimSpace(line[idx+1:])
		tag = strings.Trim(tag, `"'`)
		if mod == "" || tag == "" {
			continue
		}
		out[mod] = tag
	}
	return out
}

// PreviewImage 生成 diff，不改文件
func PreviewImage(pending map[string]string, modules map[string]Module) []DiffEntry {
	diff := []DiffEntry{}
	for name, newTag := range pending {
		e := DiffEntry{Module: name, ToTag: newTag}
		if m, ok := modules[name]; ok {
			e.FromTag = m.CurrentTag
			e.Path = m.ChartRelPath
			e.Skip = m.CurrentTag == newTag
		} else {
			e.IsNew = true
		}
		diff = append(diff, e)
	}
	return diff
}

// BuildValuesPath: {chart_base_path}/{module}/values.yaml
func BuildValuesPath(chartBasePath, moduleName string) string {
	return fmt.Sprintf("%s/%s/values.yaml", strings.TrimRight(chartBasePath, "/"), moduleName)
}

// --- UpdateImage ---

type UpdateImageInput struct {
	ProjectEnvName  string
	GitRepo         string
	GitBranch       string
	ChartBasePath   string
	GitRetry        int
	Operator        string
	Pending         map[string]string
	Modules         map[string]Module
	AutoSync        bool
	ArgocdClient    *ArgocdClient
	PollIntervalSec int
	PollTimeoutSec  int
	ConcurrentLimit int
	// OnProgress 可选：每个 app 轮询中间态/完成时回调，传入当前完整快照供渐进式写 DB。
	OnProgress func(snapshot []models.ArgocdAppResult)
}

type UpdateImageResult struct {
	GitCommit     string
	GitCommitURL  string
	Changes       []models.Change
	Skipped       []string
	NotFound      []string
	ArgocdResults []models.ArgocdAppResult
	Status        string
	Err           error
}

func (d *DeployService) UpdateImage(ctx context.Context, in UpdateImageInput) *UpdateImageResult {
	res := &UpdateImageResult{}
	d.Git.Locks.Acquire(in.ProjectEnvName)
	defer d.Git.Locks.Release(in.ProjectEnvName)

	gctx, cancel := GitCtx(ctx, 120)
	defer cancel()
	if err := d.Git.EnsureClone(gctx, in.ProjectEnvName, in.GitRepo, in.GitBranch); err != nil {
		res.Err = fmt.Errorf("git pull: %w", err)
		res.Status = models.StatusFailed
		return res
	}

	for name, newTag := range in.Pending {
		m, ok := in.Modules[name]
		if !ok {
			res.NotFound = append(res.NotFound, name)
			continue
		}
		raw, err := d.Git.ReadFile(in.ProjectEnvName, m.ChartRelPath)
		if err != nil {
			res.NotFound = append(res.NotFound, name)
			continue
		}
		newContent, changed, err := UpdateImageTag(raw, newTag)
		if err != nil {
			res.Err = fmt.Errorf("yaml edit %s: %w", name, err)
			res.Status = models.StatusFailed
			return res
		}
		if !changed {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		if err := d.Git.WriteFile(in.ProjectEnvName, m.ChartRelPath, newContent); err != nil {
			res.Err = fmt.Errorf("write %s: %w", name, err)
			res.Status = models.StatusFailed
			return res
		}
		res.Changes = append(res.Changes, models.Change{Module: name, FromTag: m.CurrentTag, ToTag: newTag})
	}

	if len(res.Changes) == 0 {
		res.Status = models.StatusNoChange
		return res
	}

	modNames := []string{}
	for _, c := range res.Changes {
		modNames = append(modNames, c.Module)
	}
	msg := fmt.Sprintf("[deploy-center] update image: %s by %s\n\nvia deploy-center",
		strings.Join(modNames, ","), in.Operator)
	sha, err := d.Git.CommitAll(gctx, in.ProjectEnvName, in.Operator, msg)
	if err != nil {
		res.Err = fmt.Errorf("commit: %w", err)
		res.Status = models.StatusFailed
		return res
	}
	if sha == "" {
		res.Status = models.StatusNoChange
		return res
	}
	res.GitCommit = sha
	res.GitCommitURL = CommitURL(in.GitRepo, sha)

	if err := d.Git.Push(gctx, in.ProjectEnvName, in.GitBranch, in.GitRetry); err != nil {
		res.Err = fmt.Errorf("push: %w", err)
		res.Status = models.StatusFailed
		return res
	}

	if !in.AutoSync {
		res.Status = models.StatusSuccess
		return res
	}

	// 有界并发池并行 sync + poll，与 Restart 保持一致的体验
	interval := in.PollIntervalSec
	if interval <= 0 {
		interval = 5
	}
	timeout := in.PollTimeoutSec
	if timeout <= 0 {
		timeout = 180
	}
	limit := in.ConcurrentLimit
	if limit <= 0 {
		limit = 10
	}
	// ArgoCD app name 与 module scanner 约定一致：全小写 kebab-case
	peNameLower := strings.ToLower(in.ProjectEnvName)
	type syncJob struct {
		module string
		app    string
	}
	jobs := make([]syncJob, 0, len(res.Changes))
	for _, c := range res.Changes {
		jobs = append(jobs, syncJob{
			module: c.Module,
			app:    strings.ToLower(c.Module) + "-" + peNameLower,
		})
	}

	res.ArgocdResults = RunBoundedConcurrent(ctx, jobs, limit,
		func(c context.Context, j syncJob, publish func(models.ArgocdAppResult)) models.ArgocdAppResult {
			// 初始状态先让前端看到这一行
			publish(models.ArgocdAppResult{
				App: j.app, SyncStatus: "Syncing", Health: "Progressing",
				DurationSec: 0, Msg: "calling argocd sync",
			})
			if err := in.ArgocdClient.Sync(c, j.app); err != nil {
				return models.ArgocdAppResult{
					App: j.app, SyncStatus: "failed",
					Msg: "sync api: " + err.Error(),
				}
			}
			return *PollUntilStable(c, in.ArgocdClient, j.app, interval, timeout,
				func(tick *models.ArgocdAppResult) { publish(*tick) })
		},
		func(_ int, snapshot []models.ArgocdAppResult) {
			if in.OnProgress != nil {
				in.OnProgress(snapshot)
			}
		},
	)

	allOK := true
	for _, ar := range res.ArgocdResults {
		if ar.SyncStatus != "Synced" || ar.Health != "Healthy" {
			allOK = false
			break
		}
	}
	if allOK {
		res.Status = models.StatusSuccess
	} else {
		res.Status = models.StatusPartial
	}
	return res
}

// --- Restart ---

type RestartInput struct {
	ProjectEnvName  string
	Namespace       string // 默认 namespace，模块自身 Namespace 为空时 fallback
	Modules         map[string]Module
	ModuleNames     []string
	ArgocdClient    *ArgocdClient
	PollIntervalSec int
	PollTimeoutSec  int
	ConcurrentLimit int // 并发 poll 池大小，<=0 默认 10
	// OnProgress 可选：每个 app 轮询完成时回调，传入当前快照供渐进式写 DB。
	// 调用方法内部已上锁，回调函数可直接读取 snapshot；但请勿持有引用到锁外。
	OnProgress func(snapshot []models.ArgocdAppResult)
}

type RestartResult struct {
	ArgocdResults []models.ArgocdAppResult
	Status        string
	Err           error
}

// Restart：
//  1. 逐个调用 ArgoCD RestartDeployment 触发 rollout（收集"没能触发"的失败项）
//  2. 有界并发池轮询每个 app 到 Healthy/Degraded/超时
//  3. 所有 app Healthy=success；任一 Degraded/timeout=failed；混合=partial
//
// 严格性：只看 ArgoCD application.health。Healthy 意味着底层 Deployment 所有 pod 都 Ready。
// Degraded 会立即跳出（在 PollUntilStable 内），避免等满超时。
//
// 并发模型：见 services/concurrent.go 顶部注释。默认 10 并发；
// 每个 app 完成时通过 OnProgress 回调提供给调用方做渐进式 DB 更新。
func (d *DeployService) Restart(ctx context.Context, in RestartInput) *RestartResult {
	res := &RestartResult{}

	// Step 1: 触发 restart，记录成功触发的 app
	type triggered struct {
		app string
		mod string
		ns  string
	}
	var toPoll []triggered
	var preFail []models.ArgocdAppResult
	for _, name := range in.ModuleNames {
		m, ok := in.Modules[name]
		if !ok {
			preFail = append(preFail, models.ArgocdAppResult{
				App: name, SyncStatus: "failed", Msg: "module not found",
			})
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ns := m.Namespace
		if ns == "" {
			ns = in.Namespace
		}
		err := in.ArgocdClient.RestartDeployment(rctx, m.ArgocdApp, ns, name)
		cancel()
		if err != nil {
			preFail = append(preFail, models.ArgocdAppResult{
				App: m.ArgocdApp, SyncStatus: "failed", Msg: err.Error(),
			})
			continue
		}
		toPoll = append(toPoll, triggered{app: m.ArgocdApp, mod: name, ns: ns})
	}

	// Step 2: 等 3s 让 ArgoCD 感知到 rollout（避免 poll 到旧 Healthy）
	if len(toPoll) > 0 {
		time.Sleep(3 * time.Second)
	}

	// Step 3: 有界并发池 poll
	interval := in.PollIntervalSec
	if interval <= 0 {
		interval = 5
	}
	timeout := in.PollTimeoutSec
	if timeout <= 0 {
		timeout = 180
	}
	limit := in.ConcurrentLimit
	if limit <= 0 {
		limit = 10
	}

	pollResults := RunBoundedConcurrent(ctx, toPoll, limit,
		func(c context.Context, t triggered, publish func(models.ArgocdAppResult)) models.ArgocdAppResult {
			// 入池立刻推一个"Progressing/等待中"初始状态，让前端 3 行同时出现
			publish(models.ArgocdAppResult{
				App:         t.app,
				SyncStatus:  "Progressing",
				Health:      "Progressing",
				DurationSec: 0,
				Msg:         "waiting for ArgoCD",
			})
			// 每次 ArgoCD poll 后都把中间状态 publish 出去（5s 节奏），前端靠这个 + 本地秒表做秒级跳动
			return *PollUntilStable(c, in.ArgocdClient, t.app, interval, timeout,
				func(tick *models.ArgocdAppResult) { publish(*tick) })
		},
		func(_ int, snapshot []models.ArgocdAppResult) {
			if in.OnProgress != nil {
				merged := make([]models.ArgocdAppResult, 0, len(preFail)+len(snapshot))
				merged = append(merged, preFail...)
				merged = append(merged, snapshot...)
				in.OnProgress(merged)
			}
		},
	)

	// Step 4: 聚合结果
	res.ArgocdResults = append(res.ArgocdResults, preFail...)
	res.ArgocdResults = append(res.ArgocdResults, pollResults...)

	okCount, failCount := 0, 0
	for _, ar := range res.ArgocdResults {
		if ar.SyncStatus == "Synced" && ar.Health == "Healthy" {
			okCount++
		} else {
			failCount++
		}
	}
	switch {
	case failCount == 0 && okCount > 0:
		res.Status = models.StatusSuccess
	case okCount == 0:
		res.Status = models.StatusFailed
	default:
		res.Status = models.StatusPartial
	}
	return res
}

// --- Rollback ---

// BuildRollbackPending 根据 refChanges + 选中的模块子集，生成反向 pending
func BuildRollbackPending(refChanges []models.Change, selectedModules []string) map[string]string {
	sel := map[string]bool{}
	for _, m := range selectedModules {
		sel[m] = true
	}
	out := map[string]string{}
	for _, c := range refChanges {
		if sel[c.Module] {
			out[c.Module] = c.FromTag
		}
	}
	return out
}
