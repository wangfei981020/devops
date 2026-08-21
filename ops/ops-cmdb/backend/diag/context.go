package diag

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiagnosisContext 是诊断引擎的统一输入（规则版与未来 AI 版共用同一套采集结果）。
type DiagnosisContext struct {
	Cluster    string            `json:"cluster"`
	Namespace  string            `json:"namespace"`
	PodName    string            `json:"pod_name"`
	Phase      string            `json:"phase"`
	PodReason  string            `json:"pod_reason"` // pod 级 reason，如 Evicted
	PodMessage string            `json:"pod_message"`
	Restarts   int32             `json:"restarts"`
	AgeSeconds int64             `json:"age_seconds"`
	Containers []ContainerCtx    `json:"containers"`
	Events     []EventCtx        `json:"events"`
	LogTails   map[string]string `json:"log_tails"` // container -> 日志末尾
	// 取不到日志时必须说清为什么，否则调用方（尤其 AI）会把「拿不到日志」误当成「没有日志」而漏判。
	LogErrors map[string]string `json:"log_errors,omitempty"` // container -> 失败原因(已翻译)
	LogSource map[string]string `json:"log_source,omitempty"` // container -> kubelet|loki
	// LogRedacted 日志里被脱敏掉的凭据处数。
	//
	// 🔴 **必须发出去，哪怕是 0**。
	//	调用方（尤其 AI）需要知道这份日志被动过 —— 否则它会把 ***REDACTED***
	//	当成应用真的打印了这几个字，进而给出荒唐的结论。
	//	而报 0 也不等于干净：脱敏只覆盖已知形态（见 LogRedactNote）。
	LogRedacted int `json:"log_redacted"`
	// LogRedactNote 一句话说清这份日志的敏感级别与脱敏的局限。
	LogRedactNote string `json:"log_redact_note,omitempty"`

	// 变更关联（K8s 原生信号）
	OwnerKind   string    `json:"owner_kind"`
	OwnerName   string    `json:"owner_name"`
	PodCreated  time.Time `json:"pod_created"`
	RestartedAt string    `json:"restarted_at"` // kubectl.kubernetes.io/restartedAt 注解

	// 删除/卡住信号
	Terminating bool     `json:"terminating"` // DeletionTimestamp 已设置
	Finalizers  []string `json:"finalizers"`  // 阻塞删除的 finalizer

	// EventNote 关于「这些事件是哪来的」的说明。
	//
	// 目前只有一种情况会填：实时事件已过期，用了 CMDB 采集的历史。
	// ⚠️ 它必须被带进结论的证据里 —— 使用者不知道事件是历史的话，
	// 会把几天前的报错当成此刻的状态。
	EventNote string `json:"event_note,omitempty"`

	// NodeName / NodePressure 这个 Pod 落在哪个节点、那个节点有没有资源压力。
	//
	// # 🔴 为什么 Pod 诊断必须知道节点的事
	//
	// 节点磁盘满 → 镜像拉不动 → 一批 Pod 卡在 ContainerCreating。
	// 逐个诊断这些 Pod，每个都会得到"镜像拉不下来"，
	// **没有一个会说"因为它所在的节点磁盘满了"**。
	//
	// 实测 DEV node12 磁盘 100%，把 Jenkins 构建拖到 12 分钟拉不下镜像；
	// 而按 Pod 一个个查，永远查不到节点头上（OPSCMDB-042）。
	//
	// ⚠️ 节点越多这越常见 —— 生产集群比 DEV 更需要这一条。
	NodeName     string `json:"node_name,omitempty"`
	NodePressure string `json:"node_pressure,omitempty"` // 压力位摘要，空=没压力

	// ConfigIssues 这个 Pod 引用了但不存在的 ConfigMap/Secret。
	//
	// 来自 config_audit 的确定性判定（Secret 名录取自 KSM 的 kube_secret_info）。
	// 此前它是个**独立工具**：诊断只会说"容器配置错误（ConfigMap/Secret 引用问题）"，
	// 人还得自己再调一次 config_audit 才知道到底缺什么 ——
	// 两个工具各知道一半，没接起来。
	ConfigIssues []string `json:"config_issues,omitempty"`

	// ImageChange 这个工作负载最近一次镜像变更（来自 CMDB 采集的 k8s_changes）。
	//
	// ⚠️ 原来的变更关联**只看 k8s 的 restartedAt 注解** —— 那只能反映
	// 「有人手动 rollout 过」，看不到「镜像 tag 换了」。
	// 而"是不是刚发版引入的"恰恰是排障时最先要问的一句。
	ImageChange string `json:"image_change,omitempty"`

	// MemUsage OOM 类问题的实测内存用量（来自 Prometheus）。
	// 没有它的话，方案只能说"调高 limit"，调到多少全靠猜。
	MemUsage string `json:"mem_usage,omitempty"`
}

