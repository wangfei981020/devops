package diag

import (
	"fmt"
	"strings"
	"time"
)

// RuleProvider 规则版诊断引擎：按「症状 → 原因 → 方案」匹配已知 K8s 故障模式。
type RuleProvider struct{}

func (RuleProvider) Name() string { return "rule" }

type ruleFn func(c *DiagnosisContext) *DiagnosisResult

// 规则按「具体 → 一般」排序，第一个命中即返回。
func (RuleProvider) ruleset() []ruleFn {
	return []ruleFn{
		ruleImageNeverPull,
		ruleImagePull,
		ruleInitContainer,
		ruleOOM,
		ruleSegfault,
		ruleRunContainerError,
		ruleConfigError,
		ruleMultiAttachVolume,
		ruleMountFailed,
		ruleContainerCreating,
		ruleDNSFailure,
		ruleCrashLoop,
		ruleJobDeadline,
		ruleNonZeroExit,
		rulePendingResource,
		rulePendingTaint,
		rulePendingAffinity,
		rulePendingPVC,
		rulePendingHostPort,
		rulePendingTooManyPods,
		rulePendingUnschedulable,
		ruleNodeNotReady,
		ruleStartupProbe,
		ruleLivenessProbe,
		ruleReadinessGate,
		ruleProbe,
		ruleEphemeralStorage,
		ruleEvicted,
		ruleTerminating,
		rulePendingGeneric,
	}
}

func (p RuleProvider) Diagnose(c *DiagnosisContext) *DiagnosisResult {
	change := correlateChange(c)
	for _, r := range p.ruleset() {
		if res := r(c); res != nil {
			res.Provider = "rule"
			res.Change = change
			return res
		}
	}
	// 未命中已知模式：把料备齐让人判断（一期诚实定位）
	return &DiagnosisResult{
		Matched:    false,
		RootCause:  "未识别到已知故障模式",
		Confidence: "low",
		Evidence:   fallbackEvidence(c),
		Change:     change,
		Solutions: []Solution{
			{Text: "已采集事件/日志/容器状态，请人工研判（后续接入 AI 可自动分析未知场景）"},
		},
		Provider: "rule",
	}
}

// ---------- 变更关联（K8s 原生信号；deploy 发布历史源后续接入） ----------

func correlateChange(c *DiagnosisContext) *ChangeCorrelation {
	if c.RestartedAt != "" {
		return &ChangeCorrelation{
			Related: true, Source: "k8s-native",
			Summary: fmt.Sprintf("检测到 rollout restart 注解 restartedAt=%s，疑似手动重启触发", c.RestartedAt),
		}
	}
	// Pod 很新 + 受控于工作负载 → 最近有重建/发布迹象（弱信号）
	if c.AgeSeconds > 0 && c.AgeSeconds < 15*60 && c.OwnerKind != "" {
		mins := c.AgeSeconds / 60
		return &ChangeCorrelation{
			Related: true, Source: "k8s-native",
			Summary: fmt.Sprintf("Pod 约 %d 分钟前由 %s/%s 重建，可能由发布/重启触发（接入发布历史后可确认版本与操作人）", mins, c.OwnerKind, c.OwnerName),
		}
	}
	return &ChangeCorrelation{Related: false, Source: "k8s-native", Summary: "近期无 rollout restart / 重建迹象"}
}

// ---------- helpers ----------

func waitingContainer(c *DiagnosisContext, reasons ...string) *ContainerCtx {
	for i := range c.Containers {
		cc := &c.Containers[i]
		if cc.State == "waiting" {
			for _, r := range reasons {
				if strings.EqualFold(cc.StateReason, r) {
					return cc
				}
			}
		}
	}
	return nil
}

func eventContaining(c *DiagnosisContext, substr string) *EventCtx {
	substr = strings.ToLower(substr)
	for i := range c.Events {
		e := &c.Events[i]
		if e.Type == "Warning" && (strings.Contains(strings.ToLower(e.Message), substr) || strings.Contains(strings.ToLower(e.Reason), substr)) {
			return e
		}
	}
	return nil
}

func eventByReason(c *DiagnosisContext, reason string) *EventCtx {
	for i := range c.Events {
		if strings.EqualFold(c.Events[i].Reason, reason) {
			return &c.Events[i]
		}
	}
	return nil
}

