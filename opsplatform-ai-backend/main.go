package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// 可选模型清单(设置页下拉;也允许自输)。
var modelChoices = []string{
	"claude-opus-4-8",
	"claude-sonnet-5",
	"claude-haiku-4-5",
}

// Config 是运行时可改的 AI 接入配置(设置页写入,内存生效;重启回落到环境变量)。
type Config struct {
	APIKey    string `json:"-"` // 不回传明文
	AuthToken string `json:"-"` // 订阅 OAuth token(sk-ant-oat...);不回传明文
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
}

// MCPServer 是一个可后台配置的 MCP 接入(名称/地址/token/启用)。
type MCPServer struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Token   string `json:"-"` // 不回传明文
	Enabled bool   `json:"enabled"`
}

type Server struct {
	mu         sync.Mutex
	cfg        Config
	mcpServers []MCPServer
	chat       *Chatter
	sessions   map[string][]anthropic.MessageParam
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// rebuildChat 按当前配置重建 Anthropic 客户端与 Chatter。
// preserveTools=true 复用已缓存工具(仅改凭证/模型时);MCP 变动时须传 false 以重新拉取。
func (s *Server) rebuildChat(preserveTools bool) {
	var opts []option.RequestOption
	oauth := s.cfg.AuthToken != ""
	switch {
	case oauth:
		// 订阅 OAuth token:Bearer 认证 + oauth beta 头(缺一会 401)
		opts = append(opts, option.WithAuthToken(s.cfg.AuthToken))
		opts = append(opts, option.WithHeaderAdd("anthropic-beta", "oauth-2025-04-20"))
	case s.cfg.APIKey != "":
		opts = append(opts, option.WithAPIKey(s.cfg.APIKey))
	}
	if s.cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(s.cfg.BaseURL))
	}
	ac := anthropic.NewClient(opts...)

	var mcps []namedMCP
	for _, m := range s.mcpServers {
		if m.Enabled && m.URL != "" {
			mcps = append(mcps, namedMCP{name: m.Name, client: NewMCPClient(m.URL, m.Token)})
		}
	}
	var tools []anthropic.ToolUnionParam
	var owner map[string]*MCPClient
	if preserveTools && s.chat != nil {
		// 仅凭证/模型变(MCP 不变):工具列表和"工具->MCP"路由表一起保留,
		// 否则 loadTools 见 tools!=nil 会短路、不再重建 owner,导致工具调用找不到归属。
		tools = s.chat.tools
		owner = s.chat.owner
	}
	s.chat = NewChatter(ac, mcps, s.cfg.Model, oauth)
	s.chat.tools = tools
	s.chat.owner = owner
}

func (s *Server) hasKey() bool {
	return s.cfg.APIKey != "" || s.cfg.AuthToken != ""
}

func main() {
	port := env("PORT", "8080")

	s := &Server{
		cfg: Config{
			APIKey:    os.Getenv("ANTHROPIC_API_KEY"),
			AuthToken: os.Getenv("ANTHROPIC_AUTH_TOKEN"),
			BaseURL:   os.Getenv("ANTHROPIC_BASE_URL"),
			Model:     env("ANTHROPIC_MODEL", "claude-opus-4-8"),
		},
		// 启动种子:CMDB(来自 env);之后可在设置页增删改
		mcpServers: []MCPServer{{
			Name:    "CMDB",
			URL:     env("CMDB_MCP_URL", "http://localhost:30829/api/mcp"),
			Token:   os.Getenv("CMDB_MCP_TOKEN"),
			Enabled: true,
		}},
		sessions: map[string][]anthropic.MessageParam{},
	}
	s.rebuildChat(false)
	if !s.hasKey() {
		log.Println("⚠️  未配置 API key/token——模型调用会失败,可在设置页填入。")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/mcp-servers", s.handleMCPServers)
	mux.HandleFunc("/api/mcp-servers/delete", s.handleMCPServerDelete)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	log.Printf("运维 AI 助手后端 启动 :%s | model=%s | MCP 服务器=%d", port, s.cfg.Model, len(s.mcpServers))
	log.Fatal(http.ListenAndServe(":"+port, cors(mux)))
}

// cors 本地/同源都放行(前端 nginx 同源反代,本地 vite 跨端口)。
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	chat, model := s.chat, s.cfg.Model
	hasKey := s.hasKey()
	servers := append([]MCPServer(nil), s.mcpServers...)
	s.mu.Unlock()

	// 触发聚合(缓存);拿到 owner 表后按服务器逐个探测工具数
	loadErr := chat.loadTools(r.Context())
	total := len(chat.tools)

	systems := make([]map[string]any, 0, len(servers))
	anyOK := false
	for _, m := range servers {
		row := map[string]any{"name": m.Name, "url": m.URL, "enabled": m.Enabled}
		if m.Enabled && m.URL != "" {
			cli := NewMCPClient(m.URL, m.Token)
			_ = cli.Initialize(r.Context())
			if mts, e := cli.ListTools(r.Context()); e != nil {
				row["ok"] = false
				row["error"] = e.Error()
			} else {
				row["ok"] = true
				row["tools"] = len(mts)
				anyOK = true
			}
		} else {
			row["ok"] = false
			row["error"] = "未启用"
		}
		systems = append(systems, row)
	}

	resp := map[string]any{
		"model": model, "tools": total, "has_key": hasKey,
		"mcp_ok": anyOK, "systems": systems,
	}
	if loadErr != nil {
		resp["mcp_error"] = loadErr.Error()
	}
	writeJSON(w, resp)
}

