package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opsplatform-cmdb-backend/logx"
)

// 配置引用审计：回答「Pod 起不来，缺的到底是哪个 ConfigMap/Secret」。
//
// 判定能力在两类资源上并不对等，这个差异必须暴露给使用者，不能糊在一起：
//
//	ConfigMap —— 有名录（只读 RBAC 已含 configmaps 权限），能**确定性**判断存不存在、键在不在。
//	Secret    —— 分两种情况，取决于该集群有没有开 allow_secret_inventory：
//	             开启（目前只给 DEV）→ 有名录，可确定性判断存不存在（判不了键，
//	                                   因为 metadata-only 采集拿不到键名）
//	             关闭（UAT/生产）    → 无名录，只有拿到事件佐证才报缺失
//
// 为什么 Secret 默认不开：K8s 的 list secrets 会连 data 一起返回，等于让 CMDB 能读
// 全集群所有密码。开启的集群靠 metadata 客户端把接触面降到最低（APIServer 不返回 data），
// 但 RBAC 那层权限本身仍在——所以只在敏感度低的环境开。
//
// 为什么 Secret 不做「全部标为待确认」：那会产出几百条无效条目。
// CMDB-005 的教训——误报会连带把真问题一起淹掉。宁可少报，不可乱报。
// 同理，开了名录却一条都没采到时**不做确定性判定**——那种情况下照常判会把每一条
// Secret 引用都报成缺失。

// K8s 把「引用的东西不存在」写进事件消息，这是无 Secret 权限时唯一的判定依据。
var (
	// Error: secret "xxx" not found / configmap "xxx" not found
	notFoundPattern = regexp.MustCompile(`(?i)\b(secret|configmap)\s+"([^"]+)"\s+not\s+found`)
	// kubelet 取不到镜像拉取密钥时的措辞，DEV-002 正是这一类。
	// 实测格式（K8s 1.29 FailedToRetrieveImagePullSecret 事件）：
	//   Unable to retrieve some image pull secrets (harbor-id, other); attempting to pull the image may not succeed.
	// 括号内是逗号分隔的名字列表，可能多个。
	pullSecretListPattern = regexp.MustCompile(`(?i)unable to retrieve some image pull secrets?\s*\(([^)]+)\)`)
	// 另一种单数措辞，部分版本/运行时会用：
	//   Unable to retrieve pull secret ns/name for ns/pod because the secret does not exist
	pullSecretSinglePattern = regexp.MustCompile(`(?i)unable to retrieve pull secret\s+[^/\s]+/(\S+?)\s`)
)

// 系统自动维护的 ConfigMap：每个命名空间都有、无人显式引用，报「未被引用」纯属噪音。
var systemConfigMaps = map[string]bool{
	"kube-root-ca.crt":     true,
	"istio-ca-root-cert":   true,
	"openshift-service-ca": true,
}

// configNoiseNS 平台自带组件所在的命名空间。它们的 ConfigMap 由 operator 维护，
// 「无 Pod 引用」是常态，报出来只会淹掉业务侧的真问题。
// 与 orphans.go 的 isSystemNamespace 分开定义：那个判的是「命名空间可以为空」，
// 这里判的是「配置未被引用属正常」，两个语义不同，合用会互相牵制。
func configNoiseNS(ns string) bool {
	if isSystemNamespace(ns) {
		return true
	}
	switch ns {
	case "istio-system", "cattle-system", "kubesphere-system", "monitoring", "cert-manager":
		return true
	}
	return strings.HasPrefix(ns, "kube-")
}

type configFinding struct {
	Severity  string   `json:"severity"`
	Status    string   `json:"status"` // missing | key_missing | unused
	Namespace string   `json:"namespace"`
	RefKind   string   `json:"ref_kind"`
	RefName   string   `json:"ref_name"`
	RefKey    string   `json:"ref_key,omitempty"`
	Source    string   `json:"source,omitempty"`
	Pods      []string `json:"pods,omitempty"`
	PodCount  int      `json:"pod_count,omitempty"`
	Basis     string   `json:"basis"` // 判定依据——依据不同可信度不同，必须写明
	Issue     string   `json:"issue"`
	Action    string   `json:"action"`
}

