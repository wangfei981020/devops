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

	allOK := true
	for _, c := range res.Changes {
		appName := c.Module + "-" + in.ProjectEnvName
		if err := in.ArgocdClient.Sync(ctx, appName); err != nil {
			res.ArgocdResults = append(res.ArgocdResults, models.ArgocdAppResult{
				App: appName, SyncStatus: "failed", Msg: err.Error(),
			})
			allOK = false
			continue
		}
		ar := PollUntilStable(ctx, in.ArgocdClient, appName, in.PollIntervalSec, in.PollTimeoutSec)
		res.ArgocdResults = append(res.ArgocdResults, *ar)
		if ar.SyncStatus != "Synced" || ar.Health != "Healthy" {
			allOK = false
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
	ProjectEnvName string
	Namespace      string // 默认 namespace，模块自身 Namespace 为空时 fallback
	Modules        map[string]Module
	ModuleNames    []string
	ArgocdClient   *ArgocdClient
}

type RestartResult struct {
	ArgocdResults []models.ArgocdAppResult
	Status        string
	Err           error
}

func (d *DeployService) Restart(ctx context.Context, in RestartInput) *RestartResult {
	res := &RestartResult{}
	okCount, failCount := 0, 0
	for _, name := range in.ModuleNames {
		m, ok := in.Modules[name]
		if !ok {
			res.ArgocdResults = append(res.ArgocdResults, models.ArgocdAppResult{
				App: name, SyncStatus: "failed", Msg: "module not found",
			})
			failCount++
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		// 优先用模块自己的 namespace，空则回退到 project_env 默认
		ns := m.Namespace
		if ns == "" {
			ns = in.Namespace
		}
		err := in.ArgocdClient.RestartDeployment(rctx, m.ArgocdApp, ns, name)
		cancel()
		if err != nil {
			res.ArgocdResults = append(res.ArgocdResults, models.ArgocdAppResult{
				App: m.ArgocdApp, SyncStatus: "failed", Msg: err.Error(),
			})
			failCount++
			continue
		}
		res.ArgocdResults = append(res.ArgocdResults, models.ArgocdAppResult{
			App: m.ArgocdApp, SyncStatus: "Restarted", Msg: "restart triggered",
		})
		okCount++
	}
	switch {
	case failCount == 0:
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
