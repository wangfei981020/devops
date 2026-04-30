package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"opsplatform-deploy-backend/database"
)

type kpi struct {
	Projects              int `json:"projects"`
	EnvsTotal             int `json:"envs_total"`
	EnvsUAT               int `json:"envs_uat"`
	EnvsPROD              int `json:"envs_prod"`
	K8sModulesTotal       int `json:"k8s_modules_total"`
	VmServicesTotal       int `json:"vm_services_total"`
	Deployments24h        int `json:"deployments_24h"`
	Deployments24hSuccess int `json:"deployments_24h_success"`
	Deployments24hFailed  int `json:"deployments_24h_failed"`
}

type deploymentBrief struct {
	ID             int64     `json:"id"`
	ProjectEnvID   int64     `json:"project_env_id"`
	ProjectEnvName string    `json:"project_env_name"`
	EnvType        string    `json:"env_type"`
	Kind           string    `json:"kind"` // 'k8s' | 'vm'
	Action         string    `json:"action"`
	Status         string    `json:"status"`
	LarkNotify     string    `json:"lark_notify"`
	Operator       string    `json:"operator"`
	ModuleCount    int       `json:"module_count"`
	DurationSec    int       `json:"duration_sec"`
	CreatedAt      time.Time `json:"created_at"`
}

type envHealth struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	EnvType          string     `json:"env_type"`
	Kind             string     `json:"kind"` // 'k8s' | 'vm'
	ModulesTotal     int        `json:"modules_total"`
	LastScannedAt    *time.Time `json:"last_scanned_at"`
	LastDeployAt     *time.Time `json:"last_deploy_at"`
	LastDeployStatus string     `json:"last_deploy_status"`
}

// inClause 把 ids 渲染成 "?,?,?" 占位符 + interface{} 参数，专给 SQL IN 用。
func inClause(ids []int64) (string, []interface{}) {
	if len(ids) == 0 {
		return "", nil
	}
	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return ph, args
}

// HandleDashboardStats: GET /api/dashboard/stats
//
// 同时统计 K8s (project_env / module) 和 VM (vm_project_env / vm_service)。
// deployment 表通过 target_type 区分，project_env_id 在 K8s/VM 两张 env 表分别独立自增，
// 必须按 target_type 分支 JOIN 才不会撞号显示错环境名。
func HandleDashboardStats(w http.ResponseWriter, r *http.Request) {
	operator := UsernameFromCtx(r)

	k8sIDs, enforce := AllowedEnvIDs(r)
	vmIDs, _ := AllowedVmEnvIDs(r)

	if enforce && len(k8sIDs) == 0 && len(vmIDs) == 0 {
		JSONSuccess(w, map[string]interface{}{
			"kpi":                   kpi{},
			"recent_deployments":    []deploymentBrief{},
			"my_recent_deployments": []deploymentBrief{},
			"envs":                  []envHealth{},
		})
		return
	}

	var k kpi

	/* ---------- 项目数 ---------- */
	if !enforce {
		// admin / 不过滤：直接用 project 表（K8s + VM 共用同一张 project）
		_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project`).Scan(&k.Projects)
	} else {
		// 过滤：从两张 env 表的 name 派生项目名（去 "-{env_type}" 后缀，大小写归一）
		seen := map[string]struct{}{}
		collect := func(table string, ids []int64) {
			if len(ids) == 0 {
				return
			}
			ph, args := inClause(ids)
			rows, err := database.DB.Query(
				`SELECT name, env_type FROM `+table+` WHERE id IN (`+ph+`)`, args...)
			if err != nil {
				return
			}
			defer rows.Close()
			for rows.Next() {
				var n, t string
				_ = rows.Scan(&n, &t)
				seen[strings.TrimSuffix(strings.ToLower(n), "-"+strings.ToLower(t))] = struct{}{}
			}
		}
		collect("project_env", k8sIDs)
		collect("vm_project_env", vmIDs)
		k.Projects = len(seen)
	}

	/* ---------- 环境总数 / UAT / PROD ---------- */
	countEnvs := func(table string, ids []int64, where string) int {
		if !enforce {
			n := 0
			_ = database.DB.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE ` + where).Scan(&n)
			return n
		}
		if len(ids) == 0 {
			return 0
		}
		ph, args := inClause(ids)
		n := 0
		_ = database.DB.QueryRow(
			`SELECT COUNT(*) FROM `+table+` WHERE id IN (`+ph+`) AND `+where, args...).Scan(&n)
		return n
	}
	// K8s env_type 是小写 enum('uat','prod')；VM 是大写 enum('LPT','UAT','PROD')，业务上 LPT 算 UAT
	k.EnvsTotal = countEnvs("project_env", k8sIDs, "1=1") +
		countEnvs("vm_project_env", vmIDs, "1=1")
	k.EnvsUAT = countEnvs("project_env", k8sIDs, "env_type='uat'") +
		countEnvs("vm_project_env", vmIDs, "env_type IN ('UAT','LPT')")
	k.EnvsPROD = countEnvs("project_env", k8sIDs, "env_type='prod'") +
		countEnvs("vm_project_env", vmIDs, "env_type='PROD'")

	/* ---------- K8s 模块数 / VM 服务数（拆两个 KPI 卡） ---------- */
	countByEnvFK := func(table, fk string, ids []int64) int {
		if !enforce {
			n := 0
			_ = database.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
			return n
		}
		if len(ids) == 0 {
			return 0
		}
		ph, args := inClause(ids)
		n := 0
		_ = database.DB.QueryRow(
			`SELECT COUNT(*) FROM `+table+` WHERE `+fk+` IN (`+ph+`)`, args...).Scan(&n)
		return n
	}
	k.K8sModulesTotal = countByEnvFK("module", "project_env_id", k8sIDs)
	k.VmServicesTotal = countByEnvFK("vm_service", "vm_project_env_id", vmIDs)

	/* ---------- 24h 发布 ---------- */
	deployFilter, deployArgs := buildDeployFilter(k8sIDs, vmIDs, enforce)
	count24h := func(extraWhere string) int {
		n := 0
		_ = database.DB.QueryRow(
			`SELECT COUNT(*) FROM deployment d
			 WHERE d.created_at >= DATE_SUB(NOW(), INTERVAL 24 HOUR)`+deployFilter+extraWhere,
			deployArgs...).Scan(&n)
		return n
	}
	k.Deployments24h = count24h("")
	k.Deployments24hSuccess = count24h(" AND d.status='success'")
	k.Deployments24hFailed = count24h(" AND d.status='failed'")

	/* ---------- 最近 10 条发布 ---------- */
	recent := loadDeploymentBriefs(deployFilter+" ORDER BY d.created_at DESC LIMIT 10", deployArgs)

	/* ---------- 我的最近 5 条 ---------- */
	mine := []deploymentBrief{}
	if operator != "" && operator != "system" {
		args := append([]interface{}{}, deployArgs...)
		args = append(args, operator)
		mine = loadDeploymentBriefs(deployFilter+" AND d.operator=? ORDER BY d.created_at DESC LIMIT 5", args)
	}

	/* ---------- 环境健康列表（K8s + VM 合并） ---------- */
	envs := loadEnvHealth(k8sIDs, vmIDs, enforce)

	JSONSuccess(w, map[string]interface{}{
		"kpi":                   k,
		"recent_deployments":    recent,
		"my_recent_deployments": mine,
		"envs":                  envs,
	})
}