func fallbackEvidence(c *DiagnosisContext) []string {
	ev := []string{fmt.Sprintf("Phase=%s, 重启=%d, 存活=%s", c.Phase, c.Restarts, humanDur(c.AgeSeconds))}
	for _, cc := range c.Containers {
		if !cc.Ready {
			ev = append(ev, fmt.Sprintf("容器 %s: state=%s reason=%s", cc.Name, cc.State, cc.StateReason))
		}
	}
	if w := lastWarning(c); w != nil {
		ev = append(ev, fmt.Sprintf("事件: %s - %s", w.Reason, w.Message))
	}
	return ev
}

func lastWarning(c *DiagnosisContext) *EventCtx {
	var w *EventCtx
	for i := range c.Events {
		if c.Events[i].Type == "Warning" {
			w = &c.Events[i]
		}
	}
	return w
}

func humanDur(sec int64) string {
	d := time.Duration(sec) * time.Second
	switch {
	case d.Hours() >= 24:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d.Hours() >= 1:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d.Minutes() >= 1:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// ---------- 规则 ----------

func ruleImagePull(c *DiagnosisContext) *DiagnosisResult {
	cc := waitingContainer(c, "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "RegistryUnavailable")
	if cc == nil {
		return nil
	}
	ev := []string{fmt.Sprintf("容器 %s 处于 %s，镜像 %s", cc.Name, cc.StateReason, cc.Image)}
	if w := eventContaining(c, "pull"); w != nil {
		ev = append(ev, "事件: "+w.Message)
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: fmt.Sprintf("镜像拉取失败（%s）", cc.StateReason),
		Evidence:  ev,
		Solutions: []Solution{
			{Text: "核对镜像名/tag 是否正确、是否存在于 Harbor"},
			{Text: "检查 imagePullSecret 是否配置且有效"},
			{Text: "确认节点到镜像仓库网络可达"},
		},
	}
}

func ruleOOM(c *DiagnosisContext) *DiagnosisResult {
	for _, cc := range c.Containers {
		oom := strings.EqualFold(cc.LastReason, "OOMKilled") || strings.EqualFold(cc.StateReason, "OOMKilled")
		exit137 := (cc.ExitCode != nil && *cc.ExitCode == 137) || (cc.LastExitCode != nil && *cc.LastExitCode == 137)
		if oom || exit137 {
			conf := "high"
			cause := "容器 OOM 被杀（内存超过 limit）"
			if !oom && exit137 {
				conf = "medium"
				cause = "容器被 SIGKILL（退出码 137，疑似 OOM）"
			}
			ev := []string{fmt.Sprintf("容器 %s 重启 %d 次", cc.Name, cc.RestartCount)}
			if cc.LastReason != "" {
				ev = append(ev, "上次终止原因: "+cc.LastReason)
			}
			if tail := lastLines(c.LogTails[cc.Name], 5); tail != "" {
				ev = append(ev, "日志末尾:\n"+tail)
			}
			return &DiagnosisResult{
				Matched: true, Confidence: conf, RootCause: cause, Evidence: ev,
				Solutions: []Solution{
					{Text: "临时：调高该容器 memory limit"},
					{Text: "根治：排查内存增长/泄漏（结合发布变更时间点）"},
					{Text: "确认是否为近期发版引入（见变更关联）"},
				},
			}
		}
	}
	return nil
}

func ruleConfigError(c *DiagnosisContext) *DiagnosisResult {
	cc := waitingContainer(c, "CreateContainerConfigError", "CreateContainerError")
	if cc == nil {
		// 也可能体现在事件里
		if w := eventContaining(c, "not found"); w != nil && (strings.Contains(strings.ToLower(w.Message), "configmap") || strings.Contains(strings.ToLower(w.Message), "secret")) {
			return &DiagnosisResult{
				Matched: true, Confidence: "high",
				RootCause: "引用的 ConfigMap/Secret 不存在",
				Evidence:  []string{"事件: " + w.Message},
				Solutions: []Solution{{Text: "检查 envFrom/volume 引用的 ConfigMap/Secret 是否存在于同 namespace"}},
			}
		}
		return nil
	}
	ev := []string{fmt.Sprintf("容器 %s: %s", cc.Name, cc.StateReason)}
	if w := lastWarning(c); w != nil {
		ev = append(ev, "事件: "+w.Message)
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: "容器配置错误（ConfigMap/Secret/挂载引用问题）",
		Evidence:  ev,
		Solutions: []Solution{{Text: "检查 env/volume 引用的 ConfigMap/Secret/key 是否存在"}},
	}
}

func ruleMountFailed(c *DiagnosisContext) *DiagnosisResult {
	w := eventByReason(c, "FailedMount")
	if w == nil {
		w = eventByReason(c, "FailedAttachVolume")
	}
	if w == nil {
		return nil
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: "卷挂载失败，容器卡在创建",
		Evidence:  []string{"事件: " + w.Message},
		Solutions: []Solution{
			{Text: "检查 PVC 是否 Bound、底层存储是否可用"},
			{Text: "确认挂载的 ConfigMap/Secret 是否存在"},
		},
	}
}

func ruleCrashLoop(c *DiagnosisContext) *DiagnosisResult {
	cc := waitingContainer(c, "CrashLoopBackOff")
	if cc == nil {
		return nil
	}
	ev := []string{fmt.Sprintf("容器 %s 反复重启 %d 次", cc.Name, cc.RestartCount)}
	if cc.LastExitCode != nil {
		ev = append(ev, fmt.Sprintf("上次退出码: %d (%s)", *cc.LastExitCode, cc.LastReason))
	}
	tail := lastLines(c.LogTails[cc.Name], 8)
	if tail != "" {
		ev = append(ev, "日志末尾:\n"+tail)
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: "容器启动后即崩溃（CrashLoopBackOff）",
		Evidence:  ev,
		Solutions: []Solution{
			{Text: "查看上面日志末尾的报错，多为配置错误/依赖不可用/启动参数问题"},
			{Text: "确认是否为近期发版引入（见变更关联），可考虑回滚"},
		},
	}
}

func rulePendingResource(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	if w := eventContaining(c, "Insufficient"); w != nil {
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: "节点资源不足，Pod 无法调度",
			Evidence:  []string{"事件: " + w.Message},
			Solutions: []Solution{
				{Text: "扩容节点 / 清理其他负载"},
				{Text: "降低该 Pod 的 resources.requests"},
			},
		}
	}
	return nil
}

func rulePendingTaint(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	if w := eventContaining(c, "taint"); w != nil {
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: "节点有污点，Pod 缺少对应容忍（toleration）",
			Evidence:  []string{"事件: " + w.Message},
			Solutions: []Solution{{Text: "为 Pod 添加对应 toleration，或调度到无污点节点"}},
		}
	}
	return nil
}

func rulePendingAffinity(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	for _, kw := range []string{"didn't match node selector", "affinity", "node selector"} {
		if w := eventContaining(c, kw); w != nil {
			return &DiagnosisResult{
				Matched: true, Confidence: "medium",
				RootCause: "节点选择器/亲和性不满足，无可调度节点",
				Evidence:  []string{"事件: " + w.Message},
				Solutions: []Solution{{Text: "检查 nodeSelector/affinity 与节点标签是否匹配"}},
			}
		}
	}
	return nil
}

func rulePendingPVC(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	for _, kw := range []string{"persistentvolumeclaim", "unbound", "pvc"} {
		if w := eventContaining(c, kw); w != nil {
			return &DiagnosisResult{
				Matched: true, Confidence: "high",
				RootCause: "存储卷（PVC）未就绪，Pod 无法调度",
				Evidence:  []string{"事件: " + w.Message},
				Solutions: []Solution{{Text: "检查 PVC 是否 Bound、StorageClass/PV 是否可用"}},
			}
		}
	}
	return nil
}

func ruleProbe(c *DiagnosisContext) *DiagnosisResult {
	w := eventByReason(c, "Unhealthy")
	if w == nil {
		return nil
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "medium",
		RootCause: "健康检查探针失败（readiness/liveness）",
		Evidence:  []string{"事件: " + w.Message},
		Solutions: []Solution{
			{Text: "检查探针路径/端口是否正确、应用启动是否过慢"},
			{Text: "适当调大 initialDelaySeconds / failureThreshold"},
		},
	}
}

func ruleEvicted(c *DiagnosisContext) *DiagnosisResult {
	if !strings.EqualFold(c.PodReason, "Evicted") {
		return nil
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: "Pod 被驱逐（节点资源压力 DiskPressure/MemoryPressure）",
		Evidence:  []string{"Pod reason=Evicted: " + c.PodMessage},
		Solutions: []Solution{
			{Text: "清理节点磁盘 / 释放内存"},
			{Text: "为关键负载设置合理 requests 与优先级"},
		},
	}
}

func rulePendingGeneric(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	w := eventByReason(c, "FailedScheduling")
	if w == nil {
		return nil
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "medium",
		RootCause: "Pod 调度失败（FailedScheduling）",
		Evidence:  []string{"事件: " + w.Message},
		Solutions: []Solution{{Text: "查看事件详情定位约束（资源/污点/亲和性/拓扑）"}},
	}
}

// ---------- 扩充规则 ----------

// terminatedNonZero 返回首个「已终止且退出码非 0/非 137」的容器（含 init），用于通用崩溃兜底。
func terminatedNonZero(c *DiagnosisContext) *ContainerCtx {
	for i := range c.Containers {
		cc := &c.Containers[i]
		code := cc.ExitCode
		if code == nil {
			code = cc.LastExitCode
		}
		if code != nil && *code != 0 && *code != 137 {
			return cc
		}
	}
	return nil
}

// ruleImageNeverPull：imagePullPolicy=Never 但节点本地无该镜像（比一般拉取失败更具体）。
func ruleImageNeverPull(c *DiagnosisContext) *DiagnosisResult {
	cc := waitingContainer(c, "ErrImageNeverPull")
	if cc == nil {
		return nil
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: "镜像策略为 Never 但节点本地无该镜像",
		Evidence:  []string{fmt.Sprintf("容器 %s ErrImageNeverPull，镜像 %s", cc.Name, cc.Image)},
		Solutions: []Solution{
			{Text: "改 imagePullPolicy 为 IfNotPresent/Always，或预先在节点 load 该镜像"},
		},
	}
}

// ruleInitContainer：init 容器失败（崩溃/拉取失败/卡住），主容器尚未启动。
func ruleInitContainer(c *DiagnosisContext) *DiagnosisResult {
	for i := range c.Containers {
		cc := &c.Containers[i]
		if !cc.IsInit {
			continue
		}
		bad := cc.State == "waiting" && cc.StateReason != "" && !strings.EqualFold(cc.StateReason, "PodInitializing")
		failed := cc.State == "terminated" && cc.ExitCode != nil && *cc.ExitCode != 0
		crashed := cc.LastExitCode != nil && *cc.LastExitCode != 0
		if !bad && !failed && !crashed {
			continue
		}
		ev := []string{fmt.Sprintf("init 容器 %s: state=%s reason=%s 重启=%d", cc.Name, cc.State, cc.StateReason, cc.RestartCount)}
		if cc.LastExitCode != nil {
			ev = append(ev, fmt.Sprintf("上次退出码: %d", *cc.LastExitCode))
		}
		if tail := lastLines(c.LogTails[cc.Name], 6); tail != "" {
			ev = append(ev, "日志末尾:\n"+tail)
		}
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: fmt.Sprintf("init 容器 %s 失败，主容器无法启动", cc.Name),
			Evidence:  ev,
			Solutions: []Solution{
				{Text: "查看 init 容器日志/退出码，多为依赖未就绪、配置缺失或脚本报错"},
				{Text: "确认 init 依赖的服务/配置在启动时已可用"},
			},
		}
	}
	return nil
}

