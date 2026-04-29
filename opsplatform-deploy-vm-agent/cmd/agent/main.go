package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"opsplatform-deploy-vm-agent/internal/config"
	"opsplatform-deploy-vm-agent/internal/httpserver"
	"opsplatform-deploy-vm-agent/internal/tasks"
)

const Version = "v1"

func main() {
	configPath := flag.String("config", "/etc/deploy-vm-agent/config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		log.Printf("deploy-vm-agent %s", Version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[agent] load config: %v", err)
	}
	log.Printf("[agent] %s starting", Version)
	log.Printf("[agent] ansible_root=%s version_root=%s max_concurrent=%d listen=%s tls=%v",
		cfg.AnsibleRoot, cfg.VersionRoot, cfg.MaxConcurrent, cfg.Listen, cfg.IsTLS())

	mgr := tasks.NewManager(cfg.MaxConcurrent, time.Duration(cfg.TaskRetention)*time.Hour)
	srv := httpserver.New(cfg, mgr)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Printf("[agent] shutdown signal received")
		cancel()
	}()

	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatalf("[agent] server: %v", err)
	}
	log.Printf("[agent] exited cleanly")
}
