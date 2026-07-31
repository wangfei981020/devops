package k8ssource

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"k8s.io/client-go/metadata"
)

var (
	gatewayGVR      = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	httprouteGVR    = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	istioVSv1       = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"}
	istioGatewayGVR = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "gateways"}
	istioVSv1b1     = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1beta1", Resource: "virtualservices"}
)

const gkeNodePoolLabel = "cloud.google.com/gke-nodepool"

// SyncResult 记录每类资源的采集结果（写 k8s_sync_state）。
type SyncResult struct {
	Resource string
	Count    int
	Err      error
}

// SyncCluster 全量只读采集一个集群的资源到 DB。每类资源都是「全量比对 + 增量写」
// （见 diff.go writeRows）：采集结果与库中现存行逐字段比对，只写真正变化的行，
// 对外仍是镜像语义，但稳态下不产生任何写入。
// 返回各资源结果；单类失败不影响其它类。
// mc 为只取 metadata 的客户端，专用于 Secret 名录（不请求 data）。传 nil 则跳过该项。
func SyncCluster(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, dc dynamic.Interface, mc metadata.Interface, clusterID int, nodepoolLabel string) []SyncResult {
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
	run("virtualservices", func() (int, error) { return syncVirtualServices(ctx, db, dc, clusterID) })
	run("configmaps", func() (int, error) { return syncConfigMaps(ctx, db, cs, clusterID) })
	// Secret 名录：默认关闭，只对显式开启的集群采，且只取名字不取内容。
	// 开关状态每轮现查——关掉开关后下一轮就会把已有名录清空。
	run("secrets", func() (int, error) {
		if mc == nil {
			return 0, nil
		}
		var allow int
		_ = db.QueryRow(`SELECT COALESCE(allow_secret_inventory,0) FROM k8s_clusters WHERE id=?`, clusterID).Scan(&allow)
		return syncSecretNames(ctx, db, mc, clusterID, allow == 1)
	})
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
	rows := make([][]any, 0, len(list.Items))
	for _, ns := range list.Items {
		rows = append(rows, []any{cid, ns.Name, string(ns.Status.Phase)})
	}
	return writeRows(db, "k8s_namespaces", []string{"cluster_id", "name", "phase"}, cid, rows, "cluster_id", "name")
}

func syncNodes(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int, poolLabel string) (int, error) {
	list, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
	for i := range list.Items {
		n := &list.Items[i]
		ready, hb := readyCondition(n)
		var hbVal any
		if hb != nil && !hb.IsZero() {
			hbVal = hb.UTC()
		}
		rows = append(rows, []any{
			cid, n.Name, resolvePool(n, poolLabel), nodeInternalIP(n), nodeExternalIP(n), nodeRoles(n),
			n.Labels["node.kubernetes.io/instance-type"], n.Status.Capacity.Cpu().String(),
			n.Status.Capacity.Memory().String(), n.Status.NodeInfo.OSImage, n.Status.NodeInfo.KubeletVersion,
			ready, hbVal, pressureSummary(n), conditionsJSON(n), boolToInt(isStuck(ready, hb)),
		})
	}
	return writeRows(db, "k8s_nodes", []string{
		"cluster_id", "name", "pool", "internal_ip", "external_ip", "roles", "machine_type", "cpu_cap", "mem_cap",
		"os_image", "kubelet_version", "ready_status", "last_heartbeat", "conditions", "conditions_json", "stuck",
	}, cid, rows, "cluster_id", "name")
}

