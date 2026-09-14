package telegram

import (
	"strings"
	"testing"
	"time"

	"opsplatform-alert-backend/models"
)

func TestEscapeHTMLEscapesHTMLSpecials(t *testing.T) {
	got := EscapeHTML(`a < b & c > d "quoted" 'single'`)
	want := `a &lt; b &amp; c &gt; d &quot;quoted&quot; 'single'`
	if got != want {
		t.Errorf("EscapeHTML() = %q, want %q", got, want)
	}
}

func TestMarkdownToHTMLConvertsBold(t *testing.T) {
	got := MarkdownToHTML("**服务:** api-gateway")
	want := "<b>服务:</b> api-gateway"
	if got != want {
		t.Errorf("MarkdownToHTML() = %q, want %q", got, want)
	}
}

func TestMarkdownToHTMLConvertsInlineCode(t *testing.T) {
	got := MarkdownToHTML("错误码 `9018` 出现")
	want := "错误码 <code>9018</code> 出现"
	if got != want {
		t.Errorf("MarkdownToHTML() = %q, want %q", got, want)
	}
}

func TestMarkdownToHTMLConvertsLink(t *testing.T) {
	got := MarkdownToHTML("详情见 [Grafana](https://g.example.com/d/1)")
	want := `详情见 <a href="https://g.example.com/d/1">Grafana</a>`
	if got != want {
		t.Errorf("MarkdownToHTML() = %q, want %q", got, want)
	}
}

func TestMarkdownToHTMLStripsLarkAtTags(t *testing.T) {
	got := MarkdownToHTML("<at id=ou_abc>Bruce</at> 处理一下")
	want := "Bruce 处理一下"
	if got != want {
		t.Errorf("MarkdownToHTML() = %q, want %q", got, want)
	}
}

func TestMarkdownToHTMLEscapesRawAngleBrackets(t *testing.T) {
	// A log line like "value <nil>" must not become a bogus HTML tag.
	got := MarkdownToHTML("value <nil> here")
	want := "value &lt;nil&gt; here"
	if got != want {
		t.Errorf("MarkdownToHTML() = %q, want %q", got, want)
	}
}

func TestTruncateRunesCountsRunesNotBytes(t *testing.T) {
	// 20 Chinese characters = 60 bytes, so a byte-based cut would mangle them.
	// The marker itself is 7 runes, so a 12-rune budget leaves room for 5 of them.
	const budget = 12
	got := TruncateRunes("一二三四五六七八九十一二三四五六七八九十", budget)

	if !strings.HasPrefix(got, "一二三四五") {
		t.Errorf("TruncateRunes() = %q, want it to start with 5 chars", got)
	}
	if strings.Contains(got, "六") {
		t.Errorf("TruncateRunes() = %q, should not contain the 6th char", got)
	}
	if !strings.Contains(got, "已截断") {
		t.Errorf("TruncateRunes() = %q, want a truncation marker", got)
	}
	// The marker counts against the budget: the whole point is that the result
	// fits the platform limit, marker included.
	if n := len([]rune(got)); n > budget {
		t.Errorf("TruncateRunes() produced %d runes, must be <= %d", n, budget)
	}
}

func TestTruncateRunesLeavesShortStringsAlone(t *testing.T) {
	if got := TruncateRunes("short", 4096); got != "short" {
		t.Errorf("TruncateRunes() = %q, want %q", got, "short")
	}
}

func TestTruncateRunesExactBoundary(t *testing.T) {
	// A string of exactly max runes should be returned completely untouched
	// without truncation marker.
	const maxRunes = 50
	input := strings.Repeat("a", maxRunes)
	got := TruncateRunes(input, maxRunes)

	if got != input {
		t.Errorf("TruncateRunes() with exact boundary = %q, want %q (no marker, no loss)", got, input)
	}
	if strings.Contains(got, "已截断") {
		t.Errorf("TruncateRunes() should not add marker when at exact boundary, got %q", got)
	}
}

