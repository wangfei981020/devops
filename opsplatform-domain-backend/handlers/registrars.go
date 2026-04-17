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

func maskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + "****"
}

func getConfigByID(id string) (*models.RegistrarConfig, error) {
	row := database.DB.QueryRow(
		`SELECT id, name, provider, COALESCE(api_key,''), COALESCE(api_secret,''), COALESCE(api_user,''), COALESCE(api_token,''), COALESCE(base_url,''), status, COALESCE(DATE_FORMAT(last_sync,'%Y-%m-%d %H:%i:%s'),''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s'), DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s')
		FROM registrar_configs WHERE id=?`, id)

	var c models.RegistrarConfig
	err := row.Scan(&c.ID, &c.Name, &c.Provider, &c.APIKey, &c.APISecret, &c.APIUser, &c.APIToken, &c.BaseURL, &c.Status, &c.LastSync, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func HandleGetRegistrars(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	rows, err := database.DB.Query(
		`SELECT id, name, provider, COALESCE(api_key,''), COALESCE(api_secret,''), COALESCE(api_user,''), COALESCE(api_token,''), COALESCE(base_url,''), status, COALESCE(DATE_FORMAT(last_sync,'%Y-%m-%d %H:%i:%s'),''), DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s'), DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s')
		FROM registrar_configs ORDER BY created_at DESC`)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	defer rows.Close()

	var configs []models.RegistrarConfig
	for rows.Next() {
		var c models.RegistrarConfig
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.APIKey, &c.APISecret, &c.APIUser, &c.APIToken, &c.BaseURL, &c.Status, &c.LastSync, &c.CreatedAt, &c.UpdatedAt); err != nil {
			continue
		}
		// Mask secrets
		c.APIKey = maskSecret(c.APIKey)
		c.APISecret = maskSecret(c.APISecret)
		c.APIToken = maskSecret(c.APIToken)
		configs = append(configs, c)
	}
	if configs == nil {
		configs = []models.RegistrarConfig{}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": configs})
}

func HandleAddRegistrar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var c models.RegistrarConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "请求格式错误"})
		return
	}

	c.ID = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = "active"
	}

	_, err := database.DB.Exec(
		`INSERT INTO registrar_configs (id, name, provider, api_key, api_secret, api_user, api_token, base_url, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Provider, c.APIKey, c.APISecret, c.APIUser, c.APIToken, c.BaseURL, c.Status, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}

	c.APIKey = maskSecret(c.APIKey)
	c.APISecret = maskSecret(c.APISecret)
	c.APIToken = maskSecret(c.APIToken)
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": c, "message": "添加成功"})
}

func HandleUpdateRegistrar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	id := vars["id"]

	var c models.RegistrarConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "请求格式错误"})
		return
	}

	// Get existing config to preserve masked values
	existing, err := getConfigByID(id)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "配置不存在"})
		return
	}

	// If the value contains ****, keep old value
	if containsMask(c.APIKey) {
		c.APIKey = existing.APIKey
	}
	if containsMask(c.APISecret) {
		c.APISecret = existing.APISecret
	}
	if containsMask(c.APIToken) {
		c.APIToken = existing.APIToken
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err = database.DB.Exec(
		`UPDATE registrar_configs SET name=?, provider=?, api_key=?, api_secret=?, api_user=?, api_token=?, base_url=?, status=?, updated_at=? WHERE id=?`,
		c.Name, c.Provider, c.APIKey, c.APISecret, c.APIUser, c.APIToken, c.BaseURL, c.Status, now, id,
	)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "更新成功"})
}

func HandleDeleteRegistrar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := database.DB.Exec("DELETE FROM registrar_configs WHERE id=?", id)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "删除成功"})
}

func HandleTestRegistrar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	vars := mux.Vars(r)
	id := vars["id"]

	config, err := getConfigByID(id)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "配置不存在"})
		return
	}

	var testErr error
	switch config.Provider {
	case "godaddy":
		_, testErr = godaddyListDomains(config, 1, 0)
	case "namecom":
		_, testErr = namecomListDomains(config, 1)
	default:
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "未知注册商类型"})
		return
	}

	if testErr != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "message": "连接测试失败: " + testErr.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "连接测试成功"})
}

func containsMask(s string) bool {
	for i := 0; i < len(s)-3; i++ {
		if s[i] == '*' && s[i+1] == '*' && s[i+2] == '*' && s[i+3] == '*' {
			return true
		}
	}
	return false
}
