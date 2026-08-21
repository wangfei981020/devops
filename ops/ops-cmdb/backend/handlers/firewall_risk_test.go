package handlers

import (
	"strings"
	"testing"
)

// hasSub 用 strings.Contains 的简称，避免和本包已有的 contains 撞名
func hasSub(s, sub string) bool { return strings.Contains(s, sub) }

// 防火墙高危判定。
//
// 这一组用例守的是 OPSCMDB-031 P0-6：后端判出 18 条高危、页面一条都没标。
// 那次的真因在前端（字段名 risky vs high_risk），但顺带暴露出**判定本身没有任何测试**——
// 一个只被前端间接使用的判据，一旦前端读错字段，判定错了也没人会知道。
func TestFwRisk(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		action    string
		protocols string
		src       string
		wantRisky bool
		// 理由里必须出现的关键字。空 = 不检查
		wantIn string
	}{
		{
			name:      "任意来源 + 未限端口 = 最严重",
			direction: "INGRESS", action: "allow", protocols: "", src: "0.0.0.0/0",
			wantRisky: true, wantIn: "未限制端口",
		},
		{
			// ⚠️ 生产上 infra-it-04 的源就是这么写的（没有 /0）。
			// 语义等同全网放行，但字符串比较对不上，曾经整条规则从高危清单里消失
			name:      "0.0.0.0 不带掩码，等同全网",
			direction: "INGRESS", action: "allow", protocols: "tcp:6379", src: "0.0.0.0",
			wantRisky: true, wantIn: "6379 Redis",
		},
		{
			name:      "敏感端口在区间里也要命中",
			direction: "INGRESS", action: "allow", protocols: "tcp:6000-7000", src: "0.0.0.0/0",
			wantRisky: true, wantIn: "6379 Redis",
		},
		{
			name:      "多个敏感端口都要列出来",
			direction: "INGRESS", action: "allow", protocols: "tcp:3306,6379", src: "0.0.0.0/0",
			wantRisky: true, wantIn: "3306 MySQL",
		},
		{
			name:      "裸 tcp 没写端口 = 未限端口",
			direction: "INGRESS", action: "allow", protocols: "tcp", src: "::/0",
			wantRisky: true, wantIn: "未限端口",
		},
		{
			name:      "源是具体网段就不算高危",
			direction: "INGRESS", action: "allow", protocols: "tcp:6379", src: "10.0.0.0/8",
			wantRisky: false,
		},
		{
			name:      "出站不算（出站放行的风险模型完全不同）",
			direction: "EGRESS", action: "allow", protocols: "tcp:6379", src: "0.0.0.0/0",
			wantRisky: false,
		},
		{
			name:      "deny 不算",
			direction: "INGRESS", action: "deny", protocols: "tcp:6379", src: "0.0.0.0/0",
			wantRisky: false,
		},
		{
			name:      "非敏感端口 + 全网 = 不算（80/443 本来就该开）",
			direction: "INGRESS", action: "allow", protocols: "tcp:80,443", src: "0.0.0.0/0",
			wantRisky: false,
		},
		{
			// 单个裸 IP 是 /32，不是任意来源。把它判成高危会造出一堆误报，
			// 而误报多了以后真高危就没人看了
			name:      "裸 IP 按 /32 算，不是任意来源",
			direction: "INGRESS", action: "allow", protocols: "tcp:6379", src: "1.2.3.4",
			wantRisky: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			risky, reason := fwRisk(c.direction, c.action, c.protocols, c.src)
			if risky != c.wantRisky {
				t.Fatalf("risky=%v，期望 %v（reason=%q）", risky, c.wantRisky, reason)
			}
			if !risky {
				if reason != "" {
					t.Errorf("不高危却给了理由 %q —— 理由和判定必须同步，否则界面会出现「不标红但有理由」", reason)
				}
				return
			}
			if reason == "" {
				t.Fatal("判成高危但没给理由：一个红标签不带理由，看的人只能自己猜")
			}
			if c.wantIn != "" && !hasSub(reason, c.wantIn) {
				t.Errorf("理由 %q 里没有 %q", reason, c.wantIn)
			}
		})
	}
}

// fwHighRisk 与 fwRisk 必须永远同结论。
//
// 两个函数各判一次是分叉的经典起点：暴露面判定和防火墙判定就曾经各记一份
// 敏感端口清单，结论互相打架。这个测试把它们钉在一起。
func TestFwHighRiskAgreesWithFwRisk(t *testing.T) {
	dirs := []string{"INGRESS", "EGRESS", "ingress"}
	acts := []string{"allow", "deny"}
	protos := []string{"", "tcp", "tcp:22", "tcp:80", "tcp:1-65535", "udp:53"}
	srcs := []string{"0.0.0.0/0", "0.0.0.0", "10.0.0.0/8", "::/0", ""}
	for _, d := range dirs {
		for _, a := range acts {
			for _, p := range protos {
				for _, s := range srcs {
					want, _ := fwRisk(d, a, p, s)
					if got := fwHighRisk(d, a, p, s); got != want {
						t.Fatalf("分叉了：fwHighRisk(%q,%q,%q,%q)=%v，fwRisk=%v", d, a, p, s, got, want)
					}
				}
			}
		}
	}
}

// 敏感端口命中列表必须**按端口号排序**。
// map 遍历顺序是随机的，不排序的话同一条规则每次刷新理由里的端口顺序都在变，
// 看起来像数据在跳。
func TestFwSensitiveHitsSorted(t *testing.T) {
	for i := 0; i < 20; i++ {
		got := fwSensitiveHits("tcp:27017,3306,6379")
		if len(got) != 3 {
			t.Fatalf("命中 %d 个，期望 3：%v", len(got), got)
		}
		if !hasSub(got[0], "3306") || !hasSub(got[1], "6379") || !hasSub(got[2], "27017") {
			t.Fatalf("没按端口号排序：%v", got)
		}
	}
}
