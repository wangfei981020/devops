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

	// 用户编辑过的 configmap（相对模块目录的路径，如 "templates/configmap.yaml" → 新内容）；
	// 写文件时用这里的内容覆盖，其余 templates 文件仍原样照抄。
	ConfigMaps map[string]string
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

// PreviewNewModule 独立浅克隆目标 → 生成文件 → helm 校验 → 算 diff → 丢弃（不提交）。
func (g *GitService) PreviewNewModule(ctx context.Context, spec ModuleSpec) (*PreviewResult, error) {
	_, releaseGate, gerr := AcquireHeavy(ctx)
	if gerr != nil {
		return nil, fmt.Errorf("gate: %w", gerr)
	}
	defer releaseGate()
	st := NewStageTimer("orch-preview", spec.ModuleName, spec.TargetEnv, "")
	defer st.Done()

	tc, srcDir, cleanupSrc, err := g.openModuleCheckout(ctx, spec, st)
	if err != nil {
		return nil, err
	}
	defer tc.Release()
	defer cleanupSrc()

	if err := writeModuleFilesAt(tc.Dir, srcDir, spec); err != nil {
		return nil, err
	}
	st.Mark("edit")
	res, err := inspectStagedAt(ctx, tc.Dir, spec)
	st.Mark("helm_template")
	if err != nil {
		return nil, err
	}
	return res, nil
}

// SubmitNewModule 独立浅克隆目标 → 生成文件 → helm 校验(不过不提交) → commit → push(冲突重放)。
func (g *GitService) SubmitNewModule(ctx context.Context, spec ModuleSpec, operator, commitMsg string) (*SubmitResult, error) {
	_, releaseGate, gerr := AcquireHeavy(ctx)
	if gerr != nil {
		return nil, fmt.Errorf("gate: %w", gerr)
	}
	defer releaseGate()
	st := NewStageTimer("orchestrate", spec.ModuleName, spec.TargetEnv, operator)
	defer st.Done()

	tc, srcDir, cleanupSrc, err := g.openModuleCheckout(ctx, spec, st)
	if err != nil {
		return nil, err
	}
	defer tc.Release()
	defer cleanupSrc()

	if err := writeModuleFilesAt(tc.Dir, srcDir, spec); err != nil {
		return nil, err
	}
	st.Mark("edit")
	preview, err := inspectStagedAt(ctx, tc.Dir, spec)
	st.Mark("helm_template")
	if err != nil {
		return nil, err
	}
	if !preview.HelmOK && !preview.HelmSkipped {
		return nil, fmt.Errorf("helm 校验未通过，已阻止提交：\n%s", preview.HelmOutput)
	}

	// push 冲突时：reset 到远端最新 + 重新生成本模块文件（AppendAppsEntry 查重幂等，不覆盖别人）
	reapply := func() error { return writeModuleFilesAt(tc.Dir, srcDir, spec) }
	sha, err := tc.CommitPushReapply(ctx, operator, commitMsg, 5, reapply, st)
	if err != nil {
		return nil, err
	}
	return &SubmitResult{
		CommitSHA:     sha,
		CommitURL:     CommitURL(spec.TargetRepoURL, sha),
		PreviewResult: *preview,
	}, nil
}

// openModuleCheckout 开目标仓库的隔离浅克隆，并解析样板目录路径。
//
//	样板与目标同仓库 → 直接从目标 checkout 里读；不同仓库 → 单独浅克隆样板（只读，用完删）。
//	返回：目标 checkout、样板服务目录绝对路径、样板清理函数。
func (g *GitService) openModuleCheckout(ctx context.Context, spec ModuleSpec, st *StageTimer) (tc *Checkout, srcDir string, cleanupSrc func(), err error) {
	tc, err = g.AcquireCheckout(ctx, spec.TargetEnv, spec.TargetRepoURL, spec.TargetBranch)
	if err != nil {
		return nil, "", func() {}, fmt.Errorf("克隆目标仓库失败: %w", err)
	}
	st.Mark("git_clone")
	cleanupSrc = func() {}
	if spec.SrcRepoURL == spec.TargetRepoURL && spec.SrcBranch == spec.TargetBranch {
		// 同仓库同分支：样板就在目标 checkout 里
		srcDir = filepath.Join(tc.Dir, spec.SrcChartBasePath, spec.SrcService)
	} else {
		sc, serr := g.AcquireCheckout(ctx, spec.SrcEnv, spec.SrcRepoURL, spec.SrcBranch)
		if serr != nil {
			tc.Release()
			return nil, "", func() {}, fmt.Errorf("克隆样板仓库失败: %w", serr)
		}
		srcDir = filepath.Join(sc.Dir, spec.SrcChartBasePath, spec.SrcService)
		cleanupSrc = sc.Release
		st.Mark("git_clone_src")
	}
	return tc, srcDir, cleanupSrc, nil
}

