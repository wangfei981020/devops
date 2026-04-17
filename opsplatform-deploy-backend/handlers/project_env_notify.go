package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleGetProjectEnvNotify(w http.ResponseWriter, r *http.Request) {
	peID, _ := strconv.ParseInt(r.URL.Query().Get("project_env_id"), 10, 64)
	if peID == 0 {
		jsonError(w, 40001, "project_env_id 必填")
		return
	}
	var n models.ProjectEnvNotify
	err := database.DB.QueryRow(`SELECT id, project_env_id, lark_config_id, IFNULL(contact_ids,''), updated_at FROM project_env_notify WHERE project_env_id=?`, peID).
		Scan(&n.ID, &n.ProjectEnvID, &n.LarkConfigID, &n.ContactIDs, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		jsonSuccess(w, models.ProjectEnvNotify{ProjectEnvID: peID, ContactIDs: "[]"})
		return
	}
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, n)
}

func HandleSetProjectEnvNotify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectEnvID int64  `json:"project_env_id"`
		LarkConfigID int64  `json:"lark_config_id"`
		ContactIDs   string `json:"contact_ids"` // JSON 数组
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.ProjectEnvID == 0 {
		jsonError(w, 40001, "project_env_id 必填")
		return
	}
	if req.ContactIDs == "" {
		req.ContactIDs = "[]"
	}
	_, err := database.DB.Exec(`INSERT INTO project_env_notify (project_env_id, lark_config_id, contact_ids) VALUES (?,?,?)
		ON DUPLICATE KEY UPDATE lark_config_id=VALUES(lark_config_id), contact_ids=VALUES(contact_ids)`,
		req.ProjectEnvID, req.LarkConfigID, req.ContactIDs)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}
