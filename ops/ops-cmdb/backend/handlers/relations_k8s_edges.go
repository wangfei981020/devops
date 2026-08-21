package handlers

import (
	"strings"

	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// K8s 侧的自动建边：域名 → 入口 → 服务。
//
// # 为什么补这一段
//
// 生产实测：图谱里**全部 62 条边都是 `loadbalancer --backs--> host`**，
// 只有一种类型（OPSCMDB-031 P1-27）。而这套系统里明明有更多关系 ——
// `expose_surface` 能算出「VS → gateway → backend → 后端存活」整条链，
// `domain_topology` 能算出域名背后的服务。**关系数据是有的，图谱页只收录了一种。**
//
// 图谱只有一种边等于没有图：所有路径长度都是 1，点开任何一个起点
// 看到的都是「一个 LB 连着几台机器」，回答不了"这个域名背后是什么"。
//
// # ⚠️ 只连**记下来的事实**，不猜
//
// 这里只用两个已采集的字段：
//
//	k8s_ingresses.hosts      Ingress 对外的域名，逐条比对解析记录的 FQDN
//	k8s_ingresses.svc_names  Ingress 转发到的 Service 名
//
// 🔴 **Service → 工作负载这一跳没有建**，因为我们没采 Service 的 selector。
// 按"同命名空间下同名"去猜在多数集群里对，但错的那部分**看起来和对的一模一样** ——
// 一条错误的拓扑边比没有边危险得多：排障时人会顺着它走到无关的服务上去。
// 缺口如实记在任务摘要里（见 k8sEdgeSummary），等采到 selector 再补。

const (
	ciTypeK8sService = "k8s_service"
	ciTypeK8sIngress = "k8s_ingress"
)

// k8sObj 一个 K8s 对象在 CI 台账里的身份。
//
//	名字用 `集群/命名空间/名字` —— 只用 name 的话，
//	不同集群同名的 Service（几乎必然存在）会被合并成一个点，
//	于是图上出现一条跨集群的假边。
type k8sObj struct {
	clusterID int
	cluster   string
	namespace string
	name      string
}

func (o k8sObj) ciName() string {
	return o.cluster + "/" + o.namespace + "/" + o.name
}

// key 用于本轮内部查找。带 cluster_id，避免跨集群串味
func (o k8sObj) key() string {
	return o.cluster + "\x00" + o.namespace + "\x00" + o.name
}

// ensureK8sCIs 给一批 K8s 对象补 CI 记录，并回收已经不存在的。
//
//	与 ensureLBCIs 同一套取舍：只管自己这个 type，
//	集群上已经没有的对象把 CI 也收掉，免得图上留一堆孤点。
func ensureK8sCIs(sc *store.Scoped, ciType string, objs []k8sObj) (map[string]int64, *TaskFailure) {
	out := map[string]int64{}

	existing := map[string]int64{}
	rows, err := sc.Query(`SELECT id, name FROM cis WHERE tenant_id = ? AND type=?`, ciType)
	if err != nil {
		return out, &TaskFailure{Target: ciType, Reason: "读取现有 CI 失败：" + err.Error()}
	}
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			existing[name] = id
		}
	}
	rows.Close()

	live := map[string]bool{}
	for _, o := range objs {
		ciName := o.ciName()
		live[ciName] = true
		if id, ok := existing[ciName]; ok {
			out[o.key()] = id
			continue
		}
		res, e := sc.Insert(`INSERT INTO cis (tenant_id, type, name, project, status) VALUES (?,?,?,?,'active')`,
			ciType, ciName, o.namespace)
		if e != nil {
			logx.J("relations_auto", "k8s_ci_insert_fail",
				map[string]any{"type": ciType, "ci": ciName, "err": e.Error()})
			continue
		}
		id, _ := res.LastInsertId()
		out[o.key()] = id
	}

	for name, id := range existing {
		if !live[name] {
			if _, e := sc.Exec(`DELETE FROM cis WHERE tenant_id = ? AND id=? AND type=?`, id, ciType); e == nil {
				logx.J("relations_auto", "k8s_ci_removed", map[string]any{"type": ciType, "ci": name})
			}
		}
	}
	return out, nil
}

