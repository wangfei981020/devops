package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"opsplatform-deploy-vm-agent/internal/agentlog"
	"opsplatform-deploy-vm-agent/internal/config"
	"opsplatform-deploy-vm-agent/internal/httpserver"
	"opsplatform-deploy-vm-agent/internal/tasks"
)

const Version = "v6"

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

	// 日志双写：stdout (→ systemd journal) + 按小时切的本地文件（如果配了 log_dir）
	//   stdout 留着是因为 systemctl status / journalctl 还能用，便于即时排查
	//   文件按 <log_dir>/YYYY-MM-DD/YYYY-MM-DD-HH-00.log 滚动，按 log_retention_days 清理
	var logCloser io.Closer
	if cfg.LogDir != "" {
		hw := agentlog.NewHourlyWriter(cfg.LogDir)
		log.SetOutput(io.MultiWriter(os.Stdout, hw))
		logCloser = hw
		agentlog.StartCleaner(cfg.LogDir, cfg.LogRetentionDays)
	}

	log.Printf("[agent] %s starting", Version)
	log.Printf("[agent] ansible_root=%s version_root=%s max_concurrent=%d listen=%s tls=%v",
		cfg.AnsibleRoot, cfg.VersionRoot, cfg.MaxConcurrent, cfg.Listen, cfg.IsTLS())
	if cfg.LogDir != "" {
		log.Printf("[agent] log_dir=%s retention=%d days (hourly rotation, daily subdirs)",
			cfg.LogDir, cfg.LogRetentionDays)
	}

	runner := &tasks.AnsibleRunner{AnsibleRoot: cfg.AnsibleRoot}
	mgr := tasks.NewManager(cfg.MaxConcurrent, time.Duration(cfg.TaskRetention)*time.Hour, runner)
	srv := httpserver.New(cfg, mgr, runner)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Printf("[agent] shutdown signal received")
		cancel()
	}()

	if err := srv.ListenAndServe(ctx); err != nil {
		// 退出前 close 一下小时文件 writer，让 fsync 刷盘
		if logCloser != nil {
			_ = logCloser.Close()
		}
		log.Fatalf("[agent] server: %v", err)
	}
	if logCloser != nil {
		_ = logCloser.Close()
	}
	log.Printf("[agent] exited cleanly")
}
