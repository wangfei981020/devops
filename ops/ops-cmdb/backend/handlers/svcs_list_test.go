package handlers

import "testing"

// ★ 内网 LB 不能算对外暴露。
//
// k8s 把内网 LB 的 VIP 也写进 status.loadBalancer.ingress，
// 也就是我们的 external_ip 列。不排掉私网地址的话，每个内网 LB
// 都会进暴露面清单 —— 而误报多了，真正暴露在公网的那几个就没人看了。
func TestIsPublicIP(t *testing.T) {
	private := []string{
		"10.128.0.90",     // GCP 内网 LB 常见网段
		"172.16.4.5",      //
		"192.168.1.10",    //
		"127.0.0.1",       // 回环
		"169.254.169.254", // 链路本地（云元数据）
		"0.0.0.0",         //
		"",                // 没有 IP
		"pending",         // 解析不了的值：宁可漏报，也不要塞进安全清单
		"fd00::1",         // IPv6 ULA
	}
	for _, ip := range private {
		if isPublicIP(ip) {
			t.Errorf("%q 不该被当成公网地址", ip)
		}
	}
	for _, ip := range []string{"34.80.1.10", "8.8.8.8", "2400:cb00::1"} {
		if !isPublicIP(ip) {
			t.Errorf("%q 应当是公网地址", ip)
		}
	}
}

func TestSvcExposureInternalLBNotExposed(t *testing.T) {
	internal := svcOut{Type: "LoadBalancer", ExternalIP: "10.128.0.90", Hosts: []string{}}
	internal.Exposed = len(internal.Hosts) > 0 || isPublicIP(internal.ExternalIP)
	internal.PendingLB = internal.Type == "LoadBalancer" && internal.ExternalIP == ""
	if got := svcExposure(internal); got != "internal" {
		t.Errorf("内网 LB 的暴露档 = %q，期望 internal", got)
	}
	// 而拿不到任何 IP 的 LoadBalancer 是**卡住了**，不是内部服务
	stuck := svcOut{Type: "LoadBalancer", ExternalIP: "", Hosts: []string{}}
	stuck.PendingLB = true
	if got := svcExposure(stuck); got != "pending" {
		t.Errorf("卡住的 LB 暴露档 = %q，期望 pending", got)
	}
}
