package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/models"
)

// Engine interface for rule management
var ruleEngine interface {
	ReloadRule(ruleID int) error
	RemoveRule(ruleID int)
}

func SetRuleEngine(e interface {
	ReloadRule(ruleID int) error
	RemoveRule(ruleID int)
}) {
	ruleEngine = e
}

func HandleListAlertRules(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Count total
	countQuery := "SELECT COUNT(*) FROM alert_rules WHERE 1=1"
	args := []interface{}{}

	if status != "" {
		countQuery += " AND status = ?"
		s, _ := strconv.Atoi(status)
		args = append(args, s)
	}
	if search != "" {
		countQuery += " AND (name LIKE ? OR keyword LIKE ?)"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}

	var total int64
	database.DB.QueryRow(countQuery, args...).Scan(&total)

	// Query with joins
	query := `SELECT r.id, r.name, r.es_connection_id, r.lark_config_id, r.es_index,
		r.schedule, r.time_range, COALESCE(r.query_dsl,''), r.keyword,
		COALESCE(r.filter_fields,''), COALESCE(r.extract_fields,''),
		r.message_title, COALESCE(r.message_template,''),
		COALESCE(r.at_users,''), r.at_all, r.severity,
		r.dedup_field, r.dedup_ttl, r.max_alerts, COALESCE(r.prometheus_config,''), r.status,
		r.last_run_at, r.last_error, r.created_at, r.updated_at,
		COALESCE(e.name,'(已删除)') as es_name, COALESCE(l.name,'(已删除)') as lark_name
		FROM alert_rules r
		LEFT JOIN es_connections e ON r.es_connection_id = e.id
		LEFT JOIN lark_configs l ON r.lark_config_id = l.id
		WHERE 1=1`

	queryArgs := []interface{}{}
	if status != "" {
		query += " AND r.status = ?"
		s, _ := strconv.Atoi(status)
		queryArgs = append(queryArgs, s)
	}
	if search != "" {
		query += " AND (r.name LIKE ? OR r.keyword LIKE ?)"
		queryArgs = append(queryArgs, "%"+search+"%", "%"+search+"%")
	}

	query += " ORDER BY r.id DESC LIMIT ? OFFSET ?"
	queryArgs = append(queryArgs, limit, offset)

	rows, err := database.DB.Query(query, queryArgs...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var rule models.AlertRule
		var esName, larkName string
		err := rows.Scan(&rule.ID, &rule.Name, &rule.ESConnectionID, &rule.LarkConfigID,
			&rule.ESIndex, &rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
			&rule.Keyword, &rule.FilterFields, &rule.ExtractFields,
			&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
			&rule.Severity, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
			&rule.PrometheusConfig, &rule.Status, &rule.LastRunAt, &rule.LastError,
			&rule.CreatedAt, &rule.UpdatedAt, &esName, &larkName)
		if err != nil {
			continue
		}

		item := map[string]interface{}{
			"id":                rule.ID,
			"name":              rule.Name,
			"es_connection_id":  rule.ESConnectionID,
			"lark_config_id":    rule.LarkConfigID,
			"es_index":          rule.ESIndex,
			"schedule":          rule.Schedule,
			"time_range":        rule.TimeRange,
			"query_dsl":         rule.QueryDSL,
			"keyword":           rule.Keyword,
			"filter_fields":     rule.FilterFields,
			"extract_fields":    rule.ExtractFields,
			"message_title":     rule.MessageTitle,
			"message_template":  rule.MessageTemplate,
			"at_users":          rule.AtUsers,
			"at_all":            rule.AtAll,
			"severity":          rule.Severity,
			"dedup_field":       rule.DedupField,
			"dedup_ttl":         rule.DedupTTL,
			"max_alerts":         rule.MaxAlerts,
			"prometheus_config":  rule.PrometheusConfig,
			"status":             rule.Status,
			"created_at":        rule.CreatedAt,
			"updated_at":        rule.UpdatedAt,
			"es_connection_name": esName,
			"lark_config_name":  larkName,
		}

		if rule.LastRunAt.Valid {
			item["last_run_at"] = rule.LastRunAt.Time
		}
		if rule.LastError.Valid {
			item["last_error"] = rule.LastError.String
		}

		list = append(list, item)
	}
	if list == nil {
		list = []map[string]interface{}{}
	}

	jsonPaginated(w, list, total, page, limit)
}

func HandleGetAlertRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var rule models.AlertRule
	err := database.DB.QueryRow(`SELECT id, name, es_connection_id, lark_config_id, es_index,
		schedule, time_range, COALESCE(query_dsl,''), keyword,
		COALESCE(filter_fields,''), COALESCE(extract_fields,''),
		message_title, COALESCE(message_template,''),
		COALESCE(at_users,''), at_all, severity,
		dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''),
		status, last_run_at, last_error, created_at, updated_at
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.ESConnectionID, &rule.LarkConfigID,
		&rule.ESIndex, &rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.Severity, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
		&rule.PrometheusConfig, &rule.Status, &rule.LastRunAt, &rule.LastError,
		&rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		jsonError(w, http.StatusNotFound, "规则不存在")
		return
	}

	jsonSuccess(w, rule)
}

func HandleCreateAlertRule(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAlertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Name == "" {
		jsonError(w, http.StatusBadRequest, "规则名称不能为空")
		return
	}
	if req.ESConnectionID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择ES连接")
		return
	}
	if req.LarkConfigID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择Lark配置")
		return
	}

	// Set defaults
	if req.Schedule == "" {
		req.Schedule = "*/5 * * * *"
	}
	if req.TimeRange == "" {
		req.TimeRange = "5m"
	}
	if req.ESIndex == "" {
		req.ESIndex = "*"
	}
	if req.Severity == "" {
		req.Severity = "warning"
	}
	if req.DedupTTL == 0 {
		req.DedupTTL = 3600
	}
	if req.MaxAlerts == 0 {
		req.MaxAlerts = 10
	}

	result, err := database.DB.Exec(`INSERT INTO alert_rules
		(name, es_connection_id, lark_config_id, es_index, schedule, time_range,
		query_dsl, keyword, filter_fields, extract_fields,
		message_title, message_template, at_users, at_all, severity,
		dedup_field, dedup_ttl, max_alerts, prometheus_config, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		req.Name, req.ESConnectionID, req.LarkConfigID, req.ESIndex,
		req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll, req.Severity,
		req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()

	// Reload in engine
	if ruleEngine != nil {
		ruleEngine.ReloadRule(int(id))
	}

	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var req models.CreateAlertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE alert_rules SET
		name=?, es_connection_id=?, lark_config_id=?, es_index=?,
		schedule=?, time_range=?, query_dsl=?, keyword=?,
		filter_fields=?, extract_fields=?, message_title=?,
		message_template=?, at_users=?, at_all=?, severity=?,
		dedup_field=?, dedup_ttl=?, max_alerts=?, prometheus_config=?
		WHERE id=?`,
		req.Name, req.ESConnectionID, req.LarkConfigID, req.ESIndex,
		req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll, req.Severity,
		req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}

	// Reload in engine
	if ruleEngine != nil {
		ruleEngine.ReloadRule(id)
	}

	jsonSuccess(w, nil)
}

func HandleDeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	// Remove from engine first
	if ruleEngine != nil {
		ruleEngine.RemoveRule(id)
	}

	database.DB.Exec("DELETE FROM alert_rules WHERE id = ?", id)
	jsonSuccess(w, nil)
}

func HandleToggleAlertRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	database.DB.Exec("UPDATE alert_rules SET status = IF(status=1, 0, 1) WHERE id = ?", id)

	// Reload in engine (will add or remove based on new status)
	if ruleEngine != nil {
		ruleEngine.ReloadRule(id)
	}

	// Return new status
	var status int
	database.DB.QueryRow("SELECT status FROM alert_rules WHERE id = ?", id).Scan(&status)
	jsonSuccess(w, map[string]interface{}{"status": status})
}

// HandleRunAlertRule manually triggers an alert rule
func HandleRunAlertRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	// Check rule exists
	var status int
	err := database.DB.QueryRow("SELECT status FROM alert_rules WHERE id = ?", id).Scan(&status)
	if err == sql.ErrNoRows {
		jsonError(w, http.StatusNotFound, "规则不存在")
		return
	}

	// Trigger execution in a goroutine
	if ruleEngine != nil {
		go func() {
			if engine, ok := ruleEngine.(interface{ ExecuteRule(int) }); ok {
				engine.ExecuteRule(id)
			}
		}()
	}

	jsonSuccess(w, map[string]string{"message": "规则已触发执行"})
}
