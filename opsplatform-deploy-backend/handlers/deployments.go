package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListDeployments(w http.ResponseWriter, r *http.Request) {
	moduleID := r.URL.Query().Get("module_id")
	peID := r.URL.Query().Get("project_env_id")
	action := r.URL.Query().Get("action")
	status := r.URL.Query().Get("status")
	pageStr := r.URL.Query().Get("page")
	sizeStr := r.URL.Query().Get("page_size")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(sizeStr)
	if size <= 0 || size > 200 {
		size = 20
	}

	where := " WHERE 1=1"
	args := []interface{}{}
	if moduleID != "" {
		where += " AND module_id=?"
		args = append(args, moduleID)
	}
	if peID != "" {
		where += " AND project_env_id=?"
		args = append(args, peID)
	}
	if action != "" {
		where += " AND action=?"
		args = append(args, action)
	}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}

	var total int64
	database.DB.QueryRow("SELECT COUNT(*) FROM deployments"+where, args...).Scan(&total)

	q := `SELECT id, module_id, module_name, project_env_id, action, from_tag, to_tag,
		IFNULL(git_commit,''), IFNULL(git_commit_url,''),
		argocd_sync_status, IFNULL(argocd_sync_msg,''),
		notify_status, IFNULL(notify_msg,''),
		operator, status, IFNULL(error_msg,''), created_at
		FROM deployments` + where + " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, size, (page-1)*size)

	rows, err := database.DB.Query(q, args...)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.Deployment{}
	for rows.Next() {
		var d models.Deployment
		rows.Scan(&d.ID, &d.ModuleID, &d.ModuleName, &d.ProjectEnvID, &d.Action, &d.FromTag, &d.ToTag,
			&d.GitCommit, &d.GitCommitURL,
			&d.ArgocdSyncStatus, &d.ArgocdSyncMsg,
			&d.NotifyStatus, &d.NotifyMsg,
			&d.Operator, &d.Status, &d.ErrorMsg, &d.CreatedAt)
		list = append(list, d)
	}
	jsonSuccess(w, Page{Total: total, List: list})
}

func HandleGetDeployment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var d models.Deployment
	err := database.DB.QueryRow(`SELECT id, module_id, module_name, project_env_id, action, from_tag, to_tag,
		IFNULL(values_before,''), IFNULL(values_after,''),
		IFNULL(git_commit,''), IFNULL(git_commit_url,''),
		argocd_sync_status, IFNULL(argocd_sync_msg,''),
		notify_status, IFNULL(notify_msg,''),
		operator, status, IFNULL(error_msg,''), created_at
		FROM deployments WHERE id=?`, id).
		Scan(&d.ID, &d.ModuleID, &d.ModuleName, &d.ProjectEnvID, &d.Action, &d.FromTag, &d.ToTag,
			&d.ValuesBefore, &d.ValuesAfter,
			&d.GitCommit, &d.GitCommitURL,
			&d.ArgocdSyncStatus, &d.ArgocdSyncMsg,
			&d.NotifyStatus, &d.NotifyMsg,
			&d.Operator, &d.Status, &d.ErrorMsg, &d.CreatedAt)
	if err != nil {
		jsonError(w, 40400, "发布记录不存在")
		return
	}
	jsonSuccess(w, d)
}

func HandleGetDeploymentDiff(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var before, after string
	err := database.DB.QueryRow(`SELECT IFNULL(values_before,''), IFNULL(values_after,'') FROM deployments WHERE id=?`, id).
		Scan(&before, &after)
	if err != nil {
		jsonError(w, 40400, "发布记录不存在")
		return
	}
	jsonSuccess(w, map[string]string{"before": before, "after": after})
}