// k8sEdgeStats 这一轮建了什么、以及**没能建上什么**。
type k8sEdgeStats struct {
	Services  int
	Ingresses int
	// IngressToSvc Ingress → Service 连上的条数
	IngressToSvc int
	// SvcNotFound Ingress 指向了一个我们没采到的 Service。
	//
	//	⚠️ 这个数必须报出来：它不是"没有关系"，是**关系断了**——
	//	要么 Service 被删了（Ingress 现在是坏的），要么采集漏了。
	//	两种都值得看，而"图上少一条线"是看不出来的
	SvcNotFound int
	// DomainToIngress 域名 → Ingress 连上的条数
	DomainToIngress int
	// IngressHostNoDomain Ingress 上的域名在我们的域名台账里找不到
	IngressHostNoDomain int
}

// collectK8sEdges 收集 K8s 侧的边。
func collectK8sEdges(sc *store.Scoped) ([]autoEdge, k8sEdgeStats, []TaskFailure) {
	var edges []autoEdge
	var st k8sEdgeStats
	var failures []TaskFailure

	// 集群 id → **技术名**。CI 名里要带集群，否则跨集群同名对象会被合并成一个点。
	//
	// 🔴 这里原来取的是 `COALESCE(display_name, name)`，于是 CI 名长这样：
	//	    G32 生产/game/game-ing
	//	把**可改的别名**写进了 CI 的身份里 —— 别名一改，同一个对象下次采集
	//	就会算成一条新 CI，旧的变孤儿，关系边跟着断，而且不会报任何错（OPSCMDB-082）。
	//	技术名（k8s_clusters.name）才是稳定标识，也才是 kubectl / PromQL 里用的那个。
	//
	// ⚠️ 别名不是不要了 —— 它由前端的 clusterLabel() 在**展示时**加注，
	//	而不是写进数据。存量 CI 名由迁移 091 改写。
	clusterName := map[int]string{}
	if rows, err := sc.Query(`SELECT id, name FROM k8s_clusters WHERE tenant_id = ?`); err == nil {
		for rows.Next() {
			var id int
			var n string
			if rows.Scan(&id, &n) == nil {
				clusterName[id] = n
			}
		}
		rows.Close()
	} else {
		failures = append(failures, TaskFailure{Target: "k8s_clusters", Reason: "读取集群失败：" + err.Error()})
		return nil, st, failures
	}

	// ── Service ──
	var svcs []k8sObj
	if rows, err := sc.Query(`SELECT cluster_id, namespace, name FROM k8s_services WHERE tenant_id = ?`); err != nil {
		failures = append(failures, TaskFailure{Target: "k8s_services", Reason: "读取 Service 失败：" + err.Error()})
	} else {
		for rows.Next() {
			var o k8sObj
			if rows.Scan(&o.clusterID, &o.namespace, &o.name) == nil {
				o.cluster = clusterName[o.clusterID]
				if o.cluster != "" {
					svcs = append(svcs, o)
				}
			}
		}
		rows.Close()
	}
	svcCI, f := ensureK8sCIs(sc, ciTypeK8sService, svcs)
	if f != nil {
		failures = append(failures, *f)
	}
	st.Services = len(svcCI)

	// ── Ingress ──
	type ingRow struct {
		obj      k8sObj
		hosts    string
		svcNames string
	}
	var ings []ingRow
	if rows, err := sc.Query(`SELECT cluster_id, namespace, name, COALESCE(hosts,''), COALESCE(svc_names,'')
		FROM k8s_ingresses WHERE tenant_id = ?`); err != nil {
		failures = append(failures, TaskFailure{Target: "k8s_ingresses", Reason: "读取 Ingress 失败：" + err.Error()})
	} else {
		for rows.Next() {
			var r ingRow
			if rows.Scan(&r.obj.clusterID, &r.obj.namespace, &r.obj.name, &r.hosts, &r.svcNames) == nil {
				r.obj.cluster = clusterName[r.obj.clusterID]
				if r.obj.cluster != "" {
					ings = append(ings, r)
				}
			}
		}
		rows.Close()
	}
	ingObjs := make([]k8sObj, 0, len(ings))
	for _, r := range ings {
		ingObjs = append(ingObjs, r.obj)
	}
	ingCI, f2 := ensureK8sCIs(sc, ciTypeK8sIngress, ingObjs)
	if f2 != nil {
		failures = append(failures, *f2)
	}
	st.Ingresses = len(ingCI)

	// 域名台账：FQDN → ci_id。Ingress 的 host 要和它比
	domainByFQDN := map[string]int64{}
	if rows, err := sc.Query(`SELECT r.domain_ci_id, c.name, r.host
		FROM domain_records r JOIN cis c ON c.id = r.domain_ci_id
		WHERE r.tenant_id = ? AND c.type='domain' AND r.ignored=0`); err != nil {
		failures = append(failures, TaskFailure{Target: "domain_records", Reason: "读取域名台账失败：" + err.Error()})
	} else {
		for rows.Next() {
			var ciID int64
			var domain, host string
			if rows.Scan(&ciID, &domain, &host) == nil {
				domainByFQDN[normalizeFQDN(recordFQDN(host, domain))] = ciID
			}
		}
		rows.Close()
	}

	for _, r := range ings {
		ingID, ok := ingCI[r.obj.key()]
		if !ok {
			continue
		}
		// Ingress → Service。svc_names 是逗号分隔的服务名（同命名空间）
		for _, sn := range splitList(r.svcNames) {
			target := k8sObj{cluster: r.obj.cluster, namespace: r.obj.namespace, name: sn}
			if sid, ok := svcCI[target.key()]; ok {
				edges = append(edges, autoEdge{ingID, sid, "routes_to"})
				st.IngressToSvc++
			} else {
				// ⚠️ 不是"没有关系"，是关系断了：要么 Service 被删了
				// （这个 Ingress 现在是坏的），要么采集漏了
				st.SvcNotFound++
				logx.J("relations_auto", "ingress_svc_missing", map[string]any{
					"ingress": r.obj.ciName(), "service": sn,
					"hint": "Ingress 指向的 Service 不在台账里：它可能已被删除（该 Ingress 现在转发不到任何后端），也可能是采集漏了",
				})
			}
		}
		// 域名 → Ingress
		for _, h := range splitList(r.hosts) {
			// 通配符 host（*.example.com）连不到具体域名记录上，跳过而不是瞎连
			if strings.HasPrefix(h, "*") {
				continue
			}
			if did, ok := domainByFQDN[normalizeFQDN(h)]; ok {
				edges = append(edges, autoEdge{did, ingID, "resolves_to"})
				st.DomainToIngress++
			} else {
				st.IngressHostNoDomain++
			}
		}
	}

	return edges, st, failures
}

