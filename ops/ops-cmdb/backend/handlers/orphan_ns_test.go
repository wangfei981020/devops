package handlers

import (
	"strings"
	"testing"
)

// 孤儿命名空间判定。
//
// 守的是 OPSCMDB-031 P0-1 —— 那一轮验证里**最危险的一条**：
// `list_orphans` 报出 53 个「孤儿命名空间」，其中包括
//
//	g32-uat-devopsrkf2b       666 个 ConfigMap（KubeSphere DevOps 项目）
//	kubesphere-devops-worker  Jenkins agent 的落点，空闲时本来就 0 Pod
//	harbor                    镜像仓库
//
// 而每条都配着 `kubectl delete ns <名字>` —— **照做会搞坏整个构建体系**。
//
// 根因是判据只看「有没有工作负载与 Pod」，而有整整一类命名空间
// 平时本来就是 0 个 Pod（CI/CD agent、Job 专用、配置型项目空间）。
func TestOnDemandNamespace(t *testing.T) {
	mustSkip := []string{
		// 实测报出来的那几个
		"g32-uat-devopsrkf2b",
		"g32-test-devopsmgsmw",
		"g50-uat-devopssxzv7",
		"kubesphere-devops-worker",
		"kubesphere-controls-system",
		"harbor",
		// 平台组件：暂时没 Pod 也绝不该被当成可回收资源
		"argocd",
		"istio-system",
		"cert-manager",
		"monitoring",
		"logging",
	}
	for _, ns := range mustSkip {
		t.Run("skip/"+ns, func(t *testing.T) {
			if why := onDemandNamespace(ns); why == "" {
				t.Errorf("%q 被判成可回收的孤儿 —— 照着删除命令执行会真出事", ns)
			}
		})
	}

	// 反面：普通业务命名空间不该被豁免，否则这个判定会把真孤儿也放过
	mustNotSkip := []string{"g32-prod-game", "wallet", "pa-re", "my-app"}
	for _, ns := range mustNotSkip {
		t.Run("keep/"+ns, func(t *testing.T) {
			if why := onDemandNamespace(ns); why != "" {
				t.Errorf("%q 被豁免了（理由 %q）—— 判定过宽会让真正的空命名空间查不出来", ns, why)
			}
		})
	}
}

// 「装着大量配置」的门槛必须低于实测值，且远低于最小的那个（132）。
//
// ⚠️ 阈值判错的两个方向代价完全不对称：
//
//	多要一次人工确认   → 多花两分钟
//	漏掉一个配置命名空间 → 照着删除命令执行，搞坏整个构建体系
func TestConfigHeavyThresholdCoversRealCases(t *testing.T) {
	// 实测的三个 KubeSphere DevOps 项目
	for _, n := range []int{132, 608, 666} {
		if n < configHeavyNamespaceCM {
			t.Errorf("实测 %d 个 ConfigMap 的命名空间没被门槛(%d)覆盖", n, configHeavyNamespaceCM)
		}
	}
	// k8s 每个 ns 默认自带 1 个 kube-root-ca.crt —— 那不算"装着配置"
	if configHeavyNamespaceCM <= 1 {
		t.Error("门槛太低：每个命名空间默认就有 1 个 ConfigMap，这会把真空的也判成需确认")
	}
}

// 豁免理由必须是**人能看懂的一句话**，不是一个代号。
// 这条理由会出现在日志里，排查"为什么这个 ns 没被报出来"时靠它。
func TestOnDemandReasonIsHumanReadable(t *testing.T) {
	why := onDemandNamespace("g32-uat-devopsrkf2b")
	if len(why) < 10 || !strings.Contains(why, "Pod") {
		t.Errorf("豁免理由太简略，排查时看不懂：%q", why)
	}
}
