package handlers

import (
	"testing"

	"ops-cmdb-backend/internal/httpx"
)

// 覆盖三种生命周期：运行中 / 已停机（实例还在，磁盘还计费）/ 已销毁。
// 已停机和已销毁绝不能合并 —— 前者还在花钱，后者不存在了。
func sampleHosts() []hostOut {
	return []hostOut{
		{CIID: 1, Name: "a-run", Project: "p1", Provider: "gcp", Status: "RUNNING", CostMonth: 300, VCPU: 8},
		{CIID: 2, Name: "b-run", Project: "p1", Provider: "gcp", Status: "RUNNING", CostMonth: 100, VCPU: 4},
		{CIID: 3, Name: "c-run", Project: "p2", Provider: "gcp", Status: "RUNNING", CostMonth: 200, VCPU: 2},
		{CIID: 4, Name: "d-stop", Project: "p2", Provider: "gcp", Status: "TERMINATED", CostMonth: 50, VCPU: 4},
		{CIID: 5, Name: "e-gone", Project: "p1", Provider: "gcp", Status: "RUNNING", Stale: true, CostMonth: 0, VCPU: 4},
	}
}

func q(page, size int, sort string, filters map[string]string) httpx.PageQuery {
	if filters == nil {
		filters = map[string]string{}
	}
	desc := false
	if len(sort) > 0 && sort[0] == '-' {
		desc, sort = true, sort[1:]
	}
	return httpx.PageQuery{Page: page, Size: size, SortBy: sort, SortDesc: desc, Filters: filters}
}

func TestHostBucketSeparatesStoppedFromDestroyed(t *testing.T) {
	// TERMINATED 在 GCP 语义里是"已停机、实例还在、磁盘还在计费"。
	// 把它和"已销毁"混成一个值，会让还在花钱的机器从成本核对里消失。
	if got := hostBucket(false, "TERMINATED"); got != "stopped" {
		t.Fatalf("TERMINATED 应为 stopped，得到 %s", got)
	}
	if got := hostBucket(true, "RUNNING"); got != "destroyed" {
		// stale=1 时 status 停在最后一次同步到的 RUNNING 上，
		// 必须以 stale 为准，否则界面会出现"已删除但状态是运行中"的自相矛盾
		t.Fatalf("stale 应压过 status，得到 %s", got)
	}
	if got := hostBucket(false, "RUNNING"); got != "running" {
		t.Fatalf("RUNNING 应为 running，得到 %s", got)
	}
}

func TestFacetsIgnoreDimensionFilters(t *testing.T) {
	// facets 必须按「不含维度筛选」的结果统计。
	// 若按筛选后的结果算，选了 running 之后 destroyed 就是 0，
	// 用户看不出"切过去还有 1 台"——而那正是下拉里数字的意义。
	_, _, facets := hostPage(sampleHosts(), q(1, 50, "", map[string]string{"status": "running"}))

	if got := facets["status"]["running"]; got != 3 {
		t.Fatalf("running 应为 3，得到 %d", got)
	}
	if got := facets["status"]["stopped"]; got != 1 {
		t.Fatalf("即使筛了 running，stopped 也应照实统计为 1，得到 %d", got)
	}
	if got := facets["status"]["destroyed"]; got != 1 {
		t.Fatalf("destroyed 应为 1，得到 %d", got)
	}
}

func TestFacetsAlwaysHaveAllStatusKeys(t *testing.T) {
	// 某个状态一台都没有时，facet 里也要有这个 key 且值为 0。
	// 缺 key 的话前端下拉会渲染成空白而不是「已停止 0」，
	// 看起来像功能坏了。
	onlyRunning := []hostOut{{CIID: 1, Name: "x", Status: "RUNNING"}}
	_, _, facets := hostPage(onlyRunning, q(1, 50, "", nil))
	for _, k := range []string{"running", "stopped", "destroyed"} {
		if _, ok := facets["status"][k]; !ok {
			t.Fatalf("facets.status 缺少 key %q", k)
		}
	}
}

func TestFilterByStatus(t *testing.T) {
	items, total, _ := hostPage(sampleHosts(), q(1, 50, "", map[string]string{"status": "destroyed"}))
	if total != 1 || len(items) != 1 || items[0].Name != "e-gone" {
		t.Fatalf("按 destroyed 筛应只剩 e-gone，得到 total=%d items=%v", total, names(items))
	}
}

func TestFilterCombined(t *testing.T) {
	items, total, _ := hostPage(sampleHosts(), q(1, 50, "",
		map[string]string{"status": "running", "project": "p1"}))
	if total != 2 {
		t.Fatalf("p1 下运行中应有 2 台，得到 %d（%v）", total, names(items))
	}
}

func TestSortByCostDesc(t *testing.T) {
	items, _, _ := hostPage(sampleHosts(), q(1, 50, "-cost", nil))
	if items[0].Name != "a-run" {
		t.Fatalf("按成本降序首位应为 a-run(300)，得到 %s", items[0].Name)
	}
	// 成本是应用层估算的，SQL 排不了序 —— 这条断言锁住内存排序确实生效了
	for i := 1; i < len(items); i++ {
		if items[i-1].CostMonth < items[i].CostMonth {
			t.Fatalf("降序被破坏: %v", names(items))
		}
	}
}

func TestSortUnknownFieldFallsBack(t *testing.T) {
	// 未知排序字段必须原样返回，绝不能报错或乱序 ——
	// 前端拼错字段名时用户应该看到一个正常的列表，只是没按他想的排
	orig := sampleHosts()
	items, _, _ := hostPage(orig, q(1, 50, "no_such_field", nil))
	for i := range items {
		if items[i].Name != orig[i].Name {
			t.Fatalf("未知排序字段应保持原顺序，得到 %v", names(items))
		}
	}
}

func TestPagination(t *testing.T) {
	items, total, _ := hostPage(sampleHosts(), q(2, 2, "name", nil))
	if total != 5 {
		t.Fatalf("total 应为筛选后的全量 5，得到 %d", total)
	}
	if len(items) != 2 || items[0].Name != "c-run" {
		t.Fatalf("第 2 页（每页 2）应为 c-run/d-stop，得到 %v", names(items))
	}
}

func TestLastPagePartial(t *testing.T) {
	items, _, _ := hostPage(sampleHosts(), q(3, 2, "name", nil))
	if len(items) != 1 {
		t.Fatalf("最后一页应只有 1 条，得到 %d", len(items))
	}
}

func TestPageOutOfRangeReturnsEmpty(t *testing.T) {
	// 越界页码返回空列表，而不是静默退回最后一页 ——
	// 退回最后一页会让前端的"下一页"永远可点，翻不到头
	items, total, _ := hostPage(sampleHosts(), q(99, 50, "", nil))
	if len(items) != 0 {
		t.Fatalf("越界页码应返回空，得到 %d 条", len(items))
	}
	if total != 5 {
		t.Fatalf("越界时 total 仍应是 5，得到 %d", total)
	}
}

func TestEmptyInputNeverPanics(t *testing.T) {
	items, total, facets := hostPage(nil, q(1, 50, "-cost", map[string]string{"status": "running"}))
	if len(items) != 0 || total != 0 {
		t.Fatalf("空输入应得到空结果，得到 %d/%d", len(items), total)
	}
	if facets["status"]["running"] != 0 {
		t.Fatal("空输入的 facet 应为 0")
	}
}

func names(hs []hostOut) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name
	}
	return out
}
