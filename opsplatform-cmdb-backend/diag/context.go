package diag

import (
	"context"
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
			dc.LogErrors[st.Name] = explainLogError(err)
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

// explainLogError 把 client-go 的原始报错翻成能直接据以行动的结论。
// 取日志要经 APIServer 代理到节点 kubelet(10250)，失败形态很多但原文极不友好——
// 比如 "unknown (get pods xxx)"，光看这句分不清是没权限、Pod 不在、还是节点失联。
func explainLogError(err error) string {
	s := err.Error()
	switch {
	case apierrors.IsForbidden(err):
		return "无权限读日志(只读 RBAC 缺 pods/log): " + s
	case apierrors.IsNotFound(err):
		return "Pod 或容器不存在(可能刚被重建): " + s
	case strings.Contains(s, "context deadline exceeded"), strings.Contains(s, "Timeout"), strings.Contains(s, "timeout"):
		return "读取超时:APIServer 连节点 kubelet(10250) 超时,多为节点失联或 kubelet 繁忙: " + s
	case strings.Contains(s, "unknown ("):
		return "APIServer 无法从节点 kubelet(10250) 取到日志,多为 kubelet 不可达/证书问题;可改用 query_loki 查历史日志: " + s
	default:
		return s
	}
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
