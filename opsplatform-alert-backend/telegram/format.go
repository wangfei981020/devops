package telegram

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"opsplatform-alert-backend/models"
	"opsplatform-alert-backend/timezone"
)

// MaxMessageRunes is Telegram's sendMessage limit. Exceeding it returns HTTP 400.
const MaxMessageRunes = 4096

var (
	larkAtTagRe = regexp.MustCompile(`<at [^>]*>(.*?)</at>`)

	// Lark's renderer accepts a few raw HTML tags inline. Telegram's does not,
	// and its parse_mode rejects unknown tags outright, so anything that
	// reached EscapeHTML would show up as literal &lt;font ...&gt; text. Both
	// are therefore unwrapped BEFORE escaping: <font> loses the tag and keeps
	// its text, <a> becomes the markdown link form so linkRe can render it.
	fontTagRe   = regexp.MustCompile(`(?is)</?font[^>]*>`)
	rawAnchorRe = regexp.MustCompile(`(?is)<a\s+href=["']?([^"'>\s]+)["']?[^>]*>(.*?)</a>`)

	boldRe   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	strikeRe = regexp.MustCompile(`~~(.+?)~~`)
	// A single-asterisk emphasis, deliberately stricter than the bold rule: the
	// delimiters must hug non-space characters. Alert text says things like
	// "S1 * 命中 2 条", and treating that as emphasis would swallow the middle
	// of the line. Runs after boldRe, by which point ** pairs are already gone.
	italicRe = regexp.MustCompile(`\*(\S|\S[^*\n]*?\S)\*`)

	codeRe = regexp.MustCompile("`([^`]+)`")
	// Images must be matched before links: they differ only by a leading "!",
	// so linkRe would otherwise consume the link half and strand the "!".
	imageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	linkRe  = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

	// Line-level rules. They run last, on already-escaped text: EscapeHTML
	// touches none of the characters they key on (#, -, *, _).
	headingRe = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]+(.+?)[ \t]*$`)
	hrRe      = regexp.MustCompile(`(?m)^[ \t]*(?:-{3,}|\*{3,}|_{3,})[ \t]*$`)
	bulletRe  = regexp.MustCompile(`(?m)^([ \t]*)[-*+][ \t]+`)

	// (?s) so . matches newlines — a fenced block spans lines by definition.
	fenceRe = regexp.MustCompile("(?s)```[ \\t]*([A-Za-z0-9_+-]*)[ \\t]*\r?\n?(.*?)```")
)

// horizontalRule stands in for a markdown "---". Telegram HTML has no <hr>,
// and the raw dashes render as three stray hyphens.
const horizontalRule = "──────────"

// EscapeHTML escapes HTML special characters for Telegram's HTML parse_mode.
// It escapes &, <, and >, and also double-quotes because they appear in
// quoted attribute values (e.g., href="...").
func EscapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}

// MarkdownToHTML converts the Lark-flavoured markdown used in alert templates
// into Telegram HTML.
//
// Fenced blocks are handled FIRST and separately: their content is only
// HTML-escaped, never markup-converted. Log lines contain `**`, backticks and
// bracketed text constantly; converting those would corrupt the log and, worse,
// leave unbalanced tags behind.
func MarkdownToHTML(md string) string {
	var out strings.Builder
	last := 0

	for _, m := range fenceRe.FindAllStringSubmatchIndex(md, -1) {
		out.WriteString(convertOutsideFence(md[last:m[0]]))

		body := md[m[4]:m[5]] // capture group 2: the block's content
		body = strings.TrimSuffix(body, "\n")
		out.WriteString("<pre>")
		out.WriteString(EscapeHTML(body))
		out.WriteString("</pre>")

		last = m[1]
	}
	out.WriteString(convertOutsideFence(md[last:]))

	return out.String()
}

// convertOutsideFence handles a run of text between fenced blocks. Markdown
// tables are pulled out and escaped into a <pre> of their own, for the same
// reason a fence is: their alignment is the whole point, and Telegram renders
// everything but <pre>/<code> in a proportional font, where a table's pipes
// line up with nothing. Whatever is left goes through the inline rules.
//
// Note that a table, like a fence, means BuildMessage will not wrap the body
// in a blockquote — <pre> does not nest reliably inside one.
func convertOutsideFence(s string) string {
	if s == "" {
		return ""
	}
	// Cheap reject: no pipe means no table, which is the overwhelming majority
	// of alert bodies.
	if !strings.Contains(s, "|") {
		return convertInline(s)
	}

	lines := strings.Split(s, "\n")
	var segments, plain []string
	flush := func() {
		if len(plain) > 0 {
			segments = append(segments, convertInline(strings.Join(plain, "\n")))
			plain = nil
		}
	}

	for i := 0; i < len(lines); i++ {
		end, ok := tableBlockEnd(lines, i)
		if !ok {
			plain = append(plain, lines[i])
			continue
		}
		flush()
		segments = append(segments, "<pre>"+EscapeHTML(strings.Join(lines[i:end], "\n"))+"</pre>")
		i = end - 1
	}
	flush()

	return strings.Join(segments, "\n")
}

// tableBlockEnd reports whether a markdown table starts at lines[i], and where
// it ends (exclusive). A table is a row containing a pipe, a separator row
// beneath it, and every following row that still contains a pipe.
//
// Requiring the separator row is what keeps ordinary alert text out: a line
// like "级别: S1 | 命中: 2 条" has a pipe but nothing resembling |---|---|
// under it.
func tableBlockEnd(lines []string, i int) (int, bool) {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") || !isTableSeparator(lines[i+1]) {
		return 0, false
	}
	end := i + 2
	for end < len(lines) && strings.Contains(lines[end], "|") {
		end++
	}
	return end, true
}

// isTableSeparator matches a row made only of pipes, dashes, alignment colons
// and whitespace, e.g. "|---|:--:|". A bare "---" is a horizontal rule, not a
// separator, so at least one pipe is required.
func isTableSeparator(s string) bool {
	dashes, pipes := 0, 0
	for _, r := range s {
		switch r {
		case '-':
			dashes++
		case '|':
			pipes++
		case ':', ' ', '\t', '\r':
		default:
			return false
		}
	}
	return dashes >= 3 && pipes >= 1
}

// convertInline applies the inline and line-level markup rules to a run of
// text that is NOT inside a fenced block or a table.
//
// Order matters twice over: the raw-HTML unwrapping has to precede escaping
// (afterwards there is no tag left to recognise, only escaped text), and
// images have to precede links (they differ only by a leading "!").
func convertInline(md string) string {
	if md == "" {
		return ""
	}
	// Lark @tags carry no meaning on Telegram; keep the display name only.
	md = larkAtTagRe.ReplaceAllString(md, "$1")
	// Lark colours text with <font>; Telegram has no equivalent, so keep the
	// text and drop the colour rather than printing the tag at the reader.
	md = fontTagRe.ReplaceAllString(md, "")
	md = rawAnchorRe.ReplaceAllString(md, "[$2]($1)")

	md = EscapeHTML(md)

	md = boldRe.ReplaceAllString(md, "<b>$1</b>")
	md = strikeRe.ReplaceAllString(md, "<s>$1</s>")
	md = italicRe.ReplaceAllString(md, "<i>$1</i>")
	md = codeRe.ReplaceAllString(md, "<code>$1</code>")

	// Telegram cannot inline an image in a text message. A link labelled with
	// the alt text is the honest degradation; an image with no alt text is
	// labelled with its URL so the link is not left blank.
	md = imageRe.ReplaceAllStringFunc(md, func(m string) string {
		g := imageRe.FindStringSubmatch(m)
		alt, src := g[1], g[2]
		if strings.TrimSpace(alt) == "" {
			alt = src
		}
		return fmt.Sprintf(`<a href="%s">%s</a>`, src, alt)
	})
	md = linkRe.ReplaceAllString(md, `<a href="$2">$1</a>`)

	// Line-level rules last. The horizontal rule goes before the bullet rule:
	// "---" is not a bullet, but "- " is, and rewriting in the other order
	// would turn a rule into "• --".
	md = hrRe.ReplaceAllString(md, horizontalRule)
	md = headingRe.ReplaceAllString(md, "<b>$1</b>")
	md = bulletRe.ReplaceAllString(md, "$1• ")

	return md
}

// TruncateRunes cuts s to max runes (not bytes) and appends a marker.
// It ensures the result is well-formed HTML by backing up if cutting inside a tag
// and closing any open tags.
func TruncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	const marker = "\n…(已截断)"
	keep := max - len([]rune(marker))
	if keep < 0 {
		keep = 0
	}

	truncated := truncateAt(r, keep, marker)

	// If we exceed the limit due to closing tags, remove more content
	for len([]rune(truncated)) > max {
		keep--
		if keep < 0 {
			break
		}
		truncated = truncateAt(r, keep, marker)
	}

	return truncated
}

// truncateAt builds one truncation candidate. The order matters: a cut landing
// inside a generated tag is trimmed back BEFORE the marker is appended, because
// trimming afterwards takes the marker with it (the marker sits after the
// half-written tag) and yields a truncated message carrying no truncation mark.
func truncateAt(r []rune, keep int, marker string) string {
	return closeOpenTags(trimPartialTag(string(r[:keep])) + marker)
}

// trimPartialTag drops a trailing half-written tag such as `…<b` or `…<a href="`.
func trimPartialTag(s string) string {
	lastOpen := strings.LastIndex(s, "<")
	if lastOpen != -1 && strings.LastIndex(s, ">") < lastOpen {
		return s[:lastOpen]
	}
	return s
}

