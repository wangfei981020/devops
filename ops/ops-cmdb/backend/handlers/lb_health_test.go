package handlers

import (
	"testing"

	"ops-cmdb-backend/internal/httpx"
)

// pageQueryForTest 一个不筛不排的分页参数，只为验证默认排序与 facets。
func pageQueryForTest() httpx.PageQuery {
	return httpx.PageQuery{Page: 1, Size: 50, Filters: map[string]string{}}
}

func i64(n int64) *int64 { return &n }

// LB 后端健康判定。
//
// 守的是 OPSCMDB-031 P0-8：生产 49 条里 37 条被判成「确认无后端」，
// 而**真正打不通的是 0 条**。
//
// 那 37 条是 33 条 fgt-*（FortiGate 转发规则）+ 4 条 gkegw1-*（Gateway API），
// 它们的 target 指向 target instance / target proxy，
// **结构上就不该有 GCP 后端服务** —— 不是故障。
//
// ⚠️ 判据线索：集中的异常分布先怀疑判据。
// 37 条"故障"里 33 条同名前缀、指向同一个 target，这不可能是真实故障。
func TestLBHealthDoesNotCallNormalLBsBroken(t *testing.T) {
	cases := []struct {
		name string
		lb   lbOut
		want string
	}{
		{
			// 这一条是 P0-8 的原型：fgt-forwarding-*，target 指向 target instance
			name: "FortiGate 转发规则：有 target、0 后端 → 由 target 承载，不是故障",
			lb:   lbOut{Name: "fgt-forwarding-1", Target: "fgt-target-1", BackendState: "unsupported", Backends: i64(0)},
			want: "viaTarget",
		},
		{
			name: "Gateway API：同上",
			lb:   lbOut{Name: "gkegw1-abc", Target: "gkegw1-target-proxy", BackendState: "unsupported", Backends: i64(0)},
			want: "viaTarget",
		},
		{
			// GKE 的 Service type=LoadBalancer 后端是 Pod(NEG)，实例组里看不到，
			// 但它正在服务。实测生产 8 条 VIP 8/8 命中 K8s Service
			name: "K8s Service 后端：0 实例但在服务",
			lb:   lbOut{Name: "a1b2c3", BackendState: "k8s", Backends: i64(0)},
			want: "k8s",
		},
		{
			// 这才是真问题：target 为空、确认一个后端都没有
			name: "target 为空 + 0 后端 → 真的打不通",
			lb:   lbOut{Name: "orphan-lb", Target: "", BackendState: "none", Backends: i64(0)},
			want: "empty",
		},
		{
			// 上游拉失败：**绝不能**说成"没有后端"
			name: "上游某一跳失败 → 未知，不是无后端",
			lb:   lbOut{Name: "x", Target: "t", BackendState: "unresolved", Backends: i64(0)},
			want: "unknown",
		},
		{
			// 采集时追溯到了、读出来没有 → 数据丢了，结论不可信
			name: "数据丢失要单列，不能混进无后端",
			lb:   lbOut{Name: "y", Target: "t", BackendState: "lost", Backends: i64(0)},
			want: "lost",
		},
		{
			name: "没采过 → 未知",
			lb:   lbOut{Name: "z", Backends: nil},
			want: "unknown",
		},
		{
			name: "有后端 → 正常",
			lb:   lbOut{Name: "w", BackendState: "ok", Backends: i64(3)},
			want: "ok",
		},
		{
			// stale 优先于一切：云上已删的 LB 谈不上"有没有后端"
			name: "云上已删优先",
			lb:   lbOut{Name: "gone", Stale: true, BackendState: "none", Backends: i64(0)},
			want: "stale",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lbHealth(c.lb); got != c.want {
				t.Errorf("lbHealth=%q，期望 %q", got, c.want)
			}
		})
	}
}

