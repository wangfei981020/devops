package engine

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// 用**生产上真实的 7 条规则**跑迁移翻译。
//
// 这些断言全部来自实测踩到的坑：第一版翻译器把这 7 条规则里的
// 调度、级别、阈值字段三样都搞错了，而且没有任何提示 ——
// 界面上看起来配置完整、状态正常，只是永远不按预期告警。
//
// testdata/legacy-rules-prod.json 是旧系统导出格式的原样转录。
func loadProdRules(t *testing.T) []LegacyRule {
	t.Helper()
	raw, err := os.ReadFile("../testdata/legacy-rules-prod.json")
	if err != nil {
		t.Fatalf("读取生产规则样本失败: %v", err)
	}
	var rules []LegacyRule
	if err := json.Unmarshal(raw, &rules); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(rules) != 7 {
		t.Fatalf("期望 7 条生产规则，实际 %d 条", len(rules))
	}
	return rules
}

// cron 调度必须等价翻译。
//
// 7 条规则全是 `*/5 * * * *`（5 分钟）。第一版用 parseDuration 解析，
// Sscanf 失败就回落 60 秒 —— 查询频率放大 5 倍，且悄无声息。
func TestProdRulesScheduleTranslatedExactly(t *testing.T) {
	for _, r := range loadProdRules(t) {
		tr := Translate(r)
		if tr.Draft.IntervalSec != 300 {
			t.Errorf("#%d %s: `%s` 应翻译成 300 秒，实际 %d 秒",
				r.ID, r.Name, r.Schedule, tr.Draft.IntervalSec)
		}
	}
}

// S1 必须映射成 critical。
//
// 生产 7 条全是 S1（界面显示「灾难」）。第一版只认 critical/p0，
// S1 落进 default 变成 warning —— 整批降级，而路由里
// severity=critical 的分支从此不再命中，最该吵醒人的告警走进普通群。
func TestProdRulesSeverityNotDowngraded(t *testing.T) {
	for _, r := range loadProdRules(t) {
		tr := Translate(r)
		if tr.Draft.Severity != "critical" {
			t.Errorf("#%d %s: severity=%q 应映射为 critical，实际 %q",
				r.ID, r.Name, r.Severity, tr.Draft.Severity)
		}
	}
}

// 耗时阈值必须挑到数值字段。
//
// 规则 #8 的提取字段顺序是 [tid, domain, cost_ms, url]，
// 盲取第一个会得到 tid（十六进制事务 ID），翻译成 p95(tid) ≥ 5000 —— 规则等于废掉。
func TestProdThresholdRulePicksNumericField(t *testing.T) {
	for _, r := range loadProdRules(t) {
		if r.RealtimeEnabled != 1 {
			continue
		}
		tr := Translate(r)
		if tr.Draft.Field != "cost_ms" {
			t.Errorf("#%d %s: 阈值字段应挑中 cost_ms，实际 %q（tid 是事务 ID，不是耗时）",
				r.ID, r.Name, tr.Draft.Field)
		}
		if tr.Draft.FieldThreshold != 5000 {
			t.Errorf("#%d: 阈值应为 5000，实际 %v", r.ID, tr.Draft.FieldThreshold)
		}
	}
}

// ignore_values 是降噪配置，丢了就是告警风暴。
//
// #3/#4 各有近 20 个「这些错误码不要告警」。只说一句"路由需人工确认"
// 是不够的 —— 必须把数量和具体值摆出来，否则上线当天没人知道少了什么。
func TestProdIgnoreValuesSurfaced(t *testing.T) {
	want := map[int]string{3: "9007", 4: "0000", 7: "1254"}
	for _, r := range loadProdRules(t) {
		code, ok := want[r.ID]
		if !ok {
			continue
		}
		tr := Translate(r)
		joined := strings.Join(tr.Notes, " ")
		if !strings.Contains(joined, "不告警") || !strings.Contains(joined, code) {
			t.Errorf("#%d %s: 提示里必须列出被忽略的错误码（至少含 %s），实际提示：%v",
				r.ID, r.Name, code, tr.Notes)
		}
	}
}

