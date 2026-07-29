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
	Cluster     string         `json:"cluster"`
	Namespace   string         `json:"namespace"`
	PodName     string         `json:"pod_name"`
	Phase       string         `json:"phase"`
	PodReason   string         `json:"pod_reason"`  // pod 级 reason，如 Evicted
	PodMessage  string         `json:"pod_message"`
	Restarts    int32          `json:"restarts"`
	AgeSeconds  int64          `json:"age_seconds"`
	Containers  []ContainerCtx `json:"containers"`
	Events      []EventCtx     `json:"events"`
	LogTails    map[string]string `json:"log_tails"` // container -> 日志末尾
	// 取不到日志时必须说清为什么，否则调用方（尤其 AI）会把「拿不到日志」误当成「没有日志」而漏判。
	LogErrors map[string]string `json:"log_errors,omitempty"`  // container -> 失败原因(已翻译)
	LogSource map[string]string `json:"log_source,omitempty"`  // container -> kubelet|loki

	// 变更关联（K8s 原生信号）
	OwnerKind   string    `json:"owner_kind"`
	OwnerName   string    `json:"owner_name"`
	PodCreated  time.Time `json:"pod_created"`
	RestartedAt string    `json:"restarted_at"` // kubectl.kubernetes.io/restartedAt 注解

	// 删除/卡住信号
	Terminating bool     `json:"terminating"` // DeletionTimestamp 已设置
	Finalizers  []string `json:"finalizers"`  // 阻塞删除的 finalizer
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
		case tail != "":
			dc.LogTails[st.Name] = tail
			dc.LogSource[st.Name] = "kubelet"
		}
	}
}

// getLogTail 取容器日志末尾 30 行；hasRestart 时优先取上一次实例(Previous)的日志(崩溃原因更有用)。
func getLogTail(ctx context.Context, cs *kubernetes.Clientset, ns, pod, container string, hasRestart bool) (string, error) {
	tail := int64(30)
	opts := &corev1.PodLogOptions{Container: container, TailLines: &tail}
	if hasRestart {
		opts.Previous = true
	}
	raw, err := cs.CoreV1().Pods(ns).GetLogs(pod, opts).DoRaw(ctx)
	if err != nil && hasRestart {
		// 上一次实例可能不存在，回退到当前
		opts.Previous = false
		raw, err = cs.CoreV1().Pods(ns).GetLogs(pod, opts).DoRaw(ctx)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
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
			"用「接入管理 → 集群 → 测试连接」可一次性列出还缺哪些只读权限。原始错误: " + s
	case code == 401:
		return "认证失败(HTTP 401)：集群凭证可能已过期或被吊销。原始错误: " + s
	case code == 404:
		return "Pod 或容器不存在(可能刚被重建)。原始错误: " + s
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
