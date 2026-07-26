package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// K8sTopologyHandler 全链路关系（按需计算，不物化 ci_relations）+ 反向影响分析。
type K8sTopologyHandler struct {
	DB *sql.DB
}

func NewK8sTopologyHandler(db *sql.DB) *K8sTopologyHandler {
	return &K8sTopologyHandler{DB: db}
}

func (h *K8sTopologyHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/topology", h.Topology) // 正向：域名 → CDN/证书/Ingress/Service/工作负载/Pod/节点/集群
	r.GET("/k8s/impact", h.Impact)     // 反向：节点 → 受影响的 Service/工作负载/Ingress/域名
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
	// K8s 入口 hosts(VS/Ingress/HTTPRoute)——可能不在台账,补进来(allow-create 也能自输)
	for _, q := range []string{
		`SELECT hosts FROM k8s_virtualservices`, `SELECT hosts FROM k8s_ingresses`, `SELECT hostnames FROM k8s_httproutes`,
	} {
		if rows, _ := h.DB.Query(q); rows != nil {
			for rows.Next() {
				var hs string
				_ = rows.Scan(&hs)
				for _, host := range splitCSV(hs) {
					if host == "*" || host == "" {
						continue
					}
					if _, ok := seen[host]; !ok {
						seen[host] = &d{Name: host}
					}
				}
			}
			rows.Close()
		}
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

	// CDN（best-effort：域名台账 domain_records 匹配的 CDN）
	out["cdns"] = h.strList(`SELECT DISTINCT cd.name FROM domain_records r JOIN cdns cd ON cd.id=r.cdn_id
		WHERE r.cdn_id>0 AND (r.host=? OR CONCAT(r.host,'.') LIKE ? OR ?=r.host)`, domain, "%"+domain, domain)

	// 证书（best-effort：域名台账里该 FQDN 的证书到期；列名 cert_expiry_at）
	out["cert"] = h.oneRow(`SELECT host AS fqdn, cert_expiry_at FROM domain_records WHERE cert_expiry_at IS NOT NULL AND host=? LIMIT 1`, domain)

	// 域名台账（项目/环境/模块/回源/源站）——供前端展示归属
	out["domain_info"] = h.oneRow(`SELECT c.project, c.env, c.module, d.cert_expiry_at, d.dns_provider
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
