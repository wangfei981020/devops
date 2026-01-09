package handlers

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"opsplatform/database"
)

var startTime = time.Now()

// HandleMetricsExport 导出 Prometheus 格式的指标
func HandleMetricsExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// 基础运行时指标
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 输出 Prometheus 格式指标
	fmt.Fprintf(w, "# HELP opsplatform_uptime_seconds 服务运行时间(秒)\n")
	fmt.Fprintf(w, "# TYPE opsplatform_uptime_seconds gauge\n")
	fmt.Fprintf(w, "opsplatform_uptime_seconds %.0f\n\n", time.Since(startTime).Seconds())

	fmt.Fprintf(w, "# HELP opsplatform_goroutines 当前 goroutine 数量\n")
	fmt.Fprintf(w, "# TYPE opsplatform_goroutines gauge\n")
	fmt.Fprintf(w, "opsplatform_goroutines %d\n\n", runtime.NumGoroutine())

	fmt.Fprintf(w, "# HELP opsplatform_memory_alloc_bytes 已分配内存(字节)\n")
	fmt.Fprintf(w, "# TYPE opsplatform_memory_alloc_bytes gauge\n")
	fmt.Fprintf(w, "opsplatform_memory_alloc_bytes %d\n\n", m.Alloc)

	fmt.Fprintf(w, "# HELP opsplatform_memory_sys_bytes 系统内存(字节)\n")
	fmt.Fprintf(w, "# TYPE opsplatform_memory_sys_bytes gauge\n")
	fmt.Fprintf(w, "opsplatform_memory_sys_bytes %d\n\n", m.Sys)

	fmt.Fprintf(w, "# HELP opsplatform_gc_cycles_total GC 次数\n")
	fmt.Fprintf(w, "# TYPE opsplatform_gc_cycles_total counter\n")
	fmt.Fprintf(w, "opsplatform_gc_cycles_total %d\n\n", m.NumGC)

	// 数据库统计
	if database.DB != nil {
		stats := database.DB.Stats()
		fmt.Fprintf(w, "# HELP opsplatform_db_open_connections 数据库打开连接数\n")
		fmt.Fprintf(w, "# TYPE opsplatform_db_open_connections gauge\n")
		fmt.Fprintf(w, "opsplatform_db_open_connections %d\n\n", stats.OpenConnections)

		fmt.Fprintf(w, "# HELP opsplatform_db_in_use 数据库使用中连接数\n")
		fmt.Fprintf(w, "# TYPE opsplatform_db_in_use gauge\n")
		fmt.Fprintf(w, "opsplatform_db_in_use %d\n\n", stats.InUse)

		fmt.Fprintf(w, "# HELP opsplatform_db_idle 数据库空闲连接数\n")
		fmt.Fprintf(w, "# TYPE opsplatform_db_idle gauge\n")
		fmt.Fprintf(w, "opsplatform_db_idle %d\n\n", stats.Idle)
	}

	// 业务指标 - 统计各表记录数
	var recordCount, userCount, domainCount, datasourceCount int

	database.DB.QueryRow("SELECT COUNT(*) FROM records").Scan(&recordCount)
	database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&userCount)
	database.DB.QueryRow("SELECT COUNT(*) FROM domains").Scan(&domainCount)
	database.DB.QueryRow("SELECT COUNT(*) FROM datasources").Scan(&datasourceCount)

	fmt.Fprintf(w, "# HELP opsplatform_records_total 网络管理记录总数\n")
	fmt.Fprintf(w, "# TYPE opsplatform_records_total gauge\n")
	fmt.Fprintf(w, "opsplatform_records_total %d\n\n", recordCount)

	fmt.Fprintf(w, "# HELP opsplatform_users_total 用户总数\n")
	fmt.Fprintf(w, "# TYPE opsplatform_users_total gauge\n")
	fmt.Fprintf(w, "opsplatform_users_total %d\n\n", userCount)

	fmt.Fprintf(w, "# HELP opsplatform_domains_total 域名总数\n")
	fmt.Fprintf(w, "# TYPE opsplatform_domains_total gauge\n")
	fmt.Fprintf(w, "opsplatform_domains_total %d\n\n", domainCount)

	fmt.Fprintf(w, "# HELP opsplatform_datasources_total 数据源总数\n")
	fmt.Fprintf(w, "# TYPE opsplatform_datasources_total gauge\n")
	fmt.Fprintf(w, "opsplatform_datasources_total %d\n\n", datasourceCount)
}



