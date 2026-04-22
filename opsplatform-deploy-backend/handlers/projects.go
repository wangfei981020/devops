package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
)

// Project 聚合视图（project 表 + 从 project_env 派生的项目）
type projectView struct {
	ID          int64     `json:"id,omitempty"` // 0 表示仅从 project_env 派生（未在 project 表注册）
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description,omitempty"`
	EnvCount    int       `json:"env_count"`
	InDB        bool      `json:"in_db"` // 是否在 project 表里
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// GET /api/projects
// 返回 project 表的所有 + 从 project_env 派生出来但尚未在表里的
func HandleListProjects(w http.ResponseWriter, r *http.Request) {
	// 1. 从 project 表读
	rows, err := database.DB.Query(`SELECT id, name, display_name, description, created_at FROM project ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()

	byName := map[string]*projectView{}
	list := []*projectView{}
	for rows.Next() {
		p := &projectView{InDB: true}
		_ = rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.Description, &p.CreatedAt)
		byName[p.Name] = p
		list = append(list, p)
	}

	// 2. 从 project_env 派生项目名（name = {project}-{env_type}）
	envRows, err := database.DB.Query(`SELECT name, env_type FROM project_env`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer envRows.Close()
	for envRows.Next() {
		var peName, envType string
		_ = envRows.Scan(&peName, &envType)
		projName := strings.TrimSuffix(peName, "-"+envType)
		if p, ok := byName[projName]; ok {
			p.EnvCount++
		} else {
			p := &projectView{Name: projName, EnvCount: 1, InDB: false}
			byName[projName] = p
			list = append(list, p)
		}
	}

	// 按 name 排序
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].Name < list[i].Name {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	JSONSuccess(w, list)
}

type createProjectReq struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// POST /api/projects
func HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		JSONError(w, 40001, "name 必填")
		return
	}
	// name 不能包含短横线后面接 env_type（避免和 project_env 的 name 规则冲突）
	if strings.Contains(req.Name, " ") {
		JSONError(w, 40001, "name 不能包含空格")
		return
	}
	res, err := database.DB.Exec(`INSERT INTO project (name, display_name, description) VALUES (?, ?, ?)`,
		req.Name, strings.TrimSpace(req.DisplayName), strings.TrimSpace(req.Description))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "项目名已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	JSONSuccess(w, map[string]interface{}{"id": id, "name": req.Name})
}

type updateProjectReq struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// PUT /api/projects/{id}
// 只能改 display_name 和 description。name 不可改（改 name 意味着批量 rename 所有 envs，不支持）
func HandleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req updateProjectReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	_, err := database.DB.Exec(`UPDATE project SET display_name=?, description=? WHERE id=?`,
		strings.TrimSpace(req.DisplayName), strings.TrimSpace(req.Description), id)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	JSONSuccess(w, nil)
}

// DELETE /api/projects/{id}
// 如果该项目下还有 env，拒绝删除
func HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var name string
	err := database.DB.QueryRow(`SELECT name FROM project WHERE id=?`, id).Scan(&name)
	if err == sql.ErrNoRows {
		JSONError(w, 40400, "project not found")
		return
	}
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE name LIKE ?`, name+"-%").Scan(&cnt)
	if cnt > 0 {
		JSONError(w, 40900, "该项目下还有 "+strconv.Itoa(cnt)+" 个环境，请先删除所有环境")
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM project WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	JSONSuccess(w, nil)
}

