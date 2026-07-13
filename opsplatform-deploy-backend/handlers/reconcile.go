package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// 发布断链自愈：deploy-center 后端在发布进行中重启（升级代码版本等），
// 原来靠 in-process goroutine 跟踪的发布任务就断了联系，记录永远卡 pending。
//
// 本文件在「启动时 + 定时」对账这些孤儿 pending：
//   · push 之后被中断（DB 里有 git_commit 或 argocd_results）——代码已提交，
//     去 ArgoCD 对账：已 Synced+Healthy → 成功；否则挂后台续 poll 到终态。
//   · push 之前被中断（没有 git_commit / argocd_results）——服务实际没变化：
//       - 期间没人对同模块发过更新版本 → 自动重跑（无感续完）
//       - 期间已有更新的发布 → 不覆盖，标 interrupted + 提示重试（服务未受影响）
//
// 只有超过 reconcileGrace 且当前进程没在跟（IsTracked=false）的 pending 才算孤儿，
// 避免误伤正常在跑的长发布。

const reconcileGrace = 3 * time.Minute

type orphanDeploy struct {
	id         int64
	peID       int64
	action     string
	operator   string
	gitCommit  string
	argoRaw    string
	changesRaw string
	modNames   []string
}

// ReconcileOrphanedDeploys 对账孤儿 pending 发布。reason 仅用于日志（startup / periodic）。
func ReconcileOrphanedDeploys(reason string) {
	rows, err := database.DB.Query(`
		SELECT id, project_env_id, action, IFNULL(operator,''), IFNULL(git_commit,''),
		       IFNULL(argocd_results,''), IFNULL(changes,''), IFNULL(module_names,'[]')
		  FROM deployment
		 WHERE status='pending'
		   AND created_at < DATE_SUB(NOW(), INTERVAL 3 MINUTE)
		 ORDER BY id`)
	if err != nil {
		log.Printf("[reconcile:%s] query orphans: %v", reason, err)
		return
	}
	var list []orphanDeploy
	for rows.Next() {
		var o orphanDeploy
		var modRaw string
		if err := rows.Scan(&o.id, &o.peID, &o.action, &o.operator, &o.gitCommit, &o.argoRaw, &o.changesRaw, &modRaw); err != nil {
			continue
		}
		// 当前进程正在正常跟这条（比如刚重跑起来）→ 跳过
		if IsTracked(o.id) {
			continue
		}
		_ = jsonUnmarshalImpl([]byte(modRaw), &o.modNames)
		list = append(list, o)
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	log.Printf("[reconcile:%s] 发现 %d 条孤儿 pending 发布，开始对账", reason, len(list))
	for _, o := range list {
		reconcileOne(o)
	}
}

func reconcileOne(o orphanDeploy) {
	// VM 部署有自己的一套 task 轮询，这里的 K8s 对账逻辑不适用 → 直接标失败给出重发提示
	if strings.HasPrefix(o.action, "vm_") {
		markReconcileFailed(o.id, "后端重启中断了 VM 部署任务，请到发布控制台重新执行。")
		return
	}
	p, err := LoadProjectEnvDecrypted(o.peID)
	if err != nil {
		markReconcileFailed(o.id, "对账失败：所属环境已删除或不可用，请人工确认 git/ArgoCD 状态。")
		return
	}

	if isAfterPush(o) {
		reconcileAfterPush(o, p)
	} else {
		reconcileBeforePush(o, p)
	}
}

// isAfterPush：DB 里有 git_commit 或非空 argocd_results 说明 push 已经发生过。
func isAfterPush(o orphanDeploy) bool {
	if strings.TrimSpace(o.gitCommit) != "" {
		return true
	}
	var results []models.ArgocdAppResult
	if jsonUnmarshalImpl([]byte(o.argoRaw), &results) == nil && len(results) > 0 {
		return true
	}
	return false
}

// reconcileAfterPush 代码已提交 → 去 ArgoCD 对账。
//
//	全部 Synced+Healthy → 直接判成功（快路径）；
//	还没稳定 / 有异常 → 挂后台续 poll，让 PollUntilStable 用它自己那套稳定+降级判定收尾。
func reconcileAfterPush(o orphanDeploy, p *models.ProjectEnv) {
	apps := appsOfDeploy(o, p)
	argoURL, argoToken, aerr := ResolveArgocdForEnv(p)
	// 没开启自动同步跟踪 / 拿不到 ArgoCD → deploy-center 的职责到 push 为止，判成功。
	if p.AutoSync != 1 || aerr != nil || argoURL == "" || len(apps) == 0 {
		markReconcileSuccess(o.id, "后端曾重启，本次代码已提交到 Git，ArgoCD 将自行同步（未开启同步跟踪，状态按已提交计为成功）。")
		return
	}

	client := services.NewArgocdClient(argoURL, argoToken)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	statuses := map[string]*services.AppStatus{}
	allHealthy := true
	for _, app := range apps {
		st, err := client.GetAppStatus(ctx, app)
		if err != nil || st == nil || !strings.EqualFold(st.SyncStatus, "Synced") || !strings.EqualFold(st.Health, "Healthy") {
			allHealthy = false
			break
		}
		statuses[app] = st
	}
	cancel()

	if allHealthy {
		// 刷新同步结果快照为 Synced+Healthy（消掉重启前残留的 Progressing/等待 Pod 就绪），
		// 说明走 status_note（不脱敏），成功不再挂"错误信息"。
		fresh := refreshResultsHealthy(o.argoRaw, apps, statuses, "后端重启后对账确认已就绪")
		markReconcileSuccessRefresh(o.id,
			"此发布曾因后端重启中断，已由系统自动对账确认服务就绪、判定成功。", fresh)
		return
	}
	// 还在滚动 / 状态未定 → 后台续 poll 收尾
	log.Printf("[reconcile] dep=%d 已提交但未稳定，挂后台续 poll %d 个 app", o.id, len(apps))
	resumePollAfterRestart(o.id, p, apps, argoURL, argoToken)
}

// refreshResultsHealthy 把旧的 argocd_results 快照按 app 覆写成当前 Synced+Healthy + note，
// 保留原有的 DurationSec 等字段；旧快照里没有的 app 补一条。
func refreshResultsHealthy(argoRaw string, apps []string, statuses map[string]*services.AppStatus, note string) []models.ArgocdAppResult {
	var old []models.ArgocdAppResult
	_ = jsonUnmarshalImpl([]byte(argoRaw), &old)
	byApp := map[string]models.ArgocdAppResult{}
	for _, r := range old {
		byApp[r.App] = r
	}
	out := make([]models.ArgocdAppResult, 0, len(apps))
	for _, app := range apps {
		r := byApp[app] // 零值也没关系
		r.App = app
		r.SyncStatus = "Synced"
		r.Health = "Healthy"
		if st := statuses[app]; st != nil {
			if st.SyncStatus != "" {
				r.SyncStatus = st.SyncStatus
			}
			if st.Health != "" {
				r.Health = st.Health
			}
		}
		r.Msg = note
		r.LastPolledAt = time.Now()
		out = append(out, r)
	}
	return out
}

// resumePollAfterRestart 重新接管一次 poll：对每个 app 用零 syncStartedAt 跑 PollUntilStable
// （sync 早已触发，只需等它稳定），聚合出终态写回。复用正常发布尾部的成功/降级/超时判定。
func resumePollAfterRestart(depID int64, p *models.ProjectEnv, apps []string, argoURL, argoToken string) {
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
	InflightTrack(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		RegisterCancel(depID, cancel)
		defer UnregisterCancel(depID)
		client := services.NewArgocdClient(argoURL, argoToken)
		results := make([]models.ArgocdAppResult, len(apps))
		for i, app := range apps {
			idx := i
			r := services.PollUntilStable(ctx, client, app, interval, timeoutMin*60, time.Time{}, ticks,
				func(cur *models.ArgocdAppResult) {
					results[idx] = *cur
					_, _ = database.DB.Exec(`UPDATE deployment SET argocd_results=? WHERE id=?`, marshalJSON(results), depID)
				})
			if r != nil {
				results[idx] = *r
			}
		}
		failed := collectFailedApps(results)
		status := models.StatusSuccess
		note := "此发布曾因后端重启中断，已由系统自动接管跟踪、确认服务就绪，判定成功。"
		if len(results) > 0 && len(failed) == len(results) {
			status = models.StatusFailed
			note = "此发布曾因后端重启中断，系统接管跟踪后 ArgoCD 判定发布失败，请查看同步结果与 Pod 日志。"
		} else if len(failed) > 0 {
			status = models.StatusPartial
			note = "此发布曾因后端重启中断，系统接管跟踪后部分模块失败，请查看同步结果。"
		}
		// 说明走 status_note（不脱敏）；失败的技术细节仍在 argocd_results 各行的消息里。
		_, _ = database.DB.Exec(
			`UPDATE deployment SET argocd_results=?, status=?, status_note=? WHERE id=?`,
			marshalJSON(results), status, note, depID)
		ArchiveFailedPodLogs(depID, p, collectFailingPodsByApp(results))
	})
}

