package handlers

import (
	"strconv"
	"strings"

	"net/http"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

// GET /api/orchestration/templates?project=&module_type=
// 参照模板列表（体现 项目·模块名(样板服务)·前端/后端）。
func HandleListTemplates(w http.ResponseWriter, r *http.Request) {
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	moduleType := strings.TrimSpace(r.URL.Query().Get("module_type"))

	q := `SELECT id, name, project, module_type, src_env, src_service, description,
	             COALESCE(config,'') , enabled, created_by, created_at, updated_at
	      FROM orchestration_template WHERE 1=1`
	args := []interface{}{}
	if project != "" {
		q += " AND project=?"
		args = append(args, project)
	}
	if moduleType != "" {
		q += " AND module_type=?"
		args = append(args, moduleType)
	}
	q += " ORDER BY project, module_type, name"

	rows, err := database.DB.Query(q, args...)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []models.OrchestrationTemplate{}
	for rows.Next() {
		var t models.OrchestrationTemplate
		_ = rows.Scan(&t.ID, &t.Name, &t.Project, &t.ModuleType, &t.SrcEnv, &t.SrcService,
			&t.Description, &t.Config, &t.Enabled, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
		list = append(list, t)
	}
	JSONSuccess(w, list)
}

type templateReq struct {
	Name        string `json:"name"`
	Project     string `json:"project"`
	ModuleType  string `json:"module_type"`
	SrcEnv      string `json:"src_env"`
	SrcService  string `json:"src_service"`
	Description string `json:"description"`
	Enabled     *int   `json:"enabled"`
}

func (req *templateReq) normalize() {
	req.Name = strings.TrimSpace(req.Name)
	req.Project = strings.TrimSpace(req.Project)
	req.ModuleType = strings.TrimSpace(req.ModuleType)
	req.SrcEnv = strings.TrimSpace(req.SrcEnv)
	req.SrcService = strings.TrimSpace(req.SrcService)
	req.Description = strings.TrimSpace(req.Description)
	// z-kv 模板源目录固定 z-kv-secrets（整份密钥 chart）
	if req.ModuleType == models.ModuleTypeZkv {
		req.SrcService = "z-kv-secrets"
	}
}

func (req *templateReq) validate(w http.ResponseWriter) bool {
	if err := ValidateName(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
		return false
	}
	if req.ModuleType != models.ModuleTypeFrontend && req.ModuleType != models.ModuleTypeBackend && req.ModuleType != models.ModuleTypeZkv {
		JSONError(w, 40001, "module_type 必须是 frontend / backend / zkv")
		return false
	}
	if req.SrcEnv == "" || req.SrcService == "" {
		JSONError(w, 40001, "src_env 和 src_service 必填（指向样板服务）")
		return false
	}
	// 校验样板 env 存在
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE name=?`, req.SrcEnv).Scan(&cnt)
	if cnt == 0 {
		JSONError(w, 40001, "src_env 不存在: "+req.SrcEnv)
		return false
	}
	return true
}

// POST /api/orchestration/templates
func HandleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req templateReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.normalize()
	if !req.validate(w) {
		return
	}
	enabled := 1
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	res, err := database.DB.Exec(
		`INSERT INTO orchestration_template (name, project, module_type, src_env, src_service, description, enabled, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Project, req.ModuleType, req.SrcEnv, req.SrcService, req.Description, enabled, UsernameFromCtx(r))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "模板名已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "orchestration_template.create", "orchestration_template", req.Name, nil)
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/orchestration/templates/{id}
func HandleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req templateReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.normalize()
	if !req.validate(w) {
		return
	}
	enabled := 1
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if _, err := database.DB.Exec(
		`UPDATE orchestration_template SET name=?, project=?, module_type=?, src_env=?, src_service=?, description=?, enabled=? WHERE id=?`,
		req.Name, req.Project, req.ModuleType, req.SrcEnv, req.SrcService, req.Description, enabled, id); err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "模板名已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration_template.update", "orchestration_template", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// DELETE /api/orchestration/templates/{id}
func HandleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if _, err := database.DB.Exec(`DELETE FROM orchestration_template WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "orchestration_template.delete", "orchestration_template", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// LoadTemplate 内部：按 ID 加载模板
func LoadTemplate(id int64) (*models.OrchestrationTemplate, error) {
	var t models.OrchestrationTemplate
	err := database.DB.QueryRow(
		`SELECT id, name, project, module_type, src_env, src_service, description,
		        COALESCE(config,''), enabled, created_by, created_at, updated_at
		 FROM orchestration_template WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.Project, &t.ModuleType, &t.SrcEnv, &t.SrcService,
			&t.Description, &t.Config, &t.Enabled, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
