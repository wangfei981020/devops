package handlers

import "testing"

// 内网记录不能算"检测失败"——生产上 148 条失败里约 70 条是这类，
// 混在一起导致这个数字整列没法用（CMDB-041）。
func TestClassifyTLSError_Internal(t *testing.T) {
	cases := []struct {
		name     string
		msg      string
		originIP string
	}{
		{"origin 是 RFC1918", "连接 443 失败: dial tcp 172.16.14.161:443: i/o timeout", "172.16.14.161"},
		{"origin 空，dial 目标是内网", "连接 443 失败: dial tcp 172.16.14.161:443: i/o timeout", ""},
		{"origin 空，read 的远端是内网", "连接 443 失败: read tcp 172.16.14.161:51228->172.16.14.9:443: connection reset by peer", ""},
		{"10 段", "连接 443 失败: dial tcp 10.0.0.5:443: connect: connection refused", "10.0.0.5"},
		{"CGNAT 100.64/10", "连接 443 失败: dial tcp 100.100.1.2:443: i/o timeout", "100.100.1.2"},
		{"多个 origin 全内网", "连接 443 失败: dial tcp: i/o timeout", "10.0.0.5, 10.0.0.6"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyTLSError(c.msg, c.originIP)
			if got.Scope != scopeInternal {
				t.Fatalf("应判为内网不适用，实际 key=%s scope=%s", got.Key, got.Scope)
			}
		})
	}
}

// 有公网 origin 的失败是真失败，不能被"不适用"吞掉。
func TestClassifyTLSError_PublicStaysFailure(t *testing.T) {
	cases := []struct {
		msg      string
		originIP string
		wantKey  string
	}{
		{"连接 443 失败: dial tcp: lookup a.example.com: no such host", "", "dns"},
		{"连接 443 失败: dial tcp 1.2.3.4:443: i/o timeout", "1.2.3.4", "timeout"},
		{"连接 443 失败: dial tcp 1.2.3.4:443: connect: connection refused", "1.2.3.4", "refused"},
		{"连接 443 失败: read tcp 1.2.3.4:443: connection reset by peer", "1.2.3.4", "reset"},
		{"连接 443 失败: EOF", "1.2.3.4", "eof"},
		{"连接 443 失败: remote error: tls: handshake failure", "1.2.3.4", "tls"},
		{"未取到证书", "1.2.3.4", "nocert"},
		// 混合 origin：有一个公网地址就说明这条本该能探测到
		{"连接 443 失败: dial tcp: i/o timeout", "10.0.0.5, 1.2.3.4", "timeout"},
	}
	for _, c := range cases {
		got := classifyTLSError(c.msg, c.originIP)
		if got.Key != c.wantKey {
			t.Errorf("msg=%q origin=%q → key=%q，期望 %q", c.msg, c.originIP, got.Key, c.wantKey)
		}
		if got.Scope != scopePublic {
			t.Errorf("msg=%q 不该被判成内网", c.msg)
		}
	}
}

// ⚠️ 回归：错误串里的内网 IP 未必是连接目标。
//
//	集群 DNS 服务器就是 10.96.0.10 这种内网地址，于是**每一条 DNS 解析失败**
//	的错误串里都带内网 IP。第一版按"出现即算内网"判断，本地实测把 125 条
//	真实的 DNS 失败全归成了"内网不适用"——真故障被藏起来，比混在一起更糟。
func TestClassifyTLSError_DNSServerIPIsNotTarget(t *testing.T) {
	cases := []string{
		"连接 443 失败: dial tcp: lookup api.example.com on 10.96.0.10:53: no such host",
		"连接 443 失败: dial tcp: lookup a.example.com on 192.168.1.1:53: server misbehaving",
	}
	for _, msg := range cases {
		got := classifyTLSError(msg, "")
		if got.Scope == scopeInternal {
			t.Errorf("DNS 服务器地址不是连接目标，不该判成内网：%q", msg)
		}
		if got.Key != "dns" {
			t.Errorf("%q 应归为 dns，实际 %q", msg, got.Key)
		}
	}
	// 本机源地址同样不是目标：探测器自己在内网不代表被探的目标在内网
	got := classifyTLSError("连接 443 失败: read tcp 10.0.0.5:51228->1.2.3.4:443: connection reset by peer", "")
	if got.Scope == scopeInternal {
		t.Error("远端是公网地址，不该因为本机源地址是内网就判成内网")
	}
}

// 没见过的失败模式必须能在界面上看见，不能闷头归到"其他"。
func TestClassifyTLSError_UnknownKeepsOriginal(t *testing.T) {
	got := classifyTLSError("连接 443 失败: 某种全新的错误", "1.2.3.4")
	if got.Key == "" || got.Key == "other" {
		t.Fatalf("未知错误应保留原文作为分组键，实际 %q", got.Key)
	}
	if got.Label == "" {
		t.Fatal("未知错误的 label 不能为空")
	}
}

// 没失败的记录不该被归类。
func TestClassifyTLSError_Empty(t *testing.T) {
	if got := classifyTLSError("", "1.2.3.4"); got.Key != "" {
		t.Fatalf("空 msg 应返回零值，实际 %+v", got)
	}
	// 非致命告警（有到期日、msg 里只有 ⚠ 提示）也会走到这里，但那是 check_msg 非空
	// 且 expiry 有值的情况，调用方按 expiry 判定状态，这里只负责归类文本。
}