// writeModuleFilesAt 把一个模块的文件写进目标工作区（拷样板目录+改Chart名+写values+追加-apps）。
//
//	targetDir：目标仓库 checkout 根目录；srcServiceDir：样板服务目录绝对路径。
//	纯文件操作，可被 push 冲突重放（reset 后目标模块目录不存在 → 查重通过 → 幂等）。
func writeModuleFilesAt(targetDir, srcServiceDir string, spec ModuleSpec) error {
	if st, err := os.Stat(srcServiceDir); err != nil || !st.IsDir() {
		return fmt.Errorf("样板服务目录不存在: %s/%s", spec.SrcChartBasePath, spec.SrcService)
	}
	targetRel := filepath.Join(spec.TargetChartBasePath, spec.ModuleName)

	// 目标模块目录已存在 → 拦下，不覆盖（同名模块被他人抢先添加时也走这里）
	if _, err := os.Stat(filepath.Join(targetDir, targetRel)); err == nil {
		return fmt.Errorf("目标已存在模块目录 %s，请勿重复新增", targetRel)
	}

	err := filepath.Walk(srcServiceDir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcServiceDir, p)
		if err != nil {
			return err
		}
		var content []byte
		switch {
		case rel == "values.yaml":
			content = spec.ValuesYAML
		case rel == "Chart.yaml":
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			content, err = SetChartName(raw, spec.ModuleName)
			if err != nil {
				return err
			}
		case spec.ConfigMaps[rel] != "":
			content = []byte(spec.ConfigMaps[rel])
		default:
			content, err = os.ReadFile(p)
			if err != nil {
				return err
			}
		}
		full := filepath.Join(targetDir, targetRel, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		return os.WriteFile(full, content, 0o644)
	})
	if err != nil {
		return fmt.Errorf("拷贝样板目录失败: %w", err)
	}

	// 追加 -apps 条目
	appsRel := spec.TargetChartBasePath + "-apps/values.yaml"
	appsAbs := filepath.Join(targetDir, appsRel)
	appsContent, err := os.ReadFile(appsAbs)
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
	return os.WriteFile(appsAbs, newApps, 0o644)
}

// inspectStagedAt 计算改动文件清单 + 跑 helm 校验（新模块 chart 目录），在指定工作区。
func inspectStagedAt(ctx context.Context, targetDir string, spec ModuleSpec) (*PreviewResult, error) {
	if out, err := exec.CommandContext(ctx, "git", "-C", targetDir, "add", "-A").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git add: %w\n%s", err, out)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", targetDir, "diff", "--cached", "--name-only").CombinedOutput()
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
		filepath.Join(targetDir, spec.TargetChartBasePath, spec.ModuleName),
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
	// 物理排除 templates/tests/ 再渲染：--skip-tests 只在渲染「之后」过滤输出，test 钩子仍会被渲染，
	// 而 test 钩子常引用「从未定义、部署时也不注入」的 .Values.global.*（ArgoCD 不部署 test 钩子，
	// 所以真实部署不炸）。校验排除它，跟 ArgoCD 一致。test 文件照常提交进 git，只是不参与校验。
	renderDir := chartDir
	if tmp, err := copyChartWithoutTests(chartDir); err == nil {
		renderDir = tmp
		defer os.RemoveAll(tmp)
	}
	args := []string{"template", releaseName, renderDir, "--skip-tests"}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}
	out, err := exec.CommandContext(ctx, "helm", args...).CombinedOutput()
	if err != nil {
		return false, false, string(out)
	}
	return true, false, ""
}