// closeOpenTags closes any unclosed tags in LIFO order so the result is valid HTML.
func closeOpenTags(s string) string {
	// Find open tags in the order they appear and close in reverse (LIFO)
	openTags := findOpenTagsInOrder(s)

	// Close in reverse order (LIFO: innermost first)
	for i := len(openTags) - 1; i >= 0; i-- {
		s += "</" + openTags[i] + ">"
	}

	return s
}

// findOpenTagsInOrder scans the string and returns the names of open tags
// in the order they were opened, maintaining the stack discipline for proper LIFO closing.
func findOpenTagsInOrder(s string) []string {
	var stack []string
	i := 0
	for i < len(s) {
		if s[i] == '<' {
			// Find the end of the tag
			end := strings.Index(s[i:], ">")
			if end == -1 {
				break
			}
			tag := s[i : i+end+1]

			if strings.HasPrefix(tag, "</") {
				// Closing tag: pop from stack
				tagName := extractTagName(tag)
				if len(stack) > 0 && stack[len(stack)-1] == tagName {
					stack = stack[:len(stack)-1]
				}
			} else if !strings.HasPrefix(tag, "<!") {
				// Opening tag (not a comment or doctype)
				tagName := extractTagName(tag)
				stack = append(stack, tagName)
			}

			i += end + 1
		} else {
			i++
		}
	}

	return stack
}

