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
	// 项目名前缀匹配 project_env.name（e.g. g32 匹配 g32-uat/g32-prod）
	if v := r.URL.Query().Get("project"); v != "" {
		where += " AND project_env_id IN (SELECT id FROM project_env WHERE name LIKE ?)"
		args = append(args, v+"-%")
	}
	// 环境类型（uat/prod）
	if v := r.URL.Query().Get("env_type"); v != "" {
		where += " AND project_env_id IN (SELECT id FROM project_env WHERE env_type=?)"
		args = append(args, v)
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
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	isAdmin := IsAdmin(r)
	list := []models.Deployment{}
	for rows.Next() {
		var d models.Deployment
		var mnames, changes, argoResults []byte
		_ = rows.Scan(&d.ID, &d.ProjectEnvID, &d.Action, &d.RefDeploymentID, &mnames, &changes, &d.GitCommit, &d.GitCommitURL,
			&argoResults, &d.LarkNotify, &d.Operator, &d.Status, &d.ErrorMsg, &d.DurationSec, &d.CreatedAt)
		_ = jsonUnmarshalImpl(mnames, &d.ModuleNames)
		_ = jsonUnmarshalImpl(changes, &d.Changes)
		_ = jsonUnmarshalImpl(argoResults, &d.ArgocdResults)
		// error_msg 可能含 git stderr / 文件路径等，非 admin 脱敏
		if !isAdmin && d.ErrorMsg != "" {
			d.ErrorMsg = "（失败详情已隐藏，请联系管理员查看）"
		}
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
	if !IsAdmin(r) && d.ErrorMsg != "" {
		d.ErrorMsg = "（失败详情已隐藏，请联系管理员查看）"
	}
	JSONSuccess(w, d)
}