// ruleSegfault：容器以段错误退出（退出码 139 = 128+SIGSEGV）。
func ruleSegfault(c *DiagnosisContext) *DiagnosisResult {
	for i := range c.Containers {
		cc := &c.Containers[i]
		seg := (cc.ExitCode != nil && *cc.ExitCode == 139) || (cc.LastExitCode != nil && *cc.LastExitCode == 139)
		if !seg {
			continue
		}
		ev := []string{fmt.Sprintf("容器 %s 退出码 139（段错误 SIGSEGV），重启 %d 次", cc.Name, cc.RestartCount)}
		if tail := lastLines(c.LogTails[cc.Name], 6); tail != "" {
			ev = append(ev, "日志末尾:\n"+tail)
		}
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: "容器段错误崩溃（退出码 139 / SIGSEGV）",
			Evidence:  ev,
			Solutions: []Solution{
				{Text: "多为程序 bug、依赖库不兼容或内存越界，结合日志与发布变更排查"},
				{Text: "确认是否为近期发版引入（见变更关联），可考虑回滚"},
			},
		}
	}
	return nil
}

// ruleRunContainerError：容器无法启动（启动命令/二进制不存在、运行时拒绝）。
func ruleRunContainerError(c *DiagnosisContext) *DiagnosisResult {
	cc := waitingContainer(c, "RunContainerError")
	if cc == nil {
		return nil
	}
	ev := []string{fmt.Sprintf("容器 %s: RunContainerError", cc.Name)}
	if w := lastWarning(c); w != nil {
		ev = append(ev, "事件: "+w.Message)
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: "容器无法启动（RunContainerError）",
		Evidence:  ev,
		Solutions: []Solution{
			{Text: "检查 command/args 是否正确、入口二进制是否存在且可执行"},
			{Text: "确认挂载路径/工作目录与权限配置无误"},
		},
	}
}

