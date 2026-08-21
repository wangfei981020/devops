package diag

import (
	"fmt"
	"regexp"
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
		// 🔴 节点压力排在最前面：它是**因**，下面那些 Pod 级症状是**果**。
		//	顺序反了的话，一批被同一个节点拖垮的 Pod 会各自得到一个
		//	局部正确、但指错方向的结论（"镜像拉不下来"），
		//	而人会照着一个个去查应用 —— 全是白工。
		//	判据很紧（只认节点压力会导致的那几种症状），不会抢走真正的 Pod 故障。
		ruleNodeUnderPressure,
		ruleImageNeverPull,
		// 拉镜像失败改用读事件内容的细分判定（imagepull.go）。
		// 旧的 ruleImagePull 只说"镜像拉取失败"+三条通用建议，
		// 而事件里往往已经写清是缺拉取密钥 / tag 不存在 / 凭据无效 / 网络不通。
		ruleImagePullDetailed,
		ruleInitContainer,
		ruleOOM,
		ruleSegfault,
		ruleRunContainerError,
		ruleConfigError,
		ruleMultiAttachVolume,
		ruleMountFailed,
		ruleContainerCreating,
		// 🔴 探针杀掉的容器必须在这里判掉，否则会被后面的 ruleCrashLoop 认成"崩溃"。
		//	它的判据带硬条件（上次退出码只能是 0 或 143），所以不会从真崩溃手里抢走判定。
		//	实测 g32-test/share-images-frontend 重启 17144 次，退出码 0、存活探针 403 ——
		//	改这一版之前给出的是"容器启动后即崩溃，去看日志报错"，而日志里根本没有报错。
		ruleProbeKilled,
		ruleDNSFailure,
		// 🔴 日志内容规则必须夹在这里：在所有**确定性 k8s 信号**规则之后
		//	（OOMKilled/ImagePull 这些比日志里的只言片语可靠），
		//	但在 ruleCrashLoop / ruleNonZeroExit 这两条"它崩了"的兜底之前。
		//	放到兜底后面 = 永远轮不到，那正是改这一版之前的状况：
		//	重启 5431 次的 gitlab-runner 只能得到"容器异常退出（退出码 1）"，
		//	而日志里明写着对端证书 6 月 26 日就过期了。
		ruleLogSignals,
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
	// 🔴 CMDB 采到的**真实镜像变更**优先于 k8s 注解。
	//
	//	restartedAt 注解只能反映「有人手动 rollout 过」——
	//	而绝大多数发版是**换镜像 tag**，那个动作不写注解。
	//	于是"是不是刚发版引入的"这个排障时最先要问的问题，
	//	此前基本答不上来（注解为空 → "近期无 rollout 迹象"）。
	if c.ImageChange != "" {
		change = &ChangeCorrelation{
			Related: true, Source: "deploy",
			Summary: c.ImageChange,
		}
	}
	for _, r := range p.ruleset() {
		if res := r(c); res != nil {
			res.Provider = "rule"
			res.Change = change
			// 🔴 只有**泛化结论**才需要补证据提炼。
			//	规则已经判出具体根因时再塞一堆日志行，只会把结论淹掉。
			//	而泛化结论（"它崩了"）恰恰是最需要原始证据的那种。
			if res.Generic {
				attachExtracted(res, c)
			}
			return res
		}
	}
	// 未命中已知模式：把料备齐让人判断（一期诚实定位）
	unmatched := &DiagnosisResult{
		Matched:    false,
		Generic:    true,
		RootCause:  "未识别到已知故障模式",
		Confidence: "low",
		Evidence:   fallbackEvidence(c),
		Change:     change,
		Solutions: []Solution{
			{Text: "已采集事件/日志/容器状态，请人工研判"},
		},
		Provider: "rule",
	}
	attachExtracted(unmatched, c)
	return unmatched
}

