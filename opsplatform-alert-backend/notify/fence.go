package notify

import (
	"regexp"
	"strings"
)

// fenceSpanRe finds a fenced block's full span, from the opening ``` to the
// nearest following ```. (?s) lets "." match newlines, since a fenced block
// spans lines by definition. The middle is captured whole (group 1); any
// language tag inside it is pulled apart in Go code below, not in the regex,
// because telling a language tag apart from content that merely starts with
// letters/digits needs a judgement call a single regex cannot express safely
// (see extractLangAndRest).
var fenceSpanRe = regexp.MustCompile("(?s)```(.*?)```")

// fenceLineJunkRe matches whitespace and invisible characters sitting between
// the start of a line and a ``` delimiter.
//
// Operators do not type alert templates from scratch — they copy them out of
// chat, a wiki or a ticket, and those carry passengers. A zero-width space
// (U+200B) in front of a fence is the worst of them: invisible in the textarea,
// invisible in the rendered card, and it breaks exactly one of the two
// platforms. Telegram's fence regex is not anchored to a line, so it still
// finds the delimiter and renders the block; Lark's renderer requires the
// delimiter to start its line, so it prints the fence as text and then pairs
// the NEXT delimiter as an opening one — every block from there on is shifted
// by one, swallowing the fields between them.
//
// Leading spaces and tabs are stripped for the same reason: four spaces before
// a closing delimiter stops it closing anything.
var fenceLineJunkRe = regexp.MustCompile("(?m)^[ \t\u200b\u200c\u200d\u2060\ufeff]+(?:```)")

// NormalizeFencedBlocks rewrites every fenced code block in s so its opening
// and closing ``` delimiters each sit on their own line:
//
//	```lang<content>```   ->  ```lang\n<content>\n```
//	```\n<content>```     ->  ```\n<content>\n```
//	```<content>\n```     ->  ```\n<content>\n```
//	```\n<content>\n```   ->  unchanged
//
// Why this exists: an alert template that writes a fence inline, e.g.
// "```{{.stack}}```", renders to a fence whose delimiters share a line with
// content. Standard markdown (and Lark's renderer) requires fence delimiters
// on their own line; a delimiter glued to content is not recognised as a
// fence at all, or leaves the closing ``` printed as literal text. Telegram's
// own fence regex tolerates the glued shape, which is exactly why this defect
// only ever showed up on Lark. Normalizing once, before content reaches any
// platform, makes both platforms see the same canonical shape.
//
// Text with no fence, or a fence with no matching close (an odd number of
// ``` markers), is returned unchanged: there is nothing to safely rewrite.
func NormalizeFencedBlocks(s string) string {
	// Clear anything sitting in front of a delimiter before pairing: a fence
	// that does not start its own line is not a fence to Lark, and pairing
	// around it produces blocks shifted by one.
	s = fenceLineJunkRe.ReplaceAllString(s, "```")

	matches := fenceSpanRe.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}

	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(s[last:m[0]])
		middle := s[m[2]:m[3]]
		out.WriteString(normalizeFenceMiddle(middle))
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String()
}

// normalizeFenceMiddle rebuilds one fence's middle (everything between the
// opening and closing ```) with exactly one newline after the opening
// delimiter (and its optional language tag) and exactly one newline before
// the closing delimiter.
func normalizeFenceMiddle(middle string) string {
	lang, rest := extractLangAndRest(middle)

	// Collapse a single existing boundary newline on each side, so an
	// already-correct block is reproduced byte-for-byte (no doubled blank
	// lines) instead of gaining a second newline on top of its existing one.
	rest = strings.TrimPrefix(rest, "\r\n")
	rest = strings.TrimPrefix(rest, "\n")
	rest = strings.TrimSuffix(rest, "\r\n")
	rest = strings.TrimSuffix(rest, "\n")

	return "```" + lang + "\n" + rest + "\n```"
}

// extractLangAndRest splits an optional language tag off the front of a
// fence's middle content, returning ("", middle) when none can be safely
// identified.
//
// A candidate tag is a run of letters/digits/_/+/- starting with a LETTER
// (never a digit or hyphen). It is only treated as an actual language tag —
// and split out onto its own line — when it is followed by real whitespace
// or a newline, i.e. something that already reads as a separator between the
// tag and the content that follows. When the candidate instead runs directly
// into more content with no separator at all (e.g. a stack trace starting
// with "java.lang.RuntimeException" or a log line starting with "2026-09-07
// 20:15:03 ERROR..."), it is NOT extracted: the leading digit in a timestamp
// already fails the "starts with a letter" test, and a bare class name like
// "java" is rejected because a "." immediately follows it with no whitespace.
// Left ambiguous, content is always the safe choice — a language tag is
// cosmetic, but silently relocating a chunk of real alert content on top of
// the fence's opening line would be data loss.
func extractLangAndRest(middle string) (lang, rest string) {
	if middle == "" || !isLangStart(middle[0]) {
		return "", middle
	}

	i := 1
	for i < len(middle) && isLangChar(middle[i]) {
		i++
	}

	// Skip any run of spaces/tabs right after the candidate token — that is
	// still part of the separator, not content, mirroring how a fence's
	// language tag is written in practice ("```log content", "```log\n...").
	j := i
	for j < len(middle) && (middle[j] == ' ' || middle[j] == '\t') {
		j++
	}

	if j >= len(middle) {
		// The candidate token (plus any trailing spaces) is all there is;
		// nothing follows it to confirm it as a tag rather than the whole
		// of a short piece of content.
		return "", middle
	}

	if middle[j] == '\n' || middle[j] == '\r' {
		return middle[:i], middle[j:]
	}
	if i != j {
		// Real whitespace (but no newline) separated the token from more
		// content on the same line — still an unambiguous boundary.
		return middle[:i], middle[j:]
	}

	// The token runs straight into more content with nothing between them.
	return "", middle
}

func isLangStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isLangChar(c byte) bool {
	return isLangStart(c) || (c >= '0' && c <= '9') || c == '_' || c == '+' || c == '-'
}

// FencedBlock wraps content in a markdown code fence whose delimiters each sit
// on their own line — the canonical shape NormalizeFencedBlocks produces and
// the only shape Lark's renderer accepts.
//
// Use it wherever the backend itself formats log text, so operators get a code
// block without having to remember to write the fence into every template.
//
// The content is sanitised first: a log line that itself contains ``` would
// otherwise close the fence early, and everything after it would render as
// markdown — mangling the rest of the card. Both consumers of a fence in this
// codebase (NormalizeFencedBlocks here, and telegram.MarkdownToHTML's fenceRe)
// hardcode a three-backtick delimiter, so a longer outer fence is not an
// option; neutralising the inner run is.
func FencedBlock(content string) string {
	content = strings.Trim(content, "\r\n")
	return "```\n" + neutralizeFences(content) + "\n```"
}

// neutralizeFences breaks up any run of three or more backticks so it can no
// longer terminate a fence. A single space is inserted after the second
// backtick: no character of the original is dropped, and the run stays legible
// as backticks to anyone reading the log.
func neutralizeFences(s string) string {
	if !strings.Contains(s, "```") {
		return s
	}
	var out strings.Builder
	run := 0
	for _, r := range s {
		if r == '`' {
			run++
			out.WriteRune(r)
			if run == 2 {
				out.WriteByte(' ')
				run = 0
			}
			continue
		}
		run = 0
		out.WriteRune(r)
	}
	return out.String()
}
