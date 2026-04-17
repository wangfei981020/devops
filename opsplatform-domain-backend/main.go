package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"domain-platform/database"
	"domain-platform/handlers"

	"github.com/gorilla/mux"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	// Init database
	database.Init()
	defer database.Close()

	// Start auto sync background goroutine
	handlers.StartAutoSync()

	// Create router
	router := mux.NewRouter()

	// Public routes
	router.HandleFunc("/api/login", handlers.HandleLogin).Methods("POST", "OPTIONS")

	// Protected API routes
	api := router.PathPrefix("/api").Subrouter()
	api.Use(handlers.AuthMiddleware)

	// Registrars
	api.HandleFunc("/registrars", handlers.HandleGetRegistrars).Methods("GET")
	api.HandleFunc("/registrars", handlers.HandleAddRegistrar).Methods("POST")
	api.HandleFunc("/registrars/{id}", handlers.HandleUpdateRegistrar).Methods("PUT")
	api.HandleFunc("/registrars/{id}", handlers.HandleDeleteRegistrar).Methods("DELETE")
	api.HandleFunc("/registrars/{id}/test", handlers.HandleTestRegistrar).Methods("POST")
	api.HandleFunc("/registrars/{id}/sync", handlers.HandleSyncConfig).Methods("POST")

	// Domains - static paths must come before {id} route
	api.HandleFunc("/domains/check-cert", handlers.HandleCheckCert).Methods("GET")
	api.HandleFunc("/domains/check-expiry", handlers.HandleCheckExpiry).Methods("GET")
	api.HandleFunc("/domains/options", handlers.HandleGetDomainOptions).Methods("GET")
	api.HandleFunc("/domains", handlers.HandleGetDomains).Methods("GET")
	api.HandleFunc("/domains", handlers.HandleAddDomain).Methods("POST")
	api.HandleFunc("/domains/{id}", handlers.HandleUpdateDomain).Methods("PUT")
	api.HandleFunc("/domains/{id}", handlers.HandleDeleteDomain).Methods("DELETE")

	// Settings
	api.HandleFunc("/settings", handlers.HandleGetSettings).Methods("GET")
	api.HandleFunc("/settings", handlers.HandleSaveSettings).Methods("POST")

	// Overrides
	api.HandleFunc("/overrides", handlers.HandleGetOverrides).Methods("GET")
	api.HandleFunc("/overrides", handlers.HandleSaveOverride).Methods("POST")
	api.HandleFunc("/overrides/{id}", handlers.HandleDeleteOverride).Methods("DELETE")

	// Apply CORS middleware
	handler := corsMiddleware(router)

	// Metrics / health server
	metricsPort := getEnv("METRICS_PORT", "8088")
	go func() {
		metricsMux := http.NewServeMux()
		metricsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		})
		log.Printf("Metrics server listening on :%s", metricsPort)
		if err := http.ListenAndServe(":"+metricsPort, metricsMux); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	// Main server
	port := getEnv("PORT", "8080")
	log.Printf("Domain platform backend listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