// attachExtracted 规则给不出具体根因时，把日志里真实存在的报错行捞出来附上。
//
// 🔴 这一层不做判定，只做筛选和排版 —— 输出的每一行都是日志里原样有的。
// 它存在的理由是成本：**能在日志里解决的，绝不该花钱问 AI**。
//
// 捞到了 → 把「第一条报错」提到方案的最前面（根因通常是第一条，
//
//	后面的多是连锁反应）；
//
// 一条都没捞到 → 说清是"应用没留下任何自述"，这才是允许走 AI 的前提。
func attachExtracted(res *DiagnosisResult, c *DiagnosisContext) {
	// 取重启最多 / 最不健康的那个容器的日志
	var tail string
	for i := range c.Containers {
		if t := c.LogTails[c.Containers[i].Name]; t != "" {
			tail = t
			break
		}
	}
	if tail == "" {
		return
	}
	ex := ExtractErrors(tail, 8)
	res.Extracted = &ex
	if len(ex.Lines) == 0 {
		return
	}
	// ⚠️ 插在最前面：泛化规则原有的方案是"去看日志"，
	//	而这里已经把日志里该看的那几行摆出来了，顺序反了就白做
	res.Solutions = append([]Solution{{
		Text: "日志里其实写着报错，最接近根因的一条是：" + trimTo(ex.Lines[0], 300),
	}}, res.Solutions...)
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

func ruleOOM(c *DiagnosisContext) *DiagnosisResult {
	for _, cc := range c.Containers {
		oom := strings.EqualFold(cc.LastReason, "OOMKilled") || strings.EqualFold(cc.StateReason, "OOMKilled")
		exit137 := (cc.ExitCode != nil && *cc.ExitCode == 137) || (cc.LastExitCode != nil && *cc.LastExitCode == 137)
		if oom || exit137 {
			conf := "high"
			cause := "容器 OOM 被杀（内存超过 limit）"
			sol := []Solution{
				{Text: "临时：调高该容器 memory limit"},
				{Text: "根治：排查内存增长/泄漏（结合发布变更时间点）"},
				{Text: "确认是否为近期发版引入（见变更关联）"},
			}
			ev := []string{fmt.Sprintf("容器 %s 重启 %d 次", cc.Name, cc.RestartCount)}
			if cc.LastReason != "" {
				ev = append(ev, "上次终止原因: "+cc.LastReason)
			}
			// 🔴 没有实测用量的话，"调高 memory limit" 调到多少全靠猜。
			//	Prometheus 里有这条曲线，此前诊断没用它。
			if c.MemUsage != "" {
				ev = append(ev, "实测内存用量："+c.MemUsage)
				sol = append([]Solution{{
					Text: "按上面的实测峰值定新 limit（留 20~30% 余量），别拍脑袋加倍",
				}}, sol...)
			}

			// 🔴 137 = SIGKILL，**不等于 OOM**。另一个同样常见的来源是
			//	kubelet 在优雅停机超时后强杀 —— 存活探针判死、或删 Pod 时，
			//	进程没在 terminationGracePeriodSeconds 内退出就会吃 SIGKILL。
			//
			//	原来这里一律说「疑似 OOM」，给的三条方案全是内存相关。
			//	对探针强杀的容器，「调高 memory limit」是**无效建议**，
			//	而且会把人引向完全错误的方向。
			//	带 OOMKilled 标记时才是确定的 OOM；只有 137 时必须承认两种可能。
			if !oom && exit137 {
				conf = "medium"
				cause = "容器被 SIGKILL（退出码 137）—— 可能是内存超限，也可能是优雅停机超时被强杀"
				probe := false
				for i := range c.Events {
					if strings.Contains(strings.ToLower(c.Events[i].Message), "liveness probe failed") {
						probe = true
						break
					}
				}
				if probe {
					cause = "容器被 SIGKILL（退出码 137）。**同时存在存活探针失败**，" +
						"更可能是 kubelet 判死后强杀，而不是 OOM"
					ev = append(ev, "⚠️ 没有 OOMKilled 标记，但有存活探针失败记录 —— 别只按内存查")
					sol = []Solution{
						{Text: "先看存活探针：它探的端点现在通不通？探不通说明应用没起来，真根因在启动日志里"},
						{Text: "确认 terminationGracePeriodSeconds 够不够 —— 进程没在这个时间内退出就会吃 SIGKILL（137）"},
						{Text: "⚠️ 若确实是内存问题，容器状态里会带 OOMKilled 标记；只有 137 时不要直接当 OOM 处理"},
					}
				} else {
					ev = append(ev, "⚠️ 容器状态里**没有** OOMKilled 标记 —— 137 只说明被 SIGKILL，不能直接断定是 OOM")
					sol = append(sol, Solution{
						Text: "⚠️ 若内存用量并不高，考虑另一种来源：优雅停机超时被 kubelet 强杀（删 Pod / 探针判死时）",
					})
				}
			}

			if tail := lastLines(c.LogTails[cc.Name], 5); tail != "" {
				ev = append(ev, "日志末尾:\n"+tail)
			}
			return &DiagnosisResult{
				Matched: true, Confidence: conf, RootCause: cause, Evidence: ev,
				Solutions: sol,
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
		RootCause: configErrorCause(c),
		Evidence:  ev,
		Solutions: configErrorSolutions(c),
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
	ev = appendLogGap(ev, c, cc.Name)
	return &DiagnosisResult{
		Matched: true, Confidence: "high", Generic: true,
		RootCause: "容器启动后即崩溃（CrashLoopBackOff）",
		Evidence:  ev,
		Solutions: crashSolutions(c, cc.Name,
			"查看上面日志末尾的报错，多为配置错误/依赖不可用/启动参数问题"),
	}
}

// crashSolutions 组装"它崩了"这类兜底规则的方案。
//
// 日志末尾确实有报错时，照常让人去看；末尾看不出问题时**换一句能做到的**，
// 而不是把一条做不到的建议原样发出去。
func crashSolutions(c *DiagnosisContext, container, lookAtLog string) []Solution {
	out := []Solution{}
	if s := logInsightSolution(c, container); s != nil {
		out = append(out, *s)
	} else {
		out = append(out, Solution{Text: lookAtLog})
	}
	out = append(out, Solution{Text: "确认是否为近期发版引入（见变更关联），可考虑回滚"})
	return out
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
	ev = appendLogGap(ev, c, cc.Name)
	return &DiagnosisResult{
		Matched: true, Confidence: "medium", Generic: true,
		RootCause: fmt.Sprintf("容器异常退出（退出码 %d）—— %s", *code, hint),
		Evidence:  ev,
		Solutions: crashSolutions(c, cc.Name, "查看上面日志末尾定位报错"),
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

// appendLogGap 日志取不到时，把「为什么取不到」写进证据。
//
// 🔴 不写的后果很隐蔽：日志规则（ruleLogSignals）没命中会静默落到通用规则，
// 而通用规则的结论是"容器崩了，去看日志"——人打开一看日志是空的，
// 就会得出"日志里没东西"，然后往别处查。
//
// 实际是**根本没读到日志**（kubelet 不可达 / 无 pods/log 权限 / 容器没重启过）。
// 「读不到」和「读到了但没内容」是两件事，必须让人分得清。
func appendLogGap(ev []string, c *DiagnosisContext, container string) []string {
	if c.LogTails[container] != "" {
		return ev
	}
	if reason := c.LogErrors[container]; reason != "" {
		return append(ev, "⚠️ 没能读到这个容器的日志，所以上面的判断**没有用到日志**："+reason)
	}
	return append(ev, "⚠️ 没有读到这个容器的日志，上面的判断**没有用到日志**；可用 query_loki 查历史日志")
}

// tailLooksUninformative 判断这段日志末尾是不是「看了也白看」。
//
// # 为什么要判这个
//
// 实测 g50-dev/g50-plaza-game-server-backend：退出码 2、重启 11205 次，
// 而日志末尾 30 行**全是**每 3 秒一条的 `redis health check success` ——
// 真正的报错早被这些健康检查刷出去了。
//
// 这时给出「查看上面日志末尾定位报错」是**一句废话**：末尾全是 success。
// 更糟的是它看起来像个正经建议，人真的会去翻，翻完一无所获还以为是自己没看懂。
//
// 🔴 说「我没看到报错，而且知道为什么没看到」，比给一条做不到的建议有用得多。
//
// 判据（两个都满足才算）：
//   - 末尾没有任何像报错的行
//   - 且重复度高：去掉时间戳后不同的行很少（典型的心跳/健康检查刷屏）
func tailLooksUninformative(tail string) (noError, repetitive bool) {
	if tail == "" {
		return false, false
	}
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	if len(lines) < 5 {
		return false, false
	}
	noError = true
	for _, l := range lines {
		low := strings.ToLower(l)
		for _, kw := range []string{
			"error", "fail", "fatal", "panic", "exception", "emerg",
			"refused", "denied", "timeout", "unable", "cannot", "not found",
		} {
			if strings.Contains(low, kw) {
				noError = false
				break
			}
		}
		if !noError {
			break
		}
	}
	// 去掉行首的时间戳/数字再比，否则每行都"不同"
	seen := map[string]bool{}
	for _, l := range lines {
		seen[reDigits.ReplaceAllString(l, "")] = true
	}
	repetitive = len(seen)*3 <= len(lines) // 不同行数 ≤ 三分之一
	return noError, repetitive
}

var reDigits = regexp.MustCompile(`[0-9]+`)

// logInsightSolution 给出「日志末尾看不出问题」时的下一步，而不是让人去翻一段没有报错的日志。
func logInsightSolution(c *DiagnosisContext, container string) *Solution {
	tail := c.LogTails[container]
	noError, repetitive := tailLooksUninformative(tail)
	if !noError {
		return nil
	}
	msg := "⚠️ 日志末尾**没有任何报错**"
	if repetitive {
		msg += "，而且全是高频重复的正常输出（心跳/健康检查之类）—— 真正的报错已被顶出去。" +
			"用 pod_logs 把 tail 调大（比如 500 行）重看，或用 query_loki 查这个容器更早的历史日志"
		return &Solution{Text: msg}
	}
	// 🔴 不重复、也没报错 = 日志**直接断在某一步**。
	//	这比"刷屏"更有信息量：最后那行就是它做的最后一件事。
	//	实测 g50-dev/g50-vip-cbac（重启 10067 次）日志停在
	//	"GetMySqlGameDb username=root, host=mysql-8.public:3306" 之后再无输出 ——
	//	说"应用在连 MySQL 这一步之后就没声了"，比"容器启动后即崩溃"有用得多。
	last := ""
	if lines := strings.Split(strings.TrimSpace(tail), "\n"); len(lines) > 0 {
		last = strings.TrimSpace(lines[len(lines)-1])
		if len(last) > 160 {
			last = last[:160] + "…"
		}
	}
	msg += "，日志是**直接断掉**的。它做的最后一件事是：" + last +
		"\n多为在这一步卡死后被杀（探针判死/优雅停机超时），或进程被外部强制终止 —— " +
		"应用没来得及留下任何自述。先看事件里有没有 Killing/OOMKilled，再看这个节点是否正常"
	return &Solution{Text: msg}
}

// configErrorCause 配置错误的根因。
//
// 🔴 有 config_audit 的结论时必须点名缺的是什么。
//
// 此前这两个工具**各知道一半**：诊断只会说"容器配置错误（ConfigMap/Secret 引用问题）"，
// 而 config_audit 早就算出了「缺 Secret ls-nacos 的 TIDB_HOST」——
// 人得自己再调一次才知道。两个都对，合起来才有用。
func configErrorCause(c *DiagnosisContext) string {
	if len(c.ConfigIssues) == 0 {
		return "容器配置错误（ConfigMap/Secret/挂载引用问题）"
	}
	return "配置引用不存在：" + trimTo(joinLines(c.ConfigIssues, "；"), 300)
}

func configErrorSolutions(c *DiagnosisContext) []Solution {
	if len(c.ConfigIssues) == 0 {
		return []Solution{
			{Text: "检查 env/volume 引用的 ConfigMap/Secret/key 是否存在"},
			{Text: "⚠️ 用 config_audit 查这个命名空间 —— 它能确定性地告诉你缺的是哪个对象的哪个键"},
		}
	}
	return []Solution{
		{Text: "上面点名的对象确实不在。确认是漏建，还是名称/命名空间写错了"},
		{Text: "⚠️ 建好之后容器要重启才会重新读取 —— 已在跑的 Pod 不会自动感知"},
		{Text: "同一命名空间里若有多个 Pod 报同样的缺失，多半是一次配置变更漏了一环，一起补"},
	}
}

// joinLines 拼接，空元素跳过。
func joinLines(xs []string, sep string) string {
	out := ""
	for _, x := range xs {
		if x == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += x
	}
	return out
}
