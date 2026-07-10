package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 服务编排引擎：把"新增一个模块"落地到 git（拷样板目录 → 改 Chart 名 → 写 values →
// 追加 -apps → helm 校验 → 加锁 + 硬同步 + 安全提交）。
//
// 复用已有 GitService 原语；冲突用真 rebase（git pull --rebase 会把本地提交重放到远端最新之上），
// 不用 Push() 里的 reset --hard（那会丢掉我们刚生成的提交）。

// ModuleSpec 描述一次新增模块所需的全部信息。
type ModuleSpec struct {
	// 目标（新模块落哪）
	TargetEnv           string // project_env.name
	TargetRepoURL       string
	TargetBranch        string
	TargetChartBasePath string
	ModuleName          string // 完整模块名（= 目录名 = Chart.yaml name = -apps key）
	ValuesYAML          []byte // 前端预填+用户编辑后的最终 values.yaml
	Namespace           string
	Disable             bool // -apps 条目：true=app-of-apps 不生成 Application（安全预演）
	DisableAutomated    bool

	// 样板来源（从哪拷）
	SrcEnv           string
	SrcRepoURL       string
	SrcBranch        string
	SrcChartBasePath string
	SrcService       string
}

// PreviewResult 预览：不提交，返回将改动的文件 + helm 校验结果。
type PreviewResult struct {
	ChangedFiles []string `json:"changed_files"`
	HelmOK       bool     `json:"helm_ok"`
	HelmSkipped  bool     `json:"helm_skipped"` // 环境没装 helm CLI 时为 true
	HelmOutput   string   `json:"helm_output"`
}

// SubmitResult 提交结果。
type SubmitResult struct {
	CommitSHA string `json:"commit_sha"`
	CommitURL string `json:"commit_url"`
	PreviewResult
}

// PreviewNewModule 在目标 env 的锁下：硬同步 → 生成文件 → helm 校验 → 算 diff → 丢弃（不提交）。
func (g *GitService) PreviewNewModule(ctx context.Context, spec ModuleSpec) (*PreviewResult, error) {
	g.Locks.Acquire(spec.TargetEnv)
	defer g.Locks.Release(spec.TargetEnv)

	if err := g.stageNewModule(ctx, spec); err != nil {
		_ = g.discardWorktree(ctx, spec.TargetEnv, spec.TargetBranch)
		return nil, err
	}
	res, err := g.inspectStaged(ctx, spec)
	// 预览结束一律丢弃工作区改动，保持 cache 干净
	_ = g.discardWorktree(ctx, spec.TargetEnv, spec.TargetBranch)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// SubmitNewModule 在目标 env 的锁下：硬同步 → 生成文件 → helm 校验(不过不提交) → commit → 安全 push。
func (g *GitService) SubmitNewModule(ctx context.Context, spec ModuleSpec, operator, commitMsg string) (*SubmitResult, error) {
	g.Locks.Acquire(spec.TargetEnv)
	defer g.Locks.Release(spec.TargetEnv)

	if err := g.stageNewModule(ctx, spec); err != nil {
		_ = g.discardWorktree(ctx, spec.TargetEnv, spec.TargetBranch)
		return nil, err
	}
	preview, err := g.inspectStaged(ctx, spec)
	if err != nil {
		_ = g.discardWorktree(ctx, spec.TargetEnv, spec.TargetBranch)
		return nil, err
	}
	if !preview.HelmOK && !preview.HelmSkipped {
		_ = g.discardWorktree(ctx, spec.TargetEnv, spec.TargetBranch)
		return nil, fmt.Errorf("helm 校验未通过，已阻止提交：\n%s", preview.HelmOutput)
	}
	sha, err := g.CommitAll(ctx, spec.TargetEnv, operator, commitMsg)
	if err != nil {
		_ = g.discardWorktree(ctx, spec.TargetEnv, spec.TargetBranch)
		return nil, err
	}
	if sha == "" {
		return nil, fmt.Errorf("没有产生任何改动（模块可能已存在）")
	}
	if err := g.safePush(ctx, spec.TargetEnv, spec.TargetBranch, 3); err != nil {
		return nil, err
	}
	return &SubmitResult{
		CommitSHA:     sha,
		CommitURL:     CommitURL(spec.TargetRepoURL, sha),
		PreviewResult: *preview,
	}, nil
}

// stageNewModule 硬同步目标(+来源)后，把新模块文件写进目标 clone 的工作区（不 commit）。
func (g *GitService) stageNewModule(ctx context.Context, spec ModuleSpec) error {
	if err := g.ensureRepos(ctx, spec); err != nil {
		return err
	}
	return g.writeModuleFiles(spec)
}

// ensureRepos 硬同步目标(+来源)仓库到远端最新（批量时只需调一次）。
func (g *GitService) ensureRepos(ctx context.Context, spec ModuleSpec) error {
	if err := g.EnsureClone(ctx, spec.TargetEnv, spec.TargetRepoURL, spec.TargetBranch); err != nil {
		return fmt.Errorf("同步目标仓库失败: %w", err)
	}
	if spec.SrcEnv != spec.TargetEnv {
		if err := g.EnsureClone(ctx, spec.SrcEnv, spec.SrcRepoURL, spec.SrcBranch); err != nil {
			return fmt.Errorf("同步样板仓库失败: %w", err)
		}
	}
	return nil
}

// writeModuleFiles 把一个模块的文件写进已同步的工作区（拷目录+改Chart名+写values+追加-apps），不 clone/不 commit。
// 批量时循环调用：每次追加到同一份工作区，-apps 条目顺序累加。
func (g *GitService) writeModuleFiles(spec ModuleSpec) error {
	srcDir := filepath.Join(g.RepoPath(spec.SrcEnv), spec.SrcChartBasePath, spec.SrcService)
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return fmt.Errorf("样板服务目录不存在: %s/%s", spec.SrcChartBasePath, spec.SrcService)
	}
	targetRel := filepath.Join(spec.TargetChartBasePath, spec.ModuleName)

	// 目标模块目录已存在 → 拦下，不覆盖
	if _, err := os.Stat(filepath.Join(g.RepoPath(spec.TargetEnv), targetRel)); err == nil {
		return fmt.Errorf("目标已存在模块目录 %s，请勿重复新增", targetRel)
	}

	// 逐文件拷贝：values.yaml 用请求内容；Chart.yaml 改 name；其余照抄
	err := filepath.Walk(srcDir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return err
		}
		var content []byte
		switch rel {
		case "values.yaml":
			content = spec.ValuesYAML
		case "Chart.yaml":
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			content, err = SetChartName(raw, spec.ModuleName)
			if err != nil {
				return err
			}
		default:
			content, err = os.ReadFile(p)
			if err != nil {
				return err
			}
		}
		return g.WriteFile(spec.TargetEnv, filepath.Join(targetRel, rel), content)
	})
	if err != nil {
		return fmt.Errorf("拷贝样板目录失败: %w", err)
	}

	// 追加 -apps 条目
	appsRel := spec.TargetChartBasePath + "-apps/values.yaml"
	appsContent, err := g.ReadFile(spec.TargetEnv, appsRel)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", appsRel, err)
	}
	newApps, err := AppendAppsEntry(appsContent, AppsEntry{
		Name:             spec.ModuleName,
		Namespace:        spec.Namespace,
		Disable:          spec.Disable,
		DisableAutomated: spec.DisableAutomated,
	})
	if err != nil {
		return err
	}
	return g.WriteFile(spec.TargetEnv, appsRel, newApps)
}