// refRow 一条聚合后的引用（同名同键的多个 Pod 合并）。
type refRow struct {
	ns, kind, name, key, source string
	optional                    bool
	pods                        []string
	badPods                     int // 其中状态异常的 Pod 数
}

// ConfigAudit 审计配置引用完整性。
// 参数：cluster_id(必填)、namespace(可选)、include_unused(可选，默认不含未引用清单)
func (h *K8sDiagHandler) ConfigAudit(c *gin.Context) {
	cid, ok := requireCluster(c, h.DB)
	if !ok {
		return
	}
	ns := c.Query("namespace")

	refs, err := h.loadRefs(cid, ns)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	cmIndex, err := h.loadConfigMapIndex(cid, ns)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	secIdx := h.loadSecretIndex(cid, ns)

	// 事件佐证只在有异常 Pod 时才拉——没有异常就没有 Secret 缺失可查，白打一次 APIServer。
	var evidence map[string]bool
	evidenceOK := false
	if anyBad(refs) {
		evidence, evidenceOK = h.notFoundEvidence(cid, ns)
	}

	findings := make([]configFinding, 0, 16)
	referenced := map[string]bool{}

	for _, r := range refs {
		if r.kind == kindCMName {
			referenced[r.ns+"/"+r.name] = true
		}
		if r.optional {
			continue // 缺了也不影响启动，报出来只会淹没真问题
		}
		if f := judgeRef(r, cmIndex, secIdx, evidence, evidenceOK); f != nil {
			findings = append(findings, *f)
		}
	}

	// 反向：名录里有、但没有任何 Pod 引用。措辞是「未发现引用」而非「无用」——
	// ConfigMap 也可能被工作流/外部工具读取，CMDB 只看得到 Pod 这一侧。
	unused := []configFinding{}
	if c.Query("include_unused") == "1" {
		for k, cm := range cmIndex {
			ns0, name := splitNSName(k)
			if referenced[k] || systemConfigMaps[name] || configNoiseNS(ns0) {
				continue
			}
			unused = append(unused, configFinding{
				Severity: "low", Status: "unused", Namespace: ns0,
				RefKind: kindCMName, RefName: name,
				Basis: "名录中存在，但本集群所有 Pod 的 spec 里都没有引用它",
				Issue: "无 Pod 引用（" + itoa(cm.keyCount) + " 个键）",
				Action: "确认没有工作流/外部工具在读它之后再清理；CMDB 只能看到 Pod 侧的引用，" +
					"Job/CronJob 未运行时也不会有 Pod",
			})
		}
		sort.Slice(unused, func(i, j int) bool {
			return unused[i].Namespace+unused[i].RefName < unused[j].Namespace+unused[j].RefName
		})
	}

	sortFindings(findings)
	sum := map[string]int{}
	for _, f := range findings {
		sum[f.Status]++
		sum[f.Severity]++
	}

	c.JSON(http.StatusOK, gin.H{
		"cluster_id":        cid,
		"checked_at":        time.Now().Format("2006-01-02 15:04:05"),
		"summary":           sum,
		"findings":          findings,
		"unused_configmaps": unused,
		"capability": gin.H{
			"configmap":       "可确定性判定：CMDB 有 ConfigMap 名录（仅键名，不含内容）",
			"secret":          secretCapability(secIdx),
			"secret_detail":   secIdx.note,
			"evidence_loaded": evidenceOK,
		},
	})
}

const kindCMName = "configmap"

