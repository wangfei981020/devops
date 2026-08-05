package handlers

import "testing"

// 生产上 high=16 全是 filebeat 的 16 个副本 = **1 个问题**，promtail 一家占 35 行，
// 整体虚高 4.8 倍（523 → 112）。这个数字是拿来排优先级的，虚高就会把注意力
// 引到错的地方。
func TestMergeByWorkload(t *testing.T) {
	mk := func(ns, pod, wl string, risks ...string) secFinding {
		return secFinding{Namespace: ns, Pod: pod, Workload: wl, Risks: risks, Severity: "high"}
	}

	in := []secFinding{
		mk("logging", "filebeat-aaa", "filebeat", "hostPath /var/lib/docker"),
		mk("logging", "filebeat-bbb", "filebeat", "hostPath /var/lib/docker"),
		mk("logging", "filebeat-ccc", "filebeat", "hostPath /var/lib/docker"),
		mk("logging", "promtail-xxx", "promtail", "hostNetwork"),
		mk("logging", "promtail-yyy", "promtail", "hostNetwork"),
		mk("app", "one-off-pod", "", "privileged"), // 裸 Pod：不该被合并
	}

	got := mergeByWorkload(in)
	if len(got) != 3 {
		t.Fatalf("应合并成 3 条（filebeat / promtail / 裸 Pod），实际 %d 条", len(got))
	}

	byWl := map[string]secFinding{}
	for _, f := range got {
		k := f.Workload
		if k == "" {
			k = "(bare)" + f.Pod
		}
		byWl[k] = f
	}
	if n := byWl["filebeat"].PodCount; n != 3 {
		t.Errorf("filebeat pod_count = %d，期望 3——影响面必须留着，否则'16条变1条'会让人以为问题缩小了", n)
	}
	if n := len(byWl["filebeat"].SamplePods); n != 3 {
		t.Errorf("样例 Pod 名要留着供排查，实际 %d 个", n)
	}
	if byWl["filebeat"].Pod != "" {
		t.Error("合并后这一行代表 workload 而不是某个 Pod，Pod 字段应留空避免误导")
	}
	if byWl["(bare)one-off-pod"].PodCount != 1 {
		t.Error("裸 Pod（无 workload）本来就是独立对象，不该被合并")
	}
}

// 同一 workload 但风险不同（比如滚动更新中新旧两版 spec 并存）必须分开算，
// 只按 ns+workload 合并会把其中一版的风险吞掉——那又是一种少报。
func TestMergeByWorkload_风险不同不能合并(t *testing.T) {
	in := []secFinding{
		{Namespace: "app", Pod: "web-old", Workload: "web", Risks: []string{"privileged"}},
		{Namespace: "app", Pod: "web-new", Workload: "web", Risks: []string{"hostNetwork"}},
	}
	if got := mergeByWorkload(in); len(got) != 2 {
		t.Fatalf("风险集合不同必须分开，实际合成了 %d 条", len(got))
	}
}

// 样例 Pod 名有上限，别让一个 200 副本的 DaemonSet 把响应撑爆
func TestMergeByWorkload_样例数有上限(t *testing.T) {
	var in []secFinding
	for i := 0; i < 50; i++ {
		in = append(in, secFinding{Namespace: "ns", Pod: string(rune('a'+i%26)) + "-pod",
			Workload: "ds", Risks: []string{"hostPID"}})
	}
	got := mergeByWorkload(in)
	if len(got) != 1 || got[0].PodCount != 50 {
		t.Fatalf("应合成 1 条且 pod_count=50，实际 %d 条 count=%d", len(got), got[0].PodCount)
	}
	if len(got[0].SamplePods) > 5 {
		t.Errorf("样例 Pod 应有上限，实际 %d 个", len(got[0].SamplePods))
	}
}
