package alert

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
	"opsplatform-alert-backend/notify"
	"opsplatform-alert-backend/safego"
	"opsplatform-alert-backend/timezone"

	"github.com/robfig/cron/v3"
)

// RouteConfig defines field-value based routing for found mode alerts
type RouteConfig struct {
	RouteField    string      `json:"route_field"`     // field name to extract value from (e.g. "code")
	IgnoreValues  []string    `json:"ignore_values"`   // values to ignore (no alert)
	Routes        []RouteRule `json:"routes"`          // routing rules
	DefaultLarkID int         `json:"default_lark_id"` // fallback lark config ID (0 = use rule's default)
}

type RouteRule struct {
	Values []string `json:"values"`  // field values to match
	LarkID int      `json:"lark_id"` // lark config ID to send to
	Name   string   `json:"name"`    // description (e.g. "严重错误群")
}

func parseRouteConfig(configJSON string) *RouteConfig {
	if configJSON == "" {
		return nil
	}
	var cfg RouteConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil
	}
	if cfg.RouteField == "" {
		return nil
	}
	return &cfg
}

// extractCodeFromJSON tries to extract "code" value from JSON string like {"code":"9018","msg":"xxx"}
func extractCodeFromJSON(fieldValue string) string {
	fieldValue = strings.TrimSpace(fieldValue)
	if strings.HasPrefix(fieldValue, "{") {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(fieldValue), &obj); err == nil {
			if code, ok := obj["code"]; ok {
				return fmt.Sprintf("%v", code)
			}
		}
	}
	return fieldValue
}

// matchRoute returns the lark_config_id for the given field value, or -1 to ignore.
// Supports JSON format: if fieldValue is {"code":"9018","msg":"xxx"}, extracts "9018" for matching.
func (rc *RouteConfig) matchRoute(fieldValue string) int {
	// Extract code from JSON if applicable
	codeValue := extractCodeFromJSON(fieldValue)

	// Check ignore list (match against both raw value and extracted code)
	for _, v := range rc.IgnoreValues {
		v = strings.TrimSpace(v)
		if v == fieldValue || v == codeValue {
			return -1 // ignore
		}
	}
	// Check routes
	for _, route := range rc.Routes {
		for _, v := range route.Values {
			v = strings.TrimSpace(v)
			if v == fieldValue || v == codeValue {
				return route.LarkID
			}
		}
	}
	// Default
	return rc.DefaultLarkID
}

// GlobalQuerySemaphore limits total concurrent queries across all rules
var GlobalQuerySemaphore chan struct{}

// InitGlobalSemaphore sets the global concurrency limit
func InitGlobalSemaphore(maxConcurrency int) {
	if maxConcurrency <= 0 {
		maxConcurrency = 20
	}
	GlobalQuerySemaphore = make(chan struct{}, maxConcurrency)
	log.Printf("[Engine] Global query concurrency limit: %d", maxConcurrency)
}

// acquireGlobal acquires a slot from the global semaphore with context timeout
func acquireGlobalCtx(ctx context.Context) bool {
	if GlobalQuerySemaphore == nil {
		return true
	}
	select {
	case GlobalQuerySemaphore <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// releaseGlobal releases a slot back to the global semaphore
func releaseGlobal() {
	if GlobalQuerySemaphore != nil {
		<-GlobalQuerySemaphore
	}
}

// AlertIntervalOnce 表示 not_found 模式下「只告警一次」：
// 从正常→搜不到时发一条告警，之后持续搜不到不再重复发，直到恢复发一条恢复通知，
// 一次告警 + 一次恢复算一个闭环；再次搜不到属于新的闭环，会重新告警。
const AlertIntervalOnce = "once"

// alertStateTTL 是 Redis 中告警状态 key 的存活时间
const alertStateTTL = 7 * 24 * time.Hour

// Engine manages all alert rules and their schedules
type Engine struct {
	mu          sync.RWMutex
	cron        *cron.Cron
	jobs        map[int]cron.EntryID       // ruleID -> cronEntryID
	reportJobs  map[int]cron.EntryID       // ruleID -> daily report cronEntryID (performance alert)
	clients     map[int]*es.Client         // esConnectionID -> ES client
	lokiClients map[int]*lokiclient.Client // lokiConnectionID -> Loki client
	stopping    bool
}

func NewEngine() *Engine {
	return &Engine{
		// A rule's schedule is written in the platform's display zone: "0 3 * * *"
		// means 03:00 where the team is, not 03:00 wherever this container happens
		// to run. Without a location cron would silently follow the process zone.
		cron:        cron.New(cron.WithSeconds(), cron.WithLocation(timezone.Location()), cron.WithChain(jobGuards...)),
		jobs:        make(map[int]cron.EntryID),
		reportJobs:  make(map[int]cron.EntryID),
		clients:     make(map[int]*es.Client),
		lokiClients: make(map[int]*lokiclient.Client),
	}
}

// Start loads all enabled rules and starts the scheduler
func (e *Engine) Start() error {
	log.Println("[Engine] Starting alert engine...")

	if err := e.loadAllRules(); err != nil {
		return fmt.Errorf("failed to load rules: %w", err)
	}

	if err := e.startRetentionJob(); err != nil {
		// Pruning is housekeeping; failing to register it must not stop alerting.
		log.Printf("[Engine] retention job not registered: %v", err)
	}
	if err := e.startMuteReminderJob(); err != nil {
		log.Printf("[Engine] mute reminder job not registered: %v", err)
	}

	e.cron.Start()
	log.Println("[Engine] Alert engine started")

	// Restore container metrics from Redis cache on startup (avoid gap after restart)
	safego.Go("restoreMetricsFromRedis", e.restoreMetricsFromRedis)

	return nil
}

func (e *Engine) restoreMetricsFromRedis() {
	ctx := context.Background()

	// Get all enabled rules
	rows, err := database.DB.Query(`SELECT id, name, alert_mode, COALESCE(prometheus_config,''), COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(namespaces,''), project_id
		FROM alert_rules WHERE status = 1`)
	if err != nil {
		log.Printf("[Metrics] 恢复指标失败: %v", err)
		return
	}
	defer rows.Close()

	restored := 0
	for rows.Next() {
		var ruleID, projectID int
		var name, alertMode, promConfig, groupBy, expectedGroups, namespaces string
		rows.Scan(&ruleID, &name, &alertMode, &promConfig, &groupBy, &expectedGroups, &namespaces, &projectID)

		ruleIDStr := fmt.Sprintf("%d", ruleID)
		promCfg := ParsePrometheusConfig(promConfig)
		projectLabels := getProjectLabels(projectID)

		var staticLabels map[string]string
		mergedLabels := map[string]string{}
		for k, v := range projectLabels {
			mergedLabels[k] = v
		}
		if promCfg != nil {
			for k, v := range promCfg.GetStaticLabels() {
				mergedLabels[k] = v
			}
		}
		staticLabels = mergedLabels

		mode := alertMode
		if mode == "" {
			mode = "found"
		}

		if mode == "not_found" && expectedGroups != "" {
			// Restore not_found mode from Redis state keys
			var groups []string
			json.Unmarshal([]byte(expectedGroups), &groups)
			for _, gk := range groups {
				stateKey := fmt.Sprintf("alert:state:%d:%s", ruleID, gk)
				state, _ := database.RDB.Get(ctx, stateKey).Result()
				muted := isMuted(ruleID, gk)
				var status float64
				if muted {
					status = -1
				} else if state == "alerting" {
					status = 0
				} else {
					status = 1
				}
				if Metrics != nil {
					Metrics.RecordContainerStatus(ruleIDStr, name, "", gk, mode, status, staticLabels)
					restored++
				}
			}
		}

		// Restore found mode from Redis container_status keys
		if mode == "found" && namespaces != "" {
			var nsList []string
			json.Unmarshal([]byte(namespaces), &nsList)
			for _, ns := range nsList {
				pattern := fmt.Sprintf("alert:container_status:%d:%s:*", ruleID, ns)
				keys, _ := database.RDB.Keys(ctx, pattern).Result()
				for _, key := range keys {
					// key format: alert:container_status:{ruleID}:{ns}:{container}
					parts := strings.SplitN(key, ":", 5)
					if len(parts) < 5 {
						continue
					}
					container := parts[4]
					val, err := database.RDB.Get(ctx, key).Int()
					if err != nil {
						continue
					}
					if Metrics != nil {
						Metrics.RecordContainerStatus(ruleIDStr, name, ns, container, mode, float64(val), staticLabels)
						restored++
					}
				}
			}
		}

		// Restore error code metrics from Redis
		if mode == "found" && namespaces != "" {
			var nsList []string
			json.Unmarshal([]byte(namespaces), &nsList)
			for _, ns := range nsList {
				pattern := fmt.Sprintf("alert:error_code:%d:%s:*", ruleID, ns)
				keys, _ := database.RDB.Keys(ctx, pattern).Result()
				for _, key := range keys {
					// key format: alert:error_code:{ruleID}:{ns}:{container}:{code}
					parts := strings.SplitN(key, ":", 6)
					if len(parts) < 6 {
						continue
					}
					container := parts[4]
					code := parts[5]
					val, err := database.RDB.Get(ctx, key).Result()
					if err != nil {
						continue
					}
					// Parse cached JSON {"msg":"xxx","value":123}
					var cached struct {
						Msg   string  `json:"msg"`
						Value float64 `json:"value"`
					}
					if json.Unmarshal([]byte(val), &cached) != nil {
						continue
					}
					if Metrics != nil {
						Metrics.RecordErrorCode(ruleIDStr, name, ns, container, code, cached.Msg, cached.Value, staticLabels)
						restored++
					}
				}
			}
		}

		// Restore alerting_count
		countKey := fmt.Sprintf("alert:alerting_count:%d", ruleID)
		if _, err := database.RDB.Get(ctx, countKey).Result(); err != nil {
			database.RDB.Set(ctx, countKey, 0, 10*time.Minute)
		}
	}

	if restored > 0 {
		log.Printf("[Metrics] 从 Redis 恢复了 %d 条指标(容器状态+错误码)", restored)
	}
}

// Stop gracefully stops the engine
func (e *Engine) Stop() {
	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()

	ctx := e.cron.Stop()
	<-ctx.Done()
	log.Println("[Engine] Alert engine stopped")
}

// RebuildSchedule rebuilds the scheduler against the current display timezone.
//
// A cron entry's location is fixed when the scheduler is created, so changing
// the platform's zone has no effect on already-registered jobs — a rule set to
// fire at 03:00 would keep firing at 03:00 in the old zone until a restart.
// Changing the setting therefore has to swap the scheduler outright and
// re-register every rule against the new location.
func (e *Engine) RebuildSchedule() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.stopping {
		return nil
	}

	// Stop the old scheduler and wait for jobs already running to finish, so a
	// rule cannot be executing against the old schedule while it is re-added.
	<-e.cron.Stop().Done()

	e.cron = cron.New(cron.WithSeconds(), cron.WithLocation(timezone.Location()), cron.WithChain(jobGuards...))
	e.jobs = make(map[int]cron.EntryID)
	e.reportJobs = make(map[int]cron.EntryID)

	if err := e.loadAllRules(); err != nil {
		// Start what did register rather than leaving the platform with no
		// scheduler at all; the rules that failed are reported by loadAllRules.
		e.cron.Start()
		return fmt.Errorf("failed to reload rules after timezone change: %w", err)
	}

	// The scheduler was replaced, so every entry on it was too — the retention
	// job has to be put back or it would quietly stop running after the first
	// timezone change.
	if err := e.startRetentionJob(); err != nil {
		log.Printf("[Engine] retention job not re-registered: %v", err)
	}
	if err := e.startMuteReminderJob(); err != nil {
		log.Printf("[Engine] mute reminder job not re-registered: %v", err)
	}

	e.cron.Start()
	log.Printf("[Engine] Schedule rebuilt for timezone %s", timezone.Name())
	return nil
}

// ReloadRule reloads a single rule (add/update/remove)
func (e *Engine) ReloadRule(ruleID int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove existing main job if present
	if entryID, ok := e.jobs[ruleID]; ok {
		e.cron.Remove(entryID)
		delete(e.jobs, ruleID)
		log.Printf("[Engine] Removed job for rule %d", ruleID)
	}
	// Remove existing report job if present
	if entryID, ok := e.reportJobs[ruleID]; ok {
		e.cron.Remove(entryID)
		delete(e.reportJobs, ruleID)
		log.Printf("[Engine] Removed report job for rule %d", ruleID)
	}

	// Fetch rule from DB
	rule, err := getRuleByID(ruleID)
	if err != nil {
		log.Printf("[Engine] Rule %d not found or deleted, skipping", ruleID)
		return nil // Rule deleted, that's fine
	}

	if rule.Status != 1 {
		log.Printf("[Engine] Rule %d is disabled, skipping", ruleID)
		return nil
	}

	if err := e.addJob(rule); err != nil {
		return err
	}
	if rule.ReportEnabled == 1 {
		if err := e.addReportJob(rule); err != nil {
			log.Printf("[Engine] Failed to add report job for rule %d: %v", rule.ID, err)
		}
	}
	return nil
}

// RemoveRule removes a rule's job
func (e *Engine) RemoveRule(ruleID int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if entryID, ok := e.jobs[ruleID]; ok {
		e.cron.Remove(entryID)
		delete(e.jobs, ruleID)
		log.Printf("[Engine] Removed job for rule %d", ruleID)
	}
	if entryID, ok := e.reportJobs[ruleID]; ok {
		e.cron.Remove(entryID)
		delete(e.reportJobs, ruleID)
		log.Printf("[Engine] Removed report job for rule %d", ruleID)
	}
}

// CleanupRuleRedisKeys removes all Redis keys for a deleted rule
func (e *Engine) CleanupRuleRedisKeys(ruleID int) {
	ctx := context.Background()
	patterns := []string{
		fmt.Sprintf("alert:state:%d:*", ruleID),
		fmt.Sprintf("alert:last_hit:%d:*", ruleID),
		fmt.Sprintf("alert:dedup:%d:*", ruleID),
		fmt.Sprintf("alert:daily_stats:%d:*", ruleID),
		fmt.Sprintf("alert:daily_stats_domains:%d:*", ruleID),
		fmt.Sprintf("alert:sent_tids:%d:*", ruleID),
	}
	for _, pattern := range patterns {
		iter := database.RDB.Scan(ctx, 0, pattern, 100).Iterator()
		for iter.Next(ctx) {
			database.RDB.Del(ctx, iter.Val())
		}
	}
	// Also clean non-grouped state key
	database.RDB.Del(ctx, fmt.Sprintf("alert:state:%d", ruleID))
	log.Printf("[Engine] Cleaned Redis keys for rule %d", ruleID)
}

// RefreshESClient refreshes or removes an ES client
func (e *Engine) RefreshESClient(connID int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.clients, connID)
}

