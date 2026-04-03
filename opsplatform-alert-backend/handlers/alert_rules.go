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
	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	"opsplatform-alert-backend/lark"
	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
)

// handlerLokiClientFunc creates a getLokiClient closure for use with QueryNamespacedLoki
func handlerLokiClientFunc() func(int) (*lokiclient.Client, error) {
	return func(id int) (*lokiclient.Client, error) {
		conn := getLokiConn(id)
		if conn == nil {
			return nil, fmt.Errorf("Loki connection %d not found", id)
		}
		return lokiclient.NewClient(*conn), nil
	}
}

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
	projectID := r.URL.Query().Get("project_id")

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

	if projectID != "" {
		pid, _ := strconv.Atoi(projectID)
		if pid == -1 {
			// Unassigned rules (no project)
			countQuery += " AND (project_id = 0 OR project_id IS NULL)"
		} else if pid > 0 {
			// Include child project IDs
			countQuery += " AND project_id IN (SELECT id FROM alert_projects WHERE id = ? OR parent_id = ?)"
			args = append(args, pid, pid)
		}
	}
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
		r.severity, COALESCE(r.group_by,''), COALESCE(r.expected_groups,''), COALESCE(r.query_concurrency,5), COALESCE(r.alert_interval,''), r.dedup_field, r.dedup_ttl, r.max_alerts, COALESCE(r.prometheus_config,''), COALESCE(r.route_config,''), COALESCE(r.namespaces,''), COALESCE(r.namespace_concurrency,3), COALESCE(r.project_id,0), r.status,
		r.last_run_at, r.last_error, r.created_at, r.updated_at,
		COALESCE(e.name,'(已删除)') as es_name, COALESCE(lk.name,'') as loki_name,
		COALESCE(l.name,'(已删除)') as lark_name
		FROM alert_rules r
		LEFT JOIN es_connections e ON r.es_connection_id = e.id
		LEFT JOIN loki_connections lk ON r.loki_connection_id = lk.id
		LEFT JOIN lark_configs l ON r.lark_config_id = l.id
		WHERE 1=1`

	queryArgs := []interface{}{}
	if projectID != "" {
		pid, _ := strconv.Atoi(projectID)
		if pid == -1 {
			query += " AND (r.project_id = 0 OR r.project_id IS NULL)"
		} else if pid > 0 {
			query += " AND r.project_id IN (SELECT id FROM alert_projects WHERE id = ? OR parent_id = ?)"
			queryArgs = append(queryArgs, pid, pid)
		}
	}
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
			&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
			&rule.PrometheusConfig, &rule.RouteConfig, &rule.Namespaces, &rule.NamespaceConcurrency, &rule.ProjectID, &rule.Status, &rule.LastRunAt, &rule.LastError,
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
			"alert_interval":     rule.AlertInterval,
			"dedup_field":       rule.DedupField,
			"dedup_ttl":         rule.DedupTTL,
			"max_alerts":         rule.MaxAlerts,
			"prometheus_config":  rule.PrometheusConfig,
			"route_config":            rule.RouteConfig,
			"namespaces":              rule.Namespaces,
			"namespace_concurrency":   rule.NamespaceConcurrency,
			"project_id":              rule.ProjectID,
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

	// Batch fetch alerting counts from Redis
	ctx := r.Context()
	for _, item := range list {
		ruleID := item["id"]
		key := fmt.Sprintf("alert:alerting_count:%v", ruleID)
		val, err := database.RDB.Get(ctx, key).Int()
		if err == nil {
			item["alerting_count"] = val
		} else {
			item["alerting_count"] = 0
		}
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
		severity, COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(query_concurrency,5), COALESCE(alert_interval,''),
		dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''), COALESCE(route_config,''), COALESCE(namespaces,''), COALESCE(namespace_concurrency,3), COALESCE(project_id,0),
		status, last_run_at, last_error, created_at, updated_at
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.DataSourceType, &rule.ESConnectionID,
		&rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
		&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
		&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
		&rule.PrometheusConfig, &rule.RouteConfig, &rule.Namespaces, &rule.NamespaceConcurrency, &rule.ProjectID, &rule.Status, &rule.LastRunAt, &rule.LastError,
		&rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		jsonError(w, http.StatusNotFound, "规则不存在: "+err.Error())
		return
	}

	log.Printf("[GetRule] id=%d namespaces='%s' namespace_concurrency=%d", rule.ID, rule.Namespaces, rule.NamespaceConcurrency)
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
		severity, group_by, expected_groups, query_concurrency, alert_interval, dedup_field, dedup_ttl, max_alerts, prometheus_config, route_config, namespaces, namespace_concurrency, project_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, req.LarkConfigID,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.ExpectedGroups, req.QueryConcurrency, req.AlertInterval, req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig, req.RouteConfig, req.Namespaces, req.NamespaceConcurrency, req.ProjectID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()

	// Reload in engine
	if ruleEngine != nil {
		ruleEngine.ReloadRule(int(id))
	}

	SaveAuditLog(r, "create_rule", "rule", req.Name, fmt.Sprintf("创建告警规则 ID=%d", id))
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var req models.CreateAlertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	log.Printf("[UpdateRule] id=%d namespaces='%s' namespace_concurrency=%d logql='%s'", id, req.Namespaces, req.NamespaceConcurrency, req.LogQL)

	_, err := database.DB.Exec(`UPDATE alert_rules SET
		name=?, data_source_type=?, es_connection_id=?, loki_connection_id=?, lark_config_id=?,
		es_index=?, schedule=?, time_range=?, query_dsl=?, keyword=?, logql=?,
		filter_fields=?, extract_fields=?, message_title=?,
		message_template=?, at_users=?, at_all=?,
		alert_mode=?, recovery_enabled=?, recovery_title=?, recovery_template=?,
		severity=?, group_by=?, expected_groups=?, query_concurrency=?, alert_interval=?, dedup_field=?, dedup_ttl=?, max_alerts=?, prometheus_config=?, route_config=?, namespaces=?, namespace_concurrency=?, project_id=?
		WHERE id=?`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, req.LarkConfigID,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.ExpectedGroups, req.QueryConcurrency, req.AlertInterval,
		req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig, req.RouteConfig, req.Namespaces, req.NamespaceConcurrency, req.ProjectID, id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}

	// Reload in engine
	if ruleEngine != nil {
		ruleEngine.ReloadRule(id)
	}

	SaveAuditLog(r, "update_rule", "rule", req.Name, fmt.Sprintf("更新告警规则 ID=%d", id))
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
	SaveAuditLog(r, "delete_rule", "rule", fmt.Sprintf("ID=%d", id), "删除告警规则")
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
	action := "enable_rule"
	if status == 0 {
		action = "disable_rule"
	}
	SaveAuditLog(r, action, "rule", fmt.Sprintf("ID=%d", id), fmt.Sprintf("规则状态变更为 %d", status))
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
		sourceName = conn.Name
		sourceDetail = "Loki"

		// Check if namespace mode
		var namespaces []string
		if req.Namespaces != "" {
			json.Unmarshal([]byte(req.Namespaces), &namespaces)
		}

		if len(namespaces) > 0 {
			// Namespace mode: use shared function to query, then flatten to old format
			results, err := alert.QueryNamespacedLoki(ctx, req.LokiConnectionID, namespaces,
				req.LogQL, timeRange, req.ExtractFields, req.Severity, req.MessageTemplate, req.RouteConfig,
				maxAlerts, req.NamespaceConcurrency, handlerLokiClientFunc())
			if err != nil {
				jsonError(w, http.StatusBadRequest, "Loki 查询失败: "+err.Error())
				return
			}

			// Flatten all hits into old-format compatible list
			for _, r := range results {
				rawHits = append(rawHits, r.Hits...)
			}
			total = int64(len(rawHits))
			queryStr = req.LogQL
			sourceDetail = "Loki (多命名空间)"
			// Fall through to normal hit rendering below
		} else {
			// Single query mode (no namespaces)
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
		}
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

	// Determine alert mode and group settings
	groupBy := req.GroupBy
	alertMode := req.AlertMode
	if alertMode == "" {
		alertMode = "found"
	}

	// ========== not_found + grouped + expected_groups: per-container check ==========
	if alertMode == "not_found" && groupBy != "" && req.ExpectedGroups != "" {
		var expectedList []string
		json.Unmarshal([]byte(req.ExpectedGroups), &expectedList)

		type ContainerResult struct {
			Name     string                 `json:"name"`
			Status   string                 `json:"status"`   // "ok" or "alert"
			Source   string                 `json:"source"`    // "30m" / "24h" / "3d" / "no_history"
			Hit      map[string]interface{} `json:"hit,omitempty"`
			Rendered string                 `json:"rendered,omitempty"`
		}

		var okList []ContainerResult
		var alertList []ContainerResult

		for _, containerName := range expectedList {
			log.Printf("[Preview] Checking container: %s", containerName)

			// Step 1: 直接查这个容器的 timeRange（如 30m）
			hit := queryContainerLastHit(ctx, dsType, req, groupBy, containerName, timeRange)
			if hit != nil {
				// 30m 内搜到 → 正常
				vars := previewExtractFields(hit, req.ExtractFields)
				vars["_group_key"] = containerName
				vars["_group_field"] = groupBy
				okList = append(okList, ContainerResult{
					Name: containerName, Status: "ok", Source: timeRange,
					Rendered: previewRenderTemplate(req.MessageTemplate, vars),
				})
				continue
			}

			// Step 2: 30m 搜不到 → 需要告警，找最后一条日志
			log.Printf("[Preview] Container %s not found in %s, searching wider", containerName, timeRange)

			// 2a: Check no_history marker
			noHistKey := fmt.Sprintf("alert:no_history:preview:%s:%s", groupBy, containerName)
			noHist, _ := database.RDB.Get(ctx, noHistKey).Result()
			if noHist == "1" {
				alertList = append(alertList, ContainerResult{
					Name: containerName, Status: "alert", Source: "no_history",
				})
				continue
			}

			// 2b: Query 3h → 6h fallback
			var lastHit map[string]interface{}
			hitSource := "no_history"
			lastHit = queryContainerLastHit(ctx, dsType, req, groupBy, containerName, "3h")
			if lastHit != nil {
				hitSource = "3h"
			} else {
				lastHit = queryContainerLastHit(ctx, dsType, req, groupBy, containerName, "6h")
				if lastHit != nil {
					hitSource = "6h"
				} else {
					database.RDB.Set(ctx, noHistKey, "1", 24*time.Hour)
					hitSource = "no_history"
				}
			}

			if lastHit != nil {
				vars := previewExtractFields(lastHit, req.ExtractFields)
				vars["_group_key"] = containerName
				vars["_group_field"] = groupBy
				vars["alert_reason"] = "not_found"
				vars["time_range"] = timeRange
				alertList = append(alertList, ContainerResult{
					Name: containerName, Status: "alert", Source: hitSource,
					Hit: lastHit, Rendered: previewRenderTemplate(req.MessageTemplate, vars),
				})
			} else {
				alertList = append(alertList, ContainerResult{
					Name: containerName, Status: "alert", Source: "no_history",
				})
			}
		}

		jsonSuccess(w, map[string]interface{}{
			"total":         total,
			"source_name":   sourceName,
			"source_detail": sourceDetail,
			"data_source":   dsType,
			"group_by":      groupBy,
			"alert_mode":    "not_found",
			"time_range":    timeRange,
			"ok_list":       okList,
			"alert_list":    alertList,
			"ok_count":      len(okList),
			"alert_count":   len(alertList),
			"total_groups":  len(expectedList),
		})
		return
	}

	// ========== Default: simple group or no group ==========
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
		"time_range":    timeRange,
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

		// Check namespace mode
		var namespaces []string
		if req.Namespaces != "" {
			json.Unmarshal([]byte(req.Namespaces), &namespaces)
		}

		if len(namespaces) > 0 {
			// Namespace mode: use shared function, send all aggregated alerts
			results, qErr := alert.QueryNamespacedLoki(ctx, req.LokiConnectionID, namespaces,
				req.LogQL, timeRange, req.ExtractFields, req.Severity, req.MessageTemplate, req.RouteConfig,
				maxAlerts, req.NamespaceConcurrency, handlerLokiClientFunc())
			if qErr != nil {
				jsonError(w, http.StatusBadRequest, "Loki 查询失败: "+qErr.Error())
				return
			}
			if len(results) == 0 {
				jsonSuccess(w, map[string]interface{}{
					"message":     fmt.Sprintf("当前 %s 内未搜到匹配日志", timeRange),
					"hit_count":   0,
					"would_alert": false,
				})
				return
			}

			// Send all container alerts
			var atUsers []models.AtUser
			if req.AtUsers != "" {
				json.Unmarshal([]byte(req.AtUsers), &atUsers)
			}
			senderObj := lark.NewSender(larkCfg)
			severity := req.Severity
			if severity == "" {
				severity = "S2"
			}

			sentCount := 0
			totalHits := 0
			for _, r := range results {
				totalHits += r.HitCount
				title := req.MessageTitle
				if title == "" {
					title = req.Name
				}
				title = fmt.Sprintf("%s [测试]", title)

				_, sErr := senderObj.SendCard(title, r.Message, severity, atUsers, req.AtAll == 1)
				if sErr != nil {
					log.Printf("[TestSend] Failed to send [%s/%s]: %v", r.Namespace, r.Container, sErr)
				} else {
					sentCount++
				}
			}

			jsonSuccess(w, map[string]interface{}{
				"message":   fmt.Sprintf("测试发送成功！命中 %d 条，按容器聚合为 %d 条告警，已发送 %d 条", totalHits, len(results), sentCount),
				"hit_count": totalHits,
				"alert_count": len(results),
				"sent_count": sentCount,
			})
			return
		}

		// Single query mode (no namespaces)
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

	// For not_found with group_by + expected_groups: per-container check and send
	if alertMode == "not_found" && groupBy != "" && req.ExpectedGroups != "" {
		var expectedList []string
		json.Unmarshal([]byte(req.ExpectedGroups), &expectedList)

		var okGroups []string
		var alertGroups []string
		sentCount := 0

		// Parse at_users
		var atUsers []models.AtUser
		if req.AtUsers != "" {
			json.Unmarshal([]byte(req.AtUsers), &atUsers)
		}
		senderObj := lark.NewSender(larkCfg)

		for _, containerName := range expectedList {
			// Check this container in timeRange
			hit := queryContainerLastHit(ctx, dsType, req, groupBy, containerName, timeRange)
			if hit != nil {
				okGroups = append(okGroups, containerName)
				continue
			}

			// Not found → send alert
			alertGroups = append(alertGroups, containerName)

			// Find last log for alert content (3h → 6h fallback)
			lastHit := queryContainerLastHit(ctx, dsType, req, groupBy, containerName, "3h")
			if lastHit == nil {
				lastHit = queryContainerLastHit(ctx, dsType, req, groupBy, containerName, "6h")
			}
			vars := map[string]interface{}{
				"alert_reason": "not_found",
				"time_range":   timeRange,
				"_group_key":   containerName,
				"_group_field": groupBy,
				groupBy:        containerName,
			}
			vars["container"] = containerName
			if lastHit != nil {
				vars = previewExtractFields(lastHit, req.ExtractFields)
				vars["alert_reason"] = "not_found"
				vars["time_range"] = timeRange
				vars["_group_key"] = containerName
				vars["_group_field"] = groupBy
				vars["container"] = containerName
			}

			titleRendered := previewRenderTemplate(req.MessageTitle, vars)
			title := fmt.Sprintf("%s [%s] [测试]", titleRendered, containerName)
			message := previewRenderTemplate(req.MessageTemplate, vars)

			severity := req.Severity
			if severity == "" {
				severity = "S2"
			}
			_, sErr := senderObj.SendCard(title, message, severity, atUsers, req.AtAll == 1)
			if sErr != nil {
				log.Printf("[TestSend] Failed to send for %s: %v", containerName, sErr)
			} else {
				sentCount++
			}
		}

		jsonSuccess(w, map[string]interface{}{
			"message":      fmt.Sprintf("正常: %d 个，告警: %d 个，已发送 %d 条告警到 Lark", len(okGroups), len(alertGroups), sentCount),
			"would_alert":  len(alertGroups) > 0,
			"ok_groups":    okGroups,
			"alert_groups": alertGroups,
			"sent_count":   sentCount,
			"hit_count":    len(alertGroups),
		})
		return
	}

	// For not_found without expected_groups: simple check
	if alertMode == "not_found" && groupBy != "" {
		var foundGroups []string
		for _, g := range groups {
			foundGroups = append(foundGroups, g.Key)
		}
		jsonSuccess(w, map[string]interface{}{
			"message":      fmt.Sprintf("正常容器: %d 个（%s 内有日志），请配置期望容器列表以启用逐容器检查", len(foundGroups), timeRange),
			"hit_count":    len(rawHits),
			"would_alert":  false,
			"found_groups": foundGroups,
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

// buildLogQLWithNamespace builds full LogQL for namespace mode.
// If namespaces is set, uses first namespace and prepends {namespace="X"} to pipeline.
// If namespaces is empty, returns logql as-is.
func buildLogQLWithNamespace(logql, namespacesJSON string) string {
	if namespacesJSON == "" {
		return logql
	}
	var namespaces []string
	if err := json.Unmarshal([]byte(namespacesJSON), &namespaces); err != nil || len(namespaces) == 0 {
		return logql
	}
	// For preview/test-send, use the first namespace
	pipeline := strings.TrimSpace(logql)
	return fmt.Sprintf(`{namespace="%s"} %s`, namespaces[0], pipeline)
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

// queryContainerLastHit queries a specific container for 1 log within a time range
func queryContainerLastHit(ctx context.Context, dsType string, req models.CreateAlertRuleReq, groupField, containerName, searchRange string) map[string]interface{} {
	if dsType == "loki" {
		if req.LokiConnectionID == 0 || req.LogQL == "" {
			return nil
		}
		conn := getLokiConn(req.LokiConnectionID)
		if conn == nil {
			return nil
		}
		client := lokiclient.NewClient(*conn)

		// Build specific LogQL: replace group field with exact match
		logql := req.LogQL
		safeKey := strings.ReplaceAll(containerName, `"`, `\"`)
		var specificLogQL string
		if idx := strings.Index(logql, "}"); idx >= 0 {
			existingLabels := strings.TrimSpace(logql[1:idx])
			pipeline := strings.TrimSpace(logql[idx+1:])

			// Remove existing group field selectors, add exact match
			var parts []string
			for _, part := range strings.Split(existingLabels, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				// Check if this part is for the group field
				fieldName := strings.Split(part, "=")[0]
				fieldName = strings.Split(fieldName, "!")[0]
				fieldName = strings.Split(fieldName, "~")[0]
				fieldName = strings.TrimSpace(fieldName)
				if fieldName == groupField {
					continue // Skip, will be replaced
				}
				parts = append(parts, part)
			}
			parts = append(parts, fmt.Sprintf(`%s="%s"`, groupField, safeKey))
			specificLogQL = "{" + strings.Join(parts, ", ") + "}"
			if pipeline != "" {
				specificLogQL += " " + pipeline
			}
		} else {
			specificLogQL = fmt.Sprintf(`{%s="%s"}`, groupField, safeKey)
		}
		log.Printf("[Preview] Container %s query: %s range=%s", containerName, specificLogQL, searchRange)

		now := time.Now()
		d := parsePreviewTimeRange(searchRange)
		result, err := client.QueryRange(ctx, specificLogQL, now.Add(-d), now, 1)
		if err != nil {
			log.Printf("[Preview] Loki query error for %s/%s: %v", containerName, searchRange, err)
			return nil
		}
		hits := result.ToHits()
		if len(hits) > 0 {
			return hits[0]
		}
		return nil
	}

	// ES
	if req.ESConnectionID == 0 {
		return nil
	}
	var conn models.ESConnection
	err := database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key, skip_tls_verify
		FROM es_connections WHERE id = ?`, req.ESConnectionID).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey, &conn.SkipTLSVerify)
	if err != nil {
		return nil
	}
	client, err := es.NewClient(conn)
	if err != nil {
		return nil
	}

	var filters []models.FilterField
	if req.FilterFields != "" {
		json.Unmarshal([]byte(req.FilterFields), &filters)
	}
	filters = append(filters, models.FilterField{Field: groupField, Value: containerName, Op: "term"})
	filterJSON, _ := json.Marshal(filters)

	esIndex := req.ESIndex
	if esIndex == "" {
		esIndex = "*"
	}
	query, _ := es.BuildQuery(req.Keyword, string(filterJSON), searchRange, "", 1)
	result, err := client.Search(ctx, esIndex, query)
	if err != nil || len(result.Hits) == 0 {
		return nil
	}
	return result.Hits[0]
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