func TestTruncateRunesWellFormedHTML(t *testing.T) {
	// A string with HTML tags that gets truncated should still produce well-formed HTML.
	// Content: <b>bold</b> + 50 more runes + <code>code</code>
	longContent := "<b>bold</b>" + strings.Repeat("x", 50) + "<code>code</code>"
	got := TruncateRunes(longContent, 40)

	// Should not cut inside a tag or leave tags unclosed
	if !isWellFormedHTML(got) {
		t.Errorf("TruncateRunes() produced malformed HTML: %q", got)
	}

	if n := len([]rune(got)); n > 40 {
		t.Errorf("TruncateRunes() produced %d runes, must be <= 40", n)
	}
}

func isWellFormedHTML(s string) bool {
	// Stack-based validation: ensure proper nesting and all tags are closed.
	var stack []string
	i := 0
	for i < len(s) {
		if s[i] == '<' {
			// Find the end of the tag
			end := strings.Index(s[i:], ">")
			if end == -1 {
				// Dangling '<' without closing '>'
				return false
			}
			tag := s[i : i+end+1]

			if strings.HasPrefix(tag, "</") {
				// Closing tag: must match the top of the stack
				tagName := htmlTagName(tag)
				if len(stack) == 0 || stack[len(stack)-1] != tagName {
					// Either no opening tag or mismatched tag
					return false
				}
				stack = stack[:len(stack)-1]
			} else if !strings.HasPrefix(tag, "<!") {
				// Opening tag (not a comment or doctype)
				tagName := htmlTagName(tag)
				stack = append(stack, tagName)
			}

			i += end + 1
		} else {
			i++
		}
	}

	// All tags must be closed
	return len(stack) == 0
}

// htmlTagName extracts the tag name from an HTML tag, handling attributes.
func htmlTagName(tag string) string {
	tag = strings.TrimPrefix(tag, "<")
	tag = strings.TrimSuffix(tag, ">")
	if strings.HasPrefix(tag, "/") {
		tag = strings.TrimPrefix(tag, "/")
	}
	// For <a href="...">, extract just "a"
	spaceIdx := strings.Index(tag, " ")
	if spaceIdx != -1 {
		tag = tag[:spaceIdx]
	}
	return tag
}

func TestSeverityPrefix(t *testing.T) {
	cases := map[string]string{
		"S1": "🚨🚨🚨", "critical": "🚨🚨🚨",
		"S2": "🔴🔴", "warning": "🔴🔴",
		"S3": "⚠️", "info": "⚠️",
		"recovery": "✅",
		"report":   "",
		"":         "🚨🚨🚨",
	}
	for severity, want := range cases {
		if got := SeverityPrefix(severity); got != want {
			t.Errorf("SeverityPrefix(%q) = %q, want %q", severity, got, want)
		}
	}
}

func TestBuildMessageLayout(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	got := BuildMessage("支付超时", "**服务:** api", "S2", nil, false, now)

	// Title is prefix + bold title + a SINGLE newline, then the body opens in a
	// plain (non-expandable) blockquote since the content is short.
	if !strings.HasPrefix(got, "🔴🔴 <b>支付超时</b>\n<blockquote>") {
		t.Errorf("BuildMessage() should start with prefixed bold title, one newline, then an opening blockquote, got %q", got)
	}
	if !strings.Contains(got, "<b>服务:</b> api") {
		t.Errorf("BuildMessage() should carry converted content, got %q", got)
	}
	if !strings.Contains(got, "</blockquote>") {
		t.Errorf("BuildMessage() should close the blockquote, got %q", got)
	}
	// Footer: clock emoji + timestamp in italics, no "告警时间:" label.
	if !strings.Contains(got, "<i>🕐 2026-09-07 15:04:05 (+00:00 UTC)</i>") {
		t.Errorf("BuildMessage() should carry a clock-emoji italic timestamp footer, got %q", got)
	}
	if strings.Contains(got, "告警时间") {
		t.Errorf("BuildMessage() should drop the 告警时间 label, position and icon carry the meaning, got %q", got)
	}
}

func TestBuildMessageWrapsShortBodyInPlainBlockquote(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	got := BuildMessage("t", "short body", "S2", nil, false, now)

	if !strings.Contains(got, "<blockquote>short body</blockquote>") {
		t.Errorf("BuildMessage() should wrap short content in a plain blockquote, got %q", got)
	}
	if strings.Contains(got, "expandable") {
		t.Errorf("BuildMessage() should not mark a short body expandable, got %q", got)
	}
}