// judgeRef 判定单条引用。ConfigMap 走名录（确定性），Secret 走事件佐证（有则报、无则不报）。
func judgeRef(r refRow, idx map[string]cmEntry, sec secretInv, evidence map[string]bool, evidenceOK bool) *configFinding {
	base := configFinding{
		Namespace: r.ns, RefKind: r.kind, RefName: r.name, RefKey: r.key,
		Source: r.source, Pods: capPods(r.pods), PodCount: len(r.pods),
	}
	key := r.ns + "/" + r.name

	if r.kind == kindCMName {
		cm, ok := idx[key]
		if !ok {
			base.Severity, base.Status = "high", "missing"
			base.Basis = "CMDB 有本集群完整 ConfigMap 名录，其中不存在该名称——这是确定性判定"
			base.Issue = "引用的 ConfigMap 不存在（来源：" + r.source + "）"
			base.Action = "确认是否漏建、或名称/命名空间写错；" + restartWarning(r)
			return &base
		}
		if r.key != "" && !cm.hasKey(r.key) {
			base.Severity, base.Status = "high", "key_missing"
			base.Basis = "ConfigMap 存在，但名录记录的键名里没有 " + r.key
			base.Issue = "引用的键 " + r.key + " 在 ConfigMap 中不存在"
			base.Action = "补上该键，或修正引用的键名；" + restartWarning(r)
			return &base
		}
		return nil
	}

	// Secret：名录可用且覆盖到本命名空间时确定性判定；否则只认事件佐证。
	if sec.usable && sec.covers(r.ns) {
		if !sec.names[key] {
			base.Severity, base.Status = "high", "missing"
			base.Basis = sec.basis()
			base.Issue = "引用的 Secret 不存在（来源：" + r.source + "）"
			if r.source == "imagePullSecret" {
				base.Issue = "镜像拉取密钥不存在，镜像拉不下来（ImagePullBackOff 的直接原因）"
				base.Action = "在命名空间 " + r.ns + " 下创建该拉取密钥；" +
					"若多个命名空间共用同一仓库，检查是否漏了这个命名空间"
				return &base
			}
			base.Action = "确认是否漏建、或名称/命名空间写错；" + restartWarning(r)
			return &base
		}
		return nil // 名录里有，确定存在
	}
	if evidenceOK && evidence[key] {
		base.Severity, base.Status = "high", "missing"
		base.Basis = "集群事件中出现该 Secret 的 not found 记录（无 Secret 名录，此为唯一可用依据）"
		base.Issue = "引用的 Secret 不存在（来源：" + r.source + "）"
		if r.source == "imagePullSecret" {
			base.Issue = "镜像拉取密钥不存在，镜像拉不下来（ImagePullBackOff 的直接原因）"
			base.Action = "在命名空间 " + r.ns + " 下创建该拉取密钥；" +
				"若多个命名空间共用同一仓库，检查是否漏了这个命名空间"
			return &base
		}
		base.Action = "确认是否漏建、或名称/命名空间写错；" + restartWarning(r)
		return &base
	}
	return nil
}

// restartWarning 引用缺失但 Pod 还活着 = 定时炸弹：进程在跑只是因为它启动早于配置被删。
func restartWarning(r refRow) string {
	if r.badPods == 0 && len(r.pods) > 0 {
		return "注意：这些 Pod 目前仍在运行（启动时该配置还在），但「下次重启会直接起不来」"
	}
	return "受影响 Pod 当前已处于异常状态"
}

type cmEntry struct {
	keys     string
	keyCount int
}

func (e cmEntry) hasKey(k string) bool {
	for _, s := range strings.Split(e.keys, ",") {
		if s == k {
			return true
		}
	}
	return false
}

// secretInv Secret 名录及其「能不能用来下确定性结论」的判定。
//
// 三种状态必须分开，混起来会出大事：
//
//	开关关闭        —— 没名录，走事件佐证（默认，UAT/生产就是这个）
//	开关开启且采到  —— 可以确定性判定「这个 Secret 不存在」
//	开关开启但空的  —— **绝不能当成「都不存在」**。403 或采集失败时名录就是空的，
//	                   照常判定会把该集群每一条 Secret 引用都报成缺失，一次几百条误报。
//	开关关闭但 KSM 可用 —— 见 ksmSecretInventory：用 kube_secret_info 当名录，
//	                       但只在 KSM 确实覆盖到的命名空间内可判定（coveredNS）
type secretInv struct {
	enabled bool
	names   map[string]bool
	usable  bool
	note    string
	source  string // "" = CMDB 自采名录；"ksm" = 来自 kube-state-metrics 指标
	// coveredNS 为 nil 表示覆盖全集群（自采名录就是全量）；非 nil 时只有列出的 ns 可判定。
	coveredNS map[string]bool
}