func (e *Engine) loadAllRules() error {
	rows, err := database.DB.Query(`SELECT id, name, COALESCE(data_source_type,'es'),
		es_connection_id, COALESCE(loki_connection_id,0), lark_config_id, es_index,
		schedule, time_range, COALESCE(query_dsl,''), keyword, COALESCE(logql,''),
		COALESCE(filter_fields,''), COALESCE(extract_fields,''),
		message_title, COALESCE(message_template,''),
		COALESCE(at_users,''), at_all, COALESCE(alert_mode,'found'),
		recovery_enabled, COALESCE(recovery_title,''), COALESCE(recovery_template,''),
		severity, COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(query_concurrency,5), COALESCE(alert_interval,''), dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''), COALESCE(route_config,''),
		COALESCE(realtime_enabled,0), COALESCE(threshold_ms,0), COALESCE(report_enabled,0), COALESCE(report_schedule,''), COALESCE(report_mode,'separate'), COALESCE(report_title,''), COALESCE(report_template,''),
		status
		FROM alert_rules WHERE status = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var rule models.AlertRule
		err := rows.Scan(&rule.ID, &rule.Name, &rule.DataSourceType,
			&rule.ESConnectionID, &rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
			&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
			&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
			&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
			&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
			&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
			&rule.PrometheusConfig, &rule.RouteConfig,
			&rule.RealtimeEnabled, &rule.ThresholdMs, &rule.ReportEnabled, &rule.ReportSchedule, &rule.ReportMode, &rule.ReportTitle, &rule.ReportTemplate,
			&rule.Status)
		if err != nil {
			log.Printf("[Engine] Failed to scan rule: %v", err)
			continue
		}

		if err := e.addJob(&rule); err != nil {
			log.Printf("[Engine] Failed to add job for rule %d: %v", rule.ID, err)
			continue
		}
		// Register daily report cron if enabled
		if rule.ReportEnabled == 1 {
			if err := e.addReportJob(&rule); err != nil {
				log.Printf("[Engine] Failed to add report job for rule %d: %v", rule.ID, err)
			}
		}
		// Register Prometheus metrics
		if Metrics != nil {
			ruleIDStr := fmt.Sprintf("%d", rule.ID)
			Metrics.SetRuleStatus(ruleIDStr, rule.Name, rule.Severity, rule.Status == 1)
			Metrics.RegisterCustomMetrics(rule.PrometheusConfig)
		}
		count++
	}

	log.Printf("[Engine] Loaded %d alert rules", count)
	return nil
}

func (e *Engine) addJob(rule *models.AlertRule) error {
	schedule := NormalizeSchedule(rule.Schedule)

	entryID, err := e.cron.AddFunc(schedule, func() {
		e.executeRule(rule.ID)
	})
	if err != nil {
		return fmt.Errorf("invalid cron schedule '%s': %w", schedule, err)
	}

	e.jobs[rule.ID] = entryID
	log.Printf("[Engine] Added job for rule %d '%s' schedule=%s", rule.ID, rule.Name, schedule)
	return nil
}

// addReportJob registers a daily-report cron entry for a performance-alert rule.
// Caller must hold e.mu (or be in a single-threaded init path like loadAllRules).
func (e *Engine) addReportJob(rule *models.AlertRule) error {
	schedule := rule.ReportSchedule
	if schedule == "" {
		schedule = "0 1 0 * * *"
	}
	schedule = NormalizeSchedule(schedule)
	ruleID := rule.ID
	reportSchedule := schedule
	entryID, err := e.cron.AddFunc(schedule, func() {
		// A daily report sent once per replica is the same defect as a
		// duplicated alert, and more visible — it arrives in the group N times.
		release, ok := tryLock(reportLockKey(ruleID), lockTTL(reportSchedule))
		if !ok {
			log.Printf("[Engine] Rule %d: daily report already being sent elsewhere, skipping", ruleID)
			return
		}
		defer release()
		sendDailyReport(ruleID)
	})
	if err != nil {
		return fmt.Errorf("invalid report schedule '%s': %w", schedule, err)
	}
	e.reportJobs[rule.ID] = entryID
	log.Printf("[Engine] Added report job for rule %d '%s' schedule=%s", rule.ID, rule.Name, schedule)
	return nil
}

// ExecuteRule manually triggers rule execution (exported for handler use)
func (e *Engine) ExecuteRule(ruleID int) {
	e.executeRule(ruleID)
}

func (e *Engine) executeRule(ruleID int) {
	e.mu.RLock()
	if e.stopping {
		e.mu.RUnlock()
		return
	}
	e.mu.RUnlock()

	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Fetch latest rule config from DB (supports hot reload)
	rule, err := getRuleByID(ruleID)
	if err != nil {
		log.Printf("[Engine] Rule %d: fetch error: %v", ruleID, err)
		return
	}
	if rule.Status != 1 {
		return
	}

	// One run of this rule at a time across the platform: another replica may
	// hold its own scheduler, and cron will start the next run whether or not
	// this one has finished. See alert/lock.go — this fails open, so a Redis
	// outage costs a duplicate alert rather than a missed one.
	release, ok := tryLock(ruleLockKey(ruleID), lockTTL(rule.Schedule))
	if !ok {
		log.Printf("[Engine] Rule %d '%s': previous run still in progress, skipping", rule.ID, rule.Name)
		return
	}
	defer release()

	log.Printf("[Engine] Executing rule %d '%s'", rule.ID, rule.Name)

	// Update last_run_at
	database.DB.Exec("UPDATE alert_rules SET last_run_at = NOW(), last_error = NULL WHERE id = ?", rule.ID)

	dataSourceType := rule.DataSourceType
	if dataSourceType == "" {
		dataSourceType = "es"
	}

	// Determine alert mode
	alertMode := rule.AlertMode
	if alertMode == "" {
		alertMode = "found"
	}

	// Get notification channels (Lark and/or Telegram)
	channels, err := getChannelsForRule(rule.ID, rule.LarkConfigID)
	if err != nil {
		errMsg := fmt.Sprintf("Notify channel error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		return
	}

	sender, err := notify.NewMulti(channels)
	if err != nil {
		errMsg := fmt.Sprintf("Notify channel error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		return
	}

	// Parse at_users (supports ["name1","name2"] or [{"name":"x","user_id":"y"}])
	atUsers := resolveAtUsers(rule.AtUsers)
	atAll := rule.AtAll == 1
	ruleIDStr := fmt.Sprintf("%d", rule.ID)

	// ========== Multi-namespace mode: loop through namespaces with concurrency control ==========
	if dataSourceType == "loki" && rule.Namespaces != "" {
		var namespaces []string
		if err := json.Unmarshal([]byte(rule.Namespaces), &namespaces); err == nil && len(namespaces) > 0 {
			log.Printf("[Engine] Rule %d: multi-namespace mode, %d namespaces", rule.ID, len(namespaces))
			e.executeNamespacedRule(ctx, rule, namespaces, sender, atUsers, atAll, ruleIDStr, alertMode)
			if Metrics != nil {
				duration := time.Since(startTime).Seconds()
				Metrics.RecordRuleRun(ruleIDStr, rule.Name, rule.Severity, 0, duration)
			}
			return
		}
	}

	// ========== Grouped not_found with expected_groups: skip batch query, query per container ==========
	if rule.GroupBy != "" && alertMode == "not_found" && rule.ExpectedGroups != "" {
		log.Printf("[Engine] Rule %d: grouped not_found with expected_groups, skipping batch query", rule.ID)
		emptyGroups := make(map[string][]map[string]interface{})
		e.executeGroupedNotFound(ctx, rule, sender, atUsers, atAll, ruleIDStr, emptyGroups, strings.TrimSpace(rule.GroupBy), dataSourceType)

		if Metrics != nil {
			duration := time.Since(startTime).Seconds()
			Metrics.RecordRuleRun(ruleIDStr, rule.Name, rule.Severity, 0, duration)
		}
		return
	}

	// Query data source (ES or Loki)
	var hits []map[string]interface{}
	var totalHits int64

	if dataSourceType == "loki" {
		hits, totalHits, err = e.queryLoki(ctx, rule)
	} else {
		hits, totalHits, err = e.queryES(ctx, rule)
	}
	if err != nil {
		errMsg := fmt.Sprintf("Query error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		saveAlertLog(rule, "", "", "failed", errMsg, "")
		return
	}

	// Build a compatible result for downstream processing
	result := &es.SearchResult{Hits: hits, Total: totalHits}

	log.Printf("[Engine] Rule %d: found %d hits (total=%d) [%s]", rule.ID, len(result.Hits), result.Total, dataSourceType)

	// ========== Group By support ==========
	if rule.GroupBy != "" {
		e.executeGroupedRule(ctx, rule, result, sender, atUsers, atAll, ruleIDStr, alertMode, dataSourceType)

		// Update Prometheus metrics
		if Metrics != nil {
			duration := time.Since(startTime).Seconds()
			Metrics.RecordRuleRun(ruleIDStr, rule.Name, rule.Severity, len(result.Hits), duration)
		}
		return
	}

	// ========== not_found mode: alert when NO hits ==========
	if alertMode == "not_found" {
		stateKey := fmt.Sprintf("alert:state:%d", rule.ID)
		lastAlertKey := fmt.Sprintf("alert:last_alert_time:%d", rule.ID)
		prevState, _ := database.RDB.Get(ctx, stateKey).Result() // "alerting" or "" (normal)

		if len(result.Hits) == 0 {
			// No hits → should be alerting
			if prevState == "alerting" {
				// alert_interval=once: 一次告警 + 一次恢复为一个闭环，恢复前不重复发
				if rule.AlertInterval == AlertIntervalOnce {
					// 续期状态 key，避免长时间未恢复时 TTL 过期被误判为新故障
					database.RDB.Expire(ctx, stateKey, alertStateTTL)
					log.Printf("[Engine] Rule %d: once mode, already alerting, skip until recovery", rule.ID)
					return
				}
				// Already alerting, check alert_interval for repeat notification
				alertInterval := time.Duration(0)
				if rule.AlertInterval != "" {
					if strings.HasSuffix(rule.AlertInterval, "d") {
						days := 1
						fmt.Sscanf(rule.AlertInterval, "%dd", &days)
						alertInterval = time.Duration(days) * 24 * time.Hour
					} else if d, err := time.ParseDuration(rule.AlertInterval); err == nil {
						alertInterval = d
					}
				}
				if alertInterval > 0 {
					lastAlertStr, _ := database.RDB.Get(ctx, lastAlertKey).Result()
					if lastAlertStr != "" {
						if lastAlert, err := time.Parse(time.RFC3339, lastAlertStr); err == nil {
							if time.Since(lastAlert) < alertInterval {
								log.Printf("[Engine] Rule %d: still alerting, interval not reached, skip", rule.ID)
								return
							}
						}
					}
					log.Printf("[Engine] Rule %d: still alerting, interval reached, re-alerting", rule.ID)
				} else {
					// No interval set, alert every time
					log.Printf("[Engine] Rule %d: still alerting (no interval), re-alerting", rule.ID)
				}
			} else {
				// Transition: normal → alerting
				log.Printf("[Engine] Rule %d: no hits in %s, triggering alert", rule.ID, rule.TimeRange)
				database.RDB.Set(ctx, stateKey, "alerting", 7*24*time.Hour)
			}

			// Search wider range for the last known log (max 3h to avoid Loki OOM)
			lastHitMsg := "在指定时间范围内未搜到匹配日志"
			var widerHits []map[string]interface{}
			if dataSourceType == "loki" {
				widerHits, _ = e.queryLokiWider(ctx, rule, "3h", 1)
			} else {
				widerQuery, _ := es.BuildQuery(rule.Keyword, rule.FilterFields, "3h", "", 1)
				esClient, cErr := e.getESClient(rule.ESConnectionID)
				if cErr == nil {
					widerResult, wErr := esClient.Search(ctx, rule.ESIndex, widerQuery)
					if wErr == nil {
						widerHits = widerResult.Hits
					}
				}
			}
			var rawJSON []byte
			vars := map[string]interface{}{"alert_reason": "not_found", "time_range": rule.TimeRange}
			if len(widerHits) > 0 {
				vars = extractFields(widerHits[0], rule.ExtractFields)
				vars["alert_reason"] = "not_found"
				vars["time_range"] = rule.TimeRange
				rawJSON, _ = json.Marshal(widerHits[0])

				// If any extracted field is empty, query more hits to find non-empty values
				if hasEmptyExtractFields(vars, rule.ExtractFields) {
					var moreHits []map[string]interface{}
					if dataSourceType == "loki" {
						moreHits, _ = e.queryLokiWider(ctx, rule, "3h", 10)
					} else {
						moreQuery, _ := es.BuildQuery(rule.Keyword, rule.FilterFields, "3h", "", 10)
						esClient, cErr := e.getESClient(rule.ESConnectionID)
						if cErr == nil {
							moreResult, wErr := esClient.Search(ctx, rule.ESIndex, moreQuery)
							if wErr == nil {
								moreHits = moreResult.Hits
							}
						}
					}
					fillEmptyFields(vars, moreHits, rule.ExtractFields)
				}

				lastHitMsg = renderTemplate(rule.MessageTemplate, vars)
			} else if rule.MessageTemplate != "" {
				lastHitMsg = renderTemplate(rule.MessageTemplate, vars)
			}

			// Send alert
			resp, sErr := sender.SendCard(rule.MessageTitle, lastHitMsg, rule.Severity, atUsers, atAll)
			if sErr != nil {
				errMsg := fmt.Sprintf("notify send error: %v", sErr)
				database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
				saveAlertLog(rule, lastHitMsg, string(rawJSON), "failed", errMsg, resp)
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
				}
			} else {
				note := partialSendNote(resp)
				if note == "" {
					database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
				}
				database.RDB.Set(ctx, lastAlertKey, time.Now().Format(time.RFC3339), 7*24*time.Hour)
				saveAlertLog(rule, lastHitMsg, string(rawJSON), "success", note, resp)
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
				}
				if note != "" {
					recordPartialSend(rule, ruleIDStr, note)
				}
				log.Printf("[Engine] Rule %d: not_found alert sent, resp=%s", rule.ID, resp)
			}
		} else {
			// Has hits → should be normal
			if prevState == "alerting" && rule.RecoveryEnabled == 1 {
				// Transition: alerting → normal, send recovery
				log.Printf("[Engine] Rule %d: recovered, sending recovery notification", rule.ID)
				database.RDB.Set(ctx, stateKey, "normal", 7*24*time.Hour)
				database.RDB.Del(ctx, lastAlertKey) // clear last alert time

				// Use the earliest hit (last element, since results are sorted desc by time)
				earliestHit := result.Hits[len(result.Hits)-1]
				vars := extractFields(earliestHit, rule.ExtractFields)
				vars["alert_reason"] = "recovered"
				rawJSON, _ := json.Marshal(earliestHit)

				title := rule.RecoveryTitle
				if title == "" {
					title = rule.MessageTitle + " - 已恢复"
				}
				tmpl := rule.RecoveryTemplate
				if tmpl == "" {
					tmpl = rule.MessageTemplate
				}
				message := renderTemplate(tmpl, vars)

				resp, sErr := sender.SendCard(title, message, "recovery", atUsers, atAll)
				if sErr != nil {
					saveAlertLog(rule, message, string(rawJSON), "failed", fmt.Sprintf("Recovery send error: %v", sErr), resp)
				} else {
					saveAlertLog(rule, message, string(rawJSON), "success", "", resp)
					log.Printf("[Engine] Rule %d: recovery sent, resp=%s", rule.ID, resp)
				}
			} else if prevState == "alerting" {
				// Recovery not enabled, just clear state
				database.RDB.Set(ctx, stateKey, "normal", 7*24*time.Hour)
				database.RDB.Del(ctx, lastAlertKey)
				log.Printf("[Engine] Rule %d: recovered (no recovery notification)", rule.ID)
			} else {
				log.Printf("[Engine] Rule %d: normal (hits found)", rule.ID)
			}
		}
		return
	}

	// ========== found mode (default): alert when hits found ==========
	if len(result.Hits) == 0 {
		log.Printf("[Engine] Rule %d: no hits", rule.ID)
		return
	}

	// Parse route config for found mode
	routeCfg := parseRouteConfig(rule.RouteConfig)
	// Cache of senders for routed channels. Routing targets exactly one channel,
	// overriding the rule's multi-channel default.
	senderCache := map[int]notify.Notifier{}

	sentCount := 0
	// Contexts fetched so far in THIS run, capped by MaxContextFetchesPerRun:
	// the extra queries come out of a pool shared with every other rule.
	ctxFetches := 0
	partialNote := "" // last partial fan-out seen; keeps last_error from being cleared
	for _, hit := range result.Hits {
		vars := extractFields(hit, rule.ExtractFields)

		// ===== Performance alert: URL count check + daily stats + threshold filter =====
		if rule.RealtimeEnabled == 1 || rule.ReportEnabled == 1 {
			if !processPerformanceHit(ctx, rule, hit, vars) {
				continue
			}
		}
		// ===== end performance alert =====

		// Route: check if this hit should be ignored or sent to a different group
		activeSender := sender
		if routeCfg != nil {
			fieldVal := fmt.Sprintf("%v", vars[routeCfg.RouteField])
			larkID := routeCfg.matchRoute(fieldVal)
			if larkID == -1 {
				log.Printf("[Engine] Rule %d: ignoring hit with %s=%s", rule.ID, routeCfg.RouteField, fieldVal)
				continue
			}
			if larkID > 0 && larkID != rule.LarkConfigID {
				if cached, ok := senderCache[larkID]; ok {
					activeSender = cached
				} else if routeCh, err := getChannelByID(larkID); err == nil {
					if routeSender, err := notify.New(*routeCh); err == nil {
						activeSender = routeSender
						senderCache[larkID] = routeSender
					} else {
						log.Printf("[Engine] Rule %d: route channel_id=%d unusable (%v), using default", rule.ID, larkID, err)
					}
				} else {
					log.Printf("[Engine] Rule %d: route channel_id=%d not found, using default", rule.ID, larkID)
				}
			}
		}

		// Dedup check
		if rule.DedupField != "" {
			dedupKey := buildDedupKey(ruleID, vars, rule.DedupField)
			ttl := time.Duration(rule.DedupTTL) * time.Second
			if ttl <= 0 {
				ttl = time.Hour
			}
			isDup, err := database.CheckDedup(ctx, dedupKey, ttl)
			if err != nil {
				log.Printf("[Engine] Rule %d: dedup check error: %v", rule.ID, err)
			}
			if isDup {
				log.Printf("[Engine] Rule %d: duplicate alert skipped, key=%s", rule.ID, dedupKey)
				continue
			}
		}

		if rule.StackContextEnabled == 1 {
			vars["stack"] = e.fetchStackContext(ctx, rule, hit)
		}
		if rule.LogContextEnabled == 1 {
			if ctxFetches < MaxContextFetchesPerRun {
				vars["logcontext"] = e.fetchLogContext(ctx, rule, hit)
				ctxFetches++
			} else {
				vars["logcontext"] = RenderContextSkipped(hit, MaxContextFetchesPerRun)
			}
		}

		message := renderTemplate(rule.MessageTemplate, vars)

		// Zero-config path: the operator turned the switch on but never referenced
		// the variable, so put the stack where it can still be read.
		if rule.StackContextEnabled == 1 && !strings.Contains(rule.MessageTemplate, "{{.stack}}") {
			if s, ok := vars["stack"].(string); ok && s != "" {
				message += "\n```\n" + s + "\n```"
			}
		}
		// Same zero-config path for the log context, under its own switch: the
		// two features are independent, and a template may reference one
		// variable while leaving the other to be appended.
		if rule.LogContextEnabled == 1 && !strings.Contains(rule.MessageTemplate, "{{.logcontext}}") {
			if s, ok := vars["logcontext"].(string); ok && s != "" {
				message += "\n" + LogContextCaption + "\n```\n" + s + "\n```"
			}
		}

		rawJSON, _ := json.Marshal(hit)

		time.Sleep(200 * time.Millisecond)
		resp, err := activeSender.SendCard(rule.MessageTitle, message, rule.Severity, atUsers, atAll)
		if err != nil {
			errMsg := fmt.Sprintf("notify send error: %v", err)
			log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
			database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
			saveAlertLog(rule, message, string(rawJSON), "failed", errMsg, resp)
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
			}
			continue
		}

		note := partialSendNote(resp)
		saveAlertLog(rule, message, string(rawJSON), "success", note, resp)
		sentCount++
		if Metrics != nil {
			Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
			Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
		}
		if note != "" {
			partialNote = note
			recordPartialSend(rule, ruleIDStr, note)
		}
		log.Printf("[Engine] Rule %d: alert sent (%d/%d), resp=%s", rule.ID, sentCount, len(result.Hits), resp)
	}

	// A partial fan-out must survive here: clearing last_error would hide a
	// channel that failed on every send behind the channels that succeeded.
	if sentCount > 0 && partialNote == "" {
		database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
	}

	// Update Prometheus metrics
	if Metrics != nil {
		ruleIDStr := fmt.Sprintf("%d", rule.ID)
		duration := time.Since(startTime).Seconds()
		Metrics.RecordRuleRun(ruleIDStr, rule.Name, rule.Severity, len(result.Hits), duration)
	}
}

// extractFields extracts variables from ES hit based on extract_fields config
func extractFields(hit map[string]interface{}, extractFieldsJSON string) map[string]interface{} {
	vars := make(map[string]interface{})

	// Always add raw fields
	for k, v := range hit {
		// Internal plumbing keys are not template variables.
		if strings.HasPrefix(k, "__") {
			continue
		}
		vars[k] = v
	}

	// Add timestamp
	if ts, ok := hit["@timestamp"]; ok {
		vars["time"] = ts
	}

	if extractFieldsJSON == "" {
		return vars
	}

	var fields []models.ExtractField
	if err := json.Unmarshal([]byte(extractFieldsJSON), &fields); err != nil {
		log.Printf("[Extract] Failed to parse extract_fields: %v", err)
		return vars
	}

	for _, f := range fields {
		// Get source value
		val := es.GetNestedField(hit, f.Path)
		if val == nil {
			continue
		}

		valStr := fmt.Sprintf("%v", val)

		if f.Pattern != "" {
			// Apply regex extraction
			re, err := regexp.Compile(f.Pattern)
			if err != nil {
				log.Printf("[Extract] Invalid regex '%s': %v", f.Pattern, err)
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

// hasEmptyExtractFields checks if any extracted field has an empty value
func hasEmptyExtractFields(vars map[string]interface{}, extractFieldsJSON string) bool {
	if extractFieldsJSON == "" {
		return false
	}
	var fields []models.ExtractField
	if err := json.Unmarshal([]byte(extractFieldsJSON), &fields); err != nil {
		return false
	}
	for _, f := range fields {
		val := vars[f.Name]
		if val == nil {
			return true
		}
		s := fmt.Sprintf("%v", val)
		if s == "" || s == "<nil>" {
			return true
		}
	}
	return false
}

// fillEmptyFields fills empty extracted fields from subsequent hits
func fillEmptyFields(vars map[string]interface{}, hits []map[string]interface{}, extractFieldsJSON string) {
	if extractFieldsJSON == "" || len(hits) == 0 {
		return
	}
	var fields []models.ExtractField
	if err := json.Unmarshal([]byte(extractFieldsJSON), &fields); err != nil {
		return
	}
	for _, f := range fields {
		val := vars[f.Name]
		s := fmt.Sprintf("%v", val)
		if s != "" && s != "<nil>" && val != nil {
			continue // already has value
		}
		// Search through hits for a non-empty value
		for _, hit := range hits {
			hitVars := extractFields(hit, extractFieldsJSON)
			hitVal := hitVars[f.Name]
			hitStr := fmt.Sprintf("%v", hitVal)
			if hitVal != nil && hitStr != "" && hitStr != "<nil>" {
				vars[f.Name] = hitVal
				log.Printf("[Engine] Filled empty field '%s' with value '%s' from earlier hit", f.Name, hitStr)
				break
			}
		}
	}
}

// fetchStackContext returns the matched line together with its continuation
// lines, ready to drop into a fenced code block.
//
// When the collector already merged the stack into one record the line itself
// carries newlines, and no extra query is needed — which is both the common case
// and the cheap one. Only a genuinely single-line match triggers a follow-up
// query, and only for hits that survived dedup, mute and routing.
// FetchStackContext is the package-level entry point; see FetchLogContext for
// why the client getter is a parameter.
func FetchStackContext(ctx context.Context, rule *models.AlertRule, hit map[string]interface{},
	getClient func(int) (*lokiclient.Client, error)) string {
	line, _ := hit["message"].(string)
	if line == "" {
		return ""
	}

	// Already merged upstream: the whole stack is right here.
	if strings.Contains(line, "\n") {
		return trimStack(rule, strings.Split(line, "\n"))
	}

	labels, _ := hit["__stream_labels"].(map[string]string)
	selector := buildStreamSelector(labels)
	if selector == "" || rule.LokiConnectionID == 0 {
		return line
	}

	// The exact instant, not the rendered one: hit["timestamp"] has already been
	// rewritten into a display string ("2026-09-09 22:04:54 (+08:00 Asia/…)")
	// by ToHits, which no fixed layout parse can read back and which has lost
	// sub-second precision anyway.
	start, ok := hitInstant(hit)
	if !ok {
		log.Printf("[Stack] Rule %d: hit carries no usable timestamp, sending the matched line alone", rule.ID)
		return line
	}

	window := rule.StackWindowSec
	if window <= 0 {
		window = 5
	}
	maxLines := rule.StackMaxLines
	if maxLines <= 0 {
		maxLines = 200
	}

	client, err := getClient(rule.LokiConnectionID)
	if err != nil {
		log.Printf("[Stack] Rule %d: loki client error: %v", rule.ID, err)
		return line
	}

	if !acquireGlobalCtx(ctx) {
		log.Printf("[Stack] Rule %d: no query slot, sending the matched line alone", rule.ID)
		return line
	}
	defer releaseGlobal()

	result, err := client.QueryRangeDirection(ctx, selector,
		start, start.Add(time.Duration(window)*time.Second), maxLines+1, "forward")
	if err != nil {
		log.Printf("[Stack] Rule %d: context query failed: %v", rule.ID, err)
		return line
	}

	lines := make([]string, 0, maxLines+1)
	for _, h := range result.ToHits() {
		if s, ok := h["message"].(string); ok {
			lines = append(lines, s)
		}
	}
	if len(lines) == 0 {
		return line
	}

	boundary, err := CompileBoundary(rule.StackBoundaryPattern)
	if err != nil {
		log.Printf("[Stack] Rule %d: %v — falling back to the default pattern", rule.ID, err)
		boundary, _ = CompileBoundary("")
	}
	return trimStack(rule, CollectStack(lines, boundary, maxLines))
}

// fetchLogContext returns the matched line together with the whole log records
// around it — N before and M after — ready to drop into a fenced code block.
//
// This is a different job from fetchStackContext above, which walks ONE record's
// continuation lines forward until a boundary. Here boundaries are irrelevant:
// the neighbours ARE separate records, and they are what explains what the
// service was doing when it failed.
//
// Two queries are needed because Loki cannot return both directions at once:
// "backward" from the hit for the preceding lines, "forward" for the following
// ones. Each walks the escalating window ladder (see WindowLadder) and stops at
// the first window that satisfies the requested count.
// FetchLogContext is the package-level entry point, taking the Loki client
// getter as a parameter so callers that hold no Engine — the preview and
// test-send handlers — can render exactly what a real alert would carry.
// Showing the operator a preview WITHOUT the context they just switched on
// makes a working feature look broken, which is what it did.
func FetchLogContext(ctx context.Context, rule *models.AlertRule, hit map[string]interface{},
	getClient func(int) (*lokiclient.Client, error)) string {
	line, _ := hit["message"].(string)
	if line == "" {
		return ""
	}
	labels, _ := hit["__stream_labels"].(map[string]string)
	selector := buildStreamSelector(labels)
	if selector == "" || rule.LokiConnectionID == 0 {
		return ""
	}

	hitTime, ok := hitInstant(hit)
	if !ok {
		log.Printf("[LogContext] Rule %d: hit carries no usable timestamp, skipping context", rule.ID)
		return ""
	}

	before := clampContextLines(rule.LogContextBefore, DefaultLogContextBefore)
	after := clampContextLines(rule.LogContextAfter, DefaultLogContextAfter)
	maxWindow := rule.LogContextMaxWindowSec
	if maxWindow <= 0 {
		maxWindow = DefaultLogContextMaxWindowSec
	}
	display := rule.LogContextDisplayLines
	if display <= 0 {
		display = DefaultLogContextDisplayLines
	}

	client, err := getClient(rule.LokiConnectionID)
	if err != nil {
		log.Printf("[LogContext] Rule %d: loki client error: %v", rule.ID, err)
		return ""
	}

	block := ContextBlock{
		Hit:        line,
		WantBefore: before,
		WantAfter:  after,
		HitTime:    hitTime,
		Selector:   selector,
	}
	block.Before, block.WindowBefore, block.FailedBefore = climbLadder(ctx, rule, client, selector, hitTime, line, before, maxWindow, "backward")
	block.After, block.WindowAfter, block.FailedAfter = climbLadder(ctx, rule, client, selector, hitTime, line, after, maxWindow, "forward")

	return block.Render(display)
}

// climbLadder walks the escalating window ladder in one direction until it has
// want lines, and returns them in chronological order along with the window it
// settled on.
//
// Stopping at the first sufficient window is the whole point: a busy container
// satisfies 25 lines inside the first 30-second window, and never pays for the
// index lookup a 30-minute range would cost. A quiet stream climbs further, but
// a quiet stream is cheap to scan precisely because it holds so little.
//
// The matched line itself comes back in every one of these queries — the hit's
// own timestamp is an endpoint of the range — so it is dropped here rather than
// rendered twice: once as a neighbour and once as the marked hit.
func climbLadder(ctx context.Context, rule *models.AlertRule, client *lokiclient.Client,
	selector string, hitTime time.Time, hitLine string, want, maxWindowSec int, direction string) ([]string, time.Duration, bool) {

	var out []string
	var used time.Duration

	for _, window := range WindowLadder(maxWindowSec) {
		used = window
		start, end := hitTime.Add(-window), hitTime
		if direction == "forward" {
			start, end = hitTime, hitTime.Add(window)
		}

		// One slot over what is needed, in both directions: the matched line
		// occupies one of them whenever the query happens to return it, and
		// which query that is can flip (see dropAdjacentHit). Asking for the
		// extra costs nothing and stops a returned hit from making an otherwise
		// sufficient window look one line short.
		result, err := queryWithSlot(ctx, client, selector, start, end, want+1, direction)
		if err != nil {
			log.Printf("[LogContext] Rule %d: %s context query failed: %v", rule.ID, direction, err)
			return out, used, true
		}

		out = out[:0]
		for _, h := range result.ToHits() {
			s, ok := h["message"].(string)
			if !ok || s == "" {
				continue
			}
			out = append(out, s)
		}
		// Loki returns "backward" results newest-first; the renderer wants every
		// line in reading order, so flip them back.
		if direction == "backward" {
			for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
				out[i], out[j] = out[j], out[i]
			}
		}

		out = dropAdjacentHit(out, hitLine, direction)

		if direction == "forward" {
			// The lines nearest the hit lead a forward result, so cut the tail.
			if len(out) >= want {
				return out[:want], used, false
			}
		} else {
			// After the flip above, a backward result's nearest lines sit at its
			// TAIL — cutting the head is what keeps them.
			if len(out) >= want {
				return out[len(out)-want:], used, false
			}
		}
	}
	return out, used, false
}

// clampContextLines keeps a rule's per-direction line count inside what a Loki
// query will actually accept (see MaxLogContextLines).
func clampContextLines(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	if v > MaxLogContextLines {
		return MaxLogContextLines
	}
	return v
}

// queryWithSlot runs one context query while holding a global concurrency slot.
//
// The slot is released with defer, inside its own function, so a panic anywhere
// in the client cannot leak it. A leaked slot is permanent — there are only
// twenty, and the engine has no way to reclaim one — so a panic that would
// otherwise be contained by safego would instead throttle every rule on the
// platform, a little more with each occurrence.
func queryWithSlot(ctx context.Context, client *lokiclient.Client, selector string,
	start, end time.Time, limit int, direction string) (*lokiclient.QueryResult, error) {

	if !acquireGlobalCtx(ctx) {
		// acquireGlobalCtx blocks on a saturated semaphore and returns false
		// ONLY once ctx is done, so this is always the rule's 60s run budget
		// expiring — never mere contention. It matters which: a deadline leaves
		// the context genuinely unknown, and reporting that as "the stream had
		// no more logs" is the false negative this feature exists to avoid.
		return nil, fmt.Errorf("run deadline reached before the context query could start: %w", ctx.Err())
	}
	defer releaseGlobal()
	return client.QueryRangeDirection(ctx, selector, start, end, limit, direction)
}

// dropAdjacentHit removes the matched line from a context slice when the query
// returned it alongside the neighbours.
//
// Whether it comes back at all depends on Loki's endpoint semantics — start is
// inclusive, end exclusive, so a forward query anchored at the hit normally
// returns it and a backward one normally does not. Relying on that alone is not
// safe: the anchor instant can be a few nanoseconds off when a hit has been
// through a JSON round-trip (the preview and test-send handlers do exactly
// that, and float64 cannot hold a nanosecond epoch exactly), which is enough to
// flip either case. So the decision is made on the line's own content instead,
// at the one position the hit could occupy — the end adjacent to it. A blind
// positional drop would, in the flipped case, delete the single most valuable
// neighbour: the line immediately before the error.
//
// Two identical consecutive lines will cost one of them. That is the accepted
// trade: showing the matched line twice, once as a neighbour and once as the
// marked hit, misleads more than dropping a repeat.
func dropAdjacentHit(lines []string, hitLine, direction string) []string {
	if len(lines) == 0 || hitLine == "" {
		return lines
	}
	if direction == "forward" {
		if lines[0] == hitLine {
			return lines[1:]
		}
		return lines
	}
	if lines[len(lines)-1] == hitLine {
		return lines[:len(lines)-1]
	}
	return lines
}

// fetchLogContext / fetchStackContext keep the Engine's own call sites
// unchanged; both just supply the Engine's cached Loki client pool.
func (e *Engine) fetchLogContext(ctx context.Context, rule *models.AlertRule, hit map[string]interface{}) string {
	return FetchLogContext(ctx, rule, hit, e.getLokiClient)
}

func (e *Engine) fetchStackContext(ctx context.Context, rule *models.AlertRule, hit map[string]interface{}) string {
	return FetchStackContext(ctx, rule, hit, e.getLokiClient)
}

// trimStack applies the rule's head/tail elision and joins the result.
//
// The len(lines) > maxLines cap below is the same collection-time safety valve
// as CollectStack's, applied a second time: it exists here because the
// already-merged path in fetchStackContext hands trimStack a stack that never
// went through CollectStack at all (the collector merged it into one record
// upstream), so this is the only place that guard runs for that path. It is
// NOT a presentation setting and must stay generous — ordering matters:
//  1. the cap runs FIRST, as a runaway guard, against the WHOLE collected
//     stack (never a front-truncated prefix of it);
//  2. ElideMiddle runs SECOND, as the presentation step, and must see
//     whatever the cap left standing, including its tail, so the root cause
//     ("Caused by:") that lives at the bottom of a Java stack survives.
//
// A tight cap here would truncate from the front and discard exactly the
// tail ElideMiddle is supposed to keep — which is the bug this comment exists
// to prevent from coming back.
func trimStack(rule *models.AlertRule, lines []string) string {
	head := rule.StackHeadLines
	if head <= 0 {
		head = 12
	}
	tail := rule.StackTailLines
	if tail <= 0 {
		tail = 8
	}
	maxLines := rule.StackMaxLines
	if maxLines <= 0 {
		maxLines = 200
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(ElideMiddle(lines, head, tail), "\n")
}

// renderTemplate renders a Go template with variables
func renderTemplate(tmplStr string, vars map[string]interface{}) string {
	if tmplStr == "" {
		return RenderVarsDefault(vars)
	}

	tmpl, err := template.New("alert").Parse(tmplStr)
	if err != nil {
		log.Printf("[Template] Parse error: %v", err)
		return tmplStr
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		log.Printf("[Template] Execute error: %v", err)
		return tmplStr
	}

	return buf.String()
}

// buildDedupKey generates a dedup key from specified fields
func buildDedupKey(ruleID int, vars map[string]interface{}, dedupFields string) string {
	fields := strings.Split(dedupFields, ",")
	parts := []string{fmt.Sprintf("alert:dedup:%d", ruleID)}

	for _, f := range fields {
		f = strings.TrimSpace(f)
		if v, ok := vars[f]; ok {
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}

	key := strings.Join(parts, ":")
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("alert:dedup:%x", h[:16])
}

// DB helper functions

func getRuleByID(id int) (*models.AlertRule, error) {
	var rule models.AlertRule
	err := database.DB.QueryRow(`SELECT id, name, COALESCE(data_source_type,'es'),
		es_connection_id, COALESCE(loki_connection_id,0), lark_config_id, es_index,
		schedule, time_range, COALESCE(query_dsl,''), keyword, COALESCE(logql,''),
		COALESCE(filter_fields,''), COALESCE(extract_fields,''),
		message_title, COALESCE(message_template,''),
		COALESCE(at_users,''), at_all, COALESCE(alert_mode,'found'),
		recovery_enabled, COALESCE(recovery_title,''), COALESCE(recovery_template,''),
		severity, COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(query_concurrency,5), COALESCE(alert_interval,''), dedup_field, dedup_ttl, max_alerts,
		COALESCE(prometheus_config,''), COALESCE(route_config,''), COALESCE(namespaces,''), COALESCE(namespace_concurrency,3), COALESCE(label_filters,''),
		COALESCE(realtime_enabled,0), COALESCE(threshold_ms,0), COALESCE(report_enabled,0), COALESCE(report_schedule,''), COALESCE(report_mode,'separate'), COALESCE(report_title,''), COALESCE(report_template,''),
		COALESCE(stack_context_enabled,0), COALESCE(stack_max_lines,200), COALESCE(stack_head_lines,12), COALESCE(stack_tail_lines,8), COALESCE(stack_boundary_pattern,''), COALESCE(stack_window_sec,5),
		COALESCE(log_context_enabled,0), COALESCE(log_context_before,25), COALESCE(log_context_after,50), COALESCE(log_context_max_window_sec,1800), COALESCE(log_context_display_lines,30),
		status
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.DataSourceType,
		&rule.ESConnectionID, &rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
		&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
		&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
		&rule.PrometheusConfig, &rule.RouteConfig, &rule.Namespaces, &rule.NamespaceConcurrency, &rule.LabelFilters,
		&rule.RealtimeEnabled, &rule.ThresholdMs, &rule.ReportEnabled, &rule.ReportSchedule, &rule.ReportMode, &rule.ReportTitle, &rule.ReportTemplate,
		&rule.StackContextEnabled, &rule.StackMaxLines, &rule.StackHeadLines, &rule.StackTailLines, &rule.StackBoundaryPattern, &rule.StackWindowSec,
		&rule.LogContextEnabled, &rule.LogContextBefore, &rule.LogContextAfter, &rule.LogContextMaxWindowSec, &rule.LogContextDisplayLines,
		&rule.Status)
	return &rule, err
}

