package handlers

import (
	"net/http"
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

	var k kpi
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project`).Scan(&k.Projects)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env`).Scan(&k.EnvsTotal)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE env_type='uat'`).Scan(&k.EnvsUAT)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE env_type='prod'`).Scan(&k.EnvsPROD)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM module`).Scan(&k.ModulesTotal)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM deployment WHERE created_at >= DATE_SUB(NOW(), INTERVAL 24 HOUR)`).Scan(&k.Deployments24h)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM deployment
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL 24 HOUR) AND status='success'`).Scan(&k.Deployments24hSuccess)
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM deployment
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL 24 HOUR) AND status='failed'`).Scan(&k.Deployments24hFailed)

	// 最近 10 条发布
	recent := loadDeploymentBriefs(`ORDER BY d.created_at DESC LIMIT 10`, nil)

	// 我的最近 5 条
	mine := []deploymentBrief{}
	if operator != "" && operator != "system" {
		mine = loadDeploymentBriefs(`AND d.operator=? ORDER BY d.created_at DESC LIMIT 5`, []interface{}{operator})
	}

	// 环境健康列表
	envs := []envHealth{}
	rows, err := database.DB.Query(`SELECT pe.id, pe.name, pe.env_type,
		(SELECT COUNT(*) FROM module WHERE project_env_id=pe.id) AS modules_total,
		(SELECT MAX(last_scanned_at) FROM module WHERE project_env_id=pe.id) AS last_scanned_at,
		(SELECT MAX(created_at) FROM deployment WHERE project_env_id=pe.id) AS last_deploy_at,
		(SELECT status FROM deployment WHERE project_env_id=pe.id ORDER BY created_at DESC LIMIT 1) AS last_deploy_status
		FROM project_env pe ORDER BY last_deploy_at DESC, pe.name`)
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