// nodeExternalIP 取节点公网 IP。
//
// 这是判断「NodePort 是不是公网可达」的权威依据，而且 K8s 本来就在 status.addresses
// 里给了——之前只采 InternalIP，把它丢掉了，导致 k3s 集群的暴露面完全判不了（CMDB-009）。
func nodeExternalIP(n *corev1.Node) string {
	for _, a := range n.Status.Addresses {
		if a.Type == corev1.NodeExternalIP && a.Address != "" {
			return a.Address
		}
	}
	return ""
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
	// 变更先攒着，等新数据成功落库再写——采集失败回滚时不该留下变更记录。
	type change struct{ ns, kind, name, field, oldV, newV string }
	var changes []change
	rows := make([][]any, 0, 512)
	ins := func(ns, kind, name string, desired, ready int32, image, status string) {
		img, tag := splitImage(image)
		// diff：镜像/副本变化则记变更（首轮 oldMap 空，不产生噪音）
		if o, ok := oldMap[ns+"|"+kind+"|"+name]; ok {
			newImg := img + ":" + tag
			if o.image != newImg {
				changes = append(changes, change{ns, kind, name, "image", o.image, newImg})
			}
			if o.desired != int(desired) {
				changes = append(changes, change{ns, kind, name, "replicas",
					fmt.Sprintf("%d", o.desired), fmt.Sprintf("%d", desired)})
			}
		}
		rows = append(rows, []any{cid, ns, kind, name, desired, ready, img, tag, status})
	}
	// Deployment 是主体，拉不到就整轮放弃，保住上一轮的完整数据（旧实现此时已把表清空了）。
	if dl, err := cs.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); err == nil {
		for i := range dl.Items {
			d := &dl.Items[i]
			ins(d.Namespace, "Deployment", d.Name, deref(d.Spec.Replicas), d.Status.ReadyReplicas,
				firstImage(d.Spec.Template.Spec.Containers), wlStatus(deref(d.Spec.Replicas), d.Status.ReadyReplicas))
		}
	} else {
		return 0, err
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
	n, err := writeRows(db, "k8s_workloads", []string{
		"cluster_id", "namespace", "kind", "name", "replicas_desired", "replicas_ready", "image", "image_tag", "status",
	}, cid, rows, "cluster_id", "namespace", "kind", "name")
	if err != nil {
		return 0, err
	}
	for _, c := range changes {
		recordChange(db, cid, c.ns, c.kind, c.name, c.field, c.oldV, c.newV)
	}
	return n, nil
}

func syncServices(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
	for i := range list.Items {
		s := &list.Items[i]
		ports := []string{}
		for _, p := range s.Spec.Ports {
			ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
		}
		rows = append(rows, []any{cid, s.Namespace, s.Name, string(s.Spec.Type), s.Spec.ClusterIP,
			trunc(serviceExternalIPs(s), 255), lbTypeAnnotation(s), trunc(strings.Join(ports, ","), 255)})
	}
	return writeRows(db, "k8s_services",
		[]string{"cluster_id", "namespace", "name", "type", "cluster_ip", "external_ip", "lb_type", "ports"}, cid, rows, "cluster_id", "namespace", "name")
}

// serviceExternalIPs 取 Service 对外暴露的地址：LoadBalancer 分配的 ingress IP/域名，
// 外加显式声明的 spec.externalIPs。多个用逗号分隔。
func serviceExternalIPs(s *corev1.Service) string {
	seen := map[string]bool{}
	out := []string{}
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, ing := range s.Status.LoadBalancer.Ingress {
		add(ing.IP)
		add(ing.Hostname)
	}
	for _, ip := range s.Spec.ExternalIPs {
		add(ip)
	}
	return strings.Join(out, ",")
}

// lbTypeAnnotation 取云厂商的「内网 LB」注解值（GKE 新旧两种键都认）。
// 空值不等于外网——托管 LB 的权威内外网属性在 cloud_loadbalancers.scheme，这里只是 K8s 侧的声明。
func lbTypeAnnotation(s *corev1.Service) string {
	for _, k := range []string{
		"networking.gke.io/load-balancer-type",
		"cloud.google.com/load-balancer-type",
		"service.beta.kubernetes.io/aws-load-balancer-internal",
		"service.beta.kubernetes.io/azure-load-balancer-internal",
	} {
		if v := s.Annotations[k]; v != "" {
			return trunc(v, 32)
		}
	}
	return ""
}

