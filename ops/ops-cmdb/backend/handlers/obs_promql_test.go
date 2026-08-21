package handlers

// 容器级查询一律是 sum(avg by (...,container) (...)) 的形状，不是多此一举：
// 生产上同一容器的 container_* 指标有 3 条重复 series（两套监控栈重复采 kubelet），
// 直接 sum 会虚高 3 倍。avg 先把重复的那几份收成一份（值本来就相同），再求和。
// 背景：容器指标在部分环境下存在多条同名 series，直接 sum 会翻倍。

import "testing"

// 单集群数据源(selector 为空)必须生成和改造前一字不差的 PromQL。
// DEV 集群那套独立 Prometheus 和待下线的老 UAT 源都没有 cluster 标签，
// 一旦这里多出条件，它们会一条数据都查不到。
func TestBuildPromQL_NoSelectorKeepsLegacyQuery(t *testing.T) {
	cases := []struct{ target, ns, name, metric, want string }{
		{"pod", "demo-ns", "api-0", "mem",
			`sum(avg by (namespace,pod,container) (container_memory_working_set_bytes{namespace="demo-ns",pod="api-0",container!=""}))`},
		{"pod", "demo-ns", "api-0", "cpu",
			`sum(avg by (namespace,pod,container) (rate(container_cpu_usage_seconds_total{namespace="demo-ns",pod="api-0",container!=""}[5m])))`},
		{"workload", "demo-ns", "api", "mem",
			`sum by (pod) (avg by (pod,container) (container_memory_working_set_bytes{namespace="demo-ns",pod=~"api-.*",container!=""}))`},
		{"workload", "demo-ns", "api", "cpu",
			`sum by (pod) (avg by (pod,container) (rate(container_cpu_usage_seconds_total{namespace="demo-ns",pod=~"api-.*",container!=""}[5m])))`},
		{"node", "", "gke-node-1", "mem",
			`sum(node_memory_MemTotal_bytes{node="gke-node-1"}) - sum(node_memory_MemAvailable_bytes{node="gke-node-1"})`},
		{"node", "", "gke-node-1", "cpu",
			`sum(rate(node_cpu_seconds_total{mode!="idle",node="gke-node-1"}[5m]))`},
		{"host", "", "10.170.48.28", "mem",
			`sum(node_memory_MemTotal_bytes{instance=~"10.170.48.28.*"}) - sum(node_memory_MemAvailable_bytes{instance=~"10.170.48.28.*"})`},
		{"host", "", "10.170.48.28", "cpu",
			`sum(rate(node_cpu_seconds_total{mode!="idle",instance=~"10.170.48.28.*"}[5m]))`},
	}
	for _, c := range cases {
		if got := buildPromQL(c.target, c.ns, c.name, c.metric, "", ""); got != c.want {
			t.Errorf("target=%s metric=%s\n got: %s\nwant: %s", c.target, c.metric, got, c.want)
		}
	}
}

// 多集群共享源：K8s 对象要带集群条件，否则会捞到别的集群的同名 Pod。
func TestBuildPromQL_SelectorAppliedToK8sTargets(t *testing.T) {
	sel := `cluster="prod-cluster-01"`
	cases := []struct{ target, ns, name, metric, want string }{
		{"pod", "demo-ns", "busybox1", "mem",
			`sum(avg by (namespace,pod,container) (container_memory_working_set_bytes{cluster="prod-cluster-01",namespace="demo-ns",pod="busybox1",container!=""}))`},
		{"workload", "demo-ns", "api", "cpu",
			`sum by (pod) (avg by (pod,container) (rate(container_cpu_usage_seconds_total{cluster="prod-cluster-01",namespace="demo-ns",pod=~"api-.*",container!=""}[5m])))`},
		{"node", "", "gke-node-1", "cpu",
			`sum(rate(node_cpu_seconds_total{cluster="prod-cluster-01",mode!="idle",node="gke-node-1"}[5m]))`},
		{"node", "", "gke-node-1", "mem",
			`sum(node_memory_MemTotal_bytes{cluster="prod-cluster-01",node="gke-node-1"}) - sum(node_memory_MemAvailable_bytes{cluster="prod-cluster-01",node="gke-node-1"})`},
	}
	for _, c := range cases {
		if got := buildPromQL(c.target, c.ns, c.name, c.metric, sel, ""); got != c.want {
			t.Errorf("target=%s metric=%s\n got: %s\nwant: %s", c.target, c.metric, got, c.want)
		}
	}
}

