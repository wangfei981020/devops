package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// K8sTopologyHandler 全链路关系（按需计算，不物化 ci_relations）+ 反向影响分析。
type K8sTopologyHandler struct {
	DB *sql.DB
}

func NewK8sTopologyHandler(db *sql.DB) *K8sTopologyHandler {
	return &K8sTopologyHandler{DB: db}
}

func (h *K8sTopologyHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/topology", h.Topology)            // 正向：域名 → CDN/证书/Ingress/Service/工作负载/Pod/节点/集群
	r.GET("/k8s/impact", h.Impact)                // 反向：节点 → 受影响的 Service/工作负载/Ingress/域名
	r.GET("/k8s/topology-domains", h.TopoDomains) // 全链路域名候选(可搜下拉):解析FQDN + K8s入口hosts,带项目/环境/模块
}

// TopoDomains 返回可查链路的域名候选(带项目/环境/模块),供全链路下拉。
func (h *K8sTopologyHandler) TopoDomains(c *gin.Context) {
	type d struct {
		Name    string `json:"name"`
		Project string `json:"project"`
		Env     string `json:"env"`
		Module  string `json:"module"`
	}
	seen := map[string]*d{}
	// 解析记录 FQDN(带台账项目/环境/模块)
	if rows, _ := h.DB.Query(`SELECT DISTINCT r.host, COALESCE(c.project,''), COALESCE(c.env,''), COALESCE(c.module,'')
		FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id WHERE r.host<>''`); rows != nil {
		for rows.Next() {
			x := &d{}
			if rows.Scan(&x.Name, &x.Project, &x.Env, &x.Module) == nil && x.Name != "" {
				seen[x.Name] = x
			}
		}
		rows.Close()
	}
	// K8s 入口 hosts(VS/Ingress/HTTPRoute)——可能不在台账,补进来(allow-create 也能自输)。
	//
	// 项目/环境不能留空：这两个字段是界面上的筛选器，全空时下拉显示 "No data"，
	// 等于筛选功能不可用。而它们本来就推得出来——环境=集群的 environment，
	// 项目=该命名空间的项目归属(k8s_ns_project)。以前只填了 Name，白扔了这两层信息。
	for _, q := range []string{
		`SELECT v.hosts, COALESCE(k.environment,''), COALESCE(p.project,'')
		   FROM k8s_virtualservices v
		   LEFT JOIN k8s_clusters k ON k.id=v.cluster_id
		   LEFT JOIN k8s_ns_project p ON p.cluster_id=v.cluster_id AND p.namespace=v.namespace`,
		`SELECT i.hosts, COALESCE(k.environment,''), COALESCE(p.project,'')
		   FROM k8s_ingresses i
		   LEFT JOIN k8s_clusters k ON k.id=i.cluster_id
		   LEFT JOIN k8s_ns_project p ON p.cluster_id=i.cluster_id AND p.namespace=i.namespace`,
		`SELECT r.hostnames, COALESCE(k.environment,''), COALESCE(p.project,'')
		   FROM k8s_httproutes r
		   LEFT JOIN k8s_clusters k ON k.id=r.cluster_id
		   LEFT JOIN k8s_ns_project p ON p.cluster_id=r.cluster_id AND p.namespace=r.namespace`,
	} {
		rows, err := h.DB.Query(q)
		if err != nil {
			logx.J("topology", "topo_domains_query_fail", map[string]any{
				"err": err.Error(), "warn": "该来源的域名候选本次缺失，界面下拉会少一部分选项",
			})
			continue
		}
		for rows.Next() {
			var hs, env, proj string
			_ = rows.Scan(&hs, &env, &proj)
			for _, host := range splitCSV(hs) {
				if host == "*" || host == "" {
					continue
				}
				if ex, ok := seen[host]; ok {
					// 台账里已有的以台账为准，只补台账缺的那几项
					if ex.Env == "" {
						ex.Env = env
					}
					if ex.Project == "" {
						ex.Project = proj
					}
					continue
				}
				seen[host] = &d{Name: host, Env: env, Project: proj}
			}
		}
		rows.Close()
	}
	out := make([]d, 0, len(seen))
	for _, v := range seen {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	c.JSON(http.StatusOK, out)
}

// Topology 正向全链路：给一个域名，串出它到 Pod/节点的整条链。
func (h *K8sTopologyHandler) Topology(c *gin.Context) {
	domain := strings.TrimSpace(c.Query("domain"))
	if domain == "" {
		c.JSON(400, gin.H{"error": "domain 必填"})
		return
	}
	out := gin.H{"domain": domain}

	// 边缘接入（CDN/回源/源站/证书）——从解析记录取该 FQDN 一行，口径与解析明细页一致。
	// 有 CDN → 前端显 CDN厂商+回源CNAME；无 CDN → 显源站IP(origin_ip)。优先取有 CDN/证书的记录。
	out["edge"] = h.oneRow(`SELECT COALESCE(cd.name,'') AS cdn, r.cname, r.origin_ip, r.cert_expiry_at AS cert_expiry
		FROM domain_records r LEFT JOIN cdns cd ON cd.id=r.cdn_id
		WHERE r.host=? ORDER BY (r.cdn_id IS NOT NULL) DESC, (r.cert_expiry_at IS NOT NULL) DESC LIMIT 1`, domain)
	out["cdns"] = h.strList(`SELECT DISTINCT cd.name FROM domain_records r JOIN cdns cd ON cd.id=r.cdn_id WHERE r.cdn_id>0 AND r.host=?`, domain) // 兼容旧字段

	// 真正的 CDN 侧数据在 cdn_dns_records（Cloudflare 只读接入采来的），
	// 上面两行查的是旧域名台账 domain_records+cdns，那套很多域名压根没登记，
	// 所以 cdns/edge 长期是空的——不是没数据，是查错了地方。
	out["cdn"] = h.cdnFacts(domain)

	// 域名台账（项目/环境/模块）——供前端展示归属
	out["domain_info"] = h.oneRow(`SELECT c.project, c.env, c.module
		FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain' AND c.name=? LIMIT 1`, domain)

	// 匹配 Ingress + HTTPRoute + Istio VirtualService（hosts/hostnames 含该域名）
	chains := []gin.H{}
	// Ingress
	rows, err := h.DB.Query(`SELECT cluster_id,namespace,name,hosts,tls,svc_names FROM k8s_ingresses WHERE hosts LIKE ?`, "%"+domain+"%")
	if err == nil {
		for rows.Next() {
			var cid int
			var ns, name, hosts, tls, svcNames string
			_ = rows.Scan(&cid, &ns, &name, &hosts, &tls, &svcNames)
			if !hostMatch(hosts, domain) {
				continue
			}
			chains = append(chains, h.buildChain(cid, ns, "Ingress", name, tls, svcNames))
		}
		rows.Close()
	}
	// HTTPRoute
	rows2, err := h.DB.Query(`SELECT cluster_id,namespace,name,hostnames,backends FROM k8s_httproutes WHERE hostnames LIKE ?`, "%"+domain+"%")
	if err == nil {
		for rows2.Next() {
			var cid int
			var ns, name, hosts, backends string
			_ = rows2.Scan(&cid, &ns, &name, &hosts, &backends)
			if !hostMatch(hosts, domain) {
				continue
			}
			chains = append(chains, h.buildChain(cid, ns, "HTTPRoute", name, "", backends))
		}
		rows2.Close()
	}
	// Istio VirtualService（他们的主力入口）
	rows3, err := h.DB.Query(`SELECT cluster_id,namespace,name,hosts,backends FROM k8s_virtualservices WHERE hosts LIKE ?`, "%"+domain+"%")
	if err == nil {
		for rows3.Next() {
			var cid int
			var ns, name, hosts, backends string
			_ = rows3.Scan(&cid, &ns, &name, &hosts, &backends)
			if !hostMatch(hosts, domain) {
				continue
			}
			chains = append(chains, h.buildChain(cid, ns, "VirtualService", name, "", backends))
		}
		rows3.Close()
	}
	out["chains"] = chains
	out["matched"] = len(chains)
	c.JSON(http.StatusOK, out)
}

// buildChain 从入口(Ingress/HTTPRoute)往下：Service → Endpoints(Pod/Node) → 工作负载 → 集群。
func (h *K8sTopologyHandler) buildChain(cid int, ns, entryKind, entryName, tls, svcNames string) gin.H {
	cl := h.oneRow(`SELECT name,display_name,environment,project_id FROM k8s_clusters WHERE id=?`, cid)
	services := []gin.H{}
	for _, raw := range splitCSV(svcNames) {
		svc := shortSvc(raw) // VS/HTTPRoute 后端常是 FQDN(svc.ns.svc.cluster.local) → 取首段
		eps, _ := h.DB.Query(`SELECT DISTINCT pod_name,node_name FROM k8s_endpoints WHERE cluster_id=? AND namespace=? AND service_name=?`, cid, ns, svc)
		pods, nodes, wls := []gin.H{}, map[string]struct{}{}, map[string]struct{}{}
		if eps != nil {
			for eps.Next() {
				var pod, node string
				_ = eps.Scan(&pod, &node)
				wl := ""
				if pod != "" {
					_ = h.DB.QueryRow(`SELECT workload FROM k8s_pods WHERE cluster_id=? AND namespace=? AND name=?`, cid, ns, pod).Scan(&wl)
				}
				pods = append(pods, gin.H{"pod": pod, "node": node, "workload": wl})
				if node != "" {
					nodes[node] = struct{}{}
				}
				if wl != "" {
					wls[wl] = struct{}{}
				}
			}
			eps.Close()
		}
		services = append(services, gin.H{
			"service": svc, "namespace": ns,
			"workloads": mapKeys(wls), "nodes": mapKeys(nodes), "pods": pods,
		})
	}
	return gin.H{
		"cluster": cl, "namespace": ns, "entry_kind": entryKind, "entry_name": entryName,
		"tls_secret": tls, "services": services,
	}
}

// Impact 反向影响：给一个节点，列出它上面的 Pod/工作负载/Service，以及受影响的 Ingress/域名。
func (h *K8sTopologyHandler) Impact(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	node := strings.TrimSpace(c.Query("node"))
	if cid == 0 || node == "" {
		c.JSON(400, gin.H{"error": "cluster_id 和 node 必填"})
		return
	}
	out := gin.H{"cluster_id": cid, "node": node}

	// 节点上的 Pod + 工作负载
	pods := []gin.H{}
	wls := map[string]struct{}{}
	if rows, err := h.DB.Query(`SELECT namespace,name,workload,phase FROM k8s_pods WHERE cluster_id=? AND node_name=? ORDER BY namespace,name`, cid, node); err == nil {
		for rows.Next() {
			var ns, name, wl, phase string
			_ = rows.Scan(&ns, &name, &wl, &phase)
			pods = append(pods, gin.H{"namespace": ns, "pod": name, "workload": wl, "phase": phase})
			if wl != "" {
				wls[ns+"/"+wl] = struct{}{}
			}
		}
		rows.Close()
	}
	out["pods"] = pods
	out["workloads"] = mapKeys(wls)

	// 节点上承载的 Service（经 endpoints）
	svcSet := map[string]struct{}{} // ns/svc
	if rows, err := h.DB.Query(`SELECT DISTINCT namespace,service_name FROM k8s_endpoints WHERE cluster_id=? AND node_name=?`, cid, node); err == nil {
		for rows.Next() {
			var ns, svc string
			_ = rows.Scan(&ns, &svc)
			svcSet[ns+"/"+svc] = struct{}{}
		}
		rows.Close()
	}
	out["services"] = mapKeys(svcSet)

	// 受影响的 Ingress + 域名（Ingress 后端 service 命中上面的 service）
	ings := []gin.H{}
	domains := map[string]struct{}{}
	if rows, err := h.DB.Query(`SELECT namespace,name,hosts,svc_names FROM k8s_ingresses WHERE cluster_id=?`, cid); err == nil {
		for rows.Next() {
			var ns, name, hosts, svcNames string
			_ = rows.Scan(&ns, &name, &hosts, &svcNames)
			hit := false
			for _, s := range splitCSV(svcNames) {
				if _, ok := svcSet[ns+"/"+s]; ok {
					hit = true
					break
				}
			}
			if hit {
				ings = append(ings, gin.H{"namespace": ns, "name": name, "kind": "Ingress", "hosts": hosts})
				for _, hh := range splitCSV(hosts) {
					domains[hh] = struct{}{}
				}
			}
		}
		rows.Close()
	}
	// HTTPRoute + Istio VirtualService（后端命中受影响 service → 域名也受影响）
	for _, q := range []struct{ table, hostCol string }{
		{"k8s_httproutes", "hostnames"}, {"k8s_virtualservices", "hosts"},
	} {
		if rows, err := h.DB.Query(`SELECT namespace,name,`+q.hostCol+`,backends FROM `+q.table+` WHERE cluster_id=?`, cid); err == nil {
			for rows.Next() {
				var ns, name, hosts, backends string
				_ = rows.Scan(&ns, &name, &hosts, &backends)
				hit := false
				for _, s := range splitCSV(backends) {
					if _, ok := svcSet[ns+"/"+shortSvc(s)]; ok {
						hit = true
						break
					}
				}
				if hit {
					kind := "HTTPRoute"
					if q.table == "k8s_virtualservices" {
						kind = "VirtualService"
					}
					ings = append(ings, gin.H{"namespace": ns, "name": name, "kind": kind, "hosts": hosts})
					for _, hh := range splitCSV(hosts) {
						domains[hh] = struct{}{}
					}
				}
			}
			rows.Close()
		}
	}
	out["ingresses"] = ings
	out["domains"] = mapKeys(domains)
	c.JSON(http.StatusOK, out)
}

// ---- helpers ----

func hostMatch(hosts, domain string) bool {
	for _, h := range splitCSV(hosts) {
		if strings.EqualFold(h, domain) {
			return true
		}
		// 通配 *.example.com 匹配 sub.example.com（不含裸 "*" catch-all，避免过度命中）
		if strings.HasPrefix(h, "*.") && strings.HasSuffix(strings.ToLower(domain), strings.ToLower(h[1:])) {
			return true
		}
	}
	return false
}

// shortSvc 把 FQDN 后端(central-frontend-svc.g32-admin.svc.cluster.local)取首段(central-frontend-svc)。
func shortSvc(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '.'); i > 0 {
		return s[:i]
	}
	return s
}