type ContainerCtx struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Ready        bool   `json:"ready"`
	IsInit       bool   `json:"is_init"` // 是否 init 容器
	RestartCount int32  `json:"restart_count"`
	State        string `json:"state"` // running/waiting/terminated
	StateReason  string `json:"state_reason"`
	ExitCode     *int32 `json:"exit_code"`
	LastReason   string `json:"last_reason"`
	LastExitCode *int32 `json:"last_exit_code"`
}

type EventCtx struct {
	Type     string    `json:"type"` // Normal/Warning
	Reason   string    `json:"reason"`
	Message  string    `json:"message"`
	Count    int32     `json:"count"`
	LastSeen time.Time `json:"last_seen"`

	// Historical 这条事件来自 **CMDB 采集的历史**，不是集群上的实时事件。
	//
	// # 为什么要这个标记
	//
	// K8s 事件默认只留 1 小时。挂了很久的 Pod，带原因的那条早没了，
	// 集群上只剩无穷无尽的 BackOff 重试记录 —— 于是诊断只能说"判不出来"。
	//
	// 但 CMDB **自己采了事件**（k8s_events 表），那条原因往往还在库里。
	// 实测 DEV/metersphere2：13 个 Pod 集群上查不到原因，
	// 而 CMDB 库里明明白白写着
	//	Failed to pull image "docker.io/bitnami/kubectl:1.28.2-debian-11-r16": ... not found
	// 同一批里另外 2 个 Pod（事件恰好还没过期）就正常判出来了 ——
	// 同一个根因，差别只在事件有没有过期，而过期的那份我们自己存着。
	//
	// 🔴 但**必须标出来**：历史事件说的是"当时"，不是"此刻"。
	// 不标的话，人会把几天前的报错当成现在的状态 ——
	// 而那正是"看起来像正常、其实答非所问"的典型。
	Historical bool `json:"historical,omitempty"`
}

// Collect 采集一个 Pod 的完整诊断上下文（纯只读：get pod / list events / read logs）。
func Collect(ctx context.Context, cs *kubernetes.Clientset, cluster, ns, name string) (*DiagnosisContext, error) {
	pod, err := cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	dc := &DiagnosisContext{
		Cluster: cluster, Namespace: ns, PodName: name,
		Phase:      string(pod.Status.Phase),
		PodReason:  pod.Status.Reason,
		PodMessage: pod.Status.Message,
		PodCreated: pod.CreationTimestamp.Time,
		AgeSeconds: int64(time.Since(pod.CreationTimestamp.Time).Seconds()),
		LogTails:   map[string]string{},
		LogErrors:  map[string]string{},
		LogSource:  map[string]string{},
	}
	if a := pod.Annotations["kubectl.kubernetes.io/restartedAt"]; a != "" {
		dc.RestartedAt = a
	}
	if len(pod.OwnerReferences) > 0 {
		dc.OwnerKind = pod.OwnerReferences[0].Kind
		dc.OwnerName = pod.OwnerReferences[0].Name
	}
	if pod.DeletionTimestamp != nil {
		dc.Terminating = true
		dc.Finalizers = pod.Finalizers
	}

	// init 容器在前（其失败会卡住主容器启动），再到工作容器
	for _, st := range pod.Status.InitContainerStatuses {
		dc.collectContainer(ctx, cs, ns, name, st, true)
	}
	for _, st := range pod.Status.ContainerStatuses {
		dc.collectContainer(ctx, cs, ns, name, st, false)
	}

	// 事件（按 involvedObject.name 过滤）
	evs, err := cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + name,
	})
	if err == nil {
		for _, e := range evs.Items {
			last := e.LastTimestamp.Time
			if last.IsZero() {
				last = e.EventTime.Time
			}
			dc.Events = append(dc.Events, EventCtx{
				Type: e.Type, Reason: e.Reason, Message: e.Message,
				Count: e.Count, LastSeen: last,
			})
		}
	}

	return dc, nil
}

