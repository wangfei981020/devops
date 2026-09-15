package alert

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"opsplatform-alert-backend/timezone"
)

// Defaults for the log-context feature. They mirror the column defaults in the
// schema, so a rule row written before this feature existed still behaves
// sensibly once the switch is turned on.
const (
	DefaultLogContextBefore       = 25
	DefaultLogContextAfter        = 50
	DefaultLogContextMaxWindowSec = 1800
	// DefaultLogContextDisplayLines is the budget for ONE hit's context block.
	//
	// It was 20 when a message carried a single context block; an aggregated
	// alert shows up to three hits, each with its own, so the figure now
	// multiplies. 30 per hit is still far inside the channel: measured against
	// this platform's own log lines (~137 bytes once JSON-escaped), Lark's 30KB
	// card holds roughly 200 lines, so three hits at 30 spend well under half.
	//
	// ⚠️ Telegram is the real ceiling at 4096 RUNES — about 28 lines of the same
	// logs, i.e. barely one hit's worth. Rules delivering to Telegram should set
	// this to 10 or lower; the channel truncates rather than failing, and the
	// notices ride at the top of the block precisely so they survive that cut.
	DefaultLogContextDisplayLines = 30

	// MaxLogContextLines caps what a rule may ask for per direction.
	//
	// Loki rejects a query whose limit exceeds its max_entries_limit (5000 by
	// default) with HTTP 400 — the whole query fails, so an over-configured rule
	// gets NO context at all rather than a large one. That ceiling is per
	// deployment and can be set lower, so this sits well under it: a thousand
	// lines is already far past anything an alert can show or a person can read,
	// and staying clear of the server's limit is worth more than honouring an
	// unreasonable number.
	MaxLogContextLines = 1000

	// MaxContextFetchesPerRun caps how many of a rule's matched lines get their
	// context fetched in one run.
	//
	// Each one costs up to two Loki queries per direction rung, and the engine
	// shares a single global pool of query slots across every rule on the
	// platform. A rule matching fifty lines would otherwise fire a few hundred
	// extra queries per cycle, holding slots that every other rule is waiting
	// for and stretching its own cycle past its schedule. Ten is well above what
	// anyone reads in one alert and far below what starves the pool.
	//
	// Lines past the cap still alert — they just say so instead of carrying
	// context, which is the honest trade: an alert without context beats a
	// delayed alert, and a silent omission beats neither.
	MaxContextFetchesPerRun = 10

	// MaxPreviewContextFetches is the same cap for an interactive preview, set
	// lower because a preview blocks a person at a screen under a 15s deadline
	// and three rendered hits already show what the switch does.
	MaxPreviewContextFetches = 3
)

// PreviewContextSkippedNote explains a bare context in a preview. It says the
// limit is the PREVIEW's, not the rule's, so nobody reads it as "this hit will
// have no context when it really fires".
func PreviewContextSkippedNote(cap int) string {
	return fmt.Sprintf("（预览只为前 %d 条命中行拉取上下文，此条未拉取；真实告警不受此限制，每轮最多 %d 条）",
		cap, MaxContextFetchesPerRun)
}

// RenderContextSkipped is what a matched line gets instead of its context once
// the per-run cap is reached.
//
// It still hands over the selector and the instant to look around, built from
// the hit that is already in memory — no query, so the note costs nothing that
// the cap was imposed to save. Saying only "skipped" would leave the reader
// with an alert they cannot follow up on, which is the failure the cap is
// supposed to avoid, not cause.
func RenderContextSkipped(hit map[string]interface{}, cap int) string {
	note := fmt.Sprintf("（本轮命中行较多，已超过单轮上下文查询上限 %d 条，此条未取上下文）", cap)

	labels, _ := hit["__stream_labels"].(map[string]string)
	selector := buildStreamSelector(labels)
	at, ok := hitInstant(hit)
	if selector == "" || !ok {
		return note
	}
	local := at.In(timezone.Location())
	return fmt.Sprintf("%s\n📖 自行查看：\n   时间点    %s (%s)\n   查询语句  %s",
		note, local.Format(timezone.DisplayLayout), local.Format("-07:00"), selector)
}

