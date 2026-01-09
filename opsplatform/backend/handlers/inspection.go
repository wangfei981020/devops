package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"opsplatform/database"
	"opsplatform/models"
)

// InspectionRequest 巡检请求
type InspectionRequest struct {
	DataSourceIDs []string `json:"datasource_ids"`
	ReportType    string   `json:"report_type"` // realtime, daily, weekly
	Metrics       []string `json:"metrics"`
	Operator      string   `json:"operator"`
}

// InspectionResult 巡检结果
type InspectionResult struct {
	DataSourceID   string         `json:"datasource_id"`
	DataSourceName string         `json:"datasource_name"`
	Time           string         `json:"time"`
	Metrics        []MetricResult `json:"metrics"`
	Error          string         `json:"error,omitempty"`
}

// MetricResult 指标结果
type MetricResult struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// MetricDef 指标定义
type MetricDef struct {
	Query string
	Unit  string
}

// Node 级别指标（需要 node_exporter）
var nodeMetricQueries = map[string]MetricDef{
	"node_cpu":     {"100 - (avg(irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)", "%"},
	"node_memory":  {"(1 - avg(node_memory_MemAvailable_bytes) / avg(node_memory_MemTotal_bytes)) * 100", "%"},
	"node_disk":    {"(1 - avg(node_filesystem_avail_bytes{mountpoint=\"/\"}) / avg(node_filesystem_size_bytes{mountpoint=\"/\"})) * 100", "%"},
	"node_network": {"sum(irate(node_network_receive_bytes_total[5m])) / 1024 / 1024", "MB/s"},
	"node_uptime":  {"(avg(node_time_seconds) - avg(node_boot_time_seconds)) / 86400", "天"},
	"node_load":    {"avg(node_load1)", ""},
}

// 容器级别指标（Kubernetes 环境）
var containerMetricQueries = map[string]MetricDef{
	"container_cpu":     {"sum(rate(container_cpu_usage_seconds_total[5m])) * 100", "%"},
	"container_memory":  {"sum(container_memory_working_set_bytes) / 1024 / 1024 / 1024", "GB"},
	"container_disk":    {"sum(container_fs_usage_bytes) / 1024 / 1024 / 1024", "GB"},
	"container_network": {"sum(rate(container_network_receive_bytes_total[5m])) / 1024 / 1024", "MB/s"},
	"container_restart": {"sum(increase(kube_pod_container_status_restarts_total[24h])) or sum(increase(container_start_time_seconds[24h]))", "次"},
	"container_count":   {"count(container_last_seen)", "个"},
}

// K8s 集群级别指标（给老板看的核心指标）
var k8sMetricQueries = map[string]MetricDef{
	// Pod 健康状态
	"pod_running":       {"count(kube_pod_status_phase{phase=\"Running\"})", "个"},
	"pod_pending":       {"count(kube_pod_status_phase{phase=\"Pending\"}) or vector(0)", "个"},
	"pod_failed":        {"count(kube_pod_status_phase{phase=\"Failed\"}) or vector(0)", "个"},
	"pod_available":     {"sum(kube_deployment_status_replicas_available) / sum(kube_deployment_status_replicas) * 100 or vector(100)", "%"},
	"pod_restart_total": {"sum(increase(kube_pod_container_status_restarts_total[24h])) or vector(0)", "次"},

	// 资源使用率
	"cluster_cpu_usage":    {"sum(rate(container_cpu_usage_seconds_total[5m])) / sum(machine_cpu_cores) * 100 or sum(rate(container_cpu_usage_seconds_total[5m])) * 100", "%"},
	"cluster_memory_usage": {"sum(container_memory_working_set_bytes) / sum(machine_memory_bytes) * 100 or sum(container_memory_working_set_bytes) / 1024 / 1024 / 1024", "%"},

	// 部署状态
	"deployment_total":     {"count(kube_deployment_created) or vector(0)", "个"},
	"deployment_available": {"sum(kube_deployment_status_replicas_available) or vector(0)", "个"},

	// API 服务健康（如果有 ingress-nginx）
	"api_request_rate": {"sum(rate(nginx_ingress_controller_requests[5m])) or vector(0)", "req/s"},
	"api_success_rate": {"sum(rate(nginx_ingress_controller_requests{status=~\"2..\"}[5m])) / sum(rate(nginx_ingress_controller_requests[5m])) * 100 or vector(100)", "%"},
	"api_latency_avg":  {"avg(nginx_ingress_controller_request_duration_seconds_sum / nginx_ingress_controller_request_duration_seconds_count) * 1000 or vector(0)", "ms"},
}

// 合并所有指标
var metricQueries = func() map[string]MetricDef {
	m := make(map[string]MetricDef)
	for k, v := range nodeMetricQueries {
		m[k] = v
	}
	for k, v := range containerMetricQueries {
		m[k] = v
	}
	for k, v := range k8sMetricQueries {
		m[k] = v
	}
	// 兼容旧的指标名（默认使用容器指标）
	m["cpu"] = containerMetricQueries["container_cpu"]
	m["memory"] = containerMetricQueries["container_memory"]
	m["disk"] = containerMetricQueries["container_disk"]
	m["network"] = containerMetricQueries["container_network"]
	m["uptime"] = MetricDef{"(time() - min(container_start_time_seconds{container!=\"\"})) / 86400", "天"}
	m["load"] = MetricDef{"sum(rate(container_cpu_usage_seconds_total{container!=\"\",container!=\"POD\"}[5m]))", "核"}
	return m
}()

