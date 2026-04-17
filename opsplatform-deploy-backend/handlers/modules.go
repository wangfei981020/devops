package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListModules(w http.ResponseWriter, r *http.Request) {
	peID := r.URL.Query().Get("project_env_id")
	status := r.URL.Query().Get("status")

	q := `SELECT m.id, m.project_env_id, m.name, m.template_id, m.template_version, m.image_repo, m.current_tag,
	             m.replicas, IFNULL(m.autoscaling,''), IFNULL(m.resources,''), IFNULL(m.rolling_update,''), m.revision_history_limit,
	             IFNULL(m.env_vars,''), IFNULL(m.extra_env_vars,''), IFNULL(m.tidb_secrets,''), IFNULL(m.probe_override,''), IFNULL(m.configmap_data,''),
	             m.git_chart_path, m.argocd_app_name, m.status, m.created_at, m.updated_at,
	             p.name, e.name, t.name
	      FROM modules m
	      LEFT JOIN project_envs pe ON m.project_env_id = pe.id
	      LEFT JOIN projects p ON pe.project_id = p.id
	      LEFT JOIN environments e ON pe.env_id = e.id
	      LEFT JOIN chart_templates t ON m.template_id = t.id
	      WHERE 1=1`
	args := []interface{}{}
	if peID != "" {
		q += " AND m.project_env_id=?"
		args = append(args, peID)
	}
	if status != "" {
		q += " AND m.status=?"
		args = append(args, status)
	} else {
		q += " AND m.status!='deleted'"
	}
	q += " ORDER BY m.id DESC"

	rows, err := database.DB.Query(q, args...)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()

	list := []models.Module{}
	for rows.Next() {
		var m models.Module
		rows.Scan(&m.ID, &m.ProjectEnvID, &m.Name, &m.TemplateID, &m.TemplateVersion, &m.ImageRepo, &m.CurrentTag,
			&m.Replicas, &m.Autoscaling, &m.Resources, &m.RollingUpdate, &m.RevisionHistoryLimit,
			&m.EnvVars, &m.ExtraEnvVars, &m.TidbSecrets, &m.ProbeOverride, &m.ConfigmapData,
			&m.GitChartPath, &m.ArgocdAppName, &m.Status, &m.CreatedAt, &m.UpdatedAt,
			&m.ProjectName, &m.EnvName, &m.TemplateName)
		list = append(list, m)
	}
	jsonSuccess(w, list)
}

func HandleGetModule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var m models.Module
	err := database.DB.QueryRow(`SELECT m.id, m.project_env_id, m.name, m.template_id, m.template_version, m.image_repo, m.current_tag,
		m.replicas, IFNULL(m.autoscaling,''), IFNULL(m.resources,''), IFNULL(m.rolling_update,''), m.revision_history_limit,
		IFNULL(m.env_vars,''), IFNULL(m.extra_env_vars,''), IFNULL(m.tidb_secrets,''), IFNULL(m.probe_override,''), IFNULL(m.configmap_data,''),
		m.git_chart_path, m.argocd_app_name, m.status, m.created_at, m.updated_at,
		p.name, e.name, t.name
		FROM modules m
		LEFT JOIN project_envs pe ON m.project_env_id = pe.id
		LEFT JOIN projects p ON pe.project_id = p.id
		LEFT JOIN environments e ON pe.env_id = e.id
		LEFT JOIN chart_templates t ON m.template_id = t.id
		WHERE m.id=?`, id).
		Scan(&m.ID, &m.ProjectEnvID, &m.Name, &m.TemplateID, &m.TemplateVersion, &m.ImageRepo, &m.CurrentTag,
			&m.Replicas, &m.Autoscaling, &m.Resources, &m.RollingUpdate, &m.RevisionHistoryLimit,
			&m.EnvVars, &m.ExtraEnvVars, &m.TidbSecrets, &m.ProbeOverride, &m.ConfigmapData,
			&m.GitChartPath, &m.ArgocdAppName, &m.Status, &m.CreatedAt, &m.UpdatedAt,
			&m.ProjectName, &m.EnvName, &m.TemplateName)
	if err != nil {
		jsonError(w, 40400, "模块不存在")
		return
	}
	jsonSuccess(w, m)
}

// 以下为 stub: 涉及 git/argocd/lark 等底层调用, 在阶段 2/3 实现

func HandleCreateModule(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "新建模块: 待实现 git commit + ArgoCD App 创建 (阶段3)"})
}

func HandleUpdateModule(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "更新模块配置: 待实现 (阶段3)"})
}

func HandleDeleteModule(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "删除模块: 待实现 (阶段3)"})
}

func HandleUpdateModuleImage(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "更新镜像 tag: 待实现 (阶段3)"})
}

func HandleRestartModule(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "重启服务: 待实现 (阶段3, ArgoCD restart)"})
}

func HandleScaleModule(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "副本数调整: 待实现 (阶段3)"})
}

func HandleSyncModule(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "手动 sync: 待实现 (阶段3)"})
}

func HandleRollbackModule(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "回滚: 待实现 (阶段3)"})
}

func HandleGetModuleValues(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "查看 values.yaml: 待实现 (阶段2)"})
}

func HandleGetModuleRuntime(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "运行时状态: 待实现 (阶段3)"})
}