// logContextSteps is the escalating window ladder, in seconds.
//
// Log rate varies by orders of magnitude between services: a busy API container
// writes 25 lines in a couple of seconds, while a service idling overnight may
// need half an hour to produce that many. A single fixed window cannot serve
// both — sized for the quiet service it makes every query on the busy one scan
// a range hundreds of times larger than needed, and Loki's index lookup is
// charged by that range even though the chunk read stops early once `limit`
// entries are in hand.
//
// So the query escalates instead: try the smallest window first and stop as
// soon as the requested line count is satisfied. The busy service — the common
// case — is served by one 30-second query; only a genuinely quiet stream walks
// up the ladder, and a quiet stream is cheap to scan precisely because it holds
// so little.
var logContextSteps = []int{30, 120, 600, 1800}

// WindowLadder returns the escalating windows to try, never exceeding maxSec.
// The final entry is always exactly maxSec, so the caller's configured ceiling
// is always attempted before giving up.
func WindowLadder(maxSec int) []time.Duration {
	if maxSec <= 0 {
		maxSec = DefaultLogContextMaxWindowSec
	}
	out := make([]time.Duration, 0, len(logContextSteps)+1)
	for _, s := range logContextSteps {
		if s >= maxSec {
			break
		}
		out = append(out, time.Duration(s)*time.Second)
	}
	return append(out, time.Duration(maxSec)*time.Second)
}

// ContextBlock is the collected neighbourhood of one matched log line.
//
// Before and After are both in chronological order (oldest first), so rendering
// is a straight concatenation: Before, then Hit, then After — the same order a
// person reading a log tail expects.
type ContextBlock struct {
	Before []string
	Hit    string
	After  []string

	// WantBefore/WantAfter are what the rule asked for. They are kept so the
	// rendered block can say "asked for 25, the stream only had 9" rather than
	// silently showing a short block that reads like a complete one.
	WantBefore int
	WantAfter  int

	// HitTime is the matched line's own timestamp, and Window* are the widths
	// the ladder actually settled on. Together they produce the time range
	// printed in the footer, which is what an operator pastes into Grafana.
	HitTime      time.Time
	WindowBefore time.Duration
	WindowAfter  time.Duration

	// Selector is the LogQL stream selector the context was read from, printed
	// in the footer so the operator can re-run the exact same query by hand.
	Selector string

	// FailedBefore/FailedAfter record that the ladder stopped because a query
	// ERRORED, not because the stream ran dry.
	//
	// The difference matters more than it looks. Both end with fewer lines than
	// asked for, but one means "the service wrote nothing else" and the other
	// means "we could not find out". Reporting a failure as an exhausted stream
	// tells the operator the logs do not exist when they may well do — the exact
	// false negative this feature exists to prevent.
	FailedBefore bool
	FailedAfter  bool
}

// Presentation constants.
//
// The band exists because a bare ">>>" prefix does not survive a reader's eye
// scanning twenty near-identical log lines — the marker has to break the shape
// of the block, not just decorate one line. The band is plain ASCII on purpose:
// box-drawing and full-width characters render at inconsistent widths across
// Lark's and Telegram's monospace fonts, so a "line" drawn with them comes out
// ragged.
//
// The indent is the second, independent signal: every context line is indented
// four spaces and only the matched line starts at column zero. If a channel's
// renderer ever eats the band, the indent alone still picks the hit out.
const (
	logCtxHitPrefix  = ">>> "
	logCtxLinePrefix = "    "
	logCtxBandTop    = "=================== ↓↓↓ 命中行 ↓↓↓ ==================="
	logCtxBandBottom = "======================================================"
)

// hitInstant recovers the exact instant a hit was logged at.
//
// It reads __ts_nano, the raw nanosecond timestamp ToHits preserves alongside
// the rendered one. The rendered hit["timestamp"] cannot serve here: it is a
// display string carrying a zone suffix ("2026-09-09 22:04:54 (+08:00
// Asia/Shanghai)") and only second granularity, so parsing it back would both
// fail on the suffix and lose the sub-second position a context query anchors
// on. Hits from Elasticsearch carry no such key, which is the second reason
// this returns a flag rather than a zero time: the log-context feature is
// Loki-only and must decline quietly rather than query an arbitrary range.
func hitInstant(hit map[string]interface{}) (time.Time, bool) {
	switch v := hit["__ts_nano"].(type) {
	case int64:
		return time.Unix(0, v), true
	case int:
		return time.Unix(0, int64(v)), true
	case float64:
		// A hit that has been through a JSON round-trip (the preview and
		// test-send handlers do exactly that) arrives with its numbers widened
		// to float64.
		return time.Unix(0, int64(v)), true
	}
	return time.Time{}, false
}

