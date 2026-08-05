package handlers

import "testing"

// 排期锚定的兜底：GKE 只在"升级即将发生"时才填 minor_target_version，
// 未入通道（UNSPECIFIED）的集群往往一直是空的。
// 原来空就 continue，导致 4 个集群一条都锚不上、28 行 anchored_clusters 全 null，
// 升级窗口从真实的 2026-09（STABLE 列）退化成「第 4 季度内」——
// 而这个页面存在的唯一目的就是提前看见升级窗口，报晚一个季度等于没有预警。
func TestNextMinor_锚定兜底(t *testing.T) {
	cases := map[string]string{
		"1.34.8-gke.1278000": "1.35",
		"v1.33.5":            "1.34",
		"1.29.0":             "1.30",
		"1.9.7":              "1.10", // 整数递增，不是字符串拼接
		"":                   "",     // 连当前版本都没有，确实无从推断
		"garbage":            "",
	}
	for in, want := range cases {
		if got := nextMinor(in); got != want {
			t.Errorf("nextMinor(%q) = %q，期望 %q", in, got, want)
		}
	}
}