// buildDeployFilter 拼 deployment 表的过滤子句（按 target_type 分支取对应 env id 集合）。
//
//	不过滤 → ("", nil)
//	enforce 但单边为空 → 只输出有 id 的那一边
//	enforce 且两边都空 → 已在入口拦截，这里返回 " AND 0" 兜底
func buildDeployFilter(k8sIDs, vmIDs []int64, enforce bool) (string, []interface{}) {
	if !enforce {
		return "", nil
	}
	parts := []string{}
	args := []interface{}{}
	if len(k8sIDs) > 0 {
		ph, a := inClause(k8sIDs)
		parts = append(parts, "(IFNULL(d.target_type,'k8s')='k8s' AND d.project_env_id IN ("+ph+"))")
		args = append(args, a...)
	}
	if len(vmIDs) > 0 {
		ph, a := inClause(vmIDs)
		parts = append(parts, "(d.target_type='vm' AND d.project_env_id IN ("+ph+"))")
		args = append(args, a...)
	}
	if len(parts) == 0 {
		return " AND 0", nil
	}
	return " AND (" + strings.Join(parts, " OR ") + ")", args
}

// loadDeploymentBriefs 关键修复：按 target_type 分别 JOIN 两张 env 表，
// 避免 K8s/VM env id 撞车导致 VM 部署在 dashboard 显示成错的 K8s 环境名。
//
// COALESCE 把同名列从两边 LEFT JOIN 合并；env_type 顺便归一化（VM 大写 → 小写、LPT → uat）。
func loadDeploymentBriefs(tail string, extraArgs []interface{}) []deploymentBrief {
	q := `SELECT d.id, d.project_env_id, IFNULL(d.target_type,'k8s') AS kind,
			COALESCE(pe.name, vpe.name) AS env_name,
			COALESCE(LOWER(pe.env_type),
			         CASE WHEN vpe.env_type IN ('UAT','LPT') THEN 'uat'
			              WHEN vpe.env_type='PROD' THEN 'prod'
			              ELSE LOWER(vpe.env_type) END) AS env_type,
			d.action, d.status, IFNULL(d.lark_notify,''),
			d.operator, IFNULL(JSON_LENGTH(d.module_names), 0), d.duration_sec, d.created_at
		FROM deployment d
		LEFT JOIN project_env  pe  ON pe.id  = d.project_env_id AND IFNULL(d.target_type,'k8s')='k8s'
		LEFT JOIN vm_project_env vpe ON vpe.id = d.project_env_id AND d.target_type='vm'
		WHERE 1=1 ` + tail
	rows, err := database.DB.Query(q, extraArgs...)
	if err != nil {
		return []deploymentBrief{}
	}
	defer rows.Close()
	list := []deploymentBrief{}
	for rows.Next() {
		var d deploymentBrief
		var peName, envType *string
		_ = rows.Scan(&d.ID, &d.ProjectEnvID, &d.Kind, &peName, &envType,
			&d.Action, &d.Status, &d.LarkNotify, &d.Operator, &d.ModuleCount, &d.DurationSec, &d.CreatedAt)
		if peName != nil {
			d.ProjectEnvName = *peName
		}
		if envType != nil {
			d.EnvType = *envType
		}
		list = append(list, d)
	}
	return list
}

