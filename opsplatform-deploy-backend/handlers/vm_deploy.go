package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// =========================================================================
//   VM 部署 handler
//
// 三类动作：
//   - rsync          → 同步代码到 ansible 服务器（不改目标 VM）
//   - update_version → 真正部署到目标 VM
//   - cancel         → 取消进行中的任务
//
// 跟 K8s 路径不一样的几个点：
//   - 不需要 git pull chart 仓库（agent 自己每次任务前 git pull 整个 ansible_cicd）
//   - 不做 fail-fast pod 检测（ansible exit code 直接表示成败）
//   - 没归档 logs/events（jenkins console 历史够用；后续可加）
// =========================================================================

type vmDeployReq struct {
	VmProjectEnvID int64  `json:"vm_project_env_id"`
	Service        string `json:"service"`           // service 名
	Action         string `json:"action"`            // "rsync" | "update_version"
	Version        string `json:"version,omitempty"` // update_version 必填
}

// POST /api/deploy/vm-run
func HandleVmDeploy(w http.ResponseWriter, r *http.Request) {
	if IsDraining() {
		JSONError(w, 50300, "service is draining (rolling update), please retry in a moment")
		return
	}
	var req vmDeployReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.VmProjectEnvID == 0 || req.Service == "" || req.Action == "" {
		JSONError(w, 40001, "vm_project_env_id / service / action 必填")
		return
	}
	if req.Action != "rsync" && req.Action != "update_version" {
		JSONError(w, 40001, "action 只支持 rsync / update_version")
		return
	}
	if req.Action == "update_version" && req.Version == "" {
		JSONError(w, 40001, "update_version 必须传 version")
		return
	}

	// 数据级权限：用户对此 VM env 有访问权
	if !IsVmEnvIDAllowed(r, req.VmProjectEnvID) {
		JSONError(w, 40300, "无权操作该 VM 环境")
		return
	}

	v, err := loadVmProjectEnv(req.VmProjectEnvID)
	if err != nil {
		JSONError(w, 40400, "vm_project_env not found")
		return
	}
	agent, err := LoadDeployAgentDecrypted(v.AgentID)
	if err != nil {
		JSONError(w, 40001, "agent not configured: "+err.Error())
		return
	}

	// 权限校验：复用 K8s 那套按钮权限
	//   rsync = 类似 scan_modules，要求 submit_uat/prod
	//   update_version = 实际发布，按 env_type 检查 submit_uat/submit_prod
	//   LPT 业务等同 UAT，复用 submit_uat
	if req.Action == "update_version" {
		if v.EnvType == "PROD" && !HasButton(r, "submit_prod") {
			JSONError(w, 40300, "没有 PROD 发布权限（submit_prod）")
			return
		}
		if v.EnvType != "PROD" && !HasButton(r, "submit_uat") {
			JSONError(w, 40300, "没有 UAT 发布权限（submit_uat）")
			return
		}
	}

	// insert deployment 占位行（target_type='vm'）
	operator := getOperator(r)
	modNamesJSON := marshalJSON([]string{req.Service})
	depRes, err := database.DB.Exec(
		`INSERT INTO deployment (project_env_id, action, module_names, operator, status, target_type)
		 VALUES (?, ?, ?, ?, 'pending', 'vm')`,
		v.ID, // 复用 project_env_id 列存 vm_project_env_id（前端按 target_type 判断怎么解读）
		"vm_"+req.Action, modNamesJSON, operator)
	if err != nil {
		InternalErr(w, r, fmt.Errorf("insert deployment: %w", err))
		return
	}
	depID, _ := depRes.LastInsertId()

	// 调 agent 触发任务
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	submitCtx, cancelSubmit := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancelSubmit()
	taskID, err := cli.SubmitTask(submitCtx, services.AgentTaskSpec{
		Action:  req.Action,
		Project: v.ProjectCode,
		Env:     v.EnvType,
		Service: req.Service,
		Version: req.Version,
	})
	if err != nil {
		_, _ = database.DB.Exec(`UPDATE deployment SET status='failed', error_msg=? WHERE id=?`, err.Error(), depID)
		log.Printf("[vm-deploy] dep=%d submit to agent failed: %v", depID, err)
		JSONError(w, 50001, "提交 agent 失败，详情见服务端日志")
		return
	}
	_, _ = database.DB.Exec(
		`UPDATE deployment SET agent_task_id=?, status='pending' WHERE id=?`,
		taskID, depID)

	// 异步 goroutine 轮询 agent 任务状态，结束时回写 deployment status
	// 用 cancelable ctx 注册到 inflight 注册表，让 graceful drain 能等它跑完
	pollCtx, pollCancel := context.WithCancel(context.Background())
	RegisterCancel(depID, pollCancel)
	go pollVmTask(pollCtx, depID, agent, taskID, req, v, operator)

	Audit(r, "deploy.vm_run", "vm_project_env", v.Name, map[string]interface{}{
		"deployment_id": depID,
		"agent_task_id": taskID,
		"action":        req.Action,
		"service":       req.Service,
		"version":       req.Version,
	})
	log.Printf("[vm-deploy] dep=%d operator=%s env=%s service=%s action=%s task=%s submitted",
		depID, operator, v.Name, req.Service, req.Action, taskID)
	JSONSuccess(w, map[string]interface{}{
		"deployment_id": depID,
		"task_id":       taskID,
		"agent_url":     agent.URL,
	})
}

