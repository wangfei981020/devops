package engine

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"ops-alert-backend/internal/store"
)

// 旧规则适配器：把 opsplatform-alert 的规则翻译成新模型。
//
// # 三条约定
//
//  1. **不动旧系统**：只读它导出的 JSON（或只读连它的库），旧系统全程原样运行。
//  2. **不做静默转换**：翻译不了的字段进「需人工确认」清单，不猜、不省略。
//     悄悄丢掉一个 route_config 的后果是上线后告警发错群，而没人知道为什么。
//  3. **翻译完先回放**：与旧系统的告警流水逐条对齐后再双跑。
//
// 换一个 Parse 实现就能迁 Alertmanager / 夜莺 / Zabbix 的客户——
// 这套路径本身就是卖给新客户时的迁移工具。

// LegacyRule 是旧系统 alert_rules 表的一行（只列翻译要用到的字段）。
type LegacyRule struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	DataSourceType   string `json:"data_source_type"` // es / loki
	ESIndex          string `json:"es_index"`
	Schedule         string `json:"schedule"`
	TimeRange        string `json:"time_range"`
	QueryDSL         string `json:"query_dsl"`
	Keyword          string `json:"keyword"`
	LogQL            string `json:"logql"`
	FilterFields     string `json:"filter_fields"`
	ExtractFields    string `json:"extract_fields"`
	MessageTitle     string `json:"message_title"`
	AlertMode        string `json:"alert_mode"` // found / not_found
	RecoveryEnabled  int    `json:"recovery_enabled"`
	Severity         string `json:"severity"`
	GroupBy          string `json:"group_by"`
	ExpectedGroups   string `json:"expected_groups"`
	AlertInterval    string `json:"alert_interval"`
	DedupField       string `json:"dedup_field"`
	MaxAlerts        int    `json:"max_alerts"`
	RouteConfig      string `json:"route_config"`
	Namespaces       string `json:"namespaces"`
	LabelFilters     string `json:"label_filters"`
	AtUsers          string `json:"at_users"`
	RealtimeEnabled  int    `json:"realtime_enabled"`
	ThresholdMs      int    `json:"threshold_ms"`
	ReportEnabled    int    `json:"report_enabled"`
	PrometheusConfig string `json:"prometheus_config"`
	Status           int    `json:"status"`
}

