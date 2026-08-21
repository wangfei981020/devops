package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"ops-cmdb-backend/internal/httpx"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/diag"
	"ops-cmdb-backend/k8ssource"
	"ops-cmdb-backend/logx"
)

// K8sDiagHandler 合并自 k8sinsight：实时 Pod 日志/事件 + 规则诊断（只读）。
// 这些也是阶段5B MCP 的诊断工具后端。
type K8sDiagHandler struct {
	DB     *sql.DB
	Pool   *k8ssource.Pool
	Cipher *crypto.Cipher // 解 Loki endpoint 的 token：kubelet 取不到日志时退到 Loki
}

func NewK8sDiagHandler(db *sql.DB, pool *k8ssource.Pool, cipher *crypto.Cipher) *K8sDiagHandler {
	return &K8sDiagHandler{DB: db, Pool: pool, Cipher: cipher}
}

func (h *K8sDiagHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/pod-logs", h.Logs) // cluster_id, namespace, pod, container, tail, previous
	// 容器名单。多容器 Pod 不指定 container 时 kubelet 直接报错（不是"取默认容器"），
	// 所以界面必须先知道有哪些容器才能让人选（OPSCMDB-049）。
	r.GET("/k8s/pod-containers", h.Containers) // cluster_id, namespace, pod
	r.GET("/k8s/pod-events", h.Events)         // cluster_id, namespace, pod
	// cluster_id, namespace?, kind?, type?, reason?, sort?(count), min_count?, hours?, limit?, exclude_reason?, include_noise?
	r.GET("/k8s/events", h.ClusterEvents)
	r.GET("/k8s/diagnose", h.Diagnose)
	// 批量自检：把命中率变成一个数字，而不是靠抽样猜（见 diagnose_sweep.go）
	r.GET("/k8s/diagnose-sweep", h.DiagnoseSweep)
	// 集群/域名/成本三个诊断器：与 diagnose_pod 同一套输出契约（见 diagnose_more.go）
	r.GET("/k8s/diagnose-cluster", h.DiagnoseCluster)
	r.GET("/diagnose-domain", h.DiagnoseDomain)
	r.GET("/diagnose-cost", h.DiagnoseCost) // cluster_id, namespace, pod → 规则诊断
	// cluster_id, namespace?, include_unused? → 配置引用完整性（缺哪个 ConfigMap/Secret）
	r.GET("/k8s/config-audit", h.ConfigAudit)
	// cluster_id, kind, name, namespace?, api_group? → 单对象完整 YAML（脱敏），看探针/env/亲和性等 spec
	r.GET("/k8s/manifest", h.Manifest)
}

// eventNoiseReasons 是实测确认会把事件视图整个淹掉的高频控制器噪声。
//
// 起因：UAT 上取 500 条事件回来，argocd 的 StatusRefreshed 占了三分之二，
// 而真问题（一个 Pod BackOff 82 万次、3 个孤儿 HPA 的 FailedGetScale 20 万次）被挤到看不见。
// 这些 reason 只代表「控制器又对账了一次」，排障用不上，默认剔掉；
// 显式传 reason= 点名查某一种、或 include_noise=1 时不剔。
var eventNoiseReasons = map[string]bool{
	"StatusRefreshed":    true, // argocd application-controller，每分钟数条
	"ResourceUpdated":    true,
	"OperationStarted":   true,
	"OperationCompleted": true,
}

