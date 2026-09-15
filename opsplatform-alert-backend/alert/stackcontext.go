package alert

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DefaultBoundaryPattern recognises the start of a NEW log record: a leading
// date, a leading clock time, or a leading level word. A line that does not look
// like a new record is a continuation of the current one — which is what a stack
// frame is.
const DefaultBoundaryPattern = `^(\d{4}-\d{2}-\d{2}|\d{2}:\d{2}:\d{2}|\[?(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)\b)`

// CompileBoundary compiles a rule's boundary pattern, falling back to the
// default when the rule does not override it.
func CompileBoundary(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		pattern = DefaultBoundaryPattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid stack boundary pattern %q: %w", pattern, err)
	}
	return re, nil
}

// CollectStack takes the matched line plus the lines that follow it, and returns
// the matched line together with its continuation lines.
//
// The first line is always kept: it is the match itself and it normally DOES look
// like a new record, so testing it against the boundary would return an empty
// stack every time. maxLines is a hard stop, so a boundary pattern that never
// matches cannot drag the whole stream into an alert.
//
// maxLines here is a runaway-prevention safety valve, not presentation
// trimming — it must stay generous. A tight cap applied at collection time cuts
// off the BOTTOM of a real stack (where "Caused by:" lives) before ElideMiddle
// ever sees it, which defeats the whole point of head/tail elision. Presentation
// trimming belongs to ElideMiddle alone, applied AFTER the full stack (up to
// this safety valve) has been collected.
func CollectStack(lines []string, boundary *regexp.Regexp, maxLines int) []string {
	if len(lines) == 0 {
		return nil
	}
	if maxLines <= 0 {
		maxLines = 200
	}

	out := []string{lines[0]}
	for _, line := range lines[1:] {
		if len(out) >= maxLines {
			break
		}
		if boundary.MatchString(line) {
			break
		}
		out = append(out, line)
	}
	return out
}

// ElideMiddle keeps the head and the tail of an over-long stack and replaces the
// middle with a marker saying how much went missing.
//
// Reading a stack, the useful parts are exactly those two ends: the top carries
// the exception type and the nearest frames, the bottom carries the root cause
// ("Caused by"). The framework plumbing in between is noise.
func ElideMiddle(lines []string, head, tail int) []string {
	if head < 0 {
		head = 0
	}
	if tail < 0 {
		tail = 0
	}
	if len(lines) <= head+tail {
		return lines
	}

	elided := len(lines) - head - tail
	out := make([]string, 0, head+tail+1)
	out = append(out, lines[:head]...)
	out = append(out, fmt.Sprintf("… 省略 %d 行 …", elided))
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

// stackVarPattern matches a template's reference to the stack variable,
// allowing the optional inner spacing Go's text/template accepts:
// {{.stack}} or {{ .stack }}.
var stackVarPattern = regexp.MustCompile(`\{\{\s*\.stack\s*\}\}`)

// ApplyStackToMessage injects a fetched stack trace into a message that was
// already rendered from tmpl.
//
// This exists for paths where the message must be rendered before the stack
// is known (see the comment at the namespaced-path call site in engine.go),
// so {{.stack}} cannot be a live template variable at render time. Go's
// text/template renders a reference to a missing map key as the literal
// "<no value>", so when the operator's template DOES reference the stack
// variable, rendering has already left that placeholder sitting in message;
// this substitutes the real stack into it, exactly where the operator asked
// for it. When the template does NOT reference the variable, the stack is
// appended in a fenced block instead, same as before.
//
// The substitution is gated on the template actually referencing {{.stack}}
// (or {{ .stack }}): a "<no value>" left behind by some OTHER missing
// variable is the operator's own template bug and must not be silently
// overwritten with a stack trace.
func ApplyStackToMessage(message, tmpl, stack string) string {
	if stack == "" {
		return message
	}
	if stackVarPattern.MatchString(tmpl) {
		// The operator laid this out themselves; do not add a caption to it.
		return strings.Replace(message, "<no value>", stack, 1)
	}
	return message + "\n" + StackCaption + "\n```\n" + stack + "\n```"
}

// StackCaption labels the appended block. The timestamps inside a stack come
// from whatever system wrote the log and are reproduced exactly as they
// arrived, while the alert's own time above is rendered in the platform's
// display timezone. Two clocks in one message is only confusing when nobody
// says which is which, so this says it.
const StackCaption = "错误栈（原文，时间为来源系统时间）："

// nonSelectableLabels are labels Loki attaches to a query RESULT but will not
// match on in a stream selector.
//
// "detected_level" is Loki's own query-time level detection (Loki 3.x): it is
// present on every stream it returns, yet a selector naming it matches NOTHING.
// Verified against production: the identical selector returns 500 lines without
// it and zero lines with it. Feeding a result's labels straight back into a
// selector — which is exactly what a context query does — therefore produced a
// query that could never match, and did so silently: an empty result is
// indistinguishable from "this stream has no other lines".
//
// Labels prefixed with "__" are Loki internals (__error__, __error_details__)
// and are excluded for the same reason.
var nonSelectableLabels = map[string]bool{
	"detected_level": true,
}

// buildStreamSelector turns a hit's stream labels back into a LogQL selector.
// Labels are sorted so the generated query is stable and cache-friendly, and
// labels Loki will not match on are dropped (see nonSelectableLabels).
func buildStreamSelector(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if nonSelectableLabels[k] || strings.HasPrefix(k, "__") {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