// reconcileBeforePush 代码还没提交 → 服务实际没变化。
func reconcileBeforePush(o orphanDeploy, p *models.ProjectEnv) {
	// 期间是否已有别人对同模块发过更新版本？有 → 不覆盖，标 interrupted + 提示重试
	if hasLaterConflict(o.peID, o.id, o.modNames) {
		markInterrupted(o.id,
			"后端重启导致本次发布在提交前被中断，服务未受任何影响（还是原版本）。\n"+
				"期间该服务已有更新的发布，本次未自动重发以免覆盖新版本。\n"+
				"如仍需发布本次内容，请点「重试」。")
		return
	}
	// 没有冲突 → 自动重跑续完
	if !rerunDeployment(o, p, "自动重发") {
		// 无法自动重跑（缺目标版本等）→ 落 interrupted 让用户重试
		markInterrupted(o.id,
			"后端重启导致本次发布在提交前被中断，服务未受任何影响（还是原版本）。\n"+
				"无法自动重发，请点「重试」重新发布。")
	}
}

// hasLaterConflict 是否存在比 id 更晚、touch 到同一模块、且不是取消/无变更/中断态的发布。
// id 自增 → id 更大即更晚。
func hasLaterConflict(peID, id int64, myMods []string) bool {
	if len(myMods) == 0 {
		return false
	}
	mine := map[string]bool{}
	for _, m := range myMods {
		mine[m] = true
	}
	rows, err := database.DB.Query(`
		SELECT IFNULL(module_names,'[]') FROM deployment
		 WHERE project_env_id=? AND id>? AND status NOT IN ('canceled','no_change','interrupted')`, peID, id)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		var mods []string
		_ = jsonUnmarshalImpl([]byte(raw), &mods)
		for _, m := range mods {
			if mine[m] {
				return true
			}
		}
	}
	return false
}

