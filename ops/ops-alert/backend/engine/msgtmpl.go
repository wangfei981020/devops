package engine

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
	"ops-alert-backend/notify"
)

// 告警文案模板。
//
// # 🔴 最重要的一条：渲染失败绝不能吞掉告警
//
// 模板是人写的，人会写错。写错的模板如果导致「这条告警不发了」，
// 那么一次文案手误就变成一次静默失明 —— 而这正是这套系统存在的理由的反面。
//
// 所以渲染失败时：**照常发出去**，用内置模板，并把错误落到日志和渠道字段里。
// 值班的人会收到一条格式不对但内容完整的告警，外加一句"模板渲染失败"。
// 那比收不到好得多，也比收到一条只写着"模板错误"的空告警好得多。
//
// # 变量从哪来
//
// 三类，合并成一个平表：
//   固定字段   rule / severity / group / count / first_at / datasource / duration
//   规则提取   extract_fields 定义的业务字段（订单号、耗时……）
//   标签       labels.xxx
//
// ⚠️ 变量语法是 `{{name}}`（与规则自己的 message_template 一致）。
//
// ⚠️ 三类**可能重名**。优先级是 提取字段 > 标签 > 固定字段：
// 用户自己定义的东西应该赢过系统内置的，否则他明明定义了 `service`
// 却拿到标签里的那个，而两者可能不一样。这条必须写在界面的变量说明里。

// builtinTitle / builtinBody 是兜底模板的**代码内副本**。
//
// ⚠️ 不能只依赖库里的内置模板：库里那条可能被误删（尽管界面禁止），
// 也可能在建租户时漏种。兜底必须是永远存在的，所以它在代码里。
const (
	builtinTitle = "{{rule}}"
	builtinBody  = "级别：{{severity}}\n对象：{{group}}\n命中：{{count}} 条\n首次：{{first_at}}\n\n{{sample}}"
)

// msgTemplate 一条模板。
type msgTemplate struct {
	ID    int64
	Name  string
	Title string
	Body  string
}

// loadTemplate 取某个通知渠道绑定的模板。没绑定或取不到时返回代码内的兜底。
//
// ⚠️ 取不到时返回兜底而**不是返回错误**：模板是锦上添花，
// 让它成为发送路径上的一个失败点是本末倒置。
func loadTemplate(sc *store.Scoped, notifierID int64) msgTemplate {
	fallback := msgTemplate{Name: "内置兜底", Title: builtinTitle, Body: builtinBody}
	var id sql.NullInt64
	if err := sc.QueryRow(`SELECT template_id FROM notifiers
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, notifierID).Scan(&id); err != nil {
		return fallback
	}
	if !id.Valid {
		return fallback
	}
	var t msgTemplate
	if err := sc.QueryRow(`SELECT id, name, title_tmpl, body_tmpl FROM message_templates
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id.Int64).
		Scan(&t.ID, &t.Name, &t.Title, &t.Body); err != nil {
		// 绑了一个已被删除的模板。这是配置问题，要说出来 ——
		// 静默用兜底的话，用户会以为自己的模板生效了而实际没有
		logx.J("engine", "template_missing", map[string]any{
			"notifier_id": notifierID, "template_id": id.Int64,
			"note": "渠道绑定的模板不存在（可能已被删除），本次用内置兜底",
		})
		return fallback
	}
	return t
}

// renderMessage 用模板渲染出最终的通知内容。
//
// 返回的 warn 非空时表示渲染有问题 —— 调用方要把它带进消息里，
// 让收件人知道"这条告警的格式不对，但内容是真的"。
func renderMessage(t msgTemplate, vars map[string]string) (title, body, warn string) {
	title, missTitle := renderStrict(firstNonEmpty(t.Title, builtinTitle), vars)
	body, missBody := renderStrict(firstNonEmpty(t.Body, builtinBody), vars)

	miss := append(missTitle, missBody...)
	if len(miss) == 0 {
		return title, body, ""
	}
	sort.Strings(miss)
	miss = dedupe(miss)
	// ⚠️ 未定义的变量**不留在文案里**（renderStrict 已经把它换成占位），
	// 但必须报出来。留着 ${xxx} 的话值班的人会以为系统坏了；
	// 什么都不说的话模板会一直错下去没人发现。
	warn = fmt.Sprintf("模板 %q 里有 %d 个变量在这条告警里不存在：%s。"+
		"已用占位符代替，文案可能不完整。",
		t.Name, len(miss), strings.Join(miss, "、"))
	logx.J("engine", "template_missing_vars", map[string]any{
		"template": t.Name, "missing": miss,
		"note": "告警照常发出，未定义变量已用占位符代替",
	})
	return title, body, warn
}

