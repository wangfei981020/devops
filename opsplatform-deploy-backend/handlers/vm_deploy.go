package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
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

// vmDeployItem 批量提交时单个 service 的请求项
type vmDeployItem struct {
	Service string `json:"service"`
	Version string `json:"version,omitempty"` // update_version 必填，rsync 不用
}

type vmDeployReq struct {
	VmProjectEnvID int64          `json:"vm_project_env_id"`
	Action         string         `json:"action"`             // "rsync" | "update_version"
	Services       []vmDeployItem `json:"services,omitempty"` // 推荐：批量列表，rsync 必须 1 个 / update_version 任意 N 个

	// 旧字段（向后兼容老前端调用）—— Normalize() 会包装成单元素 Services
	Service string `json:"service,omitempty"`
	Version string `json:"version,omitempty"`
}

// Normalize 把旧 `service`+`version` 形式归一化成 Services 数组
func (req *vmDeployReq) Normalize() {
	if len(req.Services) == 0 && req.Service != "" {
		req.Services = []vmDeployItem{{Service: req.Service, Version: req.Version}}
	}
}

// vmTaskMapEntry 是 deployment.vm_task_map JSON 里的单条记录。
//
//	跟 SubmitTask 一一对应；Status 由 pollVmTaskBatch 持续更新。
//	LogObjectKey / LogSize 在终态归档后填，前端"查看日志"按 service 找各自的 MinIO 对象。
type vmTaskMapEntry struct {
	Service       string `json:"service"`
	Version       string `json:"version,omitempty"`
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`              // pending / running / success / failed / canceled
	ErrMsg        string `json:"error_msg,omitempty"`
	LogObjectKey  string `json:"log_object_key,omitempty"`
	LogSize       int    `json:"log_size,omitempty"`
}

// POST /api/deploy/vm-run
//
//	支持批量：services 数组，每个独立 service+version。
//	rsync 必须只有 1 个 service（同步代码动作不需要批量）；update_version 任意 N 个。
//	旧 service+version 字段仍兼容（自动归一化成单元素 Services）。
func HandleVmDeploy(w http.ResponseWriter, r *http.Request) {
	if IsDraining() {
		JSONError(w, 50300, "service is draining (rolling update), please retry in a moment")
		return
	}
	var req vmDeployReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Normalize()

	if req.VmProjectEnvID == 0 || req.Action == "" || len(req.Services) == 0 {
		JSONError(w, 40001, "vm_project_env_id / action / services 必填（services 至少 1 项）")
		return
	}
	if req.Action != "rsync" && req.Action != "update_version" {
		JSONError(w, 40001, "action 只支持 rsync / update_version")
		return
	}
	// rsync 也支持批量：每个 service 独立 agent task（agent 端 rsync 本就是 per-service 跑 python 脚本）
	for i, it := range req.Services {
		if it.Service == "" {
			JSONError(w, 40001, fmt.Sprintf("services[%d].service 必填", i))
			return
		}
		if req.Action == "update_version" && it.Version == "" {
			JSONError(w, 40001, fmt.Sprintf("update_version 每个 service 必须传 version: %s", it.Service))
			return
		}
	}

	// 数据级权限
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

	// 按钮权限：仅 update_version 需要校验，rsync 不部署到 VM 没风险
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

	// 收集 module_names（前端 row 显示用）+ changes（发布历史 Tag 变更明细复用 K8s 逻辑）
	operator := getOperator(r)
	modNames := make([]string, len(req.Services))
	changes := make([]map[string]string, 0, len(req.Services))
	for i, it := range req.Services {
		modNames[i] = it.Service
		if req.Action == "update_version" {
			// from_tag 留空（VM 历史版本拿不到旧 commit hash）
			changes = append(changes, map[string]string{"module": it.Service, "to_tag": it.Version})
		}
	}
	modNamesJSON := marshalJSON(modNames)
	changesJSON := marshalJSON(changes)

	depRes, err := database.DB.Exec(
		`INSERT INTO deployment (project_env_id, action, module_names, changes, operator, status, target_type)
		 VALUES (?, ?, ?, ?, ?, 'pending', 'vm')`,
		v.ID, "vm_"+req.Action, modNamesJSON, changesJSON, operator)
	if err != nil {
		InternalErr(w, r, fmt.Errorf("insert deployment: %w", err))
		return
	}
	depID, _ := depRes.LastInsertId()

	// 给每个 service 提交 agent task；某个失败不影响其他（status='failed' 标记，继续）
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	taskMap := make([]vmTaskMapEntry, len(req.Services))
	for i, it := range req.Services {
		submitCtx, cancelSubmit := context.WithTimeout(r.Context(), 10*time.Second)
		taskID, terr := cli.SubmitTask(submitCtx, services.AgentTaskSpec{
			Action:  req.Action,
			Project: v.ProjectCode,
			Env:     v.EnvType,
			Service: it.Service,
			Version: it.Version,
		})
		cancelSubmit()
		if terr != nil {
			log.Printf("[vm-deploy] dep=%d service=%s submit failed: %v", depID, it.Service, terr)
			taskMap[i] = vmTaskMapEntry{
				Service: it.Service, Version: it.Version,
				TaskID: "", Status: "failed", ErrMsg: terr.Error(),
			}
			continue
		}
		taskMap[i] = vmTaskMapEntry{
			Service: it.Service, Version: it.Version,
			TaskID: taskID, Status: "pending",
		}
	}

	// 写回 vm_task_map + agent_task_id（首个，给老查询路径兼容）
	firstTaskID := ""
	for _, e := range taskMap {
		if e.TaskID != "" {
			firstTaskID = e.TaskID
			break
		}
	}
	taskMapBytes, _ := json.Marshal(taskMap)
	_, _ = database.DB.Exec(
		`UPDATE deployment SET agent_task_id=?, vm_task_map=?, status='pending' WHERE id=?`,
		firstTaskID, taskMapBytes, depID)

	// 全部 submit 都失败 → 直接终态写 failed，不起 poll 也不发 Lark（什么都没跑）
	allFailed := true
	for _, e := range taskMap {
		if e.Status != "failed" {
			allFailed = false
			break
		}
	}
	if allFailed {
		_, _ = database.DB.Exec(
			`UPDATE deployment SET status='failed', error_msg='all submit to agent failed' WHERE id=?`,
			depID)
		Audit(r, "deploy.vm_run", "vm_project_env", v.Name, map[string]interface{}{
			"deployment_id": depID,
			"action":        req.Action,
			"services":      modNames,
			"all_failed":    true,
		})
		JSONError(w, 50001, "全部 service 提交 agent 失败，详情见服务端日志")
		return
	}

	// 起批量 poll goroutine（受 inflight registry 控制，graceful drain 能等它跑完）
	pollCtx, pollCancel := context.WithCancel(context.Background())
	RegisterCancel(depID, pollCancel)
	go pollVmTaskBatch(pollCtx, depID, agent, taskMap, req, v, operator)

	Audit(r, "deploy.vm_run", "vm_project_env", v.Name, map[string]interface{}{
		"deployment_id": depID,
		"action":        req.Action,
		"services":      modNames,
		"count":         len(req.Services),
	})
	log.Printf("[vm-deploy] dep=%d operator=%s env=%s action=%s services=%v submitted",
		depID, operator, v.Name, req.Action, modNames)
	JSONSuccess(w, map[string]interface{}{
		"deployment_id": depID,
		"agent_url":     agent.URL,
		"task_count":    len(taskMap),
	})
}

// pollVmTaskBatch 轮询批量 agent 任务，2 秒一次直到所有 task 终态或超时（30 分钟）。
//
//	parentCtx 由调用方传入绑到 inflight 注册表 → 用户取消时 ctx.Done() 唤醒；
//	本函数内部再 wrap 一层 30m 超时。
//	taskMap 是 ENTRY 索引和 services 一一对应（submit 失败的 entry TaskID 为空，跳过 polling）。
//	每次 tick 把 taskMap 写回 deployment.vm_task_map，前端历史详情能实时看到每个 service 的进度。
func pollVmTaskBatch(parentCtx context.Context, depID int64, agent *models.DeployAgent, taskMap []vmTaskMapEntry, req vmDeployReq, v *models.VmProjectEnv, operator string) {
	defer UnregisterCancel(depID)
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[vm-poll-batch] panic dep=%d: %v", depID, rec)
		}
	}()

	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Minute)
	defer cancel()
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	start := time.Now()

	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			duration := int(time.Since(start).Seconds())
			if parentCtx.Err() != nil {
				// 用户取消：调 agent 杀掉所有还在 pending/running 的 task
				for i, e := range taskMap {
					if e.TaskID != "" && (e.Status == "pending" || e.Status == "running") {
						cancelCtx, cancelFn := context.WithTimeout(context.Background(), 5*time.Second)
						_ = cli.CancelTask(cancelCtx, e.TaskID)
						cancelFn()
						taskMap[i].Status = "canceled"
						if taskMap[i].ErrMsg == "" {
							taskMap[i].ErrMsg = "用户取消"
						}
					}
				}
				log.Printf("[vm-deploy] dep=%d canceled by user (%d tasks)", depID, len(taskMap))
			} else {
				// 30m 超时：所有未终态的 task 标 failed
				for i, e := range taskMap {
					if e.Status == "pending" || e.Status == "running" {
						taskMap[i].Status = "failed"
						taskMap[i].ErrMsg = "poll timeout (>30m)"
					}
				}
				log.Printf("[vm-deploy] dep=%d batch poll timeout (>30m)", depID)
			}
			aggStatus, aggErr := aggregateBatchStatus(taskMap)
			finalizeVmBatch(depID, aggStatus, aggErr, duration, taskMap, req, agent, v, operator)
			return

		case <-tick.C:
			allDone := true
			anyChanged := false
			for i, e := range taskMap {
				if e.Status == "success" || e.Status == "failed" || e.Status == "canceled" {
					continue // 已终态，跳过
				}
				if e.TaskID == "" {
					continue // submit 阶段就失败的，已经是 failed
				}
				t, err := cli.GetTask(ctx, e.TaskID)
				if err != nil {
					allDone = false
					continue
				}
				switch t.Status {
				case "pending", "running":
					if taskMap[i].Status != t.Status {
						taskMap[i].Status = t.Status
						anyChanged = true
					}
					allDone = false
				case "success", "failed", "canceled":
					taskMap[i].Status = t.Status
					taskMap[i].ErrMsg = t.ErrMsg
					anyChanged = true
				default:
					allDone = false // 未识别状态，下个 tick 再试
				}
			}
			if anyChanged {
				taskMapBytes, _ := json.Marshal(taskMap)
				_, _ = database.DB.Exec(`UPDATE deployment SET vm_task_map=? WHERE id=?`, taskMapBytes, depID)
			}
			if !allDone {
				continue
			}
			// 全部终态：聚合 + finalize
			duration := int(time.Since(start).Seconds())
			aggStatus, aggErr := aggregateBatchStatus(taskMap)
			log.Printf("[vm-deploy] dep=%d batch done status=%s duration=%ds (%d tasks)",
				depID, aggStatus, duration, len(taskMap))
			finalizeVmBatch(depID, aggStatus, aggErr, duration, taskMap, req, agent, v, operator)
			return
		}
	}
}

// aggregateBatchStatus 汇总各个 task 状态成单一 deployment.status：
//
//	全 success → success；全 failed → failed；全 canceled → canceled；混合 → partial
//	errMsg 取第一条 failed task 的错误用于 deployment.error_msg（K8s 同款逻辑）
func aggregateBatchStatus(taskMap []vmTaskMapEntry) (status, errMsg string) {
	successCnt, failedCnt, canceledCnt := 0, 0, 0
	var firstErr string
	for _, e := range taskMap {
		switch e.Status {
		case "success":
			successCnt++
		case "failed":
			failedCnt++
			if firstErr == "" {
				firstErr = e.Service + ": " + e.ErrMsg
			}
		case "canceled":
			canceledCnt++
		}
	}
	total := len(taskMap)
	switch {
	case successCnt == total:
		return "success", ""
	case failedCnt == total:
		return "failed", firstErr
	case canceledCnt == total:
		return "canceled", "all canceled"
	default:
		// 混合或个别非终态（理论不会到这里，做保底）
		return "partial", firstErr
	}
}

// finalizeVmBatch 终态共享收尾：写 deployment 终态 + 每个 success service 的 current_version
// + 异步归档每个 task 的 ansible 日志（per task → MinIO 各自一个文件）+ Lark 通知
func finalizeVmBatch(depID int64, status, errMsg string, duration int, taskMap []vmTaskMapEntry, req vmDeployReq, agent *models.DeployAgent, v *models.VmProjectEnv, operator string) {
	taskMapBytes, _ := json.Marshal(taskMap)
	_, _ = database.DB.Exec(
		`UPDATE deployment SET status=?, error_msg=?, duration_sec=?, vm_task_map=? WHERE id=?`,
		status, errMsg, duration, taskMapBytes, depID)

	// 成功 + update_version → 写每个成功 service 的 current_version
	if req.Action == "update_version" {
		for _, e := range taskMap {
			if e.Status == "success" && e.Version != "" {
				_, _ = database.DB.Exec(
					`UPDATE vm_service SET current_version=?
					 WHERE vm_project_env_id=? AND name=?`,
					e.Version, v.ID, e.Service)
			}
		}
	}

	// 异步归档每个 task 的 ansible 日志到 MinIO（per service 各自一个 object）
	go archiveVmBatchLogs(depID, agent, taskMap)
	// 发 Lark 通知（带 3 次重试，异步）；批量时正文展开多服务列表
	go sendVmLarkNotify(depID, status, errMsg, duration, req, taskMap, v, operator)
}

// sendVmLarkNotify 发送 VM 部署完成的 Lark 卡片（带 3 次重试）。
//
//	- 只在 vm_project_env 显式绑定了 lark_bot_id 时才发（跟 K8s 行为一致，无全局回落）
//	- @ 操作人：通过 contacts 表按 operator 名查 lark_id
//	- 卡片色按状态：success=green / canceled=orange / failed/no_change=red
//	- 失败/取消/超时整 3 次仍失败 → log + UPDATE deployment.lark_notify='failed'
func sendVmLarkNotify(depID int64, status, errMsg string, duration int, req vmDeployReq, taskMap []vmTaskMapEntry, v *models.VmProjectEnv, operator string) {
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

	atID := LookupContactLarkID(operator)

	actLabel := "VM 部署"
	if req.Action == "rsync" {
		actLabel = "VM 同步代码"
	}

	// 标题：根据 status + 服务数显示不同模板
	//   单服务：用首个 service 名
	//   批量：N 个服务，按 success/fail 计数
	successCnt, failedCnt, canceledCnt := 0, 0, 0
	for _, e := range taskMap {
		switch e.Status {
		case "success":
			successCnt++
		case "failed":
			failedCnt++
		case "canceled":
			canceledCnt++
		}
	}
	total := len(taskMap)
	servicesLabel := ""
	if total == 1 && len(taskMap) > 0 {
		servicesLabel = taskMap[0].Service
	} else {
		servicesLabel = fmt.Sprintf("%d 个服务", total)
	}
	var title, color string
	switch status {
	case "success":
		title = fmt.Sprintf("✅ %s成功 · %s", actLabel, servicesLabel)
		color = "green"
	case "partial":
		title = fmt.Sprintf("⚠ %s部分成功 · %d/%d", actLabel, successCnt, total)
		color = "orange"
	case "canceled":
		title = fmt.Sprintf("⏹ %s已取消 · %s", actLabel, servicesLabel)
		color = "orange"
	default:
		title = fmt.Sprintf("❌ %s失败 · %s", actLabel, servicesLabel)
		color = "red"
	}

	// 正文：项目环境 / 操作人 / 耗时 / 服务列表（每个 service+version+状态一行）/ 错误
	var sb strings.Builder
	if atID != "" {
		sb.WriteString(fmt.Sprintf(`<at id="%s"></at>`+"\n\n", atID))
	}
	sb.WriteString(fmt.Sprintf("**项目环境**：`%s` · **%s**\n", v.Name, strings.ToUpper(v.EnvType)))
	sb.WriteString(fmt.Sprintf("**操作**：%s · **操作人**：%s · **耗时**：%ds\n", actLabel, operator, duration))
	if total > 1 {
		sb.WriteString(fmt.Sprintf("**汇总**：✅ %d · ❌ %d · ⏹ %d / 共 %d\n", successCnt, failedCnt, canceledCnt, total))
	}

	// 服务列表（最多展示 10 个，避免卡片爆）
	if len(taskMap) > 0 {
		sb.WriteString("\n**服务**：\n")
		max := 10
		for i, e := range taskMap {
			if i >= max {
				sb.WriteString(fmt.Sprintf("  …（还有 %d 个，详情见发布历史）\n", len(taskMap)-max))
				break
			}
			icon := map[string]string{"success": "✅", "failed": "❌", "canceled": "⏹", "pending": "⏳", "running": "⏳"}[e.Status]
			if icon == "" {
				icon = "•"
			}
			line := fmt.Sprintf("  %s `%s`", icon, e.Service)
			if e.Version != "" {
				v := e.Version
				if len(v) > 12 {
					v = v[:12]
				}
				line += " · `" + v + "`"
			}
			sb.WriteString(line + "\n")
		}
	}

	if status != "success" && errMsg != "" {
		em := errMsg
		if len(em) > 500 {
			em = em[:500] + "..."
		}
		sb.WriteString(fmt.Sprintf("\n**错误**：\n```\n%s\n```\n", em))
	}

	linkLabel, linkURL := "", ""
	if u := deployDetailURL(depID); u != "" {
		linkLabel, linkURL = "查看发布详情", u
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := services.SendLarkCardWithRetry(ctx, bot.Webhook, bot.Secret, title, sb.String(), color, linkLabel, linkURL, atID); err != nil {
		log.Printf("[vm-lark] dep=%d send failed (after retries): %v", depID, err)
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "failed", depID)
		return
	}
	_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "success", depID)
}

// archiveVmBatchLogs 给批量任务每个 task 各自归档一份 MinIO 日志，object_key 写回 vm_task_map[i]。
//
//	路径：vm-logs/{depID}/{service}.log（避免不同 service 的日志互相覆盖）
//	未配 MinIO / agent 端日志已 GC / 上传失败 → 各自跳过 + log warning，不影响整体
func archiveVmBatchLogs(depID int64, agent *models.DeployAgent, taskMap []vmTaskMapEntry) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[vm-archive] panic dep=%d: %v", depID, r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	mc, err := loadMinIOClientFromDB()
	if err != nil {
		log.Printf("[vm-archive] load minio config: %v (skip dep=%d)", err, depID)
		return
	}
	if mc == nil {
		log.Printf("[vm-archive] minio not configured, skip dep=%d", depID)
		return
	}
	if err := mc.EnsureBucket(ctx); err != nil {
		log.Printf("[vm-archive] ensure bucket: %v (skip dep=%d)", err, depID)
		return
	}

	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	updated := false
	for i, e := range taskMap {
		if e.TaskID == "" {
			continue // submit 失败的，没日志可拉
		}
		logs, _, err := cli.GetTaskLogs(ctx, e.TaskID, 0)
		if err != nil {
			log.Printf("[vm-archive] dep=%d service=%s get logs: %v", depID, e.Service, err)
			continue
		}
		if logs == "" {
			continue
		}
		// 用 service 名做后缀，多服务时不会撞 key（特殊字符已知合法：[A-Za-z0-9_]）
		key := fmt.Sprintf("vm-logs/%d/%s.log", depID, e.Service)
		size, err := mc.PutLog(ctx, key, logs)
		if err != nil {
			log.Printf("[vm-archive] dep=%d service=%s put log: %v", depID, e.Service, err)
			continue
		}
		taskMap[i].LogObjectKey = key
		taskMap[i].LogSize = int(size)
		updated = true
		log.Printf("[vm-archive] dep=%d service=%s archived %d bytes → %s", depID, e.Service, size, key)
	}
	if updated {
		// 把 LogObjectKey/Size 写回 vm_task_map；同时把第一个非空 key 填进 vm_log_object_key 做兼容
		var firstKey string
		var firstSize int
		for _, e := range taskMap {
			if e.LogObjectKey != "" {
				firstKey, firstSize = e.LogObjectKey, e.LogSize
				break
			}
		}
		taskMapBytes, _ := json.Marshal(taskMap)
		_, _ = database.DB.Exec(
			`UPDATE deployment SET vm_task_map=?, vm_log_object_key=?, vm_log_size=? WHERE id=?`,
			taskMapBytes, firstKey, firstSize, depID)
	}
}

// resolveVmTaskID 给批量场景挑出正确的 task_id。
//
//	优先级：vm_task_map[i].service == requestedService 的 task_id；
//	requestedService 为空时返回首个非空 task_id（单服务场景兼容）。
//	返回 "" 表示找不到。
func resolveVmTaskID(depID int64, requestedService string) (peID int64, taskID string, err error) {
	var taskMapRaw sql.NullString
	var fallbackTaskID string
	err = database.DB.QueryRow(
		`SELECT project_env_id, IFNULL(agent_task_id,''), vm_task_map FROM deployment WHERE id=? AND target_type='vm'`,
		depID).Scan(&peID, &fallbackTaskID, &taskMapRaw)
	if err != nil {
		return 0, "", err
	}
	if taskMapRaw.Valid && taskMapRaw.String != "" {
		var taskMap []vmTaskMapEntry
		if jerr := json.Unmarshal([]byte(taskMapRaw.String), &taskMap); jerr == nil {
			if requestedService != "" {
				for _, e := range taskMap {
					if e.Service == requestedService {
						return peID, e.TaskID, nil
					}
				}
				// requestedService 不在 task_map 里 → 报错
				return peID, "", fmt.Errorf("service %q not in deployment task_map", requestedService)
			}
			// 没指定 service：返第一个有 task_id 的
			for _, e := range taskMap {
				if e.TaskID != "" {
					return peID, e.TaskID, nil
				}
			}
		}
	}
	// 回退到老字段（单服务旧数据）
	return peID, fallbackTaskID, nil
}

// GET /api/deployments/{id}/vm-logs?since=<offset>&stream=true&service=<name>
//
//	代理 agent 的 SSE 流。前端打开历史「查看日志」时调这个，跟 K8s 那套统一。
//	批量场景前端要传 service= 指定看哪个服务的日志。
func HandleVmDeployLogs(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])
	requestedService := r.URL.Query().Get("service")

	peID, taskID, err := resolveVmTaskID(depID, requestedService)
	if err != nil {
		JSONError(w, 40400, "vm deployment not found: "+err.Error())
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

// GET /api/deployments/{id}/vm-archived-log?service=<name>
//
//	返回 MinIO 里归档的 ansible 完整日志（成败都归档）。任务终态后才有；
//	pending/running 期间走 /vm-logs?stream=true 实时流。
//	批量场景前端要传 service= 指定看哪个服务的日志（每个服务一份独立 object）。
func HandleVmArchivedLog(w http.ResponseWriter, r *http.Request) {
	depID := ParseID(mux.Vars(r)["id"])
	requestedService := r.URL.Query().Get("service")

	var status string
	var fallbackKey string
	var fallbackSize int
	var peID int64
	var taskMapRaw sql.NullString
	err := database.DB.QueryRow(
		`SELECT IFNULL(status,''), IFNULL(vm_log_object_key,''), IFNULL(vm_log_size,0), project_env_id, vm_task_map
		   FROM deployment
		  WHERE id=? AND target_type='vm'`,
		depID).Scan(&status, &fallbackKey, &fallbackSize, &peID, &taskMapRaw)
	if err != nil {
		JSONError(w, 40400, "vm deployment not found")
		return
	}
	if !IsVmEnvIDAllowed(r, peID) {
		JSONError(w, 40300, "无权查看该 VM 环境的日志")
		return
	}

	// 优先从 vm_task_map 找指定 service 的 object_key（批量场景）
	objectKey, size := fallbackKey, fallbackSize
	if taskMapRaw.Valid && taskMapRaw.String != "" {
		var taskMap []vmTaskMapEntry
		if jerr := json.Unmarshal([]byte(taskMapRaw.String), &taskMap); jerr == nil {
			if requestedService != "" {
				found := false
				for _, e := range taskMap {
					if e.Service == requestedService {
						objectKey = e.LogObjectKey
						size = e.LogSize
						found = true
						break
					}
				}
				if !found {
					JSONError(w, 40400, "service "+requestedService+" 不在该 deployment 里")
					return
				}
			} else if len(taskMap) > 0 && fallbackKey == "" {
				// 没指定 service 也没 fallback key：取第一个非空的
				for _, e := range taskMap {
					if e.LogObjectKey != "" {
						objectKey, size = e.LogObjectKey, e.LogSize
						break
					}
				}
			}
		}
	}

	if objectKey == "" {
		switch status {
		case "pending":
			JSONError(w, 40901, "任务进行中，去部署控制台看实时日志")
		case "":
			JSONError(w, 40901, "任务还没开始，无日志")
		default:
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
