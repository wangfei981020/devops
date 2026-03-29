package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	"opsplatform-alert-backend/lark"
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
		r.severity, COALESCE(r.group_by,''), COALESCE(r.expected_groups,''), COALESCE(r.query_concurrency,5), r.dedup_field, r.dedup_ttl, r.max_alerts, COALESCE(r.prometheus_config,''), r.status,
		r.last_run_at, r.last_error, r.created_at, r.updated_at,
		COALESCE(e.name,'(已删除)') as es_name, COALESCE(lk.name,'') as loki_name,
		COALESCE(l.name,'(已删除)') as lark_name
		FROM alert_rules r
		LEFT JOIN es_connections e ON r.es_connection_id = e.id
		LEFT JOIN loki_connections lk ON r.loki_connection_id = lk.id
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
		var esName, lokiName, larkName string
		err := rows.Scan(&rule.ID, &rule.Name, &rule.DataSourceType, &rule.ESConnectionID,
			&rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
			&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
			&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
			&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
			&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
			&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
			&rule.PrometheusConfig, &rule.Status, &rule.LastRunAt, &rule.LastError,
			&rule.CreatedAt, &rule.UpdatedAt, &esName, &lokiName, &larkName)
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
			"group_by":           rule.GroupBy,
			"expected_groups":    rule.ExpectedGroups,
			"query_concurrency":  rule.QueryConcurrency,
			"dedup_field":       rule.DedupField,
			"dedup_ttl":         rule.DedupTTL,
			"max_alerts":         rule.MaxAlerts,
			"prometheus_config":  rule.PrometheusConfig,
			"status":             rule.Status,
			"created_at":        rule.CreatedAt,
			"updated_at":        rule.UpdatedAt,
			"es_connection_name":   esName,
			"loki_connection_name": lokiName,
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
		severity, COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(query_concurrency,5), dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''),
		status, last_run_at, last_error, created_at, updated_at
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.DataSourceType, &rule.ESConnectionID,
		&rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
		&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
		&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
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
	dsType := req.DataSourceType
	if dsType == "" {
		dsType = "es"
	}
	if dsType == "es" && req.ESConnectionID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择ES连接")
		return
	}
	if dsType == "loki" && req.LokiConnectionID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择Loki连接")
		return
	}
	if dsType == "loki" && req.LogQL == "" {
		jsonError(w, http.StatusBadRequest, "LogQL查询不能为空")
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
		severity, group_by, expected_groups, query_concurrency, dedup_field, dedup_ttl, max_alerts, prometheus_config, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, req.LarkConfigID,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.ExpectedGroups, req.QueryConcurrency, req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig)
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
		severity=?, group_by=?, expected_groups=?, query_concurrency=?, dedup_field=?, dedup_ttl=?, max_alerts=?, prometheus_config=?
		WHERE id=?`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, req.LarkConfigID,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.ExpectedGroups, req.QueryConcurrency,
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

	// Remove from engine and clean Redis keys
	if ruleEngine != nil {
		ruleEngine.RemoveRule(id)
		if engine, ok := ruleEngine.(interface{ CleanupRuleRedisKeys(int) }); ok {
			engine.CleanupRuleRedisKeys(id)
		}
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

	// If group_by is set, group hits and keep only first per group
	groupBy := req.GroupBy
	if groupBy != "" {
		grouped := make(map[string]map[string]interface{})
		var order []string
		for _, hit := range rawHits {
			val := es.GetNestedField(hit, groupBy)
			key := "(unknown)"
			if val != nil {
				key = fmt.Sprintf("%v", val)
			}
			if _, exists := grouped[key]; !exists {
				grouped[key] = hit
				order = append(order, key)
			}
		}
		rawHits = nil
		for _, key := range order {
			hit := grouped[key]
			hit["_group_key"] = key
			hit["_group_field"] = groupBy
			rawHits = append(rawHits, hit)
		}
	}

	// Process hits
	var hits []PreviewHit
	for _, hit := range rawHits {
		vars := previewExtractFields(hit, req.ExtractFields)
		rendered := previewRenderTemplate(req.MessageTemplate, vars)
		hits = append(hits, PreviewHit{Raw: hit, Vars: vars, Rendered: rendered})
	}

	resp := map[string]interface{}{
		"total":         total,
		"hit_count":     len(rawHits),
		"hits":          hits,
		"query":         queryStr,
		"source_name":   sourceName,
		"source_detail": sourceDetail,
		"data_source":   dsType,
	}
	if groupBy != "" {
		resp["group_by"] = groupBy
		resp["group_count"] = len(rawHits)
	}
	jsonSuccess(w, resp)
}

// HandleTestSendAlertRule queries data source and sends one real alert to Lark
func HandleTestSendAlertRule(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAlertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.LarkConfigID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择 Lark 配置")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Get Lark config
	var larkCfg models.LarkConfig
	err := database.DB.QueryRow(`SELECT id, name, webhook_url, secret, lark_type FROM lark_configs WHERE id = ?`,
		req.LarkConfigID).Scan(&larkCfg.ID, &larkCfg.Name, &larkCfg.WebhookURL, &larkCfg.Secret, &larkCfg.LarkType)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Lark 配置不存在")
		return
	}

	// Query data source
	timeRange := req.TimeRange
	if timeRange == "" {
		timeRange = "5m"
	}
	maxAlerts := req.MaxAlerts
	if maxAlerts <= 0 {
		maxAlerts = 500 // test-send needs enough hits to cover all containers
	}
	if req.GroupBy != "" && maxAlerts < 500 {
		maxAlerts = 500 // grouped mode needs more hits
	}
	dsType := req.DataSourceType
	if dsType == "" {
		dsType = "es"
	}

	var rawHits []map[string]interface{}

	if dsType == "loki" {
		if req.LokiConnectionID == 0 || req.LogQL == "" {
			jsonError(w, http.StatusBadRequest, "请配置 Loki 连接和 LogQL")
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
		log.Printf("[TestSend] Loki query: logql=%s start=%s end=%s limit=%d", req.LogQL, start.Format(time.RFC3339), now.Format(time.RFC3339), maxAlerts)
		result, qErr := client.QueryRange(ctx, req.LogQL, start, now, maxAlerts)
		if qErr != nil {
			jsonError(w, http.StatusBadRequest, "Loki 查询失败: "+qErr.Error())
			return
		}
		rawHits = result.ToHits()
		log.Printf("[TestSend] Loki result: streams=%d total=%d hits=%d", len(result.Streams), result.Total, len(rawHits))
	} else {
		if req.ESConnectionID == 0 {
			jsonError(w, http.StatusBadRequest, "请选择 ES 连接")
			return
		}
		var conn models.ESConnection
		database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key, skip_tls_verify
			FROM es_connections WHERE id = ?`, req.ESConnectionID).Scan(
			&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey, &conn.SkipTLSVerify)
		client, cErr := es.NewClient(conn)
		if cErr != nil {
			jsonError(w, http.StatusBadRequest, "ES 客户端创建失败: "+cErr.Error())
			return
		}
		esIndex := req.ESIndex
		if esIndex == "" {
			esIndex = "*"
		}
		query, _ := es.BuildQuery(req.Keyword, req.FilterFields, timeRange, req.QueryDSL, maxAlerts)
		result, qErr := client.Search(ctx, esIndex, query)
		if qErr != nil {
			jsonError(w, http.StatusBadRequest, "ES 查询失败: "+qErr.Error())
			return
		}
		rawHits = result.Hits
	}

	// Check alert mode
	alertMode := req.AlertMode
	if alertMode == "" {
		alertMode = "found"
	}
	groupBy := req.GroupBy

	// Group hits if group_by is set
	type groupInfo struct {
		Key  string
		Hits []map[string]interface{}
	}
	var groups []groupInfo

	if groupBy != "" && len(rawHits) > 0 {
		seen := map[string]*groupInfo{}
		for _, hit := range rawHits {
			val := es.GetNestedField(hit, groupBy)
			key := "(unknown)"
			if val != nil {
				key = fmt.Sprintf("%v", val)
			}
			if g, ok := seen[key]; ok {
				g.Hits = append(g.Hits, hit)
			} else {
				g := &groupInfo{Key: key, Hits: []map[string]interface{}{hit}}
				seen[key] = g
				groups = append(groups, *g)
			}
		}
	}

	// For not_found with group_by: check which groups are present
	if alertMode == "not_found" && groupBy != "" {
		if len(rawHits) == 0 {
			// No hits at all → all containers missing
			jsonSuccess(w, map[string]interface{}{
				"message":     fmt.Sprintf("当前 %s 内未搜到任何日志，所有容器都会触发告警", timeRange),
				"hit_count":   0,
				"would_alert": true,
				"groups":      []string{},
			})
			return
		}
		// Some groups found → show which containers are OK
		var foundGroups []string
		for _, g := range groups {
			foundGroups = append(foundGroups, g.Key)
		}
		jsonSuccess(w, map[string]interface{}{
			"message":      fmt.Sprintf("正常容器: %d 个（%s 内有日志）\n缺失容器需与 24h 内已知容器对比，实际执行时自动判断", len(foundGroups), timeRange),
			"hit_count":    len(rawHits),
			"would_alert":  false,
			"found_groups": foundGroups,
			"group_count":  len(foundGroups),
		})
		return
	}

	if alertMode == "not_found" && len(rawHits) > 0 {
		jsonSuccess(w, map[string]interface{}{
			"message":   fmt.Sprintf("当前 %s 内搜到 %d 条日志，not_found 模式下不会触发告警", timeRange, len(rawHits)),
			"hit_count": len(rawHits),
			"would_alert": false,
		})
		return
	}

	if alertMode == "found" && len(rawHits) == 0 {
		jsonSuccess(w, map[string]interface{}{
			"message":     fmt.Sprintf("当前 %s 内未搜到匹配日志，found 模式下不会触发告警", timeRange),
			"hit_count":   0,
			"would_alert": false,
		})
		return
	}

	// Render message
	title := req.MessageTitle
	if title == "" {
		title = req.Name + " - 测试"
	} else {
		title = title + " [测试]"
	}

	var message string
	if alertMode == "not_found" {
		message = "**搜不到告警 (测试)**\n\n在 " + timeRange + " 内未搜到匹配日志。"
		if req.MessageTemplate != "" {
			vars := map[string]interface{}{"alert_reason": "not_found", "time_range": timeRange}
			message = previewRenderTemplate(req.MessageTemplate, vars)
		}
	} else if len(rawHits) > 0 {
		vars := previewExtractFields(rawHits[0], req.ExtractFields)
		message = previewRenderTemplate(req.MessageTemplate, vars)
	}

	// Parse at_users
	var atUsers []models.AtUser
	if req.AtUsers != "" {
		json.Unmarshal([]byte(req.AtUsers), &atUsers)
	}

	// Send to Lark
	sender := lark.NewSender(larkCfg)
	severity := req.Severity
	if severity == "" {
		severity = "info"
	}
	resp, sErr := sender.SendCard(title, message, severity, atUsers, req.AtAll == 1)
	if sErr != nil {
		jsonError(w, http.StatusBadRequest, "发送失败: "+sErr.Error())
		return
	}

	jsonSuccess(w, map[string]interface{}{
		"message":  "测试告警已发送到 Lark",
		"response": resp,
		"hit_count": len(rawHits),
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