// moduleFromSvc 由后端 Service 名推模块名：去掉 -svc 后缀（用户规则：模块=VS名=后端去-svc）。
func moduleFromSvc(svc string) string {
	svc = shortSvc(svc)
	return strings.TrimSuffix(svc, "-svc")
}

func splitCSV(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func mapKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (h *K8sTopologyHandler) strList(q string, args ...any) []string {
	out := []string{}
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (h *K8sTopologyHandler) oneRow(q string, args ...any) gin.H {
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	if !rows.Next() {
		return nil
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if rows.Scan(ptrs...) != nil {
		return nil
	}
	m := gin.H{}
	for i, col := range cols {
		m[col] = normVal(vals[i])
	}
	return m
}

// cdnFacts 从 Cloudflare 接入数据里查这个域名的 CDN 事实。
//
// 空结果必须说清是哪一种空，两者处置完全不同：
//
//	① 该域名的根域根本不在纳管的 CDN 账号里 → 它不走我们管的 CDN（可能直连，或用别家）
//	② 根域在、但这条 FQDN 没有解析记录     → CDN 上漏配了
//
// 只显示一个「—」的话，看的人会以为是 CMDB 采集坏了。
func (h *K8sTopologyHandler) cdnFacts(domain string) map[string]any {
	out := map[string]any{}
	rows, err := h.DB.Query(`SELECT zone_name, type, content, proxied, ttl
		FROM cdn_dns_records WHERE name=? ORDER BY type`, domain)
	if err != nil {
		out["error"] = "查询 CDN 解析记录失败: " + err.Error()
		return out
	}
	defer rows.Close()
	recs := []map[string]any{}
	for rows.Next() {
		var zone, typ, content string
		var proxied, ttl int
		if rows.Scan(&zone, &typ, &content, &proxied, &ttl) != nil {
			continue
		}
		recs = append(recs, map[string]any{
			"zone": zone, "type": typ, "content": content,
			"proxied": proxied == 1, "ttl": ttl,
		})
	}
	if len(recs) > 0 {
		out["records"] = recs
		viaCDN := false
		for _, r := range recs {
			if r["proxied"] == true {
				viaCDN = true
			}
		}
		out["via_cdn"] = viaCDN
		if !viaCDN {
			out["note"] = "该域名在 CDN 上有解析记录，但未开启代理（灰云）——流量直连源站，不经过 CDN 防护与缓存"
		}
		return out
	}

	// 没有记录：区分「根域不在纳管范围」和「根域在但漏配这条」
	zones := []string{}
	if zr, err := h.DB.Query(`SELECT name FROM cdn_zones ORDER BY name`); err == nil {
		for zr.Next() {
			var z string
			if zr.Scan(&z) == nil {
				zones = append(zones, z)
			}
		}
		zr.Close()
	}
	matchedZone := ""
	for _, z := range zones {
		if domain == z || strings.HasSuffix(domain, "."+z) {
			matchedZone = z
			break
		}
	}
	switch {
	case len(zones) == 0:
		out["reason"] = "CMDB 里还没有任何 CDN 站点数据——请到「接入管理 → CDN」配置只读 Token 并同步。" +
			"本项为空不代表该域名没走 CDN"
	case matchedZone != "":
		out["reason"] = "根域 " + matchedZone + " 已纳管，但这条 FQDN 在 CDN 上没有解析记录——" +
			"可能是 CDN 侧漏配，或该域名走的是别的解析途径"
		out["matched_zone"] = matchedZone
	default:
		out["reason"] = "该域名的根域不在已纳管的 CDN 站点中（已纳管 " + itoa(len(zones)) + " 个：" +
			strings.Join(zones, "、") + "）——它不走我们管理的这套 CDN，可能是直连源站或使用了其它 CDN 厂商"
	}
	out["managed_zones"] = zones
	return out
}
