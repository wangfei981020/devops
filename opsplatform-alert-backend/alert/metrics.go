package alert

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricsManager manages per-rule Prometheus metrics
type MetricsManager struct {
	mu sync.RWMutex

	// Built-in metrics (auto for every rule)
	alertTotal   *prometheus.CounterVec // total alerts fired
	alertSuccess *prometheus.CounterVec // successful sends
	alertFailed  *prometheus.CounterVec // failed sends
	alertHits    *prometheus.GaugeVec   // hits found in last run
	lastRunTime  *prometheus.GaugeVec   // last run timestamp
	lastRunDur   *prometheus.GaugeVec   // last run duration seconds
	ruleStatus   *prometheus.GaugeVec   // rule enabled/disabled

	// Custom gauges per rule (user-defined)
	customGauges map[string]*prometheus.GaugeVec // key: metric_name
}

// PrometheusConfig user-defined prometheus config per rule
type PrometheusConfig struct {
	Enabled    bool           `json:"enabled"`
	MetricName string         `json:"metric_name"` // custom metric name prefix
	Labels     map[string]string `json:"labels"`   // static labels to add
	CustomMetrics []CustomMetric `json:"custom_metrics"` // additional custom metrics
}

// CustomMetric a user-defined metric extracted from alert data
type CustomMetric struct {
	Name      string `json:"name"`       // metric name (will be prefixed)
	Help      string `json:"help"`       // metric description
	Type      string `json:"type"`       // gauge or counter
	ValueFrom string `json:"value_from"` // field name to extract value from
}

var Metrics *MetricsManager

func InitMetrics() {
	Metrics = &MetricsManager{
		customGauges: make(map[string]*prometheus.GaugeVec),
	}

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
		Help: "Number of ES hits found in last run",
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

// RecordAlertFired increments the fired counter
func (m *MetricsManager) RecordAlertFired(ruleID, ruleName, severity string) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	m.alertTotal.With(l).Inc()
}

// RecordSendSuccess increments success counter
func (m *MetricsManager) RecordSendSuccess(ruleID, ruleName, severity string) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	m.alertSuccess.With(l).Inc()
}

// RecordSendFailed increments failed counter
func (m *MetricsManager) RecordSendFailed(ruleID, ruleName, severity string) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	m.alertFailed.With(l).Inc()
}

// SetRuleStatus sets the rule enabled status
func (m *MetricsManager) SetRuleStatus(ruleID, ruleName, severity string, enabled bool) {
	l := prometheus.Labels{"rule_id": ruleID, "rule_name": ruleName, "severity": severity}
	if enabled {
		m.ruleStatus.With(l).Set(1)
	} else {
		m.ruleStatus.With(l).Set(0)
	}
}

// SetCustomGauge sets a custom gauge metric value
func (m *MetricsManager) SetCustomGauge(metricName string, labels prometheus.Labels, value float64) {
	m.mu.RLock()
	g, ok := m.customGauges[metricName]
	m.mu.RUnlock()

	if ok {
		g.With(labels).Set(value)
	}
}

// RegisterCustomMetrics registers custom metrics for a rule based on its PrometheusConfig
func (m *MetricsManager) RegisterCustomMetrics(configJSON string) {
	if configJSON == "" {
		return
	}
	var cfg PrometheusConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return
	}
	if !cfg.Enabled || len(cfg.CustomMetrics) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, cm := range cfg.CustomMetrics {
		fullName := cfg.MetricName + "_" + cm.Name
		if _, exists := m.customGauges[fullName]; exists {
			continue
		}

		// Build label names from static labels config
		labelNames := []string{"rule_id", "rule_name"}
		for k := range cfg.Labels {
			labelNames = append(labelNames, k)
		}

		g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: fullName,
			Help: cm.Help,
		}, labelNames)

		if err := prometheus.Register(g); err != nil {
			log.Printf("[Metrics] Failed to register custom metric %s: %v", fullName, err)
			continue
		}
		m.customGauges[fullName] = g
		log.Printf("[Metrics] Registered custom metric: %s", fullName)
	}
}

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
