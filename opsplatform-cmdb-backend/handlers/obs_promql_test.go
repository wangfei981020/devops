package handlers

import "testing"

// 单集群数据源(selector 为空)必须生成和改造前一字不差的 PromQL。
// DEV 集群那套独立 Prometheus 和待下线的老 UAT 源都没有 cluster 标签，
// 一旦这里多出条件，它们会一条数据都查不到。
func TestBuildPromQL_NoSelectorKeepsLegacyQuery(t *testing.T) {
	cases := []struct{ target, ns, name, metric, want string }{
		{"pod", "g32-uat", "api-0", "mem",
			`sum(container_memory_working_set_bytes{namespace="g32-uat",pod="api-0",container!=""})`},
		{"pod", "g32-uat", "api-0", "cpu",
			`sum(rate(container_cpu_usage_seconds_total{namespace="g32-uat",pod="api-0",container!=""}[5m]))`},
		{"workload", "g32-uat", "api", "mem",
			`sum by(pod)(container_memory_working_set_bytes{namespace="g32-uat",pod=~"api-.*",container!=""})`},
		{"workload", "g32-uat", "api", "cpu",
			`sum by(pod)(rate(container_cpu_usage_seconds_total{namespace="g32-uat",pod=~"api-.*",container!=""}[5m]))`},
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
	sel := `cluster="uat-k8s-cluster-01"`
	cases := []struct{ target, ns, name, metric, want string }{
		{"pod", "cesar", "busybox1", "mem",
			`sum(container_memory_working_set_bytes{cluster="uat-k8s-cluster-01",namespace="cesar",pod="busybox1",container!=""})`},
		{"workload", "cesar", "api", "cpu",
			`sum by(pod)(rate(container_cpu_usage_seconds_total{cluster="uat-k8s-cluster-01",namespace="cesar",pod=~"api-.*",container!=""}[5m]))`},
		{"node", "", "gke-node-1", "cpu",
			`sum(rate(node_cpu_seconds_total{cluster="uat-k8s-cluster-01",mode!="idle",node="gke-node-1"}[5m]))`},
		{"node", "", "gke-node-1", "mem",
			`sum(node_memory_MemTotal_bytes{cluster="uat-k8s-cluster-01",node="gke-node-1"}) - sum(node_memory_MemAvailable_bytes{cluster="uat-k8s-cluster-01",node="gke-node-1"})`},
	}
	for _, c := range cases {
		if got := buildPromQL(c.target, c.ns, c.name, c.metric, sel, ""); got != c.want {
			t.Errorf("target=%s metric=%s\n got: %s\nwant: %s", c.target, c.metric, got, c.want)
		}
	}
}

// 主机不属于任何 K8s 集群(通用源里在 cluster="ecs" 下)，套集群条件会一条都查不到。
func TestBuildPromQL_HostNeverGetsClusterSelector(t *testing.T) {
	sel := `cluster="uat-k8s-cluster-01"`
	got := buildPromQL("host", "", "10.170.48.28", "cpu", sel, `env="uat",project="g32"`)
	want := `sum(rate(node_cpu_seconds_total{env="uat",project="g32",mode!="idle",instance=~"10.170.48.28.*"}[5m]))`
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
		{"uat", "g32", "dba", `env="uat",project="g32",team="dba"`},
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
