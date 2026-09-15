package alert

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	lokiclient "opsplatform-alert-backend/loki"
	"opsplatform-alert-backend/models"
)

func TestWindowLadderStopsAtCeiling(t *testing.T) {
	tests := []struct {
		name   string
		maxSec int
		want   []time.Duration
	}{
		{
			// The default ceiling is the last rung, so the ladder is exactly the
			// built-in steps with no duplicate at the end.
			name:   "default ceiling",
			maxSec: DefaultLogContextMaxWindowSec,
			want:   []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute},
		},
		{
			// A ceiling between two rungs truncates the ladder and finishes on
			// the operator's own number, never on a larger built-in one.
			name:   "ceiling between rungs",
			maxSec: 300,
			want:   []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute},
		},
		{
			// A ceiling below the first rung must still produce one attempt, or
			// a rule configured tight would query nothing at all.
			name:   "ceiling below first rung",
			maxSec: 10,
			want:   []time.Duration{10 * time.Second},
		},
		{
			name:   "zero falls back to the default",
			maxSec: 0,
			want:   []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WindowLadder(tt.maxSec)
			if len(got) != len(tt.want) {
				t.Fatalf("WindowLadder(%d) = %v, want %v", tt.maxSec, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("WindowLadder(%d) = %v, want %v", tt.maxSec, got, tt.want)
				}
			}
			// Whatever the ceiling, no rung may exceed it: overshooting is the
			// cost this whole ladder exists to avoid.
			ceiling := time.Duration(tt.maxSec) * time.Second
			if tt.maxSec == 0 {
				ceiling = DefaultLogContextMaxWindowSec * time.Second
			}
			for _, d := range got {
				if d > ceiling {
					t.Fatalf("rung %v exceeds ceiling %v", d, ceiling)
				}
			}
		})
	}
}

// TestTrimAroundHitKeepsNearestLines pins the behaviour that distinguishes this
// from the stack path's ElideMiddle: a log neighbourhood's value decays with
// distance from the match, so the lines next to the hit must survive and the
// far ends must go.
func TestTrimAroundHitKeepsNearestLines(t *testing.T) {
	before := []string{"b1", "b2", "b3", "b4", "b5", "b6", "b7", "b8", "b9", "b10"}
	after := []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10"}

	// 6 = 7 显示行预算 - 1 行命中行
	keptBefore, keptAfter, hiddenBefore, hiddenAfter := TrimAroundHit(before, after, 25, 50, 6)

	// The kept "before" lines must be the TAIL of the slice (nearest the hit),
	// never the head — taking the head would hand back the oldest, least
	// relevant traffic and drop the line that actually explains the error.
	if len(keptBefore) > 0 && keptBefore[len(keptBefore)-1] != "b10" {
		t.Errorf("kept before = %v, want it to end at b10 (nearest the hit)", keptBefore)
	}
	// Likewise the kept "after" lines must be the HEAD.
	if len(keptAfter) > 0 && keptAfter[0] != "a1" {
		t.Errorf("kept after = %v, want it to start at a1 (nearest the hit)", keptAfter)
	}
	if total := len(keptBefore) + len(keptAfter); total > 6 {
		t.Errorf("kept %d context lines, over the budget of 6", total)
	}
	// A 25/50 rule asks for twice as much after as before, so the after side
	// must get the larger share of a tight budget.
	if len(keptAfter) <= len(keptBefore) {
		t.Errorf("before=%d after=%d, want after to get the larger share for a 25/50 rule",
			len(keptBefore), len(keptAfter))
	}
	if hiddenBefore != len(before)-len(keptBefore) || hiddenAfter != len(after)-len(keptAfter) {
		t.Errorf("hidden counts wrong: hiddenBefore=%d hiddenAfter=%d", hiddenBefore, hiddenAfter)
	}
}

// TestTrimAroundHitNoTrimWhenWithinBudget guards against a truncation notice
// appearing on a block that was never truncated.
func TestTrimAroundHitNoTrimWhenWithinBudget(t *testing.T) {
	before := []string{"b1", "b2"}
	after := []string{"a1", "a2"}
	keptBefore, keptAfter, hiddenBefore, hiddenAfter := TrimAroundHit(before, after, 25, 50, 19)
	if len(keptBefore) != 2 || len(keptAfter) != 2 || hiddenBefore != 0 || hiddenAfter != 0 {
		t.Errorf("within budget should pass through untouched, got before=%v after=%v hidden=%d/%d",
			keptBefore, keptAfter, hiddenBefore, hiddenAfter)
	}
}

