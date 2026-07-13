package services

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"opsplatform-deploy-backend/models"
)

// restartTsRe 匹配 chart 模板里 "manual restart at YYYY-MM-DD HH:MM:SS"
// 跟生产 Jenkins 流水线 sed 行为一致（同一行同一格式）
var restartTsRe = regexp.MustCompile(`manual restart at \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

// replaceRestartTimestamp 在 templates/deployment.yaml 内容里把
// "manual restart at <旧 ts>" 替换成 "manual restart at <newTs>"
// 找不到该锚点行返回 (content, false)，调用方应把这个模块标为"模板配置缺少重启标记"。
func replaceRestartTimestamp(content []byte, newTs string) ([]byte, bool) {
	if !restartTsRe.Match(content) {
		return content, false
	}
	return restartTsRe.ReplaceAll(content, []byte("manual restart at "+newTs)), true
}

// dynamicStabilityTicks 根据 poll interval 算稳定窗口需要的连续 OK 次数
// 始终维持约 30s 的稳定观察窗口，下限 3 次（防止极小 interval 配置）
//
//	interval=5  → 6 次  (30s 窗口，跟历史默认一致)
//	interval=10 → 3 次  (30s 窗口)
//	interval=20 → 3 次  (60s 窗口，下限保护)
func dynamicStabilityTicks(intervalSec int) int {
	if intervalSec <= 0 {
		return 6
	}
	t := 30 / intervalSec
	if 30%intervalSec != 0 {
		t++
	}
	if t < 3 {
		t = 3
	}
	return t
}

// 副本数感知 / 动态超时
//
//	业务事实：5 副本 maxSurge=1 maxUnavailable=0 滚动更新需要 ~150s
//	30 副本同模式需要 ~900s。固定 180s 超时对多副本服务必失败。
//	解决：clone 后顺手读 values.yaml 拿副本数，按副本数计算每个 app 的超时。
// 这些常量只决定 PollUntilStable 的"初始软 deadline"——v131 起进度追踪续命 +
// 5 分钟停滞快败 + 4 小时硬超时是真正的失败判定逻辑。所以这里给宽一点没坏处：
// 推进顺利的话 deadline 会一路被续命到 hardDeadline；卡住的话 stagnant timeout 5 分钟会先快败。
const (
	restartPerPodSec     = 60       // 重启每 pod 估时（含 readinessProbe initialDelay 30s + 容器启动 + 探针缓冲）
	updateImagePerPodSec = 90       // 发布每 pod 估时
	timeoutBufferSec     = 300      // 超时硬缓冲（git 拉推 + ArgoCD reconcile 滞后 + 镜像 pull 多副本错峰）
	timeoutCapSec        = 90 * 60  // 初始 deadline 上限 90 分钟（兜底由 PollUntilStable 内部的 4h hardDeadline 接管）
)

// readEffectiveReplicas 从 values.yaml 内容推断"实际副本数上限"
//
//	优先 autoscaling.maxReplicas，回退 replicaCount，都没有返回 1。
func readEffectiveReplicas(valuesContent []byte) int {
	const noOp = 1
	if len(valuesContent) == 0 {
		return noOp
	}
	maxRep := extractIntField(valuesContent, "maxReplicas")
	if maxRep > 0 {
		return maxRep
	}
	rep := extractIntField(valuesContent, "replicaCount")
	if rep > 0 {
		return rep
	}
	return noOp
}

// extractIntField 从 yaml 文本里抓 "<key>: <int>" 格式的值，找不到/非数字返回 0
// 不区分缩进层级，只匹配第一个出现的；够用因为 maxReplicas/replicaCount 在 values.yaml 唯一。
func extractIntField(content []byte, key string) int {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*"?(\d+)"?\s*(?:#.*)?$`)
	m := re.FindSubmatch(content)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

// computeAppTimeout 根据副本数 + per-pod 估值 + 配置下限算单个 app 的 timeout
//
//	max(配置 timeout, replicas * per_pod + buffer)，封顶 30 分钟
func computeAppTimeout(replicas, perPodSec, configSec int) int {
	if replicas < 1 {
		replicas = 1
	}
	dynamic := replicas*perPodSec + timeoutBufferSec
	t := configSec
	if dynamic > t {
		t = dynamic
	}
	if t > timeoutCapSec {
		t = timeoutCapSec
	}
	return t
}

