package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// Config 全部来自环境变量 + 挂载的 config.ini
type Config struct {
	ConfigFile     string
	OutputDir      string
	Ordinal        int
	TotalShards    int
	Mode           string
	FPS            string
	JpegQuality    int
	SkipNonKey     bool
	StartStagger   time.Duration
	SnapshotEvery  time.Duration
	SnapshotToMax  int
	SnapshotToWait time.Duration
	ReconcileEvery time.Duration
	UploadEvery    time.Duration
	UploadSettle   time.Duration
	HealthEvery    time.Duration
	StaleAfter     time.Duration
	GraceAfter     time.Duration
	CleanupEvery   time.Duration
	MetricsAddr    string
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string
	MinioBucket    string
	MinioUseSSL    bool
}

func loadConfig() Config {
	host, _ := os.Hostname()
	if pn := os.Getenv("POD_NAME"); pn != "" {
		host = pn
	}
	return Config{
		ConfigFile:     env("CONFIG_FILE", "/etc/video-images/config.ini"),
		OutputDir:      env("OUTPUT_DIR", "/data/images"),
		Ordinal:        OrdinalFromHostname(host),
		TotalShards:    envInt("TOTAL_SHARDS", 1),
		Mode:           env("MODE", "persistent"),
		FPS:            env("FFMPEG_FPS", "1/10"),
		JpegQuality:    envInt("JPEG_QUALITY", 5),
		SkipNonKey:     envBool("SKIP_NONKEY", true),
		StartStagger:   time.Duration(envInt("START_STAGGER_MS", 150)) * time.Millisecond,
		SnapshotEvery:  time.Duration(envInt("SNAPSHOT_SECONDS", 30)) * time.Second,
		SnapshotToMax:  envInt("SNAPSHOT_MAX_CONCURRENCY", 8),
		SnapshotToWait: time.Duration(envInt("SNAPSHOT_TIMEOUT_SECONDS", 15)) * time.Second,
		ReconcileEvery: time.Duration(envInt("RECONCILE_SECONDS", 5)) * time.Second,
		UploadEvery:    time.Duration(envInt("UPLOAD_SECONDS", 2)) * time.Second,
		UploadSettle:   time.Duration(envInt("UPLOAD_SETTLE_SECONDS", 2)) * time.Second,
		HealthEvery:    time.Duration(envInt("HEALTH_SECONDS", 30)) * time.Second,
		StaleAfter:     time.Duration(envInt("STALE_SECONDS", 90)) * time.Second,
		GraceAfter:     time.Duration(envInt("GRACE_SECONDS", 60)) * time.Second,
		CleanupEvery:   time.Duration(envInt("CLEANUP_SECONDS", 300)) * time.Second,
		MetricsAddr:    env("METRICS_ADDR", ":8088"),
		MinioEndpoint:  env("MINIO_ENDPOINT", "devops-minio:9000"),
		MinioAccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey: os.Getenv("MINIO_SECRET_KEY"),
		MinioBucket:    env("MINIO_BUCKET", "video-images"),
		MinioUseSSL:    envBool("MINIO_USE_SSL", false),
	}
}

type reconciler interface {
	Reconcile(owned []Stream)
}

type snapshotAdapter struct{ s *Snapshotter }

func (a snapshotAdapter) Reconcile(owned []Stream) { a.s.SetOwned(owned) }

// state 保存对账产出的共享数据(健康检查、清理器读)
type state struct {
	mu       sync.Mutex
	owned    []Stream
	allNames map[string]bool // 全部 "filename.jpg"(含其它分片的),给孤儿清理判据
}

