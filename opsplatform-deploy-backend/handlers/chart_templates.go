package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListChartTemplates(w http.ResponseWriter, r *http.Request) {
	tplType := r.URL.Query().Get("type")
	q := `SELECT id, name, type, description, source_type, git_repo, chart_path, default_values, probe_config, configmap_schema, version, created_at, updated_at FROM chart_templates WHERE 1=1`
	args := []interface{}{}
	if tplType != "" {
		q += " AND type=?"
		args = append(args, tplType)
	}
	q += " ORDER BY id"

	rows, err := database.DB.Query(q, args...)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.ChartTemplate{}
	for rows.Next() {
		var t models.ChartTemplate
		rows.Scan(&t.ID, &t.Name, &t.Type, &t.Description, &t.SourceType, &t.GitRepo, &t.ChartPath,
			&t.DefaultValues, &t.ProbeConfig, &t.ConfigmapSchema, &t.Version, &t.CreatedAt, &t.UpdatedAt)
		list = append(list, t)
	}
	jsonSuccess(w, list)
}

func HandleGetChartTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var t models.ChartTemplate
	err := database.DB.QueryRow(`SELECT id, name, type, description, source_type, git_repo, chart_path, default_values, probe_config, configmap_schema, version, created_at, updated_at FROM chart_templates WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.Type, &t.Description, &t.SourceType, &t.GitRepo, &t.ChartPath,
			&t.DefaultValues, &t.ProbeConfig, &t.ConfigmapSchema, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		jsonError(w, 40400, "模板不存在")
		return
	}
	jsonSuccess(w, t)
}

func HandleCreateChartTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		Type            string `json:"type"`
		Description     string `json:"description"`
		SourceType      string `json:"source_type"`
		GitRepo         string `json:"git_repo"`
		ChartPath       string `json:"chart_path"`
		DefaultValues   string `json:"default_values"`
		ProbeConfig     string `json:"probe_config"`
		ConfigmapSchema string `json:"configmap_schema"`
		Version         string `json:"version"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.Name == "" || req.Type == "" {
		jsonError(w, 40001, "name 和 type 必填")
		return
	}
	if req.SourceType == "" {
		req.SourceType = "git"
	}
	if req.Version == "" {
		req.Version = "v1"
	}
	res, err := database.DB.Exec(`INSERT INTO chart_templates (name, type, description, source_type, git_repo, chart_path, default_values, probe_config, configmap_schema, version) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		req.Name, req.Type, req.Description, req.SourceType, req.GitRepo, req.ChartPath,
		req.DefaultValues, req.ProbeConfig, req.ConfigmapSchema, req.Version)
	if err != nil {
		jsonError(w, 40900, "创建失败(模板名可能已存在): "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateChartTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var req struct {
		Type            string `json:"type"`
		Description     string `json:"description"`
		SourceType      string `json:"source_type"`
		GitRepo         string `json:"git_repo"`
		ChartPath       string `json:"chart_path"`
		DefaultValues   string `json:"default_values"`
		ProbeConfig     string `json:"probe_config"`
		ConfigmapSchema string `json:"configmap_schema"`
		Version         string `json:"version"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	_, err := database.DB.Exec(`UPDATE chart_templates SET type=?, description=?, source_type=?, git_repo=?, chart_path=?, default_values=?, probe_config=?, configmap_schema=?, version=? WHERE id=?`,
		req.Type, req.Description, req.SourceType, req.GitRepo, req.ChartPath,
		req.DefaultValues, req.ProbeConfig, req.ConfigmapSchema, req.Version, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandleDeleteChartTemplate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var c int
	database.DB.QueryRow(`SELECT COUNT(*) FROM modules WHERE template_id=? AND status!='deleted'`, id).Scan(&c)
	if c > 0 {
		jsonError(w, 40900, "模板被模块使用中，无法删除")
		return
	}
	_, err := database.DB.Exec(`DELETE FROM chart_templates WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func HandlePreviewChartTemplate(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "values.yaml 预览渲染待实现 (阶段2)"})
}