// TestTrimAroundHitLendsUnusedBudget: a side that came back short should not
// strand its share of the budget while the other side is being cut.
func TestTrimAroundHitLendsUnusedBudget(t *testing.T) {
	before := []string{"b1"} // only one line available
	after := []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8"}
	keptBefore, keptAfter, _, _ := TrimAroundHit(before, after, 25, 50, 6)
	if len(keptBefore) != 1 {
		t.Fatalf("kept before = %v, want all 1 available line", keptBefore)
	}
	// Budget 6 for context lines, before used 1, so after should take 5.
	if len(keptAfter) != 5 {
		t.Errorf("kept after = %d lines, want 5 (the budget before could not use)", len(keptAfter))
	}
}

func TestRenderMarksTheHitLine(t *testing.T) {
	b := ContextBlock{
		Before:     []string{"line before"},
		Hit:        "ERROR boom",
		After:      []string{"line after"},
		WantBefore: 1,
		WantAfter:  1,
	}
	out := b.Render(20)

	// Two independent signals mark the hit, so that a channel renderer eating
	// one of them still leaves the line findable.
	if !strings.Contains(out, logCtxBandTop) || !strings.Contains(out, logCtxBandBottom) {
		t.Errorf("render is missing the hit band:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "ERROR boom"):
			if !strings.HasPrefix(line, logCtxHitPrefix) {
				t.Errorf("hit line is not prefixed with %q: %q", logCtxHitPrefix, line)
			}
		case strings.Contains(line, "line before"), strings.Contains(line, "line after"):
			if !strings.HasPrefix(line, logCtxLinePrefix) {
				t.Errorf("context line is not indented: %q", line)
			}
		}
	}
}

// TestRenderNoticesOnlyWhenEarned: the notices are the feature's honesty
// guarantee, so each must appear exactly when its condition holds and never
// otherwise.
func TestRenderNoticesOnlyWhenEarned(t *testing.T) {
	full := ContextBlock{
		Before: []string{"b1", "b2"}, Hit: "hit", After: []string{"a1", "a2"},
		WantBefore: 2, WantAfter: 2,
	}
	if out := full.Render(20); strings.Contains(out, "⚠️") || strings.Contains(out, "ℹ️") {
		t.Errorf("a complete, untrimmed block must carry no notice:\n%s", out)
	}

	truncated := ContextBlock{
		Before: make([]string, 25), Hit: "hit", After: make([]string, 50),
		WantBefore: 25, WantAfter: 50,
	}
	out := truncated.Render(20)
	if !strings.Contains(out, "⚠️") {
		t.Errorf("a trimmed block must say so:\n%s", out)
	}
	if !strings.Contains(out, "实取 76 行") {
		t.Errorf("notice must report the collected total (76):\n%s", out)
	}
	if strings.Contains(out, "ℹ️") {
		t.Errorf("a block that got everything it asked for must not claim a shortfall:\n%s", out)
	}

	// A short block is not a truncated one, and must be reported as the stream
	// running dry rather than as a display limit.
	short := ContextBlock{
		Before: []string{"b1"}, Hit: "hit", After: []string{"a1"},
		WantBefore: 25, WantAfter: 50, WindowBefore: 30 * time.Minute, WindowAfter: 30 * time.Minute,
	}
	out = short.Render(20)
	if !strings.Contains(out, "ℹ️") || !strings.Contains(out, "向前只取到 1/25 行") {
		t.Errorf("a short block must report the shortfall per side:\n%s", out)
	}
	if strings.Contains(out, "⚠️") {
		t.Errorf("a short block was never trimmed, so it must not claim truncation:\n%s", out)
	}
}

