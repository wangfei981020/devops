package main

import (
	"log"
	"net/http"

	"opsplatform-deploy-backend/config"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/database/migrations"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Deploy Center backend starting...")

	cfg := config.Load()
	log.Printf("config: Port=%s HealthPort=%s GitCacheDir=%s", cfg.Port, cfg.HealthPort, cfg.GitCacheDir)

	if err := database.InitMySQL(cfg); err != nil {
		log.Fatalf("init mysql: %v", err)
	}
	defer database.Close()

	if err := migrations.Run(database.DB); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"deploy-backend"}`))
	})

	log.Printf("listening on %s", cfg.Port)
	if err := http.ListenAndServe(cfg.Port, nil); err != nil {
		log.Fatalf("server: %v", err)
	}
}
