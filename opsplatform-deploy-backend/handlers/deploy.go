package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// --- Preview ---

type previewImageReq struct {
	ProjectEnvID int64  `json:"project_env_id"`
	Text         string `json:"text"`
}

func HandlePreviewImage(w http.ResponseWriter, r *http.Request) {
	var req previewImageReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	p, err := LoadProjectEnvDecrypted(req.ProjectEnvID)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	if !IsEnvIDAllowed(r, req.ProjectEnvID) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}
	pending := services.ParseBatchInput(req.Text)
	if len(pending) == 0 {
		JSONError(w, 40001, "未解析出任何 模块:tag")
		return
	}
	modules := loadModulesMap(req.ProjectEnvID, p.ChartBasePath)
	diff := services.PreviewImage(pending, modules)
	JSONSuccess(w, map[string]interface{}{
		"diff":        diff,
		"env_type":    p.EnvType,
		"auto_sync":   p.AutoSync,
		"total_count": len(diff),
	})
}

// --- Update image ---

type updateImageReq struct {
	ProjectEnvID    int64               `json:"project_env_id"`
	Changes         []map[string]string `json:"changes"` // [{module,tag}, ...]
	RefDeploymentID int64               `json:"ref_deployment_id,omitempty"`
	// 传了 ref_deployment_id 表示"来自某次发布的回滚"；此时 action 记 rollback，
	// tag 仍以前端 textarea 里的为准（用户可以在回滚基础上改 tag）
}

func HandleUpdateImage(w http.ResponseWriter, r *http.Request) {
	if IsDraining() {
		JSONError(w, 50300, "service is draining (rolling update), please retry in a moment")
		return
	}
	var req updateImageReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if len(req.Changes) == 0 {
		JSONError(w, 40001, "changes 不能为空")
		return
	}
	p, err := LoadProjectEnvDecrypted(req.ProjectEnvID)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	if !IsEnvIDAllowed(r, req.ProjectEnvID) {
		JSONError(w, 40300, "无权操作该环境")
		return
	}
	// PROD 要 submit_prod；UAT 要 submit_uat（admin 自动放行）
	if p.EnvType == models.EnvPROD {
		if !HasButton(r, "submit_prod") {
			JSONError(w, 40300, "没有 PROD 发布权限（submit_prod）")
			return
		}
	} else {
		if !HasButton(r, "submit_uat") {
			JSONError(w, 40300, "没有 UAT 发布权限（submit_uat）")
			return
		}
	}
	pending := map[string]string{}
	for _, c := range req.Changes {
		if m, ok := c["module"]; ok {
			pending[m] = c["tag"]
		}
	}
	if len(pending) == 0 {
		JSONError(w, 40001, "没有合法的 {module,tag}")
		return
	}
	modules := loadModulesMap(req.ProjectEnvID, p.ChartBasePath)

	retry, interval, timeoutMin := loadPollCfg()
	modNamesJSON := marshalJSON(keysOf(pending))

	// 前端传了 ref_deployment_id 说明这是"基于某次发布的回滚"（可能编辑过 tag），action 记 rollback
	action := models.ActionUpdateImage
	opLabel := "发布"
	auditEvent := "deploy.update_image"
	var refID *int64
	if req.RefDeploymentID > 0 {
		action = models.ActionRollback
		opLabel = "回滚"
		auditEvent = "deploy.rollback_via_update"
		refID = &req.RefDeploymentID
	}

	depID, err := insertPendingDeployment(req.ProjectEnvID, action, refID, modNamesJSON, getOperator(r))
	if err != nil {
		JSONError(w, 50000, "insert deployment: "+err.Error())
		return
	}

	op := getOperator(r)

	// 抢同模块互斥锁——同 (env, module) 已被发布时拒绝
	moduleNames := keysOf(pending)
	conflicts, lockErr := AcquireModuleLocks(p.Name, moduleNames, depID, op)
	if lockErr != nil {
		// 锁系统异常（DB 错）—— 不阻塞发布，记 log 继续
		log.Printf("[module-lock] acquire error (continue without lock): %v", lockErr)
	} else if len(conflicts) > 0 {
		// 锁被别人占——409 Conflict 让前端显示通知
		// 把刚 INSERT 的 deployment 也撤回（避免出现一条永远 pending 的孤立记录）
		_, _ = database.DB.Exec(`DELETE FROM deployment WHERE id=?`, depID)
		JSONErrorWithData(w, 40900, "已有发布在进行中", map[string]interface{}{
			"conflicts": conflicts,
		})
		return
	}

	InflightTrack(func() {
		defer ReleaseModuleLocks(p.Name, moduleNames)
		runUpdateImageAsync(depID, p, pending, modules, retry, interval, timeoutMin, refID, op, opLabel)
	})

	Audit(r, auditEvent, "project_env", p.Name, map[string]interface{}{
		"deployment_id": depID, "env_type": p.EnvType, "modules": len(pending),
		"ref_deployment_id": req.RefDeploymentID,
	})
	JSONSuccess(w, map[string]interface{}{
		"deployment_id": depID,
		"status":        "pending",
	})
}