// getChannelByID loads one enabled notification channel.
func getChannelByID(id int) (*models.NotifyChannel, error) {
	var cfg models.NotifyChannel
	err := database.DB.QueryRow(`SELECT id, channel_type, name, webhook_url, secret, lark_type,
		bot_token, chat_id, thread_id, proxy_url, description, status
		FROM notify_channels WHERE id = ? AND status = 1`, id).Scan(
		&cfg.ID, &cfg.ChannelType, &cfg.Name, &cfg.WebhookURL, &cfg.Secret, &cfg.LarkType,
		&cfg.BotToken, &cfg.ChatID, &cfg.ThreadID, &cfg.ProxyURL, &cfg.Description, &cfg.Status)
	return &cfg, err
}

// getChannelsForRule returns every enabled channel bound to a rule. It falls
// back to the legacy alert_rules.lark_config_id only when the rule has no
// bound rows at all (a rule created before the multi-channel migration), so
// that an operator who bound channels and then disabled all of them gets a
// loud error instead of a silent resurrection of the old lark_config_id.
func getChannelsForRule(ruleID, fallbackChannelID int) ([]models.NotifyChannel, error) {
	rows, err := database.DB.Query(`SELECT c.id, c.channel_type, c.name, c.webhook_url, c.secret,
		c.lark_type, c.bot_token, c.chat_id, c.thread_id, c.proxy_url, c.description, c.status
		FROM alert_rule_channels rc
		JOIN notify_channels c ON c.id = rc.channel_id
		WHERE rc.rule_id = ?
		ORDER BY c.id`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bound int
	var list []models.NotifyChannel
	for rows.Next() {
		var c models.NotifyChannel
		if err := rows.Scan(&c.ID, &c.ChannelType, &c.Name, &c.WebhookURL, &c.Secret,
			&c.LarkType, &c.BotToken, &c.ChatID, &c.ThreadID, &c.ProxyURL, &c.Description, &c.Status); err != nil {
			return nil, err
		}
		bound++
		if c.Status == 1 {
			list = append(list, c)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if bound == 0 && fallbackChannelID > 0 {
		c, err := getChannelByID(fallbackChannelID)
		if err != nil {
			return nil, fmt.Errorf("rule %d: fallback channel %d not found or disabled: %w", ruleID, fallbackChannelID, err)
		}
		list = append(list, *c)
	}
	if len(list) == 0 {
		if bound > 0 {
			return nil, fmt.Errorf("rule %d: all %d bound notification channels are disabled", ruleID, bound)
		}
		return nil, fmt.Errorf("rule %d has no enabled notification channel", ruleID)
	}
	return list, nil
}

// queryES queries Elasticsearch and returns hits
func (e *Engine) queryES(ctx context.Context, rule *models.AlertRule) ([]map[string]interface{}, int64, error) {
	client, err := e.getESClient(rule.ESConnectionID)
	if err != nil {
		return nil, 0, fmt.Errorf("ES client error: %w", err)
	}

	maxAlerts := rule.MaxAlerts
	if maxAlerts <= 0 {
		maxAlerts = 10
	}
	if rule.GroupBy != "" && maxAlerts < 500 {
		maxAlerts = 500
	}
	query, err := es.BuildQuery(rule.Keyword, rule.FilterFields, rule.TimeRange, rule.QueryDSL, maxAlerts)
	if err != nil {
		return nil, 0, fmt.Errorf("query build error: %w", err)
	}

	result, err := client.Search(ctx, rule.ESIndex, query)
	if err != nil {
		return nil, 0, fmt.Errorf("ES search error: %w", err)
	}
	return result.Hits, result.Total, nil
}

// queryLoki queries Loki and returns hits in ES-compatible format
func (e *Engine) queryLoki(ctx context.Context, rule *models.AlertRule) ([]map[string]interface{}, int64, error) {
	if rule.LokiConnectionID == 0 {
		return nil, 0, fmt.Errorf("Loki connection not configured")
	}
	if rule.LogQL == "" {
		return nil, 0, fmt.Errorf("LogQL query is empty")
	}

	// Get Loki connection
	client, err := e.getLokiClient(rule.LokiConnectionID)
	if err != nil {
		return nil, 0, err
	}

	// Parse time range
	now := time.Now()
	duration := 5 * time.Minute
	if rule.TimeRange != "" {
		if strings.HasSuffix(rule.TimeRange, "d") {
			days := 1
			fmt.Sscanf(rule.TimeRange, "%dd", &days)
			duration = time.Duration(days) * 24 * time.Hour
		} else if d, err := time.ParseDuration(rule.TimeRange); err == nil {
			duration = d
		}
	}
	start := now.Add(-duration)

	maxAlerts := rule.MaxAlerts
	if maxAlerts <= 0 {
		maxAlerts = 10
	}
	// Grouped mode needs more hits to cover all containers
	if rule.GroupBy != "" && maxAlerts < 500 {
		maxAlerts = 500
	}

	result, err := client.QueryRange(ctx, rule.LogQL, start, now, maxAlerts)
	if err != nil {
		return nil, 0, fmt.Errorf("Loki query error: %w", err)
	}

	hits := result.ToHits()
	return hits, int64(result.Total), nil
}

// queryLokiWider queries Loki with a wider time range (for not_found mode)
func (e *Engine) queryLokiWider(ctx context.Context, rule *models.AlertRule, timeRange string, limit int) ([]map[string]interface{}, error) {
	client, err := e.getLokiClient(rule.LokiConnectionID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	start := now.Add(-24 * time.Hour)
	if timeRange != "" {
		if d, err := time.ParseDuration(timeRange); err == nil {
			start = now.Add(-d)
		}
	}

	result, err := client.QueryRange(ctx, rule.LogQL, start, now, limit)
	if err != nil {
		return nil, err
	}
	return result.ToHits(), nil
}

func (e *Engine) getLokiClient(connID int) (*lokiclient.Client, error) {
	e.mu.RLock()
	client, ok := e.lokiClients[connID]
	e.mu.RUnlock()

	if ok {
		return client, nil
	}

	var conn models.LokiConnection
	err := database.DB.QueryRow(`SELECT id, name, url, username, password, org_id, skip_tls_verify
		FROM loki_connections WHERE id = ? AND status = 1`, connID).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Username, &conn.Password, &conn.OrgID, &conn.SkipTLSVerify)
	if err != nil {
		return nil, fmt.Errorf("Loki connection %d not found: %w", connID, err)
	}

	client = lokiclient.NewClient(conn)

	e.mu.Lock()
	e.lokiClients[connID] = client
	e.mu.Unlock()

	return client, nil
}

// RefreshLokiClient refreshes or removes a cached Loki client
func (e *Engine) RefreshLokiClient(connID int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.lokiClients, connID)
}

func (e *Engine) getESClient(connID int) (*es.Client, error) {
	e.mu.RLock()
	client, ok := e.clients[connID]
	e.mu.RUnlock()

	if ok {
		return client, nil
	}

	// Fetch connection config from DB
	var conn models.ESConnection
	err := database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key, skip_tls_verify
		FROM es_connections WHERE id = ? AND status = 1`, connID).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey, &conn.SkipTLSVerify)
	if err != nil {
		return nil, fmt.Errorf("ES connection %d not found: %w", connID, err)
	}

	client, err = es.NewClient(conn)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	e.clients[connID] = client
	e.mu.Unlock()

	return client, nil
}

// partialSendNote returns a short summary naming the channels that did not
// deliver, or "" when the fan-out was a clean success (or not a fan-out at all).
//
// MultiSender reports a partial success as a nil error on purpose — the alert
// did reach someone — but that made the engine clear last_error and count a
// success, so a Telegram channel failing on every single send was visible
// nowhere except alert_logs.status. Callers use this to record the failure
// alongside the success instead.
func partialSendNote(resp string) string {
	if notify.StatusFromResponse(resp) != "partial" {
		return ""
	}
	failed := notify.FailedChannels(resp)
	if len(failed) == 0 {
		return "部分渠道发送失败"
	}
	return "部分渠道发送失败: " + strings.Join(failed, ", ")
}

// recordPartialSend applies a partial fan-out's consequences: the rule keeps a
// visible last_error naming the broken channels, and Prometheus sees a send
// failure in addition to the success the healthy channels earned.
func recordPartialSend(rule *models.AlertRule, ruleIDStr, note string) {
	database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", note, rule.ID)
	if Metrics != nil {
		Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
	}
	log.Printf("[Engine] Rule %d: %s", rule.ID, note)
}

func saveAlertLog(rule *models.AlertRule, message, esRaw, status, errMsg, resp string) {
	// A fan-out payload knows better than the caller whether this was a full
	// success, a partial one, or a total failure.
	if derived := notify.StatusFromResponse(resp); derived != "" {
		status = derived
	}
	_, err := database.DB.Exec(`INSERT INTO alert_logs (rule_id, rule_name, severity, message, es_raw, status, error_msg, lark_response)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.Name, rule.Severity, message, esRaw, status, errMsg, resp)
	if err != nil {
		log.Printf("[Engine] Failed to save alert log: %v", err)
	}
}

// executeGroupedRule processes hits grouped by a field, each group handled independently
func (e *Engine) executeGroupedRule(ctx context.Context, rule *models.AlertRule,
	result *es.SearchResult, sender notify.Notifier, atUsers []models.AtUser, atAll bool,
	ruleIDStr, alertMode, dataSourceType string) {

	groupField := strings.TrimSpace(rule.GroupBy)
	log.Printf("[Engine] Rule %d: grouping by '%s'", rule.ID, groupField)

	// Group hits by field value
	groups := make(map[string][]map[string]interface{})
	for _, hit := range result.Hits {
		val := es.GetNestedField(hit, groupField)
		key := "(unknown)"
		if val != nil {
			key = fmt.Sprintf("%v", val)
		}
		groups[key] = append(groups[key], hit)
	}

	log.Printf("[Engine] Rule %d: %d groups found", rule.ID, len(groups))

	if alertMode == "not_found" {
		// When expected_groups is set, skip batch grouping — each container will be checked independently
		if rule.ExpectedGroups != "" {
			groups = make(map[string][]map[string]interface{})
		}
		e.executeGroupedNotFound(ctx, rule, sender, atUsers, atAll, ruleIDStr, groups, groupField, dataSourceType)
		return
	}

	// ========== found mode: alert for each group ==========
	sentCount := 0
	// Contexts fetched so far in THIS run; see MaxContextFetchesPerRun.
	groupCtxFetches := 0
	partialNote := "" // last partial fan-out seen; keeps last_error from being cleared
	for groupKey, hits := range groups {
		// Use first hit for rendering
		firstHit := hits[0]
		vars := extractFields(firstHit, rule.ExtractFields)
		vars["_group_key"] = groupKey
		vars["_group_field"] = groupField
		vars["_group_count"] = len(hits)

		// Dedup per group
		if rule.DedupField != "" {
			dedupKey := buildDedupKey(rule.ID, vars, rule.DedupField)
			ttl := time.Duration(rule.DedupTTL) * time.Second
			if ttl <= 0 {
				ttl = time.Hour
			}
			isDup, _ := database.CheckDedup(ctx, dedupKey, ttl)
			if isDup {
				log.Printf("[Engine] Rule %d: group '%s' duplicate skipped", rule.ID, groupKey)
				continue
			}
		}

		if rule.StackContextEnabled == 1 {
			vars["stack"] = e.fetchStackContext(ctx, rule, firstHit)
		}
		if rule.LogContextEnabled == 1 {
			if groupCtxFetches < MaxContextFetchesPerRun {
				vars["logcontext"] = e.fetchLogContext(ctx, rule, firstHit)
				groupCtxFetches++
			} else {
				vars["logcontext"] = RenderContextSkipped(firstHit, MaxContextFetchesPerRun)
			}
		}

		message := renderTemplate(rule.MessageTemplate, vars)

		// Zero-config path: the operator turned the switch on but never referenced
		// the variable, so put the stack where it can still be read.
		if rule.StackContextEnabled == 1 && !strings.Contains(rule.MessageTemplate, "{{.stack}}") {
			if s, ok := vars["stack"].(string); ok && s != "" {
				message += "\n```\n" + s + "\n```"
			}
		}
		// Same zero-config path for the log context, under its own switch: the
		// two features are independent, and a template may reference one
		// variable while leaving the other to be appended.
		if rule.LogContextEnabled == 1 && !strings.Contains(rule.MessageTemplate, "{{.logcontext}}") {
			if s, ok := vars["logcontext"].(string); ok && s != "" {
				message += "\n" + LogContextCaption + "\n```\n" + s + "\n```"
			}
		}

		rawJSON, _ := json.Marshal(firstHit)

		title := rule.MessageTitle
		if title != "" {
			title = fmt.Sprintf("%s [%s]", rule.MessageTitle, groupKey)
		}

		resp, err := sender.SendCard(title, message, rule.Severity, atUsers, atAll)
		if err != nil {
			saveAlertLog(rule, message, string(rawJSON), "failed", fmt.Sprintf("[%s] %v", groupKey, err), resp)
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
			}
			continue
		}

		note := partialSendNote(resp)
		saveAlertLog(rule, message, string(rawJSON), "success", note, resp)
		sentCount++
		if Metrics != nil {
			Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
			Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
		}
		if note != "" {
			partialNote = note
			recordPartialSend(rule, ruleIDStr, note)
		}
		log.Printf("[Engine] Rule %d: group '%s' alert sent, resp=%s", rule.ID, groupKey, resp)
	}

	// A partial fan-out must survive here: clearing last_error would hide a
	// channel that failed on every send behind the channels that succeeded.
	if sentCount > 0 && partialNote == "" {
		database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
	}
}