// covers 判断该命名空间是否在名录覆盖范围内。
//
// 为什么必须有这一层：KSM 可以被 --namespaces 限定成只采部分命名空间。
// 对它没覆盖的 ns 照样判定，会把那些 ns 里每一条 Secret 引用都报成「不存在」——
// 正是 CMDB-005 那种一次几百条误报。名录不覆盖就退回事件佐证。
func (s secretInv) covers(ns string) bool {
	if s.coveredNS == nil {
		return true
	}
	return s.coveredNS[ns]
}

// basis 说明这条确定性判定的依据来自哪里。依据不同可信度不同，必须写明而不是糊成一句。
func (s secretInv) basis() string {
	if s.source == "ksm" {
		return "kube-state-metrics 的 kube_secret_info 中不存在该名称（该指标由 KSM 直接 watch APIServer 得出，" +
			"且已确认覆盖本命名空间）——这是确定性判定，不依赖事件 TTL"
	}
	return "该集群已开启 Secret 名录，其中不存在该名称——这是确定性判定"
}

func (h *K8sDiagHandler) loadSecretIndex(cid int, ns string) secretInv {
	inv := secretInv{names: map[string]bool{}}
	var allow int
	_ = h.DB.QueryRow(`SELECT COALESCE(allow_secret_inventory,0) FROM k8s_clusters WHERE id=?`, cid).Scan(&allow)
	inv.enabled = allow == 1
	if !inv.enabled {
		// 开关关着不代表只能靠事件：KSM 已经在暴露 Secret 的名字了，先试这条路。
		if ksm := h.ksmSecretInventory(cid, ns); ksm != nil {
			return *ksm
		}
		inv.note = "该集群未开启 Secret 名录（默认关闭），且 kube-state-metrics 的 kube_secret_info 不可用，" +
			"Secret 缺失只能靠集群事件佐证——没报出来不等于没问题，尤其是从未启动过的 Pod（事件已过 TTL）"
		return inv
	}
	q := "SELECT namespace,name FROM k8s_secrets WHERE cluster_id=?"
	args := []any{cid}
	if ns != "" {
		q += " AND namespace=?"
		args = append(args, ns)
	}
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		inv.note = "读取 Secret 名录失败: " + err.Error() + "；本次退回事件佐证"
		return inv
	}
	defer rows.Close()
	for rows.Next() {
		var n, name string
		if rows.Scan(&n, &name) == nil {
			inv.names[n+"/"+name] = true
		}
	}
	if len(inv.names) == 0 {
		// 开了开关却一条都没有，只可能是没采到（任何集群都有 default-token 之类的 Secret）
		inv.note = "已开启 Secret 名录但一条都没采到——极可能是该集群只读 ClusterRole 缺 secrets:[list]（403）。" +
			"本次退回事件佐证，不做确定性判定，以免把每一条 Secret 引用都误报成缺失"
		return inv
	}
	inv.usable = true
	inv.note = "Secret 名录可用（" + itoa(len(inv.names)) + " 个，仅名字不含内容），可确定性判定存在性"
	return inv
}