// --- Restart ---

type restartReq struct {
	ProjectEnvID int64    `json:"project_env_id"`
	ModuleNames  []string `json:"module_names"`
}

func HandleRestart(w http.ResponseWriter, r *http.Request) {
	if IsDraining() {
		JSONError(w, 50300, "service is draining (rolling update), please retry in a moment")
		return
	}
	var req restartReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if len(req.ModuleNames) == 0 {
		JSONError(w, 40001, "module_names 不能为空")
		return
	}
	p, err := LoadProjectEnvDecrypted(req.ProjectEnvID)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	if !IsEnvIDAllowed(r, req.ProjectEnvID) {
		JSONError(w, 40300, "无权操作该环境")
		return
	}
	if !HasButton(r, "restart") {
		JSONError(w, 40300, "没有重启权限（restart）")
		return
	}
	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	modules := loadModulesMap(req.ProjectEnvID, p.ChartBasePath)
	modNamesJSON := marshalJSON(req.ModuleNames)
	operator := getOperator(r)
	depID, err := insertPendingDeployment(req.ProjectEnvID, models.ActionRestart, nil, modNamesJSON, operator)
	if err != nil {
		JSONError(w, 50000, "insert deployment: "+err.Error())
		return
	}

	// 抢同模块互斥锁
	conflicts, lockErr := AcquireModuleLocks(p.Name, req.ModuleNames, depID, operator)
	if lockErr != nil {
		log.Printf("[module-lock] acquire error (continue without lock): %v", lockErr)
	} else if len(conflicts) > 0 {
		_, _ = database.DB.Exec(`DELETE FROM deployment WHERE id=?`, depID)
		JSONErrorWithData(w, 40900, "已有发布在进行中", map[string]interface{}{
			"conflicts": conflicts,
		})
		return
	}

	_, pollInterval, pollTimeoutMin := loadPollCfg()
	InflightTrack(func() {
		defer ReleaseModuleLocks(p.Name, req.ModuleNames)
		start := time.Now()
		// Restart 需要等所有 pod 真的 Healthy，30s 触发 + pollTimeoutMin 分钟轮询 + 1 分钟缓冲
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(pollTimeoutMin+1)*time.Minute+30*time.Second)
		defer cancel()
		ds := services.NewDeployService(nil)
		res := ds.Restart(ctx, services.RestartInput{
			ProjectEnvName:  p.Name,
			Namespace:       p.Namespace,
			Modules:         modules,
			ModuleNames:     req.ModuleNames,
			ArgocdClient:    services.NewArgocdClient(argoURL, argoToken),
			PollIntervalSec: pollInterval,
			PollTimeoutSec:  pollTimeoutMin * 60,
			// OnProgress 在 concurrent.go 的锁内调用；snapshot 在这里立刻序列化（离开栈后就不再引用）
			OnProgress: func(snapshot []models.ArgocdAppResult) {
				progJSON := marshalJSON(snapshot)
				_, _ = database.DB.Exec(
					`UPDATE deployment SET argocd_results=? WHERE id=?`,
					progJSON, depID)
			},
		})
		argoJSON := marshalJSON(res.ArgocdResults)
		_, _ = database.DB.Exec(`UPDATE deployment SET argocd_results=?, status=?, duration_sec=? WHERE id=?`,
			argoJSON, res.Status, int(time.Since(start).Seconds()), depID)
		archiveFailedPodLogsAsync(depID, p, collectFailedApps(res.ArgocdResults))
		sendRestartNotify(p, depID, operator, modules, res)
	})

	Audit(r, "deploy.restart", "project_env", p.Name, map[string]interface{}{
		"deployment_id": depID, "env_type": p.EnvType, "modules": len(req.ModuleNames),
	})
	JSONSuccess(w, map[string]interface{}{"deployment_id": depID, "status": "pending"})
}