// rerunDeployment 复用同一条记录重新发一次（自动重发 / 用户重试共用）。
// 返回 false 表示没能发起（缺目标版本 / 被并发锁挡住），调用方决定后续状态。
func rerunDeployment(o orphanDeploy, p *models.ProjectEnv, opLabel string) bool {
	modules := loadModulesMap(o.peID, p.ChartBasePath)
	operator := o.operator
	if operator == "" {
		operator = "system"
	}

	// restart 无 tag 意图，直接重跑重启
	if o.action == models.ActionRestart {
		if len(o.modNames) == 0 {
			return false
		}
		if !acquireForRerun(p.Name, o.modNames, o.id, operator) {
			return false
		}
		resetForRerun(o.id)
		gitRetry, interval, timeoutMin := loadPollCfg()
		InflightTrack(func() {
			defer ReleaseModuleLocks(p.Name, o.modNames)
			runRestartAsync(o.id, p, o.modNames, modules, gitRetry, interval, timeoutMin, operator)
		})
		return true
	}

	// update_image / rollback：从 changes 的 ToTag 重建 pending
	pending := pendingFromChanges(o.changesRaw)
	if len(pending) == 0 {
		return false
	}
	moduleNames := keysOf(pending)
	if !acquireForRerun(p.Name, moduleNames, o.id, operator) {
		return false
	}
	resetForRerun(o.id)
	gitRetry, interval, timeoutMin := loadPollCfg()
	InflightTrack(func() {
		defer ReleaseModuleLocks(p.Name, moduleNames)
		runUpdateImageAsync(o.id, p, pending, modules, gitRetry, interval, timeoutMin, nil, operator, opLabel)
	})
	return true
}

// acquireForRerun 抢模块锁；被别人占着（有并发发布在跑）返回 false。
func acquireForRerun(envName string, mods []string, depID int64, operator string) bool {
	conflicts, err := AcquireModuleLocks(envName, mods, depID, operator)
	if err != nil {
		// 锁系统异常不阻塞（跟正常发布一致）
		return true
	}
	return len(conflicts) == 0
}

// resetForRerun 把记录清成干净的 pending，重新走一遍流水线。
func resetForRerun(depID int64) {
	_, _ = database.DB.Exec(
		`UPDATE deployment SET status='pending', error_msg='', status_note='', argocd_results=NULL, duration_sec=NULL WHERE id=?`, depID)
}

// pendingFromChanges 从 changes JSON（[{module,to_tag}]）重建 module→目标tag。
func pendingFromChanges(raw string) map[string]string {
	var changes []models.Change
	if jsonUnmarshalImpl([]byte(raw), &changes) != nil {
		return nil
	}
	out := map[string]string{}
	for _, c := range changes {
		if c.Module != "" && c.ToTag != "" {
			out[c.Module] = c.ToTag
		}
	}
	return out
}

