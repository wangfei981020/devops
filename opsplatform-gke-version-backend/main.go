package main

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"opsplatform-gke-version-backend/config"
	"opsplatform-gke-version-backend/database"
	"opsplatform-gke-version-backend/handlers"
	"opsplatform-gke-version-backend/metrics"
	"opsplatform-gke-version-backend/services"
)

func main() {
	cfg := config.Load()
	db, err := database.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer db.Close()

	metrics.MustRegister()

	interval := 30 * time.Minute
	var minutesStr string
	if err := db.QueryRow(`SELECT v FROM settings WHERE k='scrape_interval_minutes'`).Scan(&minutesStr); err == nil {
		if n, err := strconv.Atoi(minutesStr); err == nil && n >= 5 && n <= 1440 {
			interval = time.Duration(n) * time.Minute
		}
	}
	scraper := services.NewScraper(db, interval)
	scraper.Start()
	defer scraper.Stop()

	sw := services.NewSettingsWatcher(db, scraper)
	sw.Start()
	defer sw.Stop()

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	handlers.RegisterHealth(r)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	api := r.Group("/api")
	handlers.NewClustersHandler(db).Register(api)
	handlers.NewRefreshHandler(db, scraper).Register(api)
	handlers.NewSettingsHandler(db).Register(api)

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}