// executeGroupedNotFound handles not_found mode with grouping
// It discovers known groups from a wider time range, then checks which groups are missing in the current range
func (e *Engine) executeGroupedNotFound(ctx context.Context, rule *models.AlertRule,
	sender notify.Notifier, atUsers []models.AtUser, atAll bool, ruleIDStr string,
	currentGroups map[string][]map[string]interface{}, groupField, dataSourceType string) {

	// Step 1: Get target groups (from expected_groups or auto-discover from 3h)
	var targetGroups []string

	if rule.ExpectedGroups != "" {
		// Use manually specified groups
		json.Unmarshal([]byte(rule.ExpectedGroups), &targetGroups)
		log.Printf("[Engine] Rule %d: using %d expected groups", rule.ID, len(targetGroups))
	} else {
		// Auto-discover from 3h
		targetGroups = e.discoverGroups(ctx, rule, groupField, dataSourceType)
		log.Printf("[Engine] Rule %d: discovered %d groups from 3h", rule.ID, len(targetGroups))
	}

	if len(targetGroups) == 0 {
		log.Printf("[Engine] Rule %d: no target groups found", rule.ID)
		return
	}

	// Step 2: Concurrently check each group (limit=1 per group)
	type checkResult struct {
		GroupKey string
		HasHits  bool
		FirstHit map[string]interface{}
	}

	resultCh := make(chan checkResult, len(targetGroups))
	concurrency := rule.QueryConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	semaphore := make(chan struct{}, concurrency) // per-rule concurrency

	for _, groupKey := range targetGroups {
		semaphore <- struct{}{} // acquire per-rule slot
		go func(gk string) {
			// Started inside a cron job, so cron.Recover does not reach it.
			defer safego.Recover("group worker")
			defer func() { <-semaphore }() // release per-rule slot

			// Check if already in currentGroups (from the initial batch query)
			if hits, ok := currentGroups[gk]; ok && len(hits) > 0 {
				// Cache the last hit in Redis
				cacheLastHit(rule.ID, gk, hits[0])
				resultCh <- checkResult{GroupKey: gk, HasHits: true, FirstHit: hits[0]}
				return
			}

			// Acquire global semaphore before querying (with context timeout)
			if !acquireGlobalCtx(ctx) {
				log.Printf("[Engine] Rule %d: global semaphore timeout for group '%s'", rule.ID, gk)
				resultCh <- checkResult{GroupKey: gk, HasHits: false}
				return
			}
			hasHits, firstHit := e.checkGroupHit(ctx, rule, groupField, gk, dataSourceType)
			releaseGlobal()

			if hasHits && firstHit != nil {
				cacheLastHit(rule.ID, gk, firstHit)
			}
			resultCh <- checkResult{GroupKey: gk, HasHits: hasHits, FirstHit: firstHit}
		}(groupKey)
	}

	// Collect results
	results := make(map[string]checkResult)
	for i := 0; i < len(targetGroups); i++ {
		r := <-resultCh
		results[r.GroupKey] = r
	}

	log.Printf("[Engine] Rule %d: checked %d groups", rule.ID, len(results))

	// Parse alert interval
	alertInterval := time.Duration(0) // 0 means alert every time
	if rule.AlertInterval != "" {
		if strings.HasSuffix(rule.AlertInterval, "d") {
			days := 1
			fmt.Sscanf(rule.AlertInterval, "%dd", &days)
			alertInterval = time.Duration(days) * 24 * time.Hour
		} else if d, err := time.ParseDuration(rule.AlertInterval); err == nil {
			alertInterval = d
		}
	}

	// Parse prometheus config for container metrics
	promCfg := ParsePrometheusConfig(rule.PrometheusConfig)

	partialNote := "" // last partial fan-out seen; keeps last_error from being cleared

	// Extract namespace from LogQL or first available hit
	namespace := ""
	for _, hits := range currentGroups {
		if len(hits) > 0 {
			if ns, ok := hits[0]["namespace"]; ok {
				namespace = fmt.Sprintf("%v", ns)
				break
			}
		}
	}

	// Step 3: Process each group
	for _, groupKey := range targetGroups {
		r := results[groupKey]
		stateKey := fmt.Sprintf("alert:state:%d:%s", rule.ID, groupKey)
		lastAlertKey := fmt.Sprintf("alert:last_alert_time:%d:%s", rule.ID, groupKey)

		// Record container-level Prometheus metrics first
		if Metrics != nil {
			// Merge labels: project labels + static labels
			mergedLabels := map[string]string{}
			nfProjectLabels := getProjectLabels(rule.ProjectID)
			for k, v := range nfProjectLabels {
				mergedLabels[k] = v
			}
			if promCfg != nil {
				for k, v := range promCfg.GetStaticLabels() {
					mergedLabels[k] = v
				}
			}
			muted := isMuted(rule.ID, groupKey)
			var metricStatus float64
			if muted {
				metricStatus = -1
			} else if r.HasHits {
				metricStatus = 1
			} else {
				metricStatus = 0
			}
			Metrics.RecordContainerStatus(ruleIDStr, rule.Name, namespace, groupKey, "not_found", metricStatus, mergedLabels)
		}

		if !r.HasHits {
			// Check if muted
			if isMuted(rule.ID, groupKey) {
				log.Printf("[Engine] Rule %d: group '%s' is muted, skipping", rule.ID, groupKey)
				continue
			}

			// alert_interval=once: 该分组已在告警中，恢复前不再重复发
			if rule.AlertInterval == AlertIntervalOnce {
				curState, _ := database.RDB.Get(ctx, stateKey).Result()
				if curState == "alerting" {
					// 续期状态 key，避免长时间未恢复时 TTL 过期被误判为新故障
					database.RDB.Expire(ctx, stateKey, alertStateTTL)
					log.Printf("[Engine] Rule %d: group '%s' once mode, already alerting, skip until recovery", rule.ID, groupKey)
					continue
				}
			}

			// Check alert interval (should we send again?)
			if alertInterval > 0 {
				lastAlertStr, _ := database.RDB.Get(ctx, lastAlertKey).Result()
				if lastAlertStr != "" {
					if lastAlert, err := time.Parse(time.RFC3339, lastAlertStr); err == nil {
						if time.Since(lastAlert) < alertInterval {
							log.Printf("[Engine] Rule %d: group '%s' alert interval not reached, skipping", rule.ID, groupKey)
							continue
						}
					}
				}
			}

			log.Printf("[Engine] Rule %d: group '%s' not found in %s, triggering alert", rule.ID, groupKey, rule.TimeRange)
			database.RDB.Set(ctx, stateKey, "alerting", 7*24*time.Hour)
			database.RDB.Set(ctx, lastAlertKey, time.Now().Format(time.RFC3339), 7*24*time.Hour)

			// Try to get last cached hit from Redis (avoid wider query)
			vars := map[string]interface{}{
				"alert_reason": "not_found",
				"time_range":   rule.TimeRange,
				"_group_key":   groupKey,
				"_group_field": groupField,
				groupField:     groupKey,
				"container":    groupKey,
				"namespace":    "",
			}
			lastHit := getLastHit(rule.ID, groupKey)
			if lastHit == nil {
				// No cache, try wider query (uses rule.TimeRange)
				if !acquireGlobalCtx(ctx) {
					log.Printf("[Engine] Rule %d: global semaphore timeout for last hit query '%s'", rule.ID, groupKey)
				} else {
					_, lastHit = e.checkGroupHit(ctx, rule, groupField, groupKey, dataSourceType)
					releaseGlobal()
					if lastHit != nil {
						cacheLastHit(rule.ID, groupKey, lastHit)
					}
				}
			}
			if lastHit != nil {
				vars = extractFields(lastHit, rule.ExtractFields)
				vars["alert_reason"] = "not_found"
				vars["time_range"] = rule.TimeRange
				vars["_group_key"] = groupKey
				vars["_group_field"] = groupField
				vars["container"] = groupKey

				// If any extracted field is empty, query more hits to fill
				if hasEmptyExtractFields(vars, rule.ExtractFields) {
					if !acquireGlobalCtx(ctx) {
						log.Printf("[Engine] Rule %d: global semaphore timeout for fill query '%s'", rule.ID, groupKey)
					} else {
						moreHits := e.checkGroupHits(ctx, rule, groupField, groupKey, dataSourceType, 10)
						releaseGlobal()
						fillEmptyFields(vars, moreHits, rule.ExtractFields)
					}
				}

				log.Printf("[Engine] Rule %d: using last hit for group '%s'", rule.ID, groupKey)
			}
			message := renderTemplate(rule.MessageTemplate, vars)
			// Render title template too
			titleTemplate := rule.MessageTitle
			titleRendered := renderTemplate(titleTemplate, vars)
			title := fmt.Sprintf("%s [%s]", titleRendered, groupKey)

			// Rate limit: 200ms between sends to avoid Lark frequency limiting
			time.Sleep(200 * time.Millisecond)
			resp, sErr := sender.SendCard(title, message, rule.Severity, atUsers, atAll)
			if sErr != nil {
				saveAlertLog(rule, message, "", "failed", fmt.Sprintf("[%s] %v", groupKey, sErr), resp)
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
				}
			} else {
				note := partialSendNote(resp)
				saveAlertLog(rule, message, "", "success", note, resp)
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
				}
				if note != "" {
					partialNote = note
					recordPartialSend(rule, ruleIDStr, note)
				}
				log.Printf("[Engine] Rule %d: group '%s' not_found alert sent, resp=%s", rule.ID, groupKey, resp)
			}
		} else {
			// Has hits → should be normal, check if was alerting before
			curState, _ := database.RDB.Get(ctx, stateKey).Result()
			if curState == "alerting" && rule.RecoveryEnabled == 1 {
				log.Printf("[Engine] Rule %d: group '%s' recovered", rule.ID, groupKey)
				database.RDB.Set(ctx, stateKey, "normal", 7*24*time.Hour)
				database.RDB.Del(ctx, lastAlertKey) // clear last alert time

				// Use the earliest hit for recovery (last element since results are desc sorted)
				recoveryHit := r.FirstHit
				if hits, ok := currentGroups[groupKey]; ok && len(hits) > 0 {
					recoveryHit = hits[len(hits)-1]
				}
				vars := extractFields(recoveryHit, rule.ExtractFields)
				vars["alert_reason"] = "recovered"
				vars["_group_key"] = groupKey
				vars["_group_field"] = groupField
				rawJSON, _ := json.Marshal(recoveryHit)

				title := rule.RecoveryTitle
				if title == "" {
					title = rule.MessageTitle + " - 已恢复"
				}
				title = fmt.Sprintf("%s [%s]", title, groupKey)
				tmpl := rule.RecoveryTemplate
				if tmpl == "" {
					tmpl = rule.MessageTemplate
				}
				message := renderTemplate(tmpl, vars)

				time.Sleep(200 * time.Millisecond)
				resp, sErr := sender.SendCard(title, message, "recovery", atUsers, atAll)
				if sErr != nil {
					saveAlertLog(rule, message, string(rawJSON), "failed", fmt.Sprintf("[%s] recovery: %v", groupKey, sErr), resp)
				} else {
					note := partialSendNote(resp)
					saveAlertLog(rule, message, string(rawJSON), "success", note, resp)
					if note != "" {
						partialNote = note
						recordPartialSend(rule, ruleIDStr, note)
					}
					log.Printf("[Engine] Rule %d: group '%s' recovery sent, resp=%s", rule.ID, groupKey, resp)
				}
			} else if curState == "alerting" {
				database.RDB.Set(ctx, stateKey, "normal", 7*24*time.Hour)
				database.RDB.Del(ctx, lastAlertKey)
			}
		}

	}

	// A partial fan-out must survive here: clearing last_error would hide a
	// channel that failed on every send behind the channels that succeeded.
	if partialNote == "" {
		database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
	}

	// Write alerting count to Redis for rule list display
	alertingCount := 0
	for _, gk := range targetGroups {
		r := results[gk]
		if !r.HasHits && !isMuted(rule.ID, gk) {
			alertingCount++
		}
	}
	database.RDB.Set(ctx, fmt.Sprintf("alert:alerting_count:%d", rule.ID), alertingCount, 10*time.Minute)
}

