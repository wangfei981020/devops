package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"ops-cmdb-backend/internal/httpx"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/license"
	"ops-cmdb-backend/logx"
)

// MCPHandler 把 CMDB 只读能力暴露成 MCP（Model Context Protocol）工具，供 AI 界面/Claude Code 连。
// 实现：JSON-RPC over HTTP 桥接内部 REST API（复用全部已有只读接口，工具与 API 始终一致）。
// 只读；写操作(续费/改DNS/scale)属二期，带二次确认+RBAC。
type MCPHandler struct {
	DB     *sql.DB
	secret []byte
	port   string // 内部 API 端口，如 :8080
	// License 决定给出哪些工具。可以为 nil（没接授权时按全量给）
	License *license.Manager
}

func NewMCPHandler(db *sql.DB, jwtSecret, port string, lic *license.Manager) *MCPHandler {
	return &MCPHandler{DB: db, secret: []byte(jwtSecret), port: port, License: lic}
}

// fullAllowed 这套授权能不能用全量工具。
//
// ⚠️ 没接授权（License 为 nil）时按**能用**处理，不是按不能用。
// 反过来的话，一次授权加载失败就会让所有 AI 接入静默少掉 68 个工具 ——
// 而 AI 不会说"我少了工具"，它会说"查不到"，看起来像数据没采上来。
func (h *MCPHandler) fullAllowed() bool {
	if h.License == nil {
		return true
	}
	return h.License.Has(license.FeatureMCPFull)
}

// ⚠️ 这里原来有 ensureToken() 和 Regenerate()，维护的是 mcp_config.token ——
// 一个**全局**令牌。令牌模型改成「一接入方一条、绑角色、只存哈希」之后
// （见 mcp_tokens.go），鉴权只认 mcp_tokens：resolveToken 查的是那张表。
//
// 于是 mcp_config.token 变成了一个没有任何地方会验证的字符串，
// 而 `POST /api/mcp/regenerate` 是个**彻底的死接口**：
// 它会成功、会返回一个新令牌、会写审计 —— 拿这个令牌去调 MCP 必然 401。
//
// 这类接口不能"补一个界面入口"了事，那等于把骗人的功能做出来。删掉。
// mcp_config 这张表还在用，但只用 enabled 那一列。

// RegisterPublic MCP 端点用自己的 token 鉴权（不走登录中间件），注册在 public 组。
func (h *MCPHandler) RegisterPublic(r *gin.RouterGroup) {
	r.POST("/mcp", h.RPC)
}

// RegisterAuthed token 管理走登录态。
func (h *MCPHandler) RegisterAuthed(r *gin.RouterGroup) {
	r.GET("/mcp/info", h.Info)
	r.GET("/mcp/tokens", h.ListTokens)
	r.POST("/mcp/tokens", h.CreateToken)
	r.PUT("/mcp/tokens/:id", h.UpdateToken)
	r.DELETE("/mcp/tokens/:id", h.DeleteToken)
}

func (h *MCPHandler) Info(c *gin.Context) {
	var enabled int
	_ = h.DB.QueryRow(`SELECT enabled FROM mcp_config WHERE id=1`).Scan(&enabled)
	full := h.fullAllowed()
	avail := 0
	for _, t := range mcpTools {
		if full || !t.EE {
			avail++
		}
	}
	// ⚠️ 不再返回 token。令牌现在一接入方一条、只存哈希，
	// 明文只在创建那一刻给一次（见 mcp_tokens.go）
	// ⚠️ 这个数是**授权档次的上限**，不是任何一条令牌实际能用的数量。
	//
	// 每条令牌还要再过一层角色过滤（见 toolSchemas），所以实际拿到的更少 ——
	// 比如 cmdb_viewer 令牌只看得到 67 个。界面上把这个数说成
	// 「当前可用」是虚报（OPSCMDB-008）：接入方按 78 去规划，
	// 实际调用时撞上一批看不见的工具。
	//
	// 逐令牌的真实数量在 ListTokens 里按各自角色算，界面应显示那个。
	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled == 1, "endpoint": "/api/mcp", "transport": "http-jsonrpc",
		// 三个数都给：只给"可用 10 个"的话，客户不知道自己少了什么；
		// 只给"共 78 个"又看不出当前能用多少
		"tools_licensed": avail, "tools_total": len(mcpTools), "full_licensed": full,
	})
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
	// EE 这个工具属于企业版。
	//
	// 分档的依据是**能力性质**，不是随手划的：
	// 社区版给的是"看得到有什么"（资产清单、状态、新鲜度）——
	// 它本身就能用，不是个残废版；
	// 企业版给的是"看得出哪里不对"（诊断、成本归因、日志检索、
	// 安全审计、升级预案）。后者的价值随工具数量非线性增长，
	// 因为不登服务器排障靠的正是交叉验证。
	EE bool
}

// slowTools 天然慢、内部回调需要更长超时的工具。
//
// # 🔴 为什么不全局调大 internalGet 的超时
//
// 那 30 秒**同时在保护 MCP 自己**：一个卡住的查询会一直占着连接，
// 全局放宽等于让任意一个慢查询都能把 MCP 拖住。所以只给这里列出的放宽。
//
// # 什么样的工具该进这个集合
//
// 耗时**正比于集群里的对象数**，而不是一次固定查询。
// 典型是批量诊断：每个 Pod 都要 get pod + list events + 读日志，
// 而读日志要经 APIServer 代理到节点 kubelet —— 单个就可能好几秒。
//
// ⚠️ 别拿它当"这个接口有点慢"的创可贴。慢是因为要做的事多，
// 而不是某一步该优化没优化 —— 后者应该去优化那一步。
//
// ⚠️ 用具名集合而不是 mcpTool 的字段：工具定义是位置字面量，
// 末尾已经是 `false, true` 这种谁也看不出含义的一串，
// 再加一个 `true` 只会更难读，而且要改全部 ~100 条定义。
var slowTools = map[string]bool{
	"diagnose_sweep": true,
}

// slowToolTimeout 慢工具的内部回调超时。
//
// ⚠️ 必须**大于**被调接口自己的时间预算，否则接口还没来得及
// "扫到哪算哪地返回"，就已经被这一层掐断 —— 调用方拿到一个纯错误，
// 而实际上大半的活已经干完。diagnose_sweep 的预算是 50s，这里给 120s 留余量。
const slowToolTimeout = 120 * time.Second

