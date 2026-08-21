package k8ssource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/metadata"

	"ops-cmdb-backend/logx"
)

var (
	gatewayGVR   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	httprouteGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	// ⚠️ Gateway API 的 CRD 版本在集群间不统一：GKE 装的可能是 v1beta1。
	//
	//	只试 v1 的话，装了 v1beta1 的集群会走进 crdAbsent 分支被"优雅跳过"，
	//	而跳过原来是静默的 —— 于是那个集群明明在用 Gateway API
	//	（GCP 侧的 gkegw1-* 负载均衡和防火墙规则都采到了，那些就是它的产物），
	//	K8s 侧却一条都没有（OPSCMDB-031 P0-11）。
	gatewayV1beta1GVR   = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "gateways"}
	httprouteV1beta1GVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "httproutes"}
	istioVSv1           = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "virtualservices"}
	istioGatewayGVR     = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1", Resource: "gateways"}
	istioVSv1b1         = schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1beta1", Resource: "virtualservices"}
)

const gkeNodePoolLabel = "cloud.google.com/gke-nodepool"

// lastPDBBlocking 记住每个集群上一轮「余量为 0 的 PDB」数量，用于只在变化时打日志。
// 定时采集是串行遍历集群的，但「立即采集」API 可能与它并发，所以加锁。
// 进程重启后清空——重启后第一轮当作首次观测，会补打一条当前状态，不会漏。
var (
	pdbStateMu      sync.Mutex
	lastPDBBlocking = map[int]int{}
)

// SyncResult 记录每类资源的采集结果（写 k8s_sync_state）。
type SyncResult struct {
	Resource string
	Count    int
	Err      error
	// SkipNote 这一类**被跳过了**的原因（CRD 没装、没权限、开关没开）。
	//
	//	⚠️ 跳过不是失败，但**必须留痕**。
	//	原来跳过之后什么都不记，sync_state 里留下 ok=1 / err='' / count=0 ——
	//	和"这个集群确实没有这种资源"一模一样。
	//	实测某集群在用 Gateway API（GCP 侧产物都采到了），K8s 侧一条没有，
	//	而 data_freshness 报 ok=true —— **采集自称成功**，
	//	于是这个缺失不会被任何新鲜度检查发现（OPSCMDB-031 P0-11）。
	SkipNote string
}

// skipNoteSink 让各 sync 函数把"跳过了什么"报上来。
//
//	用一个包级 map 而不是改所有 sync 函数的签名：
//	那会动十几处调用点，而这些函数的返回值语义（count, err）本身是对的。
//	⚠️ 每轮采集开头清空 —— 不清的话上一轮的说明会一直挂着，
//	而"上次跳过了"和"这次跳过了"是两件事。
var skipNotes = map[string]string{}