// appsOfDeploy 把 module_names 映射成 ArgoCD app 名（优先 module.argocd_app_name）。
func appsOfDeploy(o orphanDeploy, p *models.ProjectEnv) []string {
	modules := loadModulesMap(o.peID, p.ChartBasePath)
	var apps []string
	for _, name := range o.modNames {
		app := name
		if m, ok := modules[name]; ok && m.ArgocdApp != "" {
			app = m.ArgocdApp
		}
		apps = append(apps, app)
	}
	return apps
}

// HandleRetryDeployment POST /deployments/{id}/retry
//
//	对 failed / interrupted 的发布用原记录的意图（module→目标 tag）重新发一次，复用同一条记录。
//	主要给「提交前中断」的 interrupted 记录一键重发，也支持对真失败的记录重试。
func HandleRetryDeployment(w http.ResponseWriter, r *http.Request) {
	if IsDraining() {
		JSONError(w, 50300, "service is draining (rolling update), please retry in a moment")
		return
	}
	id := ParseID(mux.Vars(r)["id"])
	if id <= 0 {
		JSONError(w, 40001, "invalid id")
		return
	}
	var o orphanDeploy
	var status, modRaw string
	err := database.DB.QueryRow(`
		SELECT id, project_env_id, action, status, IFNULL(operator,''),
		       IFNULL(changes,''), IFNULL(module_names,'[]')
		  FROM deployment WHERE id=?`, id).
		Scan(&o.id, &o.peID, &o.action, &status, &o.operator, &o.changesRaw, &modRaw)
	if err != nil {
		JSONError(w, 40400, "deployment not found")
		return
	}
	if !IsEnvIDAllowed(r, o.peID) {
		JSONError(w, 40300, "无权操作该环境")
		return
	}
	if status != models.StatusFailed && status != models.StatusInterrupted {
		JSONError(w, 40001, "只有失败 / 已中断的发布可以重试")
		return
	}
	if IsTracked(o.id) {
		JSONError(w, 40900, "该发布正在进行中，请勿重复触发")
		return
	}
	p, err := LoadProjectEnvDecrypted(o.peID)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	// 权限：跟发布一致——PROD 要 submit_prod，其余要 submit_uat（admin 放行）
	if p.EnvType == models.EnvPROD {
		if !HasButton(r, "submit_prod") {
			JSONError(w, 40300, "没有 PROD 发布权限（submit_prod）")
			return
		}
	} else if !HasButton(r, "submit_uat") {
		JSONError(w, 40300, "没有发布权限（submit_uat）")
		return
	}
	_ = jsonUnmarshalImpl([]byte(modRaw), &o.modNames)
	// 操作人记成当前重试的人，Lark/审计更准确
	o.operator = getOperator(r)

	if !rerunDeployment(o, p, "重试") {
		JSONError(w, 40900, "无法重试：缺少目标版本，或该服务已有更新的发布在进行中")
		return
	}
	Audit(r, "deploy.retry", "project_env", p.Name, map[string]interface{}{
		"deployment_id": o.id, "action": o.action,
	})
	JSONSuccess(w, map[string]interface{}{"deployment_id": o.id, "status": "pending"})
}

// 以下 mark* 的"说明"一律写 status_note（干净、不脱敏）；error_msg 只留给真失败的技术细节。

func markInterrupted(depID int64, note string) {
	_, _ = database.DB.Exec(`UPDATE deployment SET status='interrupted', status_note=?, error_msg='' WHERE id=?`, note, depID)
	log.Printf("[reconcile] dep=%d → interrupted（提交前中断，服务未受影响，需重试）", depID)
}

func markReconcileFailed(depID int64, note string) {
	_, _ = database.DB.Exec(`UPDATE deployment SET status='failed', status_note=? WHERE id=?`, note, depID)
	log.Printf("[reconcile] dep=%d → failed", depID)
}

func markReconcileSuccess(depID int64, note string) {
	// 成功不再往 error_msg 塞说明（否则前端会当"错误信息"展示 + 非 admin 被脱敏）
	_, _ = database.DB.Exec(`UPDATE deployment SET status='success', status_note=?, error_msg='' WHERE id=?`, note, depID)
	log.Printf("[reconcile] dep=%d → success", depID)
}

// markReconcileSuccessRefresh 判成功的同时刷新同步结果快照（消掉重启前残留的 Progressing）。
func markReconcileSuccessRefresh(depID int64, note string, results []models.ArgocdAppResult) {
	_, _ = database.DB.Exec(
		`UPDATE deployment SET status='success', status_note=?, error_msg='', argocd_results=? WHERE id=?`,
		note, marshalJSON(results), depID)
	log.Printf("[reconcile] dep=%d → success（已刷新同步结果快照）", depID)
}
