package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	lokiclient "opsplatform-alert-backend/loki"
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
	query := `SELECT r.id, r.name, COALESCE(r.data_source_type,'es'), r.es_connection_id,
		COALESCE(r.loki_connection_id,0), r.lark_config_id, r.es_index,
		r.schedule, r.time_range, COALESCE(r.query_dsl,''), r.keyword, COALESCE(r.logql,''),
		COALESCE(r.filter_fields,''), COALESCE(r.extract_fields,''),
		r.message_title, COALESCE(r.message_template,''),
		COALESCE(r.at_users,''), r.at_all, COALESCE(r.alert_mode,'found'),
		r.recovery_enabled, COALESCE(r.recovery_title,''), COALESCE(r.recovery_template,''),
		r.severity, COALESCE(r.group_by,''), r.dedup_field, r.dedup_ttl, r.max_alerts, COALESCE(r.prometheus_config,''), r.status,
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
		err := rows.Scan(&rule.ID, &rule.Name, &rule.DataSourceType, &rule.ESConnectionID,
			&rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
			&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
			&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
			&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
			&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
			&rule.Severity, &rule.GroupBy, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
			&rule.PrometheusConfig, &rule.Status, &rule.LastRunAt, &rule.LastError,
			&rule.CreatedAt, &rule.UpdatedAt, &esName, &larkName)
		if err != nil {
			continue
		}

		item := map[string]interface{}{
			"id":                 rule.ID,
			"name":               rule.Name,
			"data_source_type":   rule.DataSourceType,
			"es_connection_id":   rule.ESConnectionID,
			"loki_connection_id": rule.LokiConnectionID,
			"lark_config_id":     rule.LarkConfigID,
			"es_index":          rule.ESIndex,
			"schedule":          rule.Schedule,
			"time_range":        rule.TimeRange,
			"query_dsl":         rule.QueryDSL,
			"keyword":           rule.Keyword,
			"logql":             rule.LogQL,
			"filter_fields":     rule.FilterFields,
			"extract_fields":    rule.ExtractFields,
			"message_title":     rule.MessageTitle,
			"message_template":  rule.MessageTemplate,
			"at_users":          rule.AtUsers,
			"at_all":            rule.AtAll,
			"alert_mode":         rule.AlertMode,
			"recovery_enabled":   rule.RecoveryEnabled,
			"recovery_title":     rule.RecoveryTitle,
			"recovery_template":  rule.RecoveryTemplate,
			"severity":           rule.Severity,
			"group_by":          rule.GroupBy,
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
	err := database.DB.QueryRow(`SELECT id, name, COALESCE(data_source_type,'es'), es_connection_id,
		COALESCE(loki_connection_id,0), lark_config_id, es_index,
		schedule, time_range, COALESCE(query_dsl,''), keyword, COALESCE(logql,''),
		COALESCE(filter_fields,''), COALESCE(extract_fields,''),
		message_title, COALESCE(message_template,''),
		COALESCE(at_users,''), at_all, COALESCE(alert_mode,'found'),
		recovery_enabled, COALESCE(recovery_title,''), COALESCE(recovery_template,''),
		severity, COALESCE(group_by,''), dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''),
		status, last_run_at, last_error, created_at, updated_at
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.DataSourceType, &rule.ESConnectionID,
		&rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
		&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
		&rule.Severity, &rule.GroupBy, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
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
		(name, data_source_type, es_connection_id, loki_connection_id, lark_config_id,
		es_index, schedule, time_range,
		query_dsl, keyword, logql, filter_fields, extract_fields,
		message_title, message_template, at_users, at_all,
		alert_mode, recovery_enabled, recovery_title, recovery_template,
		severity, group_by, dedup_field, dedup_ttl, max_alerts, prometheus_config, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, req.LarkConfigID,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig)
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
		name=?, data_source_type=?, es_connection_id=?, loki_connection_id=?, lark_config_id=?,
		es_index=?, schedule=?, time_range=?, query_dsl=?, keyword=?, logql=?,
		filter_fields=?, extract_fields=?, message_title=?,
		message_template=?, at_users=?, at_all=?,
		alert_mode=?, recovery_enabled=?, recovery_title=?, recovery_template=?,
		severity=?, group_by=?, dedup_field=?, dedup_ttl=?, max_alerts=?, prometheus_config=?
		WHERE id=?`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, req.LarkConfigID,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig, id)
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

// HandlePreviewAlertRule executes query and renders template without sending to Lark
func HandlePreviewAlertRule(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAlertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	maxAlerts := req.MaxAlerts
	if maxAlerts <= 0 {
		maxAlerts = 10
	}
	timeRange := req.TimeRange
	if timeRange == "" {
		timeRange = "5m"
	}

	type PreviewHit struct {
		Raw      map[string]interface{} `json:"raw"`
		Vars     map[string]interface{} `json:"vars"`
		Rendered string                 `json:"rendered"`
	}

	var rawHits []map[string]interface{}
	var total int64
	var queryStr string
	var sourceName, sourceDetail string

	dsType := req.DataSourceType
	if dsType == "" {
		dsType = "es"
	}

	if dsType == "loki" {
		// Loki preview
		if req.LokiConnectionID == 0 {
			jsonError(w, http.StatusBadRequest, "请选择 Loki 连接")
			return
		}
		if req.LogQL == "" {
			jsonError(w, http.StatusBadRequest, "LogQL 查询不能为空")
			return
		}

		conn := getLokiConn(req.LokiConnectionID)
		if conn == nil {
			jsonError(w, http.StatusBadRequest, "Loki 连接不存在")
			return
		}

		client := lokiclient.NewClient(*conn)

		now := time.Now()
		duration := parsePreviewTimeRange(timeRange)
		start := now.Add(-duration)

		result, err := client.QueryRange(ctx, req.LogQL, start, now, maxAlerts)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "Loki 查询失败: "+err.Error())
			return
		}

		rawHits = result.ToHits()
		total = int64(result.Total)
		queryStr = req.LogQL
		sourceName = conn.Name
		sourceDetail = "Loki"
	} else {
		// ES preview
		if req.ESConnectionID == 0 {
			jsonError(w, http.StatusBadRequest, "请选择 ES 连接")
			return
		}

		var conn models.ESConnection
		err := database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key, skip_tls_verify
			FROM es_connections WHERE id = ?`, req.ESConnectionID).Scan(
			&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey, &conn.SkipTLSVerify)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "ES 连接不存在")
			return
		}

		client, err := es.NewClient(conn)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "ES 客户端创建失败: "+err.Error())
			return
		}

		esIndex := req.ESIndex
		if esIndex == "" {
			esIndex = "*"
		}

		query, err := es.BuildQuery(req.Keyword, req.FilterFields, timeRange, req.QueryDSL, maxAlerts)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "查询构建失败: "+err.Error())
			return
		}

		result, err := client.Search(ctx, esIndex, query)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "ES 查询失败: "+err.Error())
			return
		}

		rawHits = result.Hits
		total = result.Total
		queryJSON, _ := json.MarshalIndent(query, "", "  ")
		queryStr = string(queryJSON)
		sourceName = conn.Name
		sourceDetail = fmt.Sprintf("ES %s.x | %s", conn.Version, esIndex)
	}

	// Process hits
	var hits []PreviewHit
	for _, hit := range rawHits {
		vars := previewExtractFields(hit, req.ExtractFields)
		rendered := previewRenderTemplate(req.MessageTemplate, vars)
		hits = append(hits, PreviewHit{Raw: hit, Vars: vars, Rendered: rendered})
	}

	jsonSuccess(w, map[string]interface{}{
		"total":         total,
		"hit_count":     len(rawHits),
		"hits":          hits,
		"query":         queryStr,
		"source_name":   sourceName,
		"source_detail": sourceDetail,
		"data_source":   dsType,
	})
}

