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
// 返回 project 表的所有 + 从 project_env 派生出来但尚未在表里的。
// 数据级权限：非 admin 只看到自己有 env 权限的项目（同 env 过滤逻辑）。
func HandleListProjects(w http.ResponseWriter, r *http.Request) {
	allowedIDs, enforce := AllowedEnvIDs(r)
	if enforce && len(allowedIDs) == 0 {
		// 用户没分到任何 env → 也不能看到任何项目（包括空的）
		JSONSuccess(w, []*projectView{})
		return
	}

	// 1. 从 project_env 派生：只取允许的 env，并算出每个项目的可见 env 数
	envQ := `SELECT name, env_type FROM project_env`
	var envArgs []interface{}
	if enforce {
		ph := strings.Repeat("?,", len(allowedIDs))
		ph = ph[:len(ph)-1]
		envQ += ` WHERE id IN (` + ph + `)`
		for _, id := range allowedIDs {
			envArgs = append(envArgs, id)
		}
	}
	envRows, err := database.DB.Query(envQ, envArgs...)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer envRows.Close()

	byName := map[string]*projectView{}
	list := []*projectView{}
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

	// 2. 从 project 表补 metadata（只补已在 byName 里的；admin 时全部补）。
	//    这样实现：
	//    - admin（不过滤）：所有 project 表行都加进来（即使 env_count=0 也能看到「空项目」）
	//    - 非 admin：只看到他有 env 权限的项目，project 表的额外信息只是补 display_name/description
	projRows, err := database.DB.Query(`SELECT id, name, display_name, description, created_at FROM project ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer projRows.Close()
	for projRows.Next() {
		var id int64
		var name, displayName, description string
		var createdAt time.Time
		_ = projRows.Scan(&id, &name, &displayName, &description, &createdAt)
		if p, ok := byName[name]; ok {
			// 已在派生集合里：补 metadata
			p.ID = id
			p.DisplayName = displayName
			p.Description = description
			p.CreatedAt = createdAt
			p.InDB = true
		} else if !enforce {
			// 不过滤（admin）：把 project 表里的「空项目」也加进来
			np := &projectView{ID: id, Name: name, DisplayName: displayName, Description: description, CreatedAt: createdAt, InDB: true}
			byName[name] = np
			list = append(list, np)
		}
		// 非 admin 且这个 project 不在用户允许 env 派生集里 → 跳过（这就是新加的过滤）
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
	if err := ValidateName(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
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
	Audit(r, "project.create", "project", req.Name, nil)
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
	Audit(r, "project.update", "project", strconv.FormatInt(id, 10), nil)
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
	Audit(r, "project.delete", "project", name, nil)
	JSONSuccess(w, nil)
}

