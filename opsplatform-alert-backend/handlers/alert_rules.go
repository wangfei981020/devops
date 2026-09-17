package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
	"opsplatform-alert-backend/notify"
	"opsplatform-alert-backend/safego"
)

// handlerLokiClientFunc creates a getLokiClient closure for use with QueryNamespacedLoki

// interactiveContextProvider builds the per-hit context provider shared by the
// preview and test-send handlers.
//
// Both are interactive: a person is waiting, and the request carries a 15s
// deadline while each hit costs up to two Loki queries per ladder rung. So only
// the first few hits fetch, and the rest say so — naming the PREVIEW's limit,
// not the rule's, so nobody reads a capped hit as "this will have no context
// when it really fires".
//
// fetched is shared across every send path in one request, so a namespaced
// test-send that fans out to several containers still pays the cap once.
func interactiveContextProvider(ctx context.Context, rule *models.AlertRule, fetched *int) alert.HitContextProvider {
	if rule.StackContextEnabled != 1 && rule.LogContextEnabled != 1 {
		return nil
	}
	return func(hit map[string]interface{}) (string, string) {
		if *fetched >= alert.MaxPreviewContextFetches {
			note := alert.PreviewContextSkippedNote(alert.MaxPreviewContextFetches)
			var stack, logctx string
			if rule.StackContextEnabled == 1 {
				stack = note
			}
			if rule.LogContextEnabled == 1 {
				logctx = note
			}
			return stack, logctx
		}
		*fetched++
		var stack, logctx string
		if rule.StackContextEnabled == 1 {
			stack = alert.FetchStackContext(ctx, rule, hit, handlerLokiClientFunc())
		}
		if rule.LogContextEnabled == 1 {
			logctx = alert.FetchLogContext(ctx, rule, hit, handlerLokiClientFunc())
		}
		return stack, logctx
	}
}