// noteSkip 记一条跳过说明。
//
//	⚠️ **累加而不是覆盖**。
//	syncGateways 一轮里会试两套（Gateway API 和 Istio），两套都跳过时
//	直接赋值会让后一条盖掉前一条 —— 界面上只看到"Istio 的 CRD 没装"，
//	而 Gateway API 那条缺口消失了。实测本地集群就是这个情况：
//	两套都没装，但只留下一条说明。
func noteSkip(resource, why string) {
	if prev := skipNotes[resource]; prev != "" {
		skipNotes[resource] = prev + "；" + why
		return
	}
	skipNotes[resource] = why
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
		// ⚠️ skip_note 和 err 是两列：err 非空 = 这一轮失败了（很多判定依赖它），
		// skip_note 非空 = 没失败但**有一部分没采**。混成一列会让"跳过"看着像"失败"
		note := truncNote(skipNotes[res])
		_, _ = db.Exec(`INSERT INTO k8s_sync_state (cluster_id,resource,last_sync,ok,err,duration_ms,count,skip_note)
			VALUES (?,?,NOW(),?,?,?,?,?)
			ON DUPLICATE KEY UPDATE last_sync=NOW(),ok=?,err=?,duration_ms=?,count=?,skip_note=?`,
			clusterID, res, ok, msg, ms(start), n, note, ok, msg, ms(start), n, note)
		if note != "" {
			logx.J("k8s_sync", "partial_skip", map[string]any{
				"cluster_id": clusterID, "resource": res, "count": n, "note": note,
			})
		}
		out = append(out, SyncResult{Resource: res, Count: n, Err: err, SkipNote: note})
	}

	// 上一轮的跳过说明必须清掉：「上次跳过了」和「这次跳过了」是两件事，
	// 留着会让一个已经修好的权限问题在界面上一直显示着
	skipNotes = map[string]string{}

	run("namespaces", func() (int, error) { return syncNamespaces(ctx, db, cs, clusterID) })
	run("nodes", func() (int, error) { return syncNodes(ctx, db, cs, clusterID, nodepoolLabel) })
	run("workloads", func() (int, error) { return syncWorkloads(ctx, db, cs, clusterID) })
	run("services", func() (int, error) { return syncServices(ctx, db, cs, clusterID) })
	// 事件：etcd 只留 1 小时，不采下来就永远看不到刚才那一下
	run("events", func() (int, error) { return syncEvents(ctx, db, cs, clusterID) })
	run("endpoints", func() (int, error) { return syncEndpoints(ctx, db, cs, clusterID) })
	run("ingresses", func() (int, error) { return syncIngresses(ctx, db, cs, clusterID) })
	run("pvcs", func() (int, error) { return syncPVCs(ctx, db, cs, clusterID) })
	run("hpas", func() (int, error) { return syncHPAs(ctx, db, cs, clusterID) })
	run("pdbs", func() (int, error) { return syncPDBs(ctx, db, cs, clusterID) })
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
	leases := nodeLeases(ctx, cs, cid)

	rows := make([][]any, 0, len(list.Items))
	current := make(map[string]nodeVersionRef, len(list.Items))
	for i := range list.Items {
		n := &list.Items[i]
		ready, condHB := readyCondition(n)
		hb, src := nodeHeartbeat(n.Name, leases, condHB)
		stale := heartbeatStale(hb, src)

		// 判定的输入、依据、结论三样都打出来。
		// 只打结论的话，「为什么这台是失联」在生产就完全查不了 —— 这个 bug 卡了三个版本
		logx.D("k8ssource", "node_heartbeat", map[string]any{
			"cluster_id": cid, "node": n.Name, "ready": ready,
			"hb_source": src, "hb": tsOrNil(hb), "age_sec": ageSec(hb),
			"threshold_sec": int(staleThresholdFor(src).Seconds()),
			"stale":         stale,
			"cond_hb":       tsOrNil(condHB), "cond_age_sec": ageSec(condHB),
		})

		var hbVal any
		if hb != nil && !hb.IsZero() {
			hbVal = bucketHeartbeat(hb.UTC())
		}
		pool := resolvePool(n, poolLabel)
		current[n.Name] = nodeVersionRef{Pool: pool, Version: n.Status.NodeInfo.KubeletVersion}
		rows = append(rows, []any{
			cid, n.Name, pool, nodeInternalIP(n), nodeExternalIP(n), nodeRoles(n),
			n.Labels["node.kubernetes.io/instance-type"], n.Status.Capacity.Cpu().String(),
			n.Status.Capacity.Memory().String(), n.Status.NodeInfo.OSImage, n.Status.NodeInfo.KubeletVersion,
			ready, hbVal, pressureSummary(n), conditionsJSON(n), boolToInt(isStuck(ready, hb)),
			boolToInt(stale),
		})
	}

	// 先记事件再覆盖当前状态：k8s_nodes 是覆盖式的，写完就再也看不出这一轮变了什么。
	// 记录失败只打日志不阻断——节点数据本身比变更事件重要得多，不能因为记流水失败就丢掉这一轮采集。
	recordNodeVersionEvents(db, cid, current)

	return writeRows(db, "k8s_nodes", []string{
		"cluster_id", "name", "pool", "internal_ip", "external_ip", "roles", "machine_type", "cpu_cap", "mem_cap",
		"os_image", "kubelet_version", "ready_status", "last_heartbeat", "conditions", "conditions_json", "stuck",
		"hb_stale",
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

// heartbeatBucket：存库前把心跳时间向下取整到这个粒度。
//
// 为什么要取整：kubelet 每 10 秒刷一次 Ready 心跳，而采集是 120 秒一轮，
// 于是每一轮拿到的 last_heartbeat 都和库里不同 → syncDiff 判为「行变了」→ 每个节点每轮重写。
// 实测本地单节点集群 129 轮采集写了 129 次 upd，一次不落——
// v149「稳态零写入」的结论对 k8s_nodes 根本不成立，写放大只是从 4.2G/天降到了约 62M/天。
//
// 取整到 5 分钟后，稳态下同一个桶内的值完全相同，syncDiff 不再判为变化，
// 每个节点从「每 2 分钟写一次」降到「每 5 分钟写一次」，约 2.4 倍。
//
// ⚠️ 做不到真正的零写入：心跳本来就是个一直在动的量，
// 想零写入只能不存它，但页面要展示「上次心跳」。这里是「写入量」与「展示不误导」的折中。
//
// 🔴 曾经这里写着「粒度明显小于 stuckHeartbeatThreshold(10 分钟)就不会误导」——
// 那句话只核对了 isStuck，漏了另一个包里的失联判定（原 handlers.heartbeatStaleAfter，也是 5 分钟）。
// 粒度等于阈值时，取整本身就足以把健康节点推过线，UAT 因此有 8 台常年显示失联。
//
// 现在两个判定都在采集时刻用**未取整**的心跳算好、各存一列（stuck / hb_stale），
// 谁都不再拿这个取整值去倒推年龄。所以本粒度可以随便调，不再和任何阈值耦合——
// 它现在**只影响展示的「上次心跳」有多精确**，不影响任何判定。
const heartbeatBucket = 5 * time.Minute

func bucketHeartbeat(t time.Time) time.Time { return t.Truncate(heartbeatBucket) }

// —— 心跳：为什么不能用 conditions[Ready].lastHeartbeatTime ——
//
// 🔴 从 K8s 1.13 引入 NodeLease、1.17 GA 之后，
// `node.status.conditions[Ready].lastHeartbeatTime` **已经不是心跳了**。
// kubelet 只在节点状态**发生变化**时才更新 node.status；
// 状态没变的话按 nodeStatusReportFrequency 走，默认正好是 **5 分钟**。
//
// 真正的心跳搬到了 kube-node-lease 命名空间里的 Lease 对象：
// kubelet 每 ~10 秒续约一次（nodeLeaseDurationSeconds/4），
// 节点控制器也是看它来判 NotReady 的（默认 40 秒宽限）。
//
// 我们原来拿「正常情况下就是 5 分钟更新一次」的字段去和 5 分钟阈值比 ——
// 每台节点各自掷硬币，所以失联数一直在飘（生产实测 8 → 7 → 2）却永远归不了零。
// 这和之前那个「取整粒度 = 判定阈值」是同一类错误，只不过这次周期是 K8s 那边定的。

// HeartbeatStaleAfter 心跳多久没更新就认为「它自称的 Ready 已经不可信」。
//
// 对 Lease 来说这个值极其宽松：续约周期 10 秒、节点控制器 40 秒就判 NotReady，
// 5 分钟意味着已经错过约 30 次续约。不会被网络抖动误伤。
const HeartbeatStaleAfter = 5 * time.Minute

// condHeartbeatStaleAfter 退化到 conditions 时用的阈值。
//
// 只在**拿不到 Lease** 时才走这条路（集群太老，或者凭据没有 leases 的读权限）。
// 这时信号本身就是 5 分钟一报，阈值必须显著大于它，否则必然误报。
// 取 3 倍上报周期 + 一点余量：能发现真正的长时间失联，又不会把正常节点判红。
//
// ⚠️ 它比 HeartbeatStaleAfter 迟钝得多，这是没办法的事 ——
// 用一个 5 分钟粒度的信号，本来就不可能做出 5 分钟精度的判定。
// 所以要把 hb_source 一并记下来，让人知道这台的判定是哪种精度。
const condHeartbeatStaleAfter = 16 * time.Minute

// 心跳来源。存进日志与诊断结论里，用来解释「这个判定有多可信」。
const (
	hbSourceLease = "lease" // kube-node-lease，~10 秒续约，权威
	hbSourceCond  = "cond"  // conditions[Ready]，默认 5 分钟一报，粗糙
	hbSourceNone  = "none"  // 两个都没有
)

// nodeLeases 取 kube-node-lease 里所有节点租约的续约时间。
//
// 取不到不是致命错误（老集群没这个 API、凭据可能没权限），
// 但**必须显式告警**：静默退化会让人以为判定精度还是好的。
func nodeLeases(ctx context.Context, cs *kubernetes.Clientset, cid int) map[string]time.Time {
	out := map[string]time.Time{}
	list, err := cs.CoordinationV1().Leases("kube-node-lease").List(ctx, metav1.ListOptions{})
	if err != nil {
		logx.J("k8ssource", "node_lease_unavailable", map[string]any{
			"cluster_id": cid, "err": err.Error(),
			"impact": "退化到 conditions[Ready].lastHeartbeatTime 判定心跳。" +
				"该字段默认 5 分钟才更新一次，判定会迟钝很多（阈值 " +
				condHeartbeatStaleAfter.String() + "）。" +
				"要恢复精度，请给集群凭据加 kube-node-lease 命名空间下 leases 的 list 权限",
		})
		return out
	}
	for i := range list.Items {
		l := &list.Items[i]
		if l.Spec.RenewTime != nil {
			out[l.Name] = l.Spec.RenewTime.Time
		}
	}
	logx.D("k8ssource", "node_leases_loaded", map[string]any{"cluster_id": cid, "count": len(out)})
	return out
}

// nodeHeartbeat 取这个节点的真实心跳，并说明来源。
func nodeHeartbeat(name string, leases map[string]time.Time, condHB *time.Time) (*time.Time, string) {
	if t, ok := leases[name]; ok && !t.IsZero() {
		tt := t
		return &tt, hbSourceLease
	}
	if condHB != nil && !condHB.IsZero() {
		return condHB, hbSourceCond
	}
	return nil, hbSourceNone
}

// staleThresholdFor 不同来源用不同阈值——因为它们的更新周期差了 30 倍。
func staleThresholdFor(src string) time.Duration {
	if src == hbSourceCond {
		return condHeartbeatStaleAfter
	}
	return HeartbeatStaleAfter
}

// heartbeatStale 用**真实**心跳判定，不经过 bucketHeartbeat。
//
// 从来没上报过心跳一律判为不可信：当成「新鲜」会让一批从没上报过的节点
// 看起来一切正常，这正是本项目反复强调的「缺失被渲染成健康」。
func heartbeatStale(hb *time.Time, src string) bool {
	if hb == nil || hb.IsZero() {
		return true
	}
	return time.Since(*hb) > staleThresholdFor(src)
}

func tsOrNil(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func ageSec(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return int(time.Since(*t).Seconds())
}

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

// syncPDBs 采集 PodDisruptionBudget。
//
// 采它只为回答一个问题：这个节点能不能被 drain 走。
// status.disruptionsAllowed=0 时驱逐请求会被 API Server 一直拒绝，节点升级卡到超时才强杀，
// 单节点可能多花一小时——而这在升级开始前是完全可见的，只要采了。
//
// 权限不足只跳过本项（老集群的只读 RBAC 里可能还没加 policy 组），
// 但必须打日志：预案里「drain 阻塞风险未知」和「无阻塞风险」是两回事，
// 静默返回 0 会让人以为后者。
func syncPDBs(ctx context.Context, db *sql.DB, cs *kubernetes.Clientset, cid int) (int, error) {
	list, err := cs.PolicyV1().PodDisruptionBudgets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			logx.J("k8s_sync", "pdb_forbidden", map[string]any{
				"cluster_id": cid, "err": truncErr(err.Error()),
				"hint": "只读 RBAC 缺 policy/poddisruptionbudgets；升级预案的 drain 阻塞风险将显示为「未知」而非「无风险」",
			})
			return 0, nil
		}
		return 0, err
	}
	rows := make([][]any, 0, len(list.Items))
	blocking := 0
	for i := range list.Items {
		p := &list.Items[i]
		var minAvail, maxUnavail string
		if p.Spec.MinAvailable != nil {
			minAvail = p.Spec.MinAvailable.String()
		}
		if p.Spec.MaxUnavailable != nil {
			maxUnavail = p.Spec.MaxUnavailable.String()
		}
		if p.Status.DisruptionsAllowed == 0 {
			blocking++
		}
		rows = append(rows, []any{
			cid, p.Namespace, p.Name, minAvail, maxUnavail, selectorText(p.Spec.Selector),
			p.Status.CurrentHealthy, p.Status.DesiredHealthy, p.Status.ExpectedPods, p.Status.DisruptionsAllowed,
		})
	}
	// 只在「阻塞数量发生变化」时打日志，不是每轮都打。
	// 原先每轮打一次：本地那个 video-images-generator 的 PDB 长期是 0，
	// 于是每 2 分钟刷一条，实测 108 条日志描述的是同一个持续状态。
	// 持续态刷屏会把真正的状态变化淹掉——这和告警要去重是同一个道理，
	// 当时在告警上想到了，日志上没做到。
	pdbStateMu.Lock()
	prev, seen := lastPDBBlocking[cid]
	changed := !seen || prev != blocking
	if changed {
		lastPDBBlocking[cid] = blocking
	}
	pdbStateMu.Unlock()
	if changed {
		if blocking > 0 {
			logx.J("k8s_sync", "pdb_zero_disruptions", map[string]any{
				"cluster_id": cid, "blocking": blocking, "total": len(rows), "prev": prev,
				"note": "这些 PDB 此刻不允许驱逐任何 Pod，若升级期间仍为 0 会阻塞 drain",
			})
		} else if seen {
			// 恢复也要说一声，否则只知道坏不知道好
			logx.J("k8s_sync", "pdb_blocking_cleared", map[string]any{
				"cluster_id": cid, "prev": prev, "total": len(rows),
				"note": "余量为 0 的 PDB 已全部恢复",
			})
		}
	}
	return writeRows(db, "k8s_pdbs", []string{
		"cluster_id", "namespace", "name", "min_available", "max_unavailable", "selector",
		"current_healthy", "desired_healthy", "expected_pods", "disruptions_allowed",
	}, cid, rows, "cluster_id", "namespace", "name")
}

// selectorText 把 LabelSelector 拍平成 "k=v,k2=v2" 便于人读和关联工作负载。
// matchExpressions 无法用这种形式无损表达，遇到时标注出来而不是悄悄丢掉。
func selectorText(sel *metav1.LabelSelector) string {
	if sel == nil {
		return ""
	}
	parts := make([]string, 0, len(sel.MatchLabels))
	for k, v := range sel.MatchLabels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	s := strings.Join(parts, ",")
	if len(sel.MatchExpressions) > 0 {
		if s != "" {
			s += ","
		}
		s += fmt.Sprintf("(+%d 个 matchExpressions)", len(sel.MatchExpressions))
	}
	return truncRunes(s, 500)
}

// truncRunes 按字符截断，避免把多字节字符切成半个。
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// syncGateways 采集 Gateway API 的 Gateway（CRD，dynamic）。集群未装 CRD → 优雅跳过(0)。
func syncGateways(ctx context.Context, db *sql.DB, dc dynamic.Interface, cid int) (int, error) {
	// 两套 Gateway 都采：Gateway API（gateway.networking.k8s.io）与 Istio
	// （networking.istio.io）是完全不同的资源，生产用的是后者。此前只采前者，
	// 结果 Istio 集群查出来 count=0，VirtualService 引用的 Gateway 名对不上任何东西。
	rows := make([][]any, 0, 32)
	anyOK := false
	for _, src := range []struct {
		gvr      schema.GroupVersionResource
		fallback schema.GroupVersionResource // 版本回退，零值 = 没有
		group    string
		label    string
	}{
		{gatewayGVR, gatewayV1beta1GVR, "gateway.networking.k8s.io", "Gateway API"},
		{istioGatewayGVR, schema.GroupVersionResource{}, "networking.istio.io", "Istio Gateway"},
	} {
		list, err := dc.Resource(src.gvr).Namespace("").List(ctx, metav1.ListOptions{})
		// v1 没有就试 v1beta1 —— GKE 上装 v1beta1 的很常见
		if err != nil && crdAbsent(err) && src.fallback.Resource != "" {
			list, err = dc.Resource(src.fallback).Namespace("").List(ctx, metav1.ListOptions{})
		}
		if err != nil {
			// 无权限或 CRD 不存在都只跳过这一套，另一套照采——
			// 只装了其中一种的集群很常见，不能因此丢掉另一种的数据。
			//
			// ⚠️ 但**必须把跳过记下来**。静默 continue 的后果是
			// sync_state 留下 ok=1/count=N，看起来"采集成功、就这么多"，
			// 而实际上少了一整类资源，排障时完全看不出来（P0-11）。
			if apierrors.IsForbidden(err) {
				noteSkip("gateways", src.label+" 没有 list 权限，这一类没采到（集群凭据缺 "+src.group+" 组的读权限）")
				continue
			}
			if crdAbsent(err) {
				noteSkip("gateways", src.label+" 的 CRD 未安装（v1 与 v1beta1 都试过），这一类没采到")
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
	// v1 没有就试 v1beta1（同 syncGateways：GKE 上装 v1beta1 的很常见）
	if err != nil && crdAbsent(err) {
		list, err = dc.Resource(httprouteV1beta1GVR).Namespace("").List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		// ⚠️ 跳过要留痕，否则 sync_state 里是 ok=1/count=0 —— 和"确实没有 HTTPRoute"
		// 完全一样，而排查"这个域名从哪进来"时会因此漏掉整条 Gateway API 链路（P0-11）
		if apierrors.IsForbidden(err) {
			noteSkip("httproutes", "HTTPRoute 没有 list 权限，这一类没采到（集群凭据缺 gateway.networking.k8s.io 组的读权限）")
			return 0, nil // 无权限:跳过,不删已有数据
		}
		if crdAbsent(err) {
			noteSkip("httproutes", "Gateway API 的 HTTPRoute CRD 未安装（v1 与 v1beta1 都试过）")
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

func truncNote(s string) string { return trunc(s, 500) }

func ms(start time.Time) int { return int(time.Since(start).Milliseconds()) }
