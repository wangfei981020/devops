package alert

import (
	"fmt"
	"strings"
	"testing"
)

// buildDeepJavaStack returns a realistic 51-line Java stack: the matched log
// line, an exception line, 45 framework frames, a "Caused by:" line carrying
// the actual root cause, and 3 tail lines (2 frames plus a "... N more"
// summary) — the same shape as the deep stack that exposed the bug where a
// tight collection cap discarded the bottom of the stack before elision ever
// ran.
func buildDeepJavaStack() []string {
	lines := []string{
		"2026-09-07 19:00:00 ERROR payment failed: downstream call timed out",
		"java.lang.RuntimeException: downstream call failed",
	}
	for i := 1; i <= 45; i++ {
		lines = append(lines, fmt.Sprintf("\tat com.acme.framework.Layer%d.invoke(Layer%d.java:%d)", i, i, i*10))
	}
	lines = append(lines,
		"Caused by: java.net.SocketTimeoutException: connect timed out",
		"\tat java.base/sun.nio.ch.NioSocketImpl.connect(NioSocketImpl.java:567)",
		"\tat java.base/java.net.Socket.connect(Socket.java:633)",
		"\t... 27 more",
	)
	return lines
}

func TestCollectStackStopsAtTheNextLogRecord(t *testing.T) {
	re, err := CompileBoundary("")
	if err != nil {
		t.Fatalf("the default pattern must compile: %v", err)
	}
	lines := []string{
		"2026-09-07 19:00:00 ERROR payment failed",
		"java.lang.RuntimeException: downstream timeout",
		"\tat com.acme.pay.Client.call(Client.java:42)",
		"\tat com.acme.pay.Service.pay(Service.java:17)",
		"2026-09-07 19:00:01 INFO request finished", // a new record — stop before this
		"\tat should.not.appear(Nope.java:1)",
	}

	got := CollectStack(lines, re, 30)

	if len(got) != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", len(got), strings.Join(got, "\n"))
	}
	if strings.Contains(strings.Join(got, "\n"), "should.not.appear") {
		t.Error("collected past the next log record")
	}
}

func TestCollectStackAlwaysKeepsTheMatchedLine(t *testing.T) {
	re, _ := CompileBoundary("")
	// The matched line itself looks like a new record. If it were tested against
	// the boundary the result would be empty every single time.
	lines := []string{
		"2026-09-07 19:00:00 ERROR payment failed",
		"\tat com.acme.Client.call(Client.java:42)",
	}
	got := CollectStack(lines, re, 30)
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(got), got)
	}
}

func TestCollectStackHonoursMaxLines(t *testing.T) {
	re, _ := CompileBoundary("")
	lines := []string{"2026-09-07 19:00:00 ERROR boom"}
	for i := 0; i < 100; i++ {
		lines = append(lines, "\tat frame")
	}
	if got := CollectStack(lines, re, 30); len(got) != 30 {
		t.Errorf("got %d lines, want the 30-line cap", len(got))
	}
}

func TestCollectStackWithABrokenBoundaryStillCapsOut(t *testing.T) {
	// A pattern that never matches must not drag the whole stream into an alert.
	re, err := CompileBoundary("^NEVER_MATCHES_ANYTHING$")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "some log line"
	}
	if got := CollectStack(lines, re, 30); len(got) != 30 {
		t.Errorf("got %d lines, want the 30-line cap", len(got))
	}
}

func TestCollectStackOnEmptyInput(t *testing.T) {
	re, _ := CompileBoundary("")
	if got := CollectStack(nil, re, 30); got != nil {
		t.Errorf("got %v, want nil for empty input", got)
	}
}

func TestCompileBoundaryRejectsABadPattern(t *testing.T) {
	if _, err := CompileBoundary("([unclosed"); err == nil {
		t.Fatal("expected an error for an invalid regex")
	}
}

