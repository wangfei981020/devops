package handlers

import (
	"strings"
	"testing"
)

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

// 锚定必须**说清楚自己是不是推断出来的**。
//
//	排期表原来只给集群名，看上去和"确定锚定"没有区别：
//	「这 4 个集群锚定在 1.36 STABLE」像是既定事实，而同一页面的升级看板
//	却说「未入通道、日期是推断」——两个 tab 互相打架，照着排期排会排错。
//	release_channel 为空是 GKE 返回的**真值**（集群确实没入通道），
//	采不下来也补不上，只能如实标成假定。
func TestAnchorNote_假定必须被标注(t *testing.T) {
	if anchorNote(false, false) != "" {
		t.Error("两项都确定时不该有说明文字，否则每条都挂个提示等于没提示")
	}
	if n := anchorNote(true, false); n == "" || !strings.Contains(n, "STABLE") {
		t.Errorf("未入通道必须说明按 STABLE 套用，实际 %q", n)
	}
	if n := anchorNote(false, true); n == "" {
		t.Error("目标版本是推出来的也必须说明")
	}
	if n := anchorNote(true, true); n == "" || !strings.Contains(n, "STABLE") {
		t.Errorf("两项都是假定时说明不能丢，实际 %q", n)
	}
}