// syncEndpoints 采集 Endpoints，打通 Service→Pod→Node（全链路/影响分析用）。
func syncEndpoints(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().Endpoints("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := [][]any{}
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
				rows = append(rows, []any{cid, ep.Namespace, ep.Name, pod, node})
			}
		}
	}
	return writeRows(db, "k8s_endpoints",
		[]string{"cluster_id", "namespace", "service_name", "pod_name", "node_name"}, cid, rows)
}

func syncIngresses(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
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
		rows = append(rows, []any{cid, ing.Namespace, ing.Name, trunc(strings.Join(hosts, ","), 1024),
			trunc(strings.Join(tls, ","), 512), trunc(strings.Join(keys(svcs), ","), 512)})
	}
	return writeRows(db, "k8s_ingresses",
		[]string{"cluster_id", "namespace", "name", "hosts", "tls", "svc_names"}, cid, rows, "cluster_id", "namespace", "name")
}

// podReason 提取失败/异常原因：容器 waiting.reason(CrashLoopBackOff/ImagePullBackOff…)、
// 上次终止 OOMKilled/Error、或 Pending 时未调度原因。正常 Running 返回 ""。
func podReason(p *corev1.Pod) string {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" && cs.State.Waiting.Reason != "ContainerCreating" {
			return cs.State.Waiting.Reason
		}
		if cs.LastTerminationState.Terminated != nil {
			if r := cs.LastTerminationState.Terminated.Reason; r == "OOMKilled" || r == "Error" {
				return r
			}
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" && cs.State.Terminated.Reason != "Completed" {
			return cs.State.Terminated.Reason
		}
	}
	if p.Status.Phase == corev1.PodPending {
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodScheduled && c.Status != corev1.ConditionTrue {
				if c.Reason != "" {
					return "Pending:" + c.Reason // 常见 Unschedulable(资源不足)
				}
				return "Pending"
			}
		}
	}
	return ""
}

func syncPods(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
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
		rows = append(rows, []any{
			cid, p.Namespace, p.Name, p.Spec.NodeName, ownerWorkload(p), string(p.Status.Phase),
			cpuReq, memReq, cpuLim, memLim, restarts, p.Status.PodIP, st, podReason(p),
		})
	}
	n, err := writeRows(db, "k8s_pods", []string{
		"cluster_id", "namespace", "name", "node_name", "workload", "phase",
		"cpu_req_m", "mem_req_mi", "cpu_lim_m", "mem_lim_mi", "restarts", "pod_ip", "start_time", "reason",
	}, cid, rows, "cluster_id", "namespace", "name")
	if err != nil {
		return 0, err
	}
	// 顺带记下 PVC 挂载关系（复用同一次 List，不额外请求 APIServer）。
	// 这是判断「盘还有没有人用」的唯一可靠依据。
	vols := make([][]any, 0, 64)
	for i := range list.Items {
		p := &list.Items[i]
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName != "" {
				vols = append(vols, []any{cid, p.Namespace, p.Name, v.PersistentVolumeClaim.ClaimName})
			}
		}
	}
	if _, err := writeRows(db, "k8s_pod_volumes",
		[]string{"cluster_id", "namespace", "pod_name", "pvc_name"}, cid, vols); err != nil {
		return n, err
	}
	// 配置引用同样复用这一次 List：谁引用了哪个 ConfigMap/Secret 全在 pod spec 里，
	// 不需要额外权限，也不额外打 APIServer。
	if err := syncPodConfigRefs(db, cid, list.Items); err != nil {
		return n, err
	}
	// 安全上下文同理：privileged/hostPath/capabilities 都在 pod spec 里，顺手采下来。
	if err := syncPodSecurity(db, cid, list.Items); err != nil {
		return n, err
	}
	return n, nil
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
	// 重建节点池：同样放进事务，避免查询读到"池已删、还没重建"的空档。
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM k8s_node_pools WHERE cluster_id=?`, cid); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO k8s_node_pools (cluster_id,name,machine_type,node_count,version)
		SELECT cluster_id, pool, MAX(machine_type), COUNT(*), MAX(kubelet_version)
		FROM k8s_nodes WHERE cluster_id=? GROUP BY cluster_id, pool`, cid)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}

