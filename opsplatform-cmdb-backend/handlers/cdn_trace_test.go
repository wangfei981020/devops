package handlers

import "testing"

func TestIsCloudflareIP(t *testing.T) {
	// 用户 dig 出来的真实 CF 边缘 IP，必须命中
	for _, ip := range []string{"104.18.20.2", "104.18.21.2", "172.64.1.1", "162.158.0.1"} {
		if !isCloudflareIP(ip) {
			t.Errorf("%s 属 Cloudflare 段，应判定为 true", ip)
		}
	}
	// 我方 GKE 网关的真实源站 IP，不能误判成 CF
	for _, ip := range []string{"34.150.1.177", "10.170.48.43", "35.220.152.223", ""} {
		if isCloudflareIP(ip) {
			t.Errorf("%s 不属 Cloudflare 段，误判为 true", ip)
		}
	}
}
