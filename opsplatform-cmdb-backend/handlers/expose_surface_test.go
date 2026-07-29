package handlers

import (
	"strings"
	"testing"
)

// 暴露面的判定逻辑是「AI 只连 CMDB 就能下结论」的关键一环，判错的代价是把公网服务
// 当成内网放着不管。本地环境没有 Istio、没有云 LB，端到端跑不到这些分支，
// 所以这里直接锁住判定规则本身。

func TestIstioExposure(t *testing.T) {
	cases := []struct {
		name     string
		gateways string
		want     string
	}{
		{"外网网关", "istio-system/uat-istio-ingressgateway-extra", "external"},
		{"内网网关", "istio-system/uat-istio-ingressgateway-inner", "internal"},
		{"多网关含外网", "istio-system/uat-istio-ingressgateway-inner,istio-system/uat-istio-ingressgateway-extra", "external"},
		{"业务前缀外网网关", "istio-system/g50-uat-istio-ingressgateway-extra", "external"},
		{"未挂网关只在网格内", "", "unknown"},
		// 真实踩过的坑：写错成 innter，这条 VS 从创建起就没生效过。
		// 不能因为「看起来像 inner」就判成内网，必须暴露出来让人去修。
		{"拼写错误的网关不猜", "istio-system/uat-istio-ingressgateway-innter", "unknown"},
		{"完全陌生的网关不猜", "istio-system/some-custom-gw", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, basis := istioExposure(c.gateways)
			if got != c.want {
				t.Errorf("istioExposure(%q) = %q, 期望 %q", c.gateways, got, c.want)
			}
			if basis == "" {
				t.Error("判定依据不能为空——人工复核时要靠它")
			}
		})
	}
}

func TestServiceExposureOf(t *testing.T) {
	lbs := map[string]string{
		"34.92.20.123":  "EXTERNAL",
		"10.170.48.103": "INTERNAL",
	}
	cases := []struct {
		name, typ, extIP, lbType, want string
	}{
		// 云侧 scheme 是权威依据，优先级最高
		{"云侧标记外网", "LoadBalancer", "34.92.20.123", "", "external"},
		{"云侧标记内网", "LoadBalancer", "10.170.48.103", "", "internal"},
		// 云侧记录缺失时退到 K8s 注解
		{"仅有内网注解", "LoadBalancer", "10.0.0.9", "Internal", "internal"},
		// 注解与云侧冲突时以云侧为准：注解只是「申请」，云上建成什么样才算数
		{"注解称内网但云侧是外网", "LoadBalancer", "34.92.20.123", "Internal", "external"},
		// 拿不准一律 unknown，绝不猜
		{"有外部IP但云侧无记录", "LoadBalancer", "1.2.3.4", "", "unknown"},
		{"LB 还没分配到IP", "LoadBalancer", "", "", "unknown"},
		{"NodePort 取决于防火墙", "NodePort", "", "", "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, basis := serviceExposureOf(c.typ, c.extIP, c.lbType, lbs, nodeNetwork{})
			if got != c.want {
				t.Errorf("serviceExposureOf(%q,%q,%q, nodeNetwork{}) = %q, 期望 %q", c.typ, c.extIP, c.lbType, got, c.want)
			}
			if basis == "" {
				t.Error("判定依据不能为空")
			}
		})
	}
}

func TestFirstSensitivePort(t *testing.T) {
	cases := []struct {
		ports    string
		wantName string
		wantPort int
	}{
		{"6379/TCP", "Redis", 6379},
		{"2181/TCP", "ZooKeeper", 2181},
		{"80/TCP,8848/TCP", "Nacos", 8848},
		{"15020/TCP,15021/TCP,443/TCP", "", 0}, // istio 自身端口不算敏感
		{"80/TCP", "", 0},
		{"", "", 0},
	}
	for _, c := range cases {
		name, port := firstSensitivePort(c.ports)
		if name != c.wantName || port != c.wantPort {
			t.Errorf("firstSensitivePort(%q) = (%q,%d), 期望 (%q,%d)", c.ports, name, port, c.wantName, c.wantPort)
		}
	}
}

func TestAssessExposure(t *testing.T) {
	// 公网 + 敏感端口 → 必须 high
	it := exposureItem{Exposure: "external", Ports: "6379/TCP", TLS: "unknown", BackendAlive: "yes"}
	if sev, _ := assessExposure(&it); sev != "high" {
		t.Errorf("公网暴露 Redis 应判 high，实际 %q", sev)
	}
	// 内外网未知 + 敏感端口 → 也要 high：不能排除公网可达，按最坏情况处理
	it = exposureItem{Exposure: "unknown", Ports: "2181/TCP", TLS: "unknown", BackendAlive: "yes"}
	if sev, _ := assessExposure(&it); sev != "high" {
		t.Errorf("内外网未知 + ZooKeeper 端口应判 high，实际 %q", sev)
	}
	// 公网无 TLS → high
	it = exposureItem{Exposure: "external", Ports: "80/TCP", TLS: "no", BackendAlive: "yes"}
	if sev, _ := assessExposure(&it); sev != "high" {
		t.Errorf("公网无 TLS 应判 high，实际 %q", sev)
	}
	// 内网 + 敏感端口 → 提示但不到 high
	it = exposureItem{Exposure: "internal", Ports: "6379/TCP", TLS: "unknown", BackendAlive: "yes"}
	if sev, risks := assessExposure(&it); sev == "high" || len(risks) == 0 {
		t.Errorf("内网敏感端口应提示但不判 high，实际 sev=%q risks=%v", sev, risks)
	}
	// 断链：入口还在但后端没了
	it = exposureItem{Exposure: "internal", Ports: "80/TCP", TLS: "yes", BackendAlive: "no"}
	_, risks := assessExposure(&it)
	if len(risks) == 0 {
		t.Error("后端无存活实例应产生风险提示")
	}
	// 一切正常 → low 且无风险
	it = exposureItem{Exposure: "internal", Ports: "80/TCP", TLS: "yes", BackendAlive: "yes"}
	if sev, risks := assessExposure(&it); sev != "low" || len(risks) != 0 {
		t.Errorf("正常内网入口应为 low 且无风险，实际 sev=%q risks=%v", sev, risks)
	}
}

