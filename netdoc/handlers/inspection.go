package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"netdoc/database"
	"netdoc/models"
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

// 预定义的 PromQL 查询
var metricQueries = map[string]struct {
	Query string
	Unit  string
}{
	"cpu":     {"100 - (avg(irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)", "%"},
	"memory":  {"(1 - avg(node_memory_MemAvailable_bytes) / avg(node_memory_MemTotal_bytes)) * 100", "%"},
	"disk":    {"(1 - avg(node_filesystem_avail_bytes{mountpoint=\"/\"}) / avg(node_filesystem_size_bytes{mountpoint=\"/\"})) * 100", "%"},
	"network": {"sum(irate(node_network_receive_bytes_total[5m])) / 1024 / 1024", "MB/s"},
	"uptime":  {"(avg(node_time_seconds) - avg(node_boot_time_seconds)) / 86400", "天"},
	"load":    {"avg(node_load1)", ""},
}

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
			metricDef, ok := metricQueries[metricKey]
			if !ok {
				continue
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