// 不该给不相关的规则加噪音。
//
// 「没找到耗时字段」这条提示只对耗时阈值规则有意义。
// 放在通用路径上会让 log_absent / log_keyword 也收到一条 ——
// 迁移清单里全是无关提示，人就会开始跳过所有提示，
// 那么真正重要的那几条（ignore_values）也会被一起跳过。
func TestNonThresholdRulesGetNoFieldNoise(t *testing.T) {
	for _, r := range loadProdRules(t) {
		tr := Translate(r)
		if tr.Kind == kindLogField {
			continue
		}
		for _, n := range tr.Notes {
			if strings.Contains(n, "耗时") && strings.Contains(n, "提取字段") {
				t.Errorf("#%d %s（%s）不该收到耗时字段提示：%s", r.ID, r.Name, tr.Kind, n)
			}
		}
	}
}

// 翻译结果的整体形态：全部可迁，但都需要人确认，没有一条能盲目自动上线。
func TestProdRulesOverallVerdict(t *testing.T) {
	var auto, confirm, rewrite int
	for _, r := range loadProdRules(t) {
		switch Translate(r).Verdict {
		case "auto":
			auto++
		case "confirm":
			confirm++
		case "rewrite":
			rewrite++
		}
	}
	if rewrite != 0 {
		t.Errorf("不该有需要重写的规则，实际 %d 条", rewrite)
	}
	if confirm != 7 {
		t.Errorf("7 条生产规则都带人工决策项（多命名空间/路由/@人），应全部 confirm，实际 confirm=%d auto=%d",
			confirm, auto)
	}
}

// 生产 7 条规则里 5 条开着 prometheus_config，static_labels 必须逐条搬过来。
//
// 这条测的是一类静默丢失：翻译器把开关和标签落在 Draft 上，
// 但只要 applyImport 的 INSERT 漏写这两列，导进来的规则就没有指标导出，
// 而预检页面上明明显示"已开启"。预检与结果不一致时，两边单看都是对的。
func TestProdRulesKeepPrometheusLabels(t *testing.T) {
	rules := loadProdRules(t)
	want := map[string]map[string]string{
		"G32 UAT 错误码告警":  {"project": "G32", "env": "UAT"},
		"G32 PROD 错误码告警": {"project": "G32", "env": "PROD"},
		"G33 PROD 错误码告警": {"project": "G33", "env": "PROD"},
		"G33 UAT 数据源告警":  {},
		"G33 PROD 数据源告警": {},
	}
	seen := map[string]bool{}
	for _, l := range rules {
		tr := Translate(l)
		exp, ok := want[l.Name]
		if !ok {
			// 没配 prometheus_config 的那两条不该被打开
			if tr.Draft.MetricsEnabled {
				t.Errorf("%q 没有 prometheus_config，却被开了指标导出", l.Name)
			}
			continue
		}
		seen[l.Name] = true
		if !tr.Draft.MetricsEnabled {
			t.Errorf("%q 旧规则开着指标导出，翻译后丢了", l.Name)
		}
		for k, v := range exp {
			if tr.Draft.MetricsLabels[k] != v {
				t.Errorf("%q 静态标签 %s 期望 %q，实际 %q", l.Name, k, v, tr.Draft.MetricsLabels[k])
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("规则 %q 没在测试数据里找到，用例失去意义", name)
		}
	}
}

// 保留标签不能被搬进去：一条带 instance 的规则会让整份 /metrics
// 被抓取端重命名，PromQL 里再也查不到这个值。
func TestReservedLabelsAreRejected(t *testing.T) {
	l := LegacyRule{
		Name:             "带保留标签的规则",
		Keyword:          "error",
		PrometheusConfig: `{"enabled":true,"static_labels":{"instance":"host-1","env":"prod","2bad":"x"}}`,
	}
	tr := Translate(l)
	if !tr.Draft.MetricsEnabled {
		t.Fatal("指标导出该开着")
	}
	if _, bad := tr.Draft.MetricsLabels["instance"]; bad {
		t.Error("instance 是保留标签，不该被搬进去")
	}
	if _, bad := tr.Draft.MetricsLabels["2bad"]; bad {
		t.Error("数字开头的标签名非法，不该被搬进去")
	}
	if tr.Draft.MetricsLabels["env"] != "prod" {
		t.Error("合法标签应该保留")
	}
	// 丢掉的必须说出来，否则用户以为全搬过来了
	joined := strings.Join(tr.Notes, " ")
	if !strings.Contains(joined, "instance") || !strings.Contains(joined, "2bad") {
		t.Errorf("被丢弃的标签必须出现在 notes 里，实际 notes=%v", tr.Notes)
	}
}
