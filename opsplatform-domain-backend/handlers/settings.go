package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"domain-platform/database"
	"domain-platform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := database.DB.Query("SELECT setting_key, COALESCE(setting_value,''), COALESCE(DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s'),'') FROM global_settings")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v, u string
		rows.Scan(&k, &v, &u)
		settings[k] = v
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": settings})
}

func HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "请求格式错误"})
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	for k, v := range body.Settings {
		_, err := database.DB.Exec(
			`INSERT INTO global_settings (setting_key, setting_value, updated_at) VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE setting_value=VALUES(setting_value), updated_at=VALUES(updated_at)`,
			k, v, now,
		)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "保存成功"})
}

func HandleGetOverrides(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := database.DB.Query(
		`SELECT id, root_domain, COALESCE(exclude_hosts,''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s'), DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s')
		FROM domain_overrides ORDER BY root_domain ASC`)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	defer rows.Close()

	var overrides []models.DomainOverride
	for rows.Next() {
		var o models.DomainOverride
		rows.Scan(&o.ID, &o.RootDomain, &o.ExcludeHosts, &o.CreatedAt, &o.UpdatedAt)
		overrides = append(overrides, o)
	}
	if overrides == nil {
		overrides = []models.DomainOverride{}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": overrides})
}

func HandleSaveOverride(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var body struct {
		RootDomain   string   `json:"root_domain"`
		ExcludeHosts []string `json:"exclude_hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "请求格式错误"})
		return
	}

	hostsJSON, _ := json.Marshal(body.ExcludeHosts)
	now := time.Now().Format("2006-01-02 15:04:05")

	// Check if exists by root_domain
	var existingID string
	database.DB.QueryRow("SELECT id FROM domain_overrides WHERE root_domain=?", body.RootDomain).Scan(&existingID)

	if existingID != "" {
		_, err := database.DB.Exec(
			"UPDATE domain_overrides SET exclude_hosts=?, updated_at=? WHERE id=?",
			string(hostsJSON), now, existingID,
		)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}
	} else {
		newID := uuid.New().String()
		_, err := database.DB.Exec(
			"INSERT INTO domain_overrides (id, root_domain, exclude_hosts, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			newID, body.RootDomain, string(hostsJSON), now, now,
		)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
			return
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "保存成功"})
}

func HandleDeleteOverride(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := database.DB.Exec("DELETE FROM domain_overrides WHERE id=?", id)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "删除成功"})
}