func syncPVCs(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
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
		rows = append(rows, []any{cid, p.Namespace, p.Name, string(p.Status.Phase), cap, sc, p.Spec.VolumeName})
	}
	return writeRows(db, "k8s_pvcs",
		[]string{"cluster_id", "namespace", "name", "status", "capacity", "storage_class", "volume_name"}, cid, rows, "cluster_id", "namespace", "name")
}

func syncHPAs(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.AutoscalingV2().HorizontalPodAutoscalers("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
	for i := range list.Items {
		h := &list.Items[i]
		minR := int32(0)
		if h.Spec.MinReplicas != nil {
			minR = *h.Spec.MinReplicas
		}
		rows = append(rows, []any{cid, h.Namespace, h.Name, h.Spec.ScaleTargetRef.Kind, h.Spec.ScaleTargetRef.Name,
			minR, h.Spec.MaxReplicas, h.Status.CurrentReplicas})
	}
	return writeRows(db, "k8s_hpas", []string{
		"cluster_id", "namespace", "name", "target_kind", "target_name",
		"min_replicas", "max_replicas", "current_replicas",
	}, cid, rows, "cluster_id", "namespace", "name")
}

// syncGateways 采集 Gateway API 的 Gateway（CRD，dynamic）。集群未装 CRD → 优雅跳过(0)。
func syncGateways(ctx context.Context, db *sql.DB, dc dynamic.Interface, cid int) (int, error) {
	// 两套 Gateway 都采：Gateway API（gateway.networking.k8s.io）与 Istio
	// （networking.istio.io）是完全不同的资源，生产用的是后者。此前只采前者，
	// 结果 Istio 集群查出来 count=0，VirtualService 引用的 Gateway 名对不上任何东西。
	rows := make([][]any, 0, 32)
	anyOK := false
	for _, src := range []struct {
		gvr   schema.GroupVersionResource
		group string
	}{
		{gatewayGVR, "gateway.networking.k8s.io"},
		{istioGatewayGVR, "networking.istio.io"},
	} {
		list, err := dc.Resource(src.gvr).Namespace("").List(ctx, metav1.ListOptions{})
		if err != nil {
			// 无权限或 CRD 不存在都只跳过这一套，另一套照采——
			// 只装了其中一种的集群很常见，不能因此丢掉另一种的数据。
			if apierrors.IsForbidden(err) || crdAbsent(err) {
				continue
			}
			return 0, err
		}
		anyOK = true
		for i := range list.Items {
			g := &list.Items[i]
			if src.group == "networking.istio.io" {
				rows = append(rows, istioGatewayRow(cid, g))
				continue
			}
			rows = append(rows, gatewayAPIRow(cid, g))
		}
	}
	if !anyOK {
		// 两套都拿不到：不清空，保留上一轮数据（可能只是本轮权限抖动）
		return 0, nil
	}
	return writeRows(db, "k8s_gateways",
		[]string{"cluster_id", "namespace", "name", "gateway_class", "listeners", "addresses",
			"api_group", "tls_secrets"}, cid, rows, "cluster_id", "namespace", "name")
}

// gatewayAPIRow 解析 Gateway API 的 Gateway。
func gatewayAPIRow(cid int, g *unstructured.Unstructured) []any {
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
	// Gateway API 的证书在 listeners[].tls.certificateRefs[].name
	certSet := map[string]struct{}{}
	if ls, found, _ := unstructured.NestedSlice(g.Object, "spec", "listeners"); found {
		for _, l := range ls {
			m, _ := l.(map[string]any)
			tls, _ := m["tls"].(map[string]any)
			refs, _ := tls["certificateRefs"].([]any)
			for _, r := range refs {
				rm, _ := r.(map[string]any)
				if n, _ := rm["name"].(string); n != "" {
					certSet[n] = struct{}{}
				}
			}
		}
	}
	certs := make([]string, 0, len(certSet))
	for c := range certSet {
		certs = append(certs, c)
	}
	sort.Strings(certs)
	return []any{cid, g.GetNamespace(), g.GetName(), class,
		trunc(strings.Join(listeners, ","), 512), trunc(strings.Join(addrs, ","), 512),
		"gateway.networking.k8s.io", trunc(strings.Join(certs, ","), 1024)}
}

// istioGatewayRow 解析 Istio Gateway。
//
// 结构与 Gateway API 完全不同：
//   - 没有 gatewayClassName，改用 spec.selector 指向承载它的网关 Pod（如 istio=ingressgateway）
//     ——这正是「这个 Gateway 落在哪个网关负载上」的答案，排障时最需要
//   - servers[].port + hosts + tls.mode 对应 listeners
//   - 没有 status.addresses，地址要看 selector 选中的那个 Service
func istioGatewayRow(cid int, g *unstructured.Unstructured) []any {
	tlsSecrets := map[string]struct{}{}
	sel := []string{}
	if m, found, _ := unstructured.NestedStringMap(g.Object, "spec", "selector"); found {
		for k, v := range m {
			sel = append(sel, k+"="+v)
		}
		sort.Strings(sel) // map 顺序随机，不排序每轮 diff 都是噪音
	}
	listeners := []string{}
	if ss, found, _ := unstructured.NestedSlice(g.Object, "spec", "servers"); found {
		for _, sv := range ss {
			m, _ := sv.(map[string]any)
			port, _ := m["port"].(map[string]any)
			name, _ := port["name"].(string)
			proto, _ := port["protocol"].(string)
			num := toInt(port["number"])
			tlsMode, credName := "", ""
			if t, ok := m["tls"].(map[string]any); ok {
				tlsMode, _ = t["mode"].(string)
				// credentialName 才回答「这个入口用哪张证书」。只采 mode 的话
				// 只知道「开了 TLS」，答不出证书是哪张、存不存在（PROD-002 就卡在这）。
				credName, _ = t["credentialName"].(string)
			}
			if credName != "" {
				tlsSecrets[credName] = struct{}{}
			}
			hosts := []string{}
			if hs, ok := m["hosts"].([]any); ok {
				for _, h := range hs {
					if v, _ := h.(string); v != "" {
						hosts = append(hosts, v)
					}
				}
			}
			entry := fmt.Sprintf("%s:%d/%s", name, num, proto)
			if tlsMode != "" {
				entry += "(tls:" + tlsMode + ")"
			}
			if len(hosts) > 0 {
				entry += " hosts=" + strings.Join(hosts, "|")
			}
			listeners = append(listeners, entry)
		}
	}
	certs := make([]string, 0, len(tlsSecrets))
	for c := range tlsSecrets {
		certs = append(certs, c)
	}
	sort.Strings(certs) // map 顺序随机，不排序每轮 diff 都是噪音
	// gateway_class 位置放 selector：它回答的是同一个问题——这个 Gateway 由谁承载
	return []any{cid, g.GetNamespace(), g.GetName(), trunc(strings.Join(sel, ","), 255),
		trunc(strings.Join(listeners, ","), 512), "", "networking.istio.io",
		trunc(strings.Join(certs, ","), 1024)}
}

// syncHTTPRoutes 采集 Gateway API 的 HTTPRoute（CRD，dynamic）。未装 CRD → 优雅跳过。
func syncHTTPRoutes(ctx context.Context, db *sql.DB, dc dynamic.Interface, cid int) (int, error) {
	list, err := dc.Resource(httprouteGVR).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return 0, nil // 无权限:跳过,不删已有数据
		}
		if crdAbsent(err) {
			return 0, clearAll(db, "k8s_httproutes", cid)
		}
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
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
		rows = append(rows, []any{cid, r.GetNamespace(), r.GetName(), trunc(strings.Join(hosts, ","), 1024),
			trunc(strings.Join(parents, ","), 512), trunc(strings.Join(keys(backends), ","), 512)})
	}
	return writeRows(db, "k8s_httproutes",
		[]string{"cluster_id", "namespace", "name", "hostnames", "parents", "backends"}, cid, rows, "cluster_id", "namespace", "name")
}