func TestElideMiddleKeepsHeadAndTailAndSaysHowMuchWentMissing(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "line"
	}
	lines[0] = "FIRST"
	lines[49] = "LAST"

	got := ElideMiddle(lines, 12, 8)

	if len(got) != 21 { // 12 head + 1 marker + 8 tail
		t.Fatalf("got %d lines, want 21", len(got))
	}
	if got[0] != "FIRST" {
		t.Errorf("head lost: %q", got[0])
	}
	if got[len(got)-1] != "LAST" {
		t.Errorf("tail lost: %q", got[len(got)-1])
	}
	if !strings.Contains(got[12], "30") { // 50 - 12 - 8
		t.Errorf("marker %q should say 30 lines were elided", got[12])
	}
}

func TestElideMiddleLeavesShortStacksAlone(t *testing.T) {
	lines := []string{"a", "b", "c"}
	if got := ElideMiddle(lines, 12, 8); len(got) != 3 {
		t.Errorf("got %d lines, want the 3 untouched", len(got))
	}
}

func TestApplyStackToMessageReplacesNoValueWhenTemplateReferencesStack(t *testing.T) {
	tmpl := "alert fired\n```{{.stack}}```"
	message := "alert fired\n```<no value>```"
	stack := "java.lang.RuntimeException: downstream timeout"

	got := ApplyStackToMessage(message, tmpl, stack)

	if strings.Contains(got, "<no value>") {
		t.Errorf("no value token was not replaced: %q", got)
	}
	if n := strings.Count(got, stack); n != 1 {
		t.Errorf("stack should appear exactly once, appeared %d times: %q", n, got)
	}
	want := "alert fired\n```" + stack + "```"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyStackToMessageReplacesNoValueWithSpacedStackReference(t *testing.T) {
	tmpl := "alert fired\n```{{ .stack }}```"
	message := "alert fired\n```<no value>```"
	stack := "java.lang.RuntimeException: downstream timeout"

	got := ApplyStackToMessage(message, tmpl, stack)

	if strings.Contains(got, "<no value>") {
		t.Errorf("no value token was not replaced: %q", got)
	}
	if n := strings.Count(got, stack); n != 1 {
		t.Errorf("stack should appear exactly once, appeared %d times: %q", n, got)
	}
}

func TestApplyStackToMessageAppendsWhenTemplateDoesNotReferenceStack(t *testing.T) {
	tmpl := "alert fired: {{.message}}"
	message := "alert fired: payment timeout"
	stack := "java.lang.RuntimeException: downstream timeout"

	got := ApplyStackToMessage(message, tmpl, stack)

	// The caption is what tells a reader that the times inside the block belong
	// to the source system, not to the platform's display timezone.
	want := message + "\n" + StackCaption + "\n```\n" + stack + "\n```"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if n := strings.Count(got, stack); n != 1 {
		t.Errorf("stack should appear exactly once, appeared %d times: %q", n, got)
	}
}

// A template that places the stack itself owns the layout; captioning it would
// insert text the operator did not ask for, in a spot they did not choose.
func TestApplyStackToMessageDoesNotCaptionAnOperatorPlacedStack(t *testing.T) {
	tmpl := "alert fired\n```\n{{.stack}}\n```"
	message := "alert fired\n```\n<no value>\n```"
	stack := "java.lang.RuntimeException: downstream timeout"

	got := ApplyStackToMessage(message, tmpl, stack)

	if strings.Contains(got, StackCaption) {
		t.Errorf("operator-placed stack should not be captioned, got %q", got)
	}
}

func TestApplyStackToMessageLeavesMessageUnchangedWhenStackIsEmpty(t *testing.T) {
	message := "alert fired\n```<no value>```"

	if got := ApplyStackToMessage(message, "alert fired\n```{{.stack}}```", ""); got != message {
		t.Errorf("empty stack (referenced template) should leave message unchanged, got %q", got)
	}
	if got := ApplyStackToMessage(message, "alert fired: {{.message}}", ""); got != message {
		t.Errorf("empty stack (unreferenced template) should leave message unchanged, got %q", got)
	}
}

func TestApplyStackToMessageLeavesUnrelatedNoValueAlone(t *testing.T) {
	// "<no value>" here comes from some OTHER missing template variable, not
	// from {{.stack}}. Since the template doesn't reference the stack, the
	// token must not be touched, and the stack must still be appended.
	tmpl := "alert fired: {{.other}}"
	message := "alert fired: <no value>"
	stack := "java.lang.RuntimeException: downstream timeout"

	got := ApplyStackToMessage(message, tmpl, stack)

	if !strings.Contains(got, "alert fired: <no value>") {
		t.Errorf("unrelated <no value> token was modified: %q", got)
	}
	want := message + "\n" + StackCaption + "\n```\n" + stack + "\n```"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCollectThenElideKeepsCausedByOnARealisticDeepStack runs the real
// collect-then-elide pipeline (CollectStack followed by ElideMiddle) over a
// realistic 51-line Java stack with the new, generous maxLines=200 safety
// valve. The stack is collected whole, so elision's tail genuinely reaches
// the "Caused by:" line and the "... 27 more" summary at the bottom — which
// is the entire point of head/tail elision.
func TestCollectThenElideKeepsCausedByOnARealisticDeepStack(t *testing.T) {
	re, err := CompileBoundary("")
	if err != nil {
		t.Fatalf("the default pattern must compile: %v", err)
	}
	lines := buildDeepJavaStack()
	if len(lines) != 51 {
		t.Fatalf("test fixture should have 51 lines, has %d", len(lines))
	}

	collected := CollectStack(lines, re, 200) // the new, generous collection-time safety valve
	result := ElideMiddle(collected, 12, 8)

	if len(result) != 21 { // 12 head + 1 marker + 8 tail
		t.Fatalf("got %d lines, want 21:\n%s", len(result), strings.Join(result, "\n"))
	}

	joined := strings.Join(result, "\n")
	if !strings.Contains(joined, "… 省略") {
		t.Error("elision marker missing")
	}
	if !strings.Contains(joined, "Caused by:") {
		t.Errorf("Caused by: line lost — the root cause did not survive elision:\n%s", joined)
	}
	if !strings.Contains(joined, "... 27 more") {
		t.Errorf("tail frame-count line lost:\n%s", joined)
	}
}

// TestOldStackMaxLinesOf30LostTheRootCause documents the bug this change
// fixes and is a permanent regression test, not a temporary one to delete.
//
// With the old maxLines=30 default, CollectStack's own cap front-truncated a
// realistic 51-line Java stack down to its first 30 lines BEFORE ElideMiddle
// ever ran — discarding the "Caused by:" line and everything after it, which
// sits at the BOTTOM of the stack, exactly where a Java root cause lives.
// ElideMiddle then dutifully kept head+tail of whatever was left, but by then
// there was no root cause left to keep. That is precisely backwards: the
// whole reason head/tail elision exists is to preserve the root cause. This
// test intentionally re-runs the pipeline with the OLD, too-tight cap to
// prove the failure mode existed, so a regression that reintroduces a tight
// collection-time cap gets caught here.
func TestOldStackMaxLinesOf30LostTheRootCause(t *testing.T) {
	re, err := CompileBoundary("")
	if err != nil {
		t.Fatalf("the default pattern must compile: %v", err)
	}
	lines := buildDeepJavaStack()

	collected := CollectStack(lines, re, 30) // the OLD, too-tight default — reproduces the bug
	result := ElideMiddle(collected, 12, 8)

	joined := strings.Join(result, "\n")
	if strings.Contains(joined, "Caused by:") {
		t.Fatalf("expected the old maxLines=30 default to have LOST the Caused by: line (that was the bug being documented) but it is present:\n%s", joined)
	}
}

// TestCollectStackCapsRunawayInputAtTheConfiguredMaxLines confirms the safety
// valve still holds with the raised default: a boundary that never matches
// must not drag an unbounded log stream into a single alert.
func TestCollectStackCapsRunawayInputAtTheConfiguredMaxLines(t *testing.T) {
	re, err := CompileBoundary("^NEVER_MATCHES_ANYTHING$")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = "some log line that never looks like a new record"
	}
	if got := CollectStack(lines, re, 200); len(got) != 200 {
		t.Errorf("got %d lines, want the 200-line safety-valve cap", len(got))
	}
}