// pollVmTask 轮询 agent 任务，2 秒一次直到终态或超时（30 分钟）。
//
//	parentCtx 由调用方传入，绑到 inflight 注册表 → 用户点取消时 ctx.Done() 唤醒；
//	本函数内部再 wrap 一层 30m 超时，避免无限等待 agent。
//	v / operator 用于终态时发 Lark 通知。
func pollVmTask(parentCtx context.Context, depID int64, agent *models.DeployAgent, taskID string, req vmDeployReq, v *models.VmProjectEnv, operator string) {
	defer UnregisterCancel(depID) // inflight 释放

	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Minute)
	defer cancel()
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	start := time.Now()

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	var status, errMsg string
	for {
		select {
		case <-ctx.Done():
			// 区分原因：parentCtx 被用户取消 vs 30m 超时
			duration := int(time.Since(start).Seconds())
			if parentCtx.Err() != nil {
				// 用户取消：调 agent 真正杀进程，状态写 canceled
				cancelCtx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
				_ = cli.CancelTask(cancelCtx, taskID)
				cancelFn()
				status, errMsg = "canceled", "用户取消"
				_, _ = database.DB.Exec(
					`UPDATE deployment SET status=?, error_msg=?, duration_sec=? WHERE id=?`,
					status, errMsg, duration, depID)
				log.Printf("[vm-deploy] dep=%d canceled by user (task=%s)", depID, taskID)
			} else {
				status, errMsg = "failed", "poll timeout (>30m)"
				_, _ = database.DB.Exec(
					`UPDATE deployment SET status=?, error_msg=?, duration_sec=? WHERE id=?`,
					status, errMsg, duration, depID)
				log.Printf("[vm-deploy] dep=%d poll timeout (>30m, task=%s)", depID, taskID)
			}
			finalizeVmDeployment(depID, status, errMsg, duration, req, agent, taskID, v, operator)
			return
		case <-tick.C:
			t, err := cli.GetTask(ctx, taskID)
			if err != nil {
				continue
			}
			if t.Status == "running" || t.Status == "pending" {
				continue
			}
			// 终态：写回 deployment
			status = "success"
			if t.Status == "failed" {
				status = "failed"
			} else if t.Status == "canceled" {
				status = "canceled"
			}
			errMsg = t.ErrMsg
			duration := int(time.Since(start).Seconds())
			_, _ = database.DB.Exec(
				`UPDATE deployment SET status=?, error_msg=?, duration_sec=? WHERE id=?`,
				status, errMsg, duration, depID)
			log.Printf("[vm-deploy] dep=%d done status=%s duration=%ds (task=%s)", depID, status, duration, taskID)
			finalizeVmDeployment(depID, status, errMsg, duration, req, agent, taskID, v, operator)
			return
		}
	}
}