// ClusterEvents 统一事件视图（全集群/命名空间，可按 involvedObject.kind(含 Node) / 类型 / reason 筛，实时）。
//
// 三个坑都是实测踩出来的：
//  1. kind/type/reason 必须下推成 apiserver 的 FieldSelector。原来是先无条件取 500 条、再在内存里筛，
//     等于「500 条里的过滤」——集群 Warning 上千条时，传 type=Warning 只能看到最新那批里的零头，
//     漏掉的恰恰是 count 几十万的重灾区。
//  2. 默认剔掉 eventNoiseReasons，否则真问题永远排不进前几十条。
//  3. 默认按时间倒序（前端事件流是这个语义，15 秒刷一次）；排障要找「反复发生」的传 sort=count。
func (h *K8sDiagHandler) ClusterEvents(c *gin.Context) {
	// 先判「集群存不存在」(404)，再判「连不连得上」(502)：
	// 这两件事的处置完全不同——前者是集群已被删除/传错了 id，后者是凭据或网络问题。
	// 原先都走 ClientFor 的 502，一律报"集群 N 不存在"，把连不上也说成了不存在。
	cid, ok := requireCluster(c, h.DB)
	if !ok {
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	kind, typ, reason := c.Query("kind"), c.Query("type"), c.Query("reason")
	ns := c.Query("namespace") // 空=全部命名空间

	var sel []string
	if typ != "" {
		sel = append(sel, "type="+typ)
	}
	if reason != "" {
		sel = append(sel, "reason="+reason)
	}
	if kind != "" {
		sel = append(sel, "involvedObject.kind="+kind)
	}
	limit := int64(1000)
	if v, e := strconv.ParseInt(c.Query("limit"), 10, 64); e == nil && v > 0 && v <= 5000 {
		limit = v
	}
	opts := metav1.ListOptions{Limit: limit}
	if len(sel) > 0 {
		opts.FieldSelector = strings.Join(sel, ",")
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	list, err := cs.CoreV1().Events(ns).List(ctx, opts)
	if err != nil {
		logx.J("k8s_diag", "cluster_events_failed", map[string]any{
			"cluster_id": cid, "namespace": ns, "field_selector": opts.FieldSelector, "err": err.Error(),
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	minCount := 0
	if v, e := strconv.Atoi(c.Query("min_count")); e == nil && v > 0 {
		minCount = v
	}
	var since time.Time
	if v, e := strconv.Atoi(c.Query("hours")); e == nil && v > 0 {
		since = time.Now().Add(-time.Duration(v) * time.Hour)
	}
	dropNoise := c.Query("include_noise") != "1" && reason == ""
	excluded := map[string]bool{}
	for _, r := range strings.Split(c.Query("exclude_reason"), ",") {
		if r = strings.TrimSpace(r); r != "" {
			excluded[r] = true
		}
	}

	out := []gin.H{}
	noiseDropped := 0
	for i := range list.Items {
		e := &list.Items[i]
		if dropNoise && eventNoiseReasons[e.Reason] {
			noiseDropped++
			continue
		}
		if excluded[e.Reason] || int(e.Count) < minCount {
			continue
		}
		ts := e.LastTimestamp.Time
		if ts.IsZero() {
			ts = e.EventTime.Time
		}
		if !since.IsZero() && ts.Before(since) {
			continue
		}
		out = append(out, gin.H{
			"type": e.Type, "reason": e.Reason, "message": e.Message, "count": e.Count,
			"kind": e.InvolvedObject.Kind, "object": e.InvolvedObject.Name, "namespace": e.Namespace,
			"last_seen": ts.Format("2006-01-02 15:04:05"),
		})
	}
	if c.Query("sort") == "count" {
		sort.SliceStable(out, func(i, j int) bool { return out[i]["count"].(int32) > out[j]["count"].(int32) })
	} else {
		sort.SliceStable(out, func(i, j int) bool { return out[i]["last_seen"].(string) > out[j]["last_seen"].(string) })
	}

	// 事件是 TTL 只有 1 小时的易失数据，取回多少 / 筛掉多少 / 有没有被 limit 截断都必须留痕，
	// 否则「查不到」分不清是真没发生、还是被剔了或截断了。
	truncated := int64(len(list.Items)) >= limit
	if truncated {
		logx.J("k8s_diag", "cluster_events_truncated", map[string]any{
			"cluster_id": cid, "namespace": ns, "limit": limit,
			"hint": "已达 limit，结果可能不完整，请收窄 namespace/kind/type 或提高 limit",
		})
	}
	logx.J("k8s_diag", "cluster_events", map[string]any{
		"cluster_id": cid, "namespace": ns, "field_selector": opts.FieldSelector, "sort": c.Query("sort"),
		"limit": limit, "fetched": len(list.Items), "returned": len(out),
		"noise_dropped": noiseDropped, "truncated": truncated,
	})
	c.JSON(http.StatusOK, out)
}

// Logs 取 Pod 日志（非流式，尾部 N 行）。
func (h *K8sDiagHandler) Logs(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pod := c.Query("namespace"), c.Query("pod")
	if cid == 0 || ns == "" || pod == "" {
		httpx.Required(c, "cluster_id/namespace/pod")
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	tail := int64(200)
	if t, e := strconv.ParseInt(c.Query("tail"), 10, 64); e == nil && t > 0 && t <= 5000 {
		tail = t
	}
	opts := &corev1.PodLogOptions{Container: c.Query("container"), TailLines: &tail}
	if c.Query("previous") == "1" {
		opts.Previous = true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	raw, err := cs.CoreV1().Pods(ns).GetLogs(pod, opts).DoRaw(ctx)
	if err != nil {
		// 与 diagnose_pod 同一套处理：翻译报错 + 退到 Loki。
		// 否则调用方在这里只会拿到 "unknown (get pods xxx)"，既不知道原因也没有退路。
		explained := diag.ExplainLogError(err)
		// 🔴 把猜测变成确定答案。
		//
		//	ExplainLogError 只能按错误码给出"可能是 A、可能是 B"——因为它手上没有 Pod。
		//	而这里有 clientset：多容器 Pod 没指定 container 时 APIServer 必然 400，
		//	去查一次 spec 就能直接说"这个 Pod 有 3 个容器，你得选一个，它们是 …"。
		//	⚠️ 这条路径对 MCP/AI 尤其重要：AI 拿到"可能是 A 可能是 B"只能盲试，
		//		拿到容器名单则一次就对。
		//	只在失败路径上多打这一次 API，正常取日志不受影响。
		if c.Query("container") == "" && c.Query("previous") != "1" {
			if names := h.containerNames(c.Request.Context(), cid, ns, pod); len(names) > 1 {
				explained = fmt.Sprintf("这个 Pod 有 %d 个容器，取日志必须指定 container=其中之一：%s。"+
					"（kubectl 会替你选第一个，接口不会）", len(names), strings.Join(names, ", "))
			}
		}
		if lines := h.lokiTail(cid, ns, pod, int(tail)); lines != "" {
			c.Data(http.StatusOK, "text/plain; charset=utf-8",
				[]byte("# kubelet 通道取日志失败，以下为 Loki 历史日志\n# 失败原因: "+explained+"\n\n"+lines))
			SetAuditTarget(c, ns+"/"+pod)
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": explained})
		return
	}
	SetAuditTarget(c, ns+"/"+pod)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", raw)
}

// containerNames 取容器名单（含 init）。只给错误提示用，取不到就返回 nil ——
// 诊断信息取不到不该盖住原本的错误。
func (h *K8sDiagHandler) containerNames(ctx context.Context, cid int, ns, pod string) []string {
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		return nil
	}
	c2, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	p, err := cs.CoreV1().Pods(ns).Get(c2, pod, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	var out []string
	for _, ct := range p.Spec.Containers {
		out = append(out, ct.Name)
	}
	for _, ct := range p.Spec.InitContainers {
		out = append(out, ct.Name+"(init)")
	}
	return out
}

// PodContainer 一个容器。
type PodContainer struct {
	Name string `json:"name"`
	// Init 为 true 表示 initContainer。它们**已经跑完了**，日志仍然可取 ——
	// 而"Pod 起不来"的原因经常就在 init 容器里，这时它是唯一有信息的地方。
	Init bool `json:"init"`
	// Sidecar 猜测值：按常见的注入容器名匹配（istio-proxy / filebeat / …）。
	// 只用来决定默认选中哪个，**不隐藏**任何容器 ——
	// 猜错了而又藏起来的话，人会以为那个容器不存在。
	Sidecar bool `json:"sidecar"`
	// Ready/State 让人在选之前就看出哪个容器有问题，不用挨个点
	Ready bool   `json:"ready"`
	State string `json:"state,omitempty"`
}

// sidecarNames 常见的注入容器。命中只影响默认选中项。
var sidecarNames = map[string]bool{
	"istio-proxy": true, "istio-init": true, "envoy": true,
	"filebeat": true, "fluent-bit": true, "fluentd": true, "vector": true,
	"linkerd-proxy": true, "linkerd-init": true, "dapr": true,
	"config-reloader": true, "prometheus-config-reloader": true,
}

// Containers 列出 Pod 里的容器。
//
// 🔴 为什么需要这个接口：k8s 在 Pod 有多个容器而请求没指定 container 时
//
//	**直接返回 400**（"a container name must be specified for pod X, choose one of: [...]"），
//	不是"取第一个"。所以界面上没有容器选择器 = 带 sidecar 的 Pod 日志根本看不了 ——
//	而接了 Istio 的集群里几乎每个业务 Pod 都带 istio-proxy（OPSCMDB-049）。
func (h *K8sDiagHandler) Containers(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pod := c.Query("namespace"), c.Query("pod")
	if cid == 0 || ns == "" || pod == "" {
		httpx.Required(c, "cluster_id/namespace/pod")
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		httpx.Fail(c, httpx.CodeUpstreamError, errors.New(SafeErr("连接集群", err)), nil)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	p, err := cs.CoreV1().Pods(ns).Get(ctx, pod, metav1.GetOptions{})
	if err != nil {
		httpx.Fail(c, httpx.CodeUpstreamError, errors.New(SafeErr("读取 Pod", err)), nil)
		return
	}
	// 容器状态按名字索引，下面两轮都要用
	st := map[string]corev1.ContainerStatus{}
	for _, s := range append(append([]corev1.ContainerStatus{}, p.Status.ContainerStatuses...), p.Status.InitContainerStatuses...) {
		st[s.Name] = s
	}
	out := make([]PodContainer, 0, len(p.Spec.Containers)+len(p.Spec.InitContainers))
	add := func(name string, isInit bool) {
		item := PodContainer{Name: name, Init: isInit, Sidecar: sidecarNames[name]}
		if s, ok := st[name]; ok {
			item.Ready = s.Ready
			switch {
			case s.State.Waiting != nil:
				item.State = s.State.Waiting.Reason
			case s.State.Terminated != nil:
				item.State = s.State.Terminated.Reason
			case s.State.Running != nil:
				item.State = "Running"
			}
		}
		out = append(out, item)
	}
	// init 排在前面：Pod 卡在 Init 时，那才是要看的
	for _, ct := range p.Spec.InitContainers {
		add(ct.Name, true)
	}
	for _, ct := range p.Spec.Containers {
		add(ct.Name, false)
	}
	SetAuditTarget(c, ns+"/"+pod)
	c.JSON(200, gin.H{"items": out})
}

// lokiTail 从 Loki 取某 Pod 的历史日志尾部；没配 Loki 或查不到时返回空串。
func (h *K8sDiagHandler) lokiTail(cid int, ns, pod string, tail int) string {
	var env string
	_ = h.DB.QueryRow(`SELECT COALESCE(environment,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "loki", env, cid)
	if err != nil {
		return ""
	}
	if tail <= 0 || tail > 1000 {
		tail = 200
	}
	q := fmt.Sprintf(`{namespace=%q,pod=%q}`, ns, pod)
	u := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&limit=%d&direction=backward&start=%d&end=%d",
		base, url.QueryEscape(q), tail, time.Now().Add(-6*time.Hour).UnixNano(), time.Now().UnixNano())
	code, body, err := obsGet(u, token, 15*time.Second)
	if err != nil || code != 200 {
		return ""
	}
	return extractLokiLines(body, tail)
}

// Events 取 Pod 相关事件（Warning 优先、按时间倒序）。
func (h *K8sDiagHandler) Events(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pod := c.Query("namespace"), c.Query("pod")
	if cid == 0 || ns == "" || pod == "" {
		httpx.Required(c, "cluster_id/namespace/pod")
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	list, err := cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + pod})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	out := []gin.H{}
	for i := range list.Items {
		e := &list.Items[i]
		ts := e.LastTimestamp.Time
		if ts.IsZero() {
			ts = e.EventTime.Time
		}
		out = append(out, gin.H{"type": e.Type, "reason": e.Reason, "message": e.Message,
			"count": e.Count, "last_seen": ts.Format("2006-01-02 15:04:05")})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["last_seen"].(string) > out[j]["last_seen"].(string)
	})
	c.JSON(http.StatusOK, out)
}

// Diagnose 规则诊断：采集 Pod 上下文 → RuleProvider → 根因 + 建议（只给方案，人工执行）。
func (h *K8sDiagHandler) Diagnose(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pod := c.Query("namespace"), c.Query("pod")
	if cid == 0 || ns == "" || pod == "" {
		httpx.Required(c, "cluster_id/namespace/pod")
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	var clusterName string
	_ = h.DB.QueryRow(`SELECT COALESCE(display_name,name) FROM k8s_clusters WHERE id=?`, cid).Scan(&clusterName)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	dc, err := diag.Collect(ctx, cs, clusterName, ns, pod)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	// kubelet 通道拿不到日志时退到 Loki。必须在诊断前补，规则要靠日志内容判根因。
	h.fillLogsFromLoki(ctx, cid, dc)
	// 实时事件里没有「带原因」的那条时，从 CMDB 采集的历史里补（见 diag_events_history.go）。
	// 这一层必须在 Diagnose 之前 —— 规则要靠事件内容判根因
	h.fillEventsFromCMDB(ctx, cid, dc)
	// 把 CMDB 已有的其余数据源接进来：节点压力 / 配置审计 / 镜像变更 / 内存实测
	// （见 diag_enrich.go）。全是免费数据源 —— 能在这里解决的绝不该花钱问模型
	h.enrichDiagnosis(ctx, cid, dc)
	res := diag.RuleProvider{}.Diagnose(dc)
	// Layer 2：前三层都没给出根因、且日志里连一条报错行都没有时，才走 AI。
	// ⚠️ 单次诊断也要有预算：一次调用的上限就是 1 次 ——
	//	没有闸门的话，一个刷新按钮就能被点成账单。
	// force_ai=1：人不认规则给的结论时，让 AI 再看一遍（OPSCMDB-064）。
	// ⚠️ 只放开"前面已经有结论"这一类拦截；没开 AI / 没日志 / 没额度仍然挡住。
	h.maybeAI(ctx, cid, dc, res, diag.NewAIBudget(1, loadAIConfig().MaxCostPerRoundUSD),
		c.Query("force_ai") == "1")
	logx.J("k8s", "diagnose", map[string]any{"cluster_id": cid, "ns": ns, "pod": pod, "matched": res.Matched, "root_cause": res.RootCause, "provider": res.Provider})
	SetAuditTarget(c, ns+"/"+pod)
	// 🔴 日志的敏感级别必须**随数据一起**说出来，不能只在文档里写。
	//
	//	读这份返回的多半是 AI 或脚本，它们不会去翻文档。
	//	而"脱掉了 N 处"与"可能还有没识别出来的"是两件事：
	//	只说前者，等于给出"已经干净了"的暗示（OPSCMDB-050 犯过同样的错）。
	//	⚠️ 即使 N=0 也要说 —— 0 只代表"已知形态里没命中"，不代表这份日志没有凭据。
	if len(dc.LogTails) > 0 {
		dc.LogRedactNote = "日志里已按已知形态脱掉 " + strconv.Itoa(dc.LogRedacted) +
			" 处凭据（显示为 " + diag.LogRedactedMark + "）。⚠️ 这不是完整脱敏：" +
			"无法自动判断任意字符串是不是密码，日志原文里可能仍有未识别的凭据 —— " +
			"转发或粘贴这份内容前请自行核对。"
	}
	c.JSON(http.StatusOK, gin.H{"result": res, "context": dc})
}

// fillLogsFromLoki 对 kubelet 取不到日志的容器，改用 Loki 补历史日志。
//
// 现实原因：取日志要经 APIServer 代理到节点 kubelet(10250)，节点一旦失联/繁忙就整条断掉，
// 而这类节点上的 Pod 往往正是要诊断的对象——最需要日志的时候恰恰取不到。
// Loki 是旁路采集的，不依赖 kubelet，正好补上这个盲区。
func (h *K8sDiagHandler) fillLogsFromLoki(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	// 需要回查 Loki 的两种情况：
	//
	//	① kubelet 压根取不到日志（原有逻辑）
	//	② 🔴 取到了，但**扫完一条报错都没有** —— 说明报错在更早的时间窗里，
	//	   已经被后来的输出顶出去了。
	//
	// ②	是新加的，和事件的历史回查（fillEventsFromCMDB）是同一个道理：
	//	实时的那份没有了，就去查我们自己存的历史。
	//	事件侧做了、日志侧只做了一半 —— 而"日志里没有报错"恰恰是
	//	最容易被误读成"应用没报错"的一种（实际是没看到）。
	need := map[string]bool{}
	for cn := range dc.LogErrors {
		need[cn] = true
	}
	for cn, tail := range dc.LogTails {
		if ex := diag.ExtractErrors(tail, 1); ex.NoErrorFound {
			need[cn] = true
		}
	}
	if len(need) == 0 {
		return
	}
	var env string
	_ = h.DB.QueryRow(`SELECT COALESCE(environment,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "loki", env, cid)
	if err != nil {
		return // 该集群没配 Loki，保留原始报错即可
	}
	for container := range need {
		q := fmt.Sprintf(`{namespace=%q,pod=%q}`, dc.Namespace, dc.PodName)
		// ⚠️ 拉 300 条而不是 30：这一趟的目的就是「在更大的窗口里找报错」，
		//	拉少了等于白跑。extractErrors 会把它筛成几行再展示。
		u := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&limit=300&direction=backward&start=%d&end=%d",
			base, url.QueryEscape(q), time.Now().Add(-24*time.Hour).UnixNano(), time.Now().UnixNano())
		code, body, err := obsGet(u, token, 10*time.Second)
		if err != nil || code != 200 {
			continue
		}
		lines := extractLokiLines(body, 300)
		if lines == "" {
			continue
		}
		hadKubelet := dc.LogSource[container] == "kubelet"
		// 🔴 kubelet 那份还在时，只有 Loki 里**确实有报错**才替换。
		//	否则会拿一份同样没报错的历史日志，把"此刻的日志"换掉 —— 更糟。
		if hadKubelet && diag.ExtractErrors(lines, 1).NoErrorFound {
			continue
		}
		red, rn := diag.RedactLogTail(lines)
		dc.LogTails[container] = red
		dc.LogRedacted += rn
		dc.LogSource[container] = "loki"
		if _, bad := dc.LogErrors[container]; bad {
			dc.LogErrors[container] += "（已改用 Loki 历史日志，见 log_tails）"
		} else {
			// ⚠️ 必须说清这份日志是**历史**的，而且为什么换了 ——
			//	不说的话，人会把 24 小时前的报错当成此刻的状态
			dc.LogErrors[container] = "ℹ️ kubelet 那份日志里一条报错都没有（多为被后来的输出顶出去了），" +
				"已改用 Loki 里最近 24 小时的历史日志重新提炼 —— 下面的报错说的是「当时」，不一定是此刻"
		}
	}
}

// extractLokiLines 从 Loki query_range 响应里抽出日志正文，按时间正序拼成文本。
func extractLokiLines(body string, max int) string {
	var resp struct {
		Data struct {
			Result []struct {
				Values [][]string `json:"values"` // [[ns时间戳, 日志行], ...]
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ""
	}
	type entry struct{ ts, line string }
	all := []entry{}
	for _, r := range resp.Data.Result {
		for _, v := range r.Values {
			if len(v) == 2 {
				all = append(all, entry{v[0], v[1]})
			}
		}
	}
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ts < all[j].ts })
	if len(all) > max {
		all = all[len(all)-max:]
	}
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, strings.TrimRight(e.line, "\n"))
	}
	return strings.Join(out, "\n")
}
