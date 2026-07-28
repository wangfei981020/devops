package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	// 云网络台账（GCP 只读采集）。判断"某服务是不是暴露在公网"必须看这里：
	// scheme=EXTERNAL/INTERNAL 是权威答案，K8s 侧的 Service/注解只是间接线索。
	{"list_loadbalancers", "列负载均衡:scheme(EXTERNAL=外网/INTERNAL=内网)+VIP+端口+后端实例——判断服务是否公网暴露看这个", "/api/cloud-loadbalancers", nil, false},
	{"list_firewalls", "列防火墙规则:方向/优先级/放行端口/来源网段/是否高危(0.0.0.0/0 放行敏感端口)", "/api/cloud-firewalls", nil, false},
	{"list_cloud_ips", "IP 台账聚合:静态IP+主机内外网IP+LB VIP,含是否闲置(预留未绑=白花钱)", "/api/cloud-ips", nil, false},
	{"list_cloud_addresses", "列云静态/预留 IP:地址/内外网类型/占用状态/使用者", "/api/cloud-addresses", nil, false},
	{"list_networks", "列 VPC 网络", "/api/cloud-networks", nil, false},
	// CDN(Cloudflare) 只读。域名类故障排查的最前面一跳，此前只能登录 CF 控制台看。
	{"list_cdn_zones", "列 CDN 站点:状态/套餐/NS/DNS记录数/SSL模式(flexible=回源明文,有风险)", "/api/cdn/zones", nil, false},
	{"list_cdn_dns", "列 CDN 的 DNS 解析记录:类型/目标/是否经CDN代理(proxied=橙云)——查'这个域名解析到哪'用它", "/api/cdn/dns-records", []mcpParam{{"zone", "string", "根域名", false}, {"type", "string", "记录类型 A/CNAME/...", false}, {"q", "string", "关键词(域名或解析目标)", false}}, false},
	{"cdn_domain_check", "DNS 一致性校验:CDN 解析目标 vs 我方实际入口IP——查出'解析到已下线IP的域名'(证书巡检里那批超时多半是这个)和'绕过CDN直连源站'的记录", "/api/cdn/domain-check", []mcpParam{{"zone", "string", "根域名", false}, {"only", "string", "issues=只看有问题的", false}}, false},
	{"list_subnets", "列子网:网段/区域", "/api/cloud-subnets", nil, false},
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
	{"data_freshness", "采集新鲜度:CMDB 里这份数据是什么时候采的/能不能信——下结论前先查这个,尤其当结果和预期不符时", "/api/k8s/sync-state", []mcpParam{{"cluster_id", "integer", "集群ID", false}}, false},
	{"expose_surface", "暴露面总览:所有对外入口(VS/Ingress/LB/NodePort)的内外网判定+TLS+后端存活+风险分级——问'哪些服务暴露在公网'直接用这个,不要自己拼", "/api/k8s/expose-surface", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"only", "string", "external=只看外网, risky=只看有风险的", false}}, false},
	{"cluster_health", "集群体检:一次返回所有异常(节点/工作负载/孤儿/镜像)并按critical/warning/info分级+处置建议——问'集群有什么问题'先用这个", "/api/k8s/health", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"list_orphans", "孤儿资源:还在占资源/计费/报错但已没人用的(PVC无挂载/HPA指向已删负载/VS后端不存在/空命名空间),带浪费金额和删除命令", "/api/k8s/orphans", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"kind", "string", "pvc/hpa/virtualservice/ingress/namespace,不传=全部", false}}, false},
	{"resource_waste", "资源浪费排行:request vs Prometheus实测用量,按浪费量排序+给出推荐request值——答'能缩多少/哪里最浪费'", "/api/k8s/resource-waste", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"top", "integer", "只看前N条", false}}, false},
	{"idle_cost", "闲置成本:实付 vs 已按request分摊 vs 闲置三段拆分——按request分摊的成本看板看不见闲置那部分,而缩容能省的正是它", "/api/k8s/idle-cost", []mcpParam{{"cluster_id", "integer", "集群ID", false}}, false},
	{"ns_projects", "命名空间→业务项目归属", "/api/k8s/ns-projects", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"list_pvcs", "列 PVC 存储卷", "/api/k8s/pvcs", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"list_hpas", "列 HPA 自动伸缩", "/api/k8s/hpas", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false},
	{"workload_changes", "工作负载变更历史(镜像/副本谁何时改)", "/api/k8s/changes", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"name", "string", "工作负载名", false}, {"namespace", "string", "命名空间", false}}, false},
	{"pod_logs", "读取 Pod 日志(尾部N行)——诊断服务问题用", "/api/k8s/pod-logs", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}, {"container", "string", "容器", false}, {"tail", "integer", "行数默认200", false}}, true},
	{"pod_events", "取 Pod 相关事件", "/api/k8s/pod-events", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}}, false},
	{"list_events", "统一事件(全集群,含Node),可按对象类型/级别筛", "/api/k8s/events", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"kind", "string", "对象类型(Node/Pod/..)", false}, {"type", "string", "Warning/Normal", false}, {"namespace", "string", "命名空间", false}}, false},
	{"diagnose_pod", "规则诊断 Pod:根因+证据+处置建议(只给方案)", "/api/k8s/diagnose", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}}, false},
	{"config_audit", "配置引用审计:Pod 起不来时查缺哪个 ConfigMap/Secret(含镜像拉取密钥)。ConfigMap 有名录可确定判定;Secret 无名录,仅在事件有 not found 佐证时报出——未报出不等于没问题", "/api/k8s/config-audit", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"include_unused", "string", "1=同时列出无人引用的 ConfigMap", false}}, false},
	{"domain_topology", "域名全链路:CDN→Ingress→Service→Pod→节点→云主机", "/api/k8s/topology", []mcpParam{{"domain", "string", "域名", true}}, false},
	{"node_impact", "反向影响:某节点下线/卡死影响哪些服务和域名", "/api/k8s/impact", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"node", "string", "节点名", true}}, false},
	{"cost_overview", "云成本总览:按项目/环境/集群/类型,cloud真实vs idc迁云估算", "/api/k8s/cost/overview", []mcpParam{{"dim", "string", "维度:biz_project/gcp_project/cluster/env/type", false}, {"mode", "string", "cloud/idc", false}}, false},
	{"cost_detail", "成本明细:某项目/环境的逐资源费用", "/api/k8s/cost/detail", []mcpParam{{"biz_project", "string", "业务项目", false}, {"gcp_project", "string", "GCP项目", false}, {"env", "string", "环境", false}, {"mode", "string", "cloud/idc", false}}, false},
	{"cost_report", "成本月/季/年报告+环比", "/api/k8s/cost/report", []mcpParam{{"period", "string", "month/quarter/year", false}, {"anchor", "string", "YYYY-MM", false}, {"dim", "string", "维度", false}}, false},
	{"cost_attribution", "环比归因:本月比上月哪些资源涨了/降了", "/api/k8s/cost/attribution", []mcpParam{{"month", "string", "YYYY-MM", false}}, false},
	{"resource_usage", "实际资源使用率(Prometheus):CPU/内存时序", "/api/obs/usage", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"target", "string", "pod/workload/node/host", false}, {"namespace", "string", "命名空间", false}, {"name", "string", "资源名(target=host 时传内网IP)", false}, {"metric", "string", "cpu/mem", false}, {"minutes", "integer", "时间窗分钟", false}, {"query", "string", "原始PromQL(可选)", false}, {"host_env", "string", "仅 target=host:环境 uat/prod", false}, {"host_project", "string", "仅 target=host:项目 g01/g02/g32/g33/infra", false}, {"host_team", "string", "仅 target=host:团队 app/dba/infra", false}}, false},
	// 云主机不在任何 K8s 集群里，node_usage 那套按 node 标签的口径覆盖不到——问"哪台机器内存快满了"用这个。
	{"host_usage", "云主机(非K8s)用量排行,按内存降序+可按环境/项目/团队筛——问'哪台机器CPU/内存高'用这个,node_usage 只覆盖K8s节点", "/api/obs/host-usage", []mcpParam{{"cluster_id", "integer", "集群ID(用于选数据源)", false}, {"env", "string", "环境 uat/prod", false}, {"project", "string", "项目 g01/g02/g32/g33/infra", false}, {"team", "string", "团队 app/dba/infra", false}}, false},
	{"pod_usage", "全 Pod 实时用量(cpu_m/mem_mi)——找吃资源的Pod", "/api/k8s/pod-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"node_usage", "全节点实时用量(cpu%/mem%)——找压力大的节点", "/api/k8s/node-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"node_capacity", "节点可分配vs已request vs limit——答'节点够不够/还能排多少/超卖'", "/api/k8s/node-capacity", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"ns_overview", "命名空间/项目Pod概览:总/Running/失败/Pending+失败原因", "/api/k8s/ns-overview", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"project", "string", "业务项目", false}}, false},
	{"pvc_usage", "全PVC使用率(used/cap/pct)——找快满的存储", "/api/k8s/pvc-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false},
	{"event_center", "事件中心:最近平台出了什么事(到期/变更/同步失败/K8s Warning统一时间线)——排障先看这个", "/api/k8s/event-center", []mcpParam{{"days", "integer", "天数窗口,默认30", false}, {"source", "string", "expiry/change/sync/k8s", false}, {"level", "string", "critical/warning/info", false}}, false},
	{"query_loki", "Loki 日志检索(LogQL),跨Pod/历史深查", "/api/obs/loki", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"query", "string", "LogQL", true}, {"minutes", "integer", "时间窗", false}}, false},
	{"query_prometheus", "通用 PromQL 查询,中间件指标(Kafka积压/nacos实例/etcd延迟/Harbor配额)全靠它。多集群共享数据源时须在标签里写 $CLUSTER 占位符做隔离,如 up{$CLUSTER};传 minutes 则查区间看趋势", "/api/obs/prom-query", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"query", "string", "PromQL,标签里可用 $CLUSTER 占位", true}, {"minutes", "integer", "时间窗(分钟),不传=瞬时值", false}, {"step", "string", "区间查询步长,如 1m,不传自动选", false}}, false},
	{"prom_metrics", "发现 Prometheus 里有哪些指标(按关键字过滤)。不知道指标名时先用它,再用 query_prometheus", "/api/obs/prom-metrics", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"keyword", "string", "关键字,如 kafka/nacos/etcd/harbor", false}, {"limit", "integer", "返回上限,默认200", false}}, false},
	{"prom_labels", "列某标签的可选值(如 namespace/job/instance),搞清楚指标能按什么维度筛", "/api/obs/prom-labels", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"label", "string", "标签名", true}, {"metric", "string", "限定到某指标", false}}, false},
	{"harbor_status", "Harbor 健康+存储用量+GC 状态。ImagePullBackOff 时用它区分「仓库挂了」和「凭证缺失(config_audit)」", "/api/harbor/status", []mcpParam{{"registry_id", "integer", "接入ID,不传用第一个启用的", false}}, false},
	{"harbor_projects", "Harbor 项目+配额用量(按用量比倒序,快满的在前)。推送失败先看这个", "/api/harbor/projects", []mcpParam{{"registry_id", "integer", "接入ID", false}}, false},
	{"harbor_repositories", "某项目下的镜像仓库(tag数/拉取数/最后推送时间),确认镜像到底推上去没有", "/api/harbor/repositories", []mcpParam{{"registry_id", "integer", "接入ID", false}, {"project", "string", "项目名", true}}, false},
	{"kubesphere_fetch", "拉 KubeSphere 原始 kapis 数据(兜底用;查流水线优先用 pipeline_runs/pipeline_log)", "/api/obs/kubesphere", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"path", "string", "kapis 路径", true}}, false},
	// Jenkins 的编译输出不进 pod stdout、Loki 也采不到，pod_logs/query_loki 都看不到构建失败原因，只能走这两个。
	{"pipeline_runs", "列流水线运行记录(默认只列失败的):哪条流水线/第几次构建挂了+Jenkins构建号——问'构建失败'先用这个", "/api/devops/pipeline-runs", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "DevOps项目命名空间,形如 g66-test-devopsj2q22", true}, {"pipeline", "string", "只看某条流水线", false}, {"only_failed", "string", "0=含成功的,默认只列失败", false}}, false},
	{"pipeline_log", "构建日志+自动抽出报错行:直接回答'这次构建为什么失败'(编译报错/测试失败/镜像推送失败都在这)", "/api/devops/pipeline-log", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "DevOps项目命名空间", true}, {"pipeline", "string", "流水线名", true}, {"run", "string", "Jenkins构建号(从 pipeline_runs 拿)", true}, {"tail", "integer", "尾部行数,默认60", false}, {"full", "string", "1=返回全文(可能很大)", false}}, false},
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
		// 列表类工具统一支持 fields 裁剪：全量返回动辄几十上百 KB，直接读会超上下文上限。
		if strings.HasPrefix(t.Name, "list_") {
			props["fields"] = gin.H{"type": "string",
				"description": "只返回这些列(逗号分隔),用于裁剪大列表避免超长,如 namespace,name,restarts;不传=全部列"}
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
	body = h.hintIfNarrowedToEmpty(tool, q, p.Arguments, body)
	if !tool.Text {
		body = applyFields(body, str(p.Arguments["fields"]))
	}
	c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"content": []gin.H{{"type": "text", "text": body}}}))
}