func (s *state) set(owned []Stream, allNames map[string]bool) {
	s.mu.Lock()
	s.owned, s.allNames = owned, allNames
	s.mu.Unlock()
}
func (s *state) getOwned() []Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owned
}
func (s *state) getAllNames() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allNames
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[video-images-generator] ")

	cfg := loadConfig()
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		log.Fatalf("创建输出目录失败: %v", err)
	}
	log.Printf("INFO 启动 分片=%d/%d 模式=%s 输出=%s MinIO=%s 桶=%s fps=%s q=%d",
		cfg.Ordinal, cfg.TotalShards, cfg.Mode, cfg.OutputDir, cfg.MinioEndpoint, cfg.MinioBucket, cfg.FPS, cfg.JpegQuality)

	metrics := NewMetrics(cfg.Ordinal, cfg.TotalShards)
	mc, err := NewMinioClient(cfg)
	if err != nil {
		log.Fatalf("初始化 MinIO 失败: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go NewUploader(mc, cfg.MinioBucket, cfg.OutputDir, cfg.UploadEvery, cfg.UploadSettle, metrics).Run(ctx)

	st := &state{}

	var rec reconciler
	var stopAll func()
	var statusFn func() []StreamStatus // /streams 接口:返回实际在拉的地址
	if cfg.Mode == "snapshot" {
		snap := NewSnapshotter(cfg.SnapshotEvery, cfg.SnapshotToWait, cfg.JpegQuality, cfg.SnapshotToMax, metrics)
		go snap.Run(ctx)
		rec, stopAll = snapshotAdapter{snap}, func() {}
		statusFn = func() []StreamStatus { return ownedToStatus(st.getOwned()) }
	} else {
		mgr := NewManager(cfg.FPS, cfg.JpegQuality, cfg.SkipNonKey, cfg.StartStagger, metrics)
		rec, stopAll = mgr, mgr.StopAll
		statusFn = mgr.Status
	}

	// 健康检查(与模式无关)
	hc := NewHealthChecker(cfg.StaleAfter, cfg.GraceAfter, metrics, st.getOwned)
	go tickLoop(ctx, cfg.HealthEvery, hc.Check)

	// 孤儿图清理:只在分片 0 上跑(避免多分片重复删)
	if cfg.Ordinal == 0 {
		cleaner := NewCleaner(mc, cfg.MinioBucket, cfg.CleanupEvery, st.getAllNames, metrics)
		go cleaner.Run(ctx)
		log.Printf("INFO 本分片为 0,启用孤儿图清理(每 %s)", cfg.CleanupEvery)
	}

	go serveMetrics(cfg.MetricsAddr, metrics, statusFn)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Printf("INFO 收到退出信号,停止...")
		cancel()
		stopAll()
		os.Exit(0)
	}()

	t := time.NewTicker(cfg.ReconcileEvery)
	defer t.Stop()
	reconcile(cfg, rec, metrics, st)
	for range t.C {
		reconcile(cfg, rec, metrics, st)
	}
}

func reconcile(cfg Config, rec reconciler, metrics *Metrics, st *state) {
	streams, err := ParseConfig(cfg.ConfigFile)
	if err != nil {
		log.Printf("ERROR 解析配置失败: %v", err)
		return
	}
	allNames := make(map[string]bool, len(streams))
	dupCheck := make(map[string]int, len(streams))
	owned := make([]Stream, 0, len(streams))
	for _, s := range streams {
		allNames[s.OutputFile] = true
		dupCheck[s.Name]++
		if Owns(s.Name, cfg.Ordinal, cfg.TotalShards) {
			owned = append(owned, s)
		}
	}
	for name, n := range dupCheck {
		if n > 1 {
			log.Printf("WARN 配置存在重复 filename %q(出现 %d 次),会互相覆盖,请检查", name, n)
		}
	}
	st.set(owned, allNames)
	metrics.Owned.Store(int64(len(owned)))
	log.Printf("INFO 配置共 %d 路,本分片 %d/%d 负责 %d 路", len(streams), cfg.Ordinal, cfg.TotalShards, len(owned))
	rec.Reconcile(owned)
}

func tickLoop(ctx context.Context, every time.Duration, fn func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

func serveMetrics(addr string, m *Metrics, statusFn func() []StreamStatus) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		m.WriteText(w)
	})
	// /streams:随时 curl 出【当前实际在拉的地址】,用来核对配置改了有没有生效
	mux.HandleFunc("/streams", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statusFn())
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("WARN metrics 服务退出: %v", err)
	}
}

// ownedToStatus:snapshot 模式没有常驻进程,用本分片负责的流 + 其配置地址
func ownedToStatus(streams []Stream) []StreamStatus {
	out := make([]StreamStatus, 0, len(streams))
	for _, s := range streams {
		out = append(out, StreamStatus{Name: s.Name, URL: s.InputURL, Running: true})
	}
	return out
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
