package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opsplatform-deploy-vm-agent/internal/config"
	"opsplatform-deploy-vm-agent/internal/inventory"
	"opsplatform-deploy-vm-agent/internal/tasks"
)

// Server 把 manager / scanner / config 拼起来对外暴露 HTTP API
type Server struct {
	cfg     *config.Config
	manager *tasks.Manager
	runner  *tasks.AnsibleRunner
	scanner *inventory.Scanner
	srv     *http.Server
}

func New(cfg *config.Config, mgr *tasks.Manager) *Server {
	return &Server{
		cfg:     cfg,
		manager: mgr,
		runner:  &tasks.AnsibleRunner{AnsibleRoot: cfg.AnsibleRoot},
		scanner: &inventory.Scanner{AnsibleRoot: cfg.AnsibleRoot},
	}
}

// ListenAndServe 阻塞启动 HTTP/HTTPS server
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)               // 不要鉴权
	mux.HandleFunc("/v1/services", s.auth(s.handleServices))
	mux.HandleFunc("/v1/tasks", s.auth(s.handleTasks))
	mux.HandleFunc("/v1/tasks/", s.auth(s.handleTaskDetail)) // /v1/tasks/<id> + /v1/tasks/<id>/logs + /v1/tasks/<id>/cancel

	s.srv = &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           accessLog(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdownCtx)
	}()

	if s.cfg.IsTLS() {
		log.Printf("[agent] listening HTTPS on %s", s.cfg.Listen)
		return s.srv.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
	}
	log.Printf("[agent] listening HTTP on %s (no TLS, dev only)", s.cfg.Listen)
	return s.srv.ListenAndServe()
}

// auth 鉴权中间件：要求 Authorization: Bearer <token>
//
//	subtle.ConstantTimeCompare 防时序攻击
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	expected := []byte(s.cfg.Token)
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next(w, r)
	}
}

// accessLog 简单访问日志
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[http] %s %s %s %dms", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start).Milliseconds())
	})
}

// ---- Handlers ----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "ok",
		"version":      "v1",
		"ansible_root": s.cfg.AnsibleRoot,
		"version_root": s.cfg.VersionRoot,
		"max_concurrent": s.cfg.MaxConcurrent,
	})
}

// GET /v1/services?project=G01&env=UAT
func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	env := r.URL.Query().Get("env")
	if project == "" || env == "" {
		writeError(w, http.StatusBadRequest, "project / env required")
		return
	}
	services, err := s.scanner.ListServices(project, env)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"project":  project,
		"env":      env,
		"services": services,
		"count":    len(services),
	})
}

// POST /v1/tasks  body: { action, project, env, service, version }
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var spec tasks.Spec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "decode body: "+err.Error())
		return
	}
	t, err := s.manager.Submit(spec, s.runner)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task_id": t.ID,
		"status":  t.Status,
	})
}

// 路由分发：/v1/tasks/<id>     → 状态
//          /v1/tasks/<id>/logs → 日志（普通 / SSE）
//          /v1/tasks/<id>/cancel → 取消
func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	tail := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
	parts := strings.SplitN(tail, "/", 2)
	id := parts[0]
	if id == "" {
		writeError(w, http.StatusBadRequest, "task id required")
		return
	}
	if len(parts) == 1 {
		s.handleTaskGet(w, r, id)
		return
	}
	switch parts[1] {
	case "logs":
		s.handleTaskLogs(w, r, id)
	case "cancel":
		s.handleTaskCancel(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "unknown sub-resource")
	}
}

func (s *Server) handleTaskGet(w http.ResponseWriter, r *http.Request, id string) {
	t, ok := s.manager.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	ok := s.manager.Cancel(id)
	if !ok {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"canceled": true})
}

// GET /v1/tasks/<id>/logs?since=<offset>&stream=true
//
//	stream=true → SSE 流式，连接保持直到任务结束或客户端断
//	stream=false → 一次性返回 since 到当前的全部内容
func (s *Server) handleTaskLogs(w http.ResponseWriter, r *http.Request, id string) {
	lb := s.manager.LogBuffer(id)
	if lb == nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	since, _ := strconv.Atoi(r.URL.Query().Get("since"))
	stream := r.URL.Query().Get("stream") == "true"

	if !stream {
		data, total := lb.ReadFrom(since)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Log-Total", fmt.Sprintf("%d", total))
		w.Write(data)
		return
	}

	// SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// 先把已有的发出去（since 起点开始），再订阅增量
	initial, _ := lb.ReadFrom(since)
	if len(initial) > 0 {
		writeSSEEvent(w, "log", string(initial))
		flusher.Flush()
	}

	// 任务已结束，发完就 close
	if lb.IsClosed() {
		writeSSEEvent(w, "end", "closed")
		flusher.Flush()
		return
	}

	ch := lb.Subscribe()
	defer lb.Unsubscribe(ch)

	// 心跳防止 proxy 掐连接
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case b, ok := <-ch:
			if !ok {
				writeSSEEvent(w, "end", "closed")
				flusher.Flush()
				return
			}
			writeSSEEvent(w, "log", string(b))
			flusher.Flush()
		case <-heartbeat.C:
			// 单独的 ":" 是 SSE 注释，不会产生 message
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// ---- Helpers ----

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{"error": msg})
}

// writeSSEEvent SSE 协议格式：
//
//	event: <name>
//	data: <line1>
//	data: <line2>
//	<empty line>
func writeSSEEvent(w http.ResponseWriter, event, data string) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}
