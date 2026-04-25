package handlers

import (
	"net/http"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListModules(w http.ResponseWriter, r *http.Request) {
	peIDStr := r.URL.Query().Get("project_env_id")
	if peIDStr == "" {
		JSONError(w, 40001, "project_env_id required")
		return
	}
	if !IsEnvIDAllowed(r, ParseID(peIDStr)) {
		JSONError(w, 40300, "无权访问该环境")
		return
	}
	rows, err := database.DB.Query(`SELECT id, project_env_id, name, current_tag, image_repository, argocd_app_name,
		IFNULL(namespace,''), last_scanned_at, created_at, updated_at FROM module WHERE project_env_id=? ORDER BY name`, peIDStr)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []models.Module{}
	for rows.Next() {
		var m models.Module
		_ = rows.Scan(&m.ID, &m.ProjectEnvID, &m.Name, &m.CurrentTag, &m.ImageRepository, &m.ArgocdAppName,
			&m.Namespace, &m.LastScannedAt, &m.CreatedAt, &m.UpdatedAt)
		list = append(list, m)
	}
	JSONSuccess(w, list)
}

func HandleModuleTagHistory(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	limit := int64(10)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n := ParseID(v); n > 0 && n <= 50 {
			limit = n
		}
	}
	var name string
	var peID int64
	if err := database.DB.QueryRow(`SELECT name, project_env_id FROM module WHERE id=?`, id).Scan(&name, &peID); err != nil {
		JSONError(w, 40400, "module not found")
		return
	}
	if !IsEnvIDAllowed(r, peID) {
		JSONError(w, 40300, "无权访问该模块所在环境")
		return
	}
	rows, err := database.DB.Query(`SELECT id, changes, created_at FROM deployment
		WHERE project_env_id=? AND action IN ('update_image','rollback') AND status IN ('success','partial')
		ORDER BY created_at DESC`, peID)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	type entry struct {
		Tag          string `json:"tag"`
		DeploymentID int64  `json:"deployment_id"`
		CreatedAt    string `json:"created_at"`
	}
	seen := map[string]bool{}
	out := []entry{}
	for rows.Next() {
		var did int64
		var raw []byte
		var created string
		_ = rows.Scan(&did, &raw, &created)
		var changes []models.Change
		_ = jsonUnmarshalImpl(raw, &changes)
		for _, c := range changes {
			if c.Module != name || seen[c.ToTag] {
				continue
			}
			seen[c.ToTag] = true
			out = append(out, entry{Tag: c.ToTag, DeploymentID: did, CreatedAt: created})
			if int64(len(out)) >= limit {
				JSONSuccess(w, out)
				return
			}
		}
	}
	JSONSuccess(w, out)
}
