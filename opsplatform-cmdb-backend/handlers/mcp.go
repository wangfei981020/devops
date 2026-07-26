package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"opsplatform-cmdb-backend/logx"
)

// MCPHandler 把 CMDB 只读能力暴露成 MCP（Model Context Protocol）工具，供 AI 界面/Claude Code 连。
// 实现：JSON-RPC over HTTP 桥接内部 REST API（复用全部已有只读接口，工具与 API 始终一致）。
// 只读；写操作(续费/改DNS/scale)属二期，带二次确认+RBAC。
type MCPHandler struct {
	DB     *sql.DB
	secret []byte
	port   string // 内部 API 端口，如 :8080
}

func NewMCPHandler(db *sql.DB, jwtSecret, port string) *MCPHandler {
	h := &MCPHandler{DB: db, secret: []byte(jwtSecret), port: port}
	h.ensureToken()
	return h
}

func (h *MCPHandler) ensureToken() string {
	var tok string
	_ = h.DB.QueryRow(`SELECT token FROM mcp_config WHERE id=1`).Scan(&tok)
	if tok == "" {
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		tok = hex.EncodeToString(b)
		_, _ = h.DB.Exec(`INSERT INTO mcp_config (id,token) VALUES (1,?) ON DUPLICATE KEY UPDATE token=?`, tok, tok)
	}
	return tok
}

// RegisterPublic MCP 端点用自己的 token 鉴权（不走登录中间件），注册在 public 组。
func (h *MCPHandler) RegisterPublic(r *gin.RouterGroup) {
	r.POST("/mcp", h.RPC)
}

// RegisterAuthed token 管理走登录态。
func (h *MCPHandler) RegisterAuthed(r *gin.RouterGroup) {
	r.GET("/mcp/info", h.Info)
	r.POST("/mcp/regenerate", h.Regenerate)
}

func (h *MCPHandler) Info(c *gin.Context) {
	var tok string
	var enabled int
	_ = h.DB.QueryRow(`SELECT token, enabled FROM mcp_config WHERE id=1`).Scan(&tok, &enabled)
	c.JSON(http.StatusOK, gin.H{"token": tok, "enabled": enabled, "endpoint": "/api/mcp", "tools": len(mcpTools), "transport": "http-jsonrpc"})
}

func (h *MCPHandler) Regenerate(c *gin.Context) {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	_, _ = h.DB.Exec(`UPDATE mcp_config SET token=? WHERE id=1`, tok)
	WriteAudit(h.DB, c, "regenerate_mcp_token", "")
	c.JSON(http.StatusOK, gin.H{"token": tok})
}

// ---- 工具注册表：MCP 工具 → 内部只读 API ----

type mcpParam struct {
	Name, Type, Desc string
	Required         bool
}
type mcpTool struct {
	Name, Desc, Path string
	Params           []mcpParam
	Text             bool // 返回纯文本(如日志)
}

