package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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
		JSONError(w, 50000, "insert deployment: "+err.Error())
		return
	}
	depID, _ := depRes.LastInsertId()

	// 调 agent 触发任务
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	taskID, err := cli.SubmitTask(ctx, services.AgentTaskSpec{
		Action:  req.Action,
		Project: v.ProjectCode,
		Env:     v.EnvType,
		Service: req.Service,
		Version: req.Version,
	})
	if err != nil {
		_, _ = database.DB.Exec(`UPDATE deployment SET status='failed', error_msg=? WHERE id=?`, err.Error(), depID)
		JSONError(w, 50000, "agent: "+err.Error())
		return
	}
	_, _ = database.DB.Exec(
		`UPDATE deployment SET agent_task_id=?, status='pending' WHERE id=?`,
		taskID, depID)

	// 异步 goroutine 轮询 agent 任务状态，结束时回写 deployment status
	go pollVmTask(depID, agent, taskID, req)

	Audit(r, "deploy.vm_run", "vm_project_env", v.Name, map[string]interface{}{
		"deployment_id": depID,
		"agent_task_id": taskID,
		"action":        req.Action,
		"service":       req.Service,
		"version":       req.Version,
	})
	JSONSuccess(w, map[string]interface{}{
		"deployment_id": depID,
		"task_id":       taskID,
		"agent_url":     agent.URL,
	})
}

// pollVmTask 轮询 agent 任务，2 秒一次直到终态或超时（30 分钟）
func pollVmTask(depID int64, agent *models.DeployAgent, taskID string, req vmDeployReq) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	start := time.Now()

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			_, _ = database.DB.Exec(
				`UPDATE deployment SET status='failed', error_msg='poll timeout (>30m)', duration_sec=? WHERE id=?`,
				int(time.Since(start).Seconds()), depID)
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
			status := "success"
			if t.Status == "failed" {
				status = "failed"
			} else if t.Status == "canceled" {
				status = "canceled"
			}
			errMsg := t.ErrMsg
			duration := int(time.Since(start).Seconds())
			_, _ = database.DB.Exec(
				`UPDATE deployment SET status=?, error_msg=?, duration_sec=? WHERE id=?`,
				status, errMsg, duration, depID)
			// 成功 + update_version → 写 module.current_version
			if status == "success" && req.Action == "update_version" {
				_, _ = database.DB.Exec(
					`UPDATE vm_service SET current_version=?
					 WHERE vm_project_env_id=(SELECT project_env_id FROM deployment WHERE id=?)
					 AND name=?`,
					req.Version, depID, req.Service)
			}
			// 不管成败，归档 ansible 日志到 MinIO（异步，失败不阻塞 deployment 写回）
			go archiveVmTaskLog(depID, agent, taskID)
			return
		}
	}
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
			JSONError(w, 50000, "agent: "+err.Error())
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
		JSONError(w, 50000, "streaming unsupported")
		return
	}
	pw := &flushingWriter{w: w, flusher: flusher}
	_ = cli.StreamTaskLogs(r.Context(), taskID, since, pw)
}

// POST /api/deployments/{id}/vm-cancel
func HandleVmDeployCancel(w http.ResponseWriter, r *http.Request) {
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
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := cli.CancelTask(ctx, taskID); err != nil {
		JSONError(w, 50000, "agent: "+err.Error())
		return
	}
	Audit(r, "deploy.vm_cancel", "deployment", strconv.FormatInt(depID, 10), nil)
	JSONSuccess(w, map[string]interface{}{"canceled": true})
}

// GET /api/deployments/{id}/vm-archived-log
//
//	返回 MinIO 里归档的 ansible 完整日志（成败都归档）。任务终态后才有；
//	pending/running 期间走 /vm-logs?stream=true 实时流。
func HandleVmArchivedLog(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])

	var status, objectKey string
	var size int
	err := database.DB.QueryRow(
		`SELECT IFNULL(status,''), IFNULL(vm_log_object_key,''), IFNULL(vm_log_size,0)
		   FROM deployment
		  WHERE id=? AND target_type='vm'`,
		depID).Scan(&status, &objectKey, &size)
	if err != nil {
		JSONError(w, 40400, "vm deployment not found")
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
	if err != nil || mc == nil {
		JSONError(w, 50000, "MinIO 不可用，归档日志读不出来")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	content, err := mc.GetLog(ctx, objectKey)
	if err != nil {
		JSONError(w, 50000, "minio get: "+err.Error())
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
