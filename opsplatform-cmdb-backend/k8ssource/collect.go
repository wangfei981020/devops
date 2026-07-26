package k8ssource

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var (
	gatewayGVR   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	httprouteGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
)

const gkeNodePoolLabel = "cloud.google.com/gke-nodepool"

// SyncResult 记录每类资源的采集结果（写 k8s_sync_state）。
type SyncResult struct {
	Resource string
	Count    int
	Err      error
}

// SyncCluster 全量只读采集一个集群的资源到 DB。每类资源 delete+insert 全量替换（镜像语义）。
// 返回各资源结果；单类失败不影响其它类。
func SyncCluster(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, dc dynamic.Interface, clusterID int, nodepoolLabel string) []SyncResult {
	var out []SyncResult
	run := func(res string, fn func() (int, error)) {
		start := time.Now()
		n, err := fn()
		ok, msg := 1, ""
		if err != nil {
			ok, msg = 0, truncErr(err.Error())
		}
		_, _ = db.Exec(`INSERT INTO k8s_sync_state (cluster_id,resource,last_sync,ok,err,duration_ms,count)
			VALUES (?,?,NOW(),?,?,?,?)
			ON DUPLICATE KEY UPDATE last_sync=NOW(),ok=?,err=?,duration_ms=?,count=?`,
			clusterID, res, ok, msg, ms(start), n, ok, msg, ms(start), n)
		out = append(out, SyncResult{Resource: res, Count: n, Err: err})
	}

	run("namespaces", func() (int, error) { return syncNamespaces(ctx, db, cs, clusterID) })
	run("nodes", func() (int, error) { return syncNodes(ctx, db, cs, clusterID, nodepoolLabel) })
	run("workloads", func() (int, error) { return syncWorkloads(ctx, db, cs, clusterID) })
	run("services", func() (int, error) { return syncServices(ctx, db, cs, clusterID) })
	run("endpoints", func() (int, error) { return syncEndpoints(ctx, db, cs, clusterID) })
	run("ingresses", func() (int, error) { return syncIngresses(ctx, db, cs, clusterID) })
	run("pvcs", func() (int, error) { return syncPVCs(ctx, db, cs, clusterID) })
	run("hpas", func() (int, error) { return syncHPAs(ctx, db, cs, clusterID) })
	run("gateways", func() (int, error) { return syncGateways(ctx, db, dc, clusterID) })
	run("httproutes", func() (int, error) { return syncHTTPRoutes(ctx, db, dc, clusterID) })
	run("pods", func() (int, error) { return syncPods(ctx, db, cs, clusterID) })
	// pod 数回填到节点 + 派生节点池（依赖 nodes/pods 已入库）
	run("node_pools", func() (int, error) { return derivePools(db, clusterID) })
	return out
}