// ksmSecretInventory 用 kube-state-metrics 的 kube_secret_info 当 Secret 名录——不需要任何新权限。
//
// 动因：UAT/生产都不开 allow_secret_inventory（不给 CMDB secrets:list），于是「缺哪个 Secret」
// 只能靠集群事件佐证，而事件 TTL 只有 1 小时（UAT 实测最老就到 1 小时前）——
// 从未启动过、或昨晚就崩了的 Pod 根本查不出来，这正是本文件开头那句免责声明的由来。
// 而 KSM 一直在 watch Secret 的 metadata，把 (namespace, secret) 暴露成指标；
// 名字本身不是敏感信息，data 永远不进指标。所以这条路既不碰内容、也不加权限，
// 却把 Secret 从「只能靠事件蒙」变成了「和 ConfigMap 一样的确定性判定」。
//
// 安全阀按 CMDB-005 的教训设计——宁可不判，不可乱判，任一条不满足就返回 nil 退回事件佐证：
//  1. 数据源解析失败 / 查询失败（Prometheus 没接、KSM 没装）
//  2. 一条都没返回：任何集群都有 helm release、TLS 这类 Secret，0 条只可能是
//     KSM 关掉了 secret 收集器，此时照判会把每一条引用都误报成缺失
//  3. 覆盖面记进 coveredNS，判定时只认覆盖到的命名空间（见 secretInv.covers）
func (h *K8sDiagHandler) ksmSecretInventory(cid int, ns string) *secretInv {
	var env string
	_ = h.DB.QueryRow(`SELECT environment FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	base, token, clusterLabel, err := resolveEndpointFull(h.DB, h.Cipher, "prometheus", env, cid)
	if err != nil {
		logx.J("k8s_diag", "ksm_secret_inventory_skip", map[string]any{
			"cluster_id": cid, "reason": "无可用 Prometheus 数据源", "err": err.Error(),
		})
		return nil
	}
	label, value := clusterSelectorParts(h.DB, clusterLabel, cid)
	var filters []string
	if label != "" {
		// 多集群共享数据源时必须隔离，否则会把别的集群的 Secret 当成本集群有，进而漏报缺失
		filters = append(filters, fmt.Sprintf("%s=%q", label, value))
	}
	if ns != "" {
		filters = append(filters, fmt.Sprintf("namespace=%q", ns))
	}
	q := "kube_secret_info"
	if len(filters) > 0 {
		q += "{" + strings.Join(filters, ",") + "}"
	}
	samples, err := promInstant(base, token, q)
	if err != nil || len(samples) == 0 {
		reason := "kube_secret_info 无数据（KSM 未部署或关闭了 secret 收集器）"
		if err != nil {
			reason = "查询 kube_secret_info 失败: " + err.Error()
		}
		logx.J("k8s_diag", "ksm_secret_inventory_skip", map[string]any{
			"cluster_id": cid, "namespace": ns, "query": q, "reason": reason,
		})
		return nil
	}
	inv := &secretInv{names: map[string]bool{}, coveredNS: map[string]bool{}, source: "ksm"}
	for _, s := range samples {
		n, name := s.Metric["namespace"], s.Metric["secret"]
		if n == "" || name == "" {
			continue
		}
		inv.names[n+"/"+name] = true
		inv.coveredNS[n] = true
	}
	if len(inv.names) == 0 {
		// 有序列但标签取不出来 = 指标结构和预期不符，同样不能拿来下结论
		logx.J("k8s_diag", "ksm_secret_inventory_skip", map[string]any{
			"cluster_id": cid, "namespace": ns, "samples": len(samples),
			"reason": "kube_secret_info 缺 namespace/secret 标签，无法构成名录",
		})
		return nil
	}
	inv.usable = true
	inv.note = "Secret 名录取自 kube-state-metrics 的 kube_secret_info（" + itoa(len(inv.names)) +
		" 个，覆盖 " + itoa(len(inv.coveredNS)) + " 个命名空间，仅名字不含内容）——" +
		"该集群未开 allow_secret_inventory，改用 KSM 指标做确定性判定，不再依赖事件 TTL；" +
		"KSM 未覆盖的命名空间仍退回事件佐证"
	logx.J("k8s_diag", "ksm_secret_inventory", map[string]any{
		"cluster_id": cid, "namespace": ns, "query": q,
		"secrets": len(inv.names), "covered_ns": len(inv.coveredNS),
	})
	return inv
}

func (h *K8sDiagHandler) loadConfigMapIndex(cid int, ns string) (map[string]cmEntry, error) {
	q := "SELECT namespace,name,COALESCE(key_names,''),key_count FROM k8s_configmaps WHERE cluster_id=?"
	args := []any{cid}
	if ns != "" {
		q += " AND namespace=?"
		args = append(args, ns)
	}
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := map[string]cmEntry{}
	for rows.Next() {
		var n, name, keys string
		var cnt int
		if rows.Scan(&n, &name, &keys, &cnt) == nil {
			idx[n+"/"+name] = cmEntry{keys, cnt}
		}
	}
	return idx, rows.Err()
}

// loadRefs 取引用并按 (ns,kind,name,key,source) 聚合，顺带统计其中异常 Pod 数。
func (h *K8sDiagHandler) loadRefs(cid int, ns string) ([]refRow, error) {
	q := `SELECT r.namespace, r.ref_kind, r.ref_name, r.ref_key, r.source, r.optional, r.pod_name,
	             COALESCE(p.phase,''), COALESCE(p.reason,'')
	      FROM k8s_pod_config_refs r
	      LEFT JOIN k8s_pods p ON p.cluster_id=r.cluster_id AND p.namespace=r.namespace AND p.name=r.pod_name
	      WHERE r.cluster_id=?`
	args := []any{cid}
	if ns != "" {
		q += " AND r.namespace=?"
		args = append(args, ns)
	}
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	agg := map[string]*refRow{}
	order := []string{}
	for rows.Next() {
		var r refRow
		var pod, phase, reason string
		var opt int
		if err := rows.Scan(&r.ns, &r.kind, &r.name, &r.key, &r.source, &opt, &pod, &phase, &reason); err != nil {
			continue
		}
		k := strings.Join([]string{r.ns, r.kind, r.name, r.key, r.source}, "\x00")
		e, ok := agg[k]
		if !ok {
			r.optional = opt == 1
			e = &r
			agg[k] = e
			order = append(order, k)
		}
		e.pods = append(e.pods, pod)
		if phase != "" && phase != "Running" && phase != "Succeeded" || reason != "" {
			e.badPods++
		}
	}
	out := make([]refRow, 0, len(order))
	for _, k := range order {
		out = append(out, *agg[k])
	}
	return out, rows.Err()
}

func anyBad(refs []refRow) bool {
	for _, r := range refs {
		if r.badPods > 0 {
			return true
		}
	}
	return false
}

// notFoundEvidence 从集群事件里抽出「哪个 ConfigMap/Secret 不存在」。
// 一次拉全量事件（而非逐 Pod 查），请求数固定为 1。
// 返回 ok=false 表示事件没取到——此时「没报缺失」不能当成「没有缺失」。
func (h *K8sDiagHandler) notFoundEvidence(cid int, ns string) (map[string]bool, bool) {
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	list, err := cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{Limit: 2000})
	if err != nil {
		return nil, false
	}
	found := map[string]bool{}
	for i := range list.Items {
		e := &list.Items[i]
		msg := e.Message
		for _, m := range notFoundPattern.FindAllStringSubmatch(msg, -1) {
			found[e.Namespace+"/"+m[2]] = true
		}
		for _, m := range pullSecretListPattern.FindAllStringSubmatch(msg, -1) {
			// 括号内可能是「a, b, c」多个名字
			for _, name := range strings.Split(m[1], ",") {
				if n := strings.Trim(strings.TrimSpace(name), `"`); n != "" {
					found[e.Namespace+"/"+n] = true
				}
			}
		}
		for _, m := range pullSecretSinglePattern.FindAllStringSubmatch(msg, -1) {
			found[e.Namespace+"/"+strings.Trim(m[1], `"`)] = true
		}
	}
	return found, true
}