func TestBuildMessageNeverCollapsesALongBody(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	// Well past the length that used to earn an `expandable` attribute.
	longBody := strings.Repeat("日志内容详情", 100) // 600 runes, no markdown to convert
	got := BuildMessage("t", longBody, "S2", nil, false, now)

	// An expandable blockquote renders collapsed behind a "show more" chevron,
	// hiding the error code and log line an on-call engineer opened the chat
	// to read — and hiding the most on the biggest alerts.
	if strings.Contains(got, "expandable") {
		t.Errorf("BuildMessage() must never mark a body expandable, got a prefix of %q", got[:200])
	}
	if !strings.Contains(got, "<blockquote>") {
		t.Errorf("BuildMessage() should still wrap a long body in a plain blockquote, got a prefix of %q", got[:200])
	}
	if !isWellFormedHTML(got) {
		t.Errorf("BuildMessage() with a long body produced malformed HTML")
	}
}

func TestBuildMessageMentionsAfterBlockquoteNotInside(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	users := []models.AtUser{{Name: "Bruce", TelegramID: "12345"}}
	got := BuildMessage("t", "body", "S2", users, false, now)

	closeIdx := strings.Index(got, "</blockquote>")
	mentionIdx := strings.Index(got, `<a href="tg://user?id=12345">Bruce</a>`)
	if closeIdx == -1 || mentionIdx == -1 {
		t.Fatalf("BuildMessage() missing blockquote close or mention, got %q", got)
	}
	if mentionIdx < closeIdx {
		t.Errorf("BuildMessage() should place mentions after the blockquote closes, got %q", got)
	}
	// The mention line itself must sit outside the quoted region.
	if strings.Contains(got, "<blockquote>body\n") || strings.Contains(got, `id="12345">Bruce</a></blockquote>`) {
		t.Errorf("BuildMessage() should not nest mentions inside the blockquote, got %q", got)
	}
}

func TestBuildMessageTruncationInsideBlockquoteStaysWellFormed(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	// Long enough that the cut lands inside the blockquote's converted content.
	huge := strings.Repeat("日志", 5000)
	got := BuildMessage("t", huge, "S2", nil, false, now)

	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() produced %d runes, must be <= %d", n, MaxMessageRunes)
	}
	if !isWellFormedHTML(got) {
		t.Errorf("BuildMessage() truncated inside blockquote produced malformed HTML: %q", got[:200])
	}
	if strings.Count(got, "<blockquote") != strings.Count(got, "</blockquote>") {
		t.Errorf("BuildMessage() left the blockquote unbalanced, got %q", got[len(got)-200:])
	}
}

func TestBuildMessageRendersAtUsersAsTgLinks(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	users := []models.AtUser{
		{Name: "Bruce", TelegramID: "12345"},
		{Name: "NoTelegram", TelegramID: ""},
	}
	got := BuildMessage("t", "c", "S2", users, false, now)

	if !strings.Contains(got, `<a href="tg://user?id=12345">Bruce</a>`) {
		t.Errorf("BuildMessage() should link contacts with a telegram id, got %q", got)
	}
	if strings.Contains(got, "NoTelegram") {
		t.Errorf("BuildMessage() should skip contacts without a telegram id, got %q", got)
	}
}

func TestBuildMessageAtAllDegradesToText(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	got := BuildMessage("t", "c", "S1", nil, true, now)
	if !strings.Contains(got, "所有人") {
		t.Errorf("BuildMessage() with atAll should mention 所有人, got %q", got)
	}
}

func TestBuildMessageStaysUnderTelegramLimit(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	huge := strings.Repeat("日志", 5000) // 10000 runes
	got := BuildMessage("t", huge, "S2", nil, false, now)
	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() produced %d runes, must be <= %d", n, MaxMessageRunes)
	}
}

