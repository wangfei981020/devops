package diag

import "fmt"

// ruleNodeUnderPressure Pod 出问题、而它所在的节点正扛不住 —— 根因指向节点。
//
// # 为什么要单独一条，而且排得很靠前
//
// 节点磁盘满会连锁出一大片症状：镜像拉不动（ImagePullBackOff / ContainerCreating）、
// Pod 被驱逐、容器起不来。逐个诊断这些 Pod，每个都会得到一个**局部正确**的结论
// （"镜像拉不下来"），而**没有一个指向真凶**。
//
// 实测 DEV：node12 磁盘 100%、kubelet 反复 ImageGC 失败、已进入 DiskPressure 驱逐，
// 把 Jenkins 构建拖到 12 分钟拉不下 405MB 的镜像。
// 而按 Pod 一个个查，永远查不到节点头上（OPSCMDB-042）。
//
// 🔴 处置方向完全不同：
//
//	按 Pod 查 → 一个个去看应用、镜像、配置（全是白工）
//	按节点查 → 腾空间 / 赶走负载 / 扩容，一次解决一片
//
// # ⚠️ 判据必须紧
//
// 「节点有压力」+「Pod 有问题」并不总是因果关系 —— 一个节点有内存压力，
// 上面某个 Pod 因为配置错起不来，两件事没关系。
//
// 所以只在 Pod 的症状**确实是节点压力会导致的那几种**时才命中：
// 卡在创建 / 镜像拉不动 / 被驱逐。
// 应用自己崩了（CrashLoopBackOff、非零退出）不算 —— 那种和节点无关。
func ruleNodeUnderPressure(c *DiagnosisContext) *DiagnosisResult {
	if c.NodePressure == "" || c.NodeName == "" {
		return nil
	}
	// Pod 级的驱逐信号
	evicted := c.PodReason == "Evicted"

	var hit *ContainerCtx
	for i := range c.Containers {
		cc := &c.Containers[i]
		switch cc.StateReason {
		case "ContainerCreating", "PodInitializing",
			"ImagePullBackOff", "ErrImagePull", "CreateContainerError":
			hit = cc
		}
		if hit != nil {
			break
		}
	}
	if hit == nil && !evicted {
		return nil
	}

	what := "被驱逐"
	if hit != nil {
		what = fmt.Sprintf("容器 %s 卡在 %s", hit.Name, hit.StateReason)
	}
	return &DiagnosisResult{
		Matched: true, Confidence: "high",
		RootCause: fmt.Sprintf("根因在**节点**不在这个 Pod：%s 正处于资源压力（%s），"+
			"而这个 Pod %s —— 这正是节点压力会造成的症状",
			c.NodeName, c.NodePressure, what),
		Evidence: []string{
			fmt.Sprintf("节点 %s 的压力位：%s（采集时算好的摘要，只认 MemoryPressure/"+
				"DiskPressure/PIDPressure/NetworkUnavailable 为真压力）", c.NodeName, c.NodePressure),
			"⚠️ 这类节点的 ready_status 仍然是 Ready —— 只看 Ready 或节点列表的 health 看不出来",
		},
		Solutions: []Solution{
			{Text: fmt.Sprintf("🔴 别再逐个查这个节点上的 Pod —— 它们多半是同一个原因。"+
				"先看 list_events kind=Node 里 %s 的时间线"+
				"（ImageGCFailed / NodeHasDiskPressure / EvictionThresholdMet）", c.NodeName)},
			{Text: "磁盘满且 ImageGC 报「0 bytes eligible」时，占盘的不是可回收镜像层，" +
				"而是**正在跑的容器可写层 / emptyDir** —— 清镜像没用，要么赶走负载要么扩容"},
			{Text: fmt.Sprintf("止血：kubectl cordon %s 让新负载不再落上去；"+
				"⚠️ 别急着 drain —— 驱逐会把负载推到别的节点上，可能连锁", c.NodeName)},
			{Text: "用 node_usage 看这个节点的磁盘/内存曲线，确认是持续增长还是突发"},
		},
	}
}