// copyChartWithoutTests 把 chart 目录拷到临时目录、去掉 templates/tests/，返回临时目录路径（调用方负责删）。
func copyChartWithoutTests(src string) (string, error) {
	tmp, err := os.MkdirTemp("", "orch-chart-")
	if err != nil {
		return "", err
	}
	testsRel := filepath.Join("templates", "tests")
	err = filepath.Walk(src, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// 跳过 templates/tests 及其下所有内容
		if rel == testsRel || strings.HasPrefix(rel, testsRel+string(os.PathSeparator)) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(tmp, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}


// ============ 批量新增 ============

// BatchRow 批量新增的一行（一个模块）。ValuesYAML 由 handler 用模板派生预填。
type BatchRow struct {
	ModuleName string
	Namespace  string
	ValuesYAML []byte
	ConfigMaps map[string]string // 相对模块目录路径 → 内容（可空）
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

// runBatch 独立浅克隆一次 → 循环生成每行 + 逐行 helm 校验 → 全绿才一次性提交(冲突重放)，否则丢弃。
func (g *GitService) runBatch(ctx context.Context, base ModuleSpec, rows []BatchRow, commit bool, operator, msg string) (*BatchResult, error) {
	_, releaseGate, gerr := AcquireHeavy(ctx)
	if gerr != nil {
		return nil, fmt.Errorf("gate: %w", gerr)
	}
	defer releaseGate()
	st := NewStageTimer("batch", base.TargetEnv, base.TargetEnv, operator)
	defer st.Done()

	tc, srcDir, cleanupSrc, err := g.openModuleCheckout(ctx, base, st)
	if err != nil {
		return nil, err
	}
	defer tc.Release()
	defer cleanupSrc()

	// writeAll：把所有行写进工作区（供首次生成 + 冲突重放复用）。返回逐行错误。
	writeAll := func() []BatchRowResult {
		rowResults := make([]BatchRowResult, 0, len(rows))
		for _, row := range rows {
			spec := base
			spec.ModuleName = row.ModuleName
			spec.Namespace = row.Namespace
			spec.ValuesYAML = row.ValuesYAML
			spec.ConfigMaps = row.ConfigMaps
			rr := BatchRowResult{ModuleName: row.ModuleName}
			if werr := writeModuleFilesAt(tc.Dir, srcDir, spec); werr != nil {
				rr.Error = werr.Error()
			}
			rowResults = append(rowResults, rr)
		}
		return rowResults
	}

	res := &BatchResult{AllOK: true}
	rowResults := writeAll()
	st.Mark("edit")
	// 逐行 helm 校验
	for i := range rowResults {
		if rowResults[i].Error != "" {
			res.AllOK = false
			continue
		}
		ok, skipped, out := helmTemplateCheck(ctx,
			filepath.Join(tc.Dir, base.TargetChartBasePath, rows[i].ModuleName), rows[i].ModuleName, rows[i].Namespace)
		rowResults[i].HelmOK, rowResults[i].HelmSkipped = ok, skipped
		if !ok && !skipped {
			rowResults[i].Error = out
			res.AllOK = false
		}
	}
	res.Rows = rowResults
	st.Mark("helm_template")

	// 统计改动文件数
	_, _ = exec.CommandContext(ctx, "git", "-C", tc.Dir, "add", "-A").CombinedOutput()
	out, _ := exec.CommandContext(ctx, "git", "-C", tc.Dir, "diff", "--cached", "--name-only").CombinedOutput()
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(l) != "" {
			res.ChangedFiles++
		}
	}

	if !commit || !res.AllOK {
		return res, nil // 预览 / 有失败 → 不提交，checkout defer 释放即丢弃
	}
	// 冲突重放：reset 后重写所有行
	reapply := func() error {
		rr := writeAll()
		for _, r := range rr {
			if r.Error != "" {
				return fmt.Errorf("重放失败: %s", r.Error)
			}
		}
		return nil
	}
	sha, err := tc.CommitPushReapply(ctx, operator, msg, 5, reapply, st)
	if err != nil {
		return nil, err
	}
	res.CommitSHA = sha
	res.CommitURL = CommitURL(base.TargetRepoURL, sha)
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