func TestRenderFooterCarriesTheQuery(t *testing.T) {
	hit := time.Date(2026, 9, 9, 14, 4, 54, 0, time.UTC)
	b := ContextBlock{
		Hit: "boom", WantBefore: 0, WantAfter: 0,
		HitTime:      hit,
		WindowBefore: 30 * time.Second,
		WindowAfter:  30 * time.Second,
		Selector:     `{namespace="g66-openapi", pod="openapi-backend-66f4bbb4f5-xlscd"}`,
	}
	out := b.Render(20)
	if !strings.Contains(out, b.Selector) {
		t.Errorf("footer must print the selector so it can be re-run by hand:\n%s", out)
	}
	if !strings.Contains(out, "完整上下文") {
		t.Errorf("footer missing:\n%s", out)
	}
}

func TestRenderEmptyHitProducesNothing(t *testing.T) {
	if out := (ContextBlock{}).Render(20); out != "" {
		t.Errorf("a block with no hit must render empty, got %q", out)
	}
}

// TestApplyLogContextToMessage mirrors the stack variant's contract: substitute
// where the operator asked, append otherwise, and never overwrite a "<no value>"
// left by some other missing variable.
func TestApplyLogContextToMessage(t *testing.T) {
	tests := []struct {
		name           string
		message, tmpl  string
		logctx         string
		wantContains   string
		wantNotContain string
	}{
		{
			name:         "substitutes where the template asked",
			message:      "服务异常\n<no value>",
			tmpl:         "服务异常\n{{.logcontext}}",
			logctx:       "CTX",
			wantContains: "服务异常\nCTX",
			// The operator placed it themselves, so no caption is added.
			wantNotContain: LogContextCaption,
		},
		{
			name:         "tolerates inner spacing",
			message:      "x\n<no value>",
			tmpl:         "x\n{{ .logcontext }}",
			logctx:       "CTX",
			wantContains: "x\nCTX",
		},
		{
			name:         "appends when the template never referenced it",
			message:      "服务异常",
			tmpl:         "服务异常",
			logctx:       "CTX",
			wantContains: LogContextCaption,
		},
		{
			name:    "leaves another variable's <no value> alone",
			message: "服务异常\n<no value>",
			// The template references no log context at all, so this
			// "<no value>" belongs to some other missing key and is the
			// operator's own bug — not a slot to fill with a log dump.
			// The context is still appended (that is the zero-config path),
			// but the foreign placeholder must survive untouched.
			tmpl:         "服务异常\n{{.somethingelse}}",
			logctx:       "CTX",
			wantContains: "服务异常\n<no value>\n" + LogContextCaption,
		},
		{
			name:         "empty context changes nothing",
			message:      "服务异常",
			tmpl:         "服务异常\n{{.logcontext}}",
			logctx:       "",
			wantContains: "服务异常",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyLogContextToMessage(tt.message, tt.tmpl, tt.logctx)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("got %q, want it to contain %q", got, tt.wantContains)
			}
			if tt.wantNotContain != "" && strings.Contains(got, tt.wantNotContain) {
				t.Errorf("got %q, want it NOT to contain %q", got, tt.wantNotContain)
			}
		})
	}
}

// TestHitInstant: the context query anchors on the exact nanosecond, so this
// must read the preserved raw timestamp and must decline — rather than guess —
// when the key is absent, which is how an Elasticsearch hit arrives.
func TestHitInstant(t *testing.T) {
	want := time.Date(2026, 9, 9, 14, 4, 54, 219000000, time.UTC)

	if got, ok := hitInstant(map[string]interface{}{"__ts_nano": want.UnixNano()}); !ok || !got.Equal(want) {
		t.Errorf("int64: got %v ok=%v, want %v", got, ok, want)
	}
	// A hit that has been through JSON (preview / test-send) widens to float64,
	// which cannot hold a nanosecond epoch exactly — roughly 64ns of slack. That
	// is accepted (it is far below log resolution, and dropAdjacentHit no longer
	// depends on the anchor being exact), but it must stay slack and not drift.
	got, ok := hitInstant(map[string]interface{}{"__ts_nano": float64(want.UnixNano())})
	if !ok {
		t.Fatal("float64 timestamp must be accepted")
	}
	if d := got.Sub(want); d > time.Microsecond || d < -time.Microsecond {
		t.Errorf("float64: got %v, want within 1µs of %v (off by %v)", got, want, d)
	}
	// The rendered display string must NOT be used as a fallback: it has lost
	// sub-second precision and carries a zone suffix no layout parse can read.
	if _, ok := hitInstant(map[string]interface{}{"timestamp": "2026-09-09 22:04:54 (+08:00 Asia/Shanghai)"}); ok {
		t.Error("a hit with only the rendered timestamp must be declined, not parsed")
	}
	if _, ok := hitInstant(map[string]interface{}{}); ok {
		t.Error("a hit with no timestamp at all must be declined")
	}
}