// handleConfig GET 回读当前配置(不含明文密钥);POST 写入并即时生效。
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, map[string]any{
			"has_key":  s.hasKey(),
			"auth":     authKind(s.cfg),
			"base_url": s.cfg.BaseURL,
			"model":    s.cfg.Model,
			"models":   modelChoices,
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		APIKey    string `json:"api_key"`
		AuthToken string `json:"auth_token"`
		BaseURL   string `json:"base_url"`
		Model     string `json:"model"`
		ClearKey  bool   `json:"clear_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	// 密钥:非空才覆盖(避免每次都要重输);clear_key 显式清空。
	if in.ClearKey {
		s.cfg.APIKey, s.cfg.AuthToken = "", ""
	}
	if in.APIKey != "" {
		s.cfg.APIKey, s.cfg.AuthToken = in.APIKey, "" // 二选一
	}
	if in.AuthToken != "" {
		s.cfg.AuthToken, s.cfg.APIKey = in.AuthToken, ""
	}
	s.cfg.BaseURL = in.BaseURL // 允许清空
	if in.Model != "" {
		s.cfg.Model = in.Model
	}
	s.rebuildChat(true) // 仅凭证/模型变,复用已载入工具
	hasKey := s.hasKey()
	s.mu.Unlock()

	writeJSON(w, map[string]any{"ok": true, "has_key": hasKey})
}

// handleMCPServers GET 列出所有 MCP 接入(含实时连接状态);POST 新增/更新(按 name)。
func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.mu.Lock()
		servers := append([]MCPServer(nil), s.mcpServers...)
		s.mu.Unlock()
		out := make([]map[string]any, 0, len(servers))
		for _, m := range servers {
			row := map[string]any{"name": m.Name, "url": m.URL, "enabled": m.Enabled, "has_token": m.Token != ""}
			if m.Enabled && m.URL != "" {
				cli := NewMCPClient(m.URL, m.Token)
				_ = cli.Initialize(r.Context())
				if mts, e := cli.ListTools(r.Context()); e != nil {
					row["ok"], row["error"] = false, e.Error()
				} else {
					row["ok"], row["tools"] = true, len(mts)
				}
			}
			out = append(out, row)
		}
		writeJSON(w, map[string]any{"servers": out})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		Token   string `json:"token"`
		Enabled *bool  `json:"enabled"`
		OldName string `json:"old_name"` // 改名时传原名
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" || in.URL == "" {
		http.Error(w, "name/url 必填", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	key := in.OldName
	if key == "" {
		key = in.Name
	}
	found := false
	for i := range s.mcpServers {
		if s.mcpServers[i].Name == key {
			s.mcpServers[i].Name = in.Name
			s.mcpServers[i].URL = in.URL
			if in.Token != "" { // 空=不改动
				s.mcpServers[i].Token = in.Token
			}
			if in.Enabled != nil {
				s.mcpServers[i].Enabled = *in.Enabled
			}
			found = true
			break
		}
	}
	if !found {
		en := true
		if in.Enabled != nil {
			en = *in.Enabled
		}
		s.mcpServers = append(s.mcpServers, MCPServer{Name: in.Name, URL: in.URL, Token: in.Token, Enabled: en})
	}
	s.rebuildChat(false) // MCP 变动,重新聚合工具
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

// handleMCPServerDelete POST {name} 删除一个 MCP 接入。
func (s *Server) handleMCPServerDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		http.Error(w, "name 必填", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	kept := s.mcpServers[:0]
	for _, m := range s.mcpServers {
		if m.Name != in.Name {
			kept = append(kept, m)
		}
	}
	s.mcpServers = kept
	s.rebuildChat(false)
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

func authKind(c Config) string {
	switch {
	case c.AuthToken != "":
		return "oauth"
	case c.APIKey != "":
		return "api_key"
	default:
		return ""
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Session string `json:"session"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Message == "" {
		http.Error(w, "message 必填", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 关掉 nginx 缓冲,保证 SSE 实时

	s.mu.Lock()
	chat := s.chat
	msgs := s.sessions[in.Session]
	s.mu.Unlock()
	msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(in.Message)))

	emit := func(e Event) {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	_, updated, _ := chat.Turn(r.Context(), msgs, emit)

	s.mu.Lock()
	s.sessions[in.Session] = updated
	s.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
