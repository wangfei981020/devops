package n9e

import "testing"

// 这一组守的是 OPSCMDB-031 的告警页四条（P1-30 / P2-34 / P0-13）。
//
// 取值全部来自 2026-08-17 生产实测（279 条活跃告警），不是臆造：
// 「cluster 字段装的是数据源名」这种事，只有拿真实数据才发现得了。
func TestClusterNameIsNotDatasource(t *testing.T) {
	// ⚠️ 顶层 Cluster 是 "VictoriaMetrics" —— 实测 279 条全是这个值。
	// 照它渲染「集群」列，整页会显示成同一个集群，等于这一列没有信息量
	e := AlertEvent{
		Cluster: "VictoriaMetrics",
		TagsMap: map[string]string{"cluster": "infra-k8s-cluster-01", "env": "prod"},
	}
	if got := e.ClusterName(); got != "infra-k8s-cluster-01" {
		t.Errorf("ClusterName()=%q，期望取 tags.cluster", got)
	}

	// 取不到真集群时**必须返回空**，不能退回数据源名冒充。
	// 冒充的后果是界面显示了一个看起来合理、实际不存在的集群名
	bare := AlertEvent{Cluster: "VictoriaMetrics", TagsMap: map[string]string{"env": "prod"}}
	if got := bare.ClusterName(); got != "" {
		t.Errorf("取不到集群时返回了 %q，应该是空串（别拿数据源名冒充集群）", got)
	}
}

func TestShortRuleNameStripsSeverity(t *testing.T) {
	cases := map[string]string{
		// 实测的两个真实规则名
		"HostHighDiskUsage - S3": "HostHighDiskUsage",
		"CertExpireDays - S3":    "CertExpireDays",
		"Foo - S1":               "Foo",
		"Foo-S2":                 "Foo",
		"Foo - s3":               "Foo - s3", // 小写不剥：夜莺的标记是大写 S
		// ⚠️ 只剥**尾部**。规则名里其它含 S3 的字样（AWS S3 相关规则）不能被误伤
		"S3 Bucket Public":    "S3 Bucket Public",
		"S3BucketPublic - S2": "S3BucketPublic",
		"NoSuffix":            "NoSuffix",
		"":                    "",
	}
	for in, want := range cases {
		e := AlertEvent{RuleName: in}
		if got := e.ShortRuleName(); got != want {
			t.Errorf("ShortRuleName(%q)=%q，期望 %q", in, got, want)
		}
	}
	// 整个名字就是后缀这种畸形数据，宁可原样显示也不要显示空
	if got := (AlertEvent{RuleName: " - S1"}).ShortRuleName(); got != " - S1" {
		t.Errorf("畸形名返回了 %q，应该原样保留而不是变成空", got)
	}
}

// Object() 的优先级：业务对象标签 > annotations > tags.instance。
//
// 这个顺序不是审美问题。实测视频流告警：
//
//	tags.instance        = 10.170.96.153:8080     ← 一个 exporter 上几十条流，全一样
//	annotations.instance = svc-source_..._N7103   ← 这才是出问题的那条流
//
// 按 tags.instance 取，列表会出现 47 行长得一模一样的告警（P0-13）。
func TestObjectPrefersBusinessIdentity(t *testing.T) {
	// 域名告警：真正要看的域名在 tags.domain 里，
	// instance 是 domain-exporter 自己的地址
	dom := AlertEvent{
		TagsMap: map[string]string{"domain": "uat-prometheus.slileisure.com", "instance": "172.16.14.16:8080"},
	}
	if got := dom.Object(); got != "uat-prometheus.slileisure.com" {
		t.Errorf("域名告警 Object()=%q，期望取 tags.domain（不是 exporter 地址）", got)
	}

	// 视频流告警：annotations.instance 优先于 tags.instance
	stream := AlertEvent{
		TagsMap:     map[string]string{"instance": "10.170.96.153:8080"},
		Annotations: map[string]string{"instance": "svc-source_x_N7103"},
	}
	if got := stream.Object(); got != "svc-source_x_N7103" {
		t.Errorf("流告警 Object()=%q，期望取 annotations.instance", got)
	}

	// 都没有才退回 exporter 地址 —— 有总比没有强
	fallback := AlertEvent{TagsMap: map[string]string{"instance": "10.170.96.193:9100"}}
	if got := fallback.Object(); got != "10.170.96.193:9100" {
		t.Errorf("兜底 Object()=%q", got)
	}

	// target_ident 非空时最优先（那是夜莺自己认定的对象）
	ident := AlertEvent{TargetIdent: "node-7", TagsMap: map[string]string{"domain": "x.com"}}
	if got := ident.Object(); got != "node-7" {
		t.Errorf("target_ident 应最优先，实际 %q", got)
	}
}

// BizTags 要把已经在 Object 里体现的标识过滤掉，避免重复占地方。
func TestBizTagsDropsRedundant(t *testing.T) {
	e := AlertEvent{TagsMap: map[string]string{
		"instance": "1.2.3.4:9100", "hostname": "h1", "env": "prod", "team": "app", "empty": "",
	}}
	got := e.BizTags()
	if _, ok := got["instance"]; ok {
		t.Error("instance 应该被过滤（已在 Object 里体现）")
	}
	if _, ok := got["empty"]; ok {
		t.Error("空值标签不该保留")
	}
	if got["env"] != "prod" || got["team"] != "app" {
		t.Errorf("业务标签被丢了：%v", got)
	}
}