// TestDropAdjacentHit pins the content-based rule: the matched line is removed
// only when it is actually there, at the one position it could occupy.
func TestDropAdjacentHit(t *testing.T) {
	const hit = "ERROR boom"

	// Forward: start is inclusive, so the hit normally leads the result.
	if got := dropAdjacentHit([]string{hit, "a1", "a2"}, hit, "forward"); len(got) != 2 || got[0] != "a1" {
		t.Errorf("forward with hit present: got %v, want the leading hit removed", got)
	}
	// Forward without the hit (the anchor drifted past it): nothing may be
	// dropped, or a real neighbour is lost.
	if got := dropAdjacentHit([]string{"a1", "a2"}, hit, "forward"); len(got) != 2 || got[0] != "a1" {
		t.Errorf("forward without hit: got %v, want it untouched", got)
	}

	// Backward (already flipped into reading order): end is exclusive, so the
	// hit is normally absent — and the nearest neighbour must survive.
	if got := dropAdjacentHit([]string{"b1", "b2"}, hit, "backward"); len(got) != 2 || got[len(got)-1] != "b2" {
		t.Errorf("backward without hit: got %v, want it untouched (b2 is the most valuable line)", got)
	}
	// But if a drifted anchor did pull it in, it sits at the tail.
	if got := dropAdjacentHit([]string{"b1", "b2", hit}, hit, "backward"); len(got) != 2 || got[len(got)-1] != "b2" {
		t.Errorf("backward with hit present: got %v, want the trailing hit removed", got)
	}

	if got := dropAdjacentHit(nil, hit, "forward"); len(got) != 0 {
		t.Errorf("empty input must stay empty, got %v", got)
	}
	if got := dropAdjacentHit([]string{"a1"}, "", "forward"); len(got) != 1 {
		t.Errorf("an empty hit line must drop nothing, got %v", got)
	}
}

// TestApplyContextsToMessageOrder is the regression guard for a swap: the stack
// and the log context each leave the same anonymous "<no value>" token behind on
// the namespaced path, so filling them one at a time put whichever ran first
// into the other's slot. The assignment must follow the TEMPLATE's order.
func TestApplyContextsToMessageOrder(t *testing.T) {
	tests := []struct {
		name          string
		tmpl          string
		rendered      string
		wantLine1     string
		wantLine3     string
		wantAppended  []string
		wantNoAppends bool
	}{
		{
			name:          "logcontext first in template",
			tmpl:          "服务异常\n{{.logcontext}}\n---\n{{.stack}}",
			rendered:      "服务异常\n<no value>\n---\n<no value>",
			wantLine1:     "LOGCTX",
			wantLine3:     "STACK",
			wantNoAppends: true,
		},
		{
			name:          "stack first in template",
			tmpl:          "服务异常\n{{.stack}}\n---\n{{.logcontext}}",
			rendered:      "服务异常\n<no value>\n---\n<no value>",
			wantLine1:     "STACK",
			wantLine3:     "LOGCTX",
			wantNoAppends: true,
		},
		{
			name:         "only one referenced: the other is appended",
			tmpl:         "服务异常\n{{.stack}}",
			rendered:     "服务异常\n<no value>",
			wantLine1:    "STACK",
			wantAppended: []string{LogContextCaption, "LOGCTX"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyContextsToMessage(tt.rendered, tt.tmpl, "STACK", "LOGCTX")
			lines := strings.Split(got, "\n")
			if lines[1] != tt.wantLine1 {
				t.Errorf("line 1 = %q, want %q\nfull:\n%s", lines[1], tt.wantLine1, got)
			}
			if tt.wantLine3 != "" && lines[3] != tt.wantLine3 {
				t.Errorf("line 3 = %q, want %q\nfull:\n%s", lines[3], tt.wantLine3, got)
			}
			for _, want := range tt.wantAppended {
				if !strings.Contains(got, want) {
					t.Errorf("missing appended %q in:\n%s", want, got)
				}
			}
			if tt.wantNoAppends && (strings.Contains(got, StackCaption) || strings.Contains(got, LogContextCaption)) {
				t.Errorf("both were placed by the template; nothing may be appended:\n%s", got)
			}
		})
	}
}