// collectContainer 把一个容器状态转为 ContainerCtx 并按需拉日志末尾，追加到 dc。
func (dc *DiagnosisContext) collectContainer(ctx context.Context, cs *kubernetes.Clientset, ns, pod string, st corev1.ContainerStatus, isInit bool) {
	dc.Restarts += st.RestartCount
	cc := ContainerCtx{
		Name: st.Name, Image: st.Image, Ready: st.Ready, IsInit: isInit, RestartCount: st.RestartCount,
	}
	switch {
	case st.State.Running != nil:
		cc.State = "running"
	case st.State.Waiting != nil:
		cc.State = "waiting"
		cc.StateReason = st.State.Waiting.Reason
	case st.State.Terminated != nil:
		cc.State = "terminated"
		cc.StateReason = st.State.Terminated.Reason
		ec := st.State.Terminated.ExitCode
		cc.ExitCode = &ec
	}
	if st.LastTerminationState.Terminated != nil {
		cc.LastReason = st.LastTerminationState.Terminated.Reason
		ec := st.LastTerminationState.Terminated.ExitCode
		cc.LastExitCode = &ec
	}
	dc.Containers = append(dc.Containers, cc)

	// 仅对「未就绪 或 有重启」的容器拉日志末尾，控制开销。init 容器正常完成(exit 0)不拉。
	if (!st.Ready || st.RestartCount > 0) && !(isInit && cc.ExitCode != nil && *cc.ExitCode == 0) {
		tail, err := getLogTail(ctx, cs, ns, pod, st.Name, st.RestartCount > 0)
		switch {
		case err != nil:
			dc.LogErrors[st.Name] = ExplainLogError(err)
		// kubelet 用 200 + 占位符表示"没有日志可给"。当成内容存下去，
		// 就是把「读不到」伪装成「读到了」——必须归到 LogErrors 那一边
		case isLogPlaceholder(tail):
			dc.LogErrors[st.Name] = "容器运行时没有保留这段日志（kubelet 原话：" + strings.TrimSpace(tail) + "）。" +
				"上一个实例的日志常被运行时回收，查历史一律用 query_loki"
		case tail != "":
			// 转发日志等于扩大泄露面 —— 同一产品另一处写着「Secret 拒绝返回」（OPSCMDB-065）
			red, n := RedactLogTail(tail)
			dc.LogTails[st.Name] = red
			dc.LogRedacted += n
			dc.LogSource[st.Name] = "kubelet"
		}
	}
}

