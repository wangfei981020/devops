package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListEnvTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, env_vars, description, created_at, updated_at FROM env_templates ORDER BY id`)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.EnvTemplate{}
	for rows.Next() {
		var e models.EnvTemplate
		rows.Scan(&e.ID, &e.Name, &e.EnvVars, &e.Description, &e.CreatedAt, &e.UpdatedAt)
		list = append(list, e)
	}
	jsonSuccess(w, list)
}

func HandleCreateEnvTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		EnvVars     string `json:"env_vars"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.Name == "" {
		jsonError(w, 40001, "模板名不能为空")
		return
	}
	res, err := database.DB.Exec(`INSERT INTO env_templates (name, env_vars, description) VALUES (?,?,?)`,
		req.Name, req.EnvVars, req.Description)
	if err != nil {
		jsonError(w, 40900, "创建失败: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateEnvTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var req struct {
		EnvVars     string `json:"env_vars"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	_, err := database.DB.Exec(`UPDATE env_templates SET env_vars=?, description=? WHERE id=?`,
		req.EnvVars, req.Description, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleDeleteEnvTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	_, err := database.DB.Exec(`DELETE FROM env_templates WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}