// LogContextCaption labels the appended block on the zero-config path.
//
// It says the timestamps inside are the source system's own, for the same
// reason StackCaption does: the alert's time above is rendered in the
// platform's display timezone while these lines are reproduced exactly as they
// arrived, and two clocks in one message mislead unless labelled.
const LogContextCaption = "日志上下文（时间为来源系统时间）："

// logCtxVarPattern matches a template's reference to the log-context variable,
// allowing the optional inner spacing Go's text/template accepts.
var logCtxVarPattern = regexp.MustCompile(`\{\{\s*\.logcontext\s*\}\}`)

// ApplyLogContextToMessage injects a fetched log context into a message that was
// already rendered from tmpl.
//
// It mirrors ApplyStackToMessage exactly, and exists for the same reason: the
// namespaced path renders its message before the context is known, so
// {{.logcontext}} cannot be a live template variable at render time and Go's
// text/template has already left the literal "<no value>" where the operator
// referenced it. Substitution is gated on the template actually naming the
// variable, so a "<no value>" produced by some OTHER missing variable — the
// operator's own template bug — is not silently overwritten with a log dump.
func ApplyLogContextToMessage(message, tmpl, logctx string) string {
	if logctx == "" {
		return message
	}
	if logCtxVarPattern.MatchString(tmpl) {
		// The operator laid this out themselves; do not add a caption to it.
		return strings.Replace(message, "<no value>", logctx, 1)
	}
	return message + "\n" + LogContextCaption + "\n```\n" + logctx + "\n```"
}

// ApplyContextsToMessage fills a pre-rendered message's placeholders with the
// stack and the log context together, in the order the template asked for them.
//
// It replaces the pair of single-variable helpers on the namespaced path, and
// exists because they cannot be composed. Each one replaces the FIRST
// "<no value>" it finds, and that token says nothing about which variable left
// it: a template reading "{{.logcontext}} … {{.stack}}" renders two identical
// tokens, so applying the stack first drops it into the log context's slot and
// the two contents come out swapped. Deciding the order from the TEMPLATE —
// where the variables are distinguishable — is what makes the assignment right.
//
// Each replacement advances a cursor past the text it just inserted, so content
// that happens to contain the token itself (a log line quoting "<no value>")
// cannot swallow the next variable's slot.
//
// A variable the template never referenced is appended in a fenced block
// instead, exactly as the single-variable helpers do.
func ApplyContextsToMessage(message, tmpl, stack, logctx string) string {
	type slot struct {
		at      int
		content string
	}
	var slots []slot
	if stack != "" {
		if loc := stackVarPattern.FindStringIndex(tmpl); loc != nil {
			slots = append(slots, slot{at: loc[0], content: stack})
		}
	}
	if logctx != "" {
		if loc := logCtxVarPattern.FindStringIndex(tmpl); loc != nil {
			slots = append(slots, slot{at: loc[0], content: logctx})
		}
	}
	// Template order, not call order.
	sort.Slice(slots, func(i, j int) bool { return slots[i].at < slots[j].at })

	cursor := 0
	for _, sl := range slots {
		idx := strings.Index(message[cursor:], noValueToken)
		if idx < 0 {
			break
		}
		idx += cursor
		message = message[:idx] + sl.content + message[idx+len(noValueToken):]
		cursor = idx + len(sl.content)
	}

	// Whatever the template did not reference gets appended, captioned.
	if stack != "" && !stackVarPattern.MatchString(tmpl) {
		message += "\n" + StackCaption + "\n```\n" + stack + "\n```"
	}
	if logctx != "" && !logCtxVarPattern.MatchString(tmpl) {
		message += "\n" + LogContextCaption + "\n```\n" + logctx + "\n```"
	}
	return message
}

// noValueToken is what Go's text/template renders for a missing map key. The
// namespaced path's message is rendered before the contexts are known, so this
// token is the only trace left of where the operator asked for them.
const noValueToken = "<no value>"