func TestBuildMessageTruncatesMarkdownWithWellFormedHTML(t *testing.T) {
	// Test that truncating a long message with markdown still produces well-formed HTML
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	// Long content with markdown: bold, code, link
	longContent := strings.Repeat("**bold**", 200) + "`code`" + "[link](https://example.com)" + strings.Repeat("日志", 500)
	got := BuildMessage("长标题", longContent, "S1", nil, false, now)

	// Must stay under limit
	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() produced %d runes, must be <= %d", n, MaxMessageRunes)
	}

	// Must be well-formed HTML
	if !isWellFormedHTML(got) {
		t.Errorf("BuildMessage() with markdown truncation produced malformed HTML: %q", got)
	}
}

func TestBuildMessageNestedBoldInCode(t *testing.T) {
	// Test nesting: bold inside code span: `**x**`
	// This produces <code><b>x</b></code> (code is outer, bold is inner).
	// Truncation must close tags in LIFO order: </b> then </code>.
	// Built to exceed 4096 runes so truncation actually happens mid-nested-content.
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)

	// Create enough padding before nested part to push cut point inside the bold text
	padding := strings.Repeat("日志内容", 1000) // ~2000 runes
	// Nested markup with bold text inside code (cut will land here)
	nestedContent := strings.Repeat("x", 1000) // 1000 runes inside bold
	nested := "`**" + nestedContent + "**`"
	// Trailing content to ensure there's something to cut
	trailing := strings.Repeat("日志", 500)
	content := padding + nested + trailing

	got := BuildMessage("标题", content, "S2", nil, false, now)

	// Verify truncation actually happened (marker is present)
	if !strings.Contains(got, "已截断") {
		t.Errorf("TestBuildMessageNestedBoldInCode: expected truncation marker, got %d runes (no truncation): %q",
			len([]rune(got)), got[len(got)-200:])
	}

	// Must be well-formed HTML with proper nesting (closing order matters)
	if !isWellFormedHTML(got) {
		t.Errorf("BuildMessage() with nested bold-in-code produced malformed HTML")
	}

	// Must stay under limit
	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() produced %d runes, must be <= %d", n, MaxMessageRunes)
	}

	// Verify LIFO closing order: must NOT contain </code></b> (wrong order)
	if strings.Contains(got, "</code></b>") {
		t.Errorf("BuildMessage() closing tags in wrong order (fixed-order bug): found </code></b>")
	}
}

func TestBuildMessageNestedBoldInLink(t *testing.T) {
	// Test nesting: bold inside link anchor: [**text**](url)
	// This produces <a href="url"><b>text</b></a> (link is outer, bold is inner).
	// Truncation must close tags in LIFO order: </b> then </a>.
	// Built to exceed 4096 runes so truncation actually happens mid-nested-content.
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)

	// Create enough padding before nested part to push cut point inside the bold text
	padding := strings.Repeat("日志内容", 1000) // ~2000 runes
	// Nested markup with bold text inside link (cut will land here)
	boldContent := strings.Repeat("y", 1000) // 1000 runes inside bold
	nested := "[**" + boldContent + "**](https://example.com)"
	// Trailing content to ensure there's something to cut
	trailing := strings.Repeat("内容", 500)
	content := padding + nested + trailing

	got := BuildMessage("标题", content, "S2", nil, false, now)

	// Verify truncation actually happened (marker is present)
	if !strings.Contains(got, "已截断") {
		t.Errorf("TestBuildMessageNestedBoldInLink: expected truncation marker, got %d runes (no truncation): %q",
			len([]rune(got)), got[len(got)-200:])
	}

	// Must be well-formed HTML with proper nesting (closing order matters)
	if !isWellFormedHTML(got) {
		t.Errorf("BuildMessage() with nested bold-in-link produced malformed HTML")
	}

	// Must stay under limit
	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() produced %d runes, must be <= %d", n, MaxMessageRunes)
	}

	// Verify LIFO closing order: must NOT contain </a></b> (wrong order)
	if strings.Contains(got, "</a></b>") {
		t.Errorf("BuildMessage() closing tags in wrong order (fixed-order bug): found </a></b>")
	}
}

