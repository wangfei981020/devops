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
	"opsplatform-alert-backend/models"
)

// Engine manages all alert rules and their schedules
type Engine struct {
	mu       sync.RWMutex
	cron     *cron.Cron
	jobs     map[int]cron.EntryID // ruleID -> cronEntryID
	clients  map[int]*es.Client   // esConnectionID -> client
	stopping bool
}

func NewEngine() *Engine {
	return &Engine{
		cron:    cron.New(cron.WithSeconds()),
		jobs:    make(map[int]cron.EntryID),
		clients: make(map[int]*es.Client),
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

// RefreshESClient refreshes or removes an ES client
func (e *Engine) RefreshESClient(connID int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.clients, connID)
}

func (e *Engine) loadAllRules() error {
	rows, err := database.DB.Query(`SELECT id, name, es_connection_id, lark_config_id, es_index,
		schedule, time_range, COALESCE(query_dsl,''), keyword, COALESCE(filter_fields,''),
		COALESCE(extract_fields,''), message_title, COALESCE(message_template,''),
		COALESCE(at_users,''), at_all, severity,
		dedup_field, dedup_ttl, max_alerts, COALESCE(prometheus_config,''), status
		FROM alert_rules WHERE status = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var rule models.AlertRule
		err := rows.Scan(&rule.ID, &rule.Name, &rule.ESConnectionID, &rule.LarkConfigID,
			&rule.ESIndex, &rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
			&rule.Keyword, &rule.FilterFields, &rule.ExtractFields,
			&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
			&rule.Severity, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
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

	// Get ES client
	client, err := e.getESClient(rule.ESConnectionID)
	if err != nil {
		errMsg := fmt.Sprintf("ES client error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		saveAlertLog(rule, "", "", "failed", errMsg)
		return
	}

	// Build query
	maxAlerts := rule.MaxAlerts
	if maxAlerts <= 0 {
		maxAlerts = 10
	}
	query, err := es.BuildQuery(rule.Keyword, rule.FilterFields, rule.TimeRange, rule.QueryDSL, maxAlerts)
	if err != nil {
		errMsg := fmt.Sprintf("Query build error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		saveAlertLog(rule, "", "", "failed", errMsg)
		return
	}

	// Search ES
	result, err := client.Search(ctx, rule.ESIndex, query)
	if err != nil {
		errMsg := fmt.Sprintf("ES search error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		saveAlertLog(rule, "", "", "failed", errMsg)
		return
	}

	if len(result.Hits) == 0 {
		log.Printf("[Engine] Rule %d: no hits", rule.ID)
		return
	}

	log.Printf("[Engine] Rule %d: found %d hits (total=%d)", rule.ID, len(result.Hits), result.Total)

	// Get Lark config
	larkConfig, err := getLarkConfigByID(rule.LarkConfigID)
	if err != nil {
		errMsg := fmt.Sprintf("Lark config error: %v", err)
		log.Printf("[Engine] Rule %d: %s", rule.ID, errMsg)
		database.DB.Exec("UPDATE alert_rules SET last_error = ? WHERE id = ?", errMsg, rule.ID)
		return
	}

	sender := lark.NewSender(*larkConfig)

	// Parse at_users
	var atUsers []models.AtUser
	if rule.AtUsers != "" {
		json.Unmarshal([]byte(rule.AtUsers), &atUsers)
	}
	atAll := rule.AtAll == 1

	// Process each hit
	sentCount := 0
	for _, hit := range result.Hits {
		// Extract fields
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

		// Render message
		message := renderTemplate(rule.MessageTemplate, vars)

		// Store ES raw for log
		rawJSON, _ := json.Marshal(hit)

		// Send to Lark
		ruleIDStr := fmt.Sprintf("%d", rule.ID)
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
	err := database.DB.QueryRow(`SELECT id, name, es_connection_id, lark_config_id, es_index,
		schedule, time_range, COALESCE(query_dsl,''), keyword, COALESCE(filter_fields,''),
		COALESCE(extract_fields,''), message_title, COALESCE(message_template,''),
		COALESCE(at_users,''), at_all, severity, dedup_field, dedup_ttl, max_alerts,
		COALESCE(prometheus_config,''), status
		FROM alert_rules WHERE id = ?`, id).Scan(
		&rule.ID, &rule.Name, &rule.ESConnectionID, &rule.LarkConfigID,
		&rule.ESIndex, &rule.Schedule, &rule.TimeRange, &rule.QueryDSL,
		&rule.Keyword, &rule.FilterFields, &rule.ExtractFields,
		&rule.MessageTitle, &rule.MessageTemplate, &rule.AtUsers, &rule.AtAll,
		&rule.Severity, &rule.DedupField, &rule.DedupTTL, &rule.MaxAlerts,
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

func (e *Engine) getESClient(connID int) (*es.Client, error) {
	e.mu.RLock()
	client, ok := e.clients[connID]
	e.mu.RUnlock()

	if ok {
		return client, nil
	}

	// Fetch connection config from DB
	var conn models.ESConnection
	err := database.DB.QueryRow(`SELECT id, name, url, version, username, password, api_key
		FROM es_connections WHERE id = ? AND status = 1`, connID).Scan(
		&conn.ID, &conn.Name, &conn.URL, &conn.Version, &conn.Username, &conn.Password, &conn.APIKey)
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
