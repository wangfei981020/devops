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

	"github.com/robfig/cron/v3"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/es"
	"opsplatform-alert-backend/lark"
	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
)

// RouteConfig defines field-value based routing for found mode alerts
type RouteConfig struct {
	RouteField   string        `json:"route_field"`   // field name to extract value from (e.g. "code")
	IgnoreValues []string      `json:"ignore_values"` // values to ignore (no alert)
	Routes       []RouteRule   `json:"routes"`        // routing rules
	DefaultLarkID int          `json:"default_lark_id"` // fallback lark config ID (0 = use rule's default)
}

type RouteRule struct {
	Values []string `json:"values"` // field values to match
	LarkID int      `json:"lark_id"` // lark config ID to send to
	Name   string   `json:"name"`   // description (e.g. "严重错误群")
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

// Engine manages all alert rules and their schedules
type Engine struct {
	mu          sync.RWMutex
	cron        *cron.Cron
	jobs        map[int]cron.EntryID          // ruleID -> cronEntryID
	clients     map[int]*es.Client            // esConnectionID -> ES client
	lokiClients map[int]*lokiclient.Client    // lokiConnectionID -> Loki client
	stopping    bool
}

func NewEngine() *Engine {
	return &Engine{
		cron:        cron.New(cron.WithSeconds()),
		jobs:        make(map[int]cron.EntryID),
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

	e.cron.Start()
	log.Println("[Engine] Alert engine started")
	return nil
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

// ReloadRule reloads a single rule (add/update/remove)
func (e *Engine) ReloadRule(ruleID int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove existing job if present
	if entryID, ok := e.jobs[ruleID]; ok {
		e.cron.Remove(entryID)
		delete(e.jobs, ruleID)
		log.Printf("[Engine] Removed job for rule %d", ruleID)
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

	return e.addJob(rule)
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
}

// CleanupRuleRedisKeys removes all Redis keys for a deleted rule
func (e *Engine) CleanupRuleRedisKeys(ruleID int) {
	ctx := context.Background()
	patterns := []string{
		fmt.Sprintf("alert:state:%d:*", ruleID),
		fmt.Sprintf("alert:last_hit:%d:*", ruleID),
		fmt.Sprintf("alert:dedup:%d:*", ruleID),
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
		severity, COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(query_concurrency,5), COALESCE(alert_interval,''), dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''), COALESCE(route_config,''), status
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
			&rule.PrometheusConfig, &rule.RouteConfig, &rule.Status)
		if err != nil {
			log.Printf("[Engine] Failed to scan rule: %v", err)
			continue
		}

		if err := e.addJob(&rule); err != nil {
			log.Printf("[Engine] Failed to add job for rule %d: %v", rule.ID, err)
			continue
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
	// Convert 5-field cron to 6-field (add seconds=0)
	schedule := rule.Schedule
	fields := strings.Fields(schedule)
	if len(fields) == 5 {
		schedule = "0 " + schedule
	}

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

	// Get Lark config
	larkConfig, err := getLarkConfigByID(rule.LarkConfigID)
	if err != nil {
		errMsg := fmt.Sprintf("Lark config error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		return
	}

	sender := lark.NewSender(*larkConfig)

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
		saveAlertLog(rule, "", "", "failed", errMsg)
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
				errMsg := fmt.Sprintf("Lark send error: %v", sErr)
				database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
				saveAlertLog(rule, lastHitMsg, string(rawJSON), "failed", errMsg)
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
				}
			} else {
				database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
				database.RDB.Set(ctx, lastAlertKey, time.Now().Format(time.RFC3339), 7*24*time.Hour)
				saveAlertLog(rule, lastHitMsg, string(rawJSON), "success", "")
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
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
					saveAlertLog(rule, message, string(rawJSON), "failed", fmt.Sprintf("Recovery send error: %v", sErr))
				} else {
					saveAlertLog(rule, message, string(rawJSON), "success", "")
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
	// Cache of senders for different lark configs (for routing)
	senderCache := map[int]*lark.Sender{rule.LarkConfigID: sender}

	sentCount := 0
	for _, hit := range result.Hits {
		vars := extractFields(hit, rule.ExtractFields)

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
				} else {
					routeLarkCfg, err := getLarkConfigByID(larkID)
					if err == nil {
						activeSender = lark.NewSender(*routeLarkCfg)
						senderCache[larkID] = activeSender
					} else {
						log.Printf("[Engine] Rule %d: route lark_id=%d not found, using default", rule.ID, larkID)
					}
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

		message := renderTemplate(rule.MessageTemplate, vars)
		rawJSON, _ := json.Marshal(hit)

		time.Sleep(200 * time.Millisecond)
		resp, err := activeSender.SendCard(rule.MessageTitle, message, rule.Severity, atUsers, atAll)
		if err != nil {
			errMsg := fmt.Sprintf("Lark send error: %v", err)
			log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
			database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
			saveAlertLog(rule, message, string(rawJSON), "failed", errMsg)
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
			}
			continue
		}

		saveAlertLog(rule, message, string(rawJSON), "success", "")
		sentCount++
		if Metrics != nil {
			Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
			Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
		}
		log.Printf("[Engine] Rule %d: alert sent (%d/%d), resp=%s", rule.ID, sentCount, len(result.Hits), resp)
	}

	if sentCount > 0 {
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

// renderTemplate renders a Go template with variables
func renderTemplate(tmplStr string, vars map[string]interface{}) string {
	if tmplStr == "" {
		// Default template: list all vars
		var sb strings.Builder
		for k, v := range vars {
			if k == "_id" || k == "_index" {
				continue
			}
			sb.WriteString(fmt.Sprintf("**%s:** %v\n", k, v))
		}
		return sb.String()
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
		COALESCE(prometheus_config,''), COALESCE(route_config,''), COALESCE(namespaces,''), COALESCE(namespace_concurrency,3), status
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.DataSourceType,
		&rule.ESConnectionID, &rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
		&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
		&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
		&rule.PrometheusConfig, &rule.RouteConfig, &rule.Namespaces, &rule.NamespaceConcurrency, &rule.Status)
	return &rule, err
}

func getLarkConfigByID(id int) (*models.LarkConfig, error) {
	var cfg models.LarkConfig
	err := database.DB.QueryRow(`SELECT id, name, webhook_url, secret, lark_type, description, status
		FROM lark_configs WHERE id = ? AND status = 1`, id).Scan(
		&cfg.ID, &cfg.Name, &cfg.WebhookURL, &cfg.Secret, &cfg.LarkType, &cfg.Description, &cfg.Status)
	return &cfg, err
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

func saveAlertLog(rule *models.AlertRule, message, esRaw, status, errMsg string) {
	_, err := database.DB.Exec(`INSERT INTO alert_logs (rule_id, rule_name, severity, message, es_raw, status, error_msg)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.Name, rule.Severity, message, esRaw, status, errMsg)
	if err != nil {
		log.Printf("[Engine] Failed to save alert log: %v", err)
	}
}

// executeGroupedRule processes hits grouped by a field, each group handled independently
func (e *Engine) executeGroupedRule(ctx context.Context, rule *models.AlertRule,
	result *es.SearchResult, sender *lark.Sender, atUsers []models.AtUser, atAll bool,
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

		message := renderTemplate(rule.MessageTemplate, vars)
		rawJSON, _ := json.Marshal(firstHit)

		title := rule.MessageTitle
		if title != "" {
			title = fmt.Sprintf("%s [%s]", rule.MessageTitle, groupKey)
		}

		resp, err := sender.SendCard(title, message, rule.Severity, atUsers, atAll)
		if err != nil {
			saveAlertLog(rule, message, string(rawJSON), "failed", fmt.Sprintf("[%s] %v", groupKey, err))
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
			}
			continue
		}

		saveAlertLog(rule, message, string(rawJSON), "success", "")
		sentCount++
		if Metrics != nil {
			Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
			Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
		}
		log.Printf("[Engine] Rule %d: group '%s' alert sent, resp=%s", rule.ID, groupKey, resp)
	}

	if sentCount > 0 {
		database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
	}
}

// executeGroupedNotFound handles not_found mode with grouping
// It discovers known groups from a wider time range, then checks which groups are missing in the current range
func (e *Engine) executeGroupedNotFound(ctx context.Context, rule *models.AlertRule,
	sender *lark.Sender, atUsers []models.AtUser, atAll bool, ruleIDStr string,
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
				saveAlertLog(rule, message, "", "failed", fmt.Sprintf("[%s] %v", groupKey, sErr))
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
				}
			} else {
				saveAlertLog(rule, message, "", "success", "")
				if Metrics != nil {
					Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
					Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
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
					saveAlertLog(rule, message, string(rawJSON), "failed", fmt.Sprintf("[%s] recovery: %v", groupKey, sErr))
				} else {
					saveAlertLog(rule, message, string(rawJSON), "success", "")
					log.Printf("[Engine] Rule %d: group '%s' recovery sent, resp=%s", rule.ID, groupKey, resp)
				}
			} else if curState == "alerting" {
				database.RDB.Set(ctx, stateKey, "normal", 7*24*time.Hour)
				database.RDB.Del(ctx, lastAlertKey)
			}
		}

	}

	database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)

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
			var larkID string
			err := database.DB.QueryRow("SELECT lark_id FROM alert_contacts WHERE name = ? AND status = 1", name).Scan(&larkID)
			if err == nil && larkID != "" {
				result = append(result, models.AtUser{Name: name, UserID: larkID})
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
	Namespace string                   `json:"namespace"`
	Container string                   `json:"container"`
	Hits      []map[string]interface{} `json:"hits"`
	HitCount  int                      `json:"hit_count"`
	Message   string                   `json:"message"`   // 渲染好的样式1消息
}

// QueryNamespacedLoki is the shared function for all 4 paths (preview, test-send, manual run, cron).
// It queries Loki per namespace, groups by container, builds aggregated messages.
// Exported so handlers package can call it.
func QueryNamespacedLoki(ctx context.Context, lokiConnID int, namespaces []string, pipeline, timeRange, extractFieldsJSON, severity, messageTemplate, routeConfigJSON string, maxAlerts, concurrency int, getLokiClient func(int) (*lokiclient.Client, error)) ([]NamespacedContainerResult, error) {
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

	for _, ns := range namespaces {
		select {
		case <-ctx.Done():
			return allResults, ctx.Err()
		case sem <- struct{}{}:
		}

		go func(namespace string) {
			defer func() { <-sem }()

			logql := fmt.Sprintf(`{namespace="%s"} %s`, namespace, pipelineTrimmed)
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
			if routeCfg != nil {
				var filtered []map[string]interface{}
				for _, hit := range hits {
					vars := extractFields(hit, extractFieldsJSON)
					fieldVal := ""
					if v, ok := vars[routeCfg.RouteField]; ok {
						fieldVal = fmt.Sprintf("%v", v)
					}
					if fieldVal != "" && routeCfg.matchRoute(fieldVal) == -1 {
						log.Printf("[Namespaced] ignoring hit with %s=%s", routeCfg.RouteField, fieldVal)
						continue // ignored
					}
					filtered = append(filtered, hit)
				}
				log.Printf("[Namespaced] namespace '%s' after route filter: %d hits (filtered %d)", namespace, len(filtered), len(hits)-len(filtered))
				hits = filtered
				if len(hits) == 0 {
					return
				}
			}

			// Group by container
			containerGroups := make(map[string][]map[string]interface{})
			for _, hit := range hits {
				container := getContainerName(hit)
				containerGroups[container] = append(containerGroups[container], hit)
			}

			mu.Lock()
			for container, containerHits := range containerGroups {
				msg := BuildNamespacedAlertMessage(namespace, container, severity, extractFieldsJSON, messageTemplate, containerHits)
				allResults = append(allResults, NamespacedContainerResult{
					Namespace: namespace,
					Container: container,
					Hits:      containerHits,
					HitCount:  len(containerHits),
					Message:   msg,
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
	namespaces []string, sender *lark.Sender, atUsers []models.AtUser, atAll bool,
	ruleIDStr, alertMode string) {

	results, err := QueryNamespacedLoki(ctx, rule.LokiConnectionID, namespaces,
		rule.LogQL, rule.TimeRange, rule.ExtractFields, rule.Severity, rule.MessageTemplate, rule.RouteConfig,
		rule.MaxAlerts, rule.NamespaceConcurrency, e.GetLokiClientFunc())
	if err != nil {
		errMsg := fmt.Sprintf("Namespaced query error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		return
	}

	if len(results) == 0 {
		log.Printf("[Engine] Rule %d: namespaced execution, no results", rule.ID)
		database.DB.Exec("UPDATE alert_rules SET last_error = NULL WHERE id = ?", rule.ID)
		return
	}

	var totalSent int
	var lastErr string

	// Parse prometheus config for container metrics
	promCfg := ParsePrometheusConfig(rule.PrometheusConfig)
	projectLabels := getProjectLabels(rule.ProjectID)

	// Get all containers from Loki for each namespace (for found mode metrics)
	alertingContainers := map[string]map[string]bool{} // namespace -> set of alerting containers
	allContainers := map[string][]string{}              // namespace -> all containers
	for _, r := range results {
		if alertingContainers[r.Namespace] == nil {
			alertingContainers[r.Namespace] = map[string]bool{}
		}
		alertingContainers[r.Namespace][r.Container] = true
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

	for _, r := range results {

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

		title := rule.MessageTitle
		if title == "" {
			title = rule.Name
		}

		resp, err := sender.SendCard(title, r.Message, rule.Severity, atUsers, atAll)
		if err != nil {
			lastErr = fmt.Sprintf("[%s/%s] send error: %v", r.Namespace, r.Container, err)
			saveAlertLog(rule, r.Message, "", "failed", lastErr)
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendFailed(ruleIDStr, rule.Name, rule.Severity)
			}
		} else {
			totalSent++
			saveAlertLog(rule, r.Message, "", "success", "")
			if Metrics != nil {
				Metrics.RecordAlertFired(ruleIDStr, rule.Name, rule.Severity)
				Metrics.RecordSendSuccess(ruleIDStr, rule.Name, rule.Severity)
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
			}
		}
		database.RDB.Set(ctx, fmt.Sprintf("alert:alerting_count:%d", rule.ID), alertingTotal, 10*time.Minute)
	}
}

// BuildNamespacedAlertMessage builds the aggregated alert message.
// If messageTemplate is set, renders each hit with the user's template.
// Otherwise uses default 样式1 format.
// Exported so handlers can use it for preview.
func BuildNamespacedAlertMessage(namespace, container, severity, extractFieldsJSON, messageTemplate string, hits []map[string]interface{}) string {
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

		if messageTemplate != "" {
			// Use user's template
			vars := extractFields(hit, extractFieldsJSON)
			// Also add all raw fields as template vars
			for k, v := range hit {
				if _, exists := vars[k]; !exists {
					vars[k] = v
				}
			}
			rendered := renderTemplate(messageTemplate, vars)
			b.WriteString(rendered)
			b.WriteString("\n")
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
				logLine := fmt.Sprintf("%v", msg)
				if len(logLine) > 500 {
					logLine = logLine[:500] + "..."
				}
				b.WriteString(fmt.Sprintf("**日志:** %s\n", logLine))
			}
		}
	}

	if len(hits) > showCount {
		b.WriteString(fmt.Sprintf("\n⏰ 共 %d 条，显示前 %d 条", len(hits), showCount))
	}

	return b.String()
}

// checkAlertInterval checks if enough time has passed since last alert
func checkAlertInterval(ctx context.Context, key, interval string) bool {
	d, err := time.ParseDuration(interval)
	if err != nil {
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