// extractTagName extracts the tag name from an HTML tag string.
// Handles <b>, </b>, <code>, </code>, <a href="...">, </a>.
func extractTagName(tag string) string {
	tag = strings.TrimPrefix(tag, "<")
	tag = strings.TrimSuffix(tag, ">")

	if strings.HasPrefix(tag, "/") {
		tag = strings.TrimPrefix(tag, "/")
	}

	// For <a href="...">, <a ...>, extract just "a"
	spaceIdx := strings.Index(tag, " ")
	if spaceIdx != -1 {
		tag = tag[:spaceIdx]
	}

	return tag
}

// SeverityPrefix mirrors the emoji the Lark card uses, since Telegram has no
// coloured card header to convey severity.
func SeverityPrefix(severity string) string {
	switch severity {
	case "S3", "info":
		return "⚠️"
	case "S2", "warning":
		return "🔴🔴"
	case "recovery":
		return "✅"
	case "report":
		return ""
	default: // S1, critical, empty
		return "🚨🚨🚨"
	}
}

// BuildMessage assembles the final HTML body, guaranteed to fit Telegram's limit.
//
// Mentions are what pages the on-call engineer, and the timestamp says when the
// alert fired; an alert big enough to need truncating is exactly the alert
// where paging matters most. So truncation must never be a flat cut of the
// whole assembled string — that discards the tail (mentions, then timestamp)
// first, which is backwards. Instead the head (severity prefix + title) and
// the tail (mentions + timestamp) are built first and reserved outright; only
// the body — the least essential part — is fit to whatever budget remains.
func BuildMessage(title, content, severity string, atUsers []models.AtUser, atAll bool, now time.Time) string {
	var head strings.Builder
	if prefix := SeverityPrefix(severity); prefix != "" {
		head.WriteString(prefix)
		head.WriteString(" ")
	}
	head.WriteString("<b>")
	head.WriteString(EscapeHTML(title))
	head.WriteString("</b>\n")
	headStr := head.String()

	var tail strings.Builder
	if mentions := buildMentions(atUsers, atAll); mentions != "" {
		tail.WriteString("\n")
		tail.WriteString(mentions)
	}
	tail.WriteString(fmt.Sprintf("\n<i>🕐 %s</i>", EscapeHTML(timezone.FormatWithZone(now))))
	tailStr := tail.String()

	body := MarkdownToHTML(content)

	// A code block already is a visual group, and Telegram's blockquote does not
	// reliably nest around <pre>. Use one or the other, never both. This same
	// hasCodeBlock decision drives the budget below, so the tag actually
	// written and the tag length reserved for can never disagree.
	hasCodeBlock := strings.Contains(body, "<pre>")

	// The quote is never marked `expandable`. Telegram renders an expandable
	// blockquote collapsed, showing the first few lines behind a "show more"
	// chevron — which on an alert hides exactly the part an on-call engineer
	// opened the chat to read (the error code, the log line), and hides it
	// most aggressively on the biggest alerts. Whatever the body costs in
	// scroll, it has to be readable without a tap.
	var openTag, closeTag string
	if !hasCodeBlock {
		openTag = "<blockquote>"
		closeTag = "</blockquote>"
	}

	reserved := len([]rune(headStr)) + len([]rune(tailStr)) + len([]rune(openTag)) + len([]rune(closeTag))
	bodyBudget := MaxMessageRunes - reserved

	var truncatedBody string
	if bodyBudget > 0 {
		truncatedBody = TruncateRunes(body, bodyBudget)
	}
	// else: a pathological title and/or an enormous mention list already
	// consume the whole budget by themselves (bodyBudget <= 0). There is no
	// room left for any body content, so render an empty-but-valid blockquote
	// rather than calling TruncateRunes with a non-positive budget — its
	// truncation marker alone is a handful of runes, so it does not promise
	// to fit into a budget that small, let alone a negative one.

	msg := headStr + openTag + truncatedBody + closeTag + tailStr

	// Last-resort safety net. This only fires in the same pathological
	// scenario noted above, where head+tail already exceed MaxMessageRunes on
	// their own — something shrinking the body cannot fix, since the body is
	// already empty by this point. Falling back to the flat whole-string cut
	// guarantees the hard limit is never violated and the HTML stays
	// well-formed; in every normal case (reserved parts fit) this is a no-op.
	if len([]rune(msg)) > MaxMessageRunes {
		msg = TruncateRunes(msg, MaxMessageRunes)
	}

	return msg
}

// buildMentions renders @mentions. Telegram has no @all, so atAll degrades to
// plain text; contacts without a telegram_id are skipped rather than rendered
// as a dead link.
func buildMentions(atUsers []models.AtUser, atAll bool) string {
	if atAll {
		return "📢 所有人"
	}
	var parts []string
	for _, u := range atUsers {
		if u.TelegramID == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf(`<a href="tg://user?id=%s">%s</a>`,
			EscapeHTML(u.TelegramID), EscapeHTML(u.Name)))
	}
	return strings.Join(parts, " ")
}