// TestApplyContextsToMessageContentCannotSwallowNextSlot: a log line that quotes
// the placeholder token must not eat the following variable's slot.
func TestApplyContextsToMessageContentCannotSwallowNextSlot(t *testing.T) {
	tmpl := "{{.logcontext}}\n{{.stack}}"
	rendered := "<no value>\n<no value>"
	logctx := "line quoting <no value> verbatim"

	got := ApplyContextsToMessage(rendered, tmpl, "STACK", logctx)
	lines := strings.Split(got, "\n")
	if lines[0] != logctx {
		t.Errorf("line 0 = %q, want the log context verbatim", lines[0])
	}
	if lines[1] != "STACK" {
		t.Errorf("line 1 = %q, want STACK — the quoted token must not have been used as its slot", lines[1])
	}
}

// TestApplyContextsToMessageLeavesForeignPlaceholders: a "<no value>" from some
// other missing variable is the operator's own template bug, and must survive.
func TestApplyContextsToMessageLeavesForeignPlaceholders(t *testing.T) {
	got := ApplyContextsToMessage("a\n<no value>", "a\n{{.typo}}", "", "LOGCTX")
	if !strings.Contains(got, "<no value>") {
		t.Errorf("a foreign placeholder must not be filled:\n%s", got)
	}
	if !strings.Contains(got, LogContextCaption) {
		t.Errorf("the unreferenced log context should still be appended:\n%s", got)
	}
}

// TestRenderMultiLineHit: a collector that merges a stack into one record makes
// the matched line itself multi-line, and every one of its lines must stay
// marked — otherwise the tail of the stack sits at column zero looking like
// nothing, while the context lines around it are indented.
func TestRenderMultiLineHit(t *testing.T) {
	b := ContextBlock{
		Before:     []string{"before"},
		Hit:        "ERROR boom\n\tat Foo.bar(Foo.java:1)\nCaused by: NPE",
		After:      []string{"after"},
		WantBefore: 1, WantAfter: 1,
	}
	out := b.Render(20)
	for _, want := range []string{"ERROR boom", "at Foo.bar(Foo.java:1)", "Caused by: NPE"} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, want) {
				found = true
				if !strings.HasPrefix(line, logCtxHitPrefix) {
					t.Errorf("hit line %q is not marked: %q", want, line)
				}
			}
		}
		if !found {
			t.Errorf("hit line %q missing from render:\n%s", want, out)
		}
	}
}

// TestRenderMultiLineHitChargedToBudget: the matched line's real height counts
// against the display limit. Charging it as one line let a merged fifty-line
// stack render far past whatever the operator configured.
func TestRenderMultiLineHitChargedToBudget(t *testing.T) {
	b := ContextBlock{
		Before:     []string{"b1", "b2", "b3", "b4", "b5"},
		Hit:        "l1\nl2\nl3\nl4",
		After:      []string{"a1", "a2", "a3", "a4", "a5"},
		WantBefore: 5, WantAfter: 5,
	}
	out := b.Render(8)

	var shown int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, logCtxLinePrefix) || strings.HasPrefix(line, logCtxHitPrefix) {
			shown++
		}
	}
	if shown > 8 {
		t.Errorf("rendered %d lines against a display limit of 8:\n%s", shown, out)
	}
	// The hit is the subject and is shown whole even when it eats the budget.
	for _, l := range []string{"l1", "l2", "l3", "l4"} {
		if !strings.Contains(out, logCtxHitPrefix+l) {
			t.Errorf("hit line %q missing:\n%s", l, out)
		}
	}
}

