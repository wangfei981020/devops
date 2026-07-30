package handlers

import "testing"

// 通配符只覆盖一级子域，这是 TLS 的规则。放宽会得出「证书覆盖了」的错误结论——
// 而那种错误最隐蔽：界面上显示证书没问题，用户实际访问却是证书告警。
func TestCertCoversHost(t *testing.T) {
	hosts := "uatcfzone.com,*.uatcfzone.com"
	yes := []string{"uatcfzone.com", "g32-uat-istio-gateway.uatcfzone.com", "www.uatcfzone.com"}
	no := []string{
		"a.b.uatcfzone.com",  // 两级子域，*.uatcfzone.com 覆盖不到
		"otheruatcfzone.com", // 没有点分隔，不能靠后缀字符串蒙过
		"uatcfzone.com.evil.com",
		"",
	}
	for _, d := range yes {
		if !certCoversHost(hosts, d) {
			t.Errorf("%q 应被 %q 覆盖", d, hosts)
		}
	}
	for _, d := range no {
		if certCoversHost(hosts, d) {
			t.Errorf("%q 不该被 %q 覆盖", d, hosts)
		}
	}
	// 大小写不敏感
	if !certCoversHost("*.EXAMPLE.com", "API.example.COM") {
		t.Error("匹配应大小写不敏感")
	}
}