// syncVirtualServices 采集 Istio VirtualService（networking.istio.io，dynamic）。先试 v1 再 v1beta1，未装 → 优雅跳过。
func syncVirtualServices(ctx context.Context, db *sql.DB, dc dynamic.Interface, cid int) (int, error) {
	list, err := dc.Resource(istioVSv1).Namespace("").List(ctx, metav1.ListOptions{})
	if err != nil && crdAbsent(err) {
		list, err = dc.Resource(istioVSv1b1).Namespace("").List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		if apierrors.IsForbidden(err) {
			return 0, nil // 无权限:跳过,不删已有数据
		}
		if crdAbsent(err) {
			return 0, clearAll(db, "k8s_virtualservices", cid)
		}
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
	for i := range list.Items {
		r := &list.Items[i]
		hosts, _, _ := unstructured.NestedStringSlice(r.Object, "spec", "hosts")
		gws, _, _ := unstructured.NestedStringSlice(r.Object, "spec", "gateways")
		backends := map[string]struct{}{}
		// http[].route[].destination.host + tcp/tls 同理（取 http 为主）
		for _, proto := range []string{"http", "tcp", "tls"} {
			if routes, found, _ := unstructured.NestedSlice(r.Object, "spec", proto); found {
				for _, rt := range routes {
					rm, _ := rt.(map[string]any)
					if dests, ok := rm["route"].([]any); ok {
						for _, d := range dests {
							dm, _ := d.(map[string]any)
							if dest, ok := dm["destination"].(map[string]any); ok {
								if h, _ := dest["host"].(string); h != "" {
									backends[h] = struct{}{}
								}
							}
						}
					}
				}
			}
		}
		rows = append(rows, []any{cid, r.GetNamespace(), r.GetName(), trunc(strings.Join(hosts, ","), 1024),
			trunc(strings.Join(gws, ","), 512), trunc(strings.Join(keys(backends), ","), 512)})
	}
	return writeRows(db, "k8s_virtualservices",
		[]string{"cluster_id", "namespace", "name", "hosts", "gateways", "backends"}, cid, rows, "cluster_id", "namespace", "name")
}

// crdAbsent 判断错误是否为"CRD 未安装"（NotFound / NoResourceMatch），用于清理 stale 后跳过。
// 注意：Forbidden(无权限) 不在此列——那种情况要跳过但**不能删已有数据**，由各 sync 单独处理。
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

// realPressures 只认这几种为"真压力/异常"，GKE 的 SysctlChanged 等信息性 condition 不算。
var realPressures = map[corev1.NodeConditionType]bool{
	corev1.NodeMemoryPressure: true, corev1.NodeDiskPressure: true,
	corev1.NodePIDPressure: true, corev1.NodeNetworkUnavailable: true,
}

func pressureSummary(n *corev1.Node) string {
	p := []string{}
	for _, c := range n.Status.Conditions {
		if c.Status == corev1.ConditionTrue && realPressures[c.Type] {
			p = append(p, string(c.Type))
		}
	}
	return trunc(strings.Join(p, ","), 255)
}

// conditionsJSON 存全部 conditions（含信息性），供节点详情弹窗展示。
func conditionsJSON(n *corev1.Node) string {
	type cond struct {
		Type    string `json:"type"`
		Status  string `json:"status"`
		Reason  string `json:"reason,omitempty"`
		Message string `json:"message,omitempty"`
		Real    bool   `json:"real"` // 是否真压力(红标)
	}
	out := make([]cond, 0, len(n.Status.Conditions))
	for _, c := range n.Status.Conditions {
		out = append(out, cond{
			Type: string(c.Type), Status: string(c.Status), Reason: c.Reason,
			Message: trunc(c.Message, 300), Real: c.Status == corev1.ConditionTrue && realPressures[c.Type],
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
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
