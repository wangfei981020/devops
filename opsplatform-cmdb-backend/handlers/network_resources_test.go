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