type DeployService struct {
	Git *GitService
}

func NewDeployService(git *GitService) *DeployService {
	return &DeployService{Git: git}
}

// Module 简化视图（handler 把 models.Module 映射过来）
type Module struct {
	Name            string
	CurrentTag      string
	ChartRelPath    string // e.g. charts/g32-uat/atmosphere-frontend/values.yaml
	ArgocdApp       string
	Namespace       string // 该模块实际部署的 K8s namespace（多 ns 项目必填）
	ImageRepository string // e.g. harbor.slileisure.com/g32/user-client-backend (precheck 用)
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
	// OnCommitted 可选：git push 成功那一刻立即回调（sha, url）。
	// 让上层第一时间把 git_commit 落库——这样发布中途重启时，DB 里有没有 git_commit
	// 就是"push 之前 / 之后被中断"的判定信号：空=没推(可自动重跑)，有=已推(查 ArgoCD 对账)。
	OnCommitted func(sha, url string)
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
	ids := make([]string, 0, len(in.Pending))
	for k := range in.Pending {
		ids = append(ids, k)
	}
	st := NewStageTimer("deploy", strings.Join(ids, ","), in.ProjectEnvName, in.Operator)
	defer st.Done()

	// git clone+编辑+push 的时间预算（poll 不占这个 ctx）
	gctx, cancel := GitCtx(ctx, 300)
	defer cancel()

	// 并发闸：防 100 并发起太多 git/helm 子进程 OOM
	_, releaseGate, gerr := AcquireHeavy(gctx)
	if gerr != nil {
		res.Err = fmt.Errorf("gate: %w", gerr)
		res.Status = models.StatusFailed
		return res
	}
	gateReleased := false
	releaseGateOnce := func() {
		if !gateReleased {
			gateReleased = true
			releaseGate()
		}
	}
	defer releaseGateOnce()
	st.Mark("gate_wait")

	// 独立浅克隆（隔离工作区）——同 env 并发互不干扰
	c, err := d.Git.AcquireCheckout(gctx, in.ProjectEnvName, in.GitRepo, in.GitBranch)
	if err != nil {
		res.Err = fmt.Errorf("git clone: %w", err)
		res.Status = models.StatusFailed
		return res
	}
	defer c.Release()
	st.Mark("git_clone")

	// 语义改动：读每个模块 values → 改 tag → 写回。可被 push 冲突重放（幂等：每次基于当前工作区重算）
	var changes []models.Change
	var notFound, skipped []string
	edit := func() (bool, error) {
		changes = changes[:0]
		notFound = nil
		skipped = nil
		for name, newTag := range in.Pending {
			m, ok := in.Modules[name]
			if !ok {
				notFound = append(notFound, name)
				continue
			}
			raw, rerr := c.ReadFile(m.ChartRelPath)
			if rerr != nil {
				notFound = append(notFound, name)
				continue
			}
			newContent, ch, eerr := UpdateImageTag(raw, newTag)
			if eerr != nil {
				return false, fmt.Errorf("yaml edit %s: %w", name, eerr)
			}
			if !ch {
				skipped = append(skipped, name)
				continue
			}
			if werr := c.WriteFile(m.ChartRelPath, newContent); werr != nil {
				return false, fmt.Errorf("write %s: %w", name, werr)
			}
			changes = append(changes, models.Change{Module: name, FromTag: m.CurrentTag, ToTag: newTag})
		}
		return len(changes) > 0, nil
	}

	msg := fmt.Sprintf("[deploy-center] update image: %s by %s\n\nvia deploy-center",
		strings.Join(ids, ","), in.Operator)
	sha, changed, cerr := c.CommitAndPush(gctx, in.Operator, msg, in.GitRetry, edit, st)
	res.Changes = changes
	res.NotFound = notFound
	res.Skipped = skipped
	if cerr != nil {
		res.Err = cerr
		res.Status = models.StatusFailed
		return res
	}
	if !changed {
		res.Status = models.StatusNoChange
		return res
	}
	res.GitCommit = sha
	res.GitCommitURL = CommitURL(in.GitRepo, sha)
	// push 成功 → 立即把 git_commit 落库(before/after-push 判定信号)
	if in.OnCommitted != nil {
		in.OnCommitted(res.GitCommit, res.GitCommitURL)
	}

	interval := in.PollIntervalSec
	if interval <= 0 {
		interval = 5
	}
	configTimeout := in.PollTimeoutSec
	if configTimeout <= 0 {
		configTimeout = 180
	}
	stableTicks := dynamicStabilityTicks(interval)
	limit := in.ConcurrentLimit
	if limit <= 0 {
		limit = 10
	}
	peNameLower := strings.ToLower(in.ProjectEnvName)
	type syncJob struct {
		module     string
		app        string
		timeoutSec int
	}
	jobs := make([]syncJob, 0, len(res.Changes))
	for _, ch := range res.Changes {
		var replicas int
		var appName string
		if m, ok := in.Modules[ch.Module]; ok {
			if m.ChartRelPath != "" {
				// 在释放工作区前，从隔离克隆里读副本数算超时
				if vb, _ := c.ReadFile(m.ChartRelPath); len(vb) > 0 {
					replicas = readEffectiveReplicas(vb)
				}
			}
			if m.ArgocdApp != "" {
				appName = m.ArgocdApp
			}
		}
		if appName == "" {
			appName = strings.ToLower(ch.Module) + "-" + peNameLower
		}
		if replicas <= 0 {
			replicas = 1
		}
		jobs = append(jobs, syncJob{
			module:     ch.Module,
			app:        appName,
			timeoutSec: computeAppTimeout(replicas, updateImagePerPodSec, configTimeout),
		})
	}

	// poll 不占 git/helm 名额，也不再持有任何 env 锁 → 释放工作区 + 并发闸，让别人接着上
	releaseGateOnce()
	c.Release()
	st.Mark("git_done")

	if !in.AutoSync {
		res.Status = models.StatusSuccess
		return res
	}

	res.ArgocdResults = RunBoundedConcurrent(ctx, jobs, limit,
		func(c context.Context, j syncJob, publish func(models.ArgocdAppResult)) models.ArgocdAppResult {
			// 初始状态先让前端看到这一行
			publish(models.ArgocdAppResult{
				App: j.app, SyncStatus: "Syncing", Health: "Progressing",
				DurationSec: 0, Msg: "正在触发同步",
				LastPolledAt: time.Now(),
			})
			// 记录 Sync API 调用前的时刻，用于 PollUntilStable 验证「这次 sync 是我们触发的」
			syncStartedAt := time.Now()
			if err := in.ArgocdClient.Sync(c, j.app); err != nil {
				return models.ArgocdAppResult{
					App: j.app, SyncStatus: "Failed",
					Msg:          "失败 · 服务同步触发失败 · " + sanitizeMsg(err.Error()),
					LastPolledAt: time.Now(),
				}
			}
			// 短暂 idle 减少首次 poll 的空查；根本性的"等我们触发的 operation 真的完成"
			// 由 PollUntilStable 内部根据 OperationStartedAt > syncStartedAt 判定。
			select {
			case <-time.After(2 * time.Second):
			case <-c.Done():
				return models.ArgocdAppResult{
					App: j.app, SyncStatus: "canceled", Msg: "已由用户取消等待",
					LastPolledAt: time.Now(),
				}
			}
			// 副本数动态超时 + 稳定窗口动态 ticks（始终 ~30s 窗口）
			return *PollUntilStable(c, in.ArgocdClient, j.app, interval, j.timeoutSec, syncStartedAt, stableTicks,
				func(tick *models.ArgocdAppResult) { publish(*tick) })
		},
		func(_ int, snapshot []models.ArgocdAppResult) {
			if in.OnProgress != nil {
				in.OnProgress(snapshot)
			}
		},
	)

	// 数 ok/fail/canceled —— 单模块全 fail 时必须落到 Failed 而非 Partial
	okCount, failCount, canceledCount := 0, 0, 0
	for _, ar := range res.ArgocdResults {
		switch {
		case ar.SyncStatus == "canceled":
			canceledCount++
		case ar.SyncStatus == "Synced" && ar.Health == "Healthy":
			okCount++
		default:
			failCount++
		}
	}
	switch {
	case canceledCount > 0:
		// 任一 app 被取消 → 整次记 canceled（保留与 Restart 一致的语义）
		res.Status = models.StatusCanceled
	case failCount == 0 && okCount > 0:
		res.Status = models.StatusSuccess
	case okCount == 0:
		res.Status = models.StatusFailed
	default:
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
	// 走 git 路径触发重启需要的字段（与 UpdateImageInput 对齐）
	GitRepo         string
	GitBranch       string
	GitRetry        int    // push 冲突自动 pull --rebase 重试次数
	Operator        string // commit author 后缀
	// v130 起：跟 update_image 对齐。auto_sync=true 主动调 ArgoCD sync；false 只 git push 等手动同步。
	// PROD 默认关闭（除非有 prod_auto_sync button 权限）：重启 = 提交 git → 运维到 ArgoCD UI 点 sync。
	AutoSync        bool
	ArgocdClient    *ArgocdClient
	PollIntervalSec int
	PollTimeoutSec  int
	ConcurrentLimit int // 并发 poll 池大小，<=0 默认 10
	// OnProgress 可选：每个 app 轮询完成时回调，传入当前快照供渐进式写 DB。
	// 调用方法内部已上锁，回调函数可直接读取 snapshot；但请勿持有引用到锁外。
	OnProgress func(snapshot []models.ArgocdAppResult)
	// OnCommitted 可选：git push 成功那一刻立即回调（sha, url），供上层第一时间落 git_commit。
	OnCommitted func(sha, url string)
}

type RestartResult struct {
	ArgocdResults []models.ArgocdAppResult
	GitCommit     string // 触发重启的 commit sha（短）
	GitCommitURL  string
	Status        string
	Err           error
}

// Restart 走 git 触发重启：跟生产 Jenkins 同款做法
//  1. clone/pull 该 env 的 chart repo
//  2. 每个模块 templates/deployment.yaml 用 regex 替换 "manual restart at <ts>"
//     找不到锚点行的模块标记为失败（chart 缺 restart hook）
//  3. 单个 commit 包所有模块 + push origin <branch>
//  4. 对每个成功改文件的模块调 ArgoCD Sync API + PollUntilStable（6 ticks 稳定）
//  5. 聚合结果
//
// 之所以不用 ArgoCD resource action restart：
//   ArgoCD restart 给 Deployment 加 kubectl.kubernetes.io/restartedAt annotation，
//   生产环境 ArgoCD 会把它当成 drift → application 永远 OutOfSync → poll 等不到
//   Synced，180s 后误报失败（pod 实际已经重启好了）。改走 git 让 ArgoCD 正常
//   sync 路径，状态机干净，跟 update_image 一致。
func (d *DeployService) Restart(ctx context.Context, in RestartInput) *RestartResult {
	res := &RestartResult{}

	st := NewStageTimer("restart", strings.Join(in.ModuleNames, ","), in.ProjectEnvName, in.Operator)
	defer st.Done()

	gctx, cancel := GitCtx(ctx, 300)
	defer cancel()

	failAllModules := func(reason string) {
		for _, name := range in.ModuleNames {
			app := name
			if m, ok := in.Modules[name]; ok && m.ArgocdApp != "" {
				app = m.ArgocdApp
			}
			res.ArgocdResults = append(res.ArgocdResults, models.ArgocdAppResult{
				App: app, SyncStatus: "Failed", Msg: reason, LastPolledAt: time.Now(),
			})
		}
	}

	// 并发闸
	_, releaseGate, gerr := AcquireHeavy(gctx)
	if gerr != nil {
		res.Err = fmt.Errorf("gate: %w", gerr)
		res.Status = models.StatusFailed
		failAllModules("失败 · 系统繁忙（并发闸取消）")
		return res
	}
	gateReleased := false
	releaseGateOnce := func() {
		if !gateReleased {
			gateReleased = true
			releaseGate()
		}
	}
	defer releaseGateOnce()
	st.Mark("gate_wait")

	// 独立浅克隆隔离工作区
	c, err := d.Git.AcquireCheckout(gctx, in.ProjectEnvName, in.GitRepo, in.GitBranch)
	if err != nil {
		res.Err = fmt.Errorf("git clone: %w", err)
		res.Status = models.StatusFailed
		failAllModules("失败 · 拉取仓库出错 · " + sanitizeMsg(err.Error()))
		return res
	}
	defer c.Release()
	st.Mark("git_clone")

	// Step 2: 每个模块改 templates/deployment.yaml 的 manual restart at <ts>
	type triggered struct {
		app        string
		mod        string
		ns         string
		timeoutSec int // 按副本数动态算
	}
	var toPoll []triggered
	var preFail []models.ArgocdAppResult
	var changedModules []string
	newTs := time.Now().Format("2006-01-02 15:04:05")
	configTimeout := in.PollTimeoutSec
	if configTimeout <= 0 {
		configTimeout = 180
	}
	// 语义改动（可被 push 冲突重放，幂等：每次基于当前工作区重算）
	edit := func() (bool, error) {
		toPoll = nil
		preFail = nil
		changedModules = nil
		for _, name := range in.ModuleNames {
			m, ok := in.Modules[name]
			if !ok {
				preFail = append(preFail, models.ArgocdAppResult{
					App: name, SyncStatus: "Failed", Msg: "失败 · 模块未在系统中登记，请先扫描",
					LastPolledAt: time.Now(),
				})
				continue
			}
			deployRel := strings.Replace(m.ChartRelPath, "values.yaml", "templates/deployment.yaml", 1)
			raw, rerr := c.ReadFile(deployRel)
			if rerr != nil {
				preFail = append(preFail, models.ArgocdAppResult{
					App: m.ArgocdApp, SyncStatus: "Failed",
					Msg:          "失败 · 读取部署配置失败（模板目录可能异常）· " + sanitizeMsg(rerr.Error()),
					LastPolledAt: time.Now(),
				})
				continue
			}
			newContent, replaced := replaceRestartTimestamp(raw, newTs)
			if !replaced {
				preFail = append(preFail, models.ArgocdAppResult{
					App: m.ArgocdApp, SyncStatus: "Failed",
					Msg:          "失败 · 模板配置缺少重启标记，无法触发重启（请联系运维）",
					LastPolledAt: time.Now(),
				})
				continue
			}
			if werr := c.WriteFile(deployRel, newContent); werr != nil {
				preFail = append(preFail, models.ArgocdAppResult{
					App: m.ArgocdApp, SyncStatus: "Failed",
					Msg:          "失败 · 写入配置失败 · " + sanitizeMsg(werr.Error()),
					LastPolledAt: time.Now(),
				})
				continue
			}
			changedModules = append(changedModules, name)
			ns := m.Namespace
			if ns == "" {
				ns = in.Namespace
			}
			valuesRaw, _ := c.ReadFile(m.ChartRelPath)
			replicas := readEffectiveReplicas(valuesRaw)
			appTimeout := computeAppTimeout(replicas, restartPerPodSec, configTimeout)
			toPoll = append(toPoll, triggered{app: m.ArgocdApp, mod: name, ns: ns, timeoutSec: appTimeout})
		}
		return len(changedModules) > 0, nil
	}

	// Step 3: commit + push（隔离工作区 + push 冲突重放）
	commitMsg := fmt.Sprintf("[deploy-center] restart modules: %s by %s\n\nrestartedAt: %s\nvia deploy-center",
		strings.Join(in.ModuleNames, ","), in.Operator, newTs)
	sha, changed, cerr := c.CommitAndPush(gctx, in.Operator, commitMsg, in.GitRetry, edit, st)
	if cerr != nil {
		res.Err = cerr
		res.Status = models.StatusFailed
		res.ArgocdResults = preFail
		return res
	}
	// changed==false 可能是 1 秒内连点两次重启时间戳相同（没 diff）；toPoll 非空则仍继续 poll
	if changed {
		res.GitCommit = sha
		res.GitCommitURL = CommitURL(in.GitRepo, sha)
		if in.OnCommitted != nil {
			in.OnCommitted(res.GitCommit, res.GitCommitURL)
		}
	}

	// Step 4: 全部 preFail 的兜底
	if len(toPoll) == 0 {
		res.ArgocdResults = preFail
		res.Status = models.StatusFailed
		return res
	}

	// poll 不占 git/helm 名额，也不再持锁 → 释放工作区 + 并发闸
	releaseGateOnce()
	c.Release()
	st.Mark("git_done")

	// v130: auto_sync 关 → git push 完毕直接返回 success，不主动调 ArgoCD sync。
	// 跟 UpdateImage 同款语义（PROD 关 auto_sync 时人工到 ArgoCD UI 点 sync）。
	// 已 preFail 的模块继续标记失败，其余的标 success "已提交，待手动同步"。
	if !in.AutoSync {
		now := time.Now()
		for _, t := range toPoll {
			preFail = append(preFail, models.ArgocdAppResult{
				App:          t.app,
				SyncStatus:   "Synced",
				Health:       "Pending",
				Msg:          "已提交 git，等待手动在 ArgoCD 同步",
				LastPolledAt: now,
			})
		}
		res.ArgocdResults = preFail
		res.Status = models.StatusSuccess
		return res
	}

	// Step 5: 主动触发 ArgoCD Sync 并 poll 到 Synced+Healthy
	interval := in.PollIntervalSec
	if interval <= 0 {
		interval = 5
	}
	stableTicks := dynamicStabilityTicks(interval)
	limit := in.ConcurrentLimit
	if limit <= 0 {
		limit = 10
	}

	pollResults := RunBoundedConcurrent(ctx, toPoll, limit,
		func(c context.Context, t triggered, publish func(models.ArgocdAppResult)) models.ArgocdAppResult {
			publish(models.ArgocdAppResult{
				App: t.app, SyncStatus: "Syncing", Health: "Progressing",
				DurationSec: 0, Msg: "正在触发同步",
				LastPolledAt: time.Now(),
			})
			// 记录 Sync 调用前的 wall-clock，PollUntilStable 用它做 ourOpStarted 判定，
			// 避免命中上一次 sync 的旧 Synced+Healthy
			syncStartedAt := time.Now()
			if err := in.ArgocdClient.Sync(c, t.app); err != nil {
				return models.ArgocdAppResult{
					App: t.app, SyncStatus: "Failed",
					Msg:          "失败 · 服务同步触发失败 · " + sanitizeMsg(err.Error()),
					LastPolledAt: time.Now(),
				}
			}
			select {
			case <-time.After(2 * time.Second):
			case <-c.Done():
				return models.ArgocdAppResult{
					App: t.app, SyncStatus: "canceled", Msg: "已由用户取消等待",
					LastPolledAt: time.Now(),
				}
			}
			// 副本数动态超时 + 稳定窗口动态 ticks（始终 ~30s 窗口）
			return *PollUntilStable(c, in.ArgocdClient, t.app, interval, t.timeoutSec, syncStartedAt, stableTicks,
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

	// Step 6: 聚合
	res.ArgocdResults = append(res.ArgocdResults, preFail...)
	res.ArgocdResults = append(res.ArgocdResults, pollResults...)

	okCount, failCount, canceledCount := 0, 0, 0
	for _, ar := range res.ArgocdResults {
		switch {
		case ar.SyncStatus == "canceled":
			canceledCount++
		case ar.SyncStatus == "Synced" && ar.Health == "Healthy":
			okCount++
		default:
			failCount++
		}
	}
	switch {
	case canceledCount > 0:
		// 任一 app 被取消 → 整次记 canceled（用户取消等待的语义）
		res.Status = models.StatusCanceled
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
