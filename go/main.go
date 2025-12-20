package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// 阈值配置
var thresholds = []float64{90, 80, 70, 60, 50, 40, 30}

// DataSource 数据源配置
type DataSource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	IsActive    bool   `json:"is_active"`
}

// DataSourceManager 数据源管理器
type DataSourceManager struct {
	sources    map[string]*DataSource
	activeID   string
	configFile string
	mu         sync.RWMutex
}

// NewDataSourceManager 创建数据源管理器
func NewDataSourceManager(configFile string) *DataSourceManager {
	dm := &DataSourceManager{
		sources:    make(map[string]*DataSource),
		configFile: configFile,
	}
	dm.loadConfig()
	return dm
}

// loadConfig 从文件加载配置
func (dm *DataSourceManager) loadConfig() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, err := os.Stat(dm.configFile); os.IsNotExist(err) {
		// 如果配置文件不存在，创建默认数据源
		defaultURL := getEnv("PROMETHEUS_URL", "http://localhost:9090")
		defaultID := "default"
		dm.sources[defaultID] = &DataSource{
			ID:        defaultID,
			Name:      "默认 Prometheus",
			URL:       defaultURL,
			CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
			UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
			IsActive:  true,
		}
		dm.activeID = defaultID
		dm.saveConfig()
		return
	}

	data, err := os.ReadFile(dm.configFile)
	if err != nil {
		log.Printf("读取配置文件失败: %v", err)
		return
	}

	var config struct {
		Sources  map[string]*DataSource `json:"sources"`
		ActiveID string                 `json:"active_id"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("解析配置文件失败: %v", err)
		return
	}

	dm.sources = config.Sources
	dm.activeID = config.ActiveID

	// 如果没有活跃的数据源，设置第一个为活跃
	if dm.activeID == "" && len(dm.sources) > 0 {
		for id := range dm.sources {
			dm.activeID = id
			dm.sources[id].IsActive = true
			break
		}
	}
}

// saveConfig 保存配置到文件
func (dm *DataSourceManager) saveConfig() error {
	config := struct {
		Sources  map[string]*DataSource `json:"sources"`
		ActiveID string                 `json:"active_id"`
	}{
		Sources:  dm.sources,
		ActiveID: dm.activeID,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(dm.configFile, data, 0644)
}

// GetAllSources 获取所有数据源
func (dm *DataSourceManager) GetAllSources() []*DataSource {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	sources := make([]*DataSource, 0, len(dm.sources))
	for _, source := range dm.sources {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].CreatedAt < sources[j].CreatedAt
	})
	return sources
}

// GetActiveSource 获取当前活跃的数据源
func (dm *DataSourceManager) GetActiveSource() *DataSource {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	if dm.activeID == "" {
		return nil
	}
	return dm.sources[dm.activeID]
}

// AddSource 添加数据源
func (dm *DataSourceManager) AddSource(name, url, description string) (*DataSource, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// 检查 URL 是否已存在
	for _, source := range dm.sources {
		if source.URL == url {
			return nil, fmt.Errorf("该 URL 已存在: %s", url)
		}
	}

	id := fmt.Sprintf("ds_%d", time.Now().Unix())
	source := &DataSource{
		ID:          id,
		Name:        name,
		URL:         url,
		Description: description,
		CreatedAt:   time.Now().Format("2006-01-02 15:04:05"),
		UpdatedAt:   time.Now().Format("2006-01-02 15:04:05"),
		IsActive:    false,
	}

	dm.sources[id] = source
	if err := dm.saveConfig(); err != nil {
		delete(dm.sources, id)
		return nil, err
	}

	return source, nil
}

// UpdateSource 更新数据源
func (dm *DataSourceManager) UpdateSource(id, name, url, description string) (*DataSource, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	source, exists := dm.sources[id]
	if !exists {
		return nil, fmt.Errorf("数据源不存在: %s", id)
	}

	// 检查 URL 是否被其他数据源使用
	for sid, s := range dm.sources {
		if sid != id && s.URL == url {
			return nil, fmt.Errorf("该 URL 已被其他数据源使用: %s", url)
		}
	}

	source.Name = name
	source.URL = url
	source.Description = description
	source.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")

	if err := dm.saveConfig(); err != nil {
		return nil, err
	}

	return source, nil
}

// DeleteSource 删除数据源
func (dm *DataSourceManager) DeleteSource(id string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, exists := dm.sources[id]; !exists {
		return fmt.Errorf("数据源不存在: %s", id)
	}

	if dm.activeID == id {
		return fmt.Errorf("无法删除当前活跃的数据源，请先切换到其他数据源")
	}

	delete(dm.sources, id)
	return dm.saveConfig()
}

// SetActiveSource 设置活跃数据源
func (dm *DataSourceManager) SetActiveSource(id string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	source, exists := dm.sources[id]
	if !exists {
		return fmt.Errorf("数据源不存在: %s", id)
	}

	// 取消之前的活跃状态
	if dm.activeID != "" {
		if oldSource, ok := dm.sources[dm.activeID]; ok {
			oldSource.IsActive = false
		}
	}

	// 设置新的活跃状态
	dm.activeID = id
	source.IsActive = true
	source.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")

	return dm.saveConfig()
}

// TestConnection 测试连接
func (dm *DataSourceManager) TestConnection(url string) error {
	client, err := NewPrometheusClient(url)
	if err != nil {
		return fmt.Errorf("连接失败: %v", err)
	}

	// 尝试查询一个简单的指标来测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err = client.client.Query(ctx, "up", time.Now())
	if err != nil {
		return fmt.Errorf("查询测试失败: %v", err)
	}

	return nil
}

// MetricResult 表示单个指标结果
type MetricResult struct {
	Instance   string  `json:"instance"`
	Job        string  `json:"job,omitempty"`
	Value      float64 `json:"value"`
	Threshold  float64 `json:"threshold"`
	MetricType string  `json:"metric_type"`
}

// InspectionResult 表示巡检结果
type InspectionResult struct {
	Timestamp  string                    `json:"timestamp"`
	CPU        map[string][]MetricResult `json:"cpu"`
	Memory     map[string][]MetricResult `json:"memory"`
	Disk       map[string][]MetricResult `json:"disk"`
	Summary    Summary                   `json:"summary"`
	DataSource string                    `json:"data_source"`
}

// Summary 汇总信息
type Summary struct {
	TotalInstances int `json:"total_instances"`
	CPUCount       int `json:"cpu_count"`
	MemoryCount    int `json:"memory_count"`
	DiskCount      int `json:"disk_count"`
}

// PrometheusClient Prometheus 客户端
type PrometheusClient struct {
	client v1.API
	url    string
}

// NewPrometheusClient 创建新的 Prometheus 客户端
func NewPrometheusClient(prometheusURL string) (*PrometheusClient, error) {
	cfg := api.Config{
		Address: prometheusURL,
	}
	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &PrometheusClient{
		client: v1.NewAPI(client),
		url:    prometheusURL,
	}, nil
}

// QueryMetric 查询指标
func (pc *PrometheusClient) QueryMetric(query string) ([]MetricResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, warnings, err := pc.client.Query(ctx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("查询失败: %v", err)
	}

	if len(warnings) > 0 {
		log.Printf("警告: %v", warnings)
	}

	var metrics []MetricResult
	if result.Type() == model.ValVector {
		vector := result.(model.Vector)
		for _, sample := range vector {
			instance := ""
			job := ""

			if val, ok := sample.Metric["instance"]; ok {
				instance = string(val)
			}
			if val, ok := sample.Metric["job"]; ok {
				job = string(val)
			}
			if instance == "" {
				// 尝试从其他标签获取
				if val, ok := sample.Metric["__name__"]; ok {
					instance = string(val)
				}
			}

			metrics = append(metrics, MetricResult{
				Instance: instance,
				Job:      job,
				Value:    float64(sample.Value),
			})
		}
	}

	return metrics, nil
}

// CheckThresholds 检查阈值
func CheckThresholds(metrics []MetricResult, metricType string) map[string][]MetricResult {
	result := make(map[string][]MetricResult)

	for i := range metrics {
		metrics[i].MetricType = metricType
		for _, threshold := range thresholds {
			if metrics[i].Value >= threshold {
				key := fmt.Sprintf("%.0f%%", threshold)
				metricCopy := metrics[i]
				metricCopy.Threshold = threshold
				result[key] = append(result[key], metricCopy)
			}
		}
	}

	// 对每个阈值组按值排序
	for key := range result {
		sort.Slice(result[key], func(i, j int) bool {
			return result[key][i].Value > result[key][j].Value
		})
	}

	return result
}

// Inspect 执行巡检
func (pc *PrometheusClient) Inspect() (*InspectionResult, error) {
	// CPU 使用率查询
	cpuQuery := `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`
	cpuQueryAlt := `100 - (avg(irate(node_cpu_seconds_total{mode="idle"}[5m])) by (instance) * 100)`

	// 内存使用率查询
	memoryQuery := `100 * (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes))`
	memoryQueryAlt := `(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) / node_memory_MemTotal_bytes * 100`

	// 磁盘使用率查询
	diskQuery := `100 * (1 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}))`
	diskQueryAlt := `(node_filesystem_size_bytes{mountpoint="/"} - node_filesystem_avail_bytes{mountpoint="/"}) / node_filesystem_size_bytes{mountpoint="/"} * 100`

	var cpuMetrics, memoryMetrics, diskMetrics []MetricResult
	var err error

	// 尝试查询 CPU
	cpuMetrics, err = pc.QueryMetric(cpuQuery)
	if err != nil || len(cpuMetrics) == 0 {
		cpuMetrics, _ = pc.QueryMetric(cpuQueryAlt)
	}

	// 尝试查询内存
	memoryMetrics, err = pc.QueryMetric(memoryQuery)
	if err != nil || len(memoryMetrics) == 0 {
		memoryMetrics, _ = pc.QueryMetric(memoryQueryAlt)
	}

	// 尝试查询磁盘
	diskMetrics, err = pc.QueryMetric(diskQuery)
	if err != nil || len(diskMetrics) == 0 {
		diskMetrics, _ = pc.QueryMetric(diskQueryAlt)
	}

	// 检查阈值
	cpuResults := CheckThresholds(cpuMetrics, "CPU")
	memoryResults := CheckThresholds(memoryMetrics, "Memory")
	diskResults := CheckThresholds(diskMetrics, "Disk")

	// 计算汇总
	summary := Summary{
		TotalInstances: len(cpuMetrics),
		CPUCount:       len(cpuMetrics),
		MemoryCount:    len(memoryMetrics),
		DiskCount:      len(diskMetrics),
	}

	return &InspectionResult{
		Timestamp:  time.Now().Format("2006-01-02 15:04:05"),
		CPU:        cpuResults,
		Memory:     memoryResults,
		Disk:       diskResults,
		Summary:    summary,
		DataSource: pc.url,
	}, nil
}

// 全局变量
var (
	dsManager  *DataSourceManager
	promClient *PrometheusClient
	clientMu   sync.RWMutex
)

// updatePrometheusClient 更新 Prometheus 客户端
func updatePrometheusClient() error {
	clientMu.Lock()
	defer clientMu.Unlock()

	activeSource := dsManager.GetActiveSource()
	if activeSource == nil {
		return fmt.Errorf("没有活跃的数据源")
	}

	client, err := NewPrometheusClient(activeSource.URL)
	if err != nil {
		return fmt.Errorf("无法连接到 Prometheus: %v", err)
	}

	promClient = client
	return nil
}

// API 处理器

// handleInspect 处理巡检请求
func handleInspect(w http.ResponseWriter, r *http.Request) {
	clientMu.RLock()
	client := promClient
	clientMu.RUnlock()

	if client == nil {
		http.Error(w, "Prometheus 客户端未初始化，请先配置数据源", http.StatusInternalServerError)
		return
	}

	result, err := client.Inspect()
	if err != nil {
		http.Error(w, fmt.Sprintf("巡检失败: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(result)
}

// handleGetDataSources 获取所有数据源
func handleGetDataSources(w http.ResponseWriter, r *http.Request) {
	sources := dsManager.GetAllSources()
	activeSource := dsManager.GetActiveSource()

	response := map[string]interface{}{
		"sources":       sources,
		"active_id":     dsManager.activeID,
		"active_source": activeSource,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(response)
}

// handleAddDataSource 添加数据源
func handleAddDataSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("无效的请求: %v", err), http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.URL == "" {
		http.Error(w, "名称和 URL 不能为空", http.StatusBadRequest)
		return
	}

	source, err := dsManager.AddSource(req.Name, req.URL, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(source)
}

// handleUpdateDataSource 更新数据源
func handleUpdateDataSource(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Name        string `json:"name"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("无效的请求: %v", err), http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.URL == "" {
		http.Error(w, "名称和 URL 不能为空", http.StatusBadRequest)
		return
	}

	source, err := dsManager.UpdateSource(id, req.Name, req.URL, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(source)
}

