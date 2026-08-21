package engine

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"ops-alert-backend/datasource"
)

// 本文件是四类日志场景各自的判定语义，以及字段提取与模板渲染。
//
// # 为什么必须按 kind 分派
//
// 早期版本只给 log_absent 写了分支，其余三类全走「命中数 ≥ 阈值」。
// 于是 log_spike（量突变）和 log_field_threshold（字段阈值）被当成关键词规则跑：
// 建规则不报错、列表里状态是"运行中"、判定链看起来也正常，
// 只是判定用的根本不是那条规则的语义——这正是本产品最反对的那种失败。
// 加类型时必须同时在这里加分支，default 会显性报错而不是悄悄按关键词处理。

// groupVerdict 是单个分组的判定结论。
type groupVerdict struct {
	Hit    bool
	Reason string
	// Value 是本次判定用到的实际数值（量突变的倍数、字段阈值的分位数值），
	// 供判定链展示。只写"超过阈值"而不给出实测值，等于没解释。
	Value   float64
	Detail  map[string]any
}

// evaluateGroup 按规则类型判定单个分组。
func (e *Engine) evaluateGroup(ctx context.Context, r dueRule, spec ruleSpec,
	groupKey string, count int, hits []datasource.Hit, baseline map[string]int,
) (groupVerdict, error) {
	// 无数据告警：查询在本窗口一条都没返回。
	//
	// 对关键词规则，"没有错误日志"通常是好事，所以默认关闭；
	// 但对那些「本该一直有流量」的查询，恒为 0 往往意味着日志断流或查询写错，
	// 而不是真的没有错误——开了这个开关就把沉默本身当成异常。
	if r.NoDataAlert && count == 0 && r.Kind != kindLogAbsent {
		return groupVerdict{
			Hit: true,
			Reason: "查询在本窗口没有返回任何数据（该规则开启了无数据告警）：" +
				"通常意味着日志断流或查询条件写错，而不是「没有异常」",
			Detail: map[string]any{"count": 0, "nodata_alert": true},
		}, nil
	}

	switch r.Kind {
	case kindLogKeyword:
		return groupVerdict{
			Hit:    count >= r.Threshold,
			Reason: fmt.Sprintf("命中 %d 条，阈值 %d 条", count, r.Threshold),
			Value:  float64(count),
			Detail: map[string]any{"count": count, "threshold": r.Threshold},
		}, nil

	case kindLogAbsent:
		// 缺失检测语义相反：该出现却没出现（低于阈值）才算异常。
		return groupVerdict{
			Hit:    count < r.Threshold,
			Reason: fmt.Sprintf("本周期 %d 条，期望至少 %d 条", count, r.Threshold),
			Value:  float64(count),
			Detail: map[string]any{"count": count, "expected_min": r.Threshold},
		}, nil

	case kindLogSpike:
		// 量突变：与基线比倍数，而不是比绝对条数。
		// 绝对阈值在业务有日夜峰谷时没法用——白天正常量就超过夜里的异常量。
		ratio := spec.SpikeRatio
		if ratio <= 0 {
			ratio = 3
		}
		base := baseline[groupKey]
		if base < spec.SpikeMinBase {
			// 基线样本太小时不判定：从 1 条涨到 3 条也是 3 倍，
			// 但那通常只是噪声。报 no-hit 并说明原因，而不是硬算。
			return groupVerdict{
				Hit: false,
				Reason: fmt.Sprintf("基线仅 %d 条（低于最小基线 %d），样本太小不做倍数判定",
					base, spec.SpikeMinBase),
				Detail: map[string]any{"count": count, "baseline": base},
			}, nil
		}
		got := 0.0
		if base > 0 {
			got = float64(count) / float64(base)
		}
		return groupVerdict{
			Hit: got >= ratio,
			Reason: fmt.Sprintf("本周期 %d 条 / 基线 %d 条 = %.1f 倍，阈值 %.1f 倍",
				count, base, got, ratio),
			Value:  got,
			Detail: map[string]any{"count": count, "baseline": base, "ratio": got, "ratio_threshold": ratio},
		}, nil

	case kindLogField:
		// 字段阈值：从日志里提数值再聚合（P95 / 平均 / 最大），与阈值比。
		if spec.Field == "" {
			return groupVerdict{}, fmt.Errorf("字段阈值规则缺少 spec.field（要取哪个字段的数值）")
		}
		values := extractNumbers(hits, spec.Field, spec.FieldPattern)
		if len(values) == 0 {
			// 提不到数值不能当成"没超阈值"：字段名写错时恒为 0 条，
			// 规则会永远安静，而人以为它在守着。
			return groupVerdict{}, fmt.Errorf("命中 %d 条日志，但按 field=%q 一个数值都没提取到（字段名或正则是否写错？）",
				len(hits), spec.Field)
		}
		agg := spec.Agg
		if agg == "" {
			agg = "p95"
		}
		got := aggregate(values, agg)
		return groupVerdict{
			Hit: got >= spec.FieldThreshold,
			Reason: fmt.Sprintf("%s(%s) = %.1f，阈值 %.1f（样本 %d 条）",
				agg, spec.Field, got, spec.FieldThreshold, len(values)),
			Value: got,
			Detail: map[string]any{
				"agg": agg, "field": spec.Field, "value": got,
				"threshold": spec.FieldThreshold, "samples": len(values),
			},
		}, nil

	default:
		// 未知类型显性报错，绝不回落到关键词语义。
		// 回落的后果是规则看起来在跑、判定却用错了语义，没有任何征兆。
		return groupVerdict{}, fmt.Errorf("规则类型 %q 尚未实现判定语义", r.Kind)
	}
}