// inspectStaged 计算改动文件清单 + 跑 helm 校验（新模块 chart 目录）。
func (g *GitService) inspectStaged(ctx context.Context, spec ModuleSpec) (*PreviewResult, error) {
	path := g.RepoPath(spec.TargetEnv)
	if out, err := exec.CommandContext(ctx, "git", "-C", path, "add", "-A").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add: %w\n%s", err, out)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", path, "diff", "--cached", "--name-only").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff: %w\n%s", err, out)
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}

	res := &PreviewResult{ChangedFiles: files}
	ok, skipped, helmOut := helmTemplateCheck(ctx,
		filepath.Join(path, spec.TargetChartBasePath, spec.ModuleName),
		spec.ModuleName, spec.Namespace)
	res.HelmOK, res.HelmSkipped, res.HelmOutput = ok, skipped, helmOut
	return res, nil
}

// helmTemplateCheck 对新模块 chart 目录跑 helm template 校验渲染。
// helm 不在 PATH 时返回 skipped=true（本地无 helm 的开发环境不阻塞）。
func helmTemplateCheck(ctx context.Context, chartDir, releaseName, namespace string) (ok, skipped bool, output string) {
	if _, err := exec.LookPath("helm"); err != nil {
		return false, true, "helm CLI 未安装，跳过渲染校验"
	}
	args := []string{"template", releaseName, chartDir}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	if err != nil {
		return false, false, string(out)
	}
	return true, false, ""
}

// discardWorktree 丢弃工作区所有改动，恢复到远端最新（预览后 / 失败后清场）。
func (g *GitService) discardWorktree(ctx context.Context, envName, branch string) error {
	path := g.RepoPath(envName)
	for _, args := range [][]string{
		{"-C", path, "reset", "--hard", "HEAD"},
		{"-C", path, "clean", "-fd"},
	} {
		if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %w\n%s", args, err, out)
		}
	}
	return nil
}

