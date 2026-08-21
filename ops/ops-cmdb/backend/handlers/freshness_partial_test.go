package handlers

import (
	"strings"
	"testing"
)

// summarizeFreshness 守的是 OPSCMDB-031 P0-11 里最危险的那一半。
//
// P0-11 的表面问题是 Gateway API 没采到；真正让它躲过所有检查的，
// 是**采集自称成功**：
//
//	gateways:   ok=true, count=6   ← 6 个全是 Istio 的，Gateway API 那套一个没采到
//	httproutes: ok=true, count=0   ← 看起来就像"这个集群没有 HTTPRoute"
//
// 所有指标都正常，于是新鲜度汇总给出「全部资源采集正常且在新鲜期内，
// **数据可直接采信**」—— 而那句断言是错的。
//
// 一个体检的输出是断言，而错的断言比没有断言更糟。
func TestSummarizeFreshnessDoesNotTrustPartialCollection(t *testing.T) {
	// 这一组模拟 P0-11 的真实状态：全部 fresh、ok，但 gateways 有跳过说明
	rs := []syncResourceState{
		{Resource: "nodes", Freshness: "fresh", OK: true, Count: 16},
		{Resource: "pods", Freshness: "fresh", OK: true, Count: 1719},
		{
			Resource: "gateways", Freshness: "fresh", OK: true, Count: 6,
			SkipNote: "Gateway API 的 CRD 未安装（v1 与 v1beta1 都试过），这一类没采到",
		},
	}
	overall, trust, advice := summarizeFreshness(rs, 360)
	if trust {
		t.Error("有整类资源没采到，却说数据可直接采信 —— 这正是 P0-11 躲过所有检查的原因")
	}
	if overall != "partial" {
		t.Errorf("overall=%q，期望 partial（既不是 fresh 也不是 failed）", overall)
	}
	if !strings.Contains(advice, "gateways") {
		t.Errorf("建议里没点出是哪一类：%q", advice)
	}
	// ⚠️ 不能说成"采集失败" —— 那会让人去查一个不存在的报错
	if strings.Contains(advice, "采集失败") {
		t.Errorf("把「部分跳过」说成了「采集失败」，会把人引向错误方向：%q", advice)
	}
}

// 真失败仍然优先：failed 比 partial 严重，不能被后者盖住。
func TestSummarizeFreshnessFailedBeatsPartial(t *testing.T) {
	rs := []syncResourceState{
		{Resource: "nodes", Freshness: "failed", OK: false, Err: "connection refused"},
		{Resource: "gateways", Freshness: "fresh", OK: true, SkipNote: "CRD 未安装"},
	}
	overall, trust, _ := summarizeFreshness(rs, 360)
	if overall != "failed" {
		t.Errorf("overall=%q，期望 failed（真失败优先于部分跳过）", overall)
	}
	if trust {
		t.Error("采集失败时不该可信")
	}
}

// 全部正常时仍然要给出可信断言 —— 否则这个判定就没用了。
func TestSummarizeFreshnessAllGood(t *testing.T) {
	rs := []syncResourceState{
		{Resource: "nodes", Freshness: "fresh", OK: true, Count: 16},
		{Resource: "pods", Freshness: "fresh", OK: true, Count: 1719},
	}
	overall, trust, _ := summarizeFreshness(rs, 360)
	if overall != "fresh" || !trust {
		t.Errorf("overall=%q trust=%v，期望 fresh/true", overall, trust)
	}
}

// 从没采过：不能说"没问题"。
func TestSummarizeFreshnessNever(t *testing.T) {
	overall, trust, advice := summarizeFreshness(nil, 360)
	if overall != "never" || trust {
		t.Errorf("overall=%q trust=%v，期望 never/false", overall, trust)
	}
	if advice == "" {
		t.Error("从没采过必须给出说明")
	}
}