// contextRuleFromReq mirrors the unsaved form into the fields the context
// fetchers actually read.
func contextRuleFromReq(req *models.CreateAlertRuleReq) models.AlertRule {
	return models.AlertRule{
		LokiConnectionID:       req.LokiConnectionID,
		StackContextEnabled:    req.StackContextEnabled,
		StackMaxLines:          req.StackMaxLines,
		StackHeadLines:         req.StackHeadLines,
		StackTailLines:         req.StackTailLines,
		StackBoundaryPattern:   req.StackBoundaryPattern,
		StackWindowSec:         req.StackWindowSec,
		LogContextEnabled:      req.LogContextEnabled,
		LogContextBefore:       req.LogContextBefore,
		LogContextAfter:        req.LogContextAfter,
		LogContextMaxWindowSec: req.LogContextMaxWindowSec,
		LogContextDisplayLines: req.LogContextDisplayLines,
	}
}

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
		r.severity, COALESCE(r.group_by,''), COALESCE(r.expected_groups,''), COALESCE(r.query_concurrency,5), COALESCE(r.alert_interval,''), r.dedup_field, r.dedup_ttl, r.max_alerts, COALESCE(r.prometheus_config,''), COALESCE(r.route_config,''), COALESCE(r.namespaces,''), COALESCE(r.namespace_concurrency,3), COALESCE(r.label_filters,''), COALESCE(r.project_id,0),
		COALESCE(r.realtime_enabled,0), COALESCE(r.threshold_ms,0), COALESCE(r.report_enabled,0), COALESCE(r.report_schedule,''), COALESCE(r.report_mode,'separate'), COALESCE(r.report_title,''), COALESCE(r.report_template,''),
		COALESCE(r.stack_context_enabled,0), COALESCE(r.stack_max_lines,200), COALESCE(r.stack_head_lines,12), COALESCE(r.stack_tail_lines,8), COALESCE(r.stack_boundary_pattern,''), COALESCE(r.stack_window_sec,5),
		COALESCE(r.log_context_enabled,0), COALESCE(r.log_context_before,25), COALESCE(r.log_context_after,50), COALESCE(r.log_context_max_window_sec,1800), COALESCE(r.log_context_display_lines,30),
		r.status, r.last_run_at, r.last_error, r.created_at, r.updated_at,
		COALESCE(e.name,'(已删除)') as es_name, COALESCE(lk.name,'') as loki_name,
		COALESCE(l.name,'(已删除)') as lark_name
		FROM alert_rules r
		LEFT JOIN es_connections e ON r.es_connection_id = e.id
		LEFT JOIN loki_connections lk ON r.loki_connection_id = lk.id
		LEFT JOIN notify_channels l ON r.lark_config_id = l.id
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
			&rule.PrometheusConfig, &rule.RouteConfig, &rule.Namespaces, &rule.NamespaceConcurrency, &rule.LabelFilters, &rule.ProjectID,
			&rule.RealtimeEnabled, &rule.ThresholdMs, &rule.ReportEnabled, &rule.ReportSchedule, &rule.ReportMode, &rule.ReportTitle, &rule.ReportTemplate,
			&rule.StackContextEnabled, &rule.StackMaxLines, &rule.StackHeadLines, &rule.StackTailLines, &rule.StackBoundaryPattern, &rule.StackWindowSec,
			&rule.LogContextEnabled, &rule.LogContextBefore, &rule.LogContextAfter, &rule.LogContextMaxWindowSec, &rule.LogContextDisplayLines,
			&rule.Status, &rule.LastRunAt, &rule.LastError,
			&rule.CreatedAt, &rule.UpdatedAt, &esName, &lokiName, &larkName)
		if err != nil {
			continue
		}

		// Show every bound channel, not just the legacy primary one. One query
		// per row returns both the ids and the names together, instead of the
		// two separate round trips (channel names here, then channel_ids via
		// loadRuleChannelIDs below) this used to make.
		var channelIDs []int
		var channelNames []string
		chRows, chErr := database.DB.Query(`SELECT rc.channel_id, c.name FROM alert_rule_channels rc
			JOIN notify_channels c ON c.id = rc.channel_id WHERE rc.rule_id = ? ORDER BY c.id`, rule.ID)
		if chErr == nil {
			for chRows.Next() {
				var cid int
				var cname string
				if chRows.Scan(&cid, &cname) == nil {
					channelIDs = append(channelIDs, cid)
					channelNames = append(channelNames, cname)
				}
			}
			if err := chRows.Err(); err != nil {
				log.Printf("[ListRules] channel rows iteration error for rule %d: %v", rule.ID, err)
			}
			chRows.Close()
		}
		if len(channelNames) > 0 {
			larkName = strings.Join(channelNames, " + ")
		}

		item := map[string]interface{}{
			"id":                         rule.ID,
			"name":                       rule.Name,
			"data_source_type":           rule.DataSourceType,
			"es_connection_id":           rule.ESConnectionID,
			"loki_connection_id":         rule.LokiConnectionID,
			"lark_config_id":             rule.LarkConfigID,
			"es_index":                   rule.ESIndex,
			"schedule":                   rule.Schedule,
			"time_range":                 rule.TimeRange,
			"query_dsl":                  rule.QueryDSL,
			"keyword":                    rule.Keyword,
			"logql":                      rule.LogQL,
			"filter_fields":              rule.FilterFields,
			"extract_fields":             rule.ExtractFields,
			"message_title":              rule.MessageTitle,
			"message_template":           rule.MessageTemplate,
			"at_users":                   rule.AtUsers,
			"at_all":                     rule.AtAll,
			"alert_mode":                 rule.AlertMode,
			"recovery_enabled":           rule.RecoveryEnabled,
			"recovery_title":             rule.RecoveryTitle,
			"recovery_template":          rule.RecoveryTemplate,
			"severity":                   rule.Severity,
			"group_by":                   rule.GroupBy,
			"expected_groups":            rule.ExpectedGroups,
			"query_concurrency":          rule.QueryConcurrency,
			"alert_interval":             rule.AlertInterval,
			"dedup_field":                rule.DedupField,
			"dedup_ttl":                  rule.DedupTTL,
			"max_alerts":                 rule.MaxAlerts,
			"prometheus_config":          rule.PrometheusConfig,
			"route_config":               rule.RouteConfig,
			"namespaces":                 rule.Namespaces,
			"namespace_concurrency":      rule.NamespaceConcurrency,
			"label_filters":              rule.LabelFilters,
			"project_id":                 rule.ProjectID,
			"realtime_enabled":           rule.RealtimeEnabled,
			"threshold_ms":               rule.ThresholdMs,
			"report_enabled":             rule.ReportEnabled,
			"report_schedule":            rule.ReportSchedule,
			"report_mode":                rule.ReportMode,
			"report_title":               rule.ReportTitle,
			"report_template":            rule.ReportTemplate,
			"stack_context_enabled":      rule.StackContextEnabled,
			"stack_max_lines":            rule.StackMaxLines,
			"stack_head_lines":           rule.StackHeadLines,
			"stack_tail_lines":           rule.StackTailLines,
			"stack_boundary_pattern":     rule.StackBoundaryPattern,
			"stack_window_sec":           rule.StackWindowSec,
			"log_context_enabled":        rule.LogContextEnabled,
			"log_context_before":         rule.LogContextBefore,
			"log_context_after":          rule.LogContextAfter,
			"log_context_max_window_sec": rule.LogContextMaxWindowSec,
			"log_context_display_lines":  rule.LogContextDisplayLines,
			"status":                     rule.Status,
			"created_at":                 rule.CreatedAt,
			"updated_at":                 rule.UpdatedAt,
			"es_connection_name":         esName,
			"loki_connection_name":       lokiName,
			"lark_config_name":           larkName,
			"channel_ids":                channelIDs,
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
		dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''), COALESCE(route_config,''), COALESCE(namespaces,''), COALESCE(namespace_concurrency,3), COALESCE(label_filters,''), COALESCE(project_id,0),
		COALESCE(realtime_enabled,0), COALESCE(threshold_ms,0), COALESCE(report_enabled,0), COALESCE(report_schedule,''), COALESCE(report_mode,'separate'), COALESCE(report_title,''), COALESCE(report_template,''),
		COALESCE(stack_context_enabled,0), COALESCE(stack_max_lines,200), COALESCE(stack_head_lines,12), COALESCE(stack_tail_lines,8), COALESCE(stack_boundary_pattern,''), COALESCE(stack_window_sec,5),
		COALESCE(log_context_enabled,0), COALESCE(log_context_before,25), COALESCE(log_context_after,50), COALESCE(log_context_max_window_sec,1800), COALESCE(log_context_display_lines,30),
		status, last_run_at, last_error, created_at, updated_at
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.DataSourceType, &rule.ESConnectionID,
		&rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
		&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
		&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
		&rule.PrometheusConfig, &rule.RouteConfig, &rule.Namespaces, &rule.NamespaceConcurrency, &rule.LabelFilters, &rule.ProjectID,
		&rule.RealtimeEnabled, &rule.ThresholdMs, &rule.ReportEnabled, &rule.ReportSchedule, &rule.ReportMode, &rule.ReportTitle, &rule.ReportTemplate,
		&rule.StackContextEnabled, &rule.StackMaxLines, &rule.StackHeadLines, &rule.StackTailLines, &rule.StackBoundaryPattern, &rule.StackWindowSec,
		&rule.LogContextEnabled, &rule.LogContextBefore, &rule.LogContextAfter, &rule.LogContextMaxWindowSec, &rule.LogContextDisplayLines,
		&rule.Status, &rule.LastRunAt, &rule.LastError,
		&rule.CreatedAt, &rule.UpdatedAt)
	if err != nil {
		jsonError(w, http.StatusNotFound, "规则不存在: "+err.Error())
		return
	}

	rule.ChannelIDs = loadRuleChannelIDs(rule.ID)

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
	if len(req.ChannelIDs) == 0 && req.LarkConfigID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择通知渠道")
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
	if req.ReportSchedule == "" {
		req.ReportSchedule = "0 1 0 * * *"
	}
	if req.ReportMode == "" {
		req.ReportMode = "separate"
	}
	// Stack context defaults mirror the column defaults in the schema. The
	// INSERT below lists these columns explicitly, so an unset (zero-value)
	// field from an older client would otherwise write 0 instead of falling
	// back to the schema default.
	if req.StackMaxLines == 0 {
		req.StackMaxLines = 200
	}
	if req.StackHeadLines == 0 {
		req.StackHeadLines = 12
	}
	if req.StackTailLines == 0 {
		req.StackTailLines = 8
	}
	if req.StackWindowSec == 0 {
		req.StackWindowSec = 5
	}
	if req.LogContextBefore == 0 {
		req.LogContextBefore = 25
	}
	if req.LogContextAfter == 0 {
		req.LogContextAfter = 50
	}
	if req.LogContextMaxWindowSec == 0 {
		req.LogContextMaxWindowSec = 1800
	}
	if req.LogContextDisplayLines == 0 {
		req.LogContextDisplayLines = 30
	}

	// Placeholder only: saveRuleChannelsTx picks the real primary channel
	// (lowest-id Lark one) and rewrites lark_config_id inside the same
	// transaction, so this value never becomes visible on its own.
	primaryChannel := req.LarkConfigID
	if len(req.ChannelIDs) > 0 {
		primaryChannel = req.ChannelIDs[0]
	}

	// The rule row and its channel bindings must commit or roll back together:
	// a rule with no bindings, or a rule that never got inserted, would each be
	// a broken half-write visible to the client as either a phantom success or
	// a phantom failure.
	tx, err := database.DB.Begin()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(`INSERT INTO alert_rules
		(name, data_source_type, es_connection_id, loki_connection_id, lark_config_id,
		es_index, schedule, time_range,
		query_dsl, keyword, logql, filter_fields, extract_fields,
		message_title, message_template, at_users, at_all,
		alert_mode, recovery_enabled, recovery_title, recovery_template,
		severity, group_by, expected_groups, query_concurrency, alert_interval, dedup_field, dedup_ttl, max_alerts, prometheus_config, route_config, namespaces, namespace_concurrency, label_filters, project_id,
		realtime_enabled, threshold_ms, report_enabled, report_schedule, report_mode, report_title, report_template,
		stack_context_enabled, stack_max_lines, stack_head_lines, stack_tail_lines, stack_boundary_pattern, stack_window_sec, log_context_enabled, log_context_before, log_context_after, log_context_max_window_sec, log_context_display_lines, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, primaryChannel,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.ExpectedGroups, req.QueryConcurrency, req.AlertInterval, req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig, req.RouteConfig, req.Namespaces, req.NamespaceConcurrency, req.LabelFilters, req.ProjectID,
		req.RealtimeEnabled, req.ThresholdMs, req.ReportEnabled, req.ReportSchedule, req.ReportMode, req.ReportTitle, req.ReportTemplate,
		req.StackContextEnabled, req.StackMaxLines, req.StackHeadLines, req.StackTailLines, req.StackBoundaryPattern, req.StackWindowSec, req.LogContextEnabled, req.LogContextBefore, req.LogContextAfter, req.LogContextMaxWindowSec, req.LogContextDisplayLines)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()

	if err := saveRuleChannelsTx(tx, int(id), req.ChannelIDs, req.LarkConfigID); err != nil {
		// A rejected channel id is the client's mistake, not a server fault.
		if isChannelValidationError(err) {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonError(w, http.StatusInternalServerError, "保存通知渠道失败: "+err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, http.StatusInternalServerError, "创建失败: "+err.Error())
		return
	}

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
	if len(req.ChannelIDs) == 0 && req.LarkConfigID == 0 {
		jsonError(w, http.StatusBadRequest, "请选择通知渠道")
		return
	}

	log.Printf("[UpdateRule] id=%d namespaces='%s' namespace_concurrency=%d logql='%s'", id, req.Namespaces, req.NamespaceConcurrency, req.LogQL)

	if req.ReportSchedule == "" {
		req.ReportSchedule = "0 1 0 * * *"
	}
	if req.ReportMode == "" {
		req.ReportMode = "separate"
	}
	// Same defaulting as create: the UPDATE below lists these columns
	// explicitly, so a client that doesn't know about stack context yet
	// (an older UI tab, an automation script) would otherwise zero out an
	// already-configured rule's line limits on every save.
	if req.StackMaxLines == 0 {
		req.StackMaxLines = 200
	}
	if req.StackHeadLines == 0 {
		req.StackHeadLines = 12
	}
	if req.StackTailLines == 0 {
		req.StackTailLines = 8
	}
	if req.StackWindowSec == 0 {
		req.StackWindowSec = 5
	}
	if req.LogContextBefore == 0 {
		req.LogContextBefore = 25
	}
	if req.LogContextAfter == 0 {
		req.LogContextAfter = 50
	}
	if req.LogContextMaxWindowSec == 0 {
		req.LogContextMaxWindowSec = 1800
	}
	if req.LogContextDisplayLines == 0 {
		req.LogContextDisplayLines = 30
	}

	primaryChannel := req.LarkConfigID
	if len(req.ChannelIDs) > 0 {
		primaryChannel = req.ChannelIDs[0]
	}

	// Same reasoning as create: the rule row and its channel bindings must
	// commit or roll back together, or a failed channel save leaves
	// lark_config_id already overwritten while the client is told it failed.
	tx, err := database.DB.Begin()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}
	defer tx.Rollback()

	// An UPDATE matching no rows is not an error in MySQL, and RowsAffected is
	// 0 both for "no such rule" and for "nothing actually changed" — so neither
	// can tell us the rule exists. Without this check, saveRuleChannelsTx below
	// happily writes alert_rule_channels rows for a rule id that was never
	// there (PUT /api/alert-rules/99999, or one operator deleting a rule while
	// another saves it from an open tab), and those orphans then make the
	// referenced channel undeletable forever.
	var existingID int
	if err := tx.QueryRow("SELECT id FROM alert_rules WHERE id = ? FOR UPDATE", id).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "告警规则不存在")
			return
		}
		jsonError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}

	_, err = tx.Exec(`UPDATE alert_rules SET
		name=?, data_source_type=?, es_connection_id=?, loki_connection_id=?, lark_config_id=?,
		es_index=?, schedule=?, time_range=?, query_dsl=?, keyword=?, logql=?,
		filter_fields=?, extract_fields=?, message_title=?,
		message_template=?, at_users=?, at_all=?,
		alert_mode=?, recovery_enabled=?, recovery_title=?, recovery_template=?,
		severity=?, group_by=?, expected_groups=?, query_concurrency=?, alert_interval=?, dedup_field=?, dedup_ttl=?, max_alerts=?, prometheus_config=?, route_config=?, namespaces=?, namespace_concurrency=?, label_filters=?, project_id=?,
		realtime_enabled=?, threshold_ms=?, report_enabled=?, report_schedule=?, report_mode=?, report_title=?, report_template=?,
		stack_context_enabled=?, stack_max_lines=?, stack_head_lines=?, stack_tail_lines=?, stack_boundary_pattern=?, stack_window_sec=?, log_context_enabled=?, log_context_before=?, log_context_after=?, log_context_max_window_sec=?, log_context_display_lines=?
		WHERE id=?`,
		req.Name, req.DataSourceType, req.ESConnectionID, req.LokiConnectionID, primaryChannel,
		req.ESIndex, req.Schedule, req.TimeRange, req.QueryDSL, req.Keyword, req.LogQL,
		req.FilterFields, req.ExtractFields, req.MessageTitle,
		req.MessageTemplate, req.AtUsers, req.AtAll,
		req.AlertMode, req.RecoveryEnabled, req.RecoveryTitle, req.RecoveryTemplate,
		req.Severity, req.GroupBy, req.ExpectedGroups, req.QueryConcurrency, req.AlertInterval,
		req.DedupField, req.DedupTTL, req.MaxAlerts, req.PrometheusConfig, req.RouteConfig, req.Namespaces, req.NamespaceConcurrency, req.LabelFilters, req.ProjectID,
		req.RealtimeEnabled, req.ThresholdMs, req.ReportEnabled, req.ReportSchedule, req.ReportMode, req.ReportTitle, req.ReportTemplate,
		req.StackContextEnabled, req.StackMaxLines, req.StackHeadLines, req.StackTailLines, req.StackBoundaryPattern, req.StackWindowSec, req.LogContextEnabled, req.LogContextBefore, req.LogContextAfter, req.LogContextMaxWindowSec, req.LogContextDisplayLines,
		id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "更新失败: "+err.Error())
		return
	}

	if err := saveRuleChannelsTx(tx, id, req.ChannelIDs, req.LarkConfigID); err != nil {
		// A rejected channel id is the client's mistake, not a server fault.
		if isChannelValidationError(err) {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		jsonError(w, http.StatusInternalServerError, "保存通知渠道失败: "+err.Error())
		return
	}

	if err := tx.Commit(); err != nil {
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

	// The rule row and its alert_rule_channels bindings must be removed together:
	// leaving the join rows behind orphans them, and a later delete of the
	// channel they reference is then permanently refused by the "channel in
	// use" guard in notify_channels.go, even though the rule no longer exists.
	tx, err := database.DB.Begin()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM alert_rule_channels WHERE rule_id = ?", id); err != nil {
		jsonError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}

	result, err := tx.Exec("DELETE FROM alert_rules WHERE id = ?", id)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		jsonError(w, http.StatusNotFound, "规则不存在")
		return
	}

	if err := tx.Commit(); err != nil {
		jsonError(w, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}

	// Only touch the engine and Redis once the database delete has actually
	// committed. RemoveRule/CleanupRuleRedisKeys report no error and cannot be
	// rolled back, so doing this before the DB delete could succeed risks
	// forgetting a rule that failed to delete and is still supposed to run.
	if ruleEngine != nil {
		ruleEngine.RemoveRule(id)
		if engine, ok := ruleEngine.(interface{ CleanupRuleRedisKeys(int) }); ok {
			engine.CleanupRuleRedisKeys(id)
		}
	}

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
		// Manual runs bypass cron entirely, so they carry their own guard.
		safego.Go("manual rule run", func() {
			if engine, ok := ruleEngine.(interface{ ExecuteRule(int) }); ok {
				engine.ExecuteRule(id)
			}
		})
	}

	jsonSuccess(w, map[string]string{"message": "规则已触发执行"})
}

// reportDay resolves the target day (YYYYMMDD) from the request query:
// "today" (default), "yesterday", or an explicit YYYYMMDD value.
func reportDay(r *http.Request) string {
	switch d := r.URL.Query().Get("date"); d {
	case "yesterday":
		return time.Now().AddDate(0, 0, -1).Format("20060102")
	case "", "today":
		return time.Now().Format("20060102")
	default:
		return d
	}
}

// HandlePreviewReport renders the daily performance report card(s) for a day without sending.
func HandlePreviewReport(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	day := reportDay(r)
	cards, domainCount, err := alert.BuildDailyReportCards(id, day)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "预览失败: "+err.Error())
		return
	}
	jsonSuccess(w, map[string]interface{}{
		"day":          day,
		"domain_count": domainCount,
		"cards":        cards,
	})
}

// HandleSendReport aggregates a day's stats and sends the report to the rule's notification channels immediately.
func HandleSendReport(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	day := reportDay(r)
	sent, domainCount, err := alert.SendDailyReportNow(id, day)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "发送失败: "+err.Error())
		return
	}
	jsonSuccess(w, map[string]interface{}{
		"day":          day,
		"domain_count": domainCount,
		"sent":         sent,
	})
}

// HandlePreviewAlertRule executes query and renders template without sending anything
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
				maxAlerts, req.NamespaceConcurrency, req.LabelFilters, handlerLokiClientFunc())
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
			Status   string                 `json:"status"` // "ok" or "alert"
			Source   string                 `json:"source"` // "30m" / "24h" / "3d" / "no_history"
			Hit      map[string]interface{} `json:"hit,omitempty"`
			Rendered string                 `json:"rendered,omitempty"`
		}

		var okList []ContainerResult
		var alertList []ContainerResult

		nfRule := contextRuleFromReq(&req)
		nfCtxFetched := 0
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

				// Same contexts the engine puts on this path — around the last
				// line before the container went quiet. Containers with no
				// history land in the else branch and query nothing.
				var nfStack, nfLogctx string
				if nfCtxFetched < alert.MaxPreviewContextFetches &&
					(req.StackContextEnabled == 1 || req.LogContextEnabled == 1) {
					nfStack, nfLogctx = alert.FetchNotFoundContexts(ctx, &nfRule, lastHit, handlerLokiClientFunc())
					if nfStack != "" {
						vars["stack"] = nfStack
					}
					if nfLogctx != "" {
						vars["logcontext"] = nfLogctx
					}
					nfCtxFetched++
				}
				rendered := previewRenderTemplate(req.MessageTemplate, vars)
				rendered = alert.AppendUnreferencedContexts(rendered, req.MessageTemplate, nfStack, nfLogctx)

				alertList = append(alertList, ContainerResult{
					Name: containerName, Status: "alert", Source: hitSource,
					Hit: lastHit, Rendered: rendered,
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

	// Performance-alert preview: simulate realtime threshold gating so the
	// preview reflects what would actually alert. Read-only: unlike the engine's
	// processPerformanceHit, this writes nothing to Redis and does NOT apply the
	// "one alert per tid per day" dedup (preview should show all would-alert hits).
	perfPreview := req.RealtimeEnabled == 1

	previewRule := contextRuleFromReq(&req)
	ctxFetched := 0
	ctxFor := interactiveContextProvider(ctx, &previewRule, &ctxFetched)

	var hits []PreviewHit
	for _, hit := range rawHits {
		vars := previewExtractFields(hit, req.ExtractFields)

		if perfPreview {
			lineStr := ""
			if v, ok := hit["line"].(string); ok {
				lineStr = v
			} else if v, ok := hit["message"].(string); ok {
				lineStr = v
			}
			// Mirror engine: exactly one http(s):// URL per log line
			if strings.Count(lineStr, "http://")+strings.Count(lineStr, "https://") != 1 {
				continue
			}
			cost, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", vars["cost_ms"])))
			if err != nil || cost <= 0 {
				continue
			}
			if req.ThresholdMs > 0 && cost <= req.ThresholdMs {
				continue // under threshold, would not alert
			}
			vars["threshold_ms"] = req.ThresholdMs
			vars["cost_ms"] = cost
		}

		// Contexts, by the same rule the engine uses — a preview that renders an
		// empty {{.logcontext}} makes a working switch look broken.
		var stack, logctx string
		if ctxFor != nil {
			stack, logctx = ctxFor(hit)
			if stack != "" {
				vars["stack"] = stack
			}
			if logctx != "" {
				vars["logcontext"] = logctx
			}
		}

		rendered := previewRenderTemplate(req.MessageTemplate, vars)
		rendered = alert.AppendUnreferencedContexts(rendered, req.MessageTemplate, stack, logctx)
		hits = append(hits, PreviewHit{Raw: hit, Vars: vars, Rendered: rendered})
	}

	resp := map[string]interface{}{
		"total":         total,
		"hit_count":     len(hits),
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

// HandleTestSendAlertRule queries data source and sends one real alert to the selected notification channels
func HandleTestSendAlertRule(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAlertRuleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	// This endpoint tests an unsaved form, so the channels come from the request
	// body rather than from alert_rule_channels.
	channelIDs := req.ChannelIDs
	if len(channelIDs) == 0 && req.LarkConfigID > 0 {
		channelIDs = []int{req.LarkConfigID}
	}
	if len(channelIDs) == 0 {
		jsonError(w, http.StatusBadRequest, "请选择通知渠道")
		return
	}

	var channels []models.NotifyChannel
	for _, cid := range channelIDs {
		var c models.NotifyChannel
		qErr := database.DB.QueryRow(`SELECT id, channel_type, name, webhook_url, secret,
			lark_type, bot_token, chat_id, thread_id, proxy_url, status
			FROM notify_channels WHERE id = ? AND status = 1`, cid).Scan(
			&c.ID, &c.ChannelType, &c.Name, &c.WebhookURL, &c.Secret,
			&c.LarkType, &c.BotToken, &c.ChatID, &c.ThreadID, &c.ProxyURL, &c.Status)
		if qErr != nil {
			jsonError(w, http.StatusBadRequest, fmt.Sprintf("通知渠道 %d 不存在或已禁用", cid))
			return
		}
		channels = append(channels, c)
	}

	senderObj, err := notify.NewMulti(channels)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "通知渠道无效: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	// Same contexts a real alert would carry, by the same rules — a test send
	// that renders differently from the real thing tests the wrong message.
	// The counter is shared by every send path below, so a namespaced fan-out
	// across containers still pays the interactive cap once.
	sendRule := contextRuleFromReq(&req)
	ctxFetched := 0
	ctxFor := interactiveContextProvider(ctx, &sendRule, &ctxFetched)

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
				maxAlerts, req.NamespaceConcurrency, req.LabelFilters, handlerLokiClientFunc())
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

				msg := r.Message
				if ctxFor != nil {
					// QueryNamespacedLoki rendered without context (it also
					// serves paths where the hits may yet be discarded); with a
					// provider in hand, re-render so every shown hit carries
					// its own.
					msg = alert.BuildNamespacedAlertMessage(r.Namespace, r.Container, severity,
						req.ExtractFields, req.MessageTemplate, r.Hits, ctxFor)
				}

				_, sErr := senderObj.SendCard(title, msg, severity, atUsers, req.AtAll == 1)
				if sErr != nil {
					log.Printf("[TestSend] Failed to send [%s/%s]: %v", r.Namespace, r.Container, sErr)
				} else {
					sentCount++
				}
			}

			jsonSuccess(w, map[string]interface{}{
				"message":     fmt.Sprintf("测试发送成功！命中 %d 条，按容器聚合为 %d 条告警，已发送 %d 条", totalHits, len(results), sentCount),
				"hit_count":   totalHits,
				"alert_count": len(results),
				"sent_count":  sentCount,
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

		nfSendRule := contextRuleFromReq(&req)
		nfCtxFetched := 0
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
			var nfStack, nfLogctx string
			if lastHit != nil {
				vars = previewExtractFields(lastHit, req.ExtractFields)
				vars["alert_reason"] = "not_found"
				vars["time_range"] = timeRange
				vars["_group_key"] = containerName
				vars["_group_field"] = groupBy
				vars["container"] = containerName

				// Same contexts as the engine and the preview on this path.
				if nfCtxFetched < alert.MaxPreviewContextFetches &&
					(req.StackContextEnabled == 1 || req.LogContextEnabled == 1) {
					nfStack, nfLogctx = alert.FetchNotFoundContexts(ctx, &nfSendRule, lastHit, handlerLokiClientFunc())
					if nfStack != "" {
						vars["stack"] = nfStack
					}
					if nfLogctx != "" {
						vars["logcontext"] = nfLogctx
					}
					nfCtxFetched++
				}
			}

			titleRendered := previewRenderTemplate(req.MessageTitle, vars)
			title := fmt.Sprintf("%s [%s] [测试]", titleRendered, containerName)
			message := previewRenderTemplate(req.MessageTemplate, vars)
			message = alert.AppendUnreferencedContexts(message, req.MessageTemplate, nfStack, nfLogctx)

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
			"message":      fmt.Sprintf("正常: %d 个，告警: %d 个，已发送 %d 条告警到所选渠道", len(okGroups), len(alertGroups), sentCount),
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
			"message":     fmt.Sprintf("当前 %s 内搜到 %d 条日志，not_found 模式下不会触发告警", timeRange, len(rawHits)),
			"hit_count":   len(rawHits),
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
		var stack, logctx string
		if ctxFor != nil {
			stack, logctx = ctxFor(rawHits[0])
			if stack != "" {
				vars["stack"] = stack
			}
			if logctx != "" {
				vars["logcontext"] = logctx
			}
		}
		message = previewRenderTemplate(req.MessageTemplate, vars)
		message = alert.AppendUnreferencedContexts(message, req.MessageTemplate, stack, logctx)
	}

	// Parse at_users
	var atUsers []models.AtUser
	if req.AtUsers != "" {
		json.Unmarshal([]byte(req.AtUsers), &atUsers)
	}

	// Send via the fan-out sender built at the top of this handler.
	severity := req.Severity
	if severity == "" {
		severity = "info"
	}
	resp, sErr := senderObj.SendCard(title, message, severity, atUsers, req.AtAll == 1)
	if sErr != nil {
		jsonError(w, http.StatusBadRequest, "发送失败: "+sErr.Error())
		return
	}

	jsonSuccess(w, map[string]interface{}{
		"message":   "测试告警已发送到所选渠道",
		"response":  resp,
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
		// Shared with the engine's own no-template path, so what the preview
		// and test-send show is byte-for-byte what a real alert would send.
		return alert.RenderVarsDefault(vars)
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

// HandleExportAlertRules exports selected rules as JSON (batch)
func HandleExportAlertRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		jsonError(w, http.StatusBadRequest, "请选择要导出的规则")
		return
	}

	// Build IN clause
	placeholders := make([]string, len(req.IDs))
	args := make([]interface{}, len(req.IDs))
	for i, id := range req.IDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`SELECT id, name, COALESCE(data_source_type,'es'), es_connection_id,
		COALESCE(loki_connection_id,0), lark_config_id, es_index,
		schedule, time_range, COALESCE(query_dsl,''), keyword, COALESCE(logql,''),
		COALESCE(filter_fields,''), COALESCE(extract_fields,''),
		message_title, COALESCE(message_template,''),
		COALESCE(at_users,''), at_all, COALESCE(alert_mode,'found'),
		recovery_enabled, COALESCE(recovery_title,''), COALESCE(recovery_template,''),
		severity, COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(query_concurrency,5), COALESCE(alert_interval,''),
		dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''), COALESCE(route_config,''), COALESCE(namespaces,''), COALESCE(namespace_concurrency,3), COALESCE(label_filters,''), COALESCE(project_id,0),
		COALESCE(realtime_enabled,0), COALESCE(threshold_ms,0), COALESCE(report_enabled,0), COALESCE(report_schedule,''), COALESCE(report_mode,'separate'), COALESCE(report_title,''), COALESCE(report_template,''),
		COALESCE(stack_context_enabled,0), COALESCE(stack_max_lines,200), COALESCE(stack_head_lines,12), COALESCE(stack_tail_lines,8), COALESCE(stack_boundary_pattern,''), COALESCE(stack_window_sec,5),
		COALESCE(log_context_enabled,0), COALESCE(log_context_before,25), COALESCE(log_context_after,50), COALESCE(log_context_max_window_sec,1800), COALESCE(log_context_display_lines,30)
		FROM alert_rules WHERE id IN (%s)`, strings.Join(placeholders, ","))

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var exported []models.CreateAlertRuleReq
	for rows.Next() {
		var rule models.CreateAlertRuleReq
		var id int
		rows.Scan(&id, &rule.Name, &rule.DataSourceType, &rule.ESConnectionID,
			&rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
			&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
			&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
			&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
			&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
			&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval,
			&rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts, &rule.PrometheusConfig, &rule.RouteConfig,
			&rule.Namespaces, &rule.NamespaceConcurrency, &rule.LabelFilters, &rule.ProjectID,
			&rule.RealtimeEnabled, &rule.ThresholdMs, &rule.ReportEnabled, &rule.ReportSchedule, &rule.ReportMode, &rule.ReportTitle, &rule.ReportTemplate,
			&rule.StackContextEnabled, &rule.StackMaxLines, &rule.StackHeadLines, &rule.StackTailLines, &rule.StackBoundaryPattern, &rule.StackWindowSec,
			&rule.LogContextEnabled, &rule.LogContextBefore, &rule.LogContextAfter, &rule.LogContextMaxWindowSec, &rule.LogContextDisplayLines)
		// Import reads channel_ids, so export has to write it: without this a
		// Lark+Telegram rule silently degrades to a single channel on the
		// round trip through lark_config_id.
		rule.ChannelIDs = loadRuleChannelIDs(id)
		exported = append(exported, rule)
	}

	SaveAuditLog(r, "export_rules", "rule", "", fmt.Sprintf("导出 %d 条规则", len(exported)))
	jsonSuccess(w, exported)
}

// HandleImportAlertRules imports rules from JSON (batch)
func HandleImportAlertRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rules []models.CreateAlertRuleReq `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Rules) == 0 {
		jsonError(w, http.StatusBadRequest, "无效的导入数据")
		return
	}

	var created []int64
	var errors []string
	for i, rule := range req.Rules {
		if rule.Name == "" {
			errors = append(errors, fmt.Sprintf("第%d条: 名称不能为空", i+1))
			continue
		}
		if rule.DataSourceType == "" {
			rule.DataSourceType = "es"
		}
		if rule.Schedule == "" {
			rule.Schedule = "*/5 * * * *"
		}
		if rule.TimeRange == "" {
			rule.TimeRange = "5m"
		}
		if rule.ESIndex == "" {
			rule.ESIndex = "*"
		}
		if rule.Severity == "" {
			rule.Severity = "warning"
		}
		if rule.DedupTTL == 0 {
			rule.DedupTTL = 3600
		}
		if rule.MaxAlerts == 0 {
			rule.MaxAlerts = 10
		}

		if rule.ReportSchedule == "" {
			rule.ReportSchedule = "0 1 0 * * *"
		}
		if rule.ReportMode == "" {
			rule.ReportMode = "separate"
		}
		// Same stack-context defaulting as create/update: an exported rule
		// from before this feature existed won't carry these fields at all,
		// so treat their zero values as "use the schema default" rather than
		// literally writing 0.
		if rule.StackMaxLines == 0 {
			rule.StackMaxLines = 200
		}
		if rule.StackHeadLines == 0 {
			rule.StackHeadLines = 12
		}
		if rule.StackTailLines == 0 {
			rule.StackTailLines = 8
		}
		if rule.StackWindowSec == 0 {
			rule.StackWindowSec = 5
		}
		if rule.LogContextBefore == 0 {
			rule.LogContextBefore = 25
		}
		if rule.LogContextAfter == 0 {
			rule.LogContextAfter = 50
		}
		if rule.LogContextMaxWindowSec == 0 {
			rule.LogContextMaxWindowSec = 1800
		}
		if rule.LogContextDisplayLines == 0 {
			rule.LogContextDisplayLines = 30
		}

		id, err := func() (int64, error) {
			tx, err := database.DB.Begin()
			if err != nil {
				return 0, err
			}
			defer tx.Rollback()

			result, err := tx.Exec(`INSERT INTO alert_rules
				(name, data_source_type, es_connection_id, loki_connection_id, lark_config_id,
				es_index, schedule, time_range,
				query_dsl, keyword, logql, filter_fields, extract_fields,
				message_title, message_template, at_users, at_all,
				alert_mode, recovery_enabled, recovery_title, recovery_template,
				severity, group_by, expected_groups, query_concurrency, alert_interval, dedup_field, dedup_ttl, max_alerts, prometheus_config, route_config, namespaces, namespace_concurrency, label_filters, project_id,
				realtime_enabled, threshold_ms, report_enabled, report_schedule, report_mode, report_title, report_template,
				stack_context_enabled, stack_max_lines, stack_head_lines, stack_tail_lines, stack_boundary_pattern, stack_window_sec, log_context_enabled, log_context_before, log_context_after, log_context_max_window_sec, log_context_display_lines, status)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
				rule.Name, rule.DataSourceType, rule.ESConnectionID, rule.LokiConnectionID, rule.LarkConfigID,
				rule.ESIndex, rule.Schedule, rule.TimeRange, rule.QueryDSL, rule.Keyword, rule.LogQL,
				rule.FilterFields, rule.ExtractFields, rule.MessageTitle,
				rule.MessageTemplate, rule.AtUsers, rule.AtAll,
				rule.AlertMode, rule.RecoveryEnabled, rule.RecoveryTitle, rule.RecoveryTemplate,
				rule.Severity, rule.GroupBy, rule.ExpectedGroups, rule.QueryConcurrency, rule.AlertInterval, rule.DedupField, rule.DedupTTL, rule.MaxAlerts, rule.PrometheusConfig, rule.RouteConfig, rule.Namespaces, rule.NamespaceConcurrency, rule.LabelFilters, rule.ProjectID,
				rule.RealtimeEnabled, rule.ThresholdMs, rule.ReportEnabled, rule.ReportSchedule, rule.ReportMode, rule.ReportTitle, rule.ReportTemplate,
				rule.StackContextEnabled, rule.StackMaxLines, rule.StackHeadLines, rule.StackTailLines, rule.StackBoundaryPattern, rule.StackWindowSec, rule.LogContextEnabled, rule.LogContextBefore, rule.LogContextAfter, rule.LogContextMaxWindowSec, rule.LogContextDisplayLines)
			if err != nil {
				return 0, err
			}
			ruleID, _ := result.LastInsertId()

			// A rule that fails to get a channel binding must not be reported as
			// created: it would sit in the database with no way to notify anyone.
			if err := saveRuleChannelsTx(tx, int(ruleID), rule.ChannelIDs, rule.LarkConfigID); err != nil {
				return 0, fmt.Errorf("保存通知渠道失败: %v", err)
			}
			if err := tx.Commit(); err != nil {
				return 0, err
			}
			return ruleID, nil
		}()

		if err != nil {
			errors = append(errors, fmt.Sprintf("第%d条 '%s': %v", i+1, rule.Name, err))
			continue
		}
		created = append(created, id)
	}

	SaveAuditLog(r, "import_rules", "rule", "", fmt.Sprintf("导入 %d 条规则, 成功 %d, 失败 %d", len(req.Rules), len(created), len(errors)))
	jsonSuccess(w, map[string]interface{}{
		"created": created,
		"errors":  errors,
		"total":   len(req.Rules),
		"success": len(created),
	})
}

// sqlExecer is satisfied by both *sql.DB and *sql.Tx, so saveRuleChannelsTx can
// run either standalone or as part of a caller's larger transaction. Query is
// part of it because the channel ids have to be validated against
// notify_channels inside the caller's transaction, not on a separate connection.
type sqlExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// channelValidationError marks a bad request (an unusable channel id) as
// opposed to a database failure, so the handlers can answer 400 instead of 500.
type channelValidationError struct{ msg string }

func (e *channelValidationError) Error() string { return e.msg }

// isChannelValidationError reports whether err is a client mistake.
func isChannelValidationError(err error) bool {
	var e *channelValidationError
	return errors.As(err, &e)
}

// saveRuleChannelsTx rewrites a rule's channel bindings using the given executor.
// It filters out non-positive and duplicate ids FIRST, then falls back to
// fallbackID only if nothing valid remains, then validates that the resulting
// set is non-empty. This ordering matters: channel_ids like [0] or [0, 5] must
// not let a zero slip through into lark_config_id or leave the rule with a
// silently-empty binding. It also keeps the legacy alert_rules.lark_config_id
// in sync with one of the bound channels, so a rollback to the previous binary
// still finds a usable channel.
func saveRuleChannelsTx(exec sqlExecer, ruleID int, channelIDs []int, fallbackID int) error {
	seen := map[int]bool{}
	var valid []int
	for _, cid := range channelIDs {
		if cid <= 0 || seen[cid] {
			continue
		}
		seen[cid] = true
		valid = append(valid, cid)
	}
	if len(valid) == 0 && fallbackID > 0 {
		valid = []int{fallbackID}
	}
	if len(valid) == 0 {
		return &channelValidationError{"至少要选择一个通知渠道"}
	}

	// Sort so the binding is deterministic. Every reader (loadRuleChannelIDs,
	// the rule list query) orders by channel_id; if the writer kept the order
	// the client happened to send, the primary channel chosen below would
	// depend on which tag the operator clicked first, and re-saving a rule
	// unchanged could silently move it — which changes routing, because
	// engine.go compares route_config's lark_id against lark_config_id.
	sort.Ints(valid)

	// There is no foreign key on alert_rule_channels, so a binding to a
	// deleted or disabled channel would be accepted here and only surface at
	// run time ("all N bound notification channels are disabled") — or, for a
	// deleted channel, leave an orphan row that blocks channel deletion for
	// good. Validate inside the caller's transaction instead.
	types, err := loadChannelTypes(exec, valid)
	if err != nil {
		return err
	}
	for _, cid := range valid {
		if _, ok := types[cid]; !ok {
			return &channelValidationError{fmt.Sprintf("通知渠道 #%d 不存在或已禁用", cid)}
		}
	}

	// alert_rules.lark_config_id is the rollback path: it is the only binding
	// the previous binary can see, and it can only use a Lark channel. Prefer
	// the lowest-id Lark channel among the bound ones and fall back to the
	// lowest id overall only when the rule binds no Lark channel at all.
	primary := valid[0]
	for _, cid := range valid {
		if types[cid] == "lark" {
			primary = cid
			break
		}
	}

	if _, err := exec.Exec("DELETE FROM alert_rule_channels WHERE rule_id = ?", ruleID); err != nil {
		return err
	}
	for _, cid := range valid {
		if _, err := exec.Exec("INSERT INTO alert_rule_channels (rule_id, channel_id) VALUES (?, ?)", ruleID, cid); err != nil {
			return err
		}
	}
	if _, err := exec.Exec("UPDATE alert_rules SET lark_config_id = ? WHERE id = ?", primary, ruleID); err != nil {
		return err
	}
	return nil
}

// loadChannelTypes returns channel_type keyed by id for the enabled channels
// among ids. An id missing from the result is either unknown or disabled.
func loadChannelTypes(exec sqlExecer, ids []int) (map[int]string, error) {
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := exec.Query(fmt.Sprintf(
		"SELECT id, channel_type FROM notify_channels WHERE status = 1 AND id IN (%s)",
		strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := map[int]string{}
	for rows.Next() {
		var id int
		var ct string
		if err := rows.Scan(&id, &ct); err != nil {
			return nil, err
		}
		types[id] = ct
	}
	return types, rows.Err()
}

// loadRuleChannelIDs returns a rule's bound channel IDs, ordered.
func loadRuleChannelIDs(ruleID int) []int {
	rows, err := database.DB.Query(
		"SELECT channel_id FROM alert_rule_channels WHERE rule_id = ? ORDER BY channel_id", ruleID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
