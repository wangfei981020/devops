package handlers

import (
	"strings"
	"testing"
)

// CI 名必须带集群。
//
// 🔴 不带的话，不同集群里同名的 Service（`istio-system/istio-ingressgateway`
// 这种几乎必然在每个集群都有）会被合并成**同一个点**，
// 于是图上出现一条跨集群的假边 —— 而假边比缺边危险得多：
// 排障时人会顺着它走到另一个集群去。
func TestK8sObjCINameIncludesCluster(t *testing.T) {
	a := k8sObj{cluster: "uat", namespace: "istio-system", name: "istio-ingressgateway"}
	b := k8sObj{cluster: "prod", namespace: "istio-system", name: "istio-ingressgateway"}
	if a.ciName() == b.ciName() {
		t.Fatal("两个集群的同名 Service 生成了同一个 CI 名 —— 图上会出现跨集群假边")
	}
	if a.key() == b.key() {
		t.Fatal("查找键也必须带集群，否则本轮内部就串味了")
	}
	if !strings.Contains(a.ciName(), "uat") || !strings.Contains(a.ciName(), "istio-system") {
		t.Errorf("CI 名要能一眼看出是哪个集群哪个命名空间：%s", a.ciName())
	}
}

// svc_names / hosts 是逗号分隔的串，各种分隔与空白都要吃得下。
func TestSplitList(t *testing.T) {
	cases := map[string][]string{
		"a,b,c":   {"a", "b", "c"},
		" a , b ": {"a", "b"},
		"a;b c":   {"a", "b", "c"},
		"a,,b":    {"a", "b"},
		"":        {},
		"   ":     {},
		"a\nb\tc": {"a", "b", "c"},
	}
	for in, want := range cases {
		got := splitList(in)
		if len(got) != len(want) {
			t.Errorf("splitList(%q) = %v，期望 %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitList(%q) = %v，期望 %v", in, got, want)
				break
			}
		}
	}
	// 空串**必须**返回空切片而不是 [""]：
	// 一个空名字会被拿去查 Service，查不到就计进 SvcNotFound，
	// 于是摘要里报出一个不存在的"断链"
	if len(splitList("")) != 0 {
		t.Error("空串要返回空切片，否则会伪造出一条「Service 找不到」")
	}
}

// 🔴 摘要必须写出**没建上什么**。
//
// 只报"建了 N 条边"会让人以为图是完整的，而 Service → 工作负载
// 这一整跳根本没建（我们没采 selector）。一张看起来完整、实际缺一层的
// 拓扑图，比没有图更容易把人带偏。
func TestK8sEdgeSummaryStatesTheGap(t *testing.T) {
	s := k8sEdgeSummary(k8sEdgeStats{Services: 71, Ingresses: 2, IngressToSvc: 3, DomainToIngress: 1})
	for _, want := range []string{"71", "2", "3", "未建", "selector"} {
		if !strings.Contains(s, want) {
			t.Errorf("摘要里缺 %q：%s", want, s)
		}
	}
}

// 「Ingress 指向的 Service 不在台账里」要单独报出来。
//
// ⚠️ 它不是"没有关系"，是**关系断了**：要么 Service 被删了
// （这个 Ingress 现在转发不到任何后端），要么采集漏了。
// 两种都值得看，而"图上少一条线"是看不出来的。
func TestK8sEdgeSummaryReportsBrokenIngress(t *testing.T) {
	s := k8sEdgeSummary(k8sEdgeStats{Services: 10, Ingresses: 3, SvcNotFound: 2})
	if !strings.Contains(s, "不在台账里") {
		t.Errorf("断链没报出来：%s", s)
	}
	// 而没有断链时不该凭空说一句
	clean := k8sEdgeSummary(k8sEdgeStats{Services: 10, Ingresses: 3})
	if strings.Contains(clean, "不在台账里") {
		t.Errorf("没有断链时不该报：%s", clean)
	}
}

// 未纳管的域名和断链是两回事，措辞要分开。
func TestK8sEdgeSummarySeparatesUnmanagedDomains(t *testing.T) {
	s := k8sEdgeSummary(k8sEdgeStats{IngressHostNoDomain: 5})
	if !strings.Contains(s, "正常") {
		t.Errorf("Ingress 域名不在台账里是正常的（未纳管），不能说得像故障：%s", s)
	}
}
