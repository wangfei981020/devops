package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// 新增模块真部署后，跟发布一样轮询新模块的 ArgoCD app 到 Synced+Healthy，结果写进 orchestration_task；
// 真部署有结果(成功/失败)时发 Lark(复用发布卡片，标题"新增模块")。预演/未开自动同步不发。
//
// 比普通发布多一层 app-of-apps：git 提交后，根 app 要先同步生成子 Application，再等它起来。

// newModDeploy 一个待轮询部署的新模块。
type newModDeploy struct {
	Module    string
	Namespace string
	Version   string   // image.tag，用于 Lark 卡片版本号
	Domains   []string // 访问域名（前端模块才有），Lark 卡片列出
	App       string   // ArgoCD app 名
}

// ---------- 单个 ----------

func deployAndPollNewModule(taskID, envID int64, operator, moduleName, namespace, version string, domains, tempAt []string, start time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	p, err := LoadProjectEnvDecrypted(envID)
	if err != nil {
		finishOrchTask(taskID, "success", "已提交（环境信息加载失败，无法自动跟踪部署状态，请到 ArgoCD 查看）", nil, dur(start))
		return
	}
	gs := getGitService()
	appName := strings.ToLower(moduleName) + "-" + gs.ResolveAppNameSuffix(p.Name, p.ChartBasePath)
	setOrchTaskApp(taskID, appName, namespace)

	client, skipNote := argocdClientForDeploy(p)
	if client == nil {
		finishOrchTask(taskID, "success", skipNote, nil, dur(start)) // 无 argocd/未开自动同步 → 已提交，不发 Lark
		return
	}
	m := newModDeploy{Module: moduleName, Namespace: namespace, Version: version, Domains: domains, App: appName}
	pollAndFinishModule(ctx, client, p, taskID, operator, m, tempAt, start, true)
}

// argocdClientForDeploy 返回可跟踪部署的 argocd client；不满足(无配置/未开自动同步)返回 nil + 说明。
func argocdClientForDeploy(p *models.ProjectEnv) (*services.ArgocdClient, string) {
	argoURL, argoToken, aerr := ResolveArgocdForEnv(p)
	if aerr != nil || argoURL == "" {
		return nil, "已提交（未配置 ArgoCD，无法自动跟踪部署状态）"
	}
	if p.AutoSync != 1 {
		return nil, "已提交，该环境未开自动同步——请到 ArgoCD 手动同步，或点「重试」触发部署"
	}
	return services.NewArgocdClient(argoURL, argoToken), ""
}

