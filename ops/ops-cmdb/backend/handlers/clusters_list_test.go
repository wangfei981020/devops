package handlers

import (
	"testing"

	"ops-cmdb-backend/internal/httpx"
)

func clInt64(v int64) *int64 { return &v }

func clFixtures() []clusterOut {
	return []clusterOut{
		// 采到了，一切正常
		{ID: 1, Name: "g32-prod", Environment: "PROD", Ingested: true,
			Nodes: clInt64(46), NodesReady: clInt64(46), Pods: clInt64(900), PodsBad: clInt64(3)},
		// 采到了，但里面是**空的**（刚建、还没部署东西）
		{ID: 2, Name: "uat-new", Environment: "UAT", Ingested: true,
			Nodes: clInt64(0), NodesReady: clInt64(0), Pods: clInt64(0), PodsBad: clInt64(0)},
		// 纳管了但**没采到**：计数是 nil，不是 0
		{ID: 3, Name: "infra-02", Environment: "PROD"},
		{ID: 4, Name: "dev-k3s", Environment: "DEV", Ingested: true,
			Nodes: clInt64(3), NodesReady: clInt64(2), Pods: clInt64(40), PodsBad: clInt64(0)},
	}
}

func clQuery(over func(*httpx.PageQuery)) httpx.PageQuery {
	p := httpx.PageQuery{Page: 1, Size: 20, Filters: map[string]string{}}
	if over != nil {
		over(&p)
	}
	return p
}

func TestClusterPageFacetsIgnoreOwnDimension(t *testing.T) {
	// 选了 PROD 之后，下拉里 UAT 仍要显示真实条数 ——
	// 按筛选后统计的话它会变成 0，用户看不出"切过去还有多少"
	_, _, f := clusterPage(clFixtures(), clQuery(func(p *httpx.PageQuery) { p.Filters["env"] = "PROD" }))
	if f["env"]["UAT"] != 1 {
		t.Errorf("筛了 PROD 时 UAT 的分面计数 = %d，期望 1", f["env"]["UAT"])
	}
	if f["env"]["all"] != 4 {
		t.Errorf("all 分面 = %d，期望 4", f["env"]["all"])
	}
}

func TestClusterPageFilter(t *testing.T) {
	items, total, _ := clusterPage(clFixtures(), clQuery(func(p *httpx.PageQuery) { p.Filters["env"] = "PROD" }))
	if total != 2 || len(items) != 2 {
		t.Fatalf("PROD 应有 2 个，得到 total=%d len=%d", total, len(items))
	}
}

func TestClusterPageSearch(t *testing.T) {
	items, _, _ := clusterPage(clFixtures(), clQuery(func(p *httpx.PageQuery) { p.Keyword = "K3S" }))
	if len(items) != 1 || items[0].Name != "dev-k3s" {
		t.Errorf("搜索应大小写不敏感，得到 %+v", items)
	}
}

// ★ 这条是这个文件存在的主要理由
func TestClusterPageSortPutsNotIngestedApartFromEmpty(t *testing.T) {
	// 「没采到」和「0 个节点」必须能分开。
	// 把 nil 当 0 排序，一堆采集失败的集群会混在真正空着的集群里，
	// 而两者要做的事完全不同：一个去查连通性，一个什么都不用做。
	items, _, _ := clusterPage(clFixtures(), clQuery(func(p *httpx.PageQuery) { p.SortBy = "nodes" }))
	if items[0].Name != "infra-02" {
		t.Errorf("未采集的集群应排在最前（-1），得到 %s", items[0].Name)
	}
	if items[1].Name != "uat-new" {
		t.Errorf("空集群应排在未采集之后，得到 %s", items[1].Name)
	}
	if items[0].Nodes != nil {
		t.Error("未采集集群的节点数必须是 nil，不能兜底成 0")
	}
}

func TestClusterPagePagination(t *testing.T) {
	items, total, _ := clusterPage(clFixtures(), clQuery(func(p *httpx.PageQuery) { p.Size = 2; p.Page = 2 }))
	if total != 4 || len(items) != 2 {
		t.Errorf("第 2 页应有 2 条、总数 4，得到 len=%d total=%d", len(items), total)
	}
	// 越界页返回空列表，不退回最后一页 ——
	// 退回最后一页会让"下一页"按钮看起来永远可点
	items, _, _ = clusterPage(clFixtures(), clQuery(func(p *httpx.PageQuery) { p.Size = 2; p.Page = 99 }))
	if len(items) != 0 {
		t.Errorf("越界页应为空，得到 %d 条", len(items))
	}
}