// TestRenderPutsNoticesFirst: Telegram truncates from the END, so the caveats
// and the query to re-run must lead the block — an alert that had to omit lines
// is exactly the one long enough to lose a trailing notice.
func TestRenderPutsNoticesFirst(t *testing.T) {
	b := ContextBlock{
		Before: make([]string, 25), Hit: "hit", After: make([]string, 50),
		WantBefore: 25, WantAfter: 50,
		HitTime:      time.Date(2026, 9, 9, 14, 4, 54, 0, time.UTC),
		WindowBefore: 30 * time.Second, WindowAfter: 30 * time.Second,
		Selector: `{app="api"}`,
	}
	out := b.Render(12)

	idxNotice := strings.Index(out, "⚠️")
	idxFooter := strings.Index(out, "📖")
	idxBand := strings.Index(out, logCtxBandTop)
	if idxNotice < 0 || idxFooter < 0 || idxBand < 0 {
		t.Fatalf("missing a section:\n%s", out)
	}
	if idxNotice > idxBand || idxFooter > idxBand {
		t.Errorf("notice(%d) and footer(%d) must precede the log lines(%d):\n%s",
			idxNotice, idxFooter, idxBand, out)
	}
}

// TestRenderContextSkipped: a line that lost its context to the per-run cap must
// still be followable — the note carries the selector and instant to look at,
// built from the hit already in memory so it costs no query.
func TestRenderContextSkipped(t *testing.T) {
	hit := map[string]interface{}{
		"__stream_labels": map[string]string{"namespace": "g66-openapi", "pod": "api-1"},
		"__ts_nano":       time.Date(2026, 9, 9, 14, 4, 54, 0, time.UTC).UnixNano(),
	}
	got := RenderContextSkipped(hit, 10)
	for _, want := range []string{"上限 10 条", `{namespace="g66-openapi", pod="api-1"}`, "自行查看"} {
		if !strings.Contains(got, want) {
			t.Errorf("note missing %q:\n%s", want, got)
		}
	}

	// An Elasticsearch hit has neither key; the note must degrade to the plain
	// sentence rather than print an empty selector the reader cannot use.
	bare := RenderContextSkipped(map[string]interface{}{}, 10)
	if strings.Contains(bare, "查询语句") {
		t.Errorf("a hit with no stream labels must not advertise a selector:\n%s", bare)
	}
	if !strings.Contains(bare, "上限 10 条") {
		t.Errorf("the cap itself must still be stated:\n%s", bare)
	}
}

// TestBuildStreamSelectorDropsNonSelectableLabels is the regression guard for a
// silent production failure: a context query is built by feeding a query
// RESULT's labels back into a selector, and Loki returns labels it will not
// match on. Verified against production Loki — the identical selector returned
// 500 lines without detected_level and zero with it.
func TestBuildStreamSelectorDropsNonSelectableLabels(t *testing.T) {
	// The exact label set production Loki returns for one stream.
	prod := map[string]string{
		"app":            "ticketdesk-frontend",
		"component":      "frontend",
		"container":      "frontend",
		"detected_level": "unknown",
		"filename":       "/var/log/pods/devops_ticketdesk-frontend-59f554498d-rgqfr_x/frontend/2.log",
		"instance":       "ticketdesk",
		"job":            "devops/ticketdesk-frontend",
		"namespace":      "devops",
		"node_name":      "gke-infra-k8s-cluste-f32f9f71-3bq6",
		"pod":            "ticketdesk-frontend-59f554498d-rgqfr",
		"service_name":   "ticketdesk-frontend",
		"stream":         "stdout",
		"__error__":      "LogfmtParserErr",
	}
	got := buildStreamSelector(prod)

	if strings.Contains(got, "detected_level") {
		t.Errorf("detected_level must not reach the selector — it matches nothing:\n%s", got)
	}
	if strings.Contains(got, "__error__") {
		t.Errorf("Loki internals must not reach the selector:\n%s", got)
	}
	// Everything that IS selectable must survive: dropping a real label would
	// widen the query to other pods and pull in a neighbour's logs as context.
	for _, want := range []string{`app="ticketdesk-frontend"`, `pod="ticketdesk-frontend-59f554498d-rgqfr"`,
		`namespace="devops"`, `container="frontend"`, `stream="stdout"`, `filename="`} {
		if !strings.Contains(got, want) {
			t.Errorf("selector lost %s:\n%s", want, got)
		}
	}
	// A hit carrying only non-selectable labels yields no selector at all,
	// rather than "{}" — which would match every stream in the cluster.
	if s := buildStreamSelector(map[string]string{"detected_level": "error"}); s != "" {
		t.Errorf("labels that are all non-selectable must yield no selector, got %q", s)
	}
}

