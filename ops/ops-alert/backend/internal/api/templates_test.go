package api

import (
	"strings"
	"testing"
)

// 模板要能重现**生产上真实在跑的**那几条规则。
// 用假想的输入测模板只能证明代码不崩，证明不了它有用。

// 错误码模板 → 对应生产的 #4 G32 PROD 错误码告警。
func TestErrorCodeTemplateMatchesProdRule(t *testing.T) {
	kind, query, spec, _, _, extra, err := buildFromTemplate("error_code", map[string]any{
		"namespaces":        []any{"g32-wallet", "g32-openapi"},
		"container_exclude": `telegram.*|bi-.*`,
		"ignore_codes":      []any{"9007", "1254"},
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if kind != "log_keyword" {
		t.Errorf("kind = %q，应为 log_keyword", kind)
	}
	// 多命名空间必须合成一条查询，而不是留给调用方发 N 个请求
	if !strings.Contains(query, `namespace=~"g32-wallet|g32-openapi"`) {
		t.Errorf("多命名空间没合成正则选择器：%s", query)
	}
	if !strings.Contains(query, `container!~"telegram.*|bi-.*"`) {
		t.Errorf("排除容器没进查询：%s", query)
	}
	// 这是模板存在的主要理由：把「不告警的错误码」落成查询条件，
	// 而不是留在某个迁移时会被丢掉的路由配置里
	if !strings.Contains(query, `9007|1254`) {
		t.Errorf("忽略的错误码没写进查询，迁移时就会丢：%s", query)
	}
	if extra["ignored_codes"] == nil {
		t.Error("应回显被忽略的错误码，让人在保存前看见")
	}
	if spec["extract"] == nil {
		t.Error("错误码场景必须提取 Code/Tid，否则通知模板里没有变量可用")
	}
}

// 断流模板 → 对应生产的 #2/#6 数据源告警。
func TestHeartbeatTemplateMatchesProdRule(t *testing.T) {
	kind, query, spec, groupBy, lookback, _, err := buildFromTemplate("log_heartbeat", map[string]any{
		"namespace":           "g32-game",
		"container_pattern":   ".*resource-backend",
		"log_pattern":         "Link.* timestamp.* Round.*",
		"expected_containers": []any{"baccarat-resource-backend", "roulette-resource-backend"},
		"silence_minutes":     "30",
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if kind != "log_absent" {
		t.Errorf("kind = %q，应为 log_absent", kind)
	}
	if lookback != 1800 {
		t.Errorf("30 分钟应转成 1800 秒，实际 %d", lookback)
	}
	if len(groupBy) != 1 || groupBy[0] != "container" {
		t.Errorf("断流要按容器分组，实际 %v", groupBy)
	}
	if spec["expected_groups"] == nil {
		t.Error("必须带期望分组，否则完全不出日志的容器永远不会被发现")
	}
	if !strings.Contains(query, `container=~".*resource-backend"`) {
		t.Errorf("容器匹配没进查询：%s", query)
	}
}

// 期望清单是断流检测的必要条件，不能让它建成一条永远不告警的规则。
func TestHeartbeatRejectsEmptyExpected(t *testing.T) {
	_, _, _, _, _, _, err := buildFromTemplate("log_heartbeat", map[string]any{
		"namespace":         "g32-game",
		"container_pattern": ".*",
		"log_pattern":       "x",
	})
	if err == nil {
		t.Fatal("没有期望容器清单时必须报错：分组来自查询结果，" +
			"彻底不出日志的容器不会出现在结果里，规则会永远不触发")
	}
}

// 耗时模板 → 对应生产的 #8 电子钱包性能监控。
func TestLatencyTemplateMatchesProdRule(t *testing.T) {
	kind, _, spec, _, _, _, err := buildFromTemplate("latency_threshold", map[string]any{
		"namespace":    "g32-wallet",
		"container":    "wallet-client-backend",
		"log_pattern":  "调用站点交易接口",
		"cost_pattern": `running time\s*=\s*(\d+)\s*ms`,
		"threshold_ms": "5000",
		"agg":          "p95",
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if kind != "log_field_threshold" {
		t.Errorf("kind = %q", kind)
	}
	if spec["field"] != "cost_ms" || spec["agg"] != "p95" {
		t.Errorf("字段/聚合不对: field=%v agg=%v", spec["field"], spec["agg"])
	}
	if spec["field_threshold"] != 5000 {
		t.Errorf("阈值 = %v，应为 5000", spec["field_threshold"])
	}
}

// 没有捕获组的正则取不出数值 —— 规则能建成功但永远算不出结果，
// 属于"看着正常其实废掉"的那一类，必须在保存前拦下。
func TestLatencyRejectsPatternWithoutCaptureGroup(t *testing.T) {
	_, _, _, _, _, _, err := buildFromTemplate("latency_threshold", map[string]any{
		"namespace": "ns", "container": "c", "log_pattern": "x",
		"cost_pattern": `running time \d+ ms`, // 没有 ()
		"threshold_ms": "5000",
	})
	if err == nil {
		t.Fatal("耗时正则没有捕获组时必须报错")
	}
}

// 用户输入要转义后再拼进 LogQL：一个引号就能改变查询语义。
func TestTemplateEscapesUserInput(t *testing.T) {
	_, query, _, _, _, _, err := buildFromTemplate("log_heartbeat", map[string]any{
		"namespace":           `g32"} |= "secret`,
		"container_pattern":   ".*",
		"log_pattern":         "x",
		"expected_containers": []any{"c1"},
	})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	// 转义后原来的引号不再是结构性的引号
	if strings.Contains(query, `g32"} |= "secret`) {
		t.Errorf("用户输入没被转义，查询语义可被改写：%s", query)
	}
}

// 错误码只允许字母数字：它会被拼进正则，特殊字符能改变匹配范围
// （比如 `.*` 会把所有错误码都排除掉 —— 规则从此永不告警）。
func TestErrorCodeRejectsRegexInjection(t *testing.T) {
	_, _, _, _, _, _, err := buildFromTemplate("error_code", map[string]any{
		"namespaces":   []any{"ns"},
		"ignore_codes": []any{".*"},
	})
	if err == nil {
		t.Fatal("错误码含正则元字符时必须报错，否则 .* 会排除掉全部告警")
	}
}
