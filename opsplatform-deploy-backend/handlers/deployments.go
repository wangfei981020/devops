package handlers

import (
	"net/http"
	"strconv"

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
	addEq("action", r.URL.Query().Get("action"))
	addEq("status", r.URL.Query().Get("status"))
	if v := r.URL.Query().Get("operator"); v != "" {
		where += " AND operator LIKE ?"
		args = append(args, "%"+v+"%")
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
		git_commit, git_commit_url, argocd_results, lark_notify, operator, status, IFNULL(error_msg,''), duration_sec, created_at
		FROM deployment `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args2...)
	if err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.Deployment{}
	for rows.Next() {
		var d models.Deployment
		var mnames, changes, argoResults []byte
		_ = rows.Scan(&d.ID, &d.ProjectEnvID, &d.Action, &d.RefDeploymentID, &mnames, &changes, &d.GitCommit, &d.GitCommitURL,
			&argoResults, &d.LarkNotify, &d.Operator, &d.Status, &d.ErrorMsg, &d.DurationSec, &d.CreatedAt)
		_ = jsonUnmarshalImpl(mnames, &d.ModuleNames)
		_ = jsonUnmarshalImpl(changes, &d.Changes)
		_ = jsonUnmarshalImpl(argoResults, &d.ArgocdResults)
		list = append(list, d)
	}
	JSONSuccess(w, map[string]interface{}{"total": total, "list": list})
}

func HandleGetDeployment(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var d models.Deployment
	var mnames, changes, argoResults []byte
	err := database.DB.QueryRow(`SELECT id, project_env_id, action, ref_deployment_id, module_names, changes,
		git_commit, git_commit_url, argocd_results, lark_notify, operator, status, IFNULL(error_msg,''), duration_sec, created_at
		FROM deployment WHERE id=?`, id).
		Scan(&d.ID, &d.ProjectEnvID, &d.Action, &d.RefDeploymentID, &mnames, &changes, &d.GitCommit, &d.GitCommitURL,
			&argoResults, &d.LarkNotify, &d.Operator, &d.Status, &d.ErrorMsg, &d.DurationSec, &d.CreatedAt)
	if err != nil {
		JSONError(w, 40400, "deployment not found")
		return
	}
	_ = jsonUnmarshalImpl(mnames, &d.ModuleNames)
	_ = jsonUnmarshalImpl(changes, &d.Changes)
	_ = jsonUnmarshalImpl(argoResults, &d.ArgocdResults)
	JSONSuccess(w, d)
}
