package alert

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricsManager manages Prometheus metrics
type MetricsManager struct {
	mu sync.RWMutex

	// Built-in rule-level metrics
	alertTotal   *prometheus.CounterVec
	alertSuccess *prometheus.CounterVec
	alertFailed  *prometheus.CounterVec
	alertHits    *prometheus.GaugeVec
	lastRunTime  *prometheus.GaugeVec
	lastRunDur   *prometheus.GaugeVec
	ruleStatus   *prometheus.GaugeVec

	// Container-level metric (dynamic labels)
	containerStatusRegistered bool
	containerStatus           *prometheus.GaugeVec
	containerStatusLabels     []string // sorted label names
}

// PrometheusConfig user-defined prometheus config per rule
type PrometheusConfig struct {
	Enabled      bool              `json:"enabled"`
	StaticLabels map[string]string `json:"static_labels"` // custom labels added to alert_container_status
	Labels       map[string]string `json:"labels"`        // backward compat alias
}

// GetStaticLabels returns static labels (supports both fields)
func (c *PrometheusConfig) GetStaticLabels() map[string]string {
	if len(c.StaticLabels) > 0 {
		return c.StaticLabels
	}
	return c.Labels
}

var Metrics *MetricsManager

func InitMetrics() {
	Metrics = &MetricsManager{}

	labels := []string{"rule_id", "rule_name", "severity"}

	Metrics.alertTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alert_fired_total",
		Help: "Total number of alerts fired",
	}, labels)

	Metrics.alertSuccess = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alert_send_success_total",
		Help: "Total successful alert sends to Lark",
	}, labels)

	Metrics.alertFailed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alert_send_failed_total",
		Help: "Total failed alert sends",
	}, labels)

	Metrics.alertHits = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "alert_es_hits",
		Help: "Number of hits found in last run",
	}, labels)

	Metrics.lastRunTime = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "alert_last_run_timestamp",
		Help: "Timestamp of last rule execution",
	}, labels)

	Metrics.lastRunDur = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "alert_last_run_duration_seconds",
		Help: "Duration of last rule execution in seconds",
	}, labels)

	Metrics.ruleStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "alert_rule_enabled",
		Help: "Whether the alert rule is enabled (1) or disabled (0)",
	}, labels)

	prometheus.MustRegister(
		Metrics.alertTotal,
		Metrics.alertSuccess,
		Metrics.alertFailed,
		Metrics.alertHits,
		Metrics.lastRunTime,
		Metrics.lastRunDur,
		Metrics.ruleStatus,
	)

	log.Println("[Metrics] Prometheus metrics initialized")
}

// RecordRuleRun records metrics for a rule execution
func (m *MetricsManager) RecordRuleRun(ruleID, ruleName, severity string, hits int, duration float64) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	m.alertHits.With(l).Set(float64(hits))
	m.lastRunTime.With(l).SetToCurrentTime()
	m.lastRunDur.With(l).Set(duration)
}

func (m *MetricsManager) RecordAlertFired(ruleID, ruleName, severity string) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	m.alertTotal.With(l).Inc()
}

func (m *MetricsManager) RecordSendSuccess(ruleID, ruleName, severity string) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	m.alertSuccess.With(l).Inc()
}

func (m *MetricsManager) RecordSendFailed(ruleID, ruleName, severity string) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	m.alertFailed.With(l).Inc()
}

func (m *MetricsManager) SetRuleStatus(ruleID, ruleName, severity string, enabled bool) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	if enabled {
		m.ruleStatus.With(l).Set(1)
	} else {
		m.ruleStatus.With(l).Set(0)
	}
}

// RecordContainerStatus records per-container alert status with custom static labels
// status: 1=normal, 0=alerting
func (m *MetricsManager) RecordContainerStatus(ruleID, ruleName, namespace, container string, ok bool, staticLabels map[string]string) {
	// Build label names (need to register gauge with all label names on first call)
	m.mu.Lock()
	if !m.containerStatusRegistered {
		// Collect all static label keys
		labelNames := []string{"rule_id", "rule_name", "namespace", "container"}
		var keys []string
		for k := range staticLabels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		labelNames = append(labelNames, keys...)

		m.containerStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "alert_container_status",
			Help: "Container alert status: 1=normal 0=alerting",
		}, labelNames)

		if err := prometheus.Register(m.containerStatus); err != nil {
			log.Printf("[Metrics] 注册 alert_container_status 失败: %v", err)
			m.mu.Unlock()
			return
		}
		m.containerStatusLabels = labelNames
		m.containerStatusRegistered = true
		log.Printf("[Metrics] 注册 alert_container_status, labels: %v", labelNames)
	}
	m.mu.Unlock()

	// Build labels
	labels := prometheus.Labels{
		"rule_id":   ruleID,
		"rule_name": ruleName,
		"namespace": namespace,
		"container": container,
	}
	for k, v := range staticLabels {
		labels[k] = v
	}

	if ok {
		m.containerStatus.With(labels).Set(1)
	} else {
		m.containerStatus.With(labels).Set(0)
	}
}

// RegisterCustomMetrics kept for backward compatibility (no-op)
func (m *MetricsManager) RegisterCustomMetrics(configJSON string) {}

// ParsePrometheusConfig parses the JSON config
func ParsePrometheusConfig(configJSON string) *PrometheusConfig {
	if configJSON == "" {
		return nil
	}
	var cfg PrometheusConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil
	}
	if !cfg.Enabled {
		return nil
	}
	return &cfg
}

// SetCustomGauge legacy method (no-op)
func (m *MetricsManager) SetCustomGauge(metricName string, labels prometheus.Labels, value float64) {}

// toFloat64 converts interface{} to float64
func toFloat64(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}
