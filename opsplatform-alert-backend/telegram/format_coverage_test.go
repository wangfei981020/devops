package telegram

import (
	"strings"
	"testing"
)

// Each of these renders as formatting on Lark. Before this, everything below
// the bold/code/link trio arrived on Telegram as literal markdown source.
func TestMarkdownToHTMLCoversLarkConstructs(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"bold", "**加粗**", "<b>加粗</b>"},
		{"italic", "*斜体*", "<i>斜体</i>"},
		{"strikethrough", "~~删除线~~", "<s>删除线</s>"},
		{"inline code", "`行内代码`", "<code>行内代码</code>"},
		{"link", "[链接](https://x.com)", `<a href="https://x.com">链接</a>`},
		{"lark at tag", "<at id=ou_xxx>Bruce</at>", "Bruce"},
		{"font colour", "<font color='red'>红字</font>", "红字"},
		{"raw anchor", "<a href='https://x.com'>裸链接</a>", `<a href="https://x.com">裸链接</a>`},
		{"heading", "# 一级标题", "<b>一级标题</b>"},
		{"deep heading", "### 三级标题", "<b>三级标题</b>"},
		{"bullet list", "- 列表项\n- 第二项", "• 列表项\n• 第二项"},
		{"asterisk bullet", "* 列表项", "• 列表项"},
		{"horizontal rule", "---", horizontalRule},
		{"image", "![图](https://x.com/a.png)", `<a href="https://x.com/a.png">图</a>`},
		{"image without alt", "![](https://x.com/a.png)", `<a href="https://x.com/a.png">https://x.com/a.png</a>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MarkdownToHTML(c.in); got != c.want {
				t.Errorf("MarkdownToHTML(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Telegram renders everything but <pre>/<code> proportionally, so a table's
// pipes line up with nothing unless it is put in a monospace block.
func TestMarkdownToHTMLPutsATableInAPre(t *testing.T) {
	got := MarkdownToHTML("统计:\n| 域名 | 次数 |\n|---|---:|\n| a.com | 12 |\n完毕")

	want := "统计:\n<pre>| 域名 | 次数 |\n|---|---:|\n| a.com | 12 |</pre>\n完毕"
	if got != want {
		t.Errorf("MarkdownToHTML() = %q, want %q", got, want)
	}
}

// The table rule keys on the separator row, not on the presence of a pipe —
// the alert header itself contains one.
func TestMarkdownToHTMLLeavesAPipedLineAlone(t *testing.T) {
	got := MarkdownToHTML("**级别:** S1 | **命中:** 2 条")

	if strings.Contains(got, "<pre>") {
		t.Errorf("MarkdownToHTML() mistook an ordinary line for a table: %q", got)
	}
	if got != "<b>级别:</b> S1 | <b>命中:</b> 2 条" {
		t.Errorf("MarkdownToHTML() = %q", got)
	}
}

// Emphasis delimiters must hug non-space text. Alert bodies and log lines are
// full of lone asterisks, and treating one as emphasis eats the line.
func TestMarkdownToHTMLDoesNotEmphasiseALoneAsterisk(t *testing.T) {
	for _, in := range []string{
		"级别: S1 * 命中 2 条",
		"tid:0973c67a ERROR user_id_foo *不是斜体",
		"cron: */5 * * * *",
	} {
		if got := MarkdownToHTML(in); strings.Contains(got, "<i>") {
			t.Errorf("MarkdownToHTML(%q) = %q, should not emphasise", in, got)
		}
	}
}

// The whole point of the fence is that log text is left verbatim. The new
// line-level rules must not reach inside one.
func TestMarkdownToHTMLLeavesFencedLogTextVerbatim(t *testing.T) {
	log := "# not a heading\n- not a bullet\n---\n*not italic*\n| a | b |\n|---|---|"
	got := MarkdownToHTML("Exception:\n```\n" + log + "\n```")

	if !strings.Contains(got, "<pre>"+log+"</pre>") {
		t.Errorf("MarkdownToHTML() rewrote fenced content: %q", got)
	}
	if strings.Contains(got, "•") || strings.Contains(got, horizontalRule) {
		t.Errorf("MarkdownToHTML() applied line rules inside a fence: %q", got)
	}
}

func TestMarkdownToHTMLNewRulesStayWellFormed(t *testing.T) {
	for _, in := range []string{
		"# 标题\n- **粗体项**\n- *斜体项*\n---\n| a | b |\n|---|---|\n| 1 | 2 |",
		"<font color='red'>**红粗**</font> 与 ~~删除~~",
	} {
		if got := MarkdownToHTML(in); !isWellFormedHTML(got) {
			t.Errorf("MarkdownToHTML(%q) produced malformed HTML: %q", in, got)
		}
	}
}
