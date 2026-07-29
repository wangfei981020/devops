package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 暴露面视图：把「谁能从外面访问到什么」这个问题一次性答完。
//
// 为什么要有这个接口：判断一个服务是否公网可达，信息散在 5 张表里——
// Istio VirtualService 的 gateway 命名、K8s Service 的类型与注解、云侧 LB 的 scheme、
// Ingress 的 TLS、域名侧的 443 探测结果。任何一处单独看都不足以下结论，
// 而调用方（尤其 AI）每次自己拼就会拼错：曾经因为只看到 Service type=LoadBalancer
// 却拿不到 scheme，就把「ZooKeeper 2181 是否挂在公网」这种关键问题挂起无法回答。
// 这里做一次收敛，直接给出可采信的判定和判定依据。

// sensitivePortNames 敏感端口 → 组件名，全局唯一的一份口径：
// 要么是管理入口(SSH/RDP)，要么是默认无认证或弱认证的数据组件。
// 这些端口对 0.0.0.0/0 放行、或经公网 LB 暴露，都按高危处理。
// 暴露面判定与防火墙高危判定共用它，避免两处各记一份、结论互相打架。
var sensitivePortNames = map[int]string{
	22: "SSH", 1433: "SQLServer", 2181: "ZooKeeper", 2375: "Docker API", 2379: "etcd",
	3306: "MySQL", 3389: "RDP", 5432: "PostgreSQL", 5672: "RabbitMQ", 5984: "CouchDB",
	6379: "Redis", 6443: "K8s APIServer", 8086: "InfluxDB", 8848: "Nacos",
	9000: "MinIO", 9092: "Kafka", 9094: "Kafka(external)", 9200: "Elasticsearch",
	9848: "Nacos gRPC", 9876: "RocketMQ NameServer", 10250: "kubelet",
	11211: "Memcached", 15672: "RabbitMQ管理台", 27017: "MongoDB",
}

// sensitivePorts 同上，键为端口字符串，供防火墙规则的文本匹配使用。
var sensitivePorts = func() map[string]bool {
	m := make(map[string]bool, len(sensitivePortNames))
	for p := range sensitivePortNames {
		m[strconv.Itoa(p)] = true
	}
	return m
}()

type exposureItem struct {
	Entry         string   `json:"entry"` // 域名，或 VIP:端口
	Kind          string   `json:"kind"`  // VirtualService/Ingress/LoadBalancer/NodePort
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	Exposure      string   `json:"exposure"`       // external/internal/unknown
	ExposureBasis string   `json:"exposure_basis"` // 判定依据，便于人工复核
	TLS           string   `json:"tls"`            // yes/no/unknown
	Ports         string   `json:"ports,omitempty"`
	Backend       string   `json:"backend,omitempty"`
	BackendAlive  string   `json:"backend_alive,omitempty"` // yes/no/unknown
	Severity      string   `json:"severity"`                // high/medium/low
	Risks         []string `json:"risks,omitempty"`
}