// ruleContainerCreating：长时间卡在 ContainerCreating（多为 CNI 网络 / sandbox 创建失败）。
func ruleContainerCreating(c *DiagnosisContext) *DiagnosisResult {
	cc := waitingContainer(c, "ContainerCreating")
	if cc == nil {
		return nil
	}
	// 短时间的 ContainerCreating 属正常，超过 2 分钟才视为卡住
	if c.AgeSeconds < 120 {
		return nil
	}
	ev := []string{fmt.Sprintf("容器 %s 卡在 ContainerCreating 已 %s", cc.Name, humanDur(c.AgeSeconds))}
	for _, kw := range []string{"sandbox", "network", "cni", "FailedCreatePodSandBox"} {
		if w := eventContaining(c, kw); w != nil {
			ev = append(ev, "事件: "+w.Message)
			break
		}
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "medium",
		RootCause: "容器长时间卡在创建（多为 CNI 网络 / Pod sandbox 创建失败）",
		Evidence:  ev,
		Solutions: []Solution{
			{Text: "检查节点 CNI 插件、网络配置与 kubelet 状态"},
			{Text: "确认 ConfigMap/Secret/卷挂载是否就绪"},
		},
	}
}

// ruleJobDeadline：Job 超过 activeDeadlineSeconds 被终止。
func ruleJobDeadline(c *DiagnosisContext) *DiagnosisResult {
	if !strings.EqualFold(c.PodReason, "DeadlineExceeded") && eventByReason(c, "DeadlineExceeded") == nil {
		return nil
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: "Job 超时被终止（DeadlineExceeded）",
		Evidence:  []string{"Pod/事件 reason=DeadlineExceeded: " + c.PodMessage},
		Solutions: []Solution{
			{Text: "确认任务实际耗时，适当调大 activeDeadlineSeconds"},
			{Text: "排查任务为何变慢/卡住（依赖、数据量、死锁）"},
		},
	}
}

