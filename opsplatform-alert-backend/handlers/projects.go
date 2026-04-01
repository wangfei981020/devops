package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
)

func HandleListProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, parent_id, sort_order, created_at, updated_at
		FROM alert_projects ORDER BY sort_order, id`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, parentID, sortOrder int
		var name, createdAt, updatedAt string
		rows.Scan(&id, &name, &parentID, &sortOrder, &createdAt, &updatedAt)
		list = append(list, map[string]interface{}{
			"id":         id,
			"name":       name,
			"parent_id":  parentID,
			"sort_order": sortOrder,
			"created_at": createdAt,
			"updated_at": updatedAt,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	jsonSuccess(w, list)
}

func HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		ParentID int    `json:"parent_id"`
		SortOrder int   `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "项目名称不能为空")
		return
	}

	result, err := database.DB.Exec("INSERT INTO alert_projects (name, parent_id, sort_order) VALUES (?, ?, ?)",
		req.Name, req.ParentID, req.SortOrder)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	SaveAuditLog(r, "create_project", "project", req.Name, fmt.Sprintf("创建项目 ID=%d", id))
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req struct {
		Name      string `json:"name"`
		ParentID  int    `json:"parent_id"`
		SortOrder int    `json:"sort_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	database.DB.Exec("UPDATE alert_projects SET name=?, parent_id=?, sort_order=? WHERE id=?",
		req.Name, req.ParentID, req.SortOrder, id)
	SaveAuditLog(r, "update_project", "project", req.Name, fmt.Sprintf("更新项目 ID=%d", id))
	jsonSuccess(w, nil)
}

func HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	// Check if any rules use this project
	var ruleCount int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_rules WHERE project_id = ?", id).Scan(&ruleCount)
	if ruleCount > 0 {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("该项目下有 %d 条规则，无法删除", ruleCount))
		return
	}

	// Check if has children
	var childCount int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_projects WHERE parent_id = ?", id).Scan(&childCount)
	if childCount > 0 {
		jsonError(w, http.StatusBadRequest, "该项目下有子项目，无法删除")
		return
	}

	database.DB.Exec("DELETE FROM alert_projects WHERE id = ?", id)
	SaveAuditLog(r, "delete_project", "project", fmt.Sprintf("ID=%d", id), "删除项目")
	jsonSuccess(w, nil)
}