// finalizeVmDeployment 终态共享收尾：写 vm_service.current_version、归档日志、发 Lark
func finalizeVmDeployment(depID int64, status, errMsg string, duration int, req vmDeployReq, agent *models.DeployAgent, taskID string, v *models.VmProjectEnv, operator string) {
	// 成功 + update_version → 写 vm_service.current_version
	if status == "success" && req.Action == "update_version" {
		_, _ = database.DB.Exec(
			`UPDATE vm_service SET current_version=?
			 WHERE vm_project_env_id=? AND name=?`,
			req.Version, v.ID, req.Service)
	}
	// 不管成败，归档 ansible 日志到 MinIO（异步，失败不阻塞）
	go archiveVmTaskLog(depID, agent, taskID)
	// 发 Lark 通知（带 3 次重试，异步）
	go sendVmLarkNotify(depID, status, errMsg, duration, req, v, operator)
}

// sendVmLarkNotify 发送 VM 部署完成的 Lark 卡片（带 3 次重试）。
//
//	- 只在 vm_project_env 显式绑定了 lark_bot_id 时才发（跟 K8s 行为一致，无全局回落）
//	- @ 操作人：通过 contacts 表按 operator 名查 lark_id
//	- 卡片色按状态：success=green / canceled=orange / failed/no_change=red
//	- 失败/取消/超时整 3 次仍失败 → log + UPDATE deployment.lark_notify='failed'
func sendVmLarkNotify(depID int64, status, errMsg string, duration int, req vmDeployReq, v *models.VmProjectEnv, operator string) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[vm-lark] panic dep=%d: %v", depID, rec)
		}
	}()
	if v.LarkBotID == nil || *v.LarkBotID <= 0 {
		log.Printf("[vm-lark] skip dep=%d: no lark_bot_id on vm_project_env=%s", depID, v.Name)
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "skipped", depID)
		return
	}
	bot, err := LoadLarkBotDecrypted(*v.LarkBotID)
	if err != nil {
		log.Printf("[vm-lark] dep=%d load bot failed: %v", depID, err)
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "failed", depID)
		return
	}

	// @ 操作人
	atID := LookupContactLarkID(operator)

	// 标题 + 颜色
	actLabel := "VM 部署"
	if req.Action == "rsync" {
		actLabel = "VM 同步代码"
	}
	var title, color string
	switch status {
	case "success":
		title = fmt.Sprintf("✅ %s成功 · %s", actLabel, req.Service)
		color = "green"
	case "canceled":
		title = fmt.Sprintf("⏹ %s已取消 · %s", actLabel, req.Service)
		color = "orange"
	default:
		title = fmt.Sprintf("❌ %s失败 · %s", actLabel, req.Service)
		color = "red"
	}

	// 卡片正文：项目 / 环境 / 服务 / 版本 / 操作人 / 耗时 / 错误（失败时）
	var sb strings.Builder
	if atID != "" {
		sb.WriteString(fmt.Sprintf(`<at id="%s"></at>`+"\n\n", atID))
	}
	sb.WriteString(fmt.Sprintf("**项目环境**：`%s` · **%s**\n", v.Name, strings.ToUpper(v.EnvType)))
	sb.WriteString(fmt.Sprintf("**服务**：`%s`\n", req.Service))
	if req.Action == "update_version" && req.Version != "" {
		sb.WriteString(fmt.Sprintf("**版本**：`%s`\n", req.Version))
	}
	sb.WriteString(fmt.Sprintf("**操作人**：%s\n", operator))
	sb.WriteString(fmt.Sprintf("**耗时**：%ds\n", duration))
	if status != "success" && errMsg != "" {
		// 截断过长错误，避免卡片爆
		em := errMsg
		if len(em) > 500 {
			em = em[:500] + "..."
		}
		sb.WriteString(fmt.Sprintf("**错误**：\n```\n%s\n```\n", em))
	}

	// 链接按钮：发布详情页
	linkLabel, linkURL := "", ""
	if u := deployDetailURL(depID); u != "" {
		linkLabel, linkURL = "查看发布详情", u
	}

	// 发卡（带 3 次重试，外层 40s 总预算）
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := services.SendLarkCardWithRetry(ctx, bot.Webhook, bot.Secret, title, sb.String(), color, linkLabel, linkURL, atID); err != nil {
		log.Printf("[vm-lark] dep=%d send failed (after retries): %v", depID, err)
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "failed", depID)
		return
	}
	_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "success", depID)
}