// --- Rollback preview + Rollback ---

func HandleRollbackPreview(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var peID int64
	var raw []byte
	if err := database.DB.QueryRow(`SELECT project_env_id, changes FROM deployment WHERE id=?`, id).Scan(&peID, &raw); err != nil {
		JSONError(w, 40400, "deployment not found")
		return
	}
	if !IsEnvIDAllowed(r, peID) {
		JSONError(w, 40300, "无权访问该环境的发布记录")
		return
	}
	var changes []models.Change
	_ = jsonUnmarshalImpl(raw, &changes)
	p, _ := loadProjectEnvRaw(peID)
	modules := loadModulesMap(peID, p.ChartBasePath)
	type row struct {
		Module      string `json:"module"`
		FromTag     string `json:"from_tag"`      // 当前 tag
		ToTag       string `json:"to_tag"`        // 要回到的 tag
		CanRollback bool   `json:"can_rollback"`  // 当前 tag 等于 refChange.ToTag 时为 true
	}
	out := []row{}
	for _, c := range changes {
		cur := ""
		if m, ok := modules[c.Module]; ok {
			cur = m.CurrentTag
		}
		out = append(out, row{
			Module:      c.Module,
			FromTag:     cur,
			ToTag:       c.FromTag,
			CanRollback: cur == c.ToTag,
		})
	}
	JSONSuccess(w, map[string]interface{}{
		"ref_deployment_id": id,
		"project_env_id":    peID,
		"modules":           out,
	})
}

type rollbackReq struct {
	RefDeploymentID int64    `json:"ref_deployment_id"`
	SelectedModules []string `json:"selected_modules"`
}

