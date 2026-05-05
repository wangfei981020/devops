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

	// 三方一致性预检（Harbor + ArgoCD + GitLab）
	// preview 阶段不强制刷 ArgoCD 缓存（那是 submit 时才做），用 60s 内的快照即可，
	// 失败的模块前端展示在「未通过」分组，让用户决定跳过/取消
	precheck := runPrecheckForPreview(r.Context(), p, pending, modules)

	JSONSuccess(w, map[string]interface{}{
		"diff":        diff,
		"env_type":    p.EnvType,
		"auto_sync":   p.AutoSync,
		"total_count": len(diff),
		"precheck":    precheck, // {passed:[], failed:[], argocd_cache_at}
	})
}

// pullForPrecheck 在 precheck 之前刷新 git_cache，让 GitLab 校验吃到最新 chart 目录。
//
//	blocking=true: 提交流程，必须等到锁拿到再 pull —— 哪怕慢一点也要权威；
//	blocking=false: 预览流程，抢不到锁立刻放弃 pull（用旧 cache 校验），不让用户白等。
//	pull 失败本身不阻塞 precheck —— 旧 cache 校验比直接报错好。
//	GitLab 这边只是 git fetch，对 GitLab/ArgoCD 都不构成压力（毫秒级流量）。
func pullForPrecheck(ctx context.Context, p *models.ProjectEnv, blocking bool) {
	gs := getGitService()
	var got bool
	if blocking {
		gs.Locks.Acquire(p.Name)
		got = true
	} else {
		got = gs.Locks.TryAcquire(p.Name, 3*time.Second)
	}
	if !got {
		log.Printf("[precheck-pull] skip: lock busy on %s (using cached chart dir)", p.Name)
		return
	}
	defer gs.Locks.Release(p.Name)
	gitCtx, cancel := services.GitCtx(ctx, 60)
	defer cancel()
	if err := gs.EnsureClone(gitCtx, p.Name, p.GitRepo, p.GitBranch); err != nil {
		log.Printf("[precheck-pull] %s: %v (using cached chart dir)", p.Name, err)
	}
}