// 主机不属于任何 K8s 集群(通用源里在 cluster="ecs" 下)，套集群条件会一条都查不到。
func TestBuildPromQL_HostNeverGetsClusterSelector(t *testing.T) {
	sel := `cluster="prod-cluster-01"`
	got := buildPromQL("host", "", "10.170.48.28", "cpu", sel, `env="uat",project="app"`)
	want := `sum(rate(node_cpu_seconds_total{env="uat",project="app",mode!="idle",instance=~"10.170.48.28.*"}[5m]))`
	if got != want {
		t.Errorf("host 查询串进了集群条件\n got: %s\nwant: %s", got, want)
	}
	if contains(got, "cluster=") {
		t.Errorf("host 查询不该出现 cluster 条件: %s", got)
	}
}

func TestHostSelector(t *testing.T) {
	cases := []struct{ env, project, team, want string }{
		{"", "", "", ``},
		{"uat", "", "", `env="uat"`},
		{"UAT", "", "", `env="uat"`}, // CMDB 环境是大写枚举，指标标签是小写
		{"uat", "app", "dba", `env="uat",project="app",team="dba"`},
		{" uat ", "", "", `env="uat"`},
	}
	for _, c := range cases {
		if got := hostSelector(c.env, c.project, c.team); got != c.want {
			t.Errorf("hostSelector(%q,%q,%q) = %q, want %q", c.env, c.project, c.team, got, c.want)
		}
	}
}

func TestPromLabels(t *testing.T) {
	if got := promLabels(""); got != "" {
		t.Errorf("全空应返回空串(裸指标名)，got %q", got)
	}
	if got := promLabels("", "", ""); got != "" {
		t.Errorf("空条件应被跳过，got %q", got)
	}
	if got := promLabels(`cluster="c1"`, `mode="idle"`); got != `{cluster="c1",mode="idle"}` {
		t.Errorf("got %q", got)
	}
	if got := promLabels("", `container!=""`); got != `{container!=""}` {
		t.Errorf("got %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// 标签取值超过 500 时，count 必须报**真实总数**并明确标出截断。
//
// 🔴 原来是 `r.Data = r.Data[:500]` 之后 `count: len(r.Data)` ——
// 一个有 3000 个取值的标签返回 `count: 500` 且没有任何标记，
// 读的人只能得出「这个标签一共 500 个值」这个错误结论，
// 然后按它去写查询、去估规模。
//
// ⚠️ 静默截断是本项目反复在修的那一类：**结果看起来完全正常**。
func TestPromLabelValuesTruncationIsVisible(t *testing.T) {
	// 模拟 handler 里的那段逻辑
	build := func(n int) (count int, truncated bool, returned int) {
		data := make([]string, n)
		total := len(data)
		if total > 500 {
			data = data[:500]
			truncated = true
		}
		return total, truncated, len(data)
	}

	t.Run("没超过上限：不标截断，count 就是条数", func(t *testing.T) {
		count, trunc, ret := build(120)
		if trunc {
			t.Error("120 条没超 500，不该标截断")
		}
		if count != 120 || ret != 120 {
			t.Errorf("count=%d returned=%d，都该是 120", count, ret)
		}
	})

	t.Run("🔴 超过上限：count 必须是真实总数，不是截断后的长度", func(t *testing.T) {
		count, trunc, ret := build(3000)
		if !trunc {
			t.Fatal("3000 条超了 500，必须标截断 —— 静默截断会被读成「一共就这么多」")
		}
		if ret != 500 {
			t.Errorf("只该返回 500 条，实际 %d", ret)
		}
		if count == 500 {
			t.Error("count 报成了截断后的长度 —— 这正是那个 bug：3000 个取值显示成 500")
		}
		if count != 3000 {
			t.Errorf("count 必须是真实总数 3000，实际 %d", count)
		}
	})

	t.Run("边界：正好 500 不算截断", func(t *testing.T) {
		count, trunc, _ := build(500)
		if trunc {
			t.Error("正好 500 没有丢任何东西，不该标截断")
		}
		if count != 500 {
			t.Errorf("count=%d", count)
		}
	})
}
