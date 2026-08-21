package handlers

import "testing"

func TestHostFromProbeInstance(t *testing.T) {
	// 样本全部取自生产实测的 blackbox instance，不要自己编
	cases := map[string]string{
		"https://game.dragontiger-game.com":                   "game.dragontiger-game.com",
		"https://gci-api.g01prod.com/health_check":            "gci-api.g01prod.com",
		"https://cdnm2q.mopaxdruen.com/config.js":             "cdnm2q.mopaxdruen.com",
		"https://crav.onstpleihdky.com:7855":                  "crav.onstpleihdky.com",
		"http://g01-goagentwallet-w.agqjapi.com/api/x/health": "g01-goagentwallet-w.agqjapi.com",
		"goproxy-sl.youxijiekou.com:6339":                     "goproxy-sl.youxijiekou.com",
		// 🔴 纯 IP 的目标没有域名可对，必须丢掉而不是当成主机名 ——
		//	留着它会在"失管域名"那一桶里堆一批 IP，把真正失管的域名淹掉
		"120.76.142.21:9115": "",
		"10.0.0.1:9115":      "",
		"":                   "",
	}
	for in, want := range cases {
		if got := hostFromProbeInstance(in); got != want {
			t.Errorf("hostFromProbeInstance(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// 🔴 必须是**后缀边界**匹配。
// 用 Contains 的话 `evil-g01prod.com` 会被算成我们的 g01prod.com ——
// 别人的域名混进对账结果，而结果看起来完全正常。
func TestMatchRootDomain(t *testing.T) {
	ledger := map[string]bool{
		"g01prod.com":          true,
		"dragontiger-game.com": true,
		"g01-uat.com":          true,
	}
	cases := map[string]string{
		"gci-api.g01prod.com":       "g01prod.com",
		"game.dragontiger-game.com": "dragontiger-game.com",
		"g01prod.com":               "g01prod.com", // 根域名自己
		"a.b.c.g01prod.com":         "g01prod.com", // 多级子域
		// 🔴 这三条是判据的分界线，改实现时先跑它们
		"evil-g01prod.com":     "", // 前缀粘连，不是子域
		"g01prod.com.evil.net": "", // 后缀是别人的
		"notg01prod.com":       "",
		"unknown.example.com":  "",
	}
	for host, want := range cases {
		if got := matchRootDomain(host, ledger); got != want {
			t.Errorf("matchRootDomain(%q) = %q, 期望 %q", host, got, want)
		}
	}
}
