package handlers

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartHealthServer 在独立端口提供 /health /ready，后续阶段在此挂 /metrics。
func StartHealthServer(addr string, db *sql.DB) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "db down", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.Handle("/metrics", promhttp.Handler())
	log.Printf("Health/Metrics server on %s (/health, /ready, /metrics)", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("health server: %v", err)
	}
}