// discoverGroups auto-discovers groups from 3h data (reduced from 24h to avoid Loki OOM)
func (e *Engine) discoverGroups(ctx context.Context, rule *models.AlertRule, groupField, dataSourceType string) []string {
	if dataSourceType == "loki" {
		widerHits, err := e.queryLokiWider(ctx, rule, "3h", 1000)
		if err != nil {
			log.Printf("[Engine] Rule %d: discover groups error: %v", rule.ID, err)
			return nil
		}
		seen := map[string]bool{}
		var groups []string
		for _, hit := range widerHits {
			val := es.GetNestedField(hit, groupField)
			if val != nil {
				key := fmt.Sprintf("%v", val)
				if !seen[key] {
					seen[key] = true
					groups = append(groups, key)
				}
			}
		}
		return groups
	}

	// ES: use terms aggregation
	widerQuery, _ := es.BuildQuery(rule.Keyword, rule.FilterFields, "3h", "", 0)
	widerQuery["size"] = 0
	widerQuery["aggs"] = map[string]interface{}{
		"groups": map[string]interface{}{
			"terms": map[string]interface{}{
				"field": groupField + ".keyword",
				"size":  200,
			},
		},
	}
	esClient, err := e.getESClient(rule.ESConnectionID)
	if err != nil {
		return nil
	}
	widerResult, err := esClient.SearchRaw(ctx, rule.ESIndex, widerQuery)
	if err != nil {
		widerQuery["aggs"] = map[string]interface{}{
			"groups": map[string]interface{}{
				"terms": map[string]interface{}{
					"field": groupField,
					"size":  200,
				},
			},
		}
		widerResult, err = esClient.SearchRaw(ctx, rule.ESIndex, widerQuery)
		if err != nil {
			return nil
		}
	}
	return parseAggBuckets(widerResult, "groups")
}