// ExposeSurface GET /api/k8s/expose-surface?cluster_id=&only=external|risky
func (h *K8sResourceHandler) ExposeSurface(c *gin.Context) {
	cid := c.Query("cluster_id")
	if cid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 必填"})
		return
	}

	svcAlive := h.loadServiceLiveness(cid) // ns/svcName -> 是否有存活后端
	lbByVIP := h.loadCloudLBsByVIP()       // vip -> scheme（云侧权威内外网）

	items := []exposureItem{}
	items = append(items, h.virtualServiceExposure(cid, svcAlive)...)
	items = append(items, h.ingressExposure(cid, svcAlive)...)
	items = append(items, h.serviceExposure(cid, svcAlive, lbByVIP)...)

	for i := range items {
		items[i].Severity, items[i].Risks = assessExposure(&items[i])
	}
	sort.SliceStable(items, func(i, j int) bool {
		if r := severityRank(items[i].Severity) - severityRank(items[j].Severity); r != 0 {
			return r < 0
		}
		return items[i].Entry < items[j].Entry
	})

	only := c.Query("only")
	if only != "" {
		filtered := items[:0:0]
		for _, it := range items {
			if (only == "external" && it.Exposure == "external") ||
				(only == "risky" && it.Severity != "low") {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	sum := gin.H{"total": len(items), "external": 0, "internal": 0, "unknown": 0, "high": 0}
	for _, it := range items {
		sum[it.Exposure] = sum[it.Exposure].(int) + 1
		if it.Severity == "high" {
			sum["high"] = sum["high"].(int) + 1
		}
	}
	c.JSON(http.StatusOK, gin.H{"summary": sum, "items": items})
}

// loadServiceLiveness 用 Endpoints 判断每个 Service 背后还有没有活着的 Pod。
// 断链的入口（域名还在解析、后端已没了）就是靠这个查出来的。
func (h *K8sResourceHandler) loadServiceLiveness(cid string) map[string]bool {
	alive := map[string]bool{}
	rows, err := h.DB.Query(`SELECT namespace, name FROM k8s_services WHERE cluster_id=?`, cid)
	if err != nil {
		return alive
	}
	for rows.Next() {
		var ns, n string
		if rows.Scan(&ns, &n) == nil {
			alive[ns+"/"+n] = false // 先登记存在，默认无后端
		}
	}
	rows.Close()
	rows, err = h.DB.Query(`SELECT DISTINCT namespace, service_name FROM k8s_endpoints WHERE cluster_id=? AND pod_name<>''`, cid)
	if err != nil {
		return alive
	}
	defer rows.Close()
	for rows.Next() {
		var ns, n string
		if rows.Scan(&ns, &n) == nil {
			alive[ns+"/"+n] = true
		}
	}
	return alive
}

// loadCloudLBsByVIP 云侧 LB 的 scheme 按 VIP 索引。scheme 是内外网的权威来源，
// 优先级高于 K8s 注解——注解只是「申请」，云上实际建成什么样以这里为准。
func (h *K8sResourceHandler) loadCloudLBsByVIP() map[string]string {
	out := map[string]string{}
	rows, err := h.DB.Query(`SELECT vip, scheme FROM cloud_loadbalancers WHERE vip<>'' AND stale=0`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var vip, scheme string
		if rows.Scan(&vip, &scheme) == nil {
			out[vip] = scheme
		}
	}
	return out
}

// istioExposure 按网关命名判内外网。命名规范：*-extra = 外网入口，*-inner = 内网入口。
// 认不出的网关一律返回 unknown——宁可标注"待人工确认"，也不能猜；
// 把公网入口误判成内网，比不给结论危险得多。
func istioExposure(gateways string) (string, string) {
	g := strings.ToLower(gateways)
	switch {
	case strings.Contains(g, "-extra"):
		return "external", "istio gateway 含 -extra（外网入口）: " + gateways
	case strings.Contains(g, "-inner"):
		return "internal", "istio gateway 含 -inner（内网入口）: " + gateways
	case gateways == "":
		return "unknown", "VirtualService 未挂 gateway（仅网格内生效）"
	default:
		return "unknown", "网关命名不符合 -extra/-inner 规范，需人工确认: " + gateways
	}
}

func (h *K8sResourceHandler) virtualServiceExposure(cid string, alive map[string]bool) []exposureItem {
	rows, err := h.DB.Query(`SELECT namespace, name, hosts, gateways, backends FROM k8s_virtualservices WHERE cluster_id=?`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []exposureItem{}
	for rows.Next() {
		var ns, name, hosts, gws, backends string
		if rows.Scan(&ns, &name, &hosts, &gws, &backends) != nil {
			continue
		}
		exp, basis := istioExposure(gws)
		for _, host := range splitCSV(hosts) {
			it := exposureItem{
				Entry: host, Kind: "VirtualService", Namespace: ns, Name: name,
				Exposure: exp, ExposureBasis: basis, TLS: "unknown",
				Backend: backends, BackendAlive: backendAlive(backends, ns, alive),
			}
			// 网关统一终结 TLS，这里无法从 VS 本身判断证书情况，交给域名侧的证书巡检
			out = append(out, it)
		}
	}
	return out
}

func (h *K8sResourceHandler) ingressExposure(cid string, alive map[string]bool) []exposureItem {
	rows, err := h.DB.Query(`SELECT namespace, name, hosts, tls, svc_names FROM k8s_ingresses WHERE cluster_id=?`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []exposureItem{}
	for rows.Next() {
		var ns, name, hosts, tls, svcs string
		if rows.Scan(&ns, &name, &hosts, &tls, &svcs) != nil {
			continue
		}
		tlsState := "no"
		if strings.TrimSpace(tls) != "" {
			tlsState = "yes"
		}
		for _, host := range splitCSV(hosts) {
			out = append(out, exposureItem{
				Entry: host, Kind: "Ingress", Namespace: ns, Name: name,
				Exposure: "external", ExposureBasis: "Ingress 默认经外部入口暴露",
				TLS: tlsState, Backend: svcs, BackendAlive: backendAlive(svcs, ns, alive),
			})
		}
	}
	return out
}

func (h *K8sResourceHandler) serviceExposure(cid string, alive map[string]bool, lbByVIP map[string]string) []exposureItem {
	rows, err := h.DB.Query(`SELECT namespace, name, type, COALESCE(external_ip,''), COALESCE(lb_type,''), ports
		FROM k8s_services WHERE cluster_id=? AND type IN ('LoadBalancer','NodePort')`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	// 节点网络位置查一次即可，全集群共用——NodePort 的可达性取决于节点而非单个 Service
	nodeNet := h.loadNodeNetwork(cid)
	out := []exposureItem{}
	for rows.Next() {
		var ns, name, typ, extIP, lbType, ports string
		if rows.Scan(&ns, &name, &typ, &extIP, &lbType, &ports) != nil {
			continue
		}
		exp, basis := serviceExposureOf(typ, extIP, lbType, lbByVIP, nodeNet)
		entry := extIP
		if entry == "" {
			entry = ns + "/" + name
		}
		a := "no"
		if alive[ns+"/"+name] {
			a = "yes"
		}
		out = append(out, exposureItem{
			Entry: entry, Kind: typ, Namespace: ns, Name: name,
			Exposure: exp, ExposureBasis: basis, TLS: "unknown",
			Ports: ports, Backend: ns + "/" + name, BackendAlive: a,
		})
	}
	return out
}

// serviceExposureOf 内外网判定，按可信度从高到低取证：

// nodeNetwork 集群节点的网络位置，NodePort 判定的依据。
//
// 为什么需要它：GKE 上 Service 走云 LB，cloud_loadbalancers.scheme 是权威依据；
// 而 k3s/自建集群大量用 NodePort，云侧没有任何记录，此前一律判 unknown——
// DEV 集群 502 条里 500 条如此（CMDB-009），等于这个功能在一半环境上是瞎的。
type nodeNetwork struct {
	total      int
	withPublic int      // 有公网 IP 的节点数
	sampleIPs  []string // 举例用，最多几个
	declared   string   // 集群上人工声明的 network_exposure：public/private/空
}

// nodePortExposure 判定 NodePort 的可达性。
//
// 判定链（依据强度从高到低）：
//  1. 节点有公网 IP —— NodePort 在公网可达是确定的（K8s 会在每个节点上监听该端口）
//  2. 人工声明 network_exposure —— 节点无公网 IP 时用它兜底
//  3. 节点全无公网 IP 且未声明 —— 判 internal，但写明依据和例外（NAT/端口转发）
//
// 第 3 条以前是 unknown。改成 internal + 说明理由，是因为「无公网 IP 的节点上的 NodePort
// 走不到公网」在绝大多数部署下成立，给出带依据的结论比一句「取决于防火墙」有用得多；
// 而例外情况也一并写出来，不假装确定。
func nodePortExposure(n nodeNetwork) (string, string) {
	switch {
	case n.withPublic > 0:
		ips := ""
		if len(n.sampleIPs) > 0 {
			ips = "（如 " + strings.Join(n.sampleIPs, ", ") + "）"
		}
		return "external", fmt.Sprintf("%d/%d 个节点有公网 IP%s，NodePort 在这些节点上公网可达；"+
			"实际是否放行还要看防火墙（list_firewalls）", n.withPublic, n.total, ips)
	case strings.EqualFold(n.declared, "public"):
		return "external", "集群被声明为 public（节点可从公网访问），NodePort 视为公网可达"
	case strings.EqualFold(n.declared, "private"):
		return "internal", "集群被声明为 private（仅内网），NodePort 只在内网可达"
	case n.total == 0:
		return "unknown", "集群没有节点数据，无法判断 NodePort 可达性——先确认节点采集是否正常"
	default:
		return "internal", fmt.Sprintf("%d 个节点全部没有公网 IP，NodePort 只在内网可达。"+
			"例外：若存在 NAT/端口转发/前置负载均衡，仍可能从公网到达——"+
			"这种情况请在集群上设置「网络位置」为 public 以覆盖此判定", n.total)
	}
}

// loadNodeNetwork 汇总集群节点的公网 IP 情况与人工声明。
func (h *K8sResourceHandler) loadNodeNetwork(cid string) nodeNetwork {
	n := nodeNetwork{}
	rows, err := h.DB.Query(`SELECT COALESCE(external_ip,'') FROM k8s_nodes WHERE cluster_id=?`, cid)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ip string
			if rows.Scan(&ip) != nil {
				continue
			}
			n.total++
			if strings.TrimSpace(ip) != "" {
				n.withPublic++
				if len(n.sampleIPs) < 3 {
					n.sampleIPs = append(n.sampleIPs, ip)
				}
			}
		}
	}
	_ = h.DB.QueryRow(`SELECT COALESCE(network_exposure,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&n.declared)
	return n
}

// 云侧 LB 的 scheme > K8s 内网 LB 注解 > 有无外部 IP。
func serviceExposureOf(typ, extIP, lbType string, lbByVIP map[string]string, nodeNet nodeNetwork) (string, string) {
	for _, ip := range splitCSV(extIP) {
		if scheme, ok := lbByVIP[ip]; ok {
			if strings.EqualFold(scheme, "INTERNAL") {
				return "internal", "云侧 LB scheme=INTERNAL (VIP " + ip + ")"
			}
			return "external", "云侧 LB scheme=" + scheme + " (VIP " + ip + ")"
		}
	}
	if lbType != "" && strings.EqualFold(lbType, "Internal") {
		return "internal", "Service 带内网 LB 注解: " + lbType
	}
	if typ == "NodePort" {
		return nodePortExposure(nodeNet)
	}
	if extIP == "" {
		return "unknown", "LoadBalancer 尚未分配外部 IP（可能正在创建或创建失败）"
	}
	return "unknown", "已分配外部 IP " + extIP + " 但云侧 LB 台账里没有对应记录，需人工确认内外网"
}

// assessExposure 给单条入口定风险等级。判定要保守：能确证的才算 high，存疑的算 medium。
func assessExposure(it *exposureItem) (string, []string) {
	risks := []string{}
	external := it.Exposure == "external"

	// 端口只能提示「疑似」，不能坐实组件：9000 既可能是 MinIO 也可能是普通业务端口。
	// 所以措辞一律写成待确认，避免调用方把推测当结论去报障。
	if svc, port := firstSensitivePort(it.Ports); svc != "" {
		msg := "开放端口 " + strconv.Itoa(port) + "（" + svc + " 默认端口，此类组件通常无认证或弱认证）；端口号不足以确定实际组件，需人工确认"
		switch it.Exposure {
		case "external":
			risks = append(risks, "【高危】公网"+msg)
		case "internal":
			risks = append(risks, "内网"+msg)
		default:
			// 不确定是否公网可达 + 敏感端口 → 按最坏情况处理，宁可多查一次
			risks = append(risks, "【高危】"+msg+"，且内外网属性未确认，不能排除公网可达")
		}
	}
	if external && it.TLS == "no" {
		risks = append(risks, "【高危】公网入口未配置 TLS，流量明文传输")
	}
	if it.BackendAlive == "no" {
		risks = append(risks, "入口指向的后端没有存活实例，访问会得到 5xx（断链残留）")
	}
	if it.Exposure == "unknown" {
		risks = append(risks, "内外网属性无法自动判定，需人工确认："+it.ExposureBasis)
	}

	sev := "low"
	for _, r := range risks {
		if strings.HasPrefix(r, "【高危】") {
			sev = "high"
			break
		}
		sev = "medium"
	}
	return sev, risks
}

// firstSensitivePort 从 "6379/TCP,8848/TCP" 里找出第一个敏感端口。
func firstSensitivePort(ports string) (string, int) {
	for _, p := range splitCSV(ports) {
		numStr := p
		if i := strings.IndexByte(p, '/'); i > 0 {
			numStr = p[:i]
		}
		n, err := strconv.Atoi(strings.TrimSpace(numStr))
		if err != nil {
			continue
		}
		if name, ok := sensitivePortNames[n]; ok {
			return name, n
		}
	}
	return "", 0
}

// backendAlive 判断后端串（可能是 "svc.ns.svc.cluster.local" 或 "svc1,svc2"）里是否至少有一个活着。
func backendAlive(backends, ns string, alive map[string]bool) string {
	list := splitCSV(backends)
	if len(list) == 0 {
		return "unknown"
	}
	known := false
	for _, b := range list {
		name, bns := b, ns
		if i := strings.IndexByte(b, '.'); i > 0 { // svc.ns.svc.cluster.local
			name = b[:i]
			rest := b[i+1:]
			if j := strings.IndexByte(rest, '.'); j > 0 {
				bns = rest[:j]
			} else if rest != "" {
				bns = rest
			}
		}
		if v, ok := alive[bns+"/"+name]; ok {
			known = true
			if v {
				return "yes"
			}
		}
	}
	if !known {
		return "unknown" // Service 都不在本集群（可能是外部服务/ServiceEntry）
	}
	return "no"
}

func severityRank(s string) int {
	switch s {
	case "high":
		return 0
	case "medium":
		return 1
	default:
		return 2
	}
}