// needsBaseline 只有量突变需要额外查一次基线窗口。
// 别的类型不查——每条规则每周期多打一次数据源，规则一多就是成倍的查询压力。
func needsBaseline(kind string) bool { return kind == kindLogSpike }

// queryBaseline 查基线窗口的分组计数。
//
// previous：上一个等长窗口（默认）。week_ago：上周同一时段，
// 适合有明显日/周周期的业务——用上一个窗口比会把每天早高峰都判成突变。
func (e *Engine) queryBaseline(ctx context.Context, ad datasource.Adapter, r dueRule,
	spec ruleSpec, groupBy []string, to time.Time,
) (map[string]int, error) {
	window := time.Duration(r.Lookback) * time.Second
	var baseTo time.Time
	switch spec.Baseline {
	case "week_ago":
		baseTo = to.AddDate(0, 0, -7)
	default:
		baseTo = to.Add(-window)
	}
	res, err := ad.Query(ctx, datasource.Query{
		Expr: spec.Query, From: baseTo.Add(-window), To: baseTo,
		Limit: spec.Limit, GroupBy: groupBy,
	})
	if err != nil {
		return nil, err
	}
	if len(groupBy) == 0 {
		return map[string]int{"": int(res.Total)}, nil
	}
	return res.Groups, nil
}

// ── 字段提取与模板渲染 ──────────────────────────────────────────

// extractVars 按规则配置从命中样本里提取变量。
//
// 旧系统的 extract_fields 在这里落地：变量既用于通知模板，
// 也会成为事件标签，从而支持「按字段值路由」（例如 error_code=502 → 网络组）。
func extractVars(hits []datasource.Hit, rules []extractRule) map[string]string {
	out := map[string]string{}
	if len(hits) == 0 {
		return out
	}
	for _, ex := range rules {
		for _, h := range hits {
			if v, ok := extractOne(h, ex); ok && v != "" {
				out[ex.Name] = v
				break // 取第一条能提到的
			}
		}
		if _, ok := out[ex.Name]; !ok {
			// 提不到就明确留空标记，而不是让模板里出现半截的 {{name}}。
			// 通知里出现未替换的占位符，看的人会以为系统坏了。
			out[ex.Name] = ""
		}
	}
	return out
}

func extractOne(h datasource.Hit, ex extractRule) (string, bool) {
	raw := ""
	if ex.Path != "" {
		if v, ok := lookupField(h.Fields, ex.Path); ok {
			raw = fmt.Sprint(v)
		} else if v, ok := h.Labels[ex.Path]; ok {
			raw = v
		}
	}
	if raw == "" {
		raw = h.Line
	}
	if ex.Pattern == "" {
		return raw, raw != ""
	}
	re, err := regexp.Compile(ex.Pattern)
	if err != nil {
		return "", false
	}
	m := re.FindStringSubmatch(raw)
	if len(m) == 0 {
		return "", false
	}
	if len(m) > 1 {
		return m[1], true // 有捕获组就取第一个组
	}
	return m[0], true
}

func lookupField(fields map[string]any, path string) (any, bool) {
	if fields == nil {
		return nil, false
	}
	cur := any(fields)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

var tmplVar = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// renderTemplate 渲染 {{var}} 占位符。
//
// 未知变量替换成显式标记而不是留原样：留着 {{foo}} 会让收到通知的人
// 以为是系统故障；替换成空串则会让人以为字段本来就没值。
func renderTemplate(tmpl string, vars map[string]string) string {
	if tmpl == "" {
		return ""
	}
	return tmplVar.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := tmplVar.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			if v == "" {
				return "(未提取到)"
			}
			return v
		}
		return "(未定义变量:" + name + ")"
	})
}

// extractNumbers 从命中里提取数值序列，供字段阈值聚合。
func extractNumbers(hits []datasource.Hit, field, pattern string) []float64 {
	var out []float64
	ex := extractRule{Name: field, Path: field, Pattern: pattern}
	for _, h := range hits {
		// 指标型数据源（二期）直接带 Value
		if h.Value != 0 && h.Line == "" && len(h.Fields) == 0 {
			out = append(out, h.Value)
			continue
		}
		s, ok := extractOne(h, ex)
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			continue
		}
		out = append(out, f)
	}
	return out
}

// aggregate 聚合数值。分位数用最近邻法——样本量在告警场景通常只有几百条，
// 插值法带来的精度差异远小于日志本身的抖动，不值得为它增加理解成本。
func aggregate(values []float64, agg string) float64 {
	if len(values) == 0 {
		return 0
	}
	switch agg {
	case "avg":
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum / float64(len(values))
	case "max":
		max := values[0]
		for _, v := range values {
			if v > max {
				max = v
			}
		}
		return max
	case "sum":
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		return sum
	default: // p95 / p99 / p50
		sorted := append([]float64{}, values...)
		sort.Float64s(sorted)
		q := 0.95
		switch agg {
		case "p50":
			q = 0.5
		case "p99":
			q = 0.99
		}
		idx := int(float64(len(sorted)-1) * q)
		return sorted[idx]
	}
}

// hitsOfGroup 取某个分组的命中样本，供字段提取使用。
func hitsOfGroup(hits []datasource.Hit, groupBy []string, key string) []datasource.Hit {
	if len(groupBy) == 0 {
		return hits
	}
	var out []datasource.Hit
	for _, h := range hits {
		if datasource.GroupKey(h.Labels, groupBy) == key {
			out = append(out, h)
		}
	}
	return out
}