func HandleRollback(w http.ResponseWriter, r *http.Request) {
	if IsDraining() {
		JSONError(w, 50300, "service is draining (rolling update), please retry in a moment")
		return
	}
	var req rollbackReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if len(req.SelectedModules) == 0 {
		JSONError(w, 40001, "selected_modules 不能为空")
		return
	}
	var peID int64
	var raw []byte
	if err := database.DB.QueryRow(`SELECT project_env_id, changes FROM deployment WHERE id=?`, req.RefDeploymentID).Scan(&peID, &raw); err != nil {
		JSONError(w, 40400, "ref deployment not found")
		return
	}
	var refChanges []models.Change
	_ = jsonUnmarshalImpl(raw, &refChanges)

	pending := services.BuildRollbackPending(refChanges, req.SelectedModules)
	if len(pending) == 0 {
		JSONError(w, 40001, "selected_modules 在 ref deployment 里没匹配到")
		return
	}

	p, err := LoadProjectEnvDecrypted(peID)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	if !IsEnvIDAllowed(r, peID) {
		JSONError(w, 40300, "无权操作该环境")
		return
	}
	if !HasButton(r, "rollback") {
		JSONError(w, 40300, "没有回滚权限（rollback）")
		return
	}
	modules := loadModulesMap(peID, p.ChartBasePath)

	retry, interval, timeoutMin := loadPollCfg()

	modNamesJSON := marshalJSON(keysOf(pending))
	ref := req.RefDeploymentID
	depID, err := insertPendingDeployment(peID, models.ActionRollback, &ref, modNamesJSON, getOperator(r))
	if err != nil {
		JSONError(w, 50000, "insert deployment: "+err.Error())
		return
	}

	op := getOperator(r)

	// 抢同模块互斥锁
	moduleNames := keysOf(pending)
	conflicts, lockErr := AcquireModuleLocks(p.Name, moduleNames, depID, op)
	if lockErr != nil {
		log.Printf("[module-lock] acquire error (continue without lock): %v", lockErr)
	} else if len(conflicts) > 0 {
		_, _ = database.DB.Exec(`DELETE FROM deployment WHERE id=?`, depID)
		JSONErrorWithData(w, 40900, "已有发布在进行中", map[string]interface{}{
			"conflicts": conflicts,
		})
		return
	}

	InflightTrack(func() {
		defer ReleaseModuleLocks(p.Name, moduleNames)
		runUpdateImageAsync(depID, p, pending, modules, retry, interval, timeoutMin, &ref, op, "回滚")
	})

	Audit(r, "deploy.rollback", "project_env", p.Name, map[string]interface{}{
		"deployment_id": depID, "ref_deployment_id": ref, "modules": len(pending),
	})
	JSONSuccess(w, map[string]interface{}{
		"deployment_id": depID,
		"status":        "pending",
	})
}

// --- helpers ---

func loadModulesMap(projectEnvID int64, chartBasePath string) map[string]services.Module {
	rows, err := database.DB.Query(`SELECT name, current_tag, argocd_app_name, IFNULL(namespace,'') FROM module WHERE project_env_id=?`, projectEnvID)
	if err != nil {
		return map[string]services.Module{}
	}
	defer rows.Close()
	out := map[string]services.Module{}
	for rows.Next() {
		var name, tag, app, ns string
		_ = rows.Scan(&name, &tag, &app, &ns)
		out[name] = services.Module{
			Name:         name,
			CurrentTag:   tag,
			ChartRelPath: services.BuildValuesPath(chartBasePath, name),
			ArgocdApp:    app,
			Namespace:    ns,
		}
	}
	return out
}

// getOperator: 优先从 JWT context 拿，其次 X-Operator header，最后 system
func getOperator(r *http.Request) string {
	if u := UsernameFromCtx(r); u != "" {
		return u
	}
	op := r.Header.Get("X-Operator")
	if op == "" {
		return "system"
	}
	return op
}

func loadPollCfg() (retry, interval, timeoutMin int) {
	_ = database.DB.QueryRow(`SELECT git_retry_count, poll_interval_sec, poll_timeout_min FROM global_config WHERE id=1`).
		Scan(&retry, &interval, &timeoutMin)
	if retry == 0 {
		retry = 3
	}
	if interval == 0 {
		interval = 10
	}
	if timeoutMin == 0 {
		timeoutMin = 3
	}
	return
}

func marshalJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func insertPendingDeployment(peID int64, action string, refID *int64, modNamesJSON []byte, operator string) (int64, error) {
	res, err := database.DB.Exec(`INSERT INTO deployment (project_env_id, action, ref_deployment_id, module_names, operator, status)
		VALUES (?, ?, ?, ?, ?, 'pending')`, peID, action, refID, modNamesJSON, operator)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// runUpdateImageAsync 后台跑完整的发布流水线，update-image 和 rollback 共用
//
//	opLabel: Lark 通知标题用 —— "发布" 或 "回滚"
func runUpdateImageAsync(depID int64, p *models.ProjectEnv, pending map[string]string, modules map[string]services.Module,
	gitRetry, pollInterval, pollTimeoutMin int, _refDepID *int64, operator, opLabel string) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(pollTimeoutMin+2)*time.Minute)
	defer cancel()

	gs := getGitService()
	ds := services.NewDeployService(gs)
	var argoClient *services.ArgocdClient
	if p.AutoSync == 1 {
		if argoURL, argoToken, err := ResolveArgocdForEnv(p); err == nil {
			argoClient = services.NewArgocdClient(argoURL, argoToken)
		}
	}
	res := ds.UpdateImage(ctx, services.UpdateImageInput{
		ProjectEnvName:  p.Name,
		GitRepo:         p.GitRepo,
		GitBranch:       p.GitBranch,
		ChartBasePath:   p.ChartBasePath,
		GitRetry:        gitRetry,
		Operator:        operator,
		Pending:         pending,
		Modules:         modules,
		AutoSync:        p.AutoSync == 1,
		ArgocdClient:    argoClient,
		PollIntervalSec: pollInterval,
		PollTimeoutSec:  pollTimeoutMin * 60,
		// 渐进式写 argocd_results：每个 app 每次 poll 都推一个中间态，前端 5s 刷能看到
		// Syncing → Progressing → Healthy 的过程以及单模块耗时实时增长
		OnProgress: func(snapshot []models.ArgocdAppResult) {
			progJSON := marshalJSON(snapshot)
			_, _ = database.DB.Exec(
				`UPDATE deployment SET argocd_results=? WHERE id=?`,
				progJSON, depID)
		},
	})
	changesJSON := marshalJSON(res.Changes)
	argoJSON := marshalJSON(res.ArgocdResults)
	errMsg := ""
	if res.Err != nil {
		errMsg = res.Err.Error()
	}
	_, _ = database.DB.Exec(`UPDATE deployment SET changes=?, git_commit=?, git_commit_url=?, argocd_results=?, status=?, error_msg=?, duration_sec=?
		WHERE id=?`,
		changesJSON, res.GitCommit, res.GitCommitURL, argoJSON, res.Status, errMsg, int(time.Since(start).Seconds()), depID)

	for _, c := range res.Changes {
		_, _ = database.DB.Exec(`UPDATE module SET current_tag=? WHERE project_env_id=? AND name=?`, c.ToTag, p.ID, c.Module)
	}

	archiveFailedPodLogsAsync(depID, p, collectFailedApps(res.ArgocdResults))
	sendUpdateImageNotify(p, depID, operator, opLabel, modules, res)
}

// collectFailedApps 从 argocd 结果里挑出失败的 app 名（Failed/Timeout/Degraded）
func collectFailedApps(results []models.ArgocdAppResult) []string {
	out := []string{}
	for _, r := range results {
		s := strings.ToLower(r.SyncStatus)
		h := strings.ToLower(r.Health)
		if s == "failed" || s == "timeout" || h == "degraded" {
			out = append(out, r.App)
		}
	}
	return out
}

// --- Lark notify ---

// resolveLarkTarget 解析出 webhook + 明文 secret
// 只有两级：1) p.LarkBotID → lark_bot 表  2) 遗留字段 p.LarkWebhook/Secret（历史）
// 全局默认回落已移除——必须在项目环境里显式选 Lark 机器人才会发通知。
func resolveLarkTarget(p *models.ProjectEnv) (webhook, secret string) {
	if p.LarkBotID != nil && *p.LarkBotID > 0 {
		if bot, err := LoadLarkBotDecrypted(*p.LarkBotID); err == nil {
			return bot.Webhook, bot.Secret
		}
	}
	if p.LarkWebhook != "" {
		webhook = p.LarkWebhook
		secret, _ = crypto.Decrypt(p.LarkSecret)
		return
	}
	return "", ""
}

// deployDetailURL 构造 Lark 卡片"查看发布详情"按钮的 URL。
//
//	base_url 在「系统设置 → 全局凭证」里配置（deploy_center_base_url）。
//	未配置则返回空串，调用方可选择回落到 git commit URL 或不加按钮。
func deployDetailURL(depID int64) string {
	var base string
	_ = database.DB.QueryRow(`SELECT IFNULL(deploy_center_base_url,'') FROM global_config WHERE id=1`).Scan(&base)
	base = strings.TrimRight(base, "/")
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/history?expand=%d", base, depID)
}