// TrimAroundHit cuts an over-long context down to maxLines, keeping the lines
// NEAREST the matched line and dropping the outer ends.
//
// This is the opposite of ElideMiddle, which the stack-trace path uses, and the
// difference is not an oversight. A stack's value sits at its two ends: the
// exception type at the top and the root cause ("Caused by:") at the bottom, so
// the middle is what goes. A log neighbourhood's value decays with distance
// from the match: the line immediately before the error is what explains it,
// while the line twenty-five back is unrelated traffic. So here the middle is
// exactly what must survive.
//
// budget is the number of CONTEXT lines that fit — the matched line's own
// height is already deducted by the caller, since a merged multi-line record
// occupies more than one.
//
// The budget is split in proportion to what the rule asked for, so a rule
// configured 25-before/50-after keeps roughly twice as many lines after the hit
// as before it. Returned counts say how many lines were dropped on each side.
func TrimAroundHit(before, after []string, wantBefore, wantAfter, budget int) (keptBefore, keptAfter []string, hiddenBefore, hiddenAfter int) {
	if budget < 0 {
		budget = 0
	}
	if len(before)+len(after) <= budget {
		return before, after, 0, 0
	}

	// Split the budget by the configured ratio, not by what was collected: the
	// operator's before/after numbers are the statement of intent, and honouring
	// them keeps the shown window's shape stable even when one side came back
	// short.
	total := wantBefore + wantAfter
	budgetBefore := budget / 2
	if total > 0 {
		budgetBefore = budget * wantBefore / total
	}
	// Neither side should be starved to zero while the other has room to spare.
	if budgetBefore == 0 && budget > 1 && len(before) > 0 {
		budgetBefore = 1
	}
	budgetAfter := budget - budgetBefore

	// A side that came back short hands its unused budget to the other, so a
	// thin "before" does not waste the space a rich "after" could use.
	if len(before) < budgetBefore {
		budgetAfter += budgetBefore - len(before)
		budgetBefore = len(before)
	}
	if len(after) < budgetAfter {
		budgetBefore += budgetAfter - len(after)
		budgetAfter = len(after)
		if budgetBefore > len(before) {
			budgetBefore = len(before)
		}
	}

	// Keep the TAIL of before (closest to the hit) and the HEAD of after.
	keptBefore = before[len(before)-budgetBefore:]
	keptAfter = after[:budgetAfter]
	return keptBefore, keptAfter, len(before) - budgetBefore, len(after) - budgetAfter
}

// Render turns a collected block into the text that goes inside the alert's
// fenced code block.
//
// Everything — the lines, the truncation notice and the footer — is returned as
// one string destined for a single fence. Two reasons: {{.logcontext}} follows
// the same convention as {{.stack}} (bare content, fenced by whoever places
// it), so an operator template that wraps the variable in its own fence cannot
// produce a nested, broken one; and a footer inside the code block is
// selectable as plain text, which is the point of printing a LogQL query the
// operator is meant to copy.
func (b ContextBlock) Render(maxLines int) string {
	if b.Hit == "" {
		return ""
	}

	// The matched line is frequently a merged multi-line record, and it is the
	// alert's subject: it is shown whole regardless. Charging its real height to
	// the budget is what keeps the configured display limit honest — counting it
	// as one line let a fifty-line stack quietly render fifty lines past it.
	hitLines := strings.Count(b.Hit, "\n") + 1
	budget := maxLines - hitLines
	if budget < 0 {
		budget = 0
	}

	keptBefore, keptAfter, hiddenBefore, hiddenAfter := TrimAroundHit(
		b.Before, b.After, b.WantBefore, b.WantAfter, budget)

	var sb strings.Builder

	// Notices and the footer lead the block rather than close it.
	//
	// Telegram caps a message at 4096 runes and cuts the overflow off the END,
	// which is exactly where this information used to sit — so the one alert
	// that most needed to say "22 lines were omitted, here is the query to see
	// them" was the one that dropped it. Whatever survives truncation is the
	// head, so the caveats go there, and the log lines — which the reader can
	// tell are cut simply by them stopping — take the risk instead.
	if notice := b.truncationNotice(hiddenBefore, hiddenAfter, len(keptBefore)+len(keptAfter)+hitLines); notice != "" {
		sb.WriteString(notice)
	}
	if short := b.shortfallNotice(); short != "" {
		sb.WriteString(short)
	}
	if footer := b.footer(); footer != "" {
		sb.WriteString(footer)
	}
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}

	for _, l := range keptBefore {
		sb.WriteString(logCtxLinePrefix + l + "\n")
	}
	sb.WriteString(logCtxBandTop + "\n")
	// Every line of the hit carries the marker, not just the first. A matched
	// record is frequently multi-line — the collector merging a Java stack into
	// one record is the common case the stack-context path is built around — and
	// prefixing only the first line would leave the rest sitting at column zero,
	// indistinguishable from nothing at all while the context lines around them
	// are indented.
	for _, hl := range strings.Split(b.Hit, "\n") {
		sb.WriteString(logCtxHitPrefix + hl + "\n")
	}
	sb.WriteString(logCtxBandBottom + "\n")
	for _, l := range keptAfter {
		sb.WriteString(logCtxLinePrefix + l + "\n")
	}

	return strings.TrimRight(sb.String(), "\n")
}