// splitList 拆逗号/空白分隔的列表，去空。
func splitList(s string) []string {
	out := []string{}
	for _, x := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	}) {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// k8sEdgeSummary 摘要。
//
//	⚠️ 必须把**没连上的**也写出来。只报"建了 N 条边"会让人以为图是完整的，
//	而实际上 Service → 工作负载这一整跳根本没建（我们没采 selector）。
//	一张看起来完整、实际缺一层的拓扑图，比没有图更容易把人带偏。
func k8sEdgeSummary(st k8sEdgeStats) string {
	s := "K8s：Service " + itoa(st.Services) + " 个、Ingress " + itoa(st.Ingresses) +
		" 个，入口→服务 " + itoa(st.IngressToSvc) + " 条、域名→入口 " + itoa(st.DomainToIngress) + " 条"
	if st.SvcNotFound > 0 {
		s += "；⚠️ " + itoa(st.SvcNotFound) + " 个 Ingress 指向的 Service 不在台账里（该 Ingress 可能已转发不到后端）"
	}
	if st.IngressHostNoDomain > 0 {
		s += "；" + itoa(st.IngressHostNoDomain) + " 个 Ingress 域名不在域名台账里（未纳管的域名，正常）"
	}
	// 🔴 已知缺口如实写出来，别让图看起来是完整的
	s += "；⚠️ 未建 Service→工作负载 这一跳：我们没有采集 Service 的 selector，" +
		"按同名去猜的话，猜错的那部分和猜对的看起来一模一样，而错误的拓扑边会把排障带到无关的服务上"
	return s
}