// checkGroupHit checks if a specific group has any hits in the current time range
func (e *Engine) checkGroupHit(ctx context.Context, rule *models.AlertRule, groupField, groupKey, dataSourceType string) (bool, map[string]interface{}) {
	if dataSourceType == "loki" {
		// Build LogQL: inject group filter into original label selectors
		logql := rule.LogQL
		// Sanitize groupKey: escape quotes
		safeGroupKey := strings.ReplaceAll(groupKey, `"`, `\"`)

		var specificLogQL string
		if idx := strings.Index(logql, "}"); idx >= 0 {
			// Replace group field with exact match (remove original regex/wildcard)
			existingLabels := strings.TrimSpace(logql[1:idx])
			pipeline := strings.TrimSpace(logql[idx+1:])
			var parts []string
			for _, part := range strings.Split(existingLabels, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				fieldName := strings.Split(part, "=")[0]
				fieldName = strings.Split(fieldName, "!")[0]
				fieldName = strings.Split(fieldName, "~")[0]
				fieldName = strings.TrimSpace(fieldName)
				if fieldName == groupField {
					continue
				}
				parts = append(parts, part)
			}
			parts = append(parts, fmt.Sprintf(`%s="%s"`, groupField, safeGroupKey))
			specificLogQL = "{" + strings.Join(parts, ", ") + "}"
			if pipeline != "" {
				specificLogQL += " " + pipeline
			}
		} else {
			specificLogQL = fmt.Sprintf(`{%s="%s"}`, groupField, safeGroupKey)
		}

		client, cErr := e.getLokiClient(rule.LokiConnectionID)
		if cErr != nil {
			log.Printf("[Engine] Rule %d: getLokiClient error: %v", rule.ID, cErr)
			return false, nil
		}

		now := time.Now()
		duration := 5 * time.Minute
		if rule.TimeRange != "" {
			if strings.HasSuffix(rule.TimeRange, "d") {
				days := 1
				fmt.Sscanf(rule.TimeRange, "%dd", &days)
				duration = time.Duration(days) * 24 * time.Hour
			} else if d, err := time.ParseDuration(rule.TimeRange); err == nil {
				duration = d
			}
		}

		result, err := client.QueryRange(ctx, specificLogQL, now.Add(-duration), now, 1)
		if err != nil {
			log.Printf("[Engine] Rule %d: check group '%s' error: %v", rule.ID, groupKey, err)
			return false, nil
		}

		hits := result.ToHits()
		if len(hits) > 0 {
			return true, hits[0]
		}
		return false, nil
	}

	// ES: query with additional filter for the group
	filterJSON := rule.FilterFields
	var filters []models.FilterField
	if filterJSON != "" {
		json.Unmarshal([]byte(filterJSON), &filters)
	}
	filters = append(filters, models.FilterField{Field: groupField, Value: groupKey, Op: "term"})
	newFilterJSON, _ := json.Marshal(filters)

	query, _ := es.BuildQuery(rule.Keyword, string(newFilterJSON), rule.TimeRange, "", 1)
	esClient, err := e.getESClient(rule.ESConnectionID)
	if err != nil {
		return false, nil
	}
	result, err := esClient.Search(ctx, rule.ESIndex, query)
	if err != nil || len(result.Hits) == 0 {
		return false, nil
	}
	return true, result.Hits[0]
}

// checkGroupHits queries multiple hits for a specific group (for field value fallback)
func (e *Engine) checkGroupHits(ctx context.Context, rule *models.AlertRule, groupField, groupKey, dataSourceType string, limit int) []map[string]interface{} {
	if dataSourceType == "loki" {
		logql := rule.LogQL
		safeGroupKey := strings.ReplaceAll(groupKey, `"`, `\"`)
		var specificLogQL string
		if idx := strings.Index(logql, "}"); idx >= 0 {
			existingLabels := strings.TrimSpace(logql[1:idx])
			pipeline := strings.TrimSpace(logql[idx+1:])
			var parts []string
			for _, part := range strings.Split(existingLabels, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				fieldName := strings.Split(part, "=")[0]
				fieldName = strings.Split(fieldName, "!")[0]
				fieldName = strings.Split(fieldName, "~")[0]
				fieldName = strings.TrimSpace(fieldName)
				if fieldName == groupField {
					continue
				}
				parts = append(parts, part)
			}
			parts = append(parts, fmt.Sprintf(`%s="%s"`, groupField, safeGroupKey))
			specificLogQL = "{" + strings.Join(parts, ", ") + "}"
			if pipeline != "" {
				specificLogQL += " " + pipeline
			}
		} else {
			specificLogQL = fmt.Sprintf(`{%s="%s"}`, groupField, safeGroupKey)
		}

		client, cErr := e.getLokiClient(rule.LokiConnectionID)
		if cErr != nil {
			return nil
		}
		now := time.Now()
		duration := 5 * time.Minute
		if rule.TimeRange != "" {
			if strings.HasSuffix(rule.TimeRange, "d") {
				days := 1
				fmt.Sscanf(rule.TimeRange, "%dd", &days)
				duration = time.Duration(days) * 24 * time.Hour
			} else if d, err := time.ParseDuration(rule.TimeRange); err == nil {
				duration = d
			}
		}
		result, err := client.QueryRange(ctx, specificLogQL, now.Add(-duration), now, limit)
		if err != nil {
			return nil
		}
		return result.ToHits()
	}

	// ES
	filterJSON := rule.FilterFields
	var filters []models.FilterField
	if filterJSON != "" {
		json.Unmarshal([]byte(filterJSON), &filters)
	}
	filters = append(filters, models.FilterField{Field: groupField, Value: groupKey, Op: "term"})
	newFilterJSON, _ := json.Marshal(filters)
	query, _ := es.BuildQuery(rule.Keyword, string(newFilterJSON), rule.TimeRange, "", limit)
	esClient, err := e.getESClient(rule.ESConnectionID)
	if err != nil {
		return nil
	}
	result, err := esClient.Search(ctx, rule.ESIndex, query)
	if err != nil {
		return nil
	}
	return result.Hits
}