// ruleNonZeroExit：容器以非 0 退出但尚未进入 CrashLoopBackOff（通用崩溃兜底）。
func ruleNonZeroExit(c *DiagnosisContext) *DiagnosisResult {
	cc := terminatedNonZero(c)
	if cc == nil {
		return nil
	}
	code := cc.ExitCode
	if code == nil {
		code = cc.LastExitCode
	}
	hint := "应用启动/运行报错"
	if *code == 143 {
		hint = "容器收到 SIGTERM 被终止（退出码 143），多为优雅停机或被驱逐"
	} else if *code == 1 || *code == 2 {
		hint = "应用以错误码退出（配置错误/参数错误/依赖不可用）"
	}
	ev := []string{fmt.Sprintf("容器 %s 退出码 %d，重启 %d 次", cc.Name, *code, cc.RestartCount)}
	if tail := lastLines(c.LogTails[cc.Name], 8); tail != "" {
		ev = append(ev, "日志末尾:\n"+tail)
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "medium",
		RootCause: fmt.Sprintf("容器异常退出（退出码 %d）—— %s", *code, hint),
		Evidence:  ev,
		Solutions: []Solution{
			{Text: "查看上面日志末尾定位报错"},
			{Text: "确认是否为近期发版引入（见变更关联）"},
		},
	}
}

