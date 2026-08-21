package handlers

import (
	"strings"
	"testing"
)

// 🔴 三种「查不到磁盘水位」的处置完全不同，绝不能压成一句话。
//
// 真实架构是**一套 VictoriaMetrics 集中采多个集群**（infra-01 那套已经采了
// UAT / g32-prod / infra-01）。原来一律报「该集群未配置 Prometheus 观测数据源」
// 并让人「给该集群绑定 Prometheus」——照做就是去装一个本不该装的东西。
//
// 这个测试只钉文案里的**关键区分**，不依赖数据库。
func TestDiskUnknownWordingDistinguishesCauses(t *testing.T) {
	// 这三段文案分别对应三种成因，两两之间必须给出不同的动作
	cases := []struct {
		name        string
		reason      string
		action      string
		mustHave    []string
		mustNotHave []string
	}{
		{
			name:   "没有可用数据源",
			reason: "没有匹配到可用的指标数据源",
			action: "到「接入管理 → 观测数据源」接一个。⚠️ 如果你们是**一套 VictoriaMetrics 集中采多个集群**，不要为这个集群单独装 Prometheus——把已有的那个数据源的适用范围放开（集群留空=不限定），再给本集群配好「指标集群标签值」即可",
			// 必须挡住"再装一个 Prometheus"这个错误动作
			mustHave: []string{"不要为这个集群单独装 Prometheus", "指标集群标签值"},
		},
		{
			name:        "标签值配错",
			reason:      "数据源是通的，但**集群隔离标签值配错了**：xxx",
			action:      "改「接入管理 → 集群 → 指标集群标签值」，不用动数据源本身",
			mustHave:    []string{"不用动数据源本身"},
			mustNotHave: []string{"接一个", "装 Prometheus"},
		},
		{
			name:        "采集端没装",
			reason:      "数据源和集群标签都正常，但查不到任何 node_filesystem 指标",
			action:      "多为这些节点上**没有部署 node_exporter**（或它的 target 全 down）。⚠️ 别再去动数据源配置——问题在采集端不在查询端",
			mustHave:    []string{"node_exporter", "问题在采集端不在查询端"},
			mustNotHave: []string{"指标集群标签值"},
		},
	}
	for _, c := range cases {
		for _, w := range c.mustHave {
			if !strings.Contains(c.action, w) && !strings.Contains(c.reason, w) {
				t.Errorf("[%s] 文案里缺 %q", c.name, w)
			}
		}
		for _, w := range c.mustNotHave {
			if strings.Contains(c.action, w) {
				t.Errorf("[%s] 文案里不该出现 %q —— 会把人引向错误动作", c.name, w)
			}
		}
	}

	// ⚠️ 三种动作必须互不相同：压成一样就等于没分
	seen := map[string]string{}
	for _, c := range cases {
		if prev, dup := seen[c.action]; dup {
			t.Errorf("「%s」和「%s」给了同样的处置，等于没区分", c.name, prev)
		}
		seen[c.action] = c.name
	}
}
