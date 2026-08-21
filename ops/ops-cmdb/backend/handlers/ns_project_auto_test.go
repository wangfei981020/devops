package handlers

import "testing"

// 用生产 app 集群的真实命名空间列表（46 个）验匹配规则。
// 归错的代价是成本报表把开销算到别的项目头上——比不归还糟，
// 因为"未分配"至少是诚实的。
func TestMatchNsProject(t *testing.T) {
	projects := []string{"APP", "demo-ns", "pa-re"} // 长的在前由调用方保证，这里手工排

	cases := []struct {
		ns, wantProject, wantRule string
	}{
		// 前缀命中：生产上 app-* 一大片
		{"app-game", "APP", "prefix"},
		{"app-wallet", "APP", "prefix"},
		{"app-bet-settle", "APP", "prefix"},
		{"app-tidb", "APP", "prefix"},
		// 精确同名
		{"demo-ns", "demo-ns", "exact"},
		{"pa-re", "pa-re", "exact"},
		// 平台组件：**不归业务项目**，硬塞会污染成本口径
		{"kube-system", "", "platform"},
		{"istio-system", "", "platform"},
		{"cattle-fleet-system", "", "platform"},
		{"gke-managed-system", "", "platform"},
		{"monitoring", "", "platform"},
		{"argocd", "", "platform"},
		{"devops", "", "platform"},
		// 匹配不上：留给人工
		{"doris", "", "none"},
		{"redis", "", "none"},
		{"rocketmq", "", "none"},
		{"xxl-job-admin", "", "none"},
		{"ls-prod", "", "none"},
		{"k8sinsight", "", "none"},
	}
	for _, c := range cases {
		got, rule, reason := matchNsProject(c.ns, projects)
		if got != c.wantProject || rule != c.wantRule {
			t.Errorf("%s → (%q,%s)，期望 (%q,%s)", c.ns, got, rule, c.wantProject, c.wantRule)
		}
		if reason == "" {
			t.Errorf("%s 没给理由——预览时人要靠它判断这条对不对", c.ns)
		}
	}
}

// 前缀必须以分隔符结尾，否则项目 `app` 会误吞 `appx-foo` 这种毫不相干的命名空间。
func TestMatchNsProject_前缀要有分隔符(t *testing.T) {
	projects := []string{"app"}
	if p, rule, _ := matchNsProject("appx-foo", projects); p != "" {
		t.Errorf("appx-foo 不该匹配项目 app（得到 %q/%s）——少一个分隔符就会把别人的成本算进来", p, rule)
	}
	if p, _, _ := matchNsProject("app-foo", projects); p != "app" {
		t.Errorf("app-foo 应匹配 app，实际 %q", p)
	}
}

// 同时存在 `app` 和 `app-bi` 两个项目时，`app-bi-etl` 该归更具体的那个
func TestMatchNsProject_最长前缀优先(t *testing.T) {
	// 调用方按长度倒序传入
	projects := []string{"app-bi", "app"}
	if p, _, _ := matchNsProject("app-bi-etl", projects); p != "app-bi" {
		t.Errorf("应归到更具体的 app-bi，实际 %q", p)
	}
	if p, _, _ := matchNsProject("app-game", projects); p != "app" {
		t.Errorf("应归到 app，实际 %q", p)
	}
}

func TestIsPlatformNamespace(t *testing.T) {
	platform := []string{"kube-system", "kube-public", "istio-system", "monitoring",
		"cattle-system", "gke-managed-volumepopulator", "argocd", "falco", "default"}
	for _, ns := range platform {
		if !isPlatformNamespace(ns) {
			t.Errorf("%q 是平台组件", ns)
		}
	}
	// 业务命名空间不能被当成平台——那样它的成本会一直没人认领
	biz := []string{"app-game", "demo-ns", "pa-re", "doris", "ls-prod", "k8sinsight"}
	for _, ns := range biz {
		if isPlatformNamespace(ns) {
			t.Errorf("%q 是业务命名空间，误判成平台会让它的成本一直无人认领", ns)
		}
	}
}