// rulePendingHostPort：hostPort 端口冲突导致无法调度。
func rulePendingHostPort(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	for _, kw := range []string{"free ports", "host port", "hostport"} {
		if w := eventContaining(c, kw); w != nil {
			return &DiagnosisResult{
				Matched: true, Confidence: "high",
				RootCause: "hostPort 端口冲突，节点无空闲端口",
				Evidence:  []string{"事件: " + w.Message},
				Solutions: []Solution{
					{Text: "避免使用 hostPort，改用 Service/NodePort"},
					{Text: "若必须 hostPort，确保每节点同端口仅一个副本"},
				},
			}
		}
	}
	return nil
}

// rulePendingTooManyPods：节点 Pod 数达到上限（默认 110）。
func rulePendingTooManyPods(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	if w := eventContaining(c, "too many pods"); w != nil {
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: "节点 Pod 数已达上限，无法再调度",
			Evidence:  []string{"事件: " + w.Message},
			Solutions: []Solution{
				{Text: "扩容节点，或调高 kubelet --max-pods"},
				{Text: "清理节点上闲置 Pod"},
			},
		}
	}
	return nil
}

// ruleNodeNotReady：所在节点 NotReady，Pod 受影响。
func ruleNodeNotReady(c *DiagnosisContext) *DiagnosisResult {
	w := eventByReason(c, "NodeNotReady")
	if w == nil {
		if e := eventContaining(c, "node is not ready"); e != nil {
			w = e
		}
	}
	if w == nil {
		return nil
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "medium",
		RootCause: "所在节点 NotReady，Pod 无法正常运行",
		Evidence:  []string{"事件: " + w.Message},
		Solutions: []Solution{
			{Text: "检查节点状态（kubelet、网络、磁盘/内存压力）"},
			{Text: "节点长期不可恢复时驱逐/替换节点"},
		},
	}
}

// ruleMultiAttachVolume：RWO 卷被其他节点占用，Multi-Attach 报错（迁移/重调度常见）。
func ruleMultiAttachVolume(c *DiagnosisContext) *DiagnosisResult {
	if w := eventContaining(c, "multi-attach"); w != nil {
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: "卷 Multi-Attach 冲突（RWO 卷仍被其他节点上的 Pod 占用）",
			Evidence:  []string{"事件: " + w.Message},
			Solutions: []Solution{
				{Text: "等待旧 Pod 在原节点完全终止释放卷"},
				{Text: "旧节点失联时手动清理 VolumeAttachment，或卷改用 RWX"},
			},
		}
	}
	return nil
}

// ruleDNSFailure：容器内 DNS 解析失败（连不上依赖服务，日志常见 no such host）。
func ruleDNSFailure(c *DiagnosisContext) *DiagnosisResult {
	for _, tail := range c.LogTails {
		low := strings.ToLower(tail)
		if strings.Contains(low, "no such host") || strings.Contains(low, "name resolution") ||
			strings.Contains(low, "could not resolve host") || strings.Contains(low, "temporary failure in name resolution") {
			return &DiagnosisResult{
				Matched: true, Confidence: "medium",
				RootCause: "DNS 解析失败，容器无法连接依赖服务",
				Evidence:  []string{"日志末尾:\n" + lastLines(tail, 6)},
				Solutions: []Solution{
					{Text: "确认目标 Service 名/命名空间是否正确（svc.ns.svc.cluster.local）"},
					{Text: "检查 CoreDNS 是否健康、Pod 的 dnsPolicy/dnsConfig"},
				},
			}
		}
	}
	return nil
}

