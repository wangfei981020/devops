package handlers

import (
	"errors"
	"strings"
	"testing"
)

// 🔴 SQL 结构性错误整句不外发：它几乎必然带库名表名列名，
// 而对使用者没有任何可操作性 —— 那是我们的 bug，不是他的输入问题。
func TestSafeErrHidesSQLInternals(t *testing.T) {
	// 生产上真实出现过的那一条（验收会话 NEW-1）
	real := errors.New("Error 3065 (HY000): Expression #1 of ORDER BY clause is not in " +
		"SELECT list, references column 'ops_cmdb.obs_endpoints.env' which is not in " +
		"SELECT list; this is incompatible with DISTINCT")
	got := SafeErr("查夜莺接入环境", real)
	for _, leak := range []string{"ops_cmdb", "obs_endpoints", ".env", "ORDER BY", "3065"} {
		if strings.Contains(got, leak) {
			t.Errorf("泄露了内部标识 %q：%s", leak, got)
		}
	}
	if !strings.Contains(got, "查夜莺接入环境失败") {
		t.Errorf("没说清是哪一步失败了：%s", got)
	}
}

// 🔴 这一半同样重要：**该保留的不能糊掉**。
//
// 一律换成"系统错误"会把可自助处置的错误（凭据过期、权限不足、连不上）
// 也盖住，人就只能来问我们 —— 那是把一个安全问题换成了一个可用性问题。
func TestSafeErrKeepsActionableMessages(t *testing.T) {
	cases := map[string]string{
		"401 Unauthorized: token 已过期":      "401",
		"context deadline exceeded":        "deadline",
		"dial tcp: i/o timeout":            "timeout",
		"403 Forbidden: 缺少 iam.roles.list": "403",
	}
	for in, keep := range cases {
		got := SafeErr("拉取告警", errors.New(in))
		if !strings.Contains(got, keep) {
			t.Errorf("把可自助处置的信息糊掉了：输入 %q → 输出 %q（期望保留 %q）", in, got, keep)
		}
	}
}

// 内网地址与集群内 DNS 不外发 —— 它们是内部拓扑。
func TestSafeErrStripsInternalAddresses(t *testing.T) {
	cases := []struct{ in, gone string }{
		{`Get "http://vmselect.monitoring.svc:8481/api": dial tcp: lookup failed`, "vmselect.monitoring.svc"},
		{`dial tcp 10.170.1.19:9090: connect: connection refused`, "10.170.1.19"},
		{`connect 192.168.30.12:3306 failed`, "192.168.30.12"},
	}
	for _, c := range cases {
		got := SafeErr("连接数据源", errors.New(c.in))
		if strings.Contains(got, c.gone) {
			t.Errorf("泄露了内部地址 %q：%s", c.gone, got)
		}
		// ⚠️ 但"连不上/被拒绝"这个**性质**要留着，否则人不知道是网络问题
		if !strings.Contains(got, "refused") && !strings.Contains(got, "failed") &&
			!strings.Contains(got, "dial") {
			t.Errorf("连错误性质也糊掉了：%s", got)
		}
	}
}

func TestSafeErrNilAndLength(t *testing.T) {
	if got := SafeErr("同步", nil); got != "同步失败" {
		t.Errorf("nil error 应当只说哪一步失败，得到 %q", got)
	}
	long := errors.New(strings.Repeat("x", 1000))
	if got := SafeErr("同步", long); len(got) > 300 {
		t.Errorf("超长错误没有截断，长度 %d", len(got))
	}
}