func TestTruncateRunesKeepsMarkerWhenCutLandsInsideTag(t *testing.T) {
	// The 23rd rune (max 30 minus the 7-rune marker) falls inside "<code>", so
	// the well-formedness pass has to back up past the half-written tag. Backing
	// up must not take the truncation marker with it.
	input := strings.Repeat("x", 20) + "<code>abc</code>"
	got := TruncateRunes(input, 30)

	if !strings.Contains(got, "(已截断)") {
		t.Errorf("TruncateRunes() = %q, the truncation marker was lost", got)
	}
	if strings.Contains(got, "<co") && !strings.Contains(got, "<code>") {
		t.Errorf("TruncateRunes() = %q, left a half-written tag behind", got)
	}
	if !isWellFormedHTML(got) {
		t.Errorf("TruncateRunes() produced malformed HTML: %q", got)
	}
	if n := len([]rune(got)); n > 30 {
		t.Errorf("TruncateRunes() produced %d runes, must be <= 30", n)
	}
}

func TestBuildMessageLongBodyPreservesMentionAndTimestamp(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	// ~10000 runes, comfortably forcing truncation of the body.
	huge := strings.Repeat("日志", 5000)
	users := []models.AtUser{{Name: "Bruce", TelegramID: "12345"}}

	got := BuildMessage("t", huge, "S1", users, false, now)

	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() produced %d runes, must be <= %d", n, MaxMessageRunes)
	}
	if !strings.Contains(got, `<a href="tg://user?id=12345">Bruce</a>`) {
		t.Errorf("BuildMessage() truncated a long body but lost the mention, got tail %q", tailOf(got, 200))
	}
	if !strings.Contains(got, "<i>🕐 2026-09-07 15:04:05 (+00:00 UTC)</i>") {
		t.Errorf("BuildMessage() truncated a long body but lost the timestamp, got tail %q", tailOf(got, 200))
	}
	if !strings.Contains(got, "已截断") {
		t.Errorf("BuildMessage() should still carry the truncation marker, got %q", got)
	}
}

func TestBuildMessageLongBodyPreservesAtAllAndTimestamp(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	huge := strings.Repeat("日志", 5000)

	got := BuildMessage("t", huge, "S1", nil, true, now)

	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() produced %d runes, must be <= %d", n, MaxMessageRunes)
	}
	if !strings.Contains(got, "所有人") {
		t.Errorf("BuildMessage() truncated a long body but lost the atAll mention, got tail %q", tailOf(got, 200))
	}
	if !strings.Contains(got, "<i>🕐 2026-09-07 15:04:05 (+00:00 UTC)</i>") {
		t.Errorf("BuildMessage() truncated a long body but lost the timestamp, got tail %q", tailOf(got, 200))
	}
	if !strings.Contains(got, "已截断") {
		t.Errorf("BuildMessage() should still carry the truncation marker, got %q", got)
	}
}

func TestBuildMessagePathologicalTitleStaysSafe(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	// The title alone, once wrapped in <b>...</b>, already exceeds
	// MaxMessageRunes: this drives the body budget to zero/negative, the
	// degenerate case the fix has to guard against.
	hugeTitle := strings.Repeat("标", 5000)
	users := []models.AtUser{{Name: "Bruce", TelegramID: "12345"}}

	got := BuildMessage(hugeTitle, "some body content", "S1", users, false, now)

	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("BuildMessage() with a pathological title produced %d runes, must be <= %d", n, MaxMessageRunes)
	}
	if !isWellFormedHTML(got) {
		t.Errorf("BuildMessage() with a pathological title produced malformed HTML: %q", got[:200])
	}
}