// pollAndFinishModule 触发+轮询单模块到稳定，写任务终态 + 发 Lark(真部署结果)。
func pollAndFinishModule(ctx context.Context, client *services.ArgocdClient, p *models.ProjectEnv, taskID int64, operator string, m newModDeploy, tempAt []string, start time.Time, waitAppear bool) {
	setOrchTaskArgocd(taskID, marshalJSON([]models.ArgocdAppResult{{
		App: m.App, SyncStatus: "Syncing", Health: "Progressing",
		Msg: "正在触发同步 / 等待 ArgoCD 生成 Application", LastPolledAt: time.Now(),
	}}))
	if waitAppear {
		syncRootApp(ctx, client, p.Name)
		bestEffortSyncZkv(ctx, client, p) // 先推 z-kv-secrets，让新专属 secret 尽快就绪
		if !waitAppAppear(ctx, client, m.App, 3*time.Minute) {
			msg := "失败 · 等待 ArgoCD 生成 Application 超时（请检查 app-of-apps 根应用是否已同步）"
			r := models.ArgocdAppResult{App: m.App, SyncStatus: "Failed", Msg: msg, DurationSec: dur(start), LastPolledAt: time.Now()}
			finishOrchTask(taskID, "failed", msg, marshalJSON([]models.ArgocdAppResult{r}), dur(start))
			log.Printf("⚠ [orch-fail] task=%d env=%s app=%s 等待 Application 超时", taskID, p.Name, m.App)
			sendOrchNotify(p, operator, tempAt, nil, []deployNotifyItem{{Module: m.Module, Namespace: m.Namespace, ToTag: m.Version, Domains: m.Domains, FailMsg: msg}})
			return
		}
	}
	r := pollAppToStable(ctx, client, m.App, func(t *models.ArgocdAppResult) {
		setOrchTaskArgocd(taskID, marshalJSON([]models.ArgocdAppResult{*t}))
	})
	ok := r != nil && strings.EqualFold(r.SyncStatus, "Synced") && strings.EqualFold(r.Health, "Healthy")
	status, note := "success", "服务已部署（Synced + Healthy）"
	if !ok {
		status = "failed"
		note = "服务部署未就绪"
		if r != nil && r.Msg != "" {
			note = "服务部署未就绪 · " + r.Msg
		}
		log.Printf("⚠ [orch-fail] task=%d env=%s app=%s 部署未就绪: %s", taskID, p.Name, m.App, note)
	}
	var results []models.ArgocdAppResult
	if r != nil {
		results = []models.ArgocdAppResult{*r}
	}
	finishOrchTask(taskID, status, note, marshalJSON(results), dur(start))
	if ok {
		sendOrchNotify(p, operator, tempAt, []deployNotifyItem{{Module: m.Module, Namespace: m.Namespace, ToTag: m.Version, Domains: m.Domains}}, nil)
	} else {
		fm := ""
		if r != nil {
			fm = r.Msg
		}
		sendOrchNotify(p, operator, tempAt, nil, []deployNotifyItem{{Module: m.Module, Namespace: m.Namespace, ToTag: m.Version, Domains: m.Domains, FailMsg: fm}})
	}
}

// ---------- 批量 ----------

func deployAndPollBatch(taskID, envID int64, operator string, mods []newModDeploy, tempAt []string, start time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	p, err := LoadProjectEnvDecrypted(envID)
	if err != nil {
		finishOrchTask(taskID, "success", "已提交（环境信息加载失败，无法自动跟踪部署状态）", nil, dur(start))
		return
	}
	gs := getGitService()
	suffix := gs.ResolveAppNameSuffix(p.Name, p.ChartBasePath)
	for i := range mods {
		mods[i].App = strings.ToLower(mods[i].Module) + "-" + suffix
	}
	client, skipNote := argocdClientForDeploy(p)
	if client == nil {
		finishOrchTask(taskID, "success", skipNote, nil, dur(start))
		return
	}
	// 根同步一次生成所有子 Application
	syncRootApp(ctx, client, p.Name)
	bestEffortSyncZkv(ctx, client, p) // 先推 z-kv-secrets，让新专属 secret 尽快就绪

	_, interval, timeoutMin := loadPollCfg()
	limit := 8
	// 每个模块并发：等 app 出现 → 轮询到稳定
	results := services.RunBoundedConcurrent(ctx, mods, limit,
		func(c context.Context, m newModDeploy, publish func(models.ArgocdAppResult)) models.ArgocdAppResult {
			publish(models.ArgocdAppResult{App: m.App, SyncStatus: "Syncing", Health: "Progressing", Msg: "等待 ArgoCD 生成 Application", LastPolledAt: time.Now()})
			if !waitAppAppear(c, client, m.App, 3*time.Minute) {
				return models.ArgocdAppResult{App: m.App, SyncStatus: "Failed", Msg: "失败 · 等待 Application 生成超时", LastPolledAt: time.Now()}
			}
			return *pollAppToStableCfg(c, client, m.App, interval, timeoutMin, func(t *models.ArgocdAppResult) { publish(*t) })
		},
		func(_ int, snapshot []models.ArgocdAppResult) { setOrchTaskArgocd(taskID, marshalJSON(snapshot)) },
	)

	// 聚合 + Lark（拆成功/失败两组）
	var successes, faileds []deployNotifyItem
	okN := 0
	for i, r := range results {
		item := deployNotifyItem{Module: mods[i].Module, Namespace: mods[i].Namespace, ToTag: mods[i].Version, Domains: mods[i].Domains}
		if strings.EqualFold(r.SyncStatus, "Synced") && strings.EqualFold(r.Health, "Healthy") {
			successes = append(successes, item)
			okN++
		} else {
			item.FailMsg = r.Msg
			faileds = append(faileds, item)
		}
	}
	status, note := "success", fmt.Sprintf("%d 个模块已部署", okN)
	if okN == 0 {
		status, note = "failed", "所有模块部署未就绪"
	} else if okN < len(mods) {
		status, note = "failed", fmt.Sprintf("%d/%d 个模块部署未就绪", len(mods)-okN, len(mods))
	}
	if status != "success" {
		log.Printf("⚠ [orch-fail] task=%d env=%s 批量部署: %s", taskID, p.Name, note)
	}
	finishOrchTask(taskID, status, note, marshalJSON(results), dur(start))
	sendOrchNotify(p, operator, tempAt, successes, faileds)
}