func capPods(p []string) []string {
	sort.Strings(p)
	if len(p) > 5 {
		return p[:5]
	}
	return p
}

func splitNSName(k string) (string, string) {
	if i := strings.IndexByte(k, '/'); i >= 0 {
		return k[:i], k[i+1:]
	}
	return "", k
}

func sortFindings(f []configFinding) {
	rank := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(f, func(i, j int) bool {
		if rank[f[i].Severity] != rank[f[j].Severity] {
			return rank[f[i].Severity] < rank[f[j].Severity]
		}
		return f[i].PodCount > f[j].PodCount
	})
}

// secretCapability 一句话说明本次 Secret 判定的可信度，跟结论一起给出。
func secretCapability(inv secretInv) string {
	if inv.usable && inv.source == "ksm" {
		return "可确定性判定（限 KSM 覆盖的 " + itoa(len(inv.coveredNS)) + " 个命名空间）：" +
			"名录取自 kube-state-metrics 的 kube_secret_info，只有名字没有内容；" +
			"KSM 未覆盖的命名空间仍退回事件佐证"
	}
	if inv.usable {
		return "可确定性判定：该集群已开启 Secret 名录（仅名字/命名空间，metadata-only 采集，不含内容）"
	}
	if inv.enabled {
		return "本应可判定但名录为空，已退回事件佐证——未报出不等于没问题"
	}
	return "无法主动判定存在性：该集群未开启 Secret 名录。仅在集群事件出现 not found 佐证时才报出——" +
		"未报出不等于不存在问题，从未启动过的 Pod（事件已过 TTL）查不出来"
}