// truncationNotice states what was cut. It reports the collected total, what is
// on screen, and how much was dropped on each side separately — a single
// "truncated" word would leave the reader unable to tell whether the missing
// context is before the error or after it.
func (b ContextBlock) truncationNotice(hiddenBefore, hiddenAfter, shown int) string {
	if hiddenBefore == 0 && hiddenAfter == 0 {
		return ""
	}
	collected := len(b.Before) + len(b.After) + 1
	return fmt.Sprintf(
		"\n⚠️ 上下文实取 %d 行，受渠道消息长度限制仅展示命中行附近 %d 行\n"+
			"   （向前另有 %d 行、向后另有 %d 行未展示，保留的是离命中行最近的部分）\n",
		collected, shown, hiddenBefore, hiddenAfter)
}

// shortfallNotice fires when the ladder ran to its ceiling and the stream still
// did not hold the requested number of lines.
//
// Without it a short block is indistinguishable from a truncated one, and an
// operator reading "only 9 lines before the error" cannot tell whether the
// query gave up early or the service genuinely wrote nothing else. Saying which
// window was exhausted answers that, and tells them the knob to turn.
func (b ContextBlock) shortfallNotice() string {
	// A failed query is reported first and separately: it is the one case where
	// the missing lines say nothing about the log stream itself.
	var failed []string
	if b.FailedBefore {
		failed = append(failed, "向前")
	}
	if b.FailedAfter {
		failed = append(failed, "向后")
	}
	if len(failed) > 0 {
		return fmt.Sprintf("\n❌ %s上下文查询失败，已取到的部分如上 —— 这不代表日志不存在，请用下方查询语句自行确认\n",
			strings.Join(failed, "、"))
	}

	shortBefore := len(b.Before) < b.WantBefore
	shortAfter := len(b.After) < b.WantAfter
	if !shortBefore && !shortAfter {
		return ""
	}
	parts := make([]string, 0, 2)
	if shortBefore {
		parts = append(parts, fmt.Sprintf("向前只取到 %d/%d 行", len(b.Before), b.WantBefore))
	}
	if shortAfter {
		parts = append(parts, fmt.Sprintf("向后只取到 %d/%d 行", len(b.After), b.WantAfter))
	}
	return fmt.Sprintf("\nℹ️ %s —— 该 stream 在最大 %s 的时间窗内已无更多日志（非查询失败；需要更多请调大上下文时间窗上限）\n",
		strings.Join(parts, "、"), formatWindow(maxDuration(b.WindowBefore, b.WindowAfter)))
}

// footer prints the exact time range and selector the context came from, so the
// operator can pull the untruncated version themselves.
func (b ContextBlock) footer() string {
	if b.HitTime.IsZero() || b.Selector == "" {
		return ""
	}
	loc := timezone.Location()
	from := b.HitTime.Add(-b.WindowBefore).In(loc)
	to := b.HitTime.Add(b.WindowAfter).In(loc)
	return fmt.Sprintf("\n📖 完整上下文：\n   时间范围  %s ~ %s (%s)\n   查询语句  %s\n",
		from.Format(timezone.DisplayLayout), to.Format(timezone.DisplayLayout),
		from.Format("-07:00"), b.Selector)
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// formatWindow renders a ladder window the way an operator would say it.
func formatWindow(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%g 小时", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%g 分钟", d.Minutes())
	default:
		return fmt.Sprintf("%g 秒", d.Seconds())
	}
}