// ---------- 共用轮询原语 ----------

// bestEffortSyncZkv 后端新增了专属 secret 时，先推一把 z-kv-secrets 应用，让新 Secret 尽快就绪，
// 再等服务 app（服务 pod 引用 secret，secret 先到能少一轮 CreateContainerConfigError）。
// 名字约定 z-kv-secrets-<suffix>；跨环境共用/名字不符时 Sync 失败无害（B+C 轮询对 secret 短暂缺失也能容忍）。
func bestEffortSyncZkv(ctx context.Context, client *services.ArgocdClient, p *models.ProjectEnv) {
	suffix := getGitService().ResolveAppNameSuffix(p.Name, p.ChartBasePath)
	zkvApp := "z-kv-secrets-" + suffix
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	_ = client.Sync(c, zkvApp)
	cancel()
	log.Printf("[orch] best-effort synced z-kv-secrets app=%s (env=%s)", zkvApp, p.Name)
}

func syncRootApp(ctx context.Context, client *services.ArgocdClient, envName string) {
	rootApp := strings.ToLower(envName) + "-apps" // app-of-apps 根 app 名约定 <env>-apps
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	_ = client.Sync(c, rootApp) // best-effort，失败(名字不对/已自动同步)不影响
	cancel()
}

func pollAppToStable(ctx context.Context, client *services.ArgocdClient, appName string, onTick func(*models.ArgocdAppResult)) *models.ArgocdAppResult {
	_, interval, timeoutMin := loadPollCfg()
	return pollAppToStableCfg(ctx, client, appName, interval, timeoutMin, onTick)
}

func pollAppToStableCfg(ctx context.Context, client *services.ArgocdClient, appName string, interval, timeoutMin int, onTick func(*models.ArgocdAppResult)) *models.ArgocdAppResult {
	if interval <= 0 {
		interval = 10
	}
	if timeoutMin <= 0 {
		timeoutMin = 3
	}
	ticks := 30 / interval
	if ticks < 1 {
		ticks = 1
	}
	syncStartedAt := time.Now()
	_ = client.Sync(ctx, appName)
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
	}
	return services.PollUntilStable(ctx, client, appName, interval, timeoutMin*60, syncStartedAt, ticks, onTick)
}

// waitAppAppear 轮询直到 ArgoCD 里该 app 出现（app-of-apps 生成它需要点时间）。
func waitAppAppear(ctx context.Context, client *services.ArgocdClient, appName string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		st, err := client.GetAppStatus(c, appName)
		cancel()
		if err == nil && st != nil {
			return true
		}
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return false
		}
	}
	return false
}

func dur(start time.Time) int { return int(time.Since(start).Seconds()) }

// ---------- Lark（复用发布卡片，标题"新增模块"）----------