// rulePendingUnschedulable：节点被封锁/不可调度（cordon），无可用节点。
func rulePendingUnschedulable(c *DiagnosisContext) *DiagnosisResult {
	if c.Phase != "Pending" {
		return nil
	}
	for _, kw := range []string{"unschedulable", "were unschedulable", "had untolerated taint {node.kubernetes.io/unschedulable"} {
		if w := eventContaining(c, kw); w != nil {
			return &DiagnosisResult{
				Matched: true, Confidence: "medium",
				RootCause: "无可调度节点（节点被 cordon / 标记 unschedulable）",
				Evidence:  []string{"事件: " + w.Message},
				Solutions: []Solution{
					{Text: "确认是否有节点被 cordon，必要时 uncordon"},
					{Text: "扩容可调度节点"},
				},
			}
		}
	}
	return nil
}

// ruleStartupProbe：启动探针失败（应用启动慢，startupProbe 未通过即被杀）。
func ruleStartupProbe(c *DiagnosisContext) *DiagnosisResult {
	if w := eventContaining(c, "startup probe failed"); w != nil {
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: "启动探针失败（startupProbe 超时，应用启动过慢）",
			Evidence:  []string{"事件: " + w.Message},
			Solutions: []Solution{
				{Text: "调大 startupProbe 的 failureThreshold × periodSeconds 给足启动时间"},
				{Text: "排查应用启动慢的原因（依赖、预热、资源不足）"},
			},
		}
	}
	return nil
}

// ruleLivenessProbe：存活探针失败导致反复重启（区别于就绪探针）。
func ruleLivenessProbe(c *DiagnosisContext) *DiagnosisResult {
	if w := eventContaining(c, "liveness probe failed"); w != nil {
		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: "存活探针失败，容器被反复重启（livenessProbe）",
			Evidence:  []string{"事件: " + w.Message},
			Solutions: []Solution{
				{Text: "确认探针路径/端口正确、超时是否过短"},
				{Text: "应用偶发卡顿时适当调大 timeoutSeconds / failureThreshold"},
			},
		}
	}
	return nil
}

// ruleReadinessGate：Pod readiness gate 未满足，始终不就绪（不进入 Endpoints）。
func ruleReadinessGate(c *DiagnosisContext) *DiagnosisResult {
	if w := eventContaining(c, "readiness gate"); w != nil {
		return &DiagnosisResult{
			Matched: true, Confidence: "medium",
			RootCause: "Pod readiness gate 未满足，始终不就绪",
			Evidence:  []string{"事件: " + w.Message},
			Solutions: []Solution{
				{Text: "检查对应的自定义 condition 由哪个控制器写入（如 LB/网格）"},
				{Text: "确认该控制器是否正常工作"},
			},
		}
	}
	return nil
}

// ruleEphemeralStorage：临时存储超限被驱逐/终止。
func ruleEphemeralStorage(c *DiagnosisContext) *DiagnosisResult {
	for _, kw := range []string{"ephemeral-storage", "ephemeral local storage", "emptydir volume exceeds the limit"} {
		if w := eventContaining(c, kw); w != nil {
			return &DiagnosisResult{
				Matched: true, Confidence: "high",
				RootCause: "临时存储（ephemeral-storage）超限，Pod 被驱逐/终止",
				Evidence:  []string{"事件: " + w.Message},
				Solutions: []Solution{
					{Text: "调大 ephemeral-storage limit，或减少容器写本地盘/日志量"},
					{Text: "大数据落盘改用 PVC 持久卷"},
				},
			}
		}
	}
	return nil
}

// ruleTerminating：Pod 长时间卡在 Terminating（多为 finalizer 未移除或优雅停机超时）。
func ruleTerminating(c *DiagnosisContext) *DiagnosisResult {
	if !c.Terminating {
		return nil
	}
	ev := []string{"Pod 已标记删除（DeletionTimestamp 已设置）但仍存在"}
	if len(c.Finalizers) > 0 {
		ev = append(ev, "阻塞的 finalizer: "+strings.Join(c.Finalizers, ", "))
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "medium",
		RootCause: "Pod 卡在 Terminating（finalizer 未移除 / 优雅停机超时 / 节点失联）",
		Evidence:  ev,
		Solutions: []Solution{
			{Text: "检查阻塞的 finalizer 对应控制器是否正常工作"},
			{Text: "确认进程是否忽略 SIGTERM 导致 terminationGracePeriod 超时"},
			{Text: "节点失联时，待节点恢复或由控制面清理"},
		},
	}
}
