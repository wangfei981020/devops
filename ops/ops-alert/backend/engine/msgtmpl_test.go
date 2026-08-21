package engine

import (
	"strings"
	"testing"
	"time"
)

// 模板语法必须是 {{var}}，与规则自己的 message_template 一致。
//
// 🔴 这条锁的是一个真实错误：兜底模板第一版按 `${var}` 写，
// 结果预览把变量原样吐回来 —— 既不报错也没替换，看起来像"预览坏了"。
// 全站只能有一种语法，两种并存时写错的表现是变量原样出现在告警里。
func TestTemplateSyntaxIsDoubleBrace(t *testing.T) {
	vars := map[string]string{"rule": "支付超时", "severity": "critical"}
	got, miss := renderStrict("[{{severity}}] {{rule}}", vars)
	if got != "[critical] 支付超时" {
		t.Errorf("{{var}} 没有被替换，实际 %q", got)
	}
	if len(miss) != 0 {
		t.Errorf("不该有缺失变量，实际 %v", miss)
	}
	// ${var} 是**另一套语法**，不该被替换 —— 静默替换它等于承认两种语法并存
	if got, _ := renderStrict("${rule}", vars); got != "${rule}" {
		t.Errorf("${var} 不该被处理，实际 %q", got)
	}
}

// 变量名写错和变量取不到值是**两件事**，不能混为一谈。
//
// 混在一起报的话，用户会去改一个其实写对了的变量名。
func TestMissingNameVsEmptyValue(t *testing.T) {
	vars := map[string]string{"rule": "", "severity": "warning"}
	got, miss := renderStrict("{{rule}}|{{typo}}", vars)
	if !strings.Contains(got, "(未提取到)") {
		t.Errorf("已定义但为空的变量应渲染成 (未提取到)，实际 %q", got)
	}
	if !strings.Contains(got, "(无此变量)") {
		t.Errorf("未定义的变量应渲染成 (无此变量)，实际 %q", got)
	}
	if len(miss) != 1 || miss[0] != "typo" {
		t.Errorf("只有写错的名字该进 missing，实际 %v", miss)
	}
}

// 渲染失败绝不能吞掉告警：模板空着、变量全错，都必须产出可发送的内容。
//
// 🔴 一次文案手误变成一次静默失明，是这套系统最不能出的故障。
func TestRenderNeverProducesEmpty(t *testing.T) {
	vars := templateVars("规则A", "critical", "svc=pay", "loki", 3,
		time.Now().Add(-time.Hour), time.UTC, "样本", nil, map[string]string{"env": "prod"})

	for _, tc := range []struct{ name string; tpl msgTemplate }{
		{"空模板回落到内置", msgTemplate{Name: "空"}},
		{"全是错变量", msgTemplate{Name: "错", Title: "{{nope}}", Body: "{{alsonope}}"}},
	} {
		title, body, warn := renderMessage(tc.tpl, vars)
		if title == "" || body == "" {
			t.Errorf("%s: 渲染出空内容（title=%q body=%q）—— 告警会变成一条空消息", tc.name, title, body)
		}
		if tc.tpl.Title == "{{nope}}" && warn == "" {
			t.Errorf("%s: 有未定义变量却没有警告，模板错误会一直没人发现", tc.name)
		}
	}
}

// 同名时优先级：提取字段 > 标签 > 内置。
//
// 用户自己定义的东西该赢过系统内置的，否则他明明定义了 service
// 却拿到标签里的那个，而两者可能不一样。
func TestVarPrecedence(t *testing.T) {
	v := templateVars("r", "warning", "g", "ds", 1, time.Time{}, time.UTC, "s",
		map[string]string{"service": "来自提取"},
		map[string]string{"service": "来自标签", "rule": "标签也叫 rule"})
	if v["service"] != "来自提取" {
		t.Errorf("提取字段应优先于标签，实际 %q", v["service"])
	}
	if v["rule"] != "r" {
		t.Errorf("内置 rule 不该被同名标签覆盖，实际 %q", v["rule"])
	}
	if v["labels.service"] != "来自标签" {
		t.Errorf("标签必须同时以 labels.x 形式可用，实际 %q", v["labels.service"])
	}
}
