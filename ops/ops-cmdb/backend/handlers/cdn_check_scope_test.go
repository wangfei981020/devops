package handlers

import (
	"strings"
	"testing"
)

// 「不在已知入口集合中」能不能判为异常，取决于集合本身完不完整。
// 首次跑真实数据时 owned 只有 11 个 IP，却照常输出了 20 条 high，几乎全是误报——
// prod 集群没纳管、公共 DNS、云厂商地址段天然不在集合里。
// 这几条用来钉死：前提不成立时不许给强结论。

func TestManagedScopeJudgeDowngradesWhenIncomplete(t *testing.T) {
	// 纳管 2 个集群却只有 5 个入口 IP（下限 2×3=6），集合明显不完整
	incomplete := managedScope{clusters: 2, hosts: 3, ownedIPs: 5}
	incomplete.trustworthy = false

	sev, issue, action := incomplete.judge("35.241.108.68")
	if sev != "medium" {
		t.Errorf("集合不完整时应降级为 medium，实际 %q", sev)
	}
	// 措辞必须说「查不到」而不是「不是我方的」——这两者含义差很大
	if !strings.Contains(issue, "查不到") || !strings.Contains(issue, "不代表") {
		t.Errorf("措辞应明确这是「查不到」而非「不属于我方」，实际: %s", issue)
	}
	if !strings.Contains(action, "纳管") {
		t.Errorf("建议里应提示把未纳管环境纳入 CMDB，实际: %s", action)
	}
}

func TestManagedScopeJudgeStrongWhenComplete(t *testing.T) {
	complete := managedScope{clusters: 2, hosts: 20, ownedIPs: 30}
	complete.trustworthy = true

	sev, issue, _ := complete.judge("1.2.3.4")
	if sev != "high" {
		t.Errorf("集合完整时才允许判 high，实际 %q", sev)
	}
	if !strings.Contains(issue, "不在我方任何已知入口") {
		t.Errorf("完整时应给出确定结论，实际: %s", issue)
	}
}

// 可信度阈值本身：集群数 × 3 是经验下限，改动它等于改变判定强度，必须是有意识的。
func TestManagedScopeTrustworthyThreshold(t *testing.T) {
	cases := []struct {
		name     string
		clusters int
		ownedIPs int
		want     bool
	}{
		{"两集群仅5个IP-不可信", 2, 5, false},
		{"两集群刚好6个IP-可信", 2, 6, true},
		{"两集群30个IP-可信", 2, 30, true},
		{"一个集群都没纳管-不可信", 0, 100, false},
		{"三集群8个IP-不可信", 3, 8, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.ownedIPs >= c.clusters*3 && c.clusters > 0
			if got != c.want {
				t.Errorf("clusters=%d ownedIPs=%d 可信度应为 %v，实际 %v",
					c.clusters, c.ownedIPs, c.want, got)
			}
		})
	}
}

// 第三方地址不该混进待清理清单——噪声会把真问题一起淹掉。
func TestWellKnownThirdParty(t *testing.T) {
	hits := map[string]string{
		"8.8.8.8":         "Google 公共 DNS",
		"8.8.4.4":         "Google 公共 DNS",
		"1.1.1.1":         "Cloudflare 公共 DNS",
		"223.5.5.5":       "阿里公共 DNS",
		"114.114.114.114": "114 公共 DNS",
		"127.0.0.1":       "回环地址",
	}
	for ip, want := range hits {
		if got := wellKnownThirdParty(ip); got != want {
			t.Errorf("wellKnownThirdParty(%q) = %q, 期望 %q", ip, got, want)
		}
	}
	// 真实业务 IP 不能被误判成第三方，否则真问题会被降级掉
	for _, ip := range []string{"34.92.20.123", "35.241.108.68", "10.170.48.103", "8.9.10.11"} {
		if got := wellKnownThirdParty(ip); got != "" {
			t.Errorf("%q 不该被判为第三方地址，实际判成 %q", ip, got)
		}
	}
}

// PVC 使用率在 k3s local-path 下会报成宿主机水位（同节点所有卷数值相同）。
// 判定必须靠 storageClass 这个定义性特征，而不是靠「数值相同」去猜——数值相同也可能是巧合。
func TestIsSharedFSStorageClass(t *testing.T) {
	shared := []string{"local-path", "local-storage", "hostpath", "nfs-client", "manual", "LOCAL-PATH"}
	for _, sc := range shared {
		if !isSharedFSStorageClass(sc) {
			t.Errorf("%q 应判为与宿主机共用文件系统", sc)
		}
	}
	// 独立块设备的 StorageClass 不能被误判，否则真实的 PVC 用量会被标成不可信
	for _, sc := range []string{"standard-rwo", "premium-rwo", "gp3", "alicloud-disk-ssd", "ceph-rbd", ""} {
		if isSharedFSStorageClass(sc) {
			t.Errorf("%q 是独立块设备，不该判为共用文件系统", sc)
		}
	}
}