func parsePreviewTimeRange(s string) time.Duration {
	if strings.HasSuffix(s, "d") {
		days := 1
		fmt.Sscanf(s, "%dd", &days)
		return time.Duration(days) * 24 * time.Hour
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

func previewExtractFields(hit map[string]interface{}, extractFieldsJSON string) map[string]interface{} {
	vars := make(map[string]interface{})
	for k, v := range hit {
		vars[k] = v
	}
	if ts, ok := hit["@timestamp"]; ok {
		vars["time"] = ts
	}
	if extractFieldsJSON == "" {
		return vars
	}

	var fields []models.ExtractField
	if err := json.Unmarshal([]byte(extractFieldsJSON), &fields); err != nil {
		return vars
	}
	for _, f := range fields {
		val := es.GetNestedField(hit, f.Path)
		if val == nil {
			continue
		}
		valStr := fmt.Sprintf("%v", val)
		if f.Pattern != "" {
			re, err := regexp.Compile(f.Pattern)
			if err != nil {
				vars[f.Name] = valStr
				continue
			}
			matches := re.FindStringSubmatch(valStr)
			if len(matches) > 1 {
				vars[f.Name] = matches[1]
			} else if len(matches) == 1 {
				vars[f.Name] = matches[0]
			}
		} else {
			vars[f.Name] = valStr
		}
	}
	return vars
}

func previewRenderTemplate(tmplStr string, vars map[string]interface{}) string {
	if tmplStr == "" {
		var sb strings.Builder
		for k, v := range vars {
			if k == "_id" || k == "_index" {
				continue
			}
			sb.WriteString(fmt.Sprintf("**%s:** %v\n", k, v))
		}
		return sb.String()
	}
	tmpl, err := template.New("preview").Parse(tmplStr)
	if err != nil {
		return fmt.Sprintf("模板解析错误: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return fmt.Sprintf("模板渲染错误: %v", err)
	}
	return buf.String()
}