// safePush 推当前分支；冲突用真 rebase 把本地提交重放到远端最新之上，不丢提交。
func (g *GitService) safePush(ctx context.Context, envName, branch string, retries int) error {
	if retries <= 0 {
		retries = 3
	}
	path := g.RepoPath(envName)
	for attempt := 1; attempt <= retries; attempt++ {
		out, err := exec.CommandContext(ctx, "git", "-C", path, "push", "origin", branch).CombinedOutput()
		if err == nil {
			return nil
		}
		lower := strings.ToLower(string(out))
		if !strings.Contains(lower, "rejected") && !strings.Contains(lower, "non-fast-forward") {
			return fmt.Errorf("git push: %w\n%s", err, ScrubSecrets(out))
		}
		// 真 rebase：把我们的提交重放到 origin/branch 最新之上
		if rout, rerr := exec.CommandContext(ctx, "git", "-C", path, "pull", "--rebase", "origin", branch).CombinedOutput(); rerr != nil {
			_, _ = exec.CommandContext(ctx, "git", "-C", path, "rebase", "--abort").CombinedOutput()
			return fmt.Errorf("推送冲突且 rebase 失败（可能有人同时改了同一文件）: %w\n%s", rerr, ScrubSecrets(rout))
		}
	}
	return fmt.Errorf("git push: 超过 %d 次重试仍失败", retries)
}

// ============ 批量新增 ============

// BatchRow 批量新增的一行（一个模块）。ValuesYAML 由 handler 用模板派生预填。
type BatchRow struct {
	ModuleName string
	Namespace  string
	ValuesYAML []byte
}

// BatchRowResult 每行的校验结果。
type BatchRowResult struct {
	ModuleName  string `json:"module_name"`
	HelmOK      bool   `json:"helm_ok"`
	HelmSkipped bool   `json:"helm_skipped"`
	Error       string `json:"error"`
}

// BatchResult 批量结果。AllOK=false 时不提交。
type BatchResult struct {
	Rows         []BatchRowResult `json:"rows"`
	ChangedFiles int              `json:"changed_files"`
	AllOK        bool             `json:"all_ok"`
	CommitSHA    string           `json:"commit_sha,omitempty"`
	CommitURL    string           `json:"commit_url,omitempty"`
}

// runBatch 同步一次 → 循环生成每行 + 逐行 helm 校验 → 全绿才一次性提交，否则丢弃。
func (g *GitService) runBatch(ctx context.Context, base ModuleSpec, rows []BatchRow, commit bool, operator, msg string) (*BatchResult, error) {
	g.Locks.Acquire(base.TargetEnv)
	defer g.Locks.Release(base.TargetEnv)

	if err := g.ensureRepos(ctx, base); err != nil {
		return nil, err
	}
	res := &BatchResult{AllOK: true}
	path := g.RepoPath(base.TargetEnv)
	for _, row := range rows {
		spec := base
		spec.ModuleName = row.ModuleName
		spec.Namespace = row.Namespace
		spec.ValuesYAML = row.ValuesYAML
		rr := BatchRowResult{ModuleName: row.ModuleName}
		if err := g.writeModuleFiles(spec); err != nil {
			rr.Error = err.Error()
			res.AllOK = false
			res.Rows = append(res.Rows, rr)
			continue
		}
		ok, skipped, out := helmTemplateCheck(ctx,
			filepath.Join(path, base.TargetChartBasePath, row.ModuleName), row.ModuleName, row.Namespace)
		rr.HelmOK, rr.HelmSkipped = ok, skipped
		if !ok && !skipped {
			rr.Error = out
			res.AllOK = false
		}
		res.Rows = append(res.Rows, rr)
	}
	// 统计改动文件数
	_, _ = exec.CommandContext(ctx, "git", "-C", path, "add", "-A").CombinedOutput()
	out, _ := exec.CommandContext(ctx, "git", "-C", path, "diff", "--cached", "--name-only").CombinedOutput()
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(l) != "" {
			res.ChangedFiles++
		}
	}

	if !commit || !res.AllOK {
		_ = g.discardWorktree(ctx, base.TargetEnv, base.TargetBranch)
		return res, nil
	}
	sha, err := g.CommitAll(ctx, base.TargetEnv, operator, msg)
	if err != nil {
		_ = g.discardWorktree(ctx, base.TargetEnv, base.TargetBranch)
		return nil, err
	}
	if sha != "" {
		if err := g.safePush(ctx, base.TargetEnv, base.TargetBranch, 3); err != nil {
			return nil, err
		}
		res.CommitSHA = sha
		res.CommitURL = CommitURL(base.TargetRepoURL, sha)
	}
	return res, nil
}

// PreviewBatch 批量预览（逐行 helm 校验，不提交）。
func (g *GitService) PreviewBatch(ctx context.Context, base ModuleSpec, rows []BatchRow) (*BatchResult, error) {
	return g.runBatch(ctx, base, rows, false, "", "")
}

// SubmitBatch 批量提交（全绿才一次性 commit + push）。
func (g *GitService) SubmitBatch(ctx context.Context, base ModuleSpec, rows []BatchRow, operator, msg string) (*BatchResult, error) {
	return g.runBatch(ctx, base, rows, true, operator, msg)
}