func TestBackendAlive(t *testing.T) {
	alive := map[string]bool{
		"g32-user/user-client-backend-svc": true,
		"devops/nacos":                     false, // Service 还在，但没有存活 Pod
	}
	cases := []struct {
		name, backends, ns, want string
	}{
		{"全限定域名解析到存活服务", "user-client-backend-svc.g32-user.svc.cluster.local", "g32-user", "yes"},
		{"服务存在但无后端", "nacos.devops.svc.cluster.local", "devops", "no"},
		{"集群内不存在的服务", "external-api.example.com", "g32-user", "unknown"},
		{"空后端", "", "g32-user", "unknown"},
		{"多后端只要有一个活着就算活", "nacos.devops.svc.cluster.local,user-client-backend-svc.g32-user.svc.cluster.local", "devops", "yes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := backendAlive(c.backends, c.ns, alive); got != c.want {
				t.Errorf("backendAlive(%q,%q) = %q, 期望 %q", c.backends, c.ns, got, c.want)
			}
		})
	}
}

// 敏感端口字典被暴露面判定和防火墙高危判定共用，两边口径必须一致。
func TestSensitivePortsConsistency(t *testing.T) {
	if len(sensitivePorts) != len(sensitivePortNames) {
		t.Fatalf("两份端口口径数量不一致: 字符串版 %d, 命名版 %d", len(sensitivePorts), len(sensitivePortNames))
	}
	for _, p := range []int{2181, 6379, 8848, 9876, 9092} {
		if _, ok := sensitivePortNames[p]; !ok {
			t.Errorf("端口 %d 应在敏感端口字典里（这些中间件默认无认证）", p)
		}
	}
}

// ── CMDB-009：k3s 上 NodePort 的可达性判定 ──────────────────────────

// 节点有公网 IP 是最强依据：NodePort 在每个节点上都监听，公网可达是确定的。
func TestNodePortExposurePublicNodes(t *testing.T) {
	exp, basis := nodePortExposure(nodeNetwork{total: 3, withPublic: 2, sampleIPs: []string{"1.2.3.4"}})
	if exp != "external" {
		t.Fatalf("有公网 IP 的节点应判 external，实际 %s", exp)
	}
	if !strings.Contains(basis, "1.2.3.4") || !strings.Contains(basis, "防火墙") {
		t.Errorf("依据应给出样例 IP 并提示还要看防火墙，实际: %s", basis)
	}
}

// 这是 CMDB-009 的核心：节点全无公网 IP 时不再一律 unknown，
// 给出带依据的 internal 判定，同时写明 NAT/端口转发这个例外。
func TestNodePortExposurePrivateNodesGivesConclusion(t *testing.T) {
	exp, basis := nodePortExposure(nodeNetwork{total: 14})
	if exp != "internal" {
		t.Fatalf("节点全无公网 IP 应判 internal 而非 unknown，实际 %s", exp)
	}
	if !strings.Contains(basis, "NAT") {
		t.Errorf("必须写明 NAT/端口转发这个例外，不能假装确定，实际: %s", basis)
	}
	if !strings.Contains(basis, "网络位置") {
		t.Errorf("应告诉用户如何覆盖该判定，实际: %s", basis)
	}
}

// 人工声明优先于「无公网 IP」的推断——存在 NAT 的集群靠它纠正。
func TestNodePortExposureDeclaredOverrides(t *testing.T) {
	if exp, _ := nodePortExposure(nodeNetwork{total: 14, declared: "public"}); exp != "external" {
		t.Errorf("声明 public 时应判 external，实际 %s", exp)
	}
	if exp, _ := nodePortExposure(nodeNetwork{total: 14, declared: "private"}); exp != "internal" {
		t.Errorf("声明 private 时应判 internal，实际 %s", exp)
	}
	// 但节点确实有公网 IP 时，事实依据优先于声明
	if exp, _ := nodePortExposure(nodeNetwork{total: 3, withPublic: 1, declared: "private"}); exp != "external" {
		t.Errorf("节点实际有公网 IP 时应以事实为准，实际 %s", exp)
	}
}

// 没有节点数据时不能瞎猜——那说明采集有问题，要说出来。
func TestNodePortExposureNoNodesStaysUnknown(t *testing.T) {
	exp, basis := nodePortExposure(nodeNetwork{})
	if exp != "unknown" {
		t.Errorf("无节点数据时应为 unknown，实际 %s", exp)
	}
	if !strings.Contains(basis, "采集") {
		t.Errorf("应提示先查采集是否正常，实际: %s", basis)
	}
}