// 「正常形态」不能排在「真问题」前面。
//
// viaTarget / k8s 是正常的，把它们顶到列表最前面等于把 37 条正常 LB
// 推到运维眼前，而真正打不通的那条被挤到后面 —— 这和不修一样糟。
func TestLBSortPutsRealProblemsFirst(t *testing.T) {
	all := []lbOut{
		{Name: "b-via", Target: "t", BackendState: "unsupported", Backends: i64(0)},
		{Name: "c-k8s", BackendState: "k8s", Backends: i64(0)},
		{Name: "a-broken", Target: "", BackendState: "none", Backends: i64(0)},
		{Name: "d-ok", BackendState: "ok", Backends: i64(2)},
		{Name: "e-lost", Target: "t", BackendState: "lost", Backends: i64(0)},
	}
	items, total, facets := lbPage(all, pageQueryForTest())
	if total != 5 {
		t.Fatalf("total=%d，期望 5", total)
	}
	if items[0].Name != "a-broken" {
		t.Errorf("排第一的是 %q，期望 a-broken（唯一真打不通的）", items[0].Name)
	}
	if items[1].Name != "e-lost" {
		t.Errorf("排第二的是 %q，期望 e-lost（数据不可信，第二该看）", items[1].Name)
	}
	// facets 里 empty 必须只有 1 —— 那个红字计数就是从这儿来的
	if n := facets["health"]["empty"]; n != 1 {
		t.Errorf("facets.health.empty=%d，期望 1（原来这里会是 3）", n)
	}
	if n := facets["health"]["viaTarget"]; n != 1 {
		t.Errorf("facets.health.viaTarget=%d，期望 1", n)
	}
}

// 🔴 实测发现：backend_state 这一列有**两套取值**。
//
//	采集侧写的是小写的追溯结果（ok/none/unsupported/unresolved），
//	而库里也存在大写的云原生健康度（HEALTHY/UNHEALTHY）——
//	后者回答的是"后端健不健康"，不是"有没有追溯到后端"，是另一个维度。
//
// 这一组守住两件事：
//  1. 云说 HEALTHY 而我们数到 0 个后端时，**结论是"不知道"而不是"打不通"** ——
//     说打不通会让人去查一个云上认为健康的 LB
//  2. 不认识的取值不能静默落到"只看条数"（那是 P0-8 的失效模式）
func TestLBHealthHandlesCloudNativeStates(t *testing.T) {
	cases := []struct {
		name string
		lb   lbOut
		want string
	}{
		{
			// 矛盾场景：云自报健康，我们一个后端都没数到
			name: "云说 HEALTHY 但我们数到 0 → 不知道，不能说打不通",
			lb:   lbOut{Name: "dev-lb-x", BackendState: "HEALTHY", Backends: i64(0)},
			want: "unknown",
		},
		{
			name: "云说 HEALTHY 且我们数到了 → 正常",
			lb:   lbOut{Name: "dev-lb-api", BackendState: "HEALTHY", Backends: i64(2)},
			want: "ok",
		},
		{
			// UNHEALTHY + 0 后端：两个结论一致，可以判 empty
			name: "云说 UNHEALTHY 且 0 后端 → 打不通（两个结论一致）",
			lb:   lbOut{Name: "dev-lb-pay", BackendState: "UNHEALTHY", Backends: i64(0)},
			want: "empty",
		},
		{
			name: "大小写不敏感：healthy 也要认",
			lb:   lbOut{Name: "y", BackendState: "healthy", Backends: i64(0)},
			want: "unknown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lbHealth(c.lb); got != c.want {
				t.Errorf("lbHealth=%q，期望 %q", got, c.want)
			}
		})
	}
}

// 已知取值集必须覆盖两套，否则启动自检会对正常数据刷 WARN
// （一个天天误报的告警等于没有告警）。
func TestKnownBackendStatesCoversBothSets(t *testing.T) {
	for _, st := range []string{
		// 采集侧
		"", "ok", "none", "unsupported", "unresolved",
		// 读取侧加工
		"lost", "k8s",
		// 云原生健康度（实测库里就有）
		"HEALTHY", "UNHEALTHY",
	} {
		if !knownBackendStates[st] {
			t.Errorf("已知取值集漏了 %q —— 启动自检会对正常数据误报 WARN", st)
		}
	}
	// 反面：真的没见过的值必须被认出来，否则自检等于没有
	if knownBackendStates["SOMETHING_NEW"] {
		t.Error("已知取值集把任意值都当成已知了，自检失效")
	}
}
