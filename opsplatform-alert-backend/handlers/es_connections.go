package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	"opsplatform-alert-backend/models"
)

var alertEngine interface {
	RefreshESClient(connID int)
}

func SetAlertEngine(e interface {
	RefreshESClient(connID int)
}) {
	alertEngine = e
}

func HandleListESConnections(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT id, name, url, version, username, skip_tls_verify, description, status, created_at, updated_at FROM es_connections ORDER BY id DESC")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []models.ESConnection
	for rows.Next() {
		var c models.ESConnection
		rows.Scan(&c.ID, &c.Name, &c.URL, &c.Version, &c.Username, &c.SkipTLSVerify, &c.Description, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		list = append(list, c)
	}
	if list == nil {
		list = []models.ESConnection{}
	}
	jsonSuccess(w, list)
}

func HandleCreateESConnection(w http.ResponseWriter, r *http.Request) {
	var req models.CreateESConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Name == "" || req.URL == "" {
		jsonError(w, http.StatusBadRequest, "名称和URL不能为空")
		return
	}
	if req.Version == "" {
		req.Version = "7"
	}

	result, err := database.DB.Exec(`INSERT INTO es_connections (name, url, version, username, password, api_key, skip_tls_verify, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, req.Name, req.URL, req.Version, req.Username, req.Password, req.APIKey, req.SkipTLSVerify, req.Description)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateESConnection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req models.CreateESConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE es_connections SET name=?, url=?, version=?, username=?, password=?, api_key=?, skip_tls_verify=?, description=? WHERE id=?`,
		req.Name, req.URL, req.Version, req.Username, req.Password, req.APIKey, req.SkipTLSVerify, req.Description, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	// Refresh ES client cache
	if alertEngine != nil {
		alertEngine.RefreshESClient(id)
	}

	jsonSuccess(w, nil)
}

func HandleDeleteESConnection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	// Check if any rules use this connection
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_rules WHERE es_connection_id = ?", id).Scan(&count)
	if count > 0 {
		jsonError(w, http.StatusBadRequest, "该连接正在被告警规则使用，无法删除")
		return
	}

	database.DB.Exec("DELETE FROM es_connections WHERE id = ?", id)
	if alertEngine != nil {
		alertEngine.RefreshESClient(id)
	}
	jsonSuccess(w, nil)
}

func HandleToggleESConnection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	database.DB.Exec("UPDATE es_connections SET status = IF(status=1, 0, 1) WHERE id = ?", id)
	if alertEngine != nil {
		alertEngine.RefreshESClient(id)
	}
	jsonSuccess(w, nil)
}

func HandleTestESConnection(w http.ResponseWriter, r *http.Request) {
	var req models.CreateESConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	conn := models.ESConnection{
		URL:           req.URL,
		Version:       req.Version,
		Username:      req.Username,
		Password:      req.Password,
		APIKey:        req.APIKey,
		SkipTLSVerify: req.SkipTLSVerify,
	}

	client, err := es.NewClient(conn)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "创建客户端失败: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		jsonError(w, http.StatusBadRequest, "连接失败: "+err.Error())
		return
	}

	jsonSuccess(w, map[string]string{"status": "connected"})
}