var mcpTools = []mcpTool{
	{"list_domains", "列域名(可按项目/状态/关键词筛)，看到期/CDN/证书", "/api/domains", []mcpParam{{"status", "string", "状态筛选", false}, {"q", "string", "关键词(域名/模块)", false}}, false, false},
	{"list_certificates", "证书巡检:临期/过期/检测失败的证书。⚠️返回包装对象 {items,probe_state,probe_note}：probe_state=never 表示 443 探测任务从没跑过,此时 items 里的 expiry_at 空值是「未知」不是「没有到期日」,probe_note 里有原因和处置办法——别把这种情况报告成「证书都没有到期日」", "/api/cert-inspect", nil, false, false},
	// ⚠️ lifecycle 必须写进描述：status 是云上最后一次观测值，已销毁的机器
	// 它停在 RUNNING 上。判断"这台机器还在不在"只能看 lifecycle，
	// 否则人看对了、AI 看错了（CMDB-003）。
	{"list_hosts", "列主机(云VM):机型/IP/状态/项目;⚠️判断机器是否还存在看 lifecycle(present=在/gone=云上已查不到),不要看 status——status 是最后一次观测值,已销毁的机器它停在 RUNNING", "/api/hosts", nil, false, false},
	// 云网络台账（GCP 只读采集）。判断"某服务是不是暴露在公网"必须看这里：
	// scheme=EXTERNAL/INTERNAL 是权威答案，K8s 侧的 Service/注解只是间接线索。
	{"list_loadbalancers", "列负载均衡:scheme(EXTERNAL=外网/INTERNAL=内网)+VIP+端口+后端实例——判断服务是否公网暴露看这个", "/api/cloud-loadbalancers", nil, false, true},
	{"list_firewalls", "列防火墙规则:方向/优先级/放行端口/来源网段/是否高危(0.0.0.0/0 放行敏感端口)", "/api/cloud-firewalls", nil, false, true},
	{"list_cloud_ips", "IP 台账聚合:静态IP+主机内外网IP+LB VIP,含是否闲置(预留未绑=白花钱)", "/api/cloud-ips", nil, false, true},
	{"list_cloud_addresses", "列云静态/预留 IP:地址/内外网类型/占用状态/使用者", "/api/cloud-addresses", nil, false, true},
	{"list_networks", "列 VPC 网络", "/api/cloud-networks", nil, false, true},
	// CDN(Cloudflare) 只读。域名类故障排查的最前面一跳，此前只能登录 CF 控制台看。
	{"list_cdn_zones", "列 CDN 站点:状态/套餐/NS/DNS记录数/SSL模式(flexible=回源明文,有风险)", "/api/cdn/zones", nil, false, true},
	{"list_registrar_dns", "列注册商(GoDaddy)侧的 DNS 解析记录,并标出这一份生不生效(effective: true/false/null=没采到NS)。⚠️ NS 指向 GoDaddy 的域名,真正生效的解析在这里而不在 list_cdn_dns", "/api/registrar/dns-records", []mcpParam{{"domain", "string", "主域名", false}, {"type", "string", "记录类型 A/CNAME/...", false}, {"q", "string", "关键词", false}}, false, true},
	{"list_cdn_dns", "列 CDN 的 DNS 解析记录:类型/目标/是否经CDN代理(proxied=橙云)——查'这个域名解析到哪'用它", "/api/cdn/dns-records", []mcpParam{{"zone", "string", "根域名", false}, {"type", "string", "记录类型 A/CNAME/...", false}, {"q", "string", "关键词(域名或解析目标)", false}}, false, true},
	{"cdn_domain_check", "DNS 一致性校验:CDN 解析目标 vs 我方实际入口IP——查出'解析到已下线IP的域名'(证书巡检里那批超时多半是这个)和'绕过CDN直连源站'的记录", "/api/cdn/domain-check", []mcpParam{{"zone", "string", "根域名", false}, {"only", "string", "issues=只看有问题的", false}}, false, true},
	{"list_subnets", "列子网:网段/区域", "/api/cloud-subnets", nil, false, true},
	{"list_clusters", "列纳管的 K8s 集群", "/api/k8s/clusters", nil, false, false},
	{"list_nodes", "列节点(可只看某集群/节点池/异常),含卡死状态", "/api/k8s/nodes", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"pool", "string", "节点池", false}, {"q", "string", "关键词", false}}, false, false},
	{"list_workloads", "列工作负载(Deploy/STS/DS/CronJob),含副本/镜像/状态", "/api/k8s/workloads", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}, {"kind", "string", "类型", false}, {"q", "string", "关键词", false}}, false, false},
	{"list_pods", "列 Pod(可按命名空间/节点筛),含 req/limit/重启", "/api/k8s/pods", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}, {"node", "string", "节点名", false}, {"q", "string", "关键词", false}}, false, false},
	{"list_services", "列 Service", "/api/k8s/services", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false, false},
	{"list_ingresses", "列 Ingress(hosts/tls/后端svc)", "/api/k8s/ingresses", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false, true},
	{"list_virtualservices", "列 Istio VirtualService(hosts/挂载gateway/后端)——Istio入口排障必看", "/api/k8s/virtualservices", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false, true},
	{"list_gateways", "列 Gateway(两套都含):api_group=networking.istio.io 是 Istio Gateway(gateway_class 列放的是 selector,即由哪个网关负载承载),gateway.networking.k8s.io 是 Gateway API。查 VirtualService 挂的 Gateway 在哪用它", "/api/k8s/gateways", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}, {"api_group", "string", "按 API 组筛", false}}, false, true},
	{"list_httproutes", "列 HTTPRoute", "/api/k8s/httproutes", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false, true},
	{"list_namespaces", "列命名空间", "/api/k8s/namespaces", []mcpParam{{"cluster_id", "integer", "集群ID", false}}, false, false},
	{"triage", "分诊入口:一次列出当前所有值得关注的事(按严重度排序)+每条该调哪个工具继续查+为什么查它。⚠️排障从这里开始,别挨个试 list_* 去撞问题;findings 为空时看 note 和 freshness——采集没跑通时也会是空的", "/api/triage", nil, false, true},
	{"data_freshness", "采集新鲜度:CMDB 里这份数据是什么时候采的/能不能信——下结论前先查这个,尤其当结果和预期不符时", "/api/k8s/sync-state", []mcpParam{{"cluster_id", "integer", "集群ID", false}}, false, false},
	{"expose_surface", "暴露面总览:所有对外入口(VS/Ingress/LB/NodePort)的内外网判定+TLS+后端存活+风险分级——问'哪些服务暴露在公网'直接用这个,不要自己拼", "/api/k8s/expose-surface", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"only", "string", "external=只看外网, risky=只看有风险的", false}}, false, true},
	// GKE 版本与升级（阶段 6）：AI 直接回答「什么时候会被升级」「历史上是不是被自动升的」
	{"gke_upgrade_status", "GKE 升级状态:每个集群的控制面/节点池分别什么时候会被自动升级、目标版本、被什么挡住、支持截止(取控制面与所有节点池最早)。问'某集群什么时候升级''会不会被强制升'用这个", "/api/gke/upgrade/overview", nil, false, true},
	{"gke_upgrade_history", "GKE 升级历史:何时升过/从哪版到哪版/是Google自动升(AUTOMATIC)还是我们手动升(MANUAL)。⚠️GCP保留期很短,空结果不等于没发生过,看返回的 coverage", "/api/gke/upgrade/history", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"start_type", "string", "AUTOMATIC=自动 MANUAL=手动", false}, {"scope", "string", "control_plane=控制面 nodepool=节点池", false}}, false, true},
	{"gke_node_repair_history", "GKE 节点自动修复记录:哪些节点被静默drain重建过及原因。node auto-repair 默认开启且无通知,这是唯一能事后追溯的地方", "/api/gke/repair-history", []mcpParam{{"cluster_id", "integer", "集群ID", false}}, false, true},
	{"gke_version_schedule", "GKE 官网版本排期表:某小版本在各通道的自动升级日期与标准/扩展支持截止。⚠️月(2026-09)或季度(2026-Q4)粒度是官方近似值,看 precision 字段别当精确日期", "/api/gke/version-schedule", nil, false, true},
	// 升级预案与过程看板：把「排一次升级」需要的东西一次性给全，不用再逐个工具拼。
	// 只读——CMDB 不执行升级，执行在 GCP 控制台，预案里给的是控制台步骤。
	{"gke_upgrade_plan", "GKE 升级预案:一次给全排升级要的所有东西——分池耗时预估(按真实 blueGreenSettings 算,标明是实测还是经验区间)、配额需求(BLUE_GREEN 要翻倍节点)、会中断的单副本服务清单、单点集中节点、余量为0会卡住drain的PDB、升级前基线快照、控制台执行步骤、验证清单。问'升级要多久''会断什么''该怎么升'用这个,别自己拼。⚠️看 estimate.measured 与 incomplete:false/非空表示用的经验区间且有参数缺失,排停机窗口按上限算", "/api/gke/upgrade/plan", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"target_version", "string", "目标版本如 1.35.6-gke.1127000;不传=用CMDB推断的下一个小版本", false}}, false, true},
	{"gke_available_versions", "某集群所在区域可选的升级目标版本(GKE getServerConfig 权威清单,按官方降序)。给 gke_upgrade_plan 填 target_version 前先查这个,别猜版本号。⚠️ versions 为空是「还没采集」不是「没有可用版本」,看 note 字段", "/api/gke/available-versions", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"kind", "string", "master=控制面(默认) node=节点池", false}}, false, true},
	{"gke_upgrade_progress", "GKE 升级过程与实测节奏:升级中看各池进度/卡没卡住,升完自动算出实测并行度(每批几台)、单批耗时中位数与最慢值、整池耗时,并给出外推到其他集群的公式。排生产升级窗口前先看目标集群和已升过集群的这个。⚠️时间粒度=采集间隔120秒,有±2分钟误差", "/api/gke/upgrade/progress", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"hours", "integer", "回看几小时,默认24", false}}, false, true},
	{"cluster_health", "集群体检:一次返回所有异常(节点/工作负载/孤儿/镜像)并按critical/warning/info分级+处置建议——问'集群有什么问题'先用这个", "/api/k8s/health", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	// 🔴 体检/分诊只给数量，这个才给「是哪几个」。没有它，「8 个 Pod 异常」
	// 到「调 diagnose_pod(namespace,pod)」之间是断的 —— 只能把 650 条 Pod 全拉下来自己筛
	{"health_detail", "体检项下钻:把'有 N 个 Pod 重启超100次'变成'是哪 N 个'(带命名空间/Pod名/重启数/原因)。⚠️ cluster_health 与 triage 只给数量,要拿到具体对象名去调 diagnose_pod/pod_logs,必须先过这一步。key 取自 cluster_health 返回里的 key 字段,如 pod_high_restart/pod_pending/pod_oomkilled/workload_replica_gap/orphan_pvc/sync_failed", "/api/k8s/health/detail", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"key", "string", "体检项 key,来自 cluster_health 的 key 字段", true}}, false, true},
	{"list_orphans", "孤儿资源:还在占资源/计费/报错但已没人用的(PVC无挂载/HPA指向已删负载/VS后端不存在/空命名空间),带浪费金额和删除命令", "/api/k8s/orphans", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"kind", "string", "pvc/hpa/virtualservice/ingress/namespace,不传=全部", false}}, false, true},
	{"resource_waste", "资源浪费排行:request vs Prometheus实测用量,按浪费量排序+给出推荐request值——答'能缩多少/哪里最浪费'", "/api/k8s/resource-waste", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"top", "integer", "只看前N条", false}}, false, true},
	{"idle_cost", "闲置成本:实付 vs 已按request分摊 vs 闲置三段拆分——按request分摊的成本看板看不见闲置那部分,而缩容能省的正是它", "/api/k8s/idle-cost", []mcpParam{{"cluster_id", "integer", "集群ID", false}}, false, true},
	{"ns_projects", "命名空间→业务项目归属", "/api/k8s/ns-projects", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	{"list_pvcs", "列 PVC 存储卷", "/api/k8s/pvcs", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false, true},
	{"list_hpas", "列 HPA 自动伸缩", "/api/k8s/hpas", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"namespace", "string", "命名空间", false}}, false, true},
	// 排升级/维护窗口必查：余量为 0 的 PDB 会让 drain 卡到超时才强杀，单节点多花一小时。
	// collected=false 表示没采到（多半是只读 RBAC 缺 policy 组），此时「没有阻塞」这个结论不成立。
	{"list_pdbs", "列 PodDisruptionBudget,带 drain 阻塞判定。排升级/维护窗口/节点下线必查:blocking=1 只看余量为0的(此刻驱逐任何Pod都会被拒,节点drain会卡到超时)。⚠️看返回的 collected 字段,false=没采到,此时不能得出'无阻塞'的结论", "/api/k8s/pdbs", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"blocking", "string", "1=只看余量为0的(会阻塞drain的)", false}}, false, true},
	{"workload_changes", "工作负载变更历史(镜像/副本谁何时改)", "/api/k8s/changes", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"name", "string", "工作负载名", false}, {"namespace", "string", "命名空间", false}}, false, true},
	// previous 是 CrashLoopBackOff 排障的唯一入口：当前实例刚起来往往还没打日志，真正的报错在上一个已崩溃的实例里。
	// 后端一直支持，但这里漏了声明，等于调用方不知道有这个参数——重启上百次的 Pod 就此查不出根因。
	{"pod_logs", "读取 Pod 日志(尾部N行)——诊断服务问题用。CrashLoopBackOff/重启次数高的 Pod 必须传 previous=1,否则拿到的是刚起来的空日志", "/api/k8s/pod-logs", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}, {"container", "string", "容器", false}, {"tail", "integer", "行数默认200", false}, {"previous", "string", "1=取上一个已崩溃容器实例的日志(CrashLoop 必用)", false}}, true, true},
	{"pod_events", "取 Pod 相关事件", "/api/k8s/pod-events", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}}, false, true},
	// 默认剔噪声 + sort=count，是因为原版实测不可用：argocd 的 StatusRefreshed 占了返回的三分之二，
	// 而 count 达 82 万的真问题被挤出视野。排障要的是「哪个对象在反复报错」，不是「最近一分钟谁动了」。
	{"list_events", "统一事件(全集群,含Node),可按对象类型/级别/reason筛。默认剔掉 argocd StatusRefreshed 等控制器对账噪声;排查'反复发生的问题'传 sort=count,按累计次数倒序,能一眼看出哪个对象在刷几十万次", "/api/k8s/events", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"kind", "string", "对象类型(Node/Pod/..)", false}, {"type", "string", "Warning/Normal", false}, {"namespace", "string", "命名空间", false}, {"reason", "string", "按 reason 精确筛(BackOff/FailedScheduling/OOMKilling 等);传了就不再剔噪声", false}, {"sort", "string", "count=按累计次数倒序(排障用),默认按时间倒序", false}, {"min_count", "integer", "只看累计次数>=N 的,用来过滤偶发", false}, {"hours", "integer", "只看最近N小时(注意 apiserver 事件 TTL 通常只有1小时,更早的查不到)", false}, {"limit", "integer", "apiserver 取回上限,默认1000最大5000;返回条数接近它说明可能被截断,应收窄 namespace 或提高它", false}, {"exclude_reason", "string", "额外排除的 reason,逗号分隔", false}, {"include_noise", "string", "1=不剔噪声 reason", false}}, false, true},
	{"diagnose_sweep", "批量诊断自检:把整个集群的异常 Pod 跑一遍规则,报「几个判出了具体根因、几个只给了泛化结论」+未判出清单。⚠️ 用它回答「诊断覆盖到什么程度了」——一个个点 diagnose_pod 抽样永远只会先命中高频形态,长尾看不到头。⚠️ 昂贵操作(每个 Pod 都打一次 APIServer 并读日志),默认扫 40 个,最大 100", "/api/k8s/diagnose-sweep", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"limit", "integer", "最多扫几个异常 Pod(默认30,最大100)", false}, {"offset", "integer", "从第几个开始扫,用于翻页扫完全部(按重启次数倒序;不翻页的话永远只看得到重启最多的那批,长尾扫不到)", false}}, false, true},
	{"diagnose_pod", "规则诊断 Pod:根因+证据+处置建议(只给方案)", "/api/k8s/diagnose", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", true}, {"pod", "string", "Pod名", true}, {"force_ai", "string", "填 1 = 不认规则给的结论，让 AI 再看一遍（规则只是初判；默认规则命中就不调 AI）", false}}, false, true},
	{"diagnose_cluster", "集群级诊断:采集健康+节点心跳+异常Pod → 根因+证据+处置建议。⚠️先查采集健康,采集断了的话后面所有「没发现问题」都不成立", "/api/k8s/diagnose-cluster", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	{"diagnose_domain", "域名整链诊断:注册到期→解析→CDN→证书,四段任一断了表现都是「打不开」但处置完全不同——从错的那段开始查最浪费时间", "/api/diagnose-domain", []mcpParam{{"domain", "string", "完整域名", true}}, false, true},
	{"diagnose_cost", "成本诊断入口:按闲置→环比归因→浪费→孤儿的顺序指路。⚠️只指路不下结论,省钱决策要看业务容量规划", "/api/diagnose-cost", nil, false, true},
	// 采集只落列不落 spec，所以探针路径/超时、env、亲和性、preStop 这些之前完全看不到——
	// 而"探针为什么失败""preStop 为什么挂"恰恰是排障主力问题。这是 kubectl get -o yaml 的只读等价物。
	{"get_manifest", "取单个对象的完整 YAML(已脱敏)。list_* 只给汇总列,看不到 spec;要判探针路径/超时/initialDelay、env、volumeMounts、亲和性/容忍度、preStop、Istio 路由细则、cert-manager Challenge 卡在哪,都用这个。支持 pod/deployment/statefulset/daemonset/replicaset/job/cronjob/service/endpoints/configmap/ingress/networkpolicy/pdb/hpa/pvc/pv/node/namespace/serviceaccount/resourcequota/limitrange/storageclass、Istio(virtualservice/destinationrule/gateway/serviceentry/sidecar/peerauthentication/authorizationpolicy/envoyfilter)、httproute、cert-manager(certificate/certificaterequest/order/challenge/issuer/clusterissuer)、argocd application。Secret 拒绝返回", "/api/k8s/manifest", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"kind", "string", "资源类型,如 pod/deployment/virtualservice/challenge", true}, {"name", "string", "对象名", true}, {"namespace", "string", "命名空间(集群级资源如 node/pv/storageclass/clusterissuer 不用传)", false}, {"api_group", "string", "仅 kind=gateway 时用于区分两套API:networking.istio.io(默认) 或 gateway.networking.k8s.io", false}}, true, true},
	{"config_audit", "配置引用审计:Pod 起不来时查缺哪个 ConfigMap/Secret(含镜像拉取密钥)。ConfigMap 有名录可确定判定;Secret 无名录,仅在事件有 not found 佐证时报出——未报出不等于没问题", "/api/k8s/config-audit", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"include_unused", "string", "1=同时列出无人引用的 ConfigMap", false}}, false, true},
	{"domain_topology", "域名全链路:CDN→Ingress→Service→Pod→节点→云主机", "/api/k8s/topology", []mcpParam{{"domain", "string", "域名", true}}, false, true},
	{"node_impact", "反向影响:某节点下线/卡死影响哪些服务和域名", "/api/k8s/impact", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"node", "string", "节点名", true}}, false, true},
	{"cost_overview", "云成本总览:按项目/环境/集群/类型,cloud真实vs idc迁云估算", "/api/k8s/cost/overview", []mcpParam{{"dim", "string", "维度:biz_project/gcp_project/cluster/env/type", false}, {"mode", "string", "cloud/idc", false}}, false, true},
	{"cost_detail", "成本明细:某项目/环境的逐资源费用", "/api/k8s/cost/detail", []mcpParam{{"biz_project", "string", "业务项目", false}, {"gcp_project", "string", "GCP项目", false}, {"env", "string", "环境", false}, {"mode", "string", "cloud/idc", false}}, false, true},
	{"cost_report", "成本月/季/年报告+环比", "/api/k8s/cost/report", []mcpParam{{"period", "string", "month/quarter/year", false}, {"anchor", "string", "YYYY-MM", false}, {"dim", "string", "维度", false}}, false, true},
	{"cost_attribution", "环比归因:本月比上月哪些资源涨了/降了", "/api/k8s/cost/attribution", []mcpParam{{"month", "string", "YYYY-MM", false}}, false, true},
	{"resource_usage", "实际资源使用率(Prometheus):CPU/内存时序", "/api/obs/usage", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"target", "string", "pod/workload/node/host", false}, {"namespace", "string", "命名空间", false}, {"name", "string", "资源名(target=host 时传内网IP)", false}, {"metric", "string", "cpu/mem", false}, {"minutes", "integer", "时间窗分钟", false}, {"query", "string", "原始PromQL(可选)", false}, {"host_env", "string", "仅 target=host:环境 uat/prod", false}, {"host_project", "string", "仅 target=host:项目标签值（取值见数据源）", false}, {"host_team", "string", "仅 target=host:团队标签值", false}}, false, true},
	// 云主机不在任何 K8s 集群里，node_usage 那套按 node 标签的口径覆盖不到——问"哪台机器内存快满了"用这个。
	{"host_usage", "云主机(非K8s)用量排行,按内存降序+可按环境/项目/团队筛——问'哪台机器CPU/内存高'用这个,node_usage 只覆盖K8s节点", "/api/obs/host-usage", []mcpParam{{"cluster_id", "integer", "集群ID(用于选数据源)", false}, {"env", "string", "环境 uat/prod", false}, {"project", "string", "项目标签值（取值见数据源）", false}, {"team", "string", "团队标签值", false}}, false, true},
	{"pod_usage", "全 Pod 实时用量(cpu_m/mem_mi)——找吃资源的Pod", "/api/k8s/pod-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	{"node_usage", "全节点实时用量(cpu%/mem%)——找压力大的节点", "/api/k8s/node-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	{"node_capacity", "节点可分配vs已request vs limit——答'节点够不够/还能排多少/超卖'", "/api/k8s/node-capacity", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	{"ns_overview", "命名空间/项目Pod概览:总/Running/失败/Pending+失败原因", "/api/k8s/ns-overview", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"project", "string", "业务项目", false}}, false, true},
	{"pvc_usage", "全PVC使用率(used/cap/pct)——找快满的存储", "/api/k8s/pvc-usage", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	// 告警：接夜莺。排障第一句往往是"现在有什么在响"，
	// 以前要切到夜莺看，现在和资产在同一个 AI 会话里
	{"list_alerts", "夜莺当前活跃告警(未恢复):严重度/对象/规则/触发值/已通知次数。severity=critical|warning|info 筛级别。⚠️返回里 total 是夜莺侧总数、returned 是本次取回数,两者不等说明被 limit 截断了", "/api/alerts", []mcpParam{{"severity", "string", "critical/warning/info,不传=全部", false}, {"limit", "integer", "取回条数上限,默认200", false}}, false, true},
	{"event_center", "事件中心:最近平台出了什么事(到期/变更/同步失败/K8s Warning/告警统一时间线)——排障先看这个。⚠️ days 窗口**不作用于告警类**:告警只取未恢复的(活跃),一条几个月前触发、至今没恢复的仍会出现,它的时间戳是**开始触发的时刻**不是刚发生;超过 48 小时的会在 message 里标「已持续 N 天未恢复」", "/api/k8s/event-center", []mcpParam{{"days", "integer", "天数窗口,默认30", false}, {"source", "string", "expiry/change/sync/k8s", false}, {"level", "string", "critical/warning/info", false}}, false, true},
	{"query_loki", "Loki 日志检索(LogQL),跨Pod/历史深查。事件历史也在这里:event-exporter 把 K8s 事件以结构化 JSON 打到 stdout,可用 | json | type=\"Warning\" | reason=\"OOMKilling\" 过滤", "/api/obs/loki", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"query", "string", "LogQL", true}, {"minutes", "integer", "时间窗", false}, {"step", "string", "聚合查询步长,如 1m,不传自动选", false}}, false, true},
	{"query_prometheus", "通用 PromQL 查询,中间件指标(Kafka积压/nacos实例/etcd延迟/Harbor配额)全靠它。多集群共享数据源时须在标签里写 $CLUSTER 占位符做隔离,如 up{$CLUSTER};传 minutes 则查区间看趋势", "/api/obs/prom-query", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"query", "string", "PromQL,标签里可用 $CLUSTER 占位", true}, {"minutes", "integer", "时间窗(分钟),不传=瞬时值", false}, {"step", "string", "区间查询步长,如 1m,不传自动选", false}}, false, true},
	{"prom_metrics", "发现 Prometheus 里有哪些指标(按关键字过滤)。不知道指标名时先用它,再用 query_prometheus", "/api/obs/prom-metrics", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"keyword", "string", "关键字,如 kafka/nacos/etcd/harbor", false}, {"limit", "integer", "返回上限,默认200", false}}, false, true},
	{"prom_labels", "列某标签的可选值(如 namespace/job/instance),搞清楚指标能按什么维度筛", "/api/obs/prom-labels", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"label", "string", "标签名", true}, {"metric", "string", "限定到某指标", false}}, false, true},
	{"list_cdn_rules", "CDN 规则台账(Page Rules + Rulesets):缓存/强制HTTPS/WAF自定义规则。排查'为什么这个路径没走缓存'用它", "/api/cdn/rules", []mcpParam{{"zone", "string", "根域名", false}, {"source", "string", "pagerule 或 ruleset", false}}, false, true},
	{"cdn_rule_analysis", "CDN 规则优化分析:数量逼近套餐上限/禁用规则占额度/同一匹配式重复配置(只有优先级最高的生效)", "/api/cdn/rule-analysis", []mcpParam{{"zone", "string", "根域名", false}}, false, true},
	{"cdn_token_check", "CDN token 权限体检:**实时**调 Cloudflare 逐项探测(不读库快照),报出 token id/有效期/每项能力通不通/不通缺哪条控制台权限。改完 CF 权限先用它验证——其它 cdn_* 接口读的是上次同步的快照,会把'没重新同步'误判成'权限没生效'", "/api/cdn/token-check", []mcpParam{{"zone", "string", "根域名,不传则取第一个站点", false}, {"account_id", "integer", "CDN账号ID,不传则全部账号都探", false}}, false, true},
	{"cdn_traffic", "CDN 逐条请求时序(**实时**查 Cloudflare):关键字段 edge_start_cst = **CF边缘开始处理该请求的时刻**,外加回源耗时/边缘与源站状态码/RayID。排查'对方说请求发出很久我们才收到'时,用它把总延迟切成两段:边缘时刻≈我方应用收到时刻→慢在到达CF之前(对端出网);边缘时刻≈对方发出时刻→慢在CF内部滞留。安全事件只能证明'有没有被拦',这个才能证明'有没有被压着'", "/api/cdn/traffic", []mcpParam{{"zone", "string", "根域名,如 g32cf.com", true}, {"host", "string", "主机名过滤", false}, {"path", "string", "路径精确匹配,如 /api/wallet/transactions/callback", false}, {"client_ip", "string", "客户端IP过滤", false}, {"since", "string", "起始时间(北京时间),如 2026-08-14 16:53:00", false}, {"until", "string", "结束时间(北京时间)", false}, {"minutes", "integer", "时间窗(分钟),默认60;传了 since 则忽略", false}, {"min_origin_ms", "integer", "只看回源耗时超过该毫秒数的请求", false}, {"limit", "integer", "返回条数上限,默认100", false}}, false, true},
	{"cdn_security_events", "CDN 边缘安全事件(**实时**查 Cloudflare,不读库):某请求有没有被 block/managed_challenge/限速拦下,含命中的规则ID与来源。规则台账只能说'规则怎么配',这个才能说'有没有真命中过'——排查'对方说访问我们超时'时,用它把责任分到对端出网还是我方CDN。时间用北京时间,支持查历史窗口", "/api/cdn/security-events", []mcpParam{{"zone", "string", "根域名,如 g32cf.com", true}, {"host", "string", "主机名过滤,如 openapi-gateway.g32-prod.com", false}, {"since", "string", "起始时间(北京时间),如 2026-08-14 15:00:00", false}, {"until", "string", "结束时间(北京时间),不传=现在", false}, {"minutes", "integer", "时间窗(分钟),默认60;传了 since 则忽略", false}, {"limit", "integer", "返回条数上限,默认200", false}}, false, true},
	{"list_cdn_certificates", "CDN 边缘证书(Cloudflare Universal SSL 等)+到期天数。与 list_certificates(我方源站证书)是两套,到期时间互相独立", "/api/cdn/certificates", []mcpParam{{"zone", "string", "根域名", false}}, false, true},
	{"cloud_iam_audit", "GCP 项目权限审计:谁对项目有什么角色,标出过宽权限(owner/editor/可自行提权)与 allUsers 公开授权。only=issues 只看有风险的", "/api/cloud-iam", []mcpParam{{"project", "string", "项目ID", false}, {"only", "string", "issues=只看有风险的", false}}, false, true},
	{"list_cloud_dns", "GCP Cloud DNS 托管区与解析记录——与 list_cdn_dns(Cloudflare) 是两套解析,排查'改了没生效'时两边都要看", "/api/cloud-dns", []mcpParam{{"project", "string", "项目ID", false}, {"q", "string", "关键词", false}}, false, true},
	{"domain_quality", "域名台账 vs **客户端实际拿到的东西**对账（读现成的 blackbox 拨测，不自建拨测）。三个桶各答一个问题:matched=拨测到了台账也有(可对账,带客户端实际拿到的证书到期日) / unledgered=拨测到了但台账没有(**失管域名**) / unprobed=台账有解析但没有任何拨测(**监控盲区**)。⚠️ cert_days_left 是客户端真实拿到的那张证书,不是台账登记的 —— 两者不一致就是「证书换了但没生效」", "/api/domains/quality", nil, false, true},
	{"dns_consistency", "三方(GoDaddy/Cloudflare/GCP Cloud DNS)解析一致性,并按域名的 NS 判定哪一方才生效:conflicts=目标不一致 / ineffective=配了但 NS 没指向这边(改了没用) / split_ns=NS 同时指向多方。⚠️ unknown_ns_domains>0 表示有域名没采到 NS,ineffective 清单不完整,不能反过来当成「其余的都生效」", "/api/dns-consistency", nil, false, true},
	{"security_audit", "容器安全审计:特权容器/hostPath/hostNetwork/capabilities/以root运行。默认隐藏平台组件(CNI/CSI等特权是设计使然),include_platform=1 可全列", "/api/k8s/security-audit", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "命名空间", false}, {"include_platform", "string", "1=含平台组件", false}}, false, true},
	{"harbor_status", "Harbor 健康+存储用量+GC 状态。ImagePullBackOff 时用它区分「仓库挂了」和「凭证缺失(config_audit)」", "/api/harbor/status", []mcpParam{{"registry_id", "integer", "接入ID,不传用第一个启用的", false}}, false, true},
	{"harbor_projects", "Harbor 项目+配额用量(按用量比倒序,快满的在前)。推送失败先看这个", "/api/harbor/projects", []mcpParam{{"registry_id", "integer", "接入ID", false}}, false, true},
	{"harbor_repositories", "某项目下的镜像仓库(tag数/拉取数/最后推送时间),确认镜像到底推上去没有", "/api/harbor/repositories", []mcpParam{{"registry_id", "integer", "接入ID", false}, {"project", "string", "项目名", true}}, false, true},
	{"kubesphere_fetch", "拉 KubeSphere 原始 kapis 数据(兜底用;查流水线优先用 pipeline_runs/pipeline_log)", "/api/obs/kubesphere", []mcpParam{{"cluster_id", "integer", "集群ID", false}, {"env", "string", "环境", false}, {"path", "string", "kapis 路径", true}}, false, true},
	// Jenkins 的编译输出不进 pod stdout、Loki 也采不到，pod_logs/query_loki 都看不到构建失败原因，只能走这两个。
	{"list_devops_projects", "列 DevOps 项目命名空间(带 ConfigMap 数)。⚠️ pipeline_runs 的 namespace 是必填的,先用这个拿到有哪些项目,别自己按名字猜", "/api/devops/projects", []mcpParam{{"cluster_id", "integer", "集群ID", true}}, false, true},
	{"pipeline_runs", "列流水线运行记录(默认只列失败的):哪条流水线/第几次构建挂了+Jenkins构建号——问'构建失败'先用这个", "/api/devops/pipeline-runs", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "DevOps项目命名空间,形如 test-test-devopsj2q22", true}, {"pipeline", "string", "只看某条流水线", false}, {"only_failed", "string", "0=含成功的,默认只列失败", false}}, false, true},
	{"pipeline_log", "构建日志+自动抽出报错行:直接回答'这次构建为什么失败'(编译报错/测试失败/镜像推送失败都在这)", "/api/devops/pipeline-log", []mcpParam{{"cluster_id", "integer", "集群ID", true}, {"namespace", "string", "DevOps项目命名空间", true}, {"pipeline", "string", "流水线名", true}, {"run", "string", "Jenkins构建号(从 pipeline_runs 拿)", true}, {"tail", "integer", "尾部行数,默认60", false}, {"full", "string", "1=返回全文(可能很大)", false}}, false, true},
}