// archiveVmTaskLog 把 agent 端的 logBuffer 全量拉回上传 MinIO，并把 object_key/size 写回 deployment 行。
//
//	不管 success / failed / canceled 都归档。MinIO 未配置时优雅跳过。
//	bucket 跟 K8s pod 日志共用同一个，lifecycle 自动过期；object key 路径 vm-logs/{depID}.log
func archiveVmTaskLog(depID int64, agent *models.DeployAgent, taskID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[vm-archive] panic dep=%d: %v", depID, r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	mc, err := loadMinIOClientFromDB()
	if err != nil {
		log.Printf("[vm-archive] load minio config: %v (skip dep=%d)", err, depID)
		return
	}
	if mc == nil {
		// 未配置，优雅跳过
		log.Printf("[vm-archive] minio not configured, skip dep=%d", depID)
		return
	}
	if err := mc.EnsureBucket(ctx); err != nil {
		log.Printf("[vm-archive] ensure bucket: %v (skip dep=%d)", err, depID)
		return
	}

	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	logs, _, err := cli.GetTaskLogs(ctx, taskID, 0)
	if err != nil {
		log.Printf("[vm-archive] get task logs from agent: %v (dep=%d)", err, depID)
		return
	}
	if logs == "" {
		// 任务可能根本没产出（agent 端日志已被 24h GC 清掉，或者任务一启动就崩没输出）
		log.Printf("[vm-archive] empty log content, skip dep=%d", depID)
		return
	}

	key := fmt.Sprintf("vm-logs/%d.log", depID)
	size, err := mc.PutLog(ctx, key, logs)
	if err != nil {
		log.Printf("[vm-archive] put log: %v (dep=%d)", err, depID)
		return
	}
	if _, err := database.DB.Exec(
		`UPDATE deployment SET vm_log_object_key=?, vm_log_size=? WHERE id=?`,
		key, int(size), depID); err != nil {
		log.Printf("[vm-archive] update deployment: %v (dep=%d)", err, depID)
		return
	}
	log.Printf("[vm-archive] dep=%d archived %d bytes → %s", depID, size, key)
}

// GET /api/deployments/{id}/vm-logs?since=<offset>&stream=true
//
//	代理 agent 的 SSE 流。前端打开历史「查看日志」时调这个，跟 K8s 那套统一。
func HandleVmDeployLogs(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])

	var taskID string
	var peID int64
	err := database.DB.QueryRow(
		`SELECT agent_task_id, project_env_id FROM deployment WHERE id=? AND target_type='vm'`,
		depID).Scan(&taskID, &peID)
	if err != nil {
		JSONError(w, 40400, "vm deployment not found")
		return
	}
	if taskID == "" {
		JSONError(w, 40001, "deployment 没有 agent task id（任务可能创建失败）")
		return
	}

	// 数据级权限
	if !IsVmEnvIDAllowed(r, peID) {
		JSONError(w, 40300, "无权查看该 VM 环境的日志")
		return
	}

	v, err := loadVmProjectEnv(peID)
	if err != nil {
		JSONError(w, 40404, "vm_project_env not found")
		return
	}
	agent, err := LoadDeployAgentDecrypted(v.AgentID)
	if err != nil {
		JSONError(w, 40404, "agent not found")
		return
	}

	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	stream := r.URL.Query().Get("stream") == "true"

	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	if !stream {
		logs, total, err := cli.GetTaskLogs(r.Context(), taskID, since)
		if err != nil {
			InternalErr(w, r, fmt.Errorf("agent get task logs (dep=%d): %w", depID, err))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Log-Total", strconv.Itoa(total))
		_, _ = w.Write([]byte(logs))
		return
	}

	// SSE：把 agent 那边的 SSE 直接 pipe 给前端
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("[vm-logs] dep=%d streaming unsupported by ResponseWriter type", depID)
		JSONError(w, 50001, "streaming not supported by current connection")
		return
	}
	pw := &flushingWriter{w: w, flusher: flusher}
	_ = cli.StreamTaskLogs(r.Context(), taskID, since, pw)
}

// POST /api/deployments/{id}/vm-cancel
func HandleVmDeployCancel(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])
	if !cancelVmDeploymentByID(w, r, depID) {
		// helper 已写好错误响应，直接返
		return
	}
	Audit(r, "deploy.vm_cancel", "deployment", strconv.FormatInt(depID, 10), nil)
	log.Printf("[vm-cancel] dep=%d operator=%s canceled", depID, getOperator(r))
	JSONSuccess(w, map[string]interface{}{"canceled": true})
}