func larkColorForStatus(status string) (color, title string) {
	switch status {
	case models.StatusFailed:
		return "red", "❌ 发布失败"
	case models.StatusPartial:
		return "orange", "⚠️ 部分成功"
	case models.StatusNoChange:
		return "blue", "ℹ️ 无变更"
	default:
		return "green", "✅ 发布成功"
	}
}

// sendUpdateImageNotify 发布/回滚的 Lark 通知。
//
//	**partial 场景拆成两张卡发送**（一张绿成功 + 一张红失败），其它纯成功/纯失败/无变更
//	照常一张。所有卡都 @ 操作人，方便 cesar 这种业务负责人在 lark 群里被定位。
func sendUpdateImageNotify(p *models.ProjectEnv, depID int64, operator, opLabel string,
	modules map[string]services.Module, res *services.UpdateImageResult) {
	webhook, secret := resolveLarkTarget(p)
	if webhook == "" {
		log.Printf("lark skip: dep=%d no webhook configured (project_env=%s)", depID, p.Name)
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "skipped", depID)
		return
	}
	atID := LookupContactLarkID(operator)
	successes, skippeds, faileds := buildUpdateNotifyItems(modules, res)
	sendCardSets(p, depID, operator, opLabel, atID, webhook, secret, res.GitCommitURL,
		successes, skippeds, faileds)
}

func sendRestartNotify(p *models.ProjectEnv, depID int64, operator string,
	modules map[string]services.Module, res *services.RestartResult) {
	webhook, secret := resolveLarkTarget(p)
	if webhook == "" {
		log.Printf("lark skip: dep=%d no webhook configured (project_env=%s)", depID, p.Name)
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "skipped", depID)
		return
	}
	atID := LookupContactLarkID(operator)
	successes, faileds := buildRestartNotifyItems(modules, res)
	sendCardSets(p, depID, operator, "重启", atID, webhook, secret, "",
		successes, nil, faileds)
}

// sendCardSets 根据成功/跳过/失败三个分组决定发几张卡：
//   - 全部三组都为空 → 不发
//   - 只有一组非空 → 发 1 张该类型卡
//   - 多组非空 → 拆开发多张（先成功，后跳过，最后失败）
func sendCardSets(p *models.ProjectEnv, depID int64, operator, opLabel, atID,
	webhook, secret, gitCommitURL string,
	successes, skippeds, faileds []deployNotifyItem) {
	type setEntry struct {
		kind  string
		items []deployNotifyItem
	}
	sets := []setEntry{}
	if len(successes) > 0 {
		sets = append(sets, setEntry{"success", successes})
	}
	if len(skippeds) > 0 {
		sets = append(sets, setEntry{"skip", skippeds})
	}
	if len(faileds) > 0 {
		sets = append(sets, setEntry{"fail", faileds})
	}
	if len(sets) == 0 {
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "skipped", depID)
		return
	}

	// 链接按钮：优先发布详情，回落 git commit
	linkLabel, linkURL := "", ""
	if u := deployDetailURL(depID); u != "" {
		linkLabel, linkURL = "查看发布详情", u
	} else if gitCommitURL != "" {
		linkLabel, linkURL = "查看 commit", gitCommitURL
	}

	anyFail := false
	for _, s := range sets {
		title, color, body := buildDeployNotifyBody(opLabel, operator, atID, s.kind, s.items)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := services.SendLarkCard(ctx, webhook, secret, title, body, color, linkLabel, linkURL)
		cancel()
		if err != nil {
			log.Printf("lark send failed: dep=%d kind=%s err=%v", depID, s.kind, err)
			anyFail = true
		}
	}
	status := "success"
	if anyFail {
		status = "failed"
	}
	_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, status, depID)
}
