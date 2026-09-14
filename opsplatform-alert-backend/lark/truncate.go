package lark

import (
	"encoding/json"
	"strings"
)

// maxCardBytes caps the JSON body posted to a Lark webhook.
//
// Lark's own rejection reads "The message exceeds the size limit of 30KB"
// (code 19036), so 30KB is the limit the platform states. Probing the live
// endpoint shows it actually accepts well past that — 150KB went through, 200KB
// did not — but that headroom is undocumented, unexplained, and free to change
// or differ per tenant. Sizing to the stated limit is the difference between a
// truncated alert and a rejected one, and a rejected one is silent: the engine
// records a send error and the on-call group receives nothing at all.
//
// A card this size is already far past readable; the cap exists so an
// unbounded stack trace cannot cost the whole alert.
const maxCardBytes = 30 * 1024

// cardTruncationMarker tells the reader the card was cut rather than the
// service having stopped logging mid-stack.
const cardTruncationMarker = "\n…(已截断)"

// truncateCardContent returns the longest prefix of content whose assembled
// card still fits maxCardBytes.
//
// It measures a marshalled candidate rather than the content string, because
// what Lark weighs is the whole JSON body: the title, the @mentions, the
// footer, and every backslash JSON escaping adds to the log text all count.
// Estimating from rune length instead would have to guess an escaping factor,
// and guessing low means the send still fails.
func truncateCardContent(build func(string) map[string]interface{}, content string) string {
	r := []rune(content)

	fits := func(keep int) bool {
		body, err := json.Marshal(build(finishTruncated(string(r[:keep]))))
		return err == nil && len(body) <= maxCardBytes
	}

	// Binary search for the longest prefix that fits. Cutting a fixed number of
	// runes per round instead would need an escaping factor to converge, which
	// is the guess this avoids.
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	// finishTruncated can add a fence-closing line, so a prefix is not strictly
	// monotonic in size and the search can land one step over. Walk back until
	// it genuinely fits; the loop is bounded because keep reaches 0.
	for lo > 0 && !fits(lo) {
		lo--
	}

	return finishTruncated(string(r[:lo]))
}

// finishTruncated closes anything the cut left open and appends the marker.
//
// A cut landing inside a fenced code block would otherwise leave the fence
// unterminated, and Lark renders everything after an unclosed ``` as code —
// including the truncation marker, which is exactly the part that has to read
// as a note from us rather than as more log text.
func finishTruncated(s string) string {
	if strings.Count(s, "```")%2 == 1 {
		s += "\n```"
	}
	return s + cardTruncationMarker
}