// Translation 是一条规则的翻译结果。
type Translation struct {
	LegacyID int       `json:"legacy_id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Verdict  string    `json:"verdict"` // auto / confirm / rewrite
	Notes    []string  `json:"notes"`
	Draft    ruleDraft `json:"draft"`
}

type ruleDraft struct {
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	Query          string            `json:"query"`
	IntervalSec    int               `json:"interval_sec"`
	LookbackSec    int               `json:"lookback_sec"`
	Threshold      int               `json:"threshold"`
	ForPeriods     int               `json:"for_periods"`
	GroupBy        []string          `json:"group_by"`
	Severity       string            `json:"severity"`
	Labels         map[string]string `json:"labels"`
	MaxEvents      int               `json:"max_events"`
	Expected       []string          `json:"expected_groups"`
	NotifyResolved bool              `json:"notify_resolved"`
	Extract        []extractRule     `json:"extract,omitempty"`
	MessageTitle   string            `json:"message_title,omitempty"`
	Field          string            `json:"field,omitempty"`
	FieldPattern   string            `json:"field_pattern,omitempty"`
	Agg            string            `json:"agg,omitempty"`
	FieldThreshold float64           `json:"field_threshold,omitempty"`
	MetricsEnabled bool              `json:"metrics_enabled,omitempty"`
	MetricsLabels  map[string]string `json:"metrics_labels,omitempty"`
}

// legacyPrometheus 旧系统 prometheus_config 的形状：
// {"enabled":true,"static_labels":{"project":"G32","env":"PROD"}}
type legacyPrometheus struct {
	Enabled      bool              `json:"enabled"`
	StaticLabels map[string]string `json:"static_labels"`
}

// decodeExtract 解析旧系统的 extract_fields。
// 结构与新模型一致（name/path/pattern），所以是直接搬而不是重写。
func decodeExtract(raw string) []extractRule {
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []extractRule
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// Translate 把一条旧规则翻译成草稿，并给出结论。
func Translate(l LegacyRule) Translation {
	t := Translation{LegacyID: l.ID, Name: l.Name, Verdict: "auto"}

	// 场景判定：not_found 是心跳缺失；带实时耗时阈值的是字段阈值；其余是关键词。
	switch {
	case l.AlertMode == "not_found":
		t.Kind = kindLogAbsent
	case l.RealtimeEnabled == 1 && l.ThresholdMs > 0:
		t.Kind = kindLogField
		t.Verdict = "confirm"
	default:
		t.Kind = kindLogKeyword
	}

	query := l.LogQL
	if query == "" {
		query = l.QueryDSL
	}
	if query == "" {
		query = l.Keyword
	}
	if query == "" {
		t.Notes = append(t.Notes, "旧规则没有可识别的查询语句（keyword / query_dsl / logql 均为空）")
		t.Verdict = "rewrite"
	}
	// 多命名空间：旧系统用单独字段，新模型直接写进查询选择器。
	if ns := decodeJSONStrings(l.Namespaces); len(ns) > 0 {
		t.Notes = append(t.Notes, fmt.Sprintf("原多命名空间字段（%s）已并入查询选择器，请确认语法",
			strings.Join(ns, ", ")))
		if t.Verdict == "auto" {
			t.Verdict = "confirm"
		}
	}
	if l.LabelFilters != "" {
		t.Notes = append(t.Notes, "label_filters 需并入查询表达式："+l.LabelFilters)
		if t.Verdict == "auto" {
			t.Verdict = "confirm"
		}
	}
	if l.RouteConfig != "" && l.RouteConfig != "{}" {
		// 路由在新模型里是独立对象，多条规则共用。
		// 重复的路由要合并成几条，这个决定必须由人做。
		t.Notes = append(t.Notes, "按字段值路由已升为独立的路由树对象，需人工确认合并方案")
		t.Verdict = "confirm"

		// ⚠️ ignore_values 是**降噪配置**，不是路由细节。
		// 生产规则里有近 20 个「这些错误码不要告警」，迁移时若只说一句
		// "路由需人工确认"，这些忽略项会全部失效 —— 上线当天就是告警风暴，
		// 而且没人会想到是迁移丢了它。所以要把数量和内容原样摆出来。
		var rc struct {
			RouteField   string   `json:"route_field"`
			IgnoreValues []string `json:"ignore_values"`
		}
		if err := json.Unmarshal([]byte(l.RouteConfig), &rc); err == nil && len(rc.IgnoreValues) > 0 {
			uniq := map[string]bool{}
			for _, v := range rc.IgnoreValues {
				uniq[v] = true
			}
			t.Notes = append(t.Notes, fmt.Sprintf(
				"🔴 有 %d 个「不告警」的 %s 值（去重后 %d 个）：%s。"+
					"新模型里要落成查询排除条件或抑制规则，**没搬过去就会全部开始告警**",
				len(rc.IgnoreValues), rc.RouteField, len(uniq), strings.Join(rc.IgnoreValues, ",")))
		}
	}
	if l.AtUsers != "" && l.AtUsers != "[]" {
		t.Notes = append(t.Notes, "@人由 open_id 改为平台用户映射，需要建立映射关系")
		if t.Verdict == "auto" {
			t.Verdict = "confirm"
		}
	}
	if l.ReportEnabled == 1 {
		t.Notes = append(t.Notes,
			"旧规则开了每日报表。新系统的日报是**租户级的一份**（在「日报」页配置），"+
				"不再每条规则一封 —— 导入后需要去那里打开开关并选投递渠道")
		t.Verdict = "confirm"
	}

	// 字段提取：旧格式 [{name,path,pattern}] 与新模型结构一致，可直接搬。
	// 这是很多旧规则的核心——通知模板里的变量、按字段值路由都靠它。
	extract := decodeExtract(l.ExtractFields)

	// 实时耗时阈值：旧系统用 realtime_enabled + threshold_ms（毫秒），
	// 新模型统一成"提数值 + 聚合 + 阈值"。取第一个提取字段作为数值来源——
	// 旧系统本来就是这么用的（extract 出耗时字段再比阈值）。
	// ⚠️ 不能盲取 extract[0]。生产那条性能监控规则的提取字段顺序是
	// [tid, domain, cost_ms, url] —— 取第一个会得到 **tid（事务 ID，十六进制）**，
	// 翻译成 "p95(tid) ≥ 5000"，对一串 ID 求分位数，规则等于废掉，
	// 而界面上它看起来配置完整、状态正常。
	// 按字段名挑最像"数值/耗时"的那个，挑不出来再退回第一个并要求人工指定。
	fieldName, fieldPattern := "", ""
	fieldGuessed := false
	if len(extract) > 0 {
		for _, e := range extract {
			n := strings.ToLower(e.Name)
			if strings.Contains(n, "cost") || strings.Contains(n, "duration") ||
				strings.Contains(n, "elapsed") || strings.Contains(n, "latency") ||
				strings.HasSuffix(n, "_ms") || strings.HasSuffix(n, "_time") {
				fieldName, fieldPattern = e.Name, e.Pattern
				break
			}
		}
		if fieldName == "" {
			// 只记下回退结果，是否成问题由下面的 kindLogField 分支判断：
			// log_absent / log_keyword 根本不需要数值字段，在这里报警等于制造噪音
			fieldName, fieldPattern = extract[0].Name, extract[0].Pattern
			fieldGuessed = true
		}
	}

	// 调度：旧的是 cron，新模型是固定间隔
	intervalSec := 60
	if sec, ok := parseCronInterval(l.Schedule); ok {
		intervalSec = sec
	} else if sec := parseDuration(l.Schedule, 0); sec > 0 {
		intervalSec = sec // 本来就是 5m/30s 这类写法
	} else if l.Schedule != "" {
		t.Notes = append(t.Notes, fmt.Sprintf(
			"调度 %q 是带具体时刻的 cron，新模型只支持固定间隔，暂按 60 秒填入，必须人工确认", l.Schedule))
		t.Verdict = "confirm"
	}

	// 级别：无法识别的不能猜
	sev, sevOK := normalizeSeverity(l.Severity)
	if !sevOK {
		t.Notes = append(t.Notes, fmt.Sprintf(
			"级别 %q 无法识别，暂按 warning 填入。⚠️ 若它在旧系统里是最高档，"+
				"迁过来会被降级，路由中 severity=critical 的分支将不再命中", l.Severity))
		t.Verdict = "confirm"
	}

	// ⚠️ 这里是**整体赋值**，会覆盖之前对 t.Draft 任何字段的修改。
	// 所有往 Draft 上写字段的代码都必须排在这一行**之后**。
	// 指标导出就在这里栽过：翻译逻辑写在上面，被这一行整个抹掉，
	// 而预检页面显示"已开启"——只有测试发现得了。
	t.Draft = ruleDraft{
		Name:           l.Name,
		Kind:           t.Kind,
		Query:          query,
		IntervalSec:    intervalSec,
		LookbackSec:    parseDuration(l.TimeRange, 300),
		Threshold:      1,
		ForPeriods:     1,
		GroupBy:        decodeJSONStrings(l.GroupBy),
		Severity:       sev,
		Labels:         map[string]string{"imported_from": fmt.Sprintf("opsplatform-alert#%d", l.ID)},
		MaxEvents:      defaultIntVal(l.MaxAlerts, 20),
		Expected:       decodeJSONStrings(l.ExpectedGroups),
		NotifyResolved: l.RecoveryEnabled == 1,
		Extract:        extract,
		MessageTitle:   l.MessageTitle,
	}

	// 指标导出：static_labels 直接搬过来。
	//
	// ⚠️ 指标**名字变了**（旧系统一套命名，这里是 opsalert_rule_*），
	// 所以看板上的 PromQL 必须改写 —— 标签迁过来了不等于看板能直接用。
	// 这条 note 不能省：静默改名的表现是"迁完之后看板全空"，
	// 而每一层看上去都正常。
	if l.PrometheusConfig != "" && l.PrometheusConfig != "{}" {
		var pc legacyPrometheus
		if err := json.Unmarshal([]byte(l.PrometheusConfig), &pc); err != nil {
			t.Notes = append(t.Notes, "prometheus_config 解析失败，指标导出未迁移："+err.Error())
			t.Verdict = "confirm"
		} else if pc.Enabled {
			t.Draft.MetricsEnabled = true
			// 保留标签必须在这里挡掉：带着 job/instance 存进去，
			// 之后整份 /metrics 会因为一条规则解析失败而全部消失
			kept := map[string]string{}
			for k, v := range pc.StaticLabels {
				if err := validateLabelName(k); err != nil {
					t.Notes = append(t.Notes, "静态标签 "+k+" 未迁移："+err.Error())
					t.Verdict = "confirm"
					continue
				}
				kept[k] = v
			}
			if len(kept) > 0 {
				t.Draft.MetricsLabels = kept
			}
			t.Notes = append(t.Notes,
				"已开启指标导出并保留静态标签。⚠️ 指标名与旧系统不同（新名字是 "+
					"opsalert_rule_hits_total / opsalert_rule_runs_total 等），"+
					"依赖旧指标名的看板和二次告警必须改写 PromQL")
			t.Verdict = "confirm"
		}
	}
	if t.Kind == kindLogField {
		t.Draft.Field = fieldName
		t.Draft.FieldPattern = fieldPattern
		t.Draft.Agg = "p95"
		t.Draft.FieldThreshold = float64(l.ThresholdMs)
		if fieldName == "" {
			t.Notes = append(t.Notes, "耗时阈值规则没有可用的提取字段，需人工指定 spec.field")
			t.Verdict = "rewrite"
		} else if fieldGuessed {
			t.Notes = append(t.Notes, fmt.Sprintf(
				"🔴 没有名字像耗时的提取字段，暂用第一个 %q。它很可能不是数值字段——"+
					"对非数值求分位数会让这条规则**永远不触发**，请人工指定 spec.field", fieldName))
			t.Verdict = "confirm"
		} else {
			// 有提取字段就能自动翻译，不必再让人重填
			t.Notes = append(t.Notes,
				fmt.Sprintf("耗时阈值 %dms 已翻译为 p95(%s) ≥ %d，单位需确认（旧系统按毫秒）",
					l.ThresholdMs, fieldName, l.ThresholdMs))
		}
	}
	if t.Kind == kindLogAbsent {
		// 缺失检测：阈值 1 表示"至少要有 1 条"，低于它就是缺失。
		t.Draft.Threshold = 1
	}
	// alert_interval=once 由事件生命周期天然表达：
	// 活跃事件不重复通知，恢复后再次发生才是新事件。
	if l.AlertInterval == "once" {
		t.Notes = append(t.Notes, "alert_interval=once 由事件生命周期表达：把路由的重复通知间隔设为 0")
	}
	return t
}

// ImportSummary 是一次导入的汇总。
type ImportSummary struct {
	Total   int           `json:"total"`
	Auto    int           `json:"auto"`
	Confirm int           `json:"confirm"`
	Rewrite int           `json:"rewrite"`
	Items   []Translation `json:"items"`
}

// Preflight 预检：只翻译不写库，让人先看清单。
func Preflight(rules []LegacyRule) ImportSummary {
	sum := ImportSummary{Total: len(rules)}
	for _, l := range rules {
		t := Translate(l)
		switch t.Verdict {
		case "auto":
			sum.Auto++
		case "confirm":
			sum.Confirm++
		default:
			sum.Rewrite++
		}
		sum.Items = append(sum.Items, t)
	}
	return sum
}

// Import 把翻译结果写库。只导入 verdict=auto 的，其余留给人确认后再单独建。
//
// 导入的规则一律**先停用**：直接启用意味着几十条没验证过的规则立刻开始发告警，
// 而其中一部分翻译得未必对。
// Import 把旧规则写进本租户。
//
//	only 非空时**只导这几条**（按旧系统的规则 ID）。界面上是逐条勾选的：
//	7 条规则里往往先只导 UAT 的两条试水，全量 include_confirm 开关做不到这件事。
//	only 为空时退回按 includeConfirm 整批导入（供脚本/API 调用方用）。
//
// ⚠️ 导入的规则一律**停用**入库。旧系统的查询语句在新数据源上不一定等价
// （多命名空间已并进选择器、label_filters 还要人工合并），
// 直接启用等于把一批未经验证的查询放上生产。
func Import(sc *store.Scoped, dsID int64, rules []LegacyRule, includeConfirm bool, only []int) (int, error) {
	pick := map[int]bool{}
	for _, id := range only {
		pick[id] = true
	}
	n := 0
	for _, l := range rules {
		if len(pick) > 0 && !pick[l.ID] {
			continue
		}
		t := Translate(l)
		// 逐条勾选时，人已经看过每条的注意事项了，不再用 includeConfirm 二次拦截；
		// rewrite 仍然拦住 —— 它没有可用的草稿，导进去也是坏的
		if t.Verdict == "rewrite" || (len(pick) == 0 && t.Verdict == "confirm" && !includeConfirm) {
			continue
		}
		spec, _ := json.Marshal(map[string]any{
			"query":           t.Draft.Query,
			"expected_groups": t.Draft.Expected,
			"extract":         t.Draft.Extract,
			"message_title":   t.Draft.MessageTitle,
			"field":           t.Draft.Field,
			"field_pattern":   t.Draft.FieldPattern,
			"agg":             t.Draft.Agg,
			"field_threshold": t.Draft.FieldThreshold,
		})
		groupBy, _ := json.Marshal(t.Draft.GroupBy)
		labels, _ := json.Marshal(t.Draft.Labels)
		// ⚠️ 新增字段必须同时加进这里。翻译阶段填了 draft 而写库时漏掉，
		// 表现是"预检显示会带上，导进来却没有"——预检和结果不一致是最难查的一类，
		// 因为两边各自看都是对的。metrics_* 就差点这样漏掉。
		var metricsLabels any
		if len(t.Draft.MetricsLabels) > 0 {
			metricsLabels, _ = json.Marshal(t.Draft.MetricsLabels)
		}
		_, err := sc.Insert(`INSERT INTO rules (tenant_id, name, kind, datasource_id, spec,
				interval_sec, lookback_sec, threshold, for_periods, group_by, severity, labels,
				notify_resolved, max_events, enabled, created_by, metrics_enabled, metrics_labels)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 'importer', ?, ?)`,
			t.Draft.Name, t.Draft.Kind, dsID, spec, t.Draft.IntervalSec, t.Draft.LookbackSec,
			t.Draft.Threshold, t.Draft.ForPeriods, groupBy, t.Draft.Severity, labels,
			boolToInt(t.Draft.NotifyResolved), t.Draft.MaxEvents,
			boolToInt(t.Draft.MetricsEnabled), metricsLabels)
		if err != nil {
			// 重名等冲突跳过而不是整批失败：导入是可以多次执行的，
			// 整批回滚会让人不知道到底进去了几条。
			if isDuplicate(err) {
				continue
			}
			return n, err
		}
		n++
	}
	return n, nil
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

// parseDuration 解析旧系统的 "5m" / "1h" / "30s" 格式。
// 认不出来时回落到默认值并保持保守——猜错周期会让规则跑得过密或过疏。
// parseCronInterval 把 cron 表达式转成固定间隔（秒）。
//
// ⚠️ 旧系统的 schedule 存的是 cron，而新模型只有「每隔 N 秒」。
// 原来直接丢给 parseDuration，它 Sscanf 失败就返回默认值 60 ——
// 于是 `*/5 * * * *`（5 分钟）静默变成 60 秒，**查询频率放大 5 倍**；
// `0 9 * * *`（每天 9 点）更糟，变成每分钟跑一次。
// 7 条生产规则全是 `*/5 * * * *`，整批都会打偏。
//
// 只有 `*/N * * * *` 这种纯间隔型能等价翻译；带具体时刻的（每天 9 点）
// 在新模型里无法表达，必须让人知道，返回 ok=false。
func parseCronInterval(expr string) (sec int, ok bool) {
	f := strings.Fields(strings.TrimSpace(expr))
	if len(f) < 5 {
		return 0, false
	}
	// 秒级 cron（6 段，如 "0 1 0 * * *"）先去掉秒位再判分钟位
	if len(f) == 6 {
		f = f[1:]
	}
	min, rest := f[0], f[1:]
	for _, x := range rest {
		if x != "*" {
			return 0, false // 限定了小时/日/月/周 —— 不是固定间隔
		}
	}
	if strings.HasPrefix(min, "*/") {
		n := 0
		if _, err := fmt.Sscanf(min, "*/%d", &n); err == nil && n > 0 {
			return n * 60, true
		}
	}
	if min == "*" {
		return 60, true
	}
	return 0, false
}

func parseDuration(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	var n int
	var unit string
	if _, err := fmt.Sscanf(s, "%d%s", &n, &unit); err != nil || n <= 0 {
		return def
	}
	switch unit {
	case "s":
		return n
	case "m":
		return n * 60
	case "h":
		return n * 3600
	case "d":
		return n * 86400
	}
	return def
}

// normalizeSeverity 把旧系统的级别翻成新模型的三档。
//
// ⚠️ 生产用的是 **S1/S2/S3** 这套（界面上 S1 显示为「灾难」），
// 而这里原本只认 critical/info，S1 会走到 default 被兜底成 warning ——
// 7 条生产规则全是 S1，迁过来会**整批降级**。
// 后果不是显示难看：路由里 `severity=critical` 的分支从此不再命中，
// 最该吵醒人的那批告警会安静地走进普通群。
//
// 兜底改成 warning + 由调用方记一条 note：无法识别的级别必须被人看见，
// 而不是猜一个看起来合理的值。
func normalizeSeverity(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "紧急", "灾难", "p0", "s1":
		return "critical", true
	case "warning", "warn", "告警", "p1", "p2", "s2":
		return "warning", true
	case "info", "提示", "p3", "s3":
		return "info", true
	case "":
		return "warning", true
	default:
		return "warning", false
	}
}

func decodeJSONStrings(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func defaultIntVal(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ = sql.ErrNoRows

// validateLabelName 校验 Prometheus 标签名。
//
// 与 internal/api 里的 ValidateMetricLabels 是同一套规则，但这里不能引用它：
// engine 不依赖 api（反过来才对）。两处都改的风险由 importer_prod_test 覆盖。
func validateLabelName(k string) error {
	if !labelNameRe.MatchString(k) {
		return fmt.Errorf("%q 不是合法的 Prometheus 标签名（只能字母数字下划线，不能数字开头）", k)
	}
	switch k {
	case "job", "instance", "__name__":
		return fmt.Errorf("%q 是 Prometheus 保留标签，会被抓取端重命名成 exported_%s", k, k)
	}
	if strings.HasPrefix(k, "__") {
		return fmt.Errorf("%q 以双下划线开头，那是 Prometheus 的内部命名空间", k)
	}
	return nil
}

var labelNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
