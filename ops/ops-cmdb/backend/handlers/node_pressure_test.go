package handlers

import (
	"strings"
	"testing"
)

// 🔴 OPSCMDB-042：节点磁盘 100%、正在驱逐 Pod，ready_status 依然是 Ready。
// 节点列表把它显示成「正常」，按状态筛也永远筛不出来 ——
// 而「快撑不住但还没倒」恰恰是节点最危险的一段。
func TestNodeStatusSurfacesPressure(t *testing.T) {
	cases := []struct {
		name string
		n    nodeOut
		want string
	}{
		{"磁盘压力但仍 Ready", nodeOut{ReadyStatus: "Ready", Conditions: "DiskPressure"}, "pressure"},
		{"内存压力", nodeOut{ReadyStatus: "Ready", Conditions: "MemoryPressure"}, "pressure"},
		{"真的健康", nodeOut{ReadyStatus: "Ready", Conditions: ""}, "ready"},
		{"没就绪", nodeOut{ReadyStatus: "NotReady", Conditions: ""}, "notready"},
		// ⚠️ 失联优先于压力：采集都停了的话，压力位是冻结的旧值，不可信
		{"失联优先", nodeOut{ReadyStatus: "Ready", Conditions: "DiskPressure", HeartbeatStale: true}, "stale"},
		// 空白字符不能算有压力
		{"空白不算", nodeOut{ReadyStatus: "Ready", Conditions: "   "}, "ready"},
	}
	for _, c := range cases {
		if got := nodeStatusKey(c.n); got != c.want {
			t.Errorf("[%s] nodeStatusKey = %q, want %q", c.name, got, c.want)
		}
	}
}

// triage 必须能指路到节点压力，且要指向**事件时间线**而不是节点列表——
// 节点列表的 health 看不出压力，指过去等于让人白跑一趟。
func TestTriageGuidesNodePressureToEvents(t *testing.T) {
	g := triageGuide("nodesPressure")
	if g.what == "" {
		t.Fatal("nodesPressure 没有指引")
	}
	if !strings.Contains(g.tool, "list_events") {
		t.Errorf("没指向事件时间线: %s", g.tool)
	}
	if !strings.Contains(g.why, "别只看 list_nodes 的 health") {
		t.Error("没提醒「节点列表的 health 看不出压力」这个坑")
	}
	// 这类故障的表现常常不在节点上，必须说出来
	if !strings.Contains(g.why, "不在节点上") {
		t.Error("没说清「磁盘满表现为发布卡住/一堆 Pod 异常」")
	}
}
