package handlers

import "testing"

func TestParseBackendRef(t *testing.T) {
	cases := []struct {
		ref, defaultNS, wantName, wantNS string
	}{
		{"user-client-backend-svc.g32-user.svc.cluster.local", "other", "user-client-backend-svc", "g32-user"},
		{"nacos.devops", "other", "nacos", "devops"},
		{"local-svc", "g32-game", "local-svc", "g32-game"}, // 无点号时归属调用方所在 ns
	}
	for _, c := range cases {
		name, ns := parseBackendRef(c.ref, c.defaultNS)
		if name != c.wantName || ns != c.wantNS {
			t.Errorf("parseBackendRef(%q,%q) = (%q,%q), 期望 (%q,%q)",
				c.ref, c.defaultNS, name, ns, c.wantName, c.wantNS)
		}
	}
}

func TestIsSystemNamespace(t *testing.T) {
	// 这些天生可能为空，报成孤儿只会制造噪声
	for _, ns := range []string{"kube-system", "kube-public", "kube-node-lease", "default",
		"gke-managed-system", "gke-managed-volumepopulator"} {
		if !isSystemNamespace(ns) {
			t.Errorf("%q 应被视为系统命名空间", ns)
		}
	}
	// 这些空了就是真该清理的
	for _, ns := range []string{"falco", "kyverno", "elkeid", "g32-uat", "ls-uat"} {
		if isSystemNamespace(ns) {
			t.Errorf("%q 不该被当成系统命名空间而跳过", ns)
		}
	}
}

func TestSuggestValue(t *testing.T) {
	// 一律向上取整到 step 的倍数：request 宁可略高也不能略低——低了会被驱逐，
	// 而小值上多给的那点绝对量（如 10.5m→20m 只多 0.01 核）可以忽略。
	cases := []struct {
		used float64
		step int
		want int
	}{
		{100, 10, 150}, // 100 × 1.5 = 150，正好是整数倍
		{7, 10, 20},    // 7 × 1.5 = 10.5 → 向上取到 20
		{0, 10, 10},    // 实测为 0 也不能建议 0
		{200, 64, 320}, // 200 × 1.5 = 300 → 向上取到 64 的整数倍
		{1000, 64, 1536},
	}
	for _, c := range cases {
		if got := suggestValue(c.used, c.step); got != c.want {
			t.Errorf("suggestValue(%v,%d) = %d, 期望 %d", c.used, c.step, got, c.want)
		}
	}
	// 建议值绝不能低于实测用量，否则一上线就被驱逐
	for _, used := range []float64{5, 50, 500, 5000} {
		if got := suggestValue(used, 10); float64(got) < used {
			t.Errorf("suggestValue(%v) = %d，低于实测用量，会导致 Pod 被驱逐", used, got)
		}
	}
}

func TestPct(t *testing.T) {
	if got := pct(10, 100); got != 10 {
		t.Errorf("pct(10,100) = %v, 期望 10", got)
	}
	if got := pct(1, 0); got != 0 {
		t.Errorf("除数为 0 时应返回 0 而非 NaN/panic，实际 %v", got)
	}
}

func TestHealthRank(t *testing.T) {
	if healthRank("critical") >= healthRank("warning") || healthRank("warning") >= healthRank("info") {
		t.Error("严重度排序必须是 critical < warning < info（数字小的排前面）")
	}
}