func syncNamespaces(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_namespaces WHERE cluster_id=?`, cid)
	for _, ns := range list.Items {
		_, _ = db.Exec(`INSERT INTO k8s_namespaces (cluster_id,name,phase) VALUES (?,?,?)`,
			cid, ns.Name, string(ns.Status.Phase))
	}
	return len(list.Items), nil
}

func syncNodes(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int, poolLabel string) (int, error) {
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_nodes WHERE cluster_id=?`, cid)
	for i := range list.Items {
		n := &list.Items[i]
		ready, hb := readyCondition(n)
		var hbVal any
		if hb != nil && !hb.IsZero() {
			hbVal = hb.UTC()
		}
		_, _ = db.Exec(`INSERT INTO k8s_nodes
			(cluster_id,name,pool,internal_ip,roles,machine_type,cpu_cap,mem_cap,os_image,kubelet_version,ready_status,last_heartbeat,conditions,stuck)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			cid, n.Name, resolvePool(n, poolLabel), nodeInternalIP(n), nodeRoles(n),
			n.Labels["node.kubernetes.io/instance-type"], n.Status.Capacity.Cpu().String(),
			n.Status.Capacity.Memory().String(), n.Status.NodeInfo.OSImage, n.Status.NodeInfo.KubeletVersion,
			ready, hbVal, pressureSummary(n), boolToInt(isStuck(ready, hb)))
	}
	return len(list.Items), nil
}

// stuckHeartbeatThreshold：Ready 心跳超过此时长未更新 → 判为卡死/失联。
const stuckHeartbeatThreshold = 10 * time.Minute

// isStuck 卡死判定：Ready=Unknown(节点控制器已判失联) 或 Ready 心跳长时间未更新。
func isStuck(ready string, hb *time.Time) bool {
	if ready == "Unknown" {
		return true
	}
	if ready != "Ready" && hb != nil && time.Since(*hb) > stuckHeartbeatThreshold {
		return true
	}
	return false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// recordChange 记一条工作负载变更（同步 diff 检出）。
func recordChange(db *sql.DB, cid int, ns, kind, name, field, oldV, newV string) {
	_, _ = db.Exec(`INSERT INTO k8s_changes (cluster_id,namespace,kind,name,field,old_value,new_value) VALUES (?,?,?,?,?,?,?)`,
		cid, ns, kind, name, field, trunc(oldV, 512), trunc(newV, 512))
}

func syncWorkloads(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	// 先载入上轮快照用于 diff（key=ns|kind|name → "image:tag" / desired）
	type snap struct {
		image   string
		desired int
	}
	oldMap := map[string]snap{}
	if rows, err := db.Query(`SELECT namespace,kind,name,image,image_tag,replicas_desired FROM k8s_workloads WHERE cluster_id=?`, cid); err == nil {
		for rows.Next() {
			var ns, kind, name, img, tag string
			var des int
			_ = rows.Scan(&ns, &kind, &name, &img, &tag, &des)
			oldMap[ns+"|"+kind+"|"+name] = snap{image: img + ":" + tag, desired: des}
		}
		rows.Close()
	}
	_, _ = db.Exec(`DELETE FROM k8s_workloads WHERE cluster_id=?`, cid)
	total := 0
	ins := func(ns, kind, name string, desired, ready int32, image, status string) {
		img, tag := splitImage(image)
		// diff：镜像/副本变化则记变更（首轮 oldMap 空，不产生噪音）
		if o, ok := oldMap[ns+"|"+kind+"|"+name]; ok {
			newImg := img + ":" + tag
			if o.image != newImg {
				recordChange(db, cid, ns, kind, name, "image", o.image, newImg)
			}
			if o.desired != int(desired) {
				recordChange(db, cid, ns, kind, name, "replicas", fmt.Sprintf("%d", o.desired), fmt.Sprintf("%d", desired))
			}
		}
		_, _ = db.Exec(`INSERT INTO k8s_workloads
			(cluster_id,namespace,kind,name,replicas_desired,replicas_ready,image,image_tag,status)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			cid, ns, kind, name, desired, ready, img, tag, status)
		total++
	}
	if dl, err := cs.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range dl.Items {
			d := &dl.Items[i]
			ins(d.Namespace, "Deployment", d.Name, deref(d.Spec.Replicas), d.Status.ReadyReplicas,
				firstImage(d.Spec.Template.Spec.Containers), wlStatus(deref(d.Spec.Replicas), d.Status.ReadyReplicas))
		}
	} else {
		return total, err
	}
	if sl, err := cs.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range sl.Items {
			s := &sl.Items[i]
			ins(s.Namespace, "StatefulSet", s.Name, deref(s.Spec.Replicas), s.Status.ReadyReplicas,
				firstImage(s.Spec.Template.Spec.Containers), wlStatus(deref(s.Spec.Replicas), s.Status.ReadyReplicas))
		}
	}
	if dsl, err := cs.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range dsl.Items {
			d := &dsl.Items[i]
			ins(d.Namespace, "DaemonSet", d.Name, d.Status.DesiredNumberScheduled, d.Status.NumberReady,
				firstImage(d.Spec.Template.Spec.Containers), wlStatus(d.Status.DesiredNumberScheduled, d.Status.NumberReady))
		}
	}
	if cj, err := cs.BatchV1().CronJobs("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range cj.Items {
			c := &cj.Items[i]
			ins(c.Namespace, "CronJob", c.Name, 0, 0, firstImage(c.Spec.JobTemplate.Spec.Template.Spec.Containers), c.Spec.Schedule)
		}
	}
	return total, nil
}

