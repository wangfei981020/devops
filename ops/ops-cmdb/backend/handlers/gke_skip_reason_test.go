package handlers

import (
	"strings"
	"testing"
)

// 被筛掉的集群必须留下**能照着做**的原因。
//
// 守的是 OPSCMDB-031 P1-19：infra-01 / infra-02 两个 PROD 集群的升级页
// 显示「采集没有成功，原因未记录」。真相是它们**根本没进采集清单** ——
// `WHERE provider='gke' AND enabled=1` 把它们静默筛掉了，一条 last_error 都没写。
//
// ⚠️ "被筛掉"和"采集失败"在页面上长得一模一样，排查方向却完全相反：
// 前者去补集群登记信息，后者去查云账号权限。
func TestSkipReasonIsActionable(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		project  string
		location string
		acct     int
		want     []string
	}{
		{"不是 GKE", "k3s", "", "", 0, []string{"k3s", "不是 GKE"}},
		{"类型都没登记", "", "", "", 0, []string{"未登记", "不是 GKE"}},
		{"缺项目 ID", "gke", "", "asia-east2", 3, []string{"GCP 项目 ID", "集群"}},
		{"缺区域", "gke", "p-1", "", 3, []string{"区域", "集群"}},
		{"没关联云账号", "gke", "p-1", "asia-east2", 0, []string{"云账号", "集群"}},
		{"三项都缺", "gke", "", "", 0, []string{"GCP 项目 ID", "区域", "云账号"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := skipReason(c.provider, c.project, c.location, c.acct)
			if got == "" {
				t.Fatal("原因为空 —— 那就退回了 P1-19 的原状")
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("原因里没有 %q：%s", w, got)
				}
			}
		})
	}
}

// 三项都齐、类型也对，却还是没进清单 —— 这时候**不能编一个理由**。
//
// 编出来的理由会把人送去改一个本来就没问题的配置，比说"不知道"更糟。
func TestSkipReasonAdmitsUnknown(t *testing.T) {
	got := skipReason("gke", "p-1", "asia-east2", 3)
	if !strings.Contains(got, "原因不明") {
		t.Errorf("说得清的都说清了，说不清的必须承认，而不是编一条：%s", got)
	}
}