// ---- JSON-RPC ----

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (h *MCPHandler) RPC(c *gin.Context) {
	var enabled int
	_ = h.DB.QueryRow(`SELECT enabled FROM mcp_config WHERE id=1`).Scan(&enabled)
	got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if got == "" {
		got = c.GetHeader("X-MCP-Token")
	}
	tokenID, tokenName, tokenRole, ok := h.resolveToken(got)
	if enabled == 0 || !ok {
		// 不区分"总开关关了"和"令牌不对"：区分了就等于送给对方一个
		// 探测接口（拿任意串去试，能分辨出令牌错还是服务没开）
		logx.J("mcp", "auth_failed", map[string]any{
			"ip": c.ClientIP(), "enabled": enabled == 1, "hint": safeHint(got),
		})
		httpx.FailKey(c, httpx.CodeUnauthorized, "error.mcpTokenInvalid", nil, nil)
		return
	}
	h.touchToken(tokenID, c.ClientIP())
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
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"tools": h.toolSchemas(tokenRole)}))
	case "tools/call":
		h.callTool(c, req, tokenName, tokenRole)
	default:
		c.JSON(http.StatusOK, rpcErr(req.ID, -32601, "method not found: "+req.Method))
	}
}

// roleCanCall 这个角色能不能调这个工具。
//
// 判据直接复用路由表的 resolvePerm，而不是另立一张"工具→权限"的表：
// 两张表迟早会分叉，分叉的表现是清单里有、调用时被拒（或者更糟，反过来）。
//
// resolvePerm 返回 !ok 表示没有任何规则覆盖这条路由。这种情况**按不可调处理**并告警：
// 调用时它一样会被拒（resolvePerm 的契约就是"没规则则拒绝"），
// 列出来只会让 AI 白撞一次。但这是配置漏了，得有人知道。
func (h *MCPHandler) roleCanCall(t mcpTool, perms map[string]bool, role string) bool {
	code, ok := resolvePerm("GET", t.Path)
	if !ok {
		logx.J("mcp", "tool_perm_unmapped", map[string]any{
			"level": "WARN", "tool": t.Name, "path": t.Path, "role": role,
			"msg": "该工具的路由没有权限规则覆盖，已从 tools/list 隐藏；请在 perm.go 补规则",
		})
		return false
	}
	if code == "" {
		return true // 公共接口，登录即可
	}
	// 权限码允许写成 "a,b,c" 表示任一命中即可，与 HasPerm 的语义保持一致
	for _, one := range strings.Split(code, ",") {
		if one = strings.TrimSpace(one); one != "" && perms[one] {
			return true
		}
	}
	return false
}