func syncServices(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_services WHERE cluster_id=?`, cid)
	for i := range list.Items {
		s := &list.Items[i]
		ports := []string{}
		for _, p := range s.Spec.Ports {
			ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
		}
		_, _ = db.Exec(`INSERT INTO k8s_services (cluster_id,namespace,name,type,cluster_ip,ports) VALUES (?,?,?,?,?,?)`,
			cid, s.Namespace, s.Name, string(s.Spec.Type), s.Spec.ClusterIP, trunc(strings.Join(ports, ","), 255))
	}
	return len(list.Items), nil
}

// syncEndpoints 采集 Endpoints，打通 Service→Pod→Node（全链路/影响分析用）。
func syncEndpoints(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().Endpoints("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_endpoints WHERE cluster_id=?`, cid)
	n := 0
	for i := range list.Items {
		ep := &list.Items[i]
		for _, ss := range ep.Subsets {
			for _, a := range ss.Addresses {
				pod, node := "", ""
				if a.TargetRef != nil && a.TargetRef.Kind == "Pod" {
					pod = a.TargetRef.Name
				}
				if a.NodeName != nil {
					node = *a.NodeName
				}
				_, _ = db.Exec(`INSERT INTO k8s_endpoints (cluster_id,namespace,service_name,pod_name,node_name) VALUES (?,?,?,?,?)`,
					cid, ep.Namespace, ep.Name, pod, node)
				n++
			}
		}
	}
	return n, nil
}

func syncIngresses(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_ingresses WHERE cluster_id=?`, cid)
	for i := range list.Items {
		ing := &list.Items[i]
		hosts, svcs := []string{}, map[string]struct{}{}
		for _, r := range ing.Spec.Rules {
			if r.Host != "" {
				hosts = append(hosts, r.Host)
			}
			if r.HTTP != nil {
				for _, p := range r.HTTP.Paths {
					if p.Backend.Service != nil {
						svcs[p.Backend.Service.Name] = struct{}{}
					}
				}
			}
		}
		tls := []string{}
		for _, t := range ing.Spec.TLS {
			if t.SecretName != "" {
				tls = append(tls, t.SecretName)
			}
		}
		_, _ = db.Exec(`INSERT INTO k8s_ingresses (cluster_id,namespace,name,hosts,tls,svc_names) VALUES (?,?,?,?,?,?)`,
			cid, ing.Namespace, ing.Name, trunc(strings.Join(hosts, ","), 1024),
			trunc(strings.Join(tls, ","), 512), trunc(strings.Join(keys(svcs), ","), 512))
	}
	return len(list.Items), nil
}

func syncPods(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_pods WHERE cluster_id=?`, cid)
	for i := range list.Items {
		p := &list.Items[i]
		var restarts int32
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
		}
		var st any
		if p.Status.StartTime != nil && !p.Status.StartTime.IsZero() {
			st = p.Status.StartTime.UTC()
		}
		cpuReq, memReq, cpuLim, memLim := podResources(p)
		_, _ = db.Exec(`INSERT INTO k8s_pods
			(cluster_id,namespace,name,node_name,workload,phase,cpu_req_m,mem_req_mi,cpu_lim_m,mem_lim_mi,restarts,pod_ip,start_time)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			cid, p.Namespace, p.Name, p.Spec.NodeName, ownerWorkload(p), string(p.Status.Phase),
			cpuReq, memReq, cpuLim, memLim, restarts, p.Status.PodIP, st)
	}
	return len(list.Items), nil
}

// podResources 汇总 Pod 所有容器的 request/limit：CPU 毫核、内存 MiB。
func podResources(p *corev1.Pod) (cpuReq, memReq, cpuLim, memLim int) {
	for _, ct := range p.Spec.Containers {
		if q, ok := ct.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuReq += int(q.MilliValue())
		}
		if q, ok := ct.Resources.Requests[corev1.ResourceMemory]; ok {
			memReq += int(q.Value() / (1024 * 1024))
		}
		if q, ok := ct.Resources.Limits[corev1.ResourceCPU]; ok {
			cpuLim += int(q.MilliValue())
		}
		if q, ok := ct.Resources.Limits[corev1.ResourceMemory]; ok {
			memLim += int(q.Value() / (1024 * 1024))
		}
	}
	return
}

// derivePools 回填每节点 pod 数 + 按 pool 聚合出 k8s_node_pools。
func derivePools(db *sql.DB, cid int) (int, error) {
	// 回填 pod_count
	_, _ = db.Exec(`UPDATE k8s_nodes n SET pod_count=(
		SELECT COUNT(*) FROM k8s_pods p WHERE p.cluster_id=n.cluster_id AND p.node_name=n.name) WHERE n.cluster_id=?`, cid)
	// 重建节点池
	_, _ = db.Exec(`DELETE FROM k8s_node_pools WHERE cluster_id=?`, cid)
	res, err := db.Exec(`INSERT INTO k8s_node_pools (cluster_id,name,machine_type,node_count,version)
		SELECT cluster_id, pool, MAX(machine_type), COUNT(*), MAX(kubelet_version)
		FROM k8s_nodes WHERE cluster_id=? GROUP BY cluster_id, pool`, cid)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func syncPVCs(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_pvcs WHERE cluster_id=?`, cid)
	for i := range list.Items {
		p := &list.Items[i]
		cap := ""
		if q, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
			cap = q.String()
		} else if q, ok := p.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			cap = q.String()
		}
		sc := ""
		if p.Spec.StorageClassName != nil {
			sc = *p.Spec.StorageClassName
		}
		_, _ = db.Exec(`INSERT INTO k8s_pvcs (cluster_id,namespace,name,status,capacity,storage_class,volume_name) VALUES (?,?,?,?,?,?,?)`,
			cid, p.Namespace, p.Name, string(p.Status.Phase), cap, sc, p.Spec.VolumeName)
	}
	return len(list.Items), nil
}