func sendOrchNotify(p *models.ProjectEnv, operator string, tempAtLarkIDs []string, successes, faileds []deployNotifyItem) {
	if len(successes) == 0 && len(faileds) == 0 {
		return
	}
	webhook, secret := resolveLarkTarget(p)
	if webhook == "" {
		return
	}
	// 艾特 = 操作人 + 项目参数固定的 + 本次临时选的，去重
	atIDs := dedupNonEmpty(append(append([]string{LookupContactLarkID(operator)}, parseNamespaces(p.AtLarkIDs)...), tempAtLarkIDs...))
	linkLabel, linkURL := "", ""
	if u := orchDetailURL(); u != "" {
		linkLabel, linkURL = "查看新增历史", u
	}
	send := func(kind string, items []deployNotifyItem) {
		if len(items) == 0 {
			return
		}
		title, color, body := buildDeployNotifyBody("新增模块", operator, atIDs, kind, p.Name, p.EnvType, items)
		c, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		if err := services.SendLarkCardWithRetry(c, webhook, secret, title, body, color, linkLabel, linkURL); err != nil {
			log.Printf("orch lark send failed: env=%s kind=%s err=%v", p.Name, kind, err)
		}
		cancel()
	}
	send("success", successes)
	send("fail", faileds)
}

// dedupNonEmpty 去空去重（保序），艾特列表用。
func dedupNonEmpty(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func orchDetailURL() string {
	var base string
	_ = database.DB.QueryRow(`SELECT IFNULL(deploy_center_base_url,'') FROM global_config WHERE id=1`).Scan(&base)
	base = strings.TrimRight(base, "/")
	if base == "" {
		return ""
	}
	return base + "/orchestration-history"
}

// ---------- 重试 ----------

// HandleRetryOrchTask POST /api/orchestration/tasks/{id}/retry —— 手动重新触发 ArgoCD 同步 + 轮询。
func HandleRetryOrchTask(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var t orchTaskRow
	var disable int
	err := database.DB.QueryRow(
		`SELECT id, project_env_id, module_name, kind, disable, IFNULL(app_name,''), IFNULL(namespace,''), IFNULL(operator,'')
		   FROM orchestration_task WHERE id=?`, id).
		Scan(&t.ID, &t.ProjectEnvID, &t.ModuleName, &t.Kind, &disable, &t.AppName, &t.Namespace, &t.Operator)
	if err != nil {
		JSONError(w, 40400, "任务不存在")
		return
	}
	if !IsEnvIDAllowed(r, t.ProjectEnvID) {
		JSONError(w, 40300, "无权操作该环境")
		return
	}
	if disable == 1 || t.Kind == "batch" {
		JSONError(w, 40001, "预演 / 批量任务不支持重试触发部署，请到 ArgoCD 手动同步")
		return
	}
	if IsTracked(t.ID) {
		JSONError(w, 40900, "该任务正在进行中，请勿重复触发")
		return
	}
	p, err := LoadProjectEnvDecrypted(t.ProjectEnvID)
	if err != nil {
		JSONError(w, 40400, "环境不存在")
		return
	}
	if p.EnvType == models.EnvPROD {
		if !HasButton(r, "submit_prod") {
			JSONError(w, 40300, "没有 PROD 发布权限（submit_prod）")
			return
		}
	} else if !HasButton(r, "submit_uat") {
		JSONError(w, 40300, "没有发布权限（submit_uat）")
		return
	}
	argoURL, argoToken, aerr := ResolveArgocdForEnv(p)
	if aerr != nil || argoURL == "" {
		JSONError(w, 40001, "未配置 ArgoCD，无法触发同步")
		return
	}
	appName := t.AppName
	if appName == "" {
		appName = strings.ToLower(t.ModuleName) + "-" + getGitService().ResolveAppNameSuffix(p.Name, p.ChartBasePath)
	}
	resetOrchTaskForRetry(t.ID)
	InflightTrack(func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
		defer cancel()
		RegisterCancel(t.ID, cancel)
		defer UnregisterCancel(t.ID)
		client := services.NewArgocdClient(argoURL, argoToken)
		m := newModDeploy{Module: t.ModuleName, Namespace: t.Namespace, App: appName}
		pollAndFinishModule(ctx, client, p, t.ID, t.Operator, m, parseNamespaces(p.AtLarkIDs), start, true) // 重试：临时艾特无法恢复，带上环境固定的
	})
	Audit(r, "orchestration.task_retry", "orchestration_task", fmt.Sprintf("%d", t.ID), map[string]interface{}{"app": appName})
	JSONSuccess(w, map[string]interface{}{"task_id": t.ID, "status": "pending"})
}
