package handlers

import (
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListDeployments(w http.ResponseWriter, r *http.Request) {
	where := "WHERE 1=1"
	args := []interface{}{}
	addEq := func(col, val string) {
		if val != "" {
			where += " AND " + col + "=?"
			args = append(args, val)
		}
	}
	addEq("project_env_id", r.URL.Query().Get("project_env_id"))
	// target_type='k8s'|'vm'：K8s 和 VM 的 project_env_id 来自不同表会撞号，
	// 单纯按 project_env_id 查会把对方的发布记录混进来。前端按 e._kind 传这个参数过滤。
	addEq("target_type", r.URL.Query().Get("target_type"))
	addEq("action", r.URL.Query().Get("action"))
	addEq("status", r.URL.Query().Get("status"))
	if v := r.URL.Query().Get("operator"); v != "" {
		// 特殊值 "me" → 解析为当前登录用户名（前端不用知道用户名）
		if v == "me" {
			v = UsernameFromCtx(r)
			if v == "" {
				JSONSuccess(w, map[string]interface{}{"total": 0, "list": []models.Deployment{}})
				return
			}
		}
		where += " AND operator LIKE ?"
		args = append(args, "%"+v+"%")
	}
	// since=30m / 1h / 2d → 转成 created_at >= NOW() - INTERVAL N MINUTE
	if v := r.URL.Query().Get("since"); v != "" {
		if d, ok := parseSinceDuration(v); ok {
			where += " AND created_at >= DATE_SUB(NOW(), INTERVAL ? SECOND)"
			args = append(args, int(d.Seconds()))
		}
	}
	// 项目名前缀匹配（e.g. g32 匹配 g32-uat/g32-prod）；K8s + VM 都看，按 target_type 选表
	if v := r.URL.Query().Get("project"); v != "" {
		where += ` AND (
			(target_type='k8s' AND project_env_id IN (SELECT id FROM project_env WHERE name LIKE ?))
			OR
			(target_type='vm'  AND project_env_id IN (SELECT id FROM vm_project_env WHERE name LIKE ?))
		)`
		args = append(args, v+"-%", v+"-%")
	}
	// 环境类型（uat/prod/lpt）；VM 表 env_type 大写，用 LOWER() 归一化
	if v := r.URL.Query().Get("env_type"); v != "" {
		where += ` AND (
			(target_type='k8s' AND project_env_id IN (SELECT id FROM project_env WHERE LOWER(env_type)=?))
			OR
			(target_type='vm'  AND project_env_id IN (SELECT id FROM vm_project_env WHERE LOWER(env_type)=?))
		)`
		args = append(args, v, v)
	}
	// 时间范围
	if v := r.URL.Query().Get("time_from"); v != "" {
		where += " AND created_at >= ?"
		args = append(args, v)
	}
	if v := r.URL.Query().Get("time_to"); v != "" {
		where += " AND created_at <= ?"
		args = append(args, v)
	}
	// 模块名模糊匹配（"user-client" 命中 user-client-backend / user-client-frontend）
	// module_names 是 JSON 数组（["a","b","c"]），用 JSON_SEARCH 在数组每个元素里 LIKE 找
	if v := strings.TrimSpace(r.URL.Query().Get("module")); v != "" {
		where += " AND JSON_SEARCH(module_names, 'one', ?) IS NOT NULL"
		args = append(args, "%"+v+"%")
	}
	// env 白名单过滤（admin 时 enforce=false 不加 WHERE）
	// K8s id 来自 project_env，VM id 来自 vm_project_env，分别取后按 target_type 拼条件
	k8sIDs, k8sEnforce := AllowedEnvIDs(r)
	vmIDs, vmEnforce := AllowedVmEnvIDs(r)
	if k8sEnforce || vmEnforce {
		if len(k8sIDs) == 0 && len(vmIDs) == 0 {
			JSONSuccess(w, map[string]interface{}{"total": 0, "list": []models.Deployment{}})
			return
		}
		// 拼 (target_type='k8s' AND id IN (...)) OR (target_type='vm' AND id IN (...))
		clauses := []string{}
		if len(k8sIDs) > 0 {
			ph := strings.Repeat("?,", len(k8sIDs))
			ph = ph[:len(ph)-1]
			clauses = append(clauses, "(target_type='k8s' AND project_env_id IN ("+ph+"))")
			for _, id := range k8sIDs {
				args = append(args, id)
			}
		}
		if len(vmIDs) > 0 {
			ph := strings.Repeat("?,", len(vmIDs))
			ph = ph[:len(ph)-1]
			clauses = append(clauses, "(target_type='vm' AND project_env_id IN ("+ph+"))")
			for _, id := range vmIDs {
				args = append(args, id)
			}
		}
		where += " AND (" + strings.Join(clauses, " OR ") + ")"
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	var total int64
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM deployment "+where, args...).Scan(&total)

	args2 := append(append([]interface{}{}, args...), pageSize, (page-1)*pageSize)
	rows, err := database.DB.Query(`SELECT id, project_env_id, action, ref_deployment_id, module_names, changes,
		git_commit, git_commit_url, argocd_results, lark_notify, operator, status, IFNULL(error_msg,''), duration_sec, created_at,
		vm_task_map
		FROM deployment `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args2...)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	isAdmin := IsAdmin(r)
	list := []models.Deployment{}
	for rows.Next() {
		var d models.Deployment
		var mnames, changes, argoResults, vmTaskMap []byte
		_ = rows.Scan(&d.ID, &d.ProjectEnvID, &d.Action, &d.RefDeploymentID, &mnames, &changes, &d.GitCommit, &d.GitCommitURL,
			&argoResults, &d.LarkNotify, &d.Operator, &d.Status, &d.ErrorMsg, &d.DurationSec, &d.CreatedAt,
			&vmTaskMap)
		_ = jsonUnmarshalImpl(mnames, &d.ModuleNames)
		_ = jsonUnmarshalImpl(changes, &d.Changes)
		_ = jsonUnmarshalImpl(argoResults, &d.ArgocdResults)
		_ = jsonUnmarshalImpl(vmTaskMap, &d.VmTaskMap)
		// error_msg 可能含 git stderr / 文件路径等，非 admin 脱敏
		if !isAdmin && d.ErrorMsg != "" {
			d.ErrorMsg = "（失败详情已隐藏，请联系管理员查看）"
		}
		list = append(list, d)
	}
	JSONSuccess(w, map[string]interface{}{"total": total, "list": list})
}

// parseSinceDuration 解析 "30m" / "1h" / "2d" 这类相对时长。无效返回 (_, false)。
//
//	支持的后缀：s/m/h/d。范围限制在 1s~7d，避免恶意大值打 DB。
var sinceRe = regexp.MustCompile(`^(\d+)([smhd])$`)

func parseSinceDuration(s string) (time.Duration, bool) {
	m := sinceRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	if n <= 0 {
		return 0, false
	}
	var unit time.Duration
	switch m[2] {
	case "s":
		unit = time.Second
	case "m":
		unit = time.Minute
	case "h":
		unit = time.Hour
	case "d":
		unit = 24 * time.Hour
	}
	d := time.Duration(n) * unit
	if d > 7*24*time.Hour {
		d = 7 * 24 * time.Hour
	}
	return d, true
}

// HandleCancelDeployment POST /api/deployments/{id}/cancel
//
//	用户在等待中点「取消等待」：停 deploy-center 这边的 poll，但已 push 的 git commit
//	不会回滚，ArgoCD 会继续把 sync 跑完。任务结束时取消 ctx 让 PollUntilStable 自然
//	退出，状态记 canceled，释放 inflight 槽 + 模块锁（各自的 defer 会处理）。
//
// 权限：操作发起者本人 OR admin
func HandleCancelDeployment(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if id <= 0 {
		JSONError(w, 40001, "id 非法")
		return
	}
	// 多读 target_type，按它分流到 K8s / VM 处理
	var d models.Deployment
	var targetType string
	err := database.DB.QueryRow(
		`SELECT id, project_env_id, status, operator, action, IFNULL(target_type,'k8s') FROM deployment WHERE id=?`, id).
		Scan(&d.ID, &d.ProjectEnvID, &d.Status, &d.Operator, &d.Action, &targetType)
	if err != nil {
		JSONError(w, 40400, "发布记录不存在")
		return
	}
	currentUser := UsernameFromCtx(r)
	if !IsAdmin(r) && d.Operator != currentUser {
		JSONError(w, 40300, "只能取消自己发起的等待")
		return
	}
	if d.Status != models.StatusPending {
		JSONError(w, 40900, "该发布已结束，无法取消（当前状态："+d.Status+"）")
		return
	}

	// VM 分支：调 agent 真正杀进程，pollVmTask 走 ctx.Done() 写状态
	if targetType == "vm" {
		if !cancelVmDeploymentByID(w, r, id) {
			return
		}
		Audit(r, "deploy.cancel", "deployment", strconv.FormatInt(id, 10),
			map[string]interface{}{"action": d.Action, "operator": d.Operator, "target_type": "vm"})
		log.Printf("[deploy-cancel] dep=%d operator=%s target_type=vm canceled", id, currentUser)
		JSONSuccess(w, map[string]interface{}{"id": id, "result": "canceled"})
		return
	}

	// K8s 分支（现有逻辑）
	if !IsEnvIDAllowed(r, d.ProjectEnvID) {
		JSONError(w, 40300, "无权操作该环境的发布")
		return
	}
	if !CancelDeployment(id) {
		// 注册表里没有：可能正好刚结束、或服务重启过。把 DB 状态置 canceled 兜底。
		_, _ = database.DB.Exec(
			`UPDATE deployment SET status=?, error_msg=CONCAT(IFNULL(error_msg,''),
				'\n[auto] cancel requested but task already finished or process restarted')
			 WHERE id=? AND status='pending'`,
			models.StatusCanceled, id)
		Audit(r, "deploy.cancel.stale", "deployment", strconv.FormatInt(id, 10), nil)
		JSONSuccess(w, map[string]interface{}{"id": id, "result": "stale"})
		return
	}
	Audit(r, "deploy.cancel", "deployment", strconv.FormatInt(id, 10),
		map[string]interface{}{"action": d.Action, "operator": d.Operator})
	log.Printf("[deploy-cancel] dep=%d operator=%s target_type=k8s canceled", id, currentUser)
	JSONSuccess(w, map[string]interface{}{"id": id, "result": "canceled"})
}

func HandleGetDeployment(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var d models.Deployment
	var targetType string
	var mnames, changes, argoResults, vmTaskMap []byte
	err := database.DB.QueryRow(`SELECT id, project_env_id, action, ref_deployment_id, module_names, changes,
		git_commit, git_commit_url, argocd_results, lark_notify, operator, status, IFNULL(error_msg,''), duration_sec, created_at,
		IFNULL(target_type,'k8s'), vm_task_map
		FROM deployment WHERE id=?`, id).
		Scan(&d.ID, &d.ProjectEnvID, &d.Action, &d.RefDeploymentID, &mnames, &changes, &d.GitCommit, &d.GitCommitURL,
			&argoResults, &d.LarkNotify, &d.Operator, &d.Status, &d.ErrorMsg, &d.DurationSec, &d.CreatedAt,
			&targetType, &vmTaskMap)
	if err != nil {
		JSONError(w, 40400, "deployment not found")
		return
	}
	// 数据级权限按 target_type 分流：VM 行 project_env_id 实际是 vm_project_env.id
	if targetType == "vm" {
		if !IsVmEnvIDAllowed(r, d.ProjectEnvID) {
			JSONError(w, 40300, "无权访问该 VM 环境的发布记录")
			return
		}
	} else {
		if !IsEnvIDAllowed(r, d.ProjectEnvID) {
			JSONError(w, 40300, "无权访问该环境的发布记录")
			return
		}
	}
	_ = jsonUnmarshalImpl(mnames, &d.ModuleNames)
	_ = jsonUnmarshalImpl(changes, &d.Changes)
	_ = jsonUnmarshalImpl(argoResults, &d.ArgocdResults)
	_ = jsonUnmarshalImpl(vmTaskMap, &d.VmTaskMap)
	if !IsAdmin(r) && d.ErrorMsg != "" {
		d.ErrorMsg = "（失败详情已隐藏，请联系管理员查看）"
	}
	JSONSuccess(w, d)
}
