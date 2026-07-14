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

// 新增模块真部署后，跟发布一样轮询新模块的 ArgoCD app 到 Synced+Healthy，结果写进 orchestration_task。
//
// 跟普通发布不同：新增比发布多一层 app-of-apps——git 提交后，根 app 要先同步、把这个新 Application
// 生成出来，再等它起来。所以流程是：best-effort 触发根 app 同步 → 等子 app 出现 → 同步子 app + 轮询到稳定。

// deployAndPollNewModule 在 git 提交成功后调用（disable=false 真部署时）。start 是整个任务起点，用于算总耗时。
func deployAndPollNewModule(taskID, envID int64, moduleName, namespace string, start time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	p, err := LoadProjectEnvDecrypted(envID)
	if err != nil {
		finishOrchTask(taskID, "success", "已提交（环境信息加载失败，无法自动跟踪部署状态，请到 ArgoCD 查看）", nil, dur(start))
		return
	}
	gs := getGitService()
	suffix := gs.ResolveAppNameSuffix(p.Name, p.ChartBasePath)
	appName := strings.ToLower(moduleName) + "-" + suffix
	setOrchTaskApp(taskID, appName, namespace)

	argoURL, argoToken, aerr := ResolveArgocdForEnv(p)
	if aerr != nil || argoURL == "" {
		finishOrchTask(taskID, "success", "已提交（未配置 ArgoCD，无法自动跟踪部署状态）", nil, dur(start))
		return
	}
	if p.AutoSync != 1 {
		finishOrchTask(taskID, "success",
			"已提交，该环境未开自动同步——请到 ArgoCD 手动同步，或点「重试」触发部署", nil, dur(start))
		return
	}
	client := services.NewArgocdClient(argoURL, argoToken)
	pollModuleApp(ctx, client, p, appName, taskID, start, true)
}

// pollModuleApp 触发 + 轮询单个模块 app 到稳定，渐进式写 argocd_results，终态写状态。
//
//	waitAppear=true：先 best-effort 触发根 app 同步 + 等子 app 出现（新增场景，app 可能还没生成）。
//	                 重试时也用 true（可能上次根没同步出来）。
func pollModuleApp(ctx context.Context, client *services.ArgocdClient, p *models.ProjectEnv, appName string, taskID int64, start time.Time, waitAppear bool) {
	_, interval, timeoutMin := loadPollCfg()
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
	publish := func(r *models.ArgocdAppResult) {
		setOrchTaskArgocd(taskID, marshalJSON([]models.ArgocdAppResult{*r}))
	}
	publish(&models.ArgocdAppResult{
		App: appName, SyncStatus: "Syncing", Health: "Progressing",
		Msg: "正在触发同步 / 等待 ArgoCD 生成 Application", LastPolledAt: time.Now(),
	})

	if waitAppear {
		// best-effort 触发 app-of-apps 根同步，让新 Application 尽快生成（根 app 名约定 <env>-apps）
		rootApp := strings.ToLower(p.Name) + "-apps"
		sctx, scancel := context.WithTimeout(ctx, 20*time.Second)
		_ = client.Sync(sctx, rootApp)
		scancel()
		// 等子 app 在 ArgoCD 出现
		if !waitAppAppear(ctx, client, appName, 3*time.Minute) {
			log.Printf("⚠ [orch-fail] task=%d env=%s app=%s 等待 ArgoCD 生成 Application 超时", taskID, p.Name, appName)
			r := models.ArgocdAppResult{
				App: appName, SyncStatus: "Failed",
				Msg:          "失败 · 等待 ArgoCD 生成 Application 超时（请检查 app-of-apps 根应用是否已同步）",
				DurationSec:  dur(start), LastPolledAt: time.Now(),
			}
			finishOrchTask(taskID, "failed", r.Msg, marshalJSON([]models.ArgocdAppResult{r}), dur(start))
			return
		}
	}

	// 触发子 app 同步 + 轮询到稳定（用发布同款动态稳定窗口 + 超时）
	syncStartedAt := time.Now()
	_ = client.Sync(ctx, appName)
	select {
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
	}
	r := services.PollUntilStable(ctx, client, appName, interval, timeoutMin*60, syncStartedAt, ticks,
		func(t *models.ArgocdAppResult) { publish(t) })

	status, note := "success", "服务已部署（Synced + Healthy）"
	if r == nil || !(strings.EqualFold(r.SyncStatus, "Synced") && strings.EqualFold(r.Health, "Healthy")) {
		status = "failed"
		note = "服务部署未就绪"
		if r != nil && r.Msg != "" {
			note = "服务部署未就绪 · " + r.Msg
		}
		log.Printf("⚠ [orch-fail] task=%d env=%s app=%s 部署未就绪: %s", taskID, p.Name, appName, note)
	}
	var results []models.ArgocdAppResult
	if r != nil {
		results = []models.ArgocdAppResult{*r}
	}
	finishOrchTask(taskID, status, note, marshalJSON(results), dur(start))
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

// HandleRetryOrchTask POST /api/orchestration/tasks/{id}/retry —— 手动重新触发 ArgoCD 同步 + 轮询。
func HandleRetryOrchTask(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var t orchTaskRow
	var disable int
	err := database.DB.QueryRow(
		`SELECT id, project_env_id, module_name, kind, disable, IFNULL(app_name,''), IFNULL(namespace,'')
		   FROM orchestration_task WHERE id=?`, id).
		Scan(&t.ID, &t.ProjectEnvID, &t.ModuleName, &t.Kind, &disable, &t.AppName, &t.Namespace)
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
	if appName == "" { // 老任务没存 app 名 → 重新推导
		gs := getGitService()
		appName = strings.ToLower(t.ModuleName) + "-" + gs.ResolveAppNameSuffix(p.Name, p.ChartBasePath)
	}
	resetOrchTaskForRetry(t.ID)
	InflightTrack(func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
		defer cancel()
		RegisterCancel(t.ID, cancel)
		defer UnregisterCancel(t.ID)
		client := services.NewArgocdClient(argoURL, argoToken)
		pollModuleApp(ctx, client, p, appName, t.ID, start, true)
	})
	Audit(r, "orchestration.task_retry", "orchestration_task", fmt.Sprintf("%d", t.ID), map[string]interface{}{"app": appName})
	JSONSuccess(w, map[string]interface{}{"task_id": t.ID, "status": "pending"})
}
