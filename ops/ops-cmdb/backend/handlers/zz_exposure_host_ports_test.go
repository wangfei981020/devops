package handlers

import "testing"

// TestHostOpenPorts_区分「没采到规则」与「确实没放行」
//
// 两者都会让端口列看起来是空的，但结论完全相反：
// 前者是我们不知道，后者是可以下结论说这台机器公网进不来。
func TestHostOpenPorts_区分没采到与确实没放行(t *testing.T) {
	// ① 一条规则都没采到 → 不知道
	if ports, known, basis := hostOpenPorts(nil, []string{"web"}); known {
		t.Errorf("没有任何规则时应为 known=false，实际 known=true ports=%q basis=%q", ports, basis)
	}

	// ② 采到了规则但没有一条命中这台主机 → 可以下结论
	rules := []hostFirewall{{protocols: "tcp:443", targetTags: []string{"lb"}}}
	ports, known, basis := hostOpenPorts(rules, []string{"web"})
	if !known {
		t.Error("采到规则但未命中，应当能下结论（known=true）")
	}
	if ports != "无公网入站放行" {
		t.Errorf("未命中时应明说「无公网入站放行」，实际 %q", ports)
	}
	if basis != "firewall" {
		t.Errorf("判据应为 firewall，实际 %q", basis)
	}

	// ③ target_tags 为空 = 作用于网络内所有实例
	all := []hostFirewall{{protocols: "tcp:22", targetTags: nil}}
	if ports, known, _ := hostOpenPorts(all, []string{"anything"}); !known || ports != "tcp:22" {
		t.Errorf("空 target_tags 应作用于所有实例，实际 ports=%q known=%v", ports, known)
	}

	// ④ 标签有交集才命中
	tagged := []hostFirewall{{protocols: "tcp:3306", targetTags: []string{"db", "internal"}}}
	if ports, _, _ := hostOpenPorts(tagged, []string{"db"}); ports != "tcp:3306" {
		t.Errorf("标签交集应命中，实际 %q", ports)
	}
}