// handleDeleteDataSource 删除数据源
func handleDeleteDataSource(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := dsManager.DeleteSource(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "删除成功"})
}

// handleSetActiveDataSource 设置活跃数据源
func handleSetActiveDataSource(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := dsManager.SetActiveSource(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 更新 Prometheus 客户端
	if err := updatePrometheusClient(); err != nil {
		http.Error(w, fmt.Sprintf("切换数据源失败: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "切换成功"})
}

// handleTestConnection 测试连接
func handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("无效的请求: %v", err), http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, "URL 不能为空", http.StatusBadRequest)
		return
	}

	if err := dsManager.TestConnection(req.URL); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"message": "连接成功"})
}

// handleHealth 健康检查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	// 初始化数据源管理器
	configFile := getEnv("CONFIG_FILE", "./datasources.json")
	dsManager = NewDataSourceManager(configFile)

	// 初始化 Prometheus 客户端
	if err := updatePrometheusClient(); err != nil {
		log.Printf("警告: %v", err)
	}

	port := getEnv("PORT", "8080")

	r := mux.NewRouter()

	// API 路由
	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/inspect", handleInspect).Methods("GET")
	api.HandleFunc("/health", handleHealth).Methods("GET")
	api.HandleFunc("/datasources", handleGetDataSources).Methods("GET")
	api.HandleFunc("/datasources", handleAddDataSource).Methods("POST")
	api.HandleFunc("/datasources/{id}", handleUpdateDataSource).Methods("PUT")
	api.HandleFunc("/datasources/{id}", handleDeleteDataSource).Methods("DELETE")
	api.HandleFunc("/datasources/{id}/activate", handleSetActiveDataSource).Methods("POST")
	api.HandleFunc("/datasources/test", handleTestConnection).Methods("POST")

	// 静态文件
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	log.Printf("服务器启动在端口 %s", port)
	log.Printf("配置文件: %s", configFile)
	if activeSource := dsManager.GetActiveSource(); activeSource != nil {
		log.Printf("当前活跃数据源: %s (%s)", activeSource.Name, activeSource.URL)
	}
	log.Printf("访问 http://localhost:%s 查看前端页面", port)

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