// toolSchemas 返回**这个令牌能用的**工具清单。
//
// 过滤有两条轴，缺一不可：
//   - 授权分档（EE）：没买就不列
//   - 令牌角色：角色没有对应菜单权限的，也不列
//
// ⚠️ 第二条轴以前是漏的。当时只按分档过滤，理由写在下面那段注释里，
// 却没意识到它对角色同样成立：一个 cmdb_viewer 令牌照样看得见全部 78 个工具，
// 调 cost_overview 才被拒。AI 会照着清单规划排查路径，撞上"没有操作权限"，
// 然后要么重试要么把它当成故障报给人——而这只是它本来就不该看见的工具。
func (h *MCPHandler) toolSchemas(role string) []gin.H {
	full := h.fullAllowed()
	// 角色为空 = 升级前的老令牌，不受限（见 internal_auth.go 的同款判断，
	// 两边必须一致：列表里给的和调用时放行的得是同一批）
	perms, unrestricted := permsOfLocalRole(h.DB, role)
	out := []gin.H{}
	for _, t := range mcpTools {
		// 没买全量就不在清单里出现。
		//
		// ⚠️ 关键是**不列出来**，而不是列出来再拒绝：AI 会照着清单去调，
		// 每次都撞一个"没有授权"，它会把这理解成系统故障并反复重试。
		// 看不见的工具它不会去想。
		if t.EE && !full {
			continue
		}
		if !unrestricted && !h.roleCanCall(t, perms, role) {
			continue
		}
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

func (h *MCPHandler) callTool(c *gin.Context, req rpcReq, actor, role string) {
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
	// 直接按名字调一个没在清单里的 EE 工具。
	// 这里的措辞要让人能分清"没买"和"坏了"——两者的下一步完全不同
	if tool.EE && !h.fullAllowed() {
		logx.J("mcp", "tool_not_licensed", map[string]any{"tool": p.Name, "actor": actor})
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"content": []gin.H{{"type": "text",
			"text": "工具 " + p.Name + " 属于企业版功能，当前授权未包含。这不是故障，数据也没有问题——" +
				"社区版提供资产清单类工具，诊断/成本/日志检索类需要企业版授权。"}},
			"isError": true}))
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
	callStart := time.Now()
	toolTimeout := 30 * time.Second
	if slowTools[tool.Name] {
		toolTimeout = slowToolTimeout
	}
	body, err := h.internalGetTimeout(tool.Path, q, actor, role, toolTimeout)
	logx.J("mcp", "tool_call", map[string]any{
		"tool": p.Name, "args": p.Arguments, "err": errStr(err),
		// 记是谁调的：一堆 AI 调用混在一起时，要能查出哪条接入在做什么
		"actor": actor, "role": role,
	})
	// 标成 mcp：这是 AI 用机器身份调的，不是某个人在页面上点的，
	// 审计里必须能一眼分开。
	// ⚠️ 这里原本写着"MCP token 不受 RBAC 约束"——那是加逐令牌角色之前的事实，
	// 现在它受约束了。过期的注释和错的注释一样会被人照着做决定
	c.Set(ctxAuthSource, "mcp")
	// ⚠️ 用 WriteAuditMCP 而不是 WriteAudit：后者的操作者取自 c.Get("username")，
	// 而 MCP 路径下没人 Set 过它 —— 于是审计里 mcp_tool 那些行操作者全是空的，
	// 多个接入方共用出口 IP 时就分不出是谁调的（P1-72）。耗时同理（P2-65）。
	WriteAuditMCP(h.DB, c, "mcp_tool:"+p.Name, tool.Path, actor, time.Since(callStart))
	if err != nil {
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"content": []gin.H{{"type": "text", "text": "调用失败: " + err.Error()}}, "isError": true}))
		return
	}
	body = h.hintIfNarrowedToEmpty(tool, q, p.Arguments, body, actor, role)
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
	// 🔴 记下**哪些字段名根本不存在**。
	//
	//	原来无法识别的字段名被静默丢弃：
	//	  fields="domain,cn,expires_at,expire_at,issuer,days,check_error,source"
	//	→ 只有 domain 命中，其余 7 个全丢，且**没有任何提示**。
	//	于是调用方（AI）拿到一堆只有 domain 的记录，
	//	会得出「这些证书没有到期日」的结论 —— 而真相是字段名写错了
	//	（OPSCMDB-031 P1-8；早先在域名页也撞到过同一件事：
	//	请求 `domain` 而实际字段叫 `name`，整列为空）。
	//
	//	⚠️ 这正是本项目最核心的失效模式：**不报错、不空、只是答非所问**。
	//	静默丢弃让"写错了"和"这个字段真的没值"看起来一模一样。
	available := map[string]bool{}
	for _, r := range rows {
		for k := range r {
			available[k] = true
		}
	}
	unknown := make([]string, 0, len(keep))
	for k := range keep {
		if len(rows) > 0 && !available[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)

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
	if len(unknown) > 0 {
		// 把结果包一层，带上「这几个字段名不存在」和「这个资源有哪些字段」。
		//
		//	⚠️ 包一层会改变响应形状，但**只在出问题时**才包 ——
		//	正常调用（字段名全对）的形状一个字都不变。
		//	代价是调用方遇到这种响应要多解析一层，
		//	而收益是它再也不会把"字段名写错"读成"这些数据是空的"。
		avail := make([]string, 0, len(available))
		for k := range available {
			avail = append(avail, k)
		}
		sort.Strings(avail)
		wrapped, err := json.Marshal(map[string]any{
			"items":            out,
			"unknown_fields":   unknown,
			"available_fields": avail,
			"warning": fmt.Sprintf(
				"请求的字段里有 %d 个在这个资源上不存在：%s。它们**没有被返回**——"+
					"不要把结果里缺这些字段读成「这些数据是空的」。可用字段见 available_fields。",
				len(unknown), strings.Join(unknown, ", ")),
		})
		if err == nil {
			return string(wrapped)
		}
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
func (h *MCPHandler) hintIfNarrowedToEmpty(tool *mcpTool, q url.Values, args map[string]any, body, actor, role string) string {
	if tool.Text || strings.TrimSpace(body) != "[]" {
		return body
	}
	narrowed := narrowingArgs(args)
	if len(narrowed) == 0 {
		// 没加任何过滤却是空 —— 此时最常见的原因不是"真没有"，
		// 而是"这类数据依赖的外部数据源根本没接"。区分这两者见下
		return h.hintIfSourceMissing(tool, body)
	}
	wide := url.Values{}
	if cid := q.Get("cluster_id"); cid != "" {
		wide.Set("cluster_id", cid) // cluster_id 是定位不是过滤，保留
	}
	// ⚠️ 放宽重查也必须带同一个身份。用不受限身份去探，
	// 会得到"其实有数据，是你筛没了"的提示——而对这个调用方来说
	// 那些数据本来就看不到，提示等于泄露了它不该知道的存在性
	wideBody, err := h.internalGet(tool.Path, wide, actor, role)
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
func (h *MCPHandler) internalGet(path string, q url.Values, actor, role string) (string, error) {
	return h.internalGetTimeout(path, q, actor, role, 30*time.Second)
}

// internalGetTimeout 同上，但由调用方决定超时。慢工具（见 mcpTool.Slow）用更长的。
func (h *MCPHandler) internalGetTimeout(path string, q url.Values, actor, role string, timeout time.Duration) (string, error) {
	// 带进程内回调令牌，不再自签 JWT。
	//
	//	原先这里签一个 2 分钟的 JWT 混过鉴权中间件。后来鉴权改成会话表之后
	//	这条路就断了（进程内回调从不写会话表），MCP 全部工具返回"登录已失效"。
	//	现在用一个只存在于内存、随进程重启失效的令牌，见 internal_auth.go。
	u := "http://127.0.0.1" + h.port + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	rq, _ := http.NewRequest("GET", u, nil)
	rq.Header.Set("Authorization", "Bearer "+internalToken)
	// 这次以谁的身份调。这两个头只在内部令牌校验通过后才被读取
	// （见 internal_auth.go），外部请求带上它们没有任何作用。
	//
	// ⚠️ 角色为空时**不要**发这个头：空值会被当成"没声明"，
	// 走的是兼容老令牌的不受限分支；发一个空字符串反而语义不明
	if role != "" {
		rq.Header.Set(internalRoleHeader, role)
	}
	if actor != "" {
		rq.Header.Set(internalActorHeader, "mcp:"+actor)
	}
	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(rq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	// ⚠️ 状态码必须看。
	//
	// 原来这里无条件 return string(b), nil —— 内部接口回 403「没有操作权限」
	// 或者 500「查询超时」，body 都被当成**成功的工具结果**交给 AI。
	// AI 收到的是一次 isError 未置位的正常返回，内容恰好是个 JSON 错误对象；
	// 它多半会把这理解成"查到了，但是空的"，然后向人转述"该项没有数据"。
	//
	// 这正是整个产品要消灭的那个反模式：**失败被渲染成正常**。
	// 差别只在于这次的受害者是 AI 不是人，而 AI 更不会去追问。
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("内部接口 %s 返回 HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(truncate(string(b), 300)))
	}
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

// safeHint 日志里给出令牌的前几位，便于对上是哪一条在报错，又不泄露它。
//
// ⚠️ 太短认不出、太长等于泄露。6 位在这两者之间：
// 令牌是 48 位十六进制，露 6 位剩下的空间仍然远超暴力范围。
func safeHint(tok string) string {
	if len(tok) <= 6 {
		return "***"
	}
	return tok[:6] + "…"
}

// sourceBackedTools 哪些工具的数据依赖外部数据源接入。
//
// key = 工具名，value = (检查哪张表有没有启用中的记录, 缺了该怎么说)
//
// 🔴 为什么需要：这些工具在没接数据源时返回的是**裸空数组**，
// 和"接了、同步了、确实 0 条"长得一模一样。生产实测：list_domains 与
// list_certificates 都返回 []，而真实原因是根本没有注册商和 CDN 账号——
// 读的人只会得出"我们没有域名"这个完全错误的结论。
//
// ⚠️ 界面上其实是对的（全局态势把域名标成「未接入」），说明这个信息后端本来就有，
// 只是没通过 MCP 暴露。两条链路对同一件事给出不同答案，是本项目反复出现的问题。
var sourceBackedTools = map[string]struct{ table, hint string }{
	"list_domains": {"registrars",
		"还没有接入任何域名注册商，所以域名台账是空的。⚠️ 这不等于「没有域名」。" +
			"去「管理 → 注册商」接入（GoDaddy 等），接入时建议先开预演模式"},
	"list_certificates": {"registrars",
		"还没有接入注册商 / CDN 账号，证书巡检没有数据来源。⚠️ 这不等于「证书都正常」"},
	"list_cdn_zones": {"cdn_accounts",
		"还没有接入 CDN 账号（Cloudflare），所以站点列表是空的。" +
			"去「域名与入口 → CDN 站点 → 添加」配 API Token + Account ID"},
	"list_cdn_dns":          {"cdn_accounts", "还没有接入 CDN 账号，取不到 CDN 侧的解析记录"},
	"list_cdn_rules":        {"cdn_accounts", "还没有接入 CDN 账号，取不到规则"},
	"list_cdn_certificates": {"cdn_accounts", "还没有接入 CDN 账号，取不到边缘证书"},
	"harbor_projects":       {"harbor_registries", "还没有接入 Harbor，取不到项目列表"},
}

// hintIfSourceMissing 空结果且没有过滤条件时，检查底层数据源接没接。
//
// 三态：没接数据源 / 接了但没同步 / 确实 0 条 —— 前两者都不能渲染成第三者。
func (h *MCPHandler) hintIfSourceMissing(tool *mcpTool, body string) string {
	src, ok := sourceBackedTools[tool.Name]
	if !ok {
		return body // 不依赖外部数据源的，空就是真的空
	}
	var n int
	// ⚠️ 查询本身失败时不要下任何结论：返回原样，让空就是空，
	// 而不是给出一个基于失败查询的"未接入"论断
	if h.DB.QueryRow("SELECT COUNT(*) FROM "+src.table+" WHERE enabled=1").Scan(&n) != nil {
		return body
	}
	if n > 0 {
		// 接了却是空 —— 那是同步的问题，指向新鲜度而不是接入
		b, err := json.Marshal(gin.H{"items": []any{}, "hint": "数据源已接入但没有数据。" +
			"可能是还没同步过或同步失败——用 data_freshness 看采集状态，或在界面上点「立即同步」看报什么错。" +
			"⚠️ 在确认之前，不要把这个空当成「确实没有」"})
		if err != nil {
			return body
		}
		return string(b)
	}
	b, err := json.Marshal(gin.H{"items": []any{}, "not_ingested": true, "hint": src.hint})
	if err != nil {
		return body
	}
	return string(b)
}
