package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 孤儿资源检测：找出「还在占资源/还在计费/还在报错，但已经没人用」的东西。
//
// 这类问题的共同点是单看任何一张表都发现不了，必须跨表比对，所以现实中长期没人管：
// UAT 上曾查出 3 个 HPA 指向已删除的 Deployment，每 15 秒失败一次、累计 15.8 万次
// 无人知晓；20 个 PVC 在计费但没有任何 Pod 挂载，白付 $108/月。
// 把比对固化在这里，调用方问一次就能拿到完整清单。

type orphanItem struct {
	Kind       string  `json:"kind"` // pvc/hpa/virtualservice/ingress/namespace
	Namespace  string  `json:"namespace"`
	Name       string  `json:"name"`
	Reason     string  `json:"reason"`                // 判定为孤儿的依据
	Detail     string  `json:"detail,omitempty"`      // 补充信息（如指向的目标）
	MonthlyUSD float64 `json:"monthly_usd,omitempty"` // 能算出金额的才有
	Action     string  `json:"action"`                // 建议动作（只给方案，人工执行）
}

// ListOrphans GET /api/k8s/orphans?cluster_id=&kind=
func (h *K8sResourceHandler) ListOrphans(c *gin.Context) {
	cidNum, ok := requireCluster(c, h.DB)
	if !ok {
		return
	}
	cid := itoa(cidNum) // 下游子查询都按字符串拼参数
	kind := c.Query("kind")

	items := []orphanItem{}
	if kind == "" || kind == "hpa" {
		items = append(items, h.orphanHPAs(cid)...)
	}
	if kind == "" || kind == "pvc" {
		items = append(items, h.orphanPVCs(cid)...)
	}
	if kind == "" || kind == "virtualservice" {
		items = append(items, h.orphanVirtualServices(cid)...)
	}
	if kind == "" || kind == "ingress" {
		items = append(items, h.orphanIngresses(cid)...)
	}
	if kind == "" || kind == "namespace" {
		items = append(items, h.emptyNamespaces(cid)...)
	}

	byKind := map[string]int{}
	total := 0.0
	for _, it := range items {
		byKind[it.Kind]++
		total += it.MonthlyUSD
	}
	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{
			"total":              len(items),
			"by_kind":            byKind,
			"monthly_usd_wasted": round2(total),
			"yearly_usd_wasted":  round2(total * 12),
		},
		"items": items,
	})
}

