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
		severity, COALESCE(group_by,''), COALESCE(expected_groups,''), COALESCE(query_concurrency,5), COALESCE(alert_interval,''), dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''), status
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
			&rule.PrometheusConfig, &rule.Status)
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

	sentCount := 0
	for _, hit := range result.Hits {
		vars := extractFields(hit, rule.ExtractFields)

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

		resp, err := sender.SendCard(rule.MessageTitle, message, rule.Severity, atUsers, atAll)
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
		COALESCE(prometheus_config,''), status
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.DataSourceType,
		&rule.ESConnectionID, &rule.LokiConnectionID, &rule.LarkConfigID, &rule.ESIndex,
		&rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.LogQL, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.AlertMode, &rule.RecoveryEnabled, &rule.RecoveryTitle, &rule.RecoveryTemplate,
		&rule.Severity, &rule.GroupBy, &rule.ExpectedGroups, &rule.QueryConcurrency, &rule.AlertInterval, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
		&rule.PrometheusConfig, &rule.Status)
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
			var staticLabels map[string]string
			if promCfg != nil {
				staticLabels = promCfg.GetStaticLabels()
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
			Metrics.RecordContainerStatus(ruleIDStr, rule.Name, namespace, groupKey, metricStatus, staticLabels)
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
				log.Printf("[Engine] Rule %d: using last hit for group '%s'", rule.ID, groupKey)
			}
			message := renderTemplate(rule.MessageTemplate, vars)
			// Render title template too
			titleTemplate := rule.MessageTitle
			titleRendered := renderTemplate(titleTemplate, vars)
			title := fmt.Sprintf("%s [%s]", titleRendered, groupKey)

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

// isMuted checks if a group is currently muted for a rule
func isMuted(ruleID int, groupKey string) bool {
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM alert_mutes WHERE rule_id = ? AND group_key = ? AND mute_until > NOW()",
		ruleID, groupKey).Scan(&count)
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
