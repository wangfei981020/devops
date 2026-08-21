package api

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

// 场景模板。
//
// # 为什么要有这层
//
// 通用表单是按**技术模型**组织的：先选检测类型，再写 LogQL，再填阈值、
// 持续周期、分组维度、期望分组……要求人先理解新系统的模型才能建出第一条规则。
// 而真实规则翻来覆去就那么几个场景 —— 生产上的 7 条，全部落在下面三个里。
//
// 模板反过来按**业务问题**提问："哪些错误码不用告警" 而不是 "写一段排除用的 LogQL"。
// LogQL、字段提取、分组维度、期望分组这些由模板生成。
//
// ⚠️ 模板定义放后端而不是前端：它是产品知识，两边各写一份必然分叉，
// 而且 MCP / API 调用方也要能用同一套。
//
// ⚠️ 模板**不是**表单的预填值，它会真的改变生成结果的形状
// （比如断流模板会把 lookback 设成"多久没日志算断"，而不是常规回看窗口）。

type tplField struct {
	Key string `json:"key"`
	// text / tags（多值）/ number
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
	// Example 是**技术示例值**（正则、容器名），不是给人读的文案，所以留在这里。
	// label / help 这类文案一律走语言包。
	Example string `json:"example,omitempty"`
}

// ⚠️ 结构里**不带任何人类可读文案**。
//
// 第一版把 Name/When/Label 写成中文字符串直出，英文界面下整页模板全是中文
// （框架文案翻了、模板内容没翻）—— 后端回文案就等于把翻译能力关在门外。
// 现在只回 key 与结构，文案由前端按约定组装：
//
//	opsalert:tplDef.<模板key>.name
//	opsalert:tplDef.<模板key>.when
//	opsalert:tplDef.<模板key>.f.<字段key>.label / .help
type ruleTemplate struct {
	Key    string     `json:"key"`
	Kind   string     `json:"kind"`
	Fields []tplField `json:"fields"`
}

// 三个模板覆盖了生产现有的全部 7 条规则。
// 加模板要有真实用例支撑——凭空加的模板没人用，却要一直维护。
var ruleTemplates = []ruleTemplate{
	{
		Key:  "error_code",
		Kind: "log_keyword",
		Fields: []tplField{
			{Key: "namespaces", Type: "tags", Required: true, Example: "g32-wallet"},
			{Key: "container_include", Type: "text", Example: "openapi-backend.*|wallet-client-backend.*"},
			{Key: "container_exclude", Type: "text", Example: "telegram.*|bi-.*"},
			{Key: "ignore_codes", Type: "tags", Example: "9007"},
		},
	},
	{
		Key:  "log_heartbeat",
		Kind: "log_absent",
		Fields: []tplField{
			{Key: "namespace", Type: "text", Required: true, Example: "g32-game"},
			{Key: "container_pattern", Type: "text", Required: true, Example: ".*resource-backend"},
			{Key: "log_pattern", Type: "text", Required: true, Example: "Link.* timestamp.* Round.*"},
			{Key: "expected_containers", Type: "tags", Required: true, Example: "baccarat-resource-backend"},
			{Key: "silence_minutes", Type: "number", Required: true, Default: "30"},
		},
	},
	{
		Key:  "latency_threshold",
		Kind: "log_field_threshold",
		Fields: []tplField{
			{Key: "namespace", Type: "text", Required: true, Example: "g32-wallet"},
			{Key: "container", Type: "text", Required: true, Example: "wallet-client-backend"},
			{Key: "log_pattern", Type: "text", Required: true, Example: "调用站点交易接口"},
			{Key: "cost_pattern", Type: "text", Required: true, Example: "running time\\s*=\\s*(\\d+)\\s*ms"},
			{Key: "threshold_ms", Type: "number", Required: true, Default: "5000"},
			{Key: "agg", Type: "text", Default: "p95"},
		},
	},
}

func (s *Server) listTemplates(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": ruleTemplates})
}