// parseAggBuckets extracts bucket keys from ES aggregation result
// cacheLastHit stores the last hit for a group in Redis (expires in 48h)
func cacheLastHit(ruleID int, groupKey string, hit map[string]interface{}) {
	key := fmt.Sprintf("alert:last_hit:%d:%s", ruleID, groupKey)
	data, err := json.Marshal(hit)
	if err != nil {
		return
	}
	database.RDB.Set(context.Background(), key, string(data), 48*time.Hour)
}

// getLastHit retrieves the cached last hit for a group from Redis
func getLastHit(ruleID int, groupKey string) map[string]interface{} {
	key := fmt.Sprintf("alert:last_hit:%d:%s", ruleID, groupKey)
	val, err := database.RDB.Get(context.Background(), key).Result()
	if err != nil || val == "" {
		return nil
	}
	var hit map[string]interface{}
	if err := json.Unmarshal([]byte(val), &hit); err != nil {
		return nil
	}
	return hit
}

// resolveAtUsers parses at_users JSON and resolves names to lark_ids from alert_contacts table
func resolveAtUsers(atUsersJSON string) []models.AtUser {
	if atUsersJSON == "" {
		return nil
	}

	// Try parsing as name array: ["Bruce","Cesar"]
	var names []string
	if err := json.Unmarshal([]byte(atUsersJSON), &names); err == nil && len(names) > 0 {
		var result []models.AtUser
		for _, name := range names {
			var larkID, telegramID string
			err := database.DB.QueryRow(
				"SELECT lark_id, COALESCE(telegram_id,'') FROM alert_contacts WHERE name = ? AND status = 1",
				name).Scan(&larkID, &telegramID)
			if err == nil && (larkID != "" || telegramID != "") {
				result = append(result, models.AtUser{Name: name, UserID: larkID, TelegramID: telegramID})
			} else {
				log.Printf("[Engine] Contact '%s' not found in alert_contacts", name)
			}
		}
		return result
	}

	// Fallback: old format [{"name":"Bruce","user_id":"ou_xxx"}]
	var users []models.AtUser
	json.Unmarshal([]byte(atUsersJSON), &users)
	return users
}

// getProjectLabels returns project hierarchy labels for a rule's project_id.
// Returns {"project": "G32", "sub_project": "G32 UAT"} for a child project,
// or {"project": "G32"} for a top-level project.
func getProjectLabels(projectID int) map[string]string {
	if projectID <= 0 {
		return nil
	}
	var name string
	var parentID int
	err := database.DB.QueryRow("SELECT name, parent_id FROM alert_projects WHERE id = ?", projectID).Scan(&name, &parentID)
	if err != nil {
		return nil
	}
	if parentID == 0 {
		// Top-level project
		return map[string]string{"project": name}
	}
	// Child project: get parent name
	var parentName string
	err = database.DB.QueryRow("SELECT name FROM alert_projects WHERE id = ?", parentID).Scan(&parentName)
	if err != nil {
		return map[string]string{"project": name}
	}
	return map[string]string{"project": parentName, "sub_project": name}
}

// isMuted checks if a group is currently muted for a rule
func isMuted(ruleID int, groupKey string) bool {
	// Debug: check without time condition first
	var totalCount int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_mutes WHERE rule_id = ? AND group_key = ?",
		ruleID, groupKey).Scan(&totalCount)

	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM alert_mutes WHERE rule_id = ? AND group_key = ? AND mute_until > NOW()",
		ruleID, groupKey).Scan(&count)
	if err != nil {
		log.Printf("[Mute] Error checking mute for rule=%d group='%s': %v", ruleID, groupKey, err)
	}

	var dbNow string
	database.DB.QueryRow("SELECT NOW()").Scan(&dbNow)

	log.Printf("[Mute] rule_id=%d group_key='%s' total_rows=%d active_rows=%d muted=%v db_now=%s", ruleID, groupKey, totalCount, count, count > 0, dbNow)
	return count > 0
}

