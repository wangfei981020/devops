package handlers

import "testing"

// 三态判定。三种情况在界面上都是"没有 Pod"，
// 判错了不会报错，只会把"我们没数据"说成"上面没东西"（或者反过来）。
func TestNodeLinkState(t *testing.T) {
	cases := []struct {
		name                    string
		found, isK8sNode, stale bool
		want                    string
	}{
		{"查到节点就是已关联", true, true, false, "linked"},
		{"查到节点，即使主机侧没标 k8s 也算关联", true, false, false, "linked"},
		{"标了是节点却查不到 —— 采集缺口", false, true, false, "not_ingested"},
		// 这条是这个测试存在的主要理由
		{"已销毁的节点查不到是正常的，不是缺口", false, true, true, "none"},
		{"本来就不是节点", false, false, false, "none"},
		{"不是节点且已销毁", false, false, true, "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nodeLinkState(c.found, c.isK8sNode, c.stale); got != c.want {
				t.Errorf("nodeLinkState(%v,%v,%v) = %q，期望 %q", c.found, c.isK8sNode, c.stale, got, c.want)
			}
		})
	}
}