// getLogTail 取容器日志末尾 30 行；hasRestart 时优先取上一次实例(Previous)的日志(崩溃原因更有用)。
func getLogTail(ctx context.Context, cs *kubernetes.Clientset, ns, pod, container string, hasRestart bool) (string, error) {
	// 🔴 30 行不够。真正的报错常常不在末尾 —— 它在中间，被后面的心跳/
	//	健康检查刷出去了。实测 g50-plaza：末尾 30 行全是每 3 秒一条的
	//	`redis health check success`，一条报错都看不到。
	//
	//	放大到 200 行，配合 ExtractErrors 把报错行捞出来（见 evidence.go）。
	//	⚠️ 展示给人的仍然只是末尾几行 + 捞出来的报错行，不会把 200 行糊到界面上。
	//
	//	代价：每次多传约 10 KB，比走一次 AI 便宜四五个数量级 ——
	//	能在日志里解决的，绝不该花钱问模型。
	tail := int64(200)
	opts := &corev1.PodLogOptions{Container: container, TailLines: &tail}
	if hasRestart {
		opts.Previous = true
	}
	raw, err := cs.CoreV1().Pods(ns).GetLogs(pod, opts).DoRaw(ctx)
	// 上一次实例取不到时回退到当前实例。
	//
	// 🔴 「取不到」有两种形态，必须都回退：
	//	① 报错（HTTP 400 "previous terminated container not found"）
	//	② **HTTP 200 + 占位符**（containerd 把上个实例的日志回收了）
	//
	// 原来只处理了 ①。于是 ② 那种情况下拿到一句占位符就收工了 ——
	// 而当前实例的日志明明读得到。本地实测：容器刚打印完 x509 报错就退出，
	// 诊断却只拿到 "unable to retrieve container logs for containerd://…"，
	// 真正的报错一行都没看到。
	if hasRestart && (err != nil || isLogPlaceholder(strings.TrimSpace(string(raw)))) {
		opts.Previous = false
		if raw2, err2 := cs.CoreV1().Pods(ns).GetLogs(pod, opts).DoRaw(ctx); err2 == nil {
			raw, err = raw2, nil
		} else if err == nil {
			// 上一次实例给的是占位符、当前实例又读失败：
			// 保留占位符让上层归到 LogErrors，别把 err2 吞掉
			err = err2
		}
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// kubelet 在**没有日志可给**时，会返回 HTTP 200 + 一句占位文字，而不是报错。
// 常见于容器运行时已经把上一个实例的日志回收掉（containerd/docker-desktop 尤其明显）。
//
// 🔴 不识别它的后果：这句占位符会被当成**日志内容**存进 LogTails。
// 于是「读不到日志」伪装成了「日志里就这一行」——
// 规则拿着垃圾去匹配，一条都命中不了；界面上还显示得有模有样。
// 本地实测就是这么被骗了一次：明明打印了 x509 报错，诊断却只给出
// 「容器异常退出（退出码 1）」，因为拿到的根本不是那份日志。
//
// ⚠️ 判据只匹配**整段**就是占位符的情况。真日志里偶然出现这句话（比如应用自己打印了
// 一段 kubelet 报错）不该被吞掉，那时它前后还有别的行。
var logPlaceholders = []string{
	"unable to retrieve container logs for",
	"failed to try resolving symlinks in path",
	"the server rejected our request",
}

// isLogPlaceholder 判断这段"日志"其实是 kubelet 的占位符。
func isLogPlaceholder(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || strings.Contains(t, "\n") {
		return false // 多行 = 有真内容
	}
	low := strings.ToLower(t)
	for _, p := range logPlaceholders {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// ExplainLogError 把 client-go 的原始报错翻成能直接据以行动的结论。
//
// 取日志要经 APIServer 代理到节点 kubelet(10250)，失败形态多且原文极不友好。
// 最常见的 "unknown (get pods xxx)" 只是 client-go 无法把响应体解析成 Status 对象时的
// 兜底文案，它本身不含任何原因信息——真正的原因在 HTTP 状态码里。
// 所以这里一律以状态码为主判据：早先按错误文本匹配，结果 403 与「kubelet 不可达」
// 都长成 "unknown (...)"，前一个分支把后一个吞掉，导致所有失败都被报成权限问题。
func ExplainLogError(err error) string {
	s := err.Error()
	switch code := apiStatusCode(err); {
	case code == 403:
		// 只说「去确认 ClusterRole」不够用：GKE 经 GCP 服务账号接入时，集群里根本没有对应的
		// ClusterRole 可查，权限来自 GCP IAM 角色映射，两种接入方式的修法完全不同。
		return "无权限读日志(HTTP 403)：当前凭据缺 pods/log 的 get 权限。" +
			"① kubeconfig/自管集群：给该 SA 绑定的 ClusterRole 补一条 " +
			`{apiGroups:[""], resources:["pods/log"], verbs:["get"]}；` +
			"② GKE 经 GCP 服务账号接入：roles/container.clusterViewer 不含读日志权限，" +
			"需改用 roles/container.viewer，或在集群内把该 SA 邮箱绑到含 pods/log 的 ClusterRole。" +
			"用「集群 → 集群 → 测试连通性」可一次性列出还缺哪些只读权限。原始错误: " + s
	case code == 401:
		return "认证失败(HTTP 401)：集群凭证可能已过期或被吊销。原始错误: " + s
	case code == 404:
		return "Pod 或容器不存在(可能刚被重建)。原始错误: " + s
	case code == 400:
		// client-go 拿不到 APIServer 的原始 message：GetLogs 的错误响应没被解析成 Status，
		// 于是 err.Error() 只剩 "the server rejected our request for an unknown reason"，
		// 对使用者零信息量。而 APIServer 其实说得很清楚，实测原话是：
		//   previous terminated container "backend" in pod "xxx" not found   (reason=BadRequest, code=400)
		// 这个 400 绝大多数就是 previous=1 但容器根本没重启过，必须替使用者翻译出来。
		return "APIServer 拒绝了这次日志请求(HTTP 400)。" +
			"① 若本次带了 previous=1：该容器没有「上一个已终止的实例」——它从未重启过，" +
			"或上个实例的日志已被节点回收；先用 list_pods 确认 restarts>0 再用 previous。" +
			"② 若没带 previous：多为容器名写错，多容器 Pod 必须显式指定 container。" +
			"查更早的历史一律用 query_loki。原始错误: " + s
	case strings.Contains(s, "context deadline exceeded"), strings.Contains(s, "Timeout"), strings.Contains(s, "timeout"):
		return "读取超时：APIServer 连节点 kubelet(10250) 超时，多为节点失联或 kubelet 繁忙；" +
			"可改用 query_loki 查历史日志。原始错误: " + s
	case code >= 500:
		return "APIServer 未能从节点 kubelet(10250) 取到日志(HTTP " + strconv.Itoa(int(code)) + ")，" +
			"多为 kubelet 不可达或证书问题；可改用 query_loki 查历史日志。原始错误: " + s
	default:
		return s
	}
}

// apiStatusCode 取 K8s API 错误的 HTTP 状态码；非 API 错误返回 0。
// 用 errors.As 而不是类型断言：client-go 会包装错误，直接断言取不到。
func apiStatusCode(err error) int32 {
	var st apierrors.APIStatus
	if errors.As(err, &st) {
		return st.Status().Code
	}
	return 0
}

// lastLines 返回字符串末尾 n 行（给规则贴日志用）。
func lastLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
