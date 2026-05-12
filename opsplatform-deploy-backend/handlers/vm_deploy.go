package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
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

// vmInflightConflict 描述某个 service 当前正被一个 pending VM deployment 占用
type vmInflightConflict struct {
	Service      string    `json:"service"`
	Operator     string    `json:"operator"`
	DeploymentID int64     `json:"deployment_id"`
	Action       string    `json:"action"`
	StartedAt    time.Time `json:"started_at"`
}

// findVmInflightConflicts 查 envID 下所有 pending VM deployment 的 vm_task_map，
// 找出 services 集合里有任何一项已经被占用的情况。
//
//	vm_task_map 为 NULL / 空 / JSON 解析失败 → 跳过该行（脏数据不阻塞主流程）。
//	返回按 service 名字母序排序，让弹窗稳定显示。
func findVmInflightConflicts(envID int64, services []string) ([]vmInflightConflict, error) {
	if len(services) == 0 {
		return nil, nil
	}
	wanted := make(map[string]struct{}, len(services))
	for _, s := range services {
		wanted[s] = struct{}{}
	}

	rows, err := database.DB.Query(`
		SELECT id, IFNULL(operator,''), IFNULL(action,''), created_at, vm_task_map
		  FROM deployment
		 WHERE project_env_id = ? AND target_type = 'vm' AND status = 'pending'`,
		envID)
	if err != nil {
		return nil, fmt.Errorf("query pending vm deployments: %w", err)
	}
	defer rows.Close()

	var out []vmInflightConflict
	for rows.Next() {
		var (
			depID     int64
			operator  string
			action    string
			createdAt time.Time
			rawMap    sql.NullString
		)
		if err := rows.Scan(&depID, &operator, &action, &createdAt, &rawMap); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if !rawMap.Valid || rawMap.String == "" {
			continue
		}
		var entries []vmTaskMapEntry
		if err := json.Unmarshal([]byte(rawMap.String), &entries); err != nil {
			log.Printf("[vm-inflight] dep=%d vm_task_map unmarshal: %v", depID, err)
			continue
		}
		for _, e := range entries {
			if _, ok := wanted[e.Service]; !ok {
				continue
			}
			// 终态的 service 不算占用，agent 锁已经释放
			if e.Status == "success" || e.Status == "failed" || e.Status == "canceled" {
				continue
			}
			out = append(out, vmInflightConflict{
				Service:      e.Service,
				Operator:     operator,
				DeploymentID: depID,
				Action:       action,
				StartedAt:    createdAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out, nil
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

	// pre-check：同 env 下 pending VM 任务里，本次请求的 services 有没有已经在跑的
	// 命中 → 直接 40901 + conflicts，前端弹冲突弹窗。
	// 不查这一步的话，请求会一路走到 agent，被 agent 端 service 锁 400 退回，
	// 用户看到的就是 "总耗时 0s + 失败" + "没归档日志" 的误导提示。
	preCheckNames := make([]string, len(req.Services))
	for i, it := range req.Services {
		preCheckNames[i] = it.Service
	}
	conflicts, err := findVmInflightConflicts(req.VmProjectEnvID, preCheckNames)
	if err != nil {
		InternalErr(w, r, fmt.Errorf("check inflight conflicts: %w", err))
		return
	}
	if len(conflicts) > 0 {
		JSONErrorWithData(w, 40901, "有服务正在发布中，无法重复提交", map[string]interface{}{
			"conflicts": conflicts,
		})
		return
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

	// v107 起改用 batch 端点：1 次 git sync（agent 内 gitMu 串行 + 智能重试）→ 并行起 N 个 work。
	// 解决多 service 并发时各自 git pull 抢 .git/index.lock 的问题；语义跟 K8s update_image 对齐：
	// git pull 失败 → 整批 failed（agent 内部把 N 个 task 都标 failed，前端轮询时一致显示）。
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	taskMap := make([]vmTaskMapEntry, len(req.Services))

	batchItems := make([]services.AgentBatchItem, len(req.Services))
	for i, it := range req.Services {
		batchItems[i] = services.AgentBatchItem{Service: it.Service, Version: it.Version}
	}
	submitCtx, cancelSubmit := context.WithTimeout(r.Context(), 30*time.Second)
	batchResults, batchErr := cli.SubmitBatch(submitCtx, services.AgentBatchSpec{
		Action:   req.Action,
		Project:  v.ProjectCode,
		Env:      v.EnvType,
		Services: batchItems,
	})
	cancelSubmit()

	if batchErr != nil {
		// agent 整体拒收（spec 校验 / service 锁占用）→ 全部 failed
		log.Printf("[vm-deploy] dep=%d batch submit failed: %v", depID, batchErr)
		for i, it := range req.Services {
			taskMap[i] = vmTaskMapEntry{
				Service: it.Service, Version: it.Version,
				TaskID: "", Status: "failed", ErrMsg: batchErr.Error(),
			}
		}
	} else {
		// 把 agent 返回的 task_id 按 service 名匹配到 taskMap（顺序通常一致，按名匹配防错位）
		idByService := make(map[string]string, len(batchResults))
		for _, r := range batchResults {
			idByService[r.Service] = r.TaskID
		}
		for i, it := range req.Services {
			tid, ok := idByService[it.Service]
			if !ok || tid == "" {
				taskMap[i] = vmTaskMapEntry{
					Service: it.Service, Version: it.Version,
					TaskID: "", Status: "failed",
					ErrMsg: "agent 未返回此 service 的 task_id（可能 spec 校验失败）",
				}
				continue
			}
			taskMap[i] = vmTaskMapEntry{
				Service: it.Service, Version: it.Version,
				TaskID: tid, Status: "pending",
			}
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
// sendVmLarkNotify 按 K8s 同款"分组拆卡"模式发 VM 部署通知。
//
//	全 success / 全 failed / 全 canceled → 1 张该色卡（列出所有该状态模块）
//	混合 partial → 拆 2-3 张：成功一张绿、失败一张红、取消一张橙
//	每张卡顶部公共字段一致：操作人 / 项目环境 / 更新时间
//	每个模块块：✅/❌/⏹ 模块名 + （update_version 时）版本号
//	rsync 卡每个模块块没有"版本号"行
//	@ 操作人通过 SendLarkCard 的变长参数加，body 内不再手写 <at>（避免重复 @）
//	任何一张卡发送失败（重试 3 次后还失败）→ deployment.lark_notify='failed'，否则 'success'
func sendVmLarkNotify(depID int64, status, errMsg string, duration int, req vmDeployReq, taskMap []vmTaskMapEntry, v *models.VmProjectEnv, operator string) {
	_ = errMsg     // 错误信息已经在每个 task 的 ErrMsg 里，不再单独显示
	_ = duration   // 用 created_at 当"更新时间"，不显示耗时（K8s 也不显示）
	_ = status     // 用 taskMap 自己分组，不依赖外层 status

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

	// 取 deployment.created_at 当"更新时间"，跟发布历史的时间列保持一致
	var createdAt time.Time
	_ = database.DB.QueryRow(`SELECT created_at FROM deployment WHERE id=?`, depID).Scan(&createdAt)
	updateTime := createdAt.Format("2006-01-02 15:04:05")

	// op 字眼按 UI 命名：rsync / 更新
	opLabel := "更新"
	if req.Action == "rsync" {
		opLabel = "rsync"
	}

	atID := LookupContactLarkID(operator)

	// 按状态分组
	var successItems, failedItems, canceledItems []vmTaskMapEntry
	for _, e := range taskMap {
		switch e.Status {
		case "success":
			successItems = append(successItems, e)
		case "failed":
			failedItems = append(failedItems, e)
		case "canceled":
			canceledItems = append(canceledItems, e)
		}
	}

	// 构造卡片集合：每组非空就出一张卡（K8s sendCardSets 同款）
	type cardSpec struct {
		title string
		color string // green / red / orange
		body  string
	}
	// 标题加 [envName] 前缀，跟 K8s 通知格式拉齐
	envPrefix := "[" + v.Name + "] "
	cards := []cardSpec{}
	if len(successItems) > 0 {
		cards = append(cards, cardSpec{
			title: fmt.Sprintf("✅ %s%s成功 · %d 个模块", envPrefix, opLabel, len(successItems)),
			color: "green",
			body:  buildVmCardBody(operator, v.Name, updateTime, "成功", "✅", successItems, req.Action),
		})
	}
	if len(failedItems) > 0 {
		cards = append(cards, cardSpec{
			title: fmt.Sprintf("❌ %s%s失败 · %d 个模块", envPrefix, opLabel, len(failedItems)),
			color: "red",
			body:  buildVmCardBody(operator, v.Name, updateTime, "失败", "❌", failedItems, req.Action),
		})
	}
	if len(canceledItems) > 0 {
		cards = append(cards, cardSpec{
			title: fmt.Sprintf("⏹ %s%s已取消 · %d 个模块", envPrefix, opLabel, len(canceledItems)),
			color: "orange",
			body:  buildVmCardBody(operator, v.Name, updateTime, "已取消", "⏹", canceledItems, req.Action),
		})
	}
	if len(cards) == 0 {
		// 极端兜底：所有 task 都是 pending/running 终态？理论不会到这里
		log.Printf("[vm-lark] dep=%d no terminal tasks to notify", depID)
		_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, "skipped", depID)
		return
	}

	// 链接按钮：发布详情页
	linkLabel, linkURL := "", ""
	if u := deployDetailURL(depID); u != "" {
		linkLabel, linkURL = "查看发布详情", u
	}

	// 逐张发送，整体成功才记 'success'，任何 1 张失败记 'failed'
	anyFail := false
	for _, c := range cards {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		err := services.SendLarkCardWithRetry(ctx, bot.Webhook, bot.Secret, c.title, c.body, c.color, linkLabel, linkURL, atID)
		cancel()
		if err != nil {
			log.Printf("[vm-lark] dep=%d send card %q failed (after retries): %v", depID, c.title, err)
			anyFail = true
		}
	}
	finalStatus := "success"
	if anyFail {
		finalStatus = "failed"
	}
	_, _ = database.DB.Exec(`UPDATE deployment SET lark_notify=? WHERE id=?`, finalStatus, depID)
}

// buildVmCardBody 拼一张 VM Lark 卡的 body（顶部公共字段 + 分隔线 + 状态分组 + 模块列表）。
//
//	统一格式：
//	  操作人: cesar
//	  项目环境: g01-uat
//	  更新时间: 2026-04-29 22:20:00
//	  ———— {状态} ({N}) ————
//	  ✅ 模块: G01_op_office
//	    版本号: 531198bb...      ← rsync 时省略此行
//	  ✅ 模块: G01_anchor_web
//	    版本号: 078e7d129cca...
func buildVmCardBody(operator, envName, updateTime, statusLabel, icon string, items []vmTaskMapEntry, action string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**操作人**: %s\n", operator))
	sb.WriteString(fmt.Sprintf("**项目环境**: %s\n", envName))
	sb.WriteString(fmt.Sprintf("**更新时间**: %s\n", updateTime))
	sb.WriteString(fmt.Sprintf("\n———— %s (%d) ————\n", statusLabel, len(items)))
	for _, e := range items {
		sb.WriteString(fmt.Sprintf("%s **模块**: %s\n", icon, e.Service))
		// rsync 不显示版本号
		if action == "update_version" && e.Version != "" {
			sb.WriteString(fmt.Sprintf("  **版本号**: %s\n", e.Version))
		}
		// 失败模块带一行错误信息（截断到 200 字防爆）
		if e.Status == "failed" && e.ErrMsg != "" {
			em := e.ErrMsg
			if len(em) > 200 {
				em = em[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("  **错误**: %s\n", em))
		}
	}
	return sb.String()
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