func syncHPAs(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.AutoscalingV2().HorizontalPodAutoscalers("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_hpas WHERE cluster_id=?`, cid)
	for i := range list.Items {
		h := &list.Items[i]
		minR := int32(0)
		if h.Spec.MinReplicas != nil {
			minR = *h.Spec.MinReplicas
		}
		_, _ = db.Exec(`INSERT INTO k8s_hpas (cluster_id,namespace,name,target_kind,target_name,min_replicas,max_replicas,current_replicas) VALUES (?,?,?,?,?,?,?,?)`,
			cid, h.Namespace, h.Name, h.Spec.ScaleTargetRef.Kind, h.Spec.ScaleTargetRef.Name,
			minR, h.Spec.MaxReplicas, h.Status.CurrentReplicas)
	}
	return len(list.Items), nil
}

// syncGateways 采集 Gateway API 的 Gateway（CRD，dynamic）。集群未装 CRD → 优雅跳过(0)。
func syncGateways(ctx context.Context, db *sql.DB, dc dynamic.Interface, cid int) (int, error) {
	list, err := dc.Resource(gatewayGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		if crdAbsent(err) {
			_, _ = db.Exec(`DELETE FROM k8s_gateways WHERE cluster_id=?`, cid)
			return 0, nil
		}
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_gateways WHERE cluster_id=?`, cid)
	for i := range list.Items {
		g := &list.Items[i]
		class, _, _ := unstructured.NestedString(g.Object, "spec", "gatewayClassName")
		listeners := []string{}
		if ls, found, _ := unstructured.NestedSlice(g.Object, "spec", "listeners"); found {
			for _, l := range ls {
				m, _ := l.(map[string]any)
				name, _ := m["name"].(string)
				proto, _ := m["protocol"].(string)
				port := toInt(m["port"])
				listeners = append(listeners, fmt.Sprintf("%s:%d/%s", name, port, proto))
			}
		}
		addrs := []string{}
		if as, found, _ := unstructured.NestedSlice(g.Object, "status", "addresses"); found {
			for _, a := range as {
				m, _ := a.(map[string]any)
				if v, _ := m["value"].(string); v != "" {
					addrs = append(addrs, v)
				}
			}
		}
		_, _ = db.Exec(`INSERT INTO k8s_gateways (cluster_id,namespace,name,gateway_class,listeners,addresses) VALUES (?,?,?,?,?,?)`,
			cid, g.GetNamespace(), g.GetName(), class, trunc(strings.Join(listeners, ","), 512), trunc(strings.Join(addrs, ","), 512))
	}
	return len(list.Items), nil
}

// syncHTTPRoutes 采集 Gateway API 的 HTTPRoute（CRD，dynamic）。未装 CRD → 优雅跳过。
func syncHTTPRoutes(ctx context.Context, db *sql.DB, dc dynamic.Interface, cid int) (int, error) {
	list, err := dc.Resource(httprouteGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		if crdAbsent(err) {
			_, _ = db.Exec(`DELETE FROM k8s_httproutes WHERE cluster_id=?`, cid)
			return 0, nil
		}
		return 0, err
	}
	_, _ = db.Exec(`DELETE FROM k8s_httproutes WHERE cluster_id=?`, cid)
	for i := range list.Items {
		r := &list.Items[i]
		hosts, _, _ := unstructured.NestedStringSlice(r.Object, "spec", "hostnames")
		parents := []string{}
		if ps, found, _ := unstructured.NestedSlice(r.Object, "spec", "parentRefs"); found {
			for _, p := range ps {
				m, _ := p.(map[string]any)
				if n, _ := m["name"].(string); n != "" {
					parents = append(parents, n)
				}
			}
		}
		backends := map[string]struct{}{}
		if rules, found, _ := unstructured.NestedSlice(r.Object, "spec", "rules"); found {
			for _, rule := range rules {
				rm, _ := rule.(map[string]any)
				if brs, ok := rm["backendRefs"].([]any); ok {
					for _, br := range brs {
						bm, _ := br.(map[string]any)
						if n, _ := bm["name"].(string); n != "" {
							backends[n] = struct{}{}
						}
					}
				}
			}
		}
		_, _ = db.Exec(`INSERT INTO k8s_httproutes (cluster_id,namespace,name,hostnames,parents,backends) VALUES (?,?,?,?,?,?)`,
			cid, r.GetNamespace(), r.GetName(), trunc(strings.Join(hosts, ","), 1024),
			trunc(strings.Join(parents, ","), 512), trunc(strings.Join(keys(backends), ","), 512))
	}
	return len(list.Items), nil
}

// crdAbsent 判断错误是否为"CRD 未安装"（NotFound / NoResourceMatch），用于优雅跳过。
func crdAbsent(err error) bool {
	if apierrors.IsNotFound(err) {
		return true
	}
	return strings.Contains(err.Error(), "could not find the requested resource") ||
		strings.Contains(err.Error(), "no matches for kind")
}

func toInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	case int:
		return int64(t)
	default:
		return 0
	}
}

// ---- helpers ----

func resolvePool(n *corev1.Node, poolLabel string) string {
	key := poolLabel
	if key == "" {
		key = gkeNodePoolLabel
	}
	if v := n.Labels[key]; v != "" {
		return v
	}
	if r := nodeRoles(n); r != "" {
		return r
	}
	return "default"
}

func nodeRoles(n *corev1.Node) string {
	roles := []string{}
	for k := range n.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			r := strings.TrimPrefix(k, "node-role.kubernetes.io/")
			if r != "" {
				roles = append(roles, r)
			}
		}
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

func nodeInternalIP(n *corev1.Node) string {
	for _, a := range n.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			return a.Address
		}
	}
	return ""
}

func readyCondition(n *corev1.Node) (string, *time.Time) {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			hb := c.LastHeartbeatTime.Time
			switch c.Status {
			case corev1.ConditionTrue:
				return "Ready", &hb
			case corev1.ConditionFalse:
				return "NotReady", &hb
			default:
				return "Unknown", &hb
			}
		}
	}
	return "Unknown", nil
}

func pressureSummary(n *corev1.Node) string {
	p := []string{}
	for _, c := range n.Status.Conditions {
		if c.Status == corev1.ConditionTrue && c.Type != corev1.NodeReady {
			p = append(p, string(c.Type))
		}
	}
	return trunc(strings.Join(p, ","), 255)
}

func firstImage(cs []corev1.Container) string {
	if len(cs) > 0 {
		return cs[0].Image
	}
	return ""
}

func splitImage(image string) (string, string) {
	if image == "" {
		return "", ""
	}
	// 去掉可能的 @sha256 摘要
	base := image
	if i := strings.Index(base, "@"); i >= 0 {
		base = base[:i]
	}
	// tag = 最后一个冒号后（且冒号在最后一个斜杠之后，避免端口号误判）
	slash := strings.LastIndex(base, "/")
	colon := strings.LastIndex(base, ":")
	if colon > slash {
		return base[:colon], base[colon+1:]
	}
	return base, "latest"
}

func ownerWorkload(p *corev1.Pod) string {
	if len(p.OwnerReferences) == 0 {
		return ""
	}
	o := p.OwnerReferences[0]
	name := o.Name
	// ReplicaSet 名带 deployment 的 hash 后缀，去掉最后一段还原 Deployment 名
	if o.Kind == "ReplicaSet" {
		if i := strings.LastIndex(name, "-"); i > 0 {
			name = name[:i]
		}
	}
	return name
}

func wlStatus(desired, ready int32) string {
	if desired == 0 {
		return "scaled-0"
	}
	if ready >= desired {
		return "healthy"
	}
	return "degraded"
}

func deref(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func truncErr(s string) string { return trunc(s, 500) }

func ms(start time.Time) int { return int(time.Since(start).Milliseconds()) }
