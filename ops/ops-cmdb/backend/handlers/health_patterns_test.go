package handlers

import (
	"strings"
	"testing"
)

// 🔴 DEV 实测：11 个 Failed Pod 里 10 个在 node12。
// 人一眼看出是节点问题，改这一版之前工具逐个报 Pod，一个字都没提。
func TestNodeConcentrationSurfaced(t *testing.T) {
	cols := []string{"命名空间", "Pod", "原因", "节点", "启动时间"}
	rows := [][]any{}
	for _, r := range []struct{ ns, pod, reason, node string }{
		{"cicd-uat", "gitlab-runner-uat-x", "Error", "node12"},
		{"g32-dev", "go-replica-consumer-x", "Error", "node17"},
		{"g32-dev", "go-replica-producer-x", "ContainerStatusUnknown", "node12"},
		{"g32-dev", "luckysix-x", "ContainerStatusUnknown", "node12"},
		{"g32-dev", "niuniu-x", "ContainerStatusUnknown", "node12"},
		{"g50-dev", "g50-sic-bo-x", "ContainerStatusUnknown", "node12"},
		{"g50-uat", "g50-classic-x", "ContainerStatusUnknown", "node12"},
		{"g50-uat", "g50-hightie-x", "ContainerStatusUnknown", "node12"},
		{"g50-uat", "g50-lobby-x", "ContainerStatusUnknown", "node12"},
		{"g50-uat", "g50-sic-bo-x", "ContainerStatusUnknown", "node12"},
		{"metersphere2", "ms-ui-test-x", "PodInitializing", "node12"},
	} {
		rows = append(rows, []any{r.ns, r.pod, r.reason, r.node, "2026-01-01"})
	}
	ps := concentrations(cols, rows)
	var nodeHint string
	for _, p := range ps {
		if p["column"] == "节点" {
			nodeHint = p["hint"].(string)
		}
	}
	if nodeHint == "" {
		t.Fatal("没识别出节点集中")
	}
	for _, want := range []string{"node12", "11 条里有 10 条", "先查它"} {
		if !strings.Contains(nodeHint, want) {
			t.Errorf("提示缺 %q: %s", want, nodeHint)
		}
	}
	// 推断必须说出来，不能只报统计数字
	if !strings.Contains(nodeHint, "互不相干") {
		t.Error("只报了统计，没说出「这不是 N 个独立故障」这个推断")
	}
}

// ⚠️ 分散的数据绝不能报"发现规律"。
// 一个总在喊发现规律的工具，和不喊的一样没人看。
func TestScatteredNoPattern(t *testing.T) {
	cols := []string{"命名空间", "Pod", "原因", "节点", "启动时间"}
	rows := [][]any{}
	reasons := []string{"Error", "OOMKilled", "Evicted", "Error", "OOMKilled", "Evicted"}
	for i, n := range []string{"node1", "node2", "node3", "node4", "node5", "node6"} {
		rows = append(rows, []any{"ns-" + n, "pod-" + n, reasons[i], n, "t"})
	}
	if ps := concentrations(cols, rows); len(ps) > 0 {
		t.Fatalf("分散的数据报出了规律: %+v", ps)
	}
}

// 🔴 「原因」列全同，往往正是这个体检项的筛选条件本身
// （pod_failed 全是 Error、pod_high_restart 全是 CrashLoopBackOff）——
// 报出来是同义反复，不是发现。
func TestTautologicalReasonNotReported(t *testing.T) {
	cols := []string{"命名空间", "Pod", "原因", "节点", "启动时间"}
	rows := [][]any{}
	for i, n := range []string{"node1", "node2", "node3", "node4", "node5", "node6"} {
		_ = i
		rows = append(rows, []any{"ns-" + n, "pod-" + n, "Error", n, "t"})
	}
	for _, p := range concentrations(cols, rows) {
		if p["column"] == "原因" {
			t.Fatalf("把筛选条件本身报成了规律: %v", p["hint"])
		}
	}
}

// ⚠️ 反过来：节点全同必须报 —— 没有机制强迫这些 Pod 挤在一个节点上。
func TestAllSameNodeStillReported(t *testing.T) {
	cols := []string{"命名空间", "Pod", "原因", "节点", "启动时间"}
	rows := [][]any{}
	for i, ns := range []string{"a", "b", "c", "d", "e"} {
		_ = i
		rows = append(rows, []any{ns, "pod-" + ns, "Error", "node9", "t"})
	}
	var got string
	for _, p := range concentrations(cols, rows) {
		if p["column"] == "节点" {
			got = p["hint"].(string)
		}
	}
	if got == "" {
		t.Fatal("5 个 Pod 全在同一个节点，却没报出来")
	}
}

// ⚠️ 样本太少时「集中」是巧合，不该报。
func TestTooFewRows(t *testing.T) {
	cols := []string{"命名空间", "Pod", "原因", "节点", "启动时间"}
	rows := [][]any{
		{"a", "p1", "Error", "node1", "t"},
		{"a", "p2", "Error", "node1", "t"},
		{"a", "p3", "Error", "node1", "t"},
	}
	if ps := concentrations(cols, rows); len(ps) > 0 {
		t.Fatalf("3 条就报规律了: %+v", ps)
	}
}

// 没有语义列（如同步状态表）时不该乱报。
func TestNoInterestingColumns(t *testing.T) {
	cols := []string{"资源类型", "最后同步", "错误", "条数"}
	rows := [][]any{{"pods", "t", "e", 1}, {"nodes", "t", "e", 2}, {"svc", "t", "e", 3}, {"pvc", "t", "e", 4}}
	if ps := concentrations(cols, rows); len(ps) > 0 {
		t.Fatalf("在无语义列上报了规律: %+v", ps)
	}
}

// 🔴 DEV 全量扫完的真实分布：22 个未判出里 15 个挤在 metersphere2。
// 逐条读是 15 个互不相干的谜；放在一起看是**一个**问题。
func TestUnresolvedConcentratesByNamespace(t *testing.T) {
	rows := [][]any{}
	for i := 0; i < 15; i++ {
		rows = append(rows, []any{"metersphere2", "ms-pod-" + string(rune('a'+i)),
			"镜像拉不下来（ImagePullBackOff），但**具体原因判不出来**：带原因的那条事件已过期，只剩重试记录"})
	}
	for _, r := range [][3]string{
		{"g50-dev", "g50-plaza-x", "容器启动后即崩溃（CrashLoopBackOff）"},
		{"g50-dev", "g50-vip-cbac-x", "容器异常退出（退出码 2）"},
		{"g35-dev", "g35-order-x", "容器异常退出（退出码 255）"},
		{"default", "busybox-x", "未识别到已知故障模式"},
		{"g32-dev", "go-replica-x", "容器异常退出（退出码 2）"},
		{"g32-dev", "maxwin24d-x", "镜像拉不下来，事件已过期"},
		{"metersphere2", "ms-extra", "未识别到已知故障模式"},
	} {
		rows = append(rows, []any{r[0], r[1], r[2]})
	}

	var nsHint string
	for _, p := range concentrations([]string{"命名空间", "Pod", "原因"}, rows) {
		if p["column"] == "命名空间" {
			nsHint = p["hint"].(string)
		}
	}
	if nsHint == "" {
		t.Fatal("22 个未判出里 16 个在 metersphere2，却没识别出命名空间集中")
	}
	if !strings.Contains(nsHint, "metersphere2") || !strings.Contains(nsHint, "别一个个查") {
		t.Errorf("提示不到位: %s", nsHint)
	}
}