// applyFields 按调用方声明的列白名单裁剪返回的 JSON 数组。
//
// 动因：list_pods 全量 246KB、list_certificates 173KB、list_nodes 67KB，都远超单次能直接读的量，
// 每次都得先落盘再写脚本解析，既慢又容易漏字段。让调用方先声明「我只要这几列」，
// 一次调用就能拿到可直接分析的数据。
//
// 只裁剪顶层为数组的响应；对象响应（诊断、成本汇总等）原样返回。
func applyFields(body, fields string) string {
	if strings.TrimSpace(fields) == "" {
		return body
	}
	keep := map[string]bool{}
	for _, f := range strings.Split(fields, ",") {
		if f = strings.TrimSpace(f); f != "" {
			keep[f] = true
		}
	}
	if len(keep) == 0 {
		return body
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &rows) != nil {
		return body // 不是对象数组，原样返回
	}
	out := make([]map[string]json.RawMessage, 0, len(rows))
	for _, r := range rows {
		m := make(map[string]json.RawMessage, len(keep))
		for k, v := range r {
			if keep[k] {
				m[k] = v
			}
		}
		out = append(out, m)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return body
	}
	return string(b)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// hintIfNarrowedToEmpty 空结果 + 带了过滤条件时，附一句「放宽后还有多少条」。
//
// 起因是真实踩过的坑：list_virtualservices(namespace=istio-system) 返回 []，
// 于是被判定成「这个集群没用 Istio」，实际 VS 都定义在各业务 ns 下，全集群有 144 条，
// 整张入口拓扑就这么被漏掉了。空数组本身不区分「真没有」和「过滤错了」，这里补上区分。
func (h *MCPHandler) hintIfNarrowedToEmpty(tool *mcpTool, q url.Values, args map[string]any, body string) string {
	if tool.Text || strings.TrimSpace(body) != "[]" {
		return body
	}
	narrowed := narrowingArgs(args)
	if len(narrowed) == 0 {
		return body // 本来就没过滤，那就是真的没有
	}
	wide := url.Values{}
	if cid := q.Get("cluster_id"); cid != "" {
		wide.Set("cluster_id", cid) // cluster_id 是定位不是过滤，保留
	}
	wideBody, err := h.internalGet(tool.Path, wide)
	if err != nil {
		return body
	}
	var items []json.RawMessage
	if json.Unmarshal([]byte(wideBody), &items) != nil || len(items) == 0 {
		return body // 放宽后也是空 → 确实没有，不必提示
	}
	hint := gin.H{
		"items": []any{},
		"hint": "当前过滤条件(" + strings.Join(narrowed, ", ") + ")下没有匹配项，" +
			"但去掉这些条件后共有 " + strconv.Itoa(len(items)) + " 条 —— 不要据此判定集群里没有该资源，请放宽条件重查。",
	}
	b, err := json.Marshal(hint)
	if err != nil {
		return body
	}
	return string(b)
}

// narrowingArgs 列出会收窄结果的参数名。cluster_id 是定位维度而非过滤，不计入。
func narrowingArgs(args map[string]any) []string {
	out := []string{}
	for k, v := range args {
		if k == "cluster_id" || v == nil || v == "" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
