package handlers

import "testing"

// ★ 分层顺序决定图的可读性：证书 → 域名 → 入口 → 主机。
//
// 未知类型必须落到最后一列而不是被丢弃 —— 丢弃会让链路断掉，
// 而用户看到断链会以为数据错了。
func TestLayerOf(t *testing.T) {
	cases := map[string]int{
		"certificate": 0,
		"domain":      1,
		ciTypeLB:      2,
		"host":        3,
		"某种新类型":       4,
	}
	for typ, want := range cases {
		if got := layerOf(typ); got != want {
			t.Errorf("layerOf(%q) = %d, want %d", typ, got, want)
		}
	}
	// 依赖方向必须严格递增，否则分层布局会画出回头线
	if !(layerOf("certificate") < layerOf("domain") &&
		layerOf("domain") < layerOf(ciTypeLB) &&
		layerOf(ciTypeLB) < layerOf("host")) {
		t.Error("分层顺序不是严格递增：证书 → 域名 → 入口 → 主机")
	}
}

// ★ 池节点的标签要说人话。
//
// 直接显示原始类型名（loadbalancer）在中文界面里很突兀，
// 而且用户在左侧列表看到的是「负载均衡」，图上却叫另一个名字。
func TestPoolLabel(t *testing.T) {
	cases := map[string]string{
		"host":        "主机组",
		ciTypeLB:      "负载均衡组",
		"domain":      "域名组",
		"certificate": "证书组",
	}
	for typ, want := range cases {
		if got := poolLabel(typ); got != want {
			t.Errorf("poolLabel(%q) = %q, want %q", typ, got, want)
		}
	}
	// 未知类型不能返回空串：图上会出现一个没有名字的方块
	if poolLabel("weird") == "" {
		t.Error("未知类型的池标签为空 —— 图上会出现无名节点")
	}
}