// HandleExecuteInspection 执行巡检
func HandleExecuteInspection(w http.ResponseWriter, r *http.Request) {
	var req InspectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if len(req.DataSourceIDs) == 0 {
		http.Error(w, "请选择至少一个数据源", http.StatusBadRequest)
		return
	}

	if len(req.Metrics) == 0 {
		http.Error(w, "请选择至少一个巡检指标", http.StatusBadRequest)
		return
	}

	// 从数据库加载自定义指标
	dbMetrics, _ := GetEnabledMetrics()
	customMetricMap := make(map[string]MetricDef)
	for _, m := range dbMetrics {
		customMetricMap[m.Name] = MetricDef{Query: m.PromQL, Unit: m.Unit}
	}

	results := make([]InspectionResult, 0, len(req.DataSourceIDs))

	for _, dsID := range req.DataSourceIDs {
		ds, err := GetDataSourceByID(dsID)
		if err != nil || ds == nil {
			results = append(results, InspectionResult{
				DataSourceID:   dsID,
				DataSourceName: "未知",
				Time:           timeNow().Format("2006-01-02 15:04:05"),
				Error:          "数据源不存在",
			})
			continue
		}

		result := InspectionResult{
			DataSourceID:   ds.ID,
			DataSourceName: ds.Name,
			Time:           timeNow().Format("2006-01-02 15:04:05"),
			Metrics:        make([]MetricResult, 0, len(req.Metrics)),
		}

		// 根据报告类型确定时间范围
		timeRange := getTimeRange(req.ReportType)

		for _, metricKey := range req.Metrics {
			// 优先从数据库自定义指标查找，否则使用内置指标
			metricDef, ok := customMetricMap[metricKey]
			if !ok {
				metricDef, ok = metricQueries[metricKey]
				if !ok {
					continue
				}
			}

			value, err := queryPrometheus(ds, metricDef.Query, timeRange)
			if err != nil {
				result.Metrics = append(result.Metrics, MetricResult{
					Name:  metricKey,
					Value: -1,
					Unit:  metricDef.Unit,
				})
				continue
			}

			result.Metrics = append(result.Metrics, MetricResult{
				Name:  metricKey,
				Value: value,
				Unit:  metricDef.Unit,
			})
		}

		results = append(results, result)
	}

	// 保存巡检记录
	saveInspectionLog(req.Operator, req.DataSourceIDs, req.Metrics, req.ReportType)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"total":   len(results),
		"results": results,
	})
}

func getTimeRange(reportType string) string {
	switch reportType {
	case "daily":
		return "24h"
	case "weekly":
		return "7d"
	default:
		return "5m"
	}
}

func queryPrometheus(ds *models.DataSource, query, timeRange string) (float64, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// 构建查询 URL
	queryURL := fmt.Sprintf("%s/api/v1/query", ds.URL)
	params := url.Values{}

	// 如果是范围查询，使用 query_range 并计算平均值
	if timeRange != "5m" {
		query = fmt.Sprintf("avg_over_time((%s)[%s:])", query, timeRange)
	}

	params.Set("query", query)

	req, err := http.NewRequest("GET", queryURL+"?"+params.Encode(), nil)
	if err != nil {
		return 0, err
	}

	// 添加认证
	if ds.Token != "" {
		req.Header.Set("Authorization", "Bearer "+ds.Token)
	} else if ds.Username != "" && ds.Password != "" {
		req.SetBasicAuth(ds.Username, ds.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus 返回错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	// 解析 Prometheus 响应
	var promResp struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []interface{} `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &promResp); err != nil {
		return 0, err
	}

	if promResp.Status != "success" || len(promResp.Data.Result) == 0 {
		return 0, fmt.Errorf("无数据")
	}

	// 获取值
	if len(promResp.Data.Result[0].Value) < 2 {
		return 0, fmt.Errorf("数据格式错误")
	}

	valueStr, ok := promResp.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("值类型错误")
	}

	var value float64
	fmt.Sscanf(valueStr, "%f", &value)

	return value, nil
}

func GetDataSourceByID(id string) (*models.DataSource, error) {
	s := &models.DataSource{}
	err := database.DB.QueryRow(`
		SELECT id, name, type, url, COALESCE(username, ''), COALESCE(password, ''),
		       COALESCE(token, ''), COALESCE(description, ''), status, created_at, COALESCE(created_by, '')
		FROM datasources WHERE id = ?
	`, id).Scan(&s.ID, &s.Name, &s.Type, &s.URL, &s.Username, &s.Password,
		&s.Token, &s.Description, &s.Status, &s.CreatedAt, &s.CreatedBy)
	if err != nil {
		return nil, nil
	}
	return s, nil
}

func saveInspectionLog(operator string, dsIDs, metrics []string, reportType string) {
	changes := fmt.Sprintf("执行巡检: 数据源=%d个, 指标=%d项, 类型=%s",
		len(dsIDs), len(metrics), reportType)

	dsIDsJSON, _ := json.Marshal(dsIDs)
	metricsJSON, _ := json.Marshal(metrics)
	newData := fmt.Sprintf(`{"datasource_ids":%s,"metrics":%s,"report_type":"%s"}`,
		string(dsIDsJSON), string(metricsJSON), reportType)

	AddAuditLog("create", "inspection", operator, "", newData, changes, "")
}
