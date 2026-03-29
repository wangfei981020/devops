package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
)

func HandleListLokiConnections(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT id, name, url, username, org_id, skip_tls_verify, description, status, created_at, updated_at FROM loki_connections ORDER BY id DESC")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var list []models.LokiConnection
	for rows.Next() {
		var c models.LokiConnection
		rows.Scan(&c.ID, &c.Name, &c.URL, &c.Username, &c.OrgID, &c.SkipTLSVerify, &c.Description, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		list = append(list, c)
	}
	if list == nil {
		list = []models.LokiConnection{}
	}
	jsonSuccess(w, list)
}

func HandleCreateLokiConnection(w http.ResponseWriter, r *http.Request) {
	var req models.CreateLokiConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Name == "" || req.URL == "" {
		jsonError(w, http.StatusBadRequest, "名称和URL不能为空")
		return
	}

	result, err := database.DB.Exec(`INSERT INTO loki_connections (name, url, username, password, org_id, skip_tls_verify, description)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, req.Name, req.URL, req.Username, req.Password, req.OrgID, req.SkipTLSVerify, req.Description)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateLokiConnection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var req models.CreateLokiConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE loki_connections SET name=?, url=?, username=?, password=?, org_id=?, skip_tls_verify=?, description=? WHERE id=?`,
		req.Name, req.URL, req.Username, req.Password, req.OrgID, req.SkipTLSVerify, req.Description, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	jsonSuccess(w, nil)
}

func HandleDeleteLokiConnection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_rules WHERE loki_connection_id = ? AND data_source_type = 'loki'", id).Scan(&count)
	if count > 0 {
		jsonError(w, http.StatusBadRequest, "该连接正在被告警规则使用，无法删除")
		return
	}

	database.DB.Exec("DELETE FROM loki_connections WHERE id = ?", id)
	jsonSuccess(w, nil)
}

func HandleToggleLokiConnection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	database.DB.Exec("UPDATE loki_connections SET status = IF(status=1, 0, 1) WHERE id = ?", id)
	jsonSuccess(w, nil)
}

func HandleTestLokiConnection(w http.ResponseWriter, r *http.Request) {
	var req models.CreateLokiConnectionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	conn := models.LokiConnection{
		URL:           req.URL,
		Username:      req.Username,
		Password:      req.Password,
		OrgID:         req.OrgID,
		SkipTLSVerify: req.SkipTLSVerify,
	}

	client := lokiclient.NewClient(conn)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		jsonError(w, http.StatusBadRequest, "连接失败: "+err.Error())
		return
	}
	jsonSuccess(w, map[string]string{"status": "connected"})
}

// HandleLokiLabels returns available labels
func HandleLokiLabels(w http.ResponseWriter, r *http.Request) {
	connID, _ := strconv.Atoi(r.URL.Query().Get("loki_connection_id"))
	if connID == 0 {
		jsonError(w, http.StatusBadRequest, "请指定 Loki 连接")
		return
	}

	conn := getLokiConn(connID)
	if conn == nil {
		jsonError(w, http.StatusBadRequest, "Loki 连接不存在")
		return
	}

	client := lokiclient.NewClient(*conn)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	labels, err := client.Labels(ctx)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "获取标签失败: "+err.Error())
		return
	}
	jsonSuccess(w, labels)
}

// HandleLokiLabelValues returns values for a specific label
func HandleLokiLabelValues(w http.ResponseWriter, r *http.Request) {
	connID, _ := strconv.Atoi(r.URL.Query().Get("loki_connection_id"))
	label := r.URL.Query().Get("label")
	if connID == 0 || label == "" {
		jsonError(w, http.StatusBadRequest, "请指定 Loki 连接和 label")
		return
	}

	conn := getLokiConn(connID)
	if conn == nil {
		jsonError(w, http.StatusBadRequest, "Loki 连接不存在")
		return
	}

	client := lokiclient.NewClient(*conn)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	values, err := client.LabelValues(ctx, label)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "获取标签值失败: "+err.Error())
		return
	}
	jsonSuccess(w, values)
}

func getLokiConn(id int) *models.LokiConnection {
	var conn models.LokiConnection
	err := database.DB.QueryRow(`SELECT id, name, url, username, password, org_id, skip_tls_verify
		FROM loki_connections WHERE id = ?`, id).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Username, &conn.Password, &conn.OrgID, &conn.SkipTLSVerify)
	if err != nil {
		return nil
	}
	return &conn
}
