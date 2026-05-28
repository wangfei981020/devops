package handlers

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartHealthServer 在独立端口启动 health + metrics server，
// 与发布系统 deploy-backend 对齐：/health /ready /metrics 都在 8088。
//
// 拆开的好处：业务 hang 死时 K8s 还能拿到 health 信号；
// 也避免业务端口暴露 /metrics 给外部抓。
func StartHealthServer(addr string, db *sql.DB) {
	m := http.NewServeMux()
	m.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"gke-version-backend"}`))
	})
	m.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"db unreachable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	m.Handle("/metrics", promhttp.Handler())
	log.Printf("Health/Metrics server on %s (/health, /ready, /metrics)", addr)
	if err := http.ListenAndServe(addr, m); err != nil {
		log.Printf("health server: %v", err)
	}
}
