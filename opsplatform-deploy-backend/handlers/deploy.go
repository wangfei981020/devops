package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	ProjectEnvID int64               `json:"project_env_id"`
	Changes      []map[string]string `json:"changes"` // [{module,tag}, ...]
}

func HandleUpdateImage(w http.ResponseWriter, r *http.Request) {
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
	if p.EnvType == models.EnvPROD && !IsAdmin(r) {
		JSONError(w, 40300, "PROD 发布仅限管理员")
		return
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
	depID, err := insertPendingDeployment(req.ProjectEnvID, models.ActionUpdateImage, nil, modNamesJSON, getOperator(r))
	if err != nil {
		JSONError(w, 50000, "insert deployment: "+err.Error())
		return
	}

	go runUpdateImageAsync(depID, p, pending, modules, retry, interval, timeoutMin, nil, getOperator(r))

	Audit(r, "deploy.update_image", "project_env", p.Name, map[string]interface{}{
		"deployment_id": depID, "env_type": p.EnvType, "modules": len(pending),
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
	if p.EnvType == models.EnvPROD && !IsAdmin(r) {
		JSONError(w, 40300, "PROD 重启仅限管理员")
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

	go func() {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ds := services.NewDeployService(nil)
		res := ds.Restart(ctx, services.RestartInput{
			ProjectEnvName: p.Name,
			Namespace:      p.Namespace,
			Modules:        modules,
			ModuleNames:    req.ModuleNames,
			ArgocdClient:   services.NewArgocdClient(argoURL, argoToken),
		})
		argoJSON := marshalJSON(res.ArgocdResults)
		_, _ = database.DB.Exec(`UPDATE deployment SET argocd_results=?, status=?, duration_sec=? WHERE id=?`,
			argoJSON, res.Status, int(time.Since(start).Seconds()), depID)
		sendRestartNotify(p, depID, operator, res)
	}()

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
		"modules":           out,
	})
}

type rollbackReq struct {
	RefDeploymentID int64    `json:"ref_deployment_id"`
	SelectedModules []string `json:"selected_modules"`
}

func HandleRollback(w http.ResponseWriter, r *http.Request) {
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
	if p.EnvType == models.EnvPROD && !IsAdmin(r) {
		JSONError(w, 40300, "PROD 回滚仅限管理员")
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

	go runUpdateImageAsync(depID, p, pending, modules, retry, interval, timeoutMin, &ref, getOperator(r))

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
func runUpdateImageAsync(depID int64, p *models.ProjectEnv, pending map[string]string, modules map[string]services.Module,
	gitRetry, pollInterval, pollTimeoutMin int, _refDepID *int64, operator string) {
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

	sendUpdateImageNotify(p, depID, operator, res)
}

// --- Lark notify ---

// resolveLarkTarget 解析出 webhook + 明文 secret
// 优先级: 1) p.LarkBotID → lark_bot 表  2) 遗留字段 p.LarkWebhook/Secret  3) global_config 默认
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
	var gw, gs string
	_ = database.DB.QueryRow(`SELECT lark_default_webhook, lark_default_secret FROM global_config WHERE id=1`).Scan(&gw, &gs)
	webhook = gw
	secret, _ = crypto.Decrypt(gs)
	return
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

func sendUpdateImageNotify(p *models.ProjectEnv, depID int64, operator string, res *services.UpdateImageResult) {
	webhook, secret := resolveLarkTarget(p)
	if webhook == "" {
		return
	}
	color, title := larkColorForStatus(res.Status)
	opDisplay := operator
	if operator != "" && operator != "system" {
		opDisplay = operator
	}
	body := fmt.Sprintf("**环境**: %s (%s)\n**模块数**: %d\n**提交**: %s\n**操作人**: %s",
		p.Name, p.EnvType, len(res.Changes), res.GitCommit, opDisplay)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	atID := LookupContactLarkID(operator)
	err := services.SendLarkCard(ctx, webhook, secret, title, body, color, "查看 commit", res.GitCommitURL, atID)
	status := "success"
	if err != nil {
		status = "failed"
	}
	_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, status, depID)
}

func sendRestartNotify(p *models.ProjectEnv, depID int64, operator string, res *services.RestartResult) {
	webhook, secret := resolveLarkTarget(p)
	if webhook == "" {
		return
	}
	color, title := larkColorForStatus(res.Status)
	title = "🔄 " + title + "（重启）"
	body := fmt.Sprintf("**环境**: %s (%s)\n**模块数**: %d\n**操作人**: %s",
		p.Name, p.EnvType, len(res.ArgocdResults), operator)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	atID := LookupContactLarkID(operator)
	err := services.SendLarkCard(ctx, webhook, secret, title, body, color, "", "", atID)
	status := "success"
	if err != nil {
		status = "failed"
	}
	_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, status, depID)
}
