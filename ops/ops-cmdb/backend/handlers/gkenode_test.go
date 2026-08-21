package handlers

import "testing"

// 判据的分界线用**生产上真实存在的名字**来写，不要自己编。
// 编出来的样本会不自觉地贴合当前实现，测不出边界。
func TestDetectGKENode(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   bool
		why    string
	}{
		// 真节点：名字带 GKE 生成的 `-<8位hex>-<4位>` 尾巴
		{"gke-g32-prod-cluster-g32-prod-cluster-1854a453-28kd", nil, true, "g32-prod 真节点"},
		{"gke-infra-k8s-cluster-02-demo-pool-01-53026855-ggck", nil, true, "infra-02 真节点（已销毁但仍是节点）"},

		// 🔴 名字以 gke- 开头的**普通虚机**。只看前缀会把它们全判成节点，
		//	进而在界面上报「该集群未接入」——一个不存在的采集缺口。
		{"gke-infra-ansible-01", nil, false, "ansible 机器，不是节点"},
		{"gke-infra-ansible-gitlab-01", nil, false, "gitlab 机器"},
		{"gke-infra-kubernetes-master-01", nil, false, "自建 k8s master，不是 GKE 节点"},

		// 标签是权威判据：名字不像也认。
		// ⚠️ 这条不能删 —— 没有它，标签齐全但名字被改过的节点会被漏判
		{"whatever-01", map[string]string{"goog-gke-node-pool-name": "pool-a"}, true, "有 goog-gke 标签"},

		// 非 gke 前缀且无标签
		{"g32-prod-haproxy-01", nil, false, "普通虚机"},
		// 尾巴形状不对：hex 段不足 8 位
		{"gke-foo-bar-1854a45-28kd", nil, false, "尾巴不是 8 位 hex"},
		// 尾巴形状不对：随机段不足 4 位
		{"gke-foo-bar-1854a453-28k", nil, false, "尾巴随机段不足 4 位"},
	}
	for _, c := range cases {
		got, _ := detectGKENode(c.name, c.labels)
		if got != c.want {
			t.Errorf("detectGKENode(%q) = %v, 期望 %v —— %s", c.name, got, c.want, c.why)
		}
	}
}

func TestDetectGKENodePool(t *testing.T) {
	// 节点池优先取标签；两个标签名都要认（GCP 侧和 k8s 侧各一个写法）
	for _, k := range []string{"goog-gke-node-pool-name", "cloud.google.com/gke-nodepool"} {
		_, pool := detectGKENode("gke-x-y-1854a453-28kd", map[string]string{k: "pool-a"})
		if pool != "pool-a" {
			t.Errorf("标签 %s 没取到节点池，得到 %q", k, pool)
		}
	}
}