var mcpTools = []mcpTool{
	{"list_domains", "列域名(可按项目/状态/关键词筛)，看到期/CDN/证书", "/api/domains", []mcpParam{{"status", "string", "状态筛选", false}, {"q", "string", "关键词(域名/模块)", false}}, false},
	{"list_certificates", "证书巡检:临期/过期/检测失败的证书", "/api/cert-inspect", nil, false},
	{"list_hosts", "列主机(云VM):机型/IP/状态/项目", "/api/hosts", nil, false},
	{"list_clusters", "列纳管的 K8s 集群", "/api/k8s/clusters", nil, false},
	{"list_nodes", "列节点(可只看某集群/节点池/异常),含卡死状态", "/api/k8s/nodes", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"pool", "string", "节点池", false}, {"q", "string", "关键词", false}}, false},
	{"list_workloads", "列工作负载(Deploy/STS/DS/CronJob),含副本/镜像/状态", "/api/k8s/workloads", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}, {"kind", "string", "类型", false}, {"q", "string", "关键词", false}}, false},
	{"list_pods", "列 Pod(可按命名空间/节点筛),含 req/limit/重启", "/api/k8s/pods", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}, {"node", "string", "节点名", false}, {"q", "string", "关键词", false}}, false},
	{"list_services", "列 Service", "/api/k8s/services", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"list_ingresses", "列 Ingress(hosts/tls/后端svc)", "/api/k8s/ingresses", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"list_virtualservices", "列 Istio VirtualService(hosts/挂载gateway/后端)——Istio入口排障必看", "/api/k8s/virtualservices", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"list_gateways", "列 Gateway API Gateway", "/api/k8s/gateways", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"list_httproutes", "列 HTTPRoute", "/api/k8s/httproutes", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"list_namespaces", "列命名空间", "/api/k8s/namespaces", []mcpParam{{"cluster_id", "integer", "集群ID", false}}, false},
	{"ns_projects", "命名空间→业务项目归属", "/api/k8s/ns-projects", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"list_pvcs", "列 PVC 存储卷", "/api/k8s/pvcs", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"list_hpas", "列 HPA 自动伸缩", "/api/k8s/hpas", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"workload_changes", "工作负载变更历史(镜像/副本谁何时改)", "/api/k8s/changes", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"name", "string", "工作负载名", false}, {"namespace", "string", "命名空间", false}}, false},
	{"pod_logs", "读取 Pod 日志(尾部N行)——诊断服务问题用", "/api/k8s/pod-logs", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}, {"container", "string", "容器", false}, {"tail", "integer", "行数默认200", false}}, true},
	{"pod_events", "取 Pod 相关事件", "/api/k8s/pod-events", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}}, false},
	{"list_events", "统一事件(全集群,含Node),可按对象类型/级别筛", "/api/k8s/events", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"kind", "string", "对象类型(Node/Pod/..)", false}, {"type", "string", "Warning/Normal", false}, {"namespace", "string", "命名空间", false}}, false},
	{"diagnose_pod", "规则诊断 Pod:根因+证据+处置建议(只给方案)", "/api/k8s/diagnose", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}}, false},
	{"domain_topology", "域名全链路:CDN→Ingress→Service→Pod→节点→云主机", "/api/k8s/topology", []mcpParam{{"domain", "string", "域名", true}}, false},
	{"node_impact", "反向影响:某节点下线/卡死影响哪些服务和域名", "/api/k8s/impact", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"node", "string", "节点名", true}}, false},
	{"cost_overview", "云成本总览:按项目/环境/集群/类型,cloud真实vs idc迁云估算", "/api/k8s/cost/overview", []mcpParam{{"dim", "string", "维度:biz_project/gcp_project/cluster/env/type", false}, {"mode", "string", "cloud/idc", false}}, false},
	{"cost_detail", "成本明细:某项目/环境的逐资源费用", "/api/k8s/cost/detail", []mcpParam{{"biz_project", "string", "业务项目", false}, {"gcp_project", "string", "GCP项目", false}, {"env", "string", "环境", false}, {"mode", "string", "cloud/idc", false}}, false},
	{"cost_report", "成本月/季/年报告+环比", "/api/k8s/cost/report", []mcpParam{{"period", "string", "month/quarter/year", false}, {"anchor", "string", "YYYY-MM", false}, {"dim", "string", "维度", false}}, false},
	{"cost_attribution", "环比归因:本月比上月哪些资源涨了/降了", "/api/k8s/cost/attribution", []mcpParam{{"month", "string", "YYYY-MM", false}}, false},
	{"resource_usage", "实际资源使用率(Prometheus):CPU/内存时序", "/api/obs/usage", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"target", "string", "pod/workload/node/host", false}, {"namespace", "string", "命名空间", false}, {"name", "string", "资源名", false}, {"metric", "string", "cpu/mem", false}, {"minutes", "integer", "时间窗分钟", false}, {"query", "string", "原始PromQL(可选)", false}}, false},
	{"pod_usage", "全 Pod 实时用量(cpu_m/mem_mi)——找吃资源的Pod", "/api/k8s/pod-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"node_usage", "全节点实时用量(cpu%/mem%)——找压力大的节点", "/api/k8s/node-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"node_capacity", "节点可分配vs已request vs limit——答'节点够不够/还能排多少/超卖'", "/api/k8s/node-capacity", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"ns_overview", "命名空间/项目Pod概览:总/Running/失败/Pending+失败原因", "/api/k8s/ns-overview", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"project", "string", "业务项目", false}}, false},
	{"pvc_usage", "全PVC使用率(used/cap/pct)——找快满的存储", "/api/k8s/pvc-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"event_center", "事件中心:最近平台出了什么事(到期/变更/同步失败/K8s Warning统一时间线)——排障先看这个", "/api/k8s/event-center", []mcpParam{{"days", "integer", "天数窗口,默认30", false}, {"source", "string", "expiry/change/sync/k8s", false}, {"level", "string", "critical/warning/info", false}}, false},
	{"query_loki", "Loki 日志检索(LogQL),跨Pod/历史深查", "/api/obs/loki", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"query", "string", "LogQL", true}, {"minutes", "integer", "时间窗", false}}, false},
	{"kubesphere_fetch", "拉 KubeSphere 数据(如流水线运行/日志),交AI诊断失败或耗时长", "/api/obs/kubesphere", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"path", "string", "kapis 路径", true}}, false},
}