func parseAggBuckets(raw []byte, aggName string) []string {
	var resp map[string]interface{}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	aggs, ok := resp["aggregations"].(map[string]interface{})
	if !ok {
		return nil
	}
	agg, ok := aggs[aggName].(map[string]interface{})
	if !ok {
		return nil
	}
	buckets, ok := agg["buckets"].([]interface{})
	if !ok {
		return nil
	}
	var keys []string
	for _, b := range buckets {
		bucket, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		if key, ok := bucket["key"].(string); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

// executeNamespacedRule loops through namespaces with concurrency control,
// queries Loki per namespace, groups hits by container, and sends aggregated alerts.
// ==================== Namespace 模式公共逻辑 ====================

// NamespacedContainerResult represents one container's aggregated result
type NamespacedContainerResult struct {
	Namespace   string                   `json:"namespace"`
	Container   string                   `json:"container"`
	Hits        []map[string]interface{} `json:"hits"`
	IgnoredHits []map[string]interface{} `json:"-"` // hits filtered by ignore list (for metrics only)
	HitCount    int                      `json:"hit_count"`
	Message     string                   `json:"message"` // 渲染好的样式1消息
}

// QueryNamespacedLoki is the shared function for all 4 paths (preview, test-send, manual run, cron).
// It queries Loki per namespace, groups by container, builds aggregated messages.
// Exported so handlers package can call it.
func QueryNamespacedLoki(ctx context.Context, lokiConnID int, namespaces []string, pipeline, timeRange, extractFieldsJSON, severity, messageTemplate, routeConfigJSON string, maxAlerts, concurrency int, labelFilters string, getLokiClient func(int) (*lokiclient.Client, error)) ([]NamespacedContainerResult, error) {
	if concurrency <= 0 {
		concurrency = 3
	}

	duration := 5 * time.Minute
	if timeRange != "" {
		if strings.HasSuffix(timeRange, "d") {
			days := 1
			fmt.Sscanf(timeRange, "%dd", &days)
			duration = time.Duration(days) * 24 * time.Hour
		} else if d, err := time.ParseDuration(timeRange); err == nil {
			duration = d
		}
	}

	if maxAlerts <= 0 {
		maxAlerts = 500
	}

	client, err := getLokiClient(lokiConnID)
	if err != nil {
		return nil, fmt.Errorf("Loki client error: %w", err)
	}

	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	var allResults []NamespacedContainerResult
	var lastErr error

	pipelineTrimmed := strings.TrimSpace(pipeline)
	extraLabels := strings.TrimSpace(labelFilters)

	for _, ns := range namespaces {
		select {
		case <-ctx.Done():
			return allResults, ctx.Err()
		case sem <- struct{}{}:
		}

		go func(namespace string) {
			defer safego.Recover("namespace query worker")
			defer func() { <-sem }()

			selector := fmt.Sprintf(`{namespace="%s"`, namespace)
			if extraLabels != "" {
				selector += ", " + extraLabels
			}
			selector += "}"
			logql := selector + " " + pipelineTrimmed
			now := time.Now()
			start := now.Add(-duration)

			result, err := client.QueryRange(ctx, logql, start, now, maxAlerts)
			if err != nil {
				log.Printf("[Namespaced] namespace '%s' query error: %v", namespace, err)
				mu.Lock()
				lastErr = fmt.Errorf("namespace %s: %w", namespace, err)
				mu.Unlock()
				return
			}

			hits := result.ToHits()
			if len(hits) == 0 {
				log.Printf("[Namespaced] namespace '%s' no hits", namespace)
				return
			}

			log.Printf("[Namespaced] namespace '%s' found %d hits", namespace, len(hits))

			// Filter hits by route config (ignore specified code values)
			routeCfg := parseRouteConfig(routeConfigJSON)
			var filtered []map[string]interface{}
			ignoredGroups := make(map[string][]map[string]interface{}) // container -> ignored hits
			if routeCfg != nil {
				for _, hit := range hits {
					vars := extractFields(hit, extractFieldsJSON)
					fieldVal := ""
					if v, ok := vars[routeCfg.RouteField]; ok {
						fieldVal = fmt.Sprintf("%v", v)
					}
					if fieldVal != "" && routeCfg.matchRoute(fieldVal) == -1 {
						container := getContainerName(hit)
						ignoredGroups[container] = append(ignoredGroups[container], hit)
						continue // ignored
					}
					filtered = append(filtered, hit)
				}
				log.Printf("[Namespaced] namespace '%s' after route filter: %d hits (filtered %d)", namespace, len(filtered), len(hits)-len(filtered))
				hits = filtered
			}

			// Group by container
			containerGroups := make(map[string][]map[string]interface{})
			for _, hit := range hits {
				container := getContainerName(hit)
				containerGroups[container] = append(containerGroups[container], hit)
			}

			mu.Lock()
			// Collect all containers that have hits (alerting or ignored)
			allContainerSet := map[string]bool{}
			for c := range containerGroups {
				allContainerSet[c] = true
			}
			for c := range ignoredGroups {
				allContainerSet[c] = true
			}
			for container := range allContainerSet {
				alertHits := containerGroups[container]
				ignoredHits := ignoredGroups[container]
				msg := ""
				if len(alertHits) > 0 {
					// nil: this runs before dedup/mute/interval, so fetching
					// context here would spend Loki queries on hits that are
					// about to be discarded. The sending path re-renders with
					// a real provider once the decision is made.
					msg = BuildNamespacedAlertMessage(namespace, container, severity, extractFieldsJSON, messageTemplate, alertHits, nil)
				}
				allResults = append(allResults, NamespacedContainerResult{
					Namespace:   namespace,
					Container:   container,
					Hits:        alertHits,
					IgnoredHits: ignoredHits,
					HitCount:    len(alertHits),
					Message:     msg,
				})
			}
			mu.Unlock()
		}(ns)
	}

	// Wait for all goroutines
	for i := 0; i < concurrency; i++ {
		sem <- struct{}{}
	}

	if lastErr != nil && len(allResults) == 0 {
		return nil, lastErr
	}
	return allResults, nil
}

// GetLokiClientFunc returns a closure that handlers can use to get Loki clients via the engine
func (e *Engine) GetLokiClientFunc() func(int) (*lokiclient.Client, error) {
	return func(id int) (*lokiclient.Client, error) {
		return e.getLokiClient(id)
	}
}

func getContainerName(hit map[string]interface{}) string {
	if v := es.GetNestedField(hit, "container"); v != nil {
		return fmt.Sprintf("%v", v)
	}
	if v := es.GetNestedField(hit, "container_name"); v != nil {
		return fmt.Sprintf("%v", v)
	}
	if v := es.GetNestedField(hit, "kubernetes.container_name"); v != nil {
		return fmt.Sprintf("%v", v)
	}
	return "(unknown)"
}

// executeNamespacedRule is the engine entry point for cron/manual execution.
// It calls the shared QueryNamespacedLoki, then sends alerts with dedup/interval control.
func (e *Engine) executeNamespacedRule(ctx context.Context, rule *models.AlertRule,
	namespaces []string, sender notify.Notifier, atUsers []models.AtUser, atAll bool,
	ruleIDStr, alertMode string) {

	results, err := QueryNamespacedLoki(ctx, rule.LokiConnectionID, namespaces,
		rule.LogQL, rule.TimeRange, rule.ExtractFields, rule.Severity, rule.MessageTemplate, rule.RouteConfig,
		rule.MaxAlerts, rule.NamespaceConcurrency, rule.LabelFilters, e.GetLokiClientFunc())
	if err != nil {
		errMsg := fmt.Sprintf("Namespaced query error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		return
	}

	var totalSent int
	var lastErr string

	// Parse prometheus config for container metrics
	promCfg := ParsePrometheusConfig(rule.PrometheusConfig)
	projectLabels := getProjectLabels(rule.ProjectID)

	// Reset error code metrics before this run
	if Metrics != nil {
		Metrics.ResetErrorCodesByRule(ruleIDStr)
	}

	// Get all containers from Loki for each namespace (for found mode metrics)
	alertingContainers := map[string]map[string]bool{} // namespace -> set of alerting containers
	allContainers := map[string][]string{}             // namespace -> all containers
	// Collect error codes per container: {ns+container} -> {code -> {msg, count, ignored}}
	type codeInfo struct {
		Msg     string
		Count   float64
		Ignored bool // true if this code is in ignore list
	}
	errorCodes := map[string]map[string]*codeInfo{} // "ns:container" -> code -> info

	for _, r := range results {
		if len(r.Hits) > 0 {
			if alertingContainers[r.Namespace] == nil {
				alertingContainers[r.Namespace] = map[string]bool{}
			}
			alertingContainers[r.Namespace][r.Container] = true
		}

		key := r.Namespace + ":" + r.Container
		if errorCodes[key] == nil {
			errorCodes[key] = map[string]*codeInfo{}
		}

		// Extract error codes from alerting hits
		for _, hit := range r.Hits {
			vars := extractFields(hit, rule.ExtractFields)
			code := fmt.Sprintf("%v", vars["Code"])
			msg := fmt.Sprintf("%v", vars["Msg"])
			if code == "" || code == "<nil>" {
				continue
			}
			if msg == "<nil>" {
				msg = ""
			}
			if errorCodes[key][code] == nil {
				errorCodes[key][code] = &codeInfo{Msg: msg, Count: 0, Ignored: false}
			}
			errorCodes[key][code].Count++
		}

		// Extract error codes from ignored hits (for metrics only)
		for _, hit := range r.IgnoredHits {
			vars := extractFields(hit, rule.ExtractFields)
			code := fmt.Sprintf("%v", vars["Code"])
			msg := fmt.Sprintf("%v", vars["Msg"])
			if code == "" || code == "<nil>" {
				continue
			}
			if msg == "<nil>" {
				msg = ""
			}
			if errorCodes[key][code] == nil {
				errorCodes[key][code] = &codeInfo{Msg: msg, Count: 0, Ignored: true}
			}
			errorCodes[key][code].Count++
		}
	}

	if len(results) == 0 {
		log.Printf("[Engine] Rule %d: namespaced execution, no results", rule.ID)
		database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
	}
	if Metrics != nil {
		// Query all container names per namespace from Loki
		var nsList []string
		json.Unmarshal([]byte(rule.Namespaces), &nsList)
		for _, ns := range nsList {
			client, err := e.getLokiClient(rule.LokiConnectionID)
			if err != nil {
				continue
			}
			containers, err := client.LabelValuesWithQuery(ctx, "container", fmt.Sprintf(`{namespace="%s"}`, ns))
			if err != nil {
				log.Printf("[Engine] Rule %d: get containers for ns %s error: %v", rule.ID, ns, err)
				continue
			}
			allContainers[ns] = containers
		}
	}

	// Contexts fetched so far in THIS run; see MaxContextFetchesPerRun.
	nsCtxFetches := 0
	for _, r := range results {
		// Skip results that only have ignored hits (no alerting hits)
		if len(r.Hits) == 0 {
			continue
		}

		// Dedup: namespace + container
		if rule.DedupField != "" {
			dedupParts := []string{fmt.Sprintf("alert:dedup:%d", rule.ID), r.Namespace, r.Container}
			h := sha256.Sum256([]byte(strings.Join(dedupParts, ":")))
			dedupKey := fmt.Sprintf("alert:dedup:%x", h[:8])
			ttl := time.Duration(rule.DedupTTL) * time.Second
			if ttl <= 0 {
				ttl = time.Hour
			}
			isDup, _ := database.CheckDedup(ctx, dedupKey, ttl)
			if isDup {
				log.Printf("[Engine] Rule %d: [%s/%s] dedup skipped", rule.ID, r.Namespace, r.Container)
				continue
			}
		}

		// Mute check
		if isMuted(rule.ID, r.Container) {
			log.Printf("[Engine] Rule %d: [%s/%s] is muted, skipping", rule.ID, r.Namespace, r.Container)
			continue
		}

		// Alert interval check
		if rule.AlertInterval != "" {
			intervalKey := fmt.Sprintf("alert:last_alert_time:%d:%s:%s", rule.ID, r.Namespace, r.Container)
			if shouldSkip := checkAlertInterval(ctx, intervalKey, rule.AlertInterval); shouldSkip {
				log.Printf("[Engine] Rule %d: [%s/%s] interval skipped", rule.ID, r.Namespace, r.Container)
				continue
			}
		}

		// Stack context for the namespaced path: r.Message is pre-rendered by
		// QueryNamespacedLoki/BuildNamespacedAlertMessage, which also serves the
		// preview and test-send handlers and runs before dedup/mute/interval are
		// decided here. Rendering {{.stack}} inline in the template would either
		// mean fetching before those decisions (exactly what we must not do) or
		// reworking that shared, handler-facing function — out of scope for this
		// change. So for this path only, the stack is applied post-decision,
		// using the container's first surviving hit, rather than substituted as
		// a live template variable during rendering. If the template referenced
		// {{.stack}}, that render already left the literal "<no value>" sitting
		// in r.Message (Go's text/template output for a missing map key);
		// ApplyStackToMessage replaces that token with the real stack. Otherwise
		// it appends the stack in a fenced block, as before.
		// Contexts are fetched HERE, after dedup/mute/interval have decided this
		// container actually alerts, and the message is re-rendered so every
		// shown hit carries its own — the shared query function above renders
		// without them precisely so nothing is spent on discarded hits.
		if (rule.StackContextEnabled == 1 || rule.LogContextEnabled == 1) && len(r.Hits) > 0 {
			provider := func(hit map[string]interface{}) (string, string) {
				var stack, logctx string
				if rule.StackContextEnabled == 1 {
					stack = e.fetchStackContext(ctx, rule, hit)
				}
				if rule.LogContextEnabled == 1 {
					if nsCtxFetches < MaxContextFetchesPerRun {
						logctx = e.fetchLogContext(ctx, rule, hit)
						nsCtxFetches++
					} else {
						logctx = RenderContextSkipped(hit, MaxContextFetchesPerRun)
					}
				}
				return stack, logctx
			}
			r.Message = BuildNamespacedAlertMessage(r.Namespace, r.Container, rule.Severity,
				rule.ExtractFields, rule.MessageTemplate, r.Hits, provider)
		}

		title := rule.MessageTitle
		if title == "" {
			title = rule.Name
		}

		resp, err := sender.SendCard(title, r.Message, rule.Severity, atUsers, atAll)
		if err != nil {
			lastErr = fmt.Sprintf("[%s/%s] send error: %v", r.Namespace, r.Container, err)
			saveAlertLog(rule, r.Message, "", "failed", lastErr, resp)
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
			}
		} else {
			totalSent++
			note := partialSendNote(resp)
			saveAlertLog(rule, r.Message, "", "success", note, resp)
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
			}
			if note != "" {
				// Feeding it through lastErr keeps it out of the "clear
				// last_error" branch below, same as a hard send failure.
				lastErr = fmt.Sprintf("[%s/%s] %s", r.Namespace, r.Container, note)
				recordPartialSend(rule, ruleIDStr, note)
			}
			log.Printf("[Engine] Rule %d: [%s/%s] alert sent, resp=%s", rule.ID, r.Namespace, r.Container, resp)
		}
	}

	if lastErr != "" {
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", lastErr, rule.ID)
	} else {
		database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
	}
	log.Printf("[Engine] Rule %d: namespaced execution done, sent %d alerts", rule.ID, totalSent)

	// Record Prometheus metrics for all containers (found mode)
	if Metrics != nil {
		mergedLabels := map[string]string{}
		for k, v := range projectLabels {
			mergedLabels[k] = v
		}
		if promCfg != nil {
			for k, v := range promCfg.GetStaticLabels() {
				mergedLabels[k] = v
			}
		}

		alertingTotal := 0
		for ns, containers := range allContainers {
			alertSet := alertingContainers[ns]
			for _, c := range containers {
				muted := isMuted(rule.ID, c)
				var status float64
				if muted {
					status = -1
				} else if alertSet != nil && alertSet[c] {
					status = 0 // alerting
					alertingTotal++
				} else {
					status = 1 // normal
				}
				Metrics.RecordContainerStatus(ruleIDStr, rule.Name, ns, c, "found", status, mergedLabels)
				// Cache container status in Redis for restart recovery
				statusKey := fmt.Sprintf("alert:container_status:%d:%s:%s", rule.ID, ns, c)
				database.RDB.Set(ctx, statusKey, int(status), 10*time.Minute)
			}
		}
		database.RDB.Set(ctx, fmt.Sprintf("alert:alerting_count:%d", rule.ID), alertingTotal, 10*time.Minute)

		// Record error code metrics with status:
		// count > 0 = alerting, -1 = muted container, -2 = ignored code
		// Also cache to Redis for restart recovery
		errorCodeContainers := map[string]bool{} // track containers that have error codes
		for key, codes := range errorCodes {
			parts := strings.SplitN(key, ":", 2)
			if len(parts) < 2 {
				continue
			}
			ns, container := parts[0], parts[1]
			errorCodeContainers[key] = true
			muted := isMuted(rule.ID, container)
			for code, info := range codes {
				var value float64
				if muted {
					value = -1
				} else if info.Ignored {
					value = -2
				} else {
					value = info.Count
				}
				Metrics.RecordErrorCode(ruleIDStr, rule.Name, ns, container, code, info.Msg, value, mergedLabels)
				// Cache to Redis: key = alert:error_code:{ruleID}:{ns}:{container}:{code}
				// value = JSON with msg and value
				redisKey := fmt.Sprintf("alert:error_code:%d:%s:%s:%s", rule.ID, ns, container, code)
				cacheVal := fmt.Sprintf(`{"msg":%q,"value":%v}`, info.Msg, value)
				database.RDB.Set(ctx, redisKey, cacheVal, 10*time.Minute)
			}
		}

		// Check if any active alerting code exists (value >= 1)
		hasActiveAlert := false
		for key, codes := range errorCodes {
			parts := strings.SplitN(key, ":", 2)
			container := ""
			if len(parts) >= 2 {
				container = parts[1]
			}
			muted := isMuted(rule.ID, container)
			for _, info := range codes {
				if !info.Ignored && !muted && info.Count >= 1 {
					hasActiveAlert = true
					break
				}
			}
			if hasActiveAlert {
				break
			}
		}
		// No active alerts → write a single =0 indicator
		if !hasActiveAlert {
			Metrics.RecordErrorCode(ruleIDStr, rule.Name, "", "", "", "", 0, mergedLabels)
		}
	}
}

// BuildNamespacedAlertMessage builds the aggregated alert message.
// If messageTemplate is set, renders each hit with the user's template.
// Otherwise uses default 样式1 format.
// Exported so handlers can use it for preview.
// AppendUnreferencedContexts renders whatever the template did not place
// itself, captioned, so a switch that is on is never silently invisible.
//
// Exported because the preview and test-send handlers must append by the same
// rule as the engine: a message that looks one way when tested and another way
// when it fires is worse than either shape on its own.
func AppendUnreferencedContexts(tmpl, stack, logctx string) string {
	var b strings.Builder
	if stack != "" && !stackVarPattern.MatchString(tmpl) {
		b.WriteString(StackCaption + "\n" + notify.FencedBlock(stack) + "\n")
	}
	if logctx != "" && !logCtxVarPattern.MatchString(tmpl) {
		b.WriteString(LogContextCaption + "\n" + notify.FencedBlock(logctx) + "\n")
	}
	return b.String()
}

// HitContextProvider returns one hit's stack and log context. It is passed in
// rather than fetched here because this builder also renders the preview and
// test-send paths, and the cron path must not spend Loki queries on hits that
// dedup, mute or the alert interval are about to discard.
//
// nil means "render without context" — the shape every caller had before.
type HitContextProvider func(hit map[string]interface{}) (stack, logctx string)

// BuildNamespacedAlertMessage renders one container's alert.
//
// ctxFor is applied PER HIT: an aggregated message shows several matched lines,
// and one context block appended to the end of all of them belongs to none of
// them — the reader cannot tell which line it explains. Each hit carries its
// own, directly under the fields it belongs to.
func BuildNamespacedAlertMessage(namespace, container, severity, extractFieldsJSON, messageTemplate string, hits []map[string]interface{}, ctxFor HitContextProvider) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("**Namespace:** %s\n", namespace))
	b.WriteString(fmt.Sprintf("**Container:** %s\n", container))
	b.WriteString(fmt.Sprintf("**级别:** %s | **命中:** %d 条\n", severity, len(hits)))

	showCount := 3
	if len(hits) < showCount {
		showCount = len(hits)
	}

	for i := 0; i < showCount; i++ {
		hit := hits[i]
		b.WriteString(fmt.Sprintf("\n─── %d/%d ───\n", i+1, showCount))

		var stack, logctx string
		if ctxFor != nil {
			stack, logctx = ctxFor(hit)
		}

		if messageTemplate != "" {
			// Use user's template
			vars := extractFields(hit, extractFieldsJSON)
			// Also add all raw fields as template vars
			for k, v := range hit {
				if _, exists := vars[k]; !exists {
					vars[k] = v
				}
			}
			// Contexts go in BEFORE rendering, so a template that places them
			// itself gets them substituted by name rather than appended.
			if stack != "" {
				vars["stack"] = stack
			}
			if logctx != "" {
				vars["logcontext"] = logctx
			}
			rendered := renderTemplate(messageTemplate, vars)
			b.WriteString(rendered)
			b.WriteString("\n")
			b.WriteString(AppendUnreferencedContexts(messageTemplate, stack, logctx))
		} else {
			// Default style1
			if extractFieldsJSON != "" {
				vars := extractFields(hit, extractFieldsJSON)
				for name, val := range vars {
					if name == "" || name[0] == '_' {
						continue
					}
					b.WriteString(fmt.Sprintf("**%s:** %v\n", name, val))
				}
			}

			podName := ""
			if v := es.GetNestedField(hit, "pod"); v != nil {
				podName = fmt.Sprintf("%v", v)
			} else if v := es.GetNestedField(hit, "pod_name"); v != nil {
				podName = fmt.Sprintf("%v", v)
			} else if v := es.GetNestedField(hit, "kubernetes.pod_name"); v != nil {
				podName = fmt.Sprintf("%v", v)
			}
			if podName != "" {
				b.WriteString(fmt.Sprintf("**Pod:** %s\n", podName))
			}

			if msg := es.GetNestedField(hit, "message"); msg != nil {
				b.WriteString("**日志:**\n")
				b.WriteString(notify.FencedBlock(truncateLogRunes(fmt.Sprintf("%v", msg), maxInlineLogRunes)))
				b.WriteString("\n")
			}
			b.WriteString(AppendUnreferencedContexts("", stack, logctx))
		}
	}

	if len(hits) > showCount {
		b.WriteString(fmt.Sprintf("\n⏰ 共 %d 条，显示前 %d 条", len(hits), showCount))
	}

	return b.String()
}

// checkAlertInterval checks if enough time has passed since last alert
func checkAlertInterval(ctx context.Context, key, interval string) bool {
	if interval == AlertIntervalOnce {
		// once 依赖 not_found 的告警状态机，found 模式没有状态机，这里按「不限流」处理
		log.Printf("[Engine] WARN: alert_interval=once 只在 not_found 模式生效，key=%s，按每次都发处理", key)
		return false
	}
	d, err := time.ParseDuration(interval)
	if err != nil {
		log.Printf("[Engine] WARN: 无法识别的 alert_interval '%s'，key=%s，按每次都发处理", interval, key)
		return false
	}
	lastTime, err := database.RDB.Get(ctx, key).Result()
	if err != nil {
		// No previous alert, allow
		database.RDB.Set(ctx, key, time.Now().Unix(), d)
		return false
	}
	var ts int64
	fmt.Sscanf(lastTime, "%d", &ts)
	if time.Since(time.Unix(ts, 0)) < d {
		return true // skip, too soon
	}
	database.RDB.Set(ctx, key, time.Now().Unix(), d)
	return false
}