// TestQueryWithSlotReportsDeadline: acquireGlobalCtx blocks on a saturated
// semaphore and gives up only when ctx is done, so a failed acquisition always
// means the rule's run budget expired. It must surface as a real error — the
// caller marks the direction failed, and the alert says the context could not
// be read rather than claiming the stream had no more logs.
func TestQueryWithSlotReportsDeadline(t *testing.T) {
	saved := GlobalQuerySemaphore
	GlobalQuerySemaphore = make(chan struct{}, 1)
	GlobalQuerySemaphore <- struct{}{} // saturate
	t.Cleanup(func() { GlobalQuerySemaphore = saved })

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := queryWithSlot(expired, nil, `{a="b"}`, time.Now(), time.Now(), 10, "forward")
	if err == nil {
		t.Fatal("an expired run budget must produce an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the cause must be preserved for the log line, got %v", err)
	}

	// A live ctx that expires while waiting behaves the same way: still a
	// deadline, never silently treated as a complete context.
	live, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()
	if _, err = queryWithSlot(live, nil, `{a="b"}`, time.Now(), time.Now(), 10, "forward"); err == nil {
		t.Error("waiting past the deadline must produce an error")
	}
}

// TestBuildNamespacedAlertMessagePerHitContext is the regression guard for an
// aggregated alert that carried ONE context block for three matched lines: it
// sat at the end, belonged to none of them, and the reader could not tell which
// line it explained.
func TestBuildNamespacedAlertMessagePerHitContext(t *testing.T) {
	hits := []map[string]interface{}{
		{"message": "ERROR one", "tid": "t1"},
		{"message": "ERROR two", "tid": "t2"},
		{"message": "ERROR three", "tid": "t3"},
	}
	// Each hit gets a context naming itself, so misattribution is visible.
	provider := func(hit map[string]interface{}) (string, string) {
		tid, _ := hit["tid"].(string)
		return "", "CTX-FOR-" + tid
	}

	got := BuildNamespacedAlertMessage("ns", "c", "S1", "", "", hits, provider)

	for _, tid := range []string{"t1", "t2", "t3"} {
		if !strings.Contains(got, "CTX-FOR-"+tid) {
			t.Errorf("hit %s has no context of its own:\n%s", tid, got)
		}
	}
	// Order is what proves attribution: each context must follow ITS hit and
	// precede the next one.
	iOne, iCtx1 := strings.Index(got, "ERROR one"), strings.Index(got, "CTX-FOR-t1")
	iTwo, iCtx2 := strings.Index(got, "ERROR two"), strings.Index(got, "CTX-FOR-t2")
	if !(iOne < iCtx1 && iCtx1 < iTwo && iTwo < iCtx2) {
		t.Errorf("contexts are not interleaved with their hits (%d/%d/%d/%d):\n%s",
			iOne, iCtx1, iTwo, iCtx2, got)
	}
}

// TestBuildNamespacedAlertMessageNilProvider: the shared query path renders
// before dedup/mute decide anything and must stay free of context work.
func TestBuildNamespacedAlertMessageNilProvider(t *testing.T) {
	hits := []map[string]interface{}{{"message": "ERROR one"}}
	got := BuildNamespacedAlertMessage("ns", "c", "S1", "", "", hits, nil)
	if strings.Contains(got, LogContextCaption) || strings.Contains(got, StackCaption) {
		t.Errorf("a nil provider must add no context section:\n%s", got)
	}
}

