package services

import (
	"database/sql"
	"log"
	"strconv"
	"time"
)

type SettingsWatcher struct {
	db      *sql.DB
	scraper *Scraper
	stop    chan struct{}
}

func NewSettingsWatcher(db *sql.DB, scraper *Scraper) *SettingsWatcher {
	return &SettingsWatcher{db: db, scraper: scraper, stop: make(chan struct{})}
}

func (w *SettingsWatcher) Start() {
	go w.loop()
}

func (w *SettingsWatcher) Stop() { close(w.stop) }

func (w *SettingsWatcher) loop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			w.check()
		case <-w.stop:
			return
		}
	}
}

func (w *SettingsWatcher) check() {
	var v string
	err := w.db.QueryRow(`SELECT v FROM settings WHERE k='scrape_interval_minutes'`).Scan(&v)
	if err != nil {
		log.Printf("settings watcher: %v", err)
		return
	}
	minutes, err := strconv.Atoi(v)
	if err != nil || minutes < 5 || minutes > 1440 {
		log.Printf("settings watcher: invalid scrape_interval_minutes=%q", v)
		return
	}
	w.scraper.SetInterval(time.Duration(minutes) * time.Minute)
}