// tailOf returns the last n runes of s, or all of s if it is shorter, for
// readable failure messages without slicing mid-rune.
func tailOf(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

func TestTruncateRunesClosesExpandableBlockquoteAsPlainBlockquote(t *testing.T) {
	// extractTagName cuts an opening tag at the first space, so an attribute
	// like "expandable" must not leak into the tag name used for closing:
	// "<blockquote expandable>" has to pair with "</blockquote>", never
	// "</blockquote expandable>" (which Telegram would reject as unknown markup).
	// Budget is chosen so the cut lands after the opening tag has fully formed
	// (it is 24 runes long) but inside the "x" filler, so there is genuinely an
	// open <blockquote expandable> tag for closeOpenTags to close.
	input := "<blockquote expandable>" + strings.Repeat("x", 100) + "</blockquote>"
	got := TruncateRunes(input, 50)

	if strings.Contains(got, "</blockquote expandable>") {
		t.Errorf("TruncateRunes() closed with attributes still attached: %q", got)
	}
	if !strings.Contains(got, "</blockquote>") {
		t.Errorf("TruncateRunes() should close the blockquote, got %q", got)
	}
	if !isWellFormedHTML(got) {
		t.Errorf("TruncateRunes() produced malformed HTML for blockquote expandable: %q", got)
	}
	if n := len([]rune(got)); n > 50 {
		t.Errorf("TruncateRunes() produced %d runes, must be <= 50", n)
	}
}

func TestMarkdownToHTMLConvertsFencedBlockToPre(t *testing.T) {
	md := "**服务:** api\n```\nline one\nline two\n```"
	got := MarkdownToHTML(md)

	if !strings.Contains(got, "<pre>line one\nline two</pre>") {
		t.Errorf("MarkdownToHTML() = %q, want a <pre> block", got)
	}
	if !strings.Contains(got, "<b>服务:</b>") {
		t.Errorf("text outside the fence must still convert, got %q", got)
	}
}

func TestFencedBlockContentIsEscapedButNotConverted(t *testing.T) {
	// Real log lines contain markdown-looking characters constantly. Inside a
	// code block they must survive verbatim, only HTML-escaped.
	md := "```\nassert x != **ptr\nsee [docs](http://x) and `q`\nvalue <nil>\n```"
	got := MarkdownToHTML(md)

	for _, unwanted := range []string{"<b>", "<a href", "<code>"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("fence content was markup-converted (%s present): %q", unwanted, got)
		}
	}
	if !strings.Contains(got, "**ptr") {
		t.Errorf("a literal ** should survive inside the fence: %q", got)
	}
	if !strings.Contains(got, "value &lt;nil&gt;") {
		t.Errorf("fence content must still be HTML-escaped: %q", got)
	}
}

func TestFencedBlockWithLanguageTag(t *testing.T) {
	got := MarkdownToHTML("```log\nboom\n```")
	if !strings.Contains(got, "boom") {
		t.Errorf("content lost: %q", got)
	}
	if strings.Contains(got, ">log") {
		t.Errorf("the language tag leaked into the block body: %q", got)
	}
}

func TestBuildMessageSkipsBlockquoteWhenBodyHasCodeBlock(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	got := BuildMessage("支付超时", "```\nstack line\n```", "S2", nil, false, now)

	if strings.Contains(got, "<blockquote") {
		t.Errorf("a code block is its own grouping; no blockquote wanted: %q", got)
	}
	if !strings.Contains(got, "<pre>") {
		t.Errorf("the code block is missing: %q", got)
	}
	if !strings.Contains(got, "2026-09-07 15:04:05") {
		t.Errorf("timestamp lost: %q", got)
	}
}

func TestBuildMessageStillBlockquotesAPlainBody(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	got := BuildMessage("支付超时", "**服务:** api", "S2", nil, false, now)
	if !strings.Contains(got, "<blockquote") {
		t.Errorf("a plain body should still be grouped in a blockquote: %q", got)
	}
}

func TestTruncationInsideACodeBlockStaysWellFormed(t *testing.T) {
	now := time.Date(2026, 9, 7, 15, 4, 5, 0, time.UTC)
	var b strings.Builder
	b.WriteString("```\n")
	for i := 0; i < 400; i++ {
		b.WriteString("\tat com.acme.very.long.package.Frame.method(Frame.java:1234)\n")
	}
	b.WriteString("```")

	got := BuildMessage("支付超时", b.String(), "S1", nil, false, now)

	if n := len([]rune(got)); n > MaxMessageRunes {
		t.Errorf("produced %d runes, must be <= %d", n, MaxMessageRunes)
	}
	if !isWellFormedHTML(got) {
		t.Errorf("truncation inside a <pre> produced malformed HTML")
	}
	if !strings.Contains(got, "已截断") {
		t.Error("truncation marker missing")
	}
}