// runPrecheckForPreview preview 阶段调用：
//
//	1. 不强刷 ArgoCD 缓存（避免每次 preview 都打 ArgoCD）
//	2. Harbor 校验仅在 verify_on_submit=true 时跑
//	3. 进 precheck 前 try-pull git_cache（抢不到锁就吃旧 cache，不阻塞 UI）
func runPrecheckForPreview(ctx context.Context, p *models.ProjectEnv, pending map[string]string,
	modules map[string]services.Module) services.PrecheckBundle {
	pullForPrecheck(ctx, p, false)
	hc, verify := LoadGlobalConfigForPrecheck()
	if !verify {
		hc = nil // verify off → 不调 Harbor
	}
	var argoClient *services.ArgocdClient
	if argoURL, argoToken, err := ResolveArgocdForEnv(p); err == nil && argoURL != "" {
		argoClient = services.NewArgocdClient(argoURL, argoToken)
	}
	items := make([]services.PrecheckRequestItem, 0, len(pending))
	for name, newTag := range pending {
		m := modules[name]
		items = append(items, services.PrecheckRequestItem{
			Module:          name,
			ArgocdAppName:   m.ArgocdApp, // 用 DB 里 scan 出来的真值，避免 g32 这种 helm 模板用 env 后缀的项目对不上
			NewTag:          newTag,
			OldTag:          m.CurrentTag,
			ImageRepository: m.ImageRepository,
		})
	}
	return services.RunPrecheck(ctx, services.PrecheckInput{
		ProjectEnvName:  p.Name,
		GitCacheRoot:    Cfg.GitCacheDir,
		ChartBasePath:   p.ChartBasePath,
		ProjectEnvLower: strings.ToLower(p.Name),
		HarborClient:    hc,
		ArgocdClient:    argoClient,
		ArgocdCache:     services.GetArgocdAppCache(),
		Items:           items,
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

	// 把改动详情 (module → 新 tag) 全部写进审计；前端展开能看到具体改了哪几个模块
	auditChanges := make([]map[string]string, 0, len(pending))
	for name, tag := range pending {
		auditChanges = append(auditChanges, map[string]string{"module": name, "new_tag": tag})
	}
	Audit(r, auditEvent, "project_env", p.Name, map[string]interface{}{
		"deployment_id":     depID,
		"env_type":          p.EnvType,
		"modules_count":     len(pending),
		"changes":           auditChanges,
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
	// 🔒 PROD 重启 = 写操作 → 跟 submit_prod 一起守门，跟 HandleUpdateImage 对齐
	// （Gate 1 env 白名单是第一道；这是第二道 defense-in-depth）
	if p.EnvType == models.EnvPROD && !HasButton(r, "submit_prod") {
		JSONError(w, 40300, "PROD 重启需要 submit_prod 权限")
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

	gitRetry, pollInterval, pollTimeoutMin := loadPollCfg()
	InflightTrack(func() {
		defer ReleaseModuleLocks(p.Name, req.ModuleNames)
		start := time.Now()
		// 单 app 动态 timeout 上限是 30 分钟（services.timeoutCapSec），父 ctx 留 35 分钟够用
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		// 注册到 cancel 表：用户点「取消等待」时调 cancel() 让 PollUntilStable 立刻退出
		RegisterCancel(depID, cancel)
		defer UnregisterCancel(depID)
		ds := services.NewDeployService(getGitService())
		res := ds.Restart(ctx, services.RestartInput{
			ProjectEnvName:  p.Name,
			Namespace:       p.Namespace,
			Modules:         modules,
			ModuleNames:     req.ModuleNames,
			GitRepo:         p.GitRepo,
			GitBranch:       p.GitBranch,
			GitRetry:        gitRetry,
			Operator:        operator,
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
		errMsg := ""
		if res.Err != nil {
			errMsg = res.Err.Error()
		}
		_, _ = database.DB.Exec(
			`UPDATE deployment SET argocd_results=?, git_commit=?, git_commit_url=?, status=?, error_msg=?, duration_sec=? WHERE id=?`,
			argoJSON, res.GitCommit, res.GitCommitURL, res.Status, errMsg,
			int(time.Since(start).Seconds()), depID)
		ArchiveFailedPodLogs(depID, p, collectFailingPodsByApp(res.ArgocdResults))
		sendRestartNotify(p, depID, operator, modules, res)
	})

	Audit(r, "deploy.restart", "project_env", p.Name, map[string]interface{}{
		"deployment_id": depID,
		"env_type":      p.EnvType,
		"modules_count": len(req.ModuleNames),
		"module_names":  req.ModuleNames,
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
	// 🔒 PROD 回滚 = 写操作 → 跟 submit_prod 一起守门，跟 HandleUpdateImage 对齐
	// （Gate 1 env 白名单是第一道；这是第二道 defense-in-depth）
	if p.EnvType == models.EnvPROD && !HasButton(r, "submit_prod") {
		JSONError(w, 40300, "PROD 回滚需要 submit_prod 权限")
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

	rollbackChanges := make([]map[string]string, 0, len(pending))
	for name, tag := range pending {
		rollbackChanges = append(rollbackChanges, map[string]string{"module": name, "new_tag": tag})
	}
	Audit(r, "deploy.rollback", "project_env", p.Name, map[string]interface{}{
		"deployment_id":     depID,
		"ref_deployment_id": ref,
		"modules_count":     len(pending),
		"changes":           rollbackChanges,
		"selected_modules":  req.SelectedModules,
	})
	JSONSuccess(w, map[string]interface{}{
		"deployment_id": depID,
		"status":        "pending",
	})
}

// --- helpers ---

func loadModulesMap(projectEnvID int64, chartBasePath string) map[string]services.Module {
	rows, err := database.DB.Query(`SELECT name, current_tag, argocd_app_name, IFNULL(namespace,''), IFNULL(image_repository,'') FROM module WHERE project_env_id=?`, projectEnvID)
	if err != nil {
		return map[string]services.Module{}
	}
	defer rows.Close()
	out := map[string]services.Module{}
	for rows.Next() {
		var name, tag, app, ns, imgRepo string
		_ = rows.Scan(&name, &tag, &app, &ns, &imgRepo)
		out[name] = services.Module{
			Name:            name,
			CurrentTag:      tag,
			ChartRelPath:    services.BuildValuesPath(chartBasePath, name),
			ArgocdApp:       app,
			Namespace:       ns,
			ImageRepository: imgRepo,
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

// preflightFilterPending submit 前实时跑三方预检，把不通过的模块从 pending map 里剔除
//
//	返回被剔除的项（含失败原因）；调用方负责把信息写到 deployment.error_msg
//	verify_on_submit=false 时跳过 Harbor 校验（仍校验 ArgoCD + GitLab）
//	提交前必须 git pull —— 阻塞式拿锁，确保校验吃到最新的 chart 目录
func preflightFilterPending(ctx context.Context, p *models.ProjectEnv,
	pending map[string]string, modules map[string]services.Module) []services.PrecheckItem {

	pullForPrecheck(ctx, p, true)
	hc, verify := LoadGlobalConfigForPrecheck()
	if !verify {
		hc = nil
	}
	var argoClient *services.ArgocdClient
	if argoURL, argoToken, err := ResolveArgocdForEnv(p); err == nil && argoURL != "" {
		argoClient = services.NewArgocdClient(argoURL, argoToken)
	}
	items := make([]services.PrecheckRequestItem, 0, len(pending))
	for name, newTag := range pending {
		m := modules[name]
		items = append(items, services.PrecheckRequestItem{
			Module:          name,
			ArgocdAppName:   m.ArgocdApp,
			NewTag:          newTag,
			OldTag:          m.CurrentTag,
			ImageRepository: m.ImageRepository,
		})
	}
	bundle := services.RunPrecheck(ctx, services.PrecheckInput{
		ProjectEnvName:  p.Name,
		GitCacheRoot:    Cfg.GitCacheDir,
		ChartBasePath:   p.ChartBasePath,
		ProjectEnvLower: strings.ToLower(p.Name),
		HarborClient:    hc,
		ArgocdClient:    argoClient,
		ArgocdCache:     services.GetArgocdAppCache(),
		Items:           items,
	})
	for _, it := range bundle.Failed {
		delete(pending, it.Module)
	}
	return bundle.Failed
}

// runUpdateImageAsync 后台跑完整的发布流水线，update-image 和 rollback 共用
//
//	opLabel: Lark 通知标题用 —— "发布" 或 "回滚"
func runUpdateImageAsync(depID int64, p *models.ProjectEnv, pending map[string]string, modules map[string]services.Module,
	gitRetry, pollInterval, pollTimeoutMin int, _refDepID *int64, operator, opLabel string) {
	start := time.Now()
	// 单 app 动态 timeout 上限是 30 分钟（services.timeoutCapSec），父 ctx 留 35 分钟够用
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	// 注册到 cancel 表：用户点「取消等待」时调 cancel() 让 PollUntilStable 立刻退出
	RegisterCancel(depID, cancel)
	defer UnregisterCancel(depID)

	// 三方一致性预检（submit 前实时校验，过滤掉 Harbor / ArgoCD / GitLab 缺失的模块）
	// 走全局开关 verify_on_submit；关闭时跳过 Harbor 校验，但 ArgoCD/GitLab 仍校验
	skippedItems := preflightFilterPending(ctx, p, pending, modules)
	if len(skippedItems) > 0 {
		// 把跳过的模块写进 deployment 错误信息，供发布历史展示
		msgLines := []string{"已跳过的模块（未通过预检）："}
		for _, it := range skippedItems {
			msgLines = append(msgLines, "  · "+it.Module+" → "+it.Reason)
		}
		_, _ = database.DB.Exec(
			`UPDATE deployment SET error_msg=? WHERE id=?`,
			strings.Join(msgLines, "\n"), depID)
	}
	if len(pending) == 0 {
		// 全部被过滤完了 → 终止流水线
		_, _ = database.DB.Exec(
			`UPDATE deployment SET status=?, duration_sec=? WHERE id=?`,
			models.StatusFailed, int(time.Since(start).Seconds()), depID)
		return
	}

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

	ArchiveFailedPodLogs(depID, p, collectFailingPodsByApp(res.ArgocdResults))
	sendUpdateImageNotify(p, depID, operator, opLabel, modules, res)
}

// collectFailingPodsByApp 把 ArgocdResults 里的 FailingPods 快照按 app 分组
//
//	成功 app 的 FailingPods 为空 → 不进 map → archive 跳过（用户要求：成功不归档）
//	只有 fail 路径才有 FailingPods（poll_service 在 Degraded/Phase=Failed/timeout/pod-fatal 时填充）
func collectFailingPodsByApp(results []models.ArgocdAppResult) map[string][]models.FailingPodSnapshot {
	out := map[string][]models.FailingPodSnapshot{}
	for _, r := range results {
		if len(r.FailingPods) == 0 {
			continue
		}
		out[r.App] = r.FailingPods
	}
	return out
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
		title, color, body := buildDeployNotifyBody(opLabel, operator, atID, s.kind, p.Name, p.EnvType, s.items)
		// 用带重试版：3 次 attempt，每次 10s + 1s/2s 退避，外层留 40s 总预算
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		err := services.SendLarkCardWithRetry(ctx, webhook, secret, title, body, color, linkLabel, linkURL)
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
