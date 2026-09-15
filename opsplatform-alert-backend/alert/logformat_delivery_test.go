package alert

import (
	"strings"
	"testing"
	"time"

	"opsplatform-alert-backend/notify"
	"opsplatform-alert-backend/telegram"
)

// The fix is only worth anything if the fence the engine writes survives all
// the way to what each platform actually receives. This walks the real path a
// namespaced alert takes: build the body, pass it through the fence
// normalization every notifier applies, then render it for Telegram.
func TestEngineFencedLogReachesTelegramAsAPreBlock(t *testing.T) {
	hits := []map[string]interface{}{
		{"message": "2026-09-09 06:37:36.638 ERROR UserTransferEventListener - {\"code\":\"1351\"}"},
	}
	body := notify.NormalizeFencedBlocks(
		BuildNamespacedAlertMessage("g32-openapi", "atmosphere-client-backend", "S1", "", "", hits, nil),
	)

	got := telegram.BuildMessage("G32 UAT 错误码告警", body, "S1", nil, false, time.Now())

	if !strings.Contains(got, "<pre>") {
		t.Errorf("telegram.BuildMessage() should render the engine's fence as <pre>, got %q", got)
	}
	// A <pre> and a blockquote must not both wrap the body: Telegram does not
	// nest them reliably, and the blockquote is what used to collapse.
	if strings.Contains(got, "<blockquote") {
		t.Errorf("telegram.BuildMessage() should not quote a body that has a code block, got %q", got)
	}
	if strings.Contains(got, "expandable") {
		t.Errorf("telegram.BuildMessage() must not collapse an alert body, got %q", got)
	}
	// The log's own JSON braces and quotes must arrive intact, not markdown-converted.
	if !strings.Contains(got, `{&quot;code&quot;:&quot;1351&quot;}`) {
		t.Errorf("telegram.BuildMessage() mangled the log payload, got %q", got)
	}
}

// Lark's renderer only accepts a fence whose delimiters each sit on their own
// line. The engine writes that shape directly, so normalization — which every
// notifier applies — must leave it untouched rather than rewriting it.
func TestEngineFencedLogIsAlreadyCanonicalForLark(t *testing.T) {
	hits := []map[string]interface{}{{"message": "ERROR boom"}}
	body := BuildNamespacedAlertMessage("ns", "c", "S1", "", "", hits, nil)

	if got := notify.NormalizeFencedBlocks(body); got != body {
		t.Errorf("NormalizeFencedBlocks rewrote the engine's own fence:\n%q\nto\n%q", body, got)
	}
	if !strings.Contains(body, "\n```\nERROR boom\n```") {
		t.Errorf("BuildNamespacedAlertMessage() fence is not on its own lines: %q", body)
	}
}
