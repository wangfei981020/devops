package handlers

import (
	"context"
	"fmt"
	"io"
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
			return
		}
	}
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
