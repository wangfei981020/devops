package handlers

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

// GET /api/environments —— 可配置环境列表（dev/test/uat/prod…）
func HandleListEnvironments(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(
		`SELECT name, display_name, permission_code, sort_order, enabled, created_at
		 FROM deploy_environment ORDER BY sort_order, name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []models.DeployEnvironment{}
	for rows.Next() {
		var e models.DeployEnvironment
		_ = rows.Scan(&e.Name, &e.DisplayName, &e.PermissionCode, &e.SortOrder, &e.Enabled, &e.CreatedAt)
		list = append(list, e)
	}
	JSONSuccess(w, list)
}

type envReq struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	PermissionCode string `json:"permission_code"`
	SortOrder      int    `json:"sort_order"`
	Enabled        *int   `json:"enabled"`
}

// POST /api/environments
func HandleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	var req envReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := ValidateName(req.Name); err != nil {
		JSONError(w, 40001, "环境名: "+err.Error())
		return
	}
	perm := strings.TrimSpace(req.PermissionCode)
	if perm == "" {
		perm = "submit_" + req.Name // 每环境独立权限档，默认 submit_<env>
	}
	enabled := 1
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if _, err := database.DB.Exec(
		`INSERT INTO deploy_environment (name, display_name, permission_code, sort_order, enabled) VALUES (?, ?, ?, ?, ?)`,
		req.Name, strings.TrimSpace(req.DisplayName), perm, req.SortOrder, enabled); err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "环境名已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	Audit(r, "environment.create", "deploy_environment", req.Name, nil)
	JSONSuccess(w, nil)
}

// PUT /api/environments/{name}
func HandleUpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	var req envReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	perm := strings.TrimSpace(req.PermissionCode)
	if perm == "" {
		perm = "submit_" + name
	}
	enabled := 1
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if _, err := database.DB.Exec(
		`UPDATE deploy_environment SET display_name=?, permission_code=?, sort_order=?, enabled=? WHERE name=?`,
		strings.TrimSpace(req.DisplayName), perm, req.SortOrder, enabled, name); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "environment.update", "deploy_environment", name, nil)
	JSONSuccess(w, nil)
}

// DELETE /api/environments/{name}
func HandleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	// 有 project_env 用这个 env_type 就不让删
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE env_type=?`, name).Scan(&cnt)
	if cnt > 0 {
		JSONError(w, 40900, "还有项目环境使用该环境类型，不能删除")
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM deploy_environment WHERE name=?`, name); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "environment.delete", "deploy_environment", name, nil)
	JSONSuccess(w, nil)
}