// renderStrict 替换 {{var}}，并把认不出的变量名收集起来。
//
// ⚠️ 语法是 `{{var}}`，**不是** `${var}`。这跟着 renderTemplate（规则自己的
// 模板）走 —— 全站必须只有一种语法。我第一版按 `${var}` 写了兜底模板和
// 界面文案，结果预览把变量原样吐了回来：既不报错，也没替换，
// 看起来像"预览功能坏了"，而实际是模板语法写错了。
//
// 与 renderTemplate（规则自己的模板）的区别：那个把未定义变量渲染成
// "(未定义变量:x)" 就完了；这里额外把名字收集出来，好让界面能提示。
func renderStrict(tmpl string, vars map[string]string) (string, []string) {
	var missing []string
	out := tmplVar.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := tmplVar.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return "(无此变量)"
		}
		if v == "" {
			// 变量存在但没取到值 —— 与"变量名写错"是两回事，不算 missing。
			// 混在一起报的话，用户会去改一个其实写对了的变量名
			return "(未提取到)"
		}
		return v
	})
	return out, missing
}

// templateVars 组装可用变量表。界面上的"可用变量"清单也从这里出，
// 保证**文档和实现是同一份东西** —— 各写一份必然出现"文档里有、实际没有"。
func templateVars(ruleName, severity, groupKey, datasource string, count int,
	firstAt time.Time, loc *time.Location, sample string,
	extracted map[string]string, labels map[string]string,
) map[string]string {
	v := map[string]string{
		"rule":       ruleName,
		"severity":   severity,
		"group":      groupKey,
		"count":      fmt.Sprint(count),
		"datasource": datasource,
		"sample":     sample,
	}
	if !firstAt.IsZero() {
		v["first_at"] = firstAt.In(loc).Format("2006-01-02 15:04:05")
		v["duration"] = humanDuration(time.Since(firstAt))
	} else {
		v["first_at"] = ""
		v["duration"] = ""
	}
	// 优先级：提取字段 > 标签 > 固定字段。见文件头的说明。
	for k, val := range labels {
		v["labels."+k] = val
		if _, taken := v[k]; !taken {
			v[k] = val
		}
	}
	for k, val := range extracted {
		v[k] = val
	}
	return v
}

// BuiltinVarNames 固定变量的名字与说明。界面上的变量清单用它。
//
// ⚠️ 与 templateVars 里的键必须一一对应。多一个少一个都会让界面说谎，
// 而说谎的方向是"照着提示写了个变量，发出来是 (无此变量)"。
func BuiltinVarNames() []([2]string) {
	return [][2]string{
		{"rule", "规则名"},
		{"severity", "级别（critical / warning / info）"},
		{"group", "分组键。没配分组维度时为空"},
		{"count", "本次命中条数"},
		{"first_at", "首次命中时刻，按业务时区（TZ）格式化"},
		{"duration", "距首次命中多久，如 1 小时 20 分钟"},
		{"datasource", "数据源名"},
		{"sample", "命中样本，最多 3 行"},
		{"labels.xxx", "规则标签，如 labels.env、labels.team"},
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分钟", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%d 小时", h)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", h, m)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func dedupe(in []string) []string {
	out := in[:0]
	var prev string
	for i, s := range in {
		if i == 0 || s != prev {
			out = append(out, s)
		}
		prev = s
	}
	return out
}

// applyTemplate 把模板渲染结果盖到消息上。
//
// ⚠️ 只覆盖 Title 和 Sample，**不动 Fields**。
// Fields 是结构化的业务字段（各渠道会渲染成表格），把它交给模板意味着
// 用户一个手误就丢掉全部上下文；而模板真正要解决的是"措辞和排版"。
func applyTemplate(msg *notify.Message, t msgTemplate, vars map[string]string) {
	title, body, warn := renderMessage(t, vars)
	if title != "" {
		msg.Title = title
	}
	if body != "" {
		msg.Sample = body
	}
	if warn != "" {
		// 挂在 Fields 上而不是拼进正文：拼进正文会被模板本身的排版吞掉，
		// 而这条警告恰恰是在说"排版不可信"
		msg.Fields = append(msg.Fields, notify.Field{Key: "⚠️ 模板", Value: warn})
	}
}
