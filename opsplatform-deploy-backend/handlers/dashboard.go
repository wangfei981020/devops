package handlers

import (
	"net/http"
	"strings"
	"time"

	"opsplatform-deploy-backend/database"
)

type kpi struct {
	Projects              int `json:"projects"`
	EnvsTotal             int `json:"envs_total"`
	EnvsUAT               int `json:"envs_uat"`
	EnvsPROD              int `json:"envs_prod"`
	ModulesTotal          int `json:"modules_total"`
	Deployments24h        int `json:"deployments_24h"`
	Deployments24hSuccess int `json:"deployments_24h_success"`
	Deployments24hFailed  int `json:"deployments_24h_failed"`
}

type deploymentBrief struct {
	ID             int64     `json:"id"`
	ProjectEnvID   int64     `json:"project_env_id"`
	ProjectEnvName string    `json:"project_env_name"`
	EnvType        string    `json:"env_type"`
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
	ModulesTotal     int        `json:"modules_total"`
	LastScannedAt    *time.Time `json:"last_scanned_at"`
	LastDeployAt     *time.Time `json:"last_deploy_at"`
	LastDeployStatus string     `json:"last_deploy_status"`
}

// GET /api/dashboard/stats
func HandleDashboardStats(w http.ResponseWriter, r *http.Request) {
	operator := UsernameFromCtx(r)

	allowedIDs, enforce := AllowedEnvIDs(r)
	// 用户启用过滤但没配任何 env → 一切归零
	if enforce && len(allowedIDs) == 0 {
		JSONSuccess(w, map[string]interface{}{
			"kpi":                   kpi{},
			"recent_deployments":    []deploymentBrief{},
			"my_recent_deployments": []deploymentBrief{},
			"envs":                  []envHealth{},
		})
		return
	}

	// 构造可复用的 IN 子句和参数；不过滤时为空串
	// 注意：project_env 表自身的主键是 id（不是 project_env_id），所以分两种 clause：
	//   - peSelfClause: 用于 project_env 本表（id IN (...)）
	//   - fkClause:     用于子表 module/deployment（project_env_id IN (...)）
	//   - peAliasClause: 用于带 pe. 别名的查询（pe.id IN (...)）
	peSelfClause := ""
	fkClause := ""
	peAliasClause := ""
	envIDArgs := []interface{}{}
	if enforce {
		ph := strings.Repeat("?,", len(allowedIDs))
		ph = ph[:len(ph)-1]
		peSelfClause = " AND id IN (" + ph + ")"
		fkClause = " AND project_env_id IN (" + ph + ")"
		peAliasClause = " AND pe.id IN (" + ph + ")"
		for _, id := range allowedIDs {
			envIDArgs = append(envIDArgs, id)
		}
	}

	var k kpi
	// projects: project_env.name 形如 "{project}-{env_type}"，去掉 "-uat/-prod" 后缀即项目名
	if enforce {
		// 拿 allowed envs 的 name+env_type，在 Go 层算 distinct 项目名
		args := append([]interface{}{}, envIDArgs...)
		rows, err := database.DB.Query(
			`SELECT name, env_type FROM project_env WHERE 1=1`+peSelfClause, args...)
		if err == nil {
			defer rows.Close()
			seen := map[string]struct{}{}
			for rows.Next() {
				var n, t string
				_ = rows.Scan(&n, &t)
				p := strings.TrimSuffix(n, "-"+t)
				seen[p] = struct{}{}
			}
			k.Projects = len(seen)
		}
	} else {
		// admin / 不过滤：直接用 project 表 + project_env 派生（与 HandleListProjects 同源）
		_ = database.DB.QueryRow(`SELECT COUNT(DISTINCT name) FROM (
			SELECT name FROM project
			UNION
			SELECT TRIM(TRAILING CONCAT('-', env_type) FROM name) AS name FROM project_env
		) t`).Scan(&k.Projects)
	}

	// envs total / uat / prod（用 project_env 自身的 id）
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE 1=1`+peSelfClause, envIDArgs...).Scan(&k.EnvsTotal)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE env_type='uat'`+peSelfClause, envIDArgs...).Scan(&k.EnvsUAT)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE env_type='prod'`+peSelfClause, envIDArgs...).Scan(&k.EnvsPROD)

	// modules（module 表的 FK 是 project_env_id）
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM module WHERE 1=1`+fkClause, envIDArgs...).Scan(&k.ModulesTotal)

	// deployments 24h（deployment 表 FK 是 project_env_id）
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM deployment
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL 24 HOUR)`+fkClause, envIDArgs...).Scan(&k.Deployments24h)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM deployment
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL 24 HOUR) AND status='success'`+fkClause, envIDArgs...).Scan(&k.Deployments24hSuccess)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM deployment
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL 24 HOUR) AND status='failed'`+fkClause, envIDArgs...).Scan(&k.Deployments24hFailed)

	// 最近 10 条发布（loadDeploymentBriefs 内部用 d.project_env_id，所以传 envIDClause 一样生效，
	// 但 d. 别名前缀更清楚——下面给 helper 同时传 clause 和 args）
	dEnvClause := ""
	if enforce {
		dEnvClause = " AND d.project_env_id IN (" + strings.Repeat("?,", len(allowedIDs))[:len(allowedIDs)*2-1] + ")"
	}
	recent := loadDeploymentBriefs(dEnvClause+` ORDER BY d.created_at DESC LIMIT 10`, envIDArgs)

	// 我的最近 5 条
	mine := []deploymentBrief{}
	if operator != "" && operator != "system" {
		args := append([]interface{}{}, envIDArgs...)
		args = append(args, operator)
		mine = loadDeploymentBriefs(dEnvClause+` AND d.operator=? ORDER BY d.created_at DESC LIMIT 5`, args)
	}

	// 环境健康列表
	envs := []envHealth{}
	rows, err := database.DB.Query(`SELECT pe.id, pe.name, pe.env_type,
		(SELECT COUNT(*) FROM module WHERE project_env_id=pe.id) AS modules_total,
		(SELECT MAX(last_scanned_at) FROM module WHERE project_env_id=pe.id) AS last_scanned_at,
		(SELECT MAX(created_at) FROM deployment WHERE project_env_id=pe.id) AS last_deploy_at,
		(SELECT status FROM deployment WHERE project_env_id=pe.id ORDER BY created_at DESC LIMIT 1) AS last_deploy_status
		FROM project_env pe WHERE 1=1`+peAliasClause+` ORDER BY last_deploy_at DESC, pe.name`, envIDArgs...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var e envHealth
			var lastStatus *string
			_ = rows.Scan(&e.ID, &e.Name, &e.EnvType, &e.ModulesTotal, &e.LastScannedAt, &e.LastDeployAt, &lastStatus)
			if lastStatus != nil {
				e.LastDeployStatus = *lastStatus
			}
			envs = append(envs, e)
		}
	}

	JSONSuccess(w, map[string]interface{}{
		"kpi":                   k,
		"recent_deployments":    recent,
		"my_recent_deployments": mine,
		"envs":                  envs,
	})
}

// loadDeploymentBriefs 拼一段 WHERE 后缀 + 额外参数
// baseTail 例："ORDER BY ... LIMIT 10"  或  "AND d.operator=? ORDER BY ... LIMIT 5"
func loadDeploymentBriefs(tail string, extraArgs []interface{}) []deploymentBrief {
	q := `SELECT d.id, d.project_env_id, pe.name, pe.env_type, d.action, d.status, IFNULL(d.lark_notify,''),
			d.operator, IFNULL(JSON_LENGTH(d.module_names), 0), d.duration_sec, d.created_at
		FROM deployment d LEFT JOIN project_env pe ON pe.id = d.project_env_id
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
		_ = rows.Scan(&d.ID, &d.ProjectEnvID, &peName, &envType, &d.Action, &d.Status, &d.LarkNotify,
			&d.Operator, &d.ModuleCount, &d.DurationSec, &d.CreatedAt)
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
