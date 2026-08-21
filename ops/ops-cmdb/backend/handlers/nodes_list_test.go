package handlers

import (
	"testing"
	"time"

	"ops-cmdb-backend/internal/httpx"
)

func ndFixtures() []nodeOut {
	return []nodeOut{
		{ClusterID: 1, ClusterName: "g32", Name: "n-ready", ReadyStatus: "Ready",
			LastHeartbeat: time.Now().Format(time.RFC3339)},
		{ClusterID: 1, ClusterName: "g32", Name: "n-notready", ReadyStatus: "NotReady",
			LastHeartbeat: time.Now().Format(time.RFC3339)},
		// ★ 库里写着 Ready，但心跳两小时没来了
		{ClusterID: 2, ClusterName: "uat", Name: "n-silent", ReadyStatus: "Ready",
			HeartbeatStale: true, LastHeartbeat: time.Now().Add(-2 * time.Hour).Format(time.RFC3339)},
	}
}

func ndQuery(over func(*httpx.PageQuery)) httpx.PageQuery {
	p := httpx.PageQuery{Page: 1, Size: 20, Filters: map[string]string{}}
	if over != nil {
		over(&p)
	}
	return p
}

// ★ 这条是这个文件存在的主要理由
func TestNodeStatusStaleIsNotReady(t *testing.T) {
	// kubelet 停止上报时 ready_status 会**停在最后一次的值**上。
	// 把它算成 ready，就是在替一个联系不上的节点担保 ——
	// 而运维会对着一个"健康"的节点排查为什么 Pod 起不来。
	stale := nodeOut{ReadyStatus: "Ready", HeartbeatStale: true}
	if got := nodeStatusKey(stale); got != "stale" {
		t.Errorf("心跳过期的节点状态 = %q，期望 stale（不能算 ready）", got)
	}
	if statusSeverity(stale) >= statusSeverity(nodeOut{ReadyStatus: "NotReady"}) {
		t.Error("心跳过期应比 NotReady 更靠前：我们对它一无所知")
	}
}

func TestNodePageStatusFilter(t *testing.T) {
	items, total, _ := nodePage(ndFixtures(), ndQuery(func(p *httpx.PageQuery) {
		p.Filters["status"] = "stale"
	}))
	if total != 1 || items[0].Name != "n-silent" {
		t.Errorf("stale 筛选应只剩 n-silent，得到 total=%d %+v", total, items)
	}
	// 库里写着 Ready 的那台不能出现在 ready 里
	items, _, _ = nodePage(ndFixtures(), ndQuery(func(p *httpx.PageQuery) {
		p.Filters["status"] = "ready"
	}))
	for _, n := range items {
		if n.Name == "n-silent" {
			t.Error("心跳过期的节点不该出现在 ready 筛选里")
		}
	}
}

func TestNodePageFacetsExcludeOwnDimension(t *testing.T) {
	// 选了 g32 之后，集群下拉里 uat 仍要显示真实条数
	_, _, f := nodePage(ndFixtures(), ndQuery(func(p *httpx.PageQuery) {
		p.Filters["cluster"] = "g32"
	}))
	if f["cluster"]["uat"] != 1 {
		t.Errorf("uat 的集群分面 = %d，期望 1", f["cluster"]["uat"])
	}
	// 而状态分面要跟着集群筛选走（它是另一个维度）
	if f["status"]["all"] != 2 {
		t.Errorf("筛了 g32 时状态分面 all = %d，期望 2", f["status"]["all"])
	}
}

func TestNodePageSortBySeverity(t *testing.T) {
	items, _, _ := nodePage(ndFixtures(), ndQuery(func(p *httpx.PageQuery) { p.SortBy = "status" }))
	if items[0].Name != "n-silent" {
		t.Errorf("按状态排序时失联节点应在最前，得到 %s", items[0].Name)
	}
}

func TestNodePagePodsNilNotZero(t *testing.T) {
	// 采不到 Pod 数时是 nil，排序当 -1 —— 与"这个节点上真的没有 Pod"分开
	if derefI64(nil) != -1 {
		t.Error("未采集的 Pod 数不能当 0 排序")
	}
}

// 「节点上没有 Pod」与「这个集群没采过 Pod」必须分开。
//
// 两种情况在 k8s_pods 里都是"这个节点没有行"，只能靠集群层面判：
// 集群里有别的 Pod = 采集跑过了 → 这个节点是真的空；
// 集群一行都没有 = 没采过。
//
// 分不开的后果是主机抽屉说"没有 Pod"、节点列表说"未接入"，
// 同一份数据两个答案。
func TestNodePodsEmptyVsNotCollected(t *testing.T) {
	// 这里只锁语义边界（nil 与 0 的区别），实际归位逻辑在 fillPodCounts 里，
	// 它依赖数据库，由本地实测覆盖。
	empty := nodeOut{PodsCollected: new(int64)} // 0：确认是空的
	notCollected := nodeOut{}                   // nil：没采过
	if derefI64(empty.PodsCollected) != 0 {
		t.Error("确认为空的节点，Pod 数必须是 0")
	}
	if notCollected.PodsCollected != nil {
		t.Error("没采过的节点，Pod 数必须是 nil，不能兜底成 0")
	}
	if derefI64(notCollected.PodsCollected) == derefI64(empty.PodsCollected) {
		t.Error("排序时两者必须可区分")
	}
}