// logQLString 把用户填的值安全地放进 LogQL 的双引号字符串里。
//
// ⚠️ 不做转义的话，一个带引号的输入就能改变查询语义（甚至让查询语法出错，
// 而错误要等到下一个调度周期才在「规则健康」里露出来）。
func logQLString(v string) string {
	return strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`)
}

var codeRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// buildFromTemplate 把模板参数翻成规则草稿。
func buildFromTemplate(key string, p map[string]any) (kind, query string, spec map[string]any,
	groupBy []string, lookback int, extra map[string]any, err error) {

	str := func(k string) string { return strings.TrimSpace(fmt.Sprint(p[k])) }
	list := func(k string) []string {
		var out []string
		if raw, ok := p[k].([]any); ok {
			for _, v := range raw {
				if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	}
	num := func(k string, def int) int {
		var n int
		if _, e := fmt.Sscanf(fmt.Sprint(p[k]), "%d", &n); e != nil || n <= 0 {
			return def
		}
		return n
	}

	spec = map[string]any{}
	extra = map[string]any{}

	switch key {
	case "error_code":
		ns := list("namespaces")
		if len(ns) == 0 {
			return "", "", nil, nil, 0, nil, fmt.Errorf("至少填一个命名空间")
		}
		var sel []string
		// 多个命名空间合成一个正则选择器 —— 旧系统是逐个查再合并，
		// 一条查询能做的事没必要发 N 个请求
		if len(ns) == 1 {
			sel = append(sel, fmt.Sprintf(`namespace="%s"`, logQLString(ns[0])))
		} else {
			sel = append(sel, fmt.Sprintf(`namespace=~"%s"`, logQLString(strings.Join(ns, "|"))))
		}
		if v := str("container_include"); v != "" && v != "<nil>" {
			sel = append(sel, fmt.Sprintf(`container=~"%s"`, logQLString(v)))
		}
		if v := str("container_exclude"); v != "" && v != "<nil>" {
			sel = append(sel, fmt.Sprintf(`container!~"%s"`, logQLString(v)))
		}
		q := fmt.Sprintf(`{%s} |= "ERROR"`, strings.Join(sel, ", "))

		// ⚠️ 这里是本模板存在的主要理由之一：
		// 旧系统把「不告警的错误码」放在路由配置里（ignore_values），
		// 迁移时最容易整批丢掉 —— 丢了就是上线即告警风暴。
		// 模板把它直接落成查询的排除条件，让它成为规则的一部分。
		if codes := list("ignore_codes"); len(codes) > 0 {
			var safe []string
			for _, cd := range codes {
				if !codeRe.MatchString(cd) {
					return "", "", nil, nil, 0, nil,
						fmt.Errorf("错误码 %q 含特殊字符，只支持字母数字和 -_", cd)
				}
				safe = append(safe, cd)
			}
			q += fmt.Sprintf(" !~ `\"code\":\"(%s)\"`", strings.Join(safe, "|"))
			extra["ignored_codes"] = safe
		}
		spec["query"] = q
		spec["extract"] = []map[string]string{
			{"name": "Code", "path": "line", "pattern": `"code":"(\d+)"`},
			{"name": "Tid", "path": "line", "pattern": `tid:([a-f0-9\-]+)`},
		}
		return "log_keyword", q, spec, nil, 300, extra, nil

	case "log_heartbeat":
		nsv, cp, lp := str("namespace"), str("container_pattern"), str("log_pattern")
		if nsv == "" || cp == "" || lp == "" {
			return "", "", nil, nil, 0, nil, fmt.Errorf("命名空间、容器名匹配、日志特征都必填")
		}
		exp := list("expected_containers")
		if len(exp) == 0 {
			// 断流检测**必须**有期望清单：分组是从查询结果里来的，
			// 彻底不出日志的容器不会出现在结果里，也就永远不会被判断为"缺失"
			return "", "", nil, nil, 0, nil,
				fmt.Errorf("必须列出期望在跑的容器，否则完全不出日志的容器不会被发现")
		}
		q := fmt.Sprintf(`{namespace="%s", container=~"%s"} |~ "%s"`,
			logQLString(nsv), logQLString(cp), logQLString(lp))
		spec["query"] = q
		spec["expected_groups"] = exp
		mins := num("silence_minutes", 30)
		return "log_absent", q, spec, []string{"container"}, mins * 60, extra, nil

	case "latency_threshold":
		nsv, ct, lp, cost := str("namespace"), str("container"), str("log_pattern"), str("cost_pattern")
		if nsv == "" || ct == "" || lp == "" || cost == "" {
			return "", "", nil, nil, 0, nil, fmt.Errorf("命名空间、容器、日志特征、耗时正则都必填")
		}
		if !strings.Contains(cost, "(") {
			// 没有捕获组就取不到数值，规则会建成功但永远算不出结果
			return "", "", nil, nil, 0, nil, fmt.Errorf("耗时正则里必须有一个捕获组 ()，用来取出数值")
		}
		q := fmt.Sprintf(`{namespace="%s", container="%s"} |~ "%s"`,
			logQLString(nsv), logQLString(ct), logQLString(lp))
		agg := str("agg")
		switch agg {
		case "p50", "p95", "p99", "max", "avg", "sum":
		default:
			agg = "p95"
		}
		spec["query"] = q
		spec["field"] = "cost_ms"
		spec["field_pattern"] = cost
		spec["agg"] = agg
		spec["field_threshold"] = num("threshold_ms", 5000)
		spec["extract"] = []map[string]string{{"name": "cost_ms", "path": "line", "pattern": cost}}
		return "log_field_threshold", q, spec, nil, 300, extra, nil
	}
	return "", "", nil, nil, 0, nil, fmt.Errorf("未知模板 %q", key)
}

// previewTemplate 只生成不保存：让人在保存前看见模板到底生成了什么查询。
//
// ⚠️ 模板隐藏了复杂度，但**不能隐藏结果**。看不到生成的 LogQL，
// 出问题时人既不知道该改哪里，也无法判断模板是不是理解错了他的意图。
func (s *Server) previewTemplate(c *gin.Context) {
	var req struct {
		Template string         `json:"template"`
		Params   map[string]any `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	kind, query, spec, groupBy, lookback, extra, err := buildFromTemplate(req.Template, req.Params)
	if err != nil {
		// 模板参数不合法是**用户输入问题**，要把原因原样告诉他
		c.JSON(http.StatusBadRequest, gin.H{"error": "template_invalid", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"kind": kind, "query": query, "spec": spec,
		"group_by": groupBy, "lookback_sec": lookback, "extra": extra,
	})
}
