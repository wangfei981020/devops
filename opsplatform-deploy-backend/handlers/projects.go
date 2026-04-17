package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, display_name, description, created_at, updated_at FROM projects ORDER BY id`)
	if err != nil {
		jsonError(w, 50000, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	list := []models.Project{}
	for rows.Next() {
		var p models.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			continue
		}
		list = append(list, p)
	}
	jsonSuccess(w, list)
}

func HandleGetProject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var p models.Project
	err := database.DB.QueryRow(`SELECT id, name, display_name, description, created_at, updated_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.DisplayName, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		jsonError(w, 40400, "项目不存在")
		return
	}
	jsonSuccess(w, p)
}

func HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.Name == "" {
		jsonError(w, 40001, "项目名不能为空")
		return
	}

	res, err := database.DB.Exec(`INSERT INTO projects (name, display_name, description) VALUES (?,?,?)`,
		req.Name, req.DisplayName, req.Description)
	if err != nil {
		jsonError(w, 40900, "创建失败(项目名可能已存在): "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var req struct {
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	_, err := database.DB.Exec(`UPDATE projects SET display_name=?, description=? WHERE id=?`,
		req.DisplayName, req.Description, id)
	if err != nil {
		jsonError(w, 50000, "更新失败: "+err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	// 检查关联 project_envs
	var c int
	database.DB.QueryRow(`SELECT COUNT(*) FROM project_envs WHERE project_id=?`, id).Scan(&c)
	if c > 0 {
		jsonError(w, 40900, "项目下存在环境配置，请先删除")
		return
	}
	_, err := database.DB.Exec(`DELETE FROM projects WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, "删除失败: "+err.Error())
		return
	}
	jsonSuccess(w, nil)
}