// TestBuildNamespacedAlertMessageTemplatePlacement: a template that names the
// variable places it itself; only what it left out is appended.
func TestBuildNamespacedAlertMessageTemplatePlacement(t *testing.T) {
	hits := []map[string]interface{}{{"message": "boom", "tid": "t1"}}
	provider := func(map[string]interface{}) (string, string) { return "STACKDATA", "CTXDATA" }

	placed := BuildNamespacedAlertMessage("ns", "c", "S1", "", "上下文：{{.logcontext}}", hits, provider)
	if !strings.Contains(placed, "上下文：CTXDATA") {
		t.Errorf("template-placed context was not substituted:\n%s", placed)
	}
	if strings.Contains(placed, LogContextCaption) {
		t.Errorf("a context the template placed must not also be appended:\n%s", placed)
	}
	// The stack was never referenced, so it still has to show up.
	if !strings.Contains(placed, StackCaption) || !strings.Contains(placed, "STACKDATA") {
		t.Errorf("the unreferenced stack must be appended:\n%s", placed)
	}
}

// TestRenderConfigLine: the block must state the counts the rule asked for, and
// flag a mismatch when the display budget cut them back. Without this the
// operator has no way to tell whether the feature honoured the form.
func TestRenderConfigLine(t *testing.T) {
	// Everything fits: state the configured figures, stay quiet about the rest.
	full := ContextBlock{
		Before: make([]string, 25), Hit: "hit", After: make([]string, 50),
		WantBefore: 25, WantAfter: 50,
	}
	out := full.Render(200)
	if !strings.Contains(out, "📐 向前 25 行 / 向后 50 行") {
		t.Errorf("configured counts missing:\n%s", out)
	}
	if strings.Contains(out, "本条实际展示") {
		t.Errorf("nothing was cut, so no mismatch note belongs here:\n%s", out)
	}

	// Budget cuts it back: both figures must appear, or the reader concludes
	// the configuration was ignored.
	cut := ContextBlock{
		Before: make([]string, 25), Hit: "hit", After: make([]string, 50),
		WantBefore: 25, WantAfter: 50,
	}
	out = cut.Render(20)
	if !strings.Contains(out, "📐 向前 25 行 / 向后 50 行（本条实际展示 6 / 13）") {
		t.Errorf("mismatch line wrong or missing:\n%s", out)
	}

	// The line leads the block — it frames everything below it.
	if i, j := strings.Index(out, "📐"), strings.Index(out, "⚠️"); i < 0 || j < 0 || i > j {
		t.Errorf("config line must precede the truncation notice (%d vs %d):\n%s", i, j, out)
	}
}

// TestFetchNotFoundContextsDeclinesWithoutHit: a container with no history at
// all has nothing to look around, and querying for it would spend Loki calls
// proving a negative that is already known.
func TestFetchNotFoundContextsDeclinesWithoutHit(t *testing.T) {
	rule := &models.AlertRule{LogContextEnabled: 1, StackContextEnabled: 1}
	called := false
	getClient := func(int) (*lokiclient.Client, error) {
		called = true
		return nil, errors.New("must not be reached")
	}
	stack, logctx := FetchNotFoundContexts(context.Background(), rule, nil, getClient)
	if stack != "" || logctx != "" {
		t.Errorf("no last hit must yield no context, got %q / %q", stack, logctx)
	}
	if called {
		t.Error("no last hit must not reach Loki at all")
	}
}

// TestNotFoundRuleClampsForward: the not_found path alerts BECAUSE the source
// went quiet, so a configured 向后 50 would make the ladder climb every rung to
// its ceiling hunting for lines that cannot exist.
func TestNotFoundRuleClampsForward(t *testing.T) {
	rule := &models.AlertRule{LogContextBefore: 25, LogContextAfter: 50}
	got := notFoundRule(rule)

	if got.LogContextAfter != NotFoundContextAfter {
		t.Errorf("forward = %d, want it clamped to %d", got.LogContextAfter, NotFoundContextAfter)
	}
	// Backward is the direction that matters here and must survive untouched.
	if got.LogContextBefore != 25 {
		t.Errorf("backward = %d, want 25 — that is the useful direction here", got.LogContextBefore)
	}
	// A rule already asking for less than the cap keeps its own figure.
	small := notFoundRule(&models.AlertRule{LogContextAfter: 2})
	if small.LogContextAfter != 2 {
		t.Errorf("a smaller configured value must be kept, got %d", small.LogContextAfter)
	}
	// The caller's rule is shared live config and must not be mutated.
	if rule.LogContextAfter != 50 {
		t.Errorf("the caller's rule was mutated: LogContextAfter = %d", rule.LogContextAfter)
	}
}