// orphanHPAs HPA 指向的工作负载已不存在 —— controller 每 15 秒重试一次并报错，纯噪声。
func (h *K8sResourceHandler) orphanHPAs(cid string) []orphanItem {
	rows, err := h.DB.Query(`SELECT hpa.namespace, hpa.name, hpa.target_kind, hpa.target_name
		FROM k8s_hpas hpa
		LEFT JOIN k8s_workloads w
		  ON w.cluster_id=hpa.cluster_id AND w.namespace=hpa.namespace
		 AND w.name=hpa.target_name AND w.kind=hpa.target_kind
		WHERE hpa.cluster_id=? AND w.id IS NULL`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []orphanItem{}
	for rows.Next() {
		var ns, name, tk, tn string
		if rows.Scan(&ns, &name, &tk, &tn) != nil {
			continue
		}
		out = append(out, orphanItem{
			Kind: "hpa", Namespace: ns, Name: name,
			Reason: "目标工作负载不存在",
			Detail: "指向 " + tk + "/" + tn + "（已被删除）",
			Action: "kubectl -n " + ns + " delete hpa " + name,
		})
	}
	return out
}

// orphanPVCs 没有任何 Pod 挂载的 PVC。盘还在，钱照付。
func (h *K8sResourceHandler) orphanPVCs(cid string) []orphanItem {
	rows, err := h.DB.Query(`SELECT p.namespace, p.name, p.capacity, p.storage_class, p.status
		FROM k8s_pvcs p
		LEFT JOIN k8s_pod_volumes v
		  ON v.cluster_id=p.cluster_id AND v.namespace=p.namespace AND v.pvc_name=p.name
		WHERE p.cluster_id=? AND v.id IS NULL
		ORDER BY p.namespace, p.name`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	rc := newRateCache(h.DB)
	loc := h.clusterLocation(cid)
	out := []orphanItem{}
	for rows.Next() {
		var ns, name, capacity, sc, status string
		if rows.Scan(&ns, &name, &capacity, &sc, &status) != nil {
			continue
		}
		out = append(out, orphanItem{
			Kind: "pvc", Namespace: ns, Name: name,
			Reason:     "没有任何 Pod 挂载",
			Detail:     "容量 " + capacity + "，storageClass " + sc + "，状态 " + status,
			MonthlyUSD: round2(float64(capToGB(capacity)) * rc.diskRate(loc, sc)),
			Action:     "先做快照备份，确认无用后：kubectl -n " + ns + " delete pvc " + name,
		})
	}
	return out
}

// orphanVirtualServices VS 的后端 Service 在本集群不存在 —— 域名还在解析，访问必然 503。
// 后端形如 svc.ns.svc.cluster.local，取首段为 Service 名、次段为命名空间。
func (h *K8sResourceHandler) orphanVirtualServices(cid string) []orphanItem {
	svcs := h.serviceNameSet(cid)
	if len(svcs) == 0 {
		return nil // 一个 Service 都没采到，八成是采集异常，不做判定免得全量误报
	}
	rows, err := h.DB.Query(`SELECT namespace, name, hosts, backends FROM k8s_virtualservices WHERE cluster_id=?`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []orphanItem{}
	for rows.Next() {
		var ns, name, hosts, backends string
		if rows.Scan(&ns, &name, &hosts, &backends) != nil {
			continue
		}
		missing := []string{}
		for _, b := range splitCSV(backends) {
			svcName, svcNS := parseBackendRef(b, ns)
			if svcNS != "" && !svcs[svcNS+"/"+svcName] {
				missing = append(missing, b)
			}
		}
		// 全部后端都缺才算断链：部分缺可能是灰度/外部服务(ServiceEntry)，不武断
		if len(missing) > 0 && len(missing) == len(splitCSV(backends)) {
			out = append(out, orphanItem{
				Kind: "virtualservice", Namespace: ns, Name: name,
				Reason: "后端 Service 在本集群不存在",
				Detail: "host " + hosts + " → 后端 " + strings.Join(missing, ",") + "（访问会得到 503）",
				Action: "确认服务是否已下线，是则：kubectl -n " + ns + " delete virtualservice " + name,
			})
		}
	}
	return out
}

func (h *K8sResourceHandler) orphanIngresses(cid string) []orphanItem {
	svcs := h.serviceNameSet(cid)
	if len(svcs) == 0 {
		return nil
	}
	rows, err := h.DB.Query(`SELECT namespace, name, hosts, svc_names FROM k8s_ingresses WHERE cluster_id=?`, cid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []orphanItem{}
	for rows.Next() {
		var ns, name, hosts, svcNames string
		if rows.Scan(&ns, &name, &hosts, &svcNames) != nil {
			continue
		}
		list := splitCSV(svcNames)
		missing := []string{}
		for _, s := range list {
			if !svcs[ns+"/"+s] {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 && len(missing) == len(list) {
			out = append(out, orphanItem{
				Kind: "ingress", Namespace: ns, Name: name,
				Reason: "后端 Service 不存在",
				Detail: "host " + hosts + " → 后端 " + strings.Join(missing, ","),
				Action: "确认服务是否已下线，是则：kubectl -n " + ns + " delete ingress " + name,
			})
		}
	}
	return out
}

// emptyNamespaces 没有任何工作负载也没有 Pod 的命名空间。本身不花钱，
// 但残留的 RBAC/Secret/NetworkPolicy 是治理死角，也会让人误以为某组件还在跑
// （UAT 上 falco/kyverno 等 6 个安全组件的 ns 都还在，实际一个 Pod 都没有）。
//
// ⚠️ 「没有 Pod」不等于「里面什么都没有」。实测 DEV 集群 51 个被判为空的命名空间里，
// 前 10 个装着 1664 个 ConfigMap（单个最多 587 个）——而 action 给的是 `kubectl delete ns`，
// 照做会把它们一并带走，且从判定结论里完全看不出这件事（CMDB-010）。
//
// 所以这里把 ConfigMap 数量实测出来一并返回：有内容的不再说「可能残留」，
// 而是直接给出数量，并把 action 从「确认无残留资源后删除」改成「先看这些东西再决定」。
func (h *K8sResourceHandler) emptyNamespaces(cid string) []orphanItem {
	// 用 LEFT JOIN 聚合而不是先查名字再逐个 count：命名空间可能上百个，逐个查会打出上百条 SQL
	rows, err := h.DB.Query(`SELECT n.name, COUNT(c.id) AS cm_cnt
		  FROM k8s_namespaces n
		  LEFT JOIN k8s_configmaps c ON c.cluster_id=n.cluster_id AND c.namespace=n.name
		 WHERE n.cluster_id=?
		   AND NOT EXISTS (SELECT 1 FROM k8s_workloads w WHERE w.cluster_id=n.cluster_id AND w.namespace=n.name)
		   AND NOT EXISTS (SELECT 1 FROM k8s_pods p WHERE p.cluster_id=n.cluster_id AND p.namespace=n.name)
		 GROUP BY n.name
		 ORDER BY n.name`, cid)
	if err != nil {
		logx.J("orphans", "empty_ns_query_failed", map[string]any{
			"cluster_id": cid, "err": err.Error(),
			"hint": "空命名空间判定查询失败，本次不返回该类孤儿（不是「没有孤儿」）",
		})
		return nil
	}
	defer rows.Close()
	out := []orphanItem{}
	for rows.Next() {
		var name string
		var cmCount int
		if rows.Scan(&name, &cmCount) != nil {
			continue
		}
		if isSystemNamespace(name) {
			continue // kube-public/kube-node-lease 之类天生就是空的，不算孤儿
		}
		it := orphanItem{
			Kind: "namespace", Name: name,
			Reason: "无任何工作负载与 Pod",
			Detail: "空命名空间，可能残留 RBAC/Secret/NetworkPolicy",
			Action: "确认无残留资源后：kubectl delete ns " + name,
		}
		if cmCount > 0 {
			// 有实测数量时就别再说「可能」——把真实内容摆出来，并让人先看再删
			it.Detail = fmt.Sprintf("⚠️ 没有 Pod，但里面有 %d 个 ConfigMap，删 ns 会一并删掉；"+
				"另可能还有 RBAC/Secret/NetworkPolicy（CMDB 未采，需自行确认）", cmCount)
			it.Action = fmt.Sprintf("先看里面装了什么：kubectl -n %s get configmap,secret,rolebinding,networkpolicy"+
				"；确认可弃后再 kubectl delete ns %s", name, name)
		}
		out = append(out, it)
	}
	return out
}

// isSystemNamespace 天生可能为空的系统命名空间，不该报为孤儿。
func isSystemNamespace(ns string) bool {
	switch ns {
	case "kube-public", "kube-node-lease", "default", "kube-system":
		return true
	}
	return strings.HasPrefix(ns, "gke-managed") || strings.HasPrefix(ns, "gmp-")
}

func (h *K8sResourceHandler) serviceNameSet(cid string) map[string]bool {
	out := map[string]bool{}
	rows, err := h.DB.Query(`SELECT namespace, name FROM k8s_services WHERE cluster_id=?`, cid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var ns, n string
		if rows.Scan(&ns, &n) == nil {
			out[ns+"/"+n] = true
		}
	}
	return out
}

func (h *K8sResourceHandler) clusterLocation(cid string) string {
	var loc string
	_ = h.DB.QueryRow(`SELECT COALESCE(location,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&loc)
	return loc
}

// parseBackendRef 解析 "svc.ns.svc.cluster.local" / "svc.ns" / "svc"。
// 只有一段时无法确定命名空间，按调用方所在 ns 处理。
func parseBackendRef(ref, defaultNS string) (name, ns string) {
	parts := strings.Split(ref, ".")
	name = parts[0]
	if len(parts) >= 2 {
		return name, parts[1]
	}
	return name, defaultNS
}
