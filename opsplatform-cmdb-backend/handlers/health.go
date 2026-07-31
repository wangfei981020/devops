package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"opsplatform-cmdb-backend/logx"
)

// StartHealthServer 在独立端口提供 /health /ready，后续阶段在此挂 /metrics。
func StartHealthServer(addr string, db *sql.DB) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// /ready：就绪探针用，检查 DB 真的可查。
	//
	// 必须带超时：裸 db.Ping() 会去连接池要连接，池子被挂死的查询占满时（CMDB-012）
	// 它自己也一起无限期挂住——探针既不成功也不失败，K8s 只能等到 timeoutSeconds
	// 才判失败，而"为什么失败"完全没有信息。带 2 秒超时后，DB 不可用会被明确、
	// 快速地判定出来，流量随即被摘掉。
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			logx.Line("health", fmt.Sprintf("readiness 失败: %v", err))
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("/metrics", promhttp.Handler())
	logx.Line("health", fmt.Sprintf("Health/Metrics server on %s (/health, /ready, /metrics)", addr))
	if err := http.ListenAndServe(addr, mux); err != nil {
		logx.Line("health", fmt.Sprintf("health server: %v", err))
	}
}