// ---- JSON-RPC ----

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (h *MCPHandler) RPC(c *gin.Context) {
	// token 鉴权
	var want string
	var enabled int
	_ = h.DB.QueryRow(`SELECT token, enabled FROM mcp_config WHERE id=1`).Scan(&want, &enabled)
	got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if got == "" {
		got = c.GetHeader("X-MCP-Token")
	}
	if enabled == 0 || want == "" || got != want {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "MCP token 无效"})
		return
	}
	var req rpcReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, rpcErr(nil, -32700, "parse error"))
		return
	}
	switch req.Method {
	case "initialize":
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{
			"protocolVersion": "2024-11-05",
			"capabilities":    gin.H{"tools": gin.H{}},
			"serverInfo":      gin.H{"name": "cmdb-mcp", "version": "1.0"},
		}))
	case "notifications/initialized", "notifications/cancelled":
		c.Status(http.StatusOK) // 通知无返回
	case "ping":
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{}))
	case "tools/list":
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"tools": toolSchemas()}))
	case "tools/call":
		h.callTool(c, req)
	default:
		c.JSON(http.StatusOK, rpcErr(req.ID, -32601, "method not found: "+req.Method))
	}
}

func toolSchemas() []gin.H {
	out := []gin.H{}
	for _, t := range mcpTools {
		props := gin.H{}
		req := []string{}
		for _, p := range t.Params {
			props[p.Name] = gin.H{"type": p.Type, "description": p.Desc}
			if p.Required {
				req = append(req, p.Name)
			}
		}
		schema := gin.H{"type": "object", "properties": props}
		if len(req) > 0 {
			schema["required"] = req
		}
		out = append(out, gin.H{"name": t.Name, "description": t.Desc, "inputSchema": schema})
	}
	return out
}

func (h *MCPHandler) callTool(c *gin.Context, req rpcReq) {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	_ = json.Unmarshal(req.Params, &p)
	var tool *mcpTool
	for i := range mcpTools {
		if mcpTools[i].Name == p.Name {
			tool = &mcpTools[i]
			break
		}
	}
	if tool == nil {
		c.JSON(http.StatusOK, rpcErr(req.ID, -32602, "unknown tool: "+p.Name))
		return
	}
	// 拼查询参数
	q := url.Values{}
	for k, v := range p.Arguments {
		if v == nil {
			continue
		}
		switch t := v.(type) {
		case float64:
			q.Set(k, trimFloat(t))
		case string:
			q.Set(k, t)
		case bool:
			if t {
				q.Set(k, "1")
			}
		default:
			q.Set(k, "")
		}
	}
	body, err := h.internalGet(tool.Path, q)
	logx.J("mcp", "tool_call", map[string]any{"tool": p.Name, "args": p.Arguments, "err": errStr(err)})
	WriteAudit(h.DB, c, "mcp_tool:"+p.Name, tool.Path)
	if err != nil {
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"content": []gin.H{{"type": "text", "text": "调用失败: " + err.Error()}}, "isError": true}))
		return
	}
	c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"content": []gin.H{{"type": "text", "text": body}}}))
}

// internalGet 用系统 JWT 调本进程 REST API（复用全部只读逻辑 + RBAC）。
func (h *MCPHandler) internalGet(path string, q url.Values) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": 0, "username": "mcp", "exp": time.Now().Add(2 * time.Minute).Unix(),
	})
	signed, _ := tok.SignedString(h.secret)
	u := "http://127.0.0.1" + h.port + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	rq, _ := http.NewRequest("GET", u, nil)
	rq.Header.Set("Authorization", "Bearer "+signed)
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(rq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(b), nil
}

// ---- helpers ----

func rpcOK(id json.RawMessage, result any) gin.H {
	return gin.H{"jsonrpc": "2.0", "id": rawID(id), "result": result}
}
func rpcErr(id json.RawMessage, code int, msg string) gin.H {
	return gin.H{"jsonrpc": "2.0", "id": rawID(id), "error": gin.H{"code": code, "message": msg}}
}
func rawID(id json.RawMessage) any {
	if len(id) == 0 {
		return nil
	}
	var v any
	_ = json.Unmarshal(id, &v)
	return v
}
func trimFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
func errStr(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
