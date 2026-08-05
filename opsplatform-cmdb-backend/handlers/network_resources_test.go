package handlers

import "testing"

// 防火墙高危判定与暴露面判定共用同一份敏感端口字典。字典扩充过一次
// （补进 ZooKeeper/Nacos/Kafka/RocketMQ），这里锁住新口径确实生效。
func TestFwHighRisk(t *testing.T) {
	cases := []struct {
		name                              string
		direction, action, protos, source string
		want                              bool
	}{
		// 字典扩充前这几条会被判成不高危，UAT 上实测到的正是这种漏报
		{"公网放行 ZooKeeper", "INGRESS", "allow", "tcp:2181", "0.0.0.0/0", true},
		{"公网放行 RocketMQ", "INGRESS", "allow", "tcp:9876", "0.0.0.0/0", true},
		{"公网放行 Nacos", "INGRESS", "allow", "tcp:8848", "0.0.0.0/0", true},
		{"公网放行 Kafka", "INGRESS", "allow", "tcp:9092", "0.0.0.0/0", true},
		{"公网放行 Redis", "INGRESS", "allow", "tcp:6379", "0.0.0.0/0", true},
		{"公网全放行", "INGRESS", "allow", "all", "0.0.0.0/0", true},
		{"公网放行裸 tcp(等于全端口)", "INGRESS", "allow", "tcp", "0.0.0.0/0", true},

		// 不该误报的
		{"限定来源网段不算高危", "INGRESS", "allow", "tcp:2181", "10.0.0.0/8", false},
		{"出站规则不算", "EGRESS", "allow", "tcp:2181", "0.0.0.0/0", false},
		{"deny 规则不算", "INGRESS", "deny", "tcp:2181", "0.0.0.0/0", false},
		{"公网放行普通业务端口", "INGRESS", "allow", "tcp:8080", "0.0.0.0/0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fwHighRisk(c.direction, c.action, c.protos, c.source); got != c.want {
				t.Errorf("fwHighRisk(%q,%q,%q,%q) = %v, 期望 %v",
					c.direction, c.action, c.protos, c.source, got, c.want)
			}
		})
	}
}

// 端口匹配走的是字符串包含，容易把 2181 匹配到 21810 上。
// 这条用来确认边界情况不会误报——误报会让真高危淹没在噪声里。
func TestFwHighRiskPortBoundary(t *testing.T) {
	if fwHighRisk("INGRESS", "allow", "tcp:21810", "0.0.0.0/0") {
		t.Error("tcp:21810 不该因为包含子串 2181 而被判高危")
	}
	if fwHighRisk("INGRESS", "allow", "tcp:63790", "0.0.0.0/0") {
		t.Error("tcp:63790 不该因为包含子串 6379 而被判高危")
	}
	// 多端口列表里真的含敏感端口时仍要判出来
	if !fwHighRisk("INGRESS", "allow", "tcp:80,443,6379", "0.0.0.0/0") {
		t.Error("端口列表中含 6379 应判高危")
	}
}

// 生产上漏判的那条：infra-it-04 的源写的是 `0.0.0.0`（没有 /0）。
// 语义等同全网放行，但原来用 strings.Contains(s,"0.0.0.0/0") 硬比字符串，
// 于是这条 source=0.0.0.0 + 全端口 + allow + 入站的规则**根本没进高危清单**。
// 少报比误报更危险：清单看着干净，真高危却不在里面。
func TestFwSourceIsAnywhere(t *testing.T) {
	anywhere := []string{
		"0.0.0.0/0",
		"0.0.0.0",           // ← 生产 infra-it-04 的写法
		"::/0",              // IPv6 全网
		"::",                //
		"10.0.0.0/8,0.0.0.0/0", // 混在一堆内网网段里
		"10.0.0.0/8, 0.0.0.0",  // 带空格 + 省略掩码
		"172.16.0.0/16;0.0.0.0/0",
	}
	for _, s := range anywhere {
		if !fwSourceIsAnywhere(s) {
			t.Errorf("源 %q 等同全网放行，必须判为任意来源", s)
		}
	}

	notAnywhere := []string{
		"",
		"10.0.0.0/8",
		"10.0.0.0/8,172.16.0.0/16",
		"35.235.240.0/20",              // IAP
		"130.211.0.0/22,35.191.0.0/16", // GCP 健康检查
		"0.0.0.1",                      // 长得像但不是
		"10.170.96.3",                  // 裸 IP = /32
	}
	for _, s := range notAnywhere {
		if fwSourceIsAnywhere(s) {
			t.Errorf("源 %q 不是全网，误判会把高危清单灌满假货——真高危就会被一起忽略", s)
		}
	}
}

// 端到端：复刻 infra-it-04 的形态，确认它现在能被判成高危
func TestFwHighRisk_省略掩码的全网放行(t *testing.T) {
	if !fwHighRisk("INGRESS", "allow", "all", "0.0.0.0") {
		t.Fatal("source=0.0.0.0 + 全端口 + allow + 入站 必须是高危（生产 infra-it-04 就是这条）")
	}
	// 别把正常规则带成高危
	if fwHighRisk("INGRESS", "allow", "tcp:443", "10.0.0.0/8") {
		t.Error("内网来源不该判高危")
	}
	if fwHighRisk("EGRESS", "allow", "all", "0.0.0.0/0") {
		t.Error("出站不该判高危")
	}
}