// cancelVmDeploymentByID 共享实现：HandleVmDeployCancel 和 HandleCancelDeployment（VM 分支）都调它。
//
//	做的事：① 数据级权限 ② 调 agent CancelTask 杀进程 ③ inflight 释放（让 pollVmTask 退出）
//	状态写回 'canceled' + duration 由 pollVmTask 的 ctx.Done() 分支统一负责，避免双写撞 race
//	返回 true = 成功；false = 已写错误响应，调用方不要再写
func cancelVmDeploymentByID(w http.ResponseWriter, r *http.Request, depID int64) bool {
	var taskID string
	var peID int64
	err := database.DB.QueryRow(
		`SELECT agent_task_id, project_env_id FROM deployment WHERE id=? AND target_type='vm'`,
		depID).Scan(&taskID, &peID)
	if err != nil {
		JSONError(w, 40400, "vm deployment not found")
		return false
	}
	if !IsVmEnvIDAllowed(r, peID) {
		JSONError(w, 40300, "无权操作该 VM 环境")
		return false
	}
	v, err := loadVmProjectEnv(peID)
	if err != nil {
		JSONError(w, 40404, "vm_project_env not found")
		return false
	}
	agent, err := LoadDeployAgentDecrypted(v.AgentID)
	if err != nil {
		JSONError(w, 40404, "agent not found")
		return false
	}
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	ctx, cancelCtx := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancelCtx()
	if err := cli.CancelTask(ctx, taskID); err != nil {
		InternalErr(w, r, fmt.Errorf("agent cancel task (dep=%d task=%s): %w", depID, taskID, err))
		return false
	}
	// 通知 pollVmTask 走 ctx.Done() 分支：写 status='canceled' + duration + Lark + 归档
	CancelDeployment(depID)
	return true
}

// GET /api/deployments/{id}/vm-archived-log
//
//	返回 MinIO 里归档的 ansible 完整日志（成败都归档）。任务终态后才有；
//	pending/running 期间走 /vm-logs?stream=true 实时流。
func HandleVmArchivedLog(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])

	var status, objectKey string
	var size int
	var peID int64
	err := database.DB.QueryRow(
		`SELECT IFNULL(status,''), IFNULL(vm_log_object_key,''), IFNULL(vm_log_size,0), project_env_id
		   FROM deployment
		  WHERE id=? AND target_type='vm'`,
		depID).Scan(&status, &objectKey, &size, &peID)
	if err != nil {
		JSONError(w, 40400, "vm deployment not found")
		return
	}
	// 数据级权限
	if !IsVmEnvIDAllowed(r, peID) {
		JSONError(w, 40300, "无权查看该 VM 环境的日志")
		return
	}
	if objectKey == "" {
		// 归档不可用的几种情况，分别给清晰提示
		switch status {
		case "pending":
			JSONError(w, 40901, "任务进行中，去部署控制台看实时日志")
		case "":
			JSONError(w, 40901, "任务还没开始，无日志")
		default:
			// 终态但没归档：可能 MinIO 未配置 / agent 端日志已 GC / 上传失败
			JSONError(w, 40402, "该任务没有归档日志（可能未配置 MinIO 或归档时 agent 已 GC 日志）")
		}
		return
	}

	mc, err := loadMinIOClientFromDB()
	if err != nil {
		InternalErr(w, r, fmt.Errorf("load minio config: %w", err))
		return
	}
	if mc == nil {
		JSONError(w, 50002, "MinIO 未配置，归档日志读不出来（去系统设置配 MinIO）")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	content, err := mc.GetLog(ctx, objectKey)
	if err != nil {
		InternalErr(w, r, fmt.Errorf("minio get %s (dep=%d): %w", objectKey, depID, err))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Log-Size", strconv.Itoa(size))
	w.Header().Set("X-Log-Status", status)
	_, _ = w.Write([]byte(content))
}

// flushingWriter 在每次 Write 后触发 HTTP flush，让 SSE 数据立刻到前端
type flushingWriter struct {
	w       io.Writer
	flusher http.Flusher
}

func (f *flushingWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err == nil {
		f.flusher.Flush()
	}
	return n, err
}

// 用一下 fmt 防止 unused
var _ = fmt.Sprintf