// loadEnvHealth 返回 K8s + VM 合并的环境健康列表，每行附 kind 给前端切显示文案。
// 排序：last_deploy_at DESC，NULL 排到最后；同值按 name 升序。
func loadEnvHealth(k8sIDs, vmIDs []int64, enforce bool) []envHealth {
	out := []envHealth{}

	scan := func(rows *sql.Rows, kind string) {
		defer rows.Close()
		for rows.Next() {
			var e envHealth
			e.Kind = kind
			var lastStatus *string
			_ = rows.Scan(&e.ID, &e.Name, &e.EnvType, &e.ModulesTotal,
				&e.LastScannedAt, &e.LastDeployAt, &lastStatus)
			if lastStatus != nil {
				e.LastDeployStatus = *lastStatus
			}
			out = append(out, e)
		}
	}

	// K8s
	if !enforce || len(k8sIDs) > 0 {
		base := `SELECT pe.id, pe.name, LOWER(pe.env_type) AS env_type,
			(SELECT COUNT(*) FROM module WHERE project_env_id=pe.id) AS modules_total,
			(SELECT MAX(last_scanned_at) FROM module WHERE project_env_id=pe.id) AS last_scanned_at,
			(SELECT MAX(created_at) FROM deployment WHERE IFNULL(target_type,'k8s')='k8s' AND project_env_id=pe.id) AS last_deploy_at,
			(SELECT status FROM deployment WHERE IFNULL(target_type,'k8s')='k8s' AND project_env_id=pe.id ORDER BY created_at DESC LIMIT 1) AS last_deploy_status
			FROM project_env pe`
		var rows *sql.Rows
		var err error
		if !enforce {
			rows, err = database.DB.Query(base)
		} else {
			ph, args := inClause(k8sIDs)
			rows, err = database.DB.Query(base+` WHERE pe.id IN (`+ph+`)`, args...)
		}
		if err == nil {
			scan(rows, "k8s")
		}
	}

	// VM
	if !enforce || len(vmIDs) > 0 {
		base := `SELECT vpe.id, vpe.name,
			CASE WHEN vpe.env_type IN ('UAT','LPT') THEN 'uat'
			     WHEN vpe.env_type='PROD' THEN 'prod'
			     ELSE LOWER(vpe.env_type) END AS env_type,
			(SELECT COUNT(*) FROM vm_service WHERE vm_project_env_id=vpe.id) AS modules_total,
			(SELECT MAX(last_scanned_at) FROM vm_service WHERE vm_project_env_id=vpe.id) AS last_scanned_at,
			(SELECT MAX(created_at) FROM deployment WHERE target_type='vm' AND project_env_id=vpe.id) AS last_deploy_at,
			(SELECT status FROM deployment WHERE target_type='vm' AND project_env_id=vpe.id ORDER BY created_at DESC LIMIT 1) AS last_deploy_status
			FROM vm_project_env vpe`
		var rows *sql.Rows
		var err error
		if !enforce {
			rows, err = database.DB.Query(base)
		} else {
			ph, args := inClause(vmIDs)
			rows, err = database.DB.Query(base+` WHERE vpe.id IN (`+ph+`)`, args...)
		}
		if err == nil {
			scan(rows, "vm")
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].LastDeployAt, out[j].LastDeployAt
		if ai == nil && aj == nil {
			return out[i].Name < out[j].Name
		}
		if ai == nil {
			return false
		}
		if aj == nil {
			return true
		}
		if !ai.Equal(*aj) {
			return ai.After(*aj)
		}
		return out[i].Name < out[j].Name
	})
	return out
}
