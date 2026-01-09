package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/google/uuid"
)

// HandleGetMetrics 获取所有指标
func HandleGetMetrics(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, name, label, promql, COALESCE(unit, ''), COALESCE(group_name, ''),
		       COALESCE(description, ''), enabled, sort_order, created_at, COALESCE(created_by, '')
		FROM metrics ORDER BY sort_order, created_at
	`)
	if err != nil {
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	metrics := []models.Metric{}
	for rows.Next() {
		var m models.Metric
		if err := rows.Scan(&m.ID, &m.Name, &m.Label, &m.PromQL, &m.Unit, &m.Group,
			&m.Description, &m.Enabled, &m.SortOrder, &m.CreatedAt, &m.CreatedBy); err != nil {
			continue
		}
		metrics = append(metrics, m)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(metrics)
}

// HandleCreateMetric 创建指标
func HandleCreateMetric(w http.ResponseWriter, r *http.Request) {
	var m models.Metric
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if m.Name == "" || m.Label == "" || m.PromQL == "" {
		http.Error(w, "指标名称、标签和PromQL不能为空", http.StatusBadRequest)
		return
	}

	m.ID = uuid.New().String()
	m.CreatedAt = time.Now().Format("2006-01-02 15:04:05")

	_, err := database.DB.Exec(`
		INSERT INTO metrics (id, name, label, promql, unit, group_name, description, enabled, sort_order, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, m.ID, m.Name, m.Label, m.PromQL, m.Unit, m.Group, m.Description, m.Enabled, m.SortOrder, m.CreatedAt, m.CreatedBy)

	if err != nil {
		http.Error(w, "创建失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	newData, _ := json.Marshal(m)
	AddAuditLog("create", "metric", m.CreatedBy, "", string(newData), "创建指标: "+m.Label, "")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(m)
}

// HandleUpdateMetric 更新指标
func HandleUpdateMetric(w http.ResponseWriter, r *http.Request) {
	var m models.Metric
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	if m.ID == "" {
		http.Error(w, "指标ID不能为空", http.StatusBadRequest)
		return
	}

	_, err := database.DB.Exec(`
		UPDATE metrics SET name=?, label=?, promql=?, unit=?, group_name=?, description=?, enabled=?, sort_order=?
		WHERE id=?
	`, m.Name, m.Label, m.PromQL, m.Unit, m.Group, m.Description, m.Enabled, m.SortOrder, m.ID)

	if err != nil {
		http.Error(w, "更新失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	newData, _ := json.Marshal(m)
	AddAuditLog("update", "metric", operator, "", string(newData), "更新指标: "+m.Label, "")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleDeleteMetric 删除指标
func HandleDeleteMetric(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "指标ID不能为空", http.StatusBadRequest)
		return
	}

	// 获取指标信息用于审计
	var label string
	database.DB.QueryRow("SELECT label FROM metrics WHERE id = ?", id).Scan(&label)

	_, err := database.DB.Exec("DELETE FROM metrics WHERE id = ?", id)
	if err != nil {
		http.Error(w, "删除失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	AddAuditLog("delete", "metric", operator, "", "", "删除指标: "+label, "")

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleInitDefaultMetrics 初始化默认指标
func HandleInitDefaultMetrics(w http.ResponseWriter, r *http.Request) {
	// 检查是否已有指标
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM metrics").Scan(&count)
	if count > 0 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "message": "已存在指标，跳过初始化"})
		return
	}

	// 默认指标
	defaultMetrics := []models.Metric{
		{Name: "pod_running", Label: "☸️ 运行中Pod数", PromQL: "count(kube_pod_status_phase{phase=\"Running\"})", Unit: "个", Group: "k8s", Enabled: true, SortOrder: 1},
		{Name: "pod_available", Label: "☸️ Pod可用率", PromQL: "sum(kube_deployment_status_replicas_available) / sum(kube_deployment_status_replicas) * 100 or vector(100)", Unit: "%", Group: "k8s", Enabled: true, SortOrder: 2},
		{Name: "pod_restart_total", Label: "☸️ Pod重启次数(24h)", PromQL: "sum(increase(kube_pod_container_status_restarts_total[24h])) or vector(0)", Unit: "次", Group: "k8s", Enabled: true, SortOrder: 3},
		{Name: "cluster_cpu_usage", Label: "☸️ 集群CPU使用率", PromQL: "sum(rate(container_cpu_usage_seconds_total[5m])) * 100", Unit: "%", Group: "k8s", Enabled: true, SortOrder: 4},
		{Name: "cluster_memory_usage", Label: "☸️ 集群内存使用率", PromQL: "sum(container_memory_working_set_bytes) / 1024 / 1024 / 1024", Unit: "GB", Group: "k8s", Enabled: true, SortOrder: 5},
		{Name: "deployment_total", Label: "☸️ Deployment总数", PromQL: "count(kube_deployment_created) or vector(0)", Unit: "个", Group: "k8s", Enabled: true, SortOrder: 6},
		{Name: "container_cpu", Label: "📦 容器 CPU", PromQL: "sum(rate(container_cpu_usage_seconds_total[5m])) * 100", Unit: "%", Group: "container", Enabled: true, SortOrder: 10},
		{Name: "container_memory", Label: "📦 容器内存", PromQL: "sum(container_memory_working_set_bytes) / 1024 / 1024 / 1024", Unit: "GB", Group: "container", Enabled: true, SortOrder: 11},
		{Name: "container_restart", Label: "📦 容器重启次数", PromQL: "sum(increase(kube_pod_container_status_restarts_total[24h])) or vector(0)", Unit: "次", Group: "container", Enabled: true, SortOrder: 12},
		{Name: "container_count", Label: "📦 容器数量", PromQL: "count(container_last_seen)", Unit: "个", Group: "container", Enabled: true, SortOrder: 13},
		{Name: "node_cpu", Label: "🖥️ Node CPU", PromQL: "100 - (avg(irate(node_cpu_seconds_total{mode=\"idle\"}[5m])) * 100)", Unit: "%", Group: "node", Enabled: false, SortOrder: 20},
		{Name: "node_memory", Label: "🖥️ Node 内存", PromQL: "(1 - avg(node_memory_MemAvailable_bytes) / avg(node_memory_MemTotal_bytes)) * 100", Unit: "%", Group: "node", Enabled: false, SortOrder: 21},
		{Name: "node_disk", Label: "🖥️ Node 磁盘", PromQL: "(1 - avg(node_filesystem_avail_bytes{mountpoint=\"/\"}) / avg(node_filesystem_size_bytes{mountpoint=\"/\"})) * 100", Unit: "%", Group: "node", Enabled: false, SortOrder: 22},
		{Name: "node_load", Label: "🖥️ Node 负载", PromQL: "avg(node_load1)", Unit: "", Group: "node", Enabled: false, SortOrder: 23},
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	for _, m := range defaultMetrics {
		m.ID = uuid.New().String()
		m.CreatedAt = now
		m.CreatedBy = "system"
		database.DB.Exec(`
			INSERT INTO metrics (id, name, label, promql, unit, group_name, description, enabled, sort_order, created_at, created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, m.ID, m.Name, m.Label, m.PromQL, m.Unit, m.Group, m.Description, m.Enabled, m.SortOrder, m.CreatedAt, m.CreatedBy)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "message": "初始化了 " + string(rune(len(defaultMetrics))) + " 个默认指标"})
}

// GetEnabledMetrics 获取启用的指标（供巡检使用）
func GetEnabledMetrics() ([]models.Metric, error) {
	rows, err := database.DB.Query(`
		SELECT id, name, label, promql, COALESCE(unit, ''), COALESCE(group_name, ''),
		       COALESCE(description, ''), enabled, sort_order, created_at, COALESCE(created_by, '')
		FROM metrics WHERE enabled = 1 ORDER BY sort_order, created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metrics := []models.Metric{}
	for rows.Next() {
		var m models.Metric
		if err := rows.Scan(&m.ID, &m.Name, &m.Label, &m.PromQL, &m.Unit, &m.Group,
			&m.Description, &m.Enabled, &m.SortOrder, &m.CreatedAt, &m.CreatedBy); err != nil {
			continue
		}
		metrics = append(metrics, m)
	}
	return metrics, nil
}

