package alert

import (
	"strings"
	"testing"
)

func TestRenderVarsDefaultFencesTheLogText(t *testing.T) {
	got := RenderVarsDefault(map[string]interface{}{
		"code":    "1351",
		"message": "2026-09-09 06:37:36.638 ERROR UserTransferEventListener - boom",
	})

	if !strings.Contains(got, "**code:** 1351") {
		t.Errorf("RenderVarsDefault() should print a short field inline, got %q", got)
	}
	if !strings.Contains(got, "```\n2026-09-09 06:37:36.638 ERROR UserTransferEventListener - boom\n```") {
		t.Errorf("RenderVarsDefault() should fence the log text, got %q", got)
	}
	// The log must not also be printed inline after a bold label.
	if strings.Contains(got, "**message:** 2026") {
		t.Errorf("RenderVarsDefault() printed the log inline as well as fenced, got %q", got)
	}
}

// A map's iteration order is randomised per call, so without sorting the same
// rule renders its fields in a different order on every alert.
func TestRenderVarsDefaultOrderIsStable(t *testing.T) {
	vars := map[string]interface{}{"z": 1, "a": 2, "m": 3, "code": 4}

	first := RenderVarsDefault(vars)
	for i := 0; i < 50; i++ {
		if got := RenderVarsDefault(vars); got != first {
			t.Fatalf("RenderVarsDefault() is order-unstable:\n%q\nvs\n%q", first, got)
		}
	}
	if !strings.HasPrefix(first, "**a:** 2\n**code:** 4\n**m:** 3\n**z:** 1\n") {
		t.Errorf("RenderVarsDefault() should sort its fields, got %q", first)
	}
}

func TestRenderVarsDefaultPutsLogTextLast(t *testing.T) {
	got := RenderVarsDefault(map[string]interface{}{
		"message": "the log line",
		"zzz":     "a short field sorting after 'message'",
	})

	if strings.Index(got, "**zzz:**") > strings.Index(got, "```") {
		t.Errorf("RenderVarsDefault() should emit short fields before the fenced log, got %q", got)
	}
}

func TestRenderVarsDefaultSkipsInternalAndEmptyKeys(t *testing.T) {
	got := RenderVarsDefault(map[string]interface{}{
		"_id":     "abc",
		"_index":  "logs-1",
		"message": "   ",
		"code":    "1351",
	})

	if strings.Contains(got, "_id") || strings.Contains(got, "_index") {
		t.Errorf("RenderVarsDefault() should skip internal keys, got %q", got)
	}
	if strings.Contains(got, "```") {
		t.Errorf("RenderVarsDefault() should not emit an empty fence, got %q", got)
	}
}

func TestTruncateLogRunesCutsOnRuneBoundaries(t *testing.T) {
	got := truncateLogRunes(strings.Repeat("日", 10), 4)

	if got != "日日日日..." {
		t.Errorf("truncateLogRunes() = %q, want %q", got, "日日日日...")
	}
	if !isValidUTF8(got) {
		t.Errorf("truncateLogRunes() produced invalid UTF-8: %q", got)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestBuildNamespacedAlertMessageFencesTheLogWithoutATemplate(t *testing.T) {
	hits := []map[string]interface{}{
		{"message": "2026-09-09 06:37:36.638 ERROR **not bold** boom", "pod": "p-1"},
	}
	got := BuildNamespacedAlertMessage("g32-openapi", "atmosphere-client-backend", "S1", "", "", hits)

	if !strings.Contains(got, "```\n2026-09-09 06:37:36.638 ERROR **not bold** boom\n```") {
		t.Errorf("BuildNamespacedAlertMessage() should fence the log line, got %q", got)
	}
	if strings.Contains(got, "**日志:** 2026") {
		t.Errorf("BuildNamespacedAlertMessage() still prints the log inline, got %q", got)
	}
}

func TestBuildNamespacedAlertMessageTruncatesLongLogsOnRuneBoundaries(t *testing.T) {
	hits := []map[string]interface{}{{"message": strings.Repeat("日", maxInlineLogRunes+50)}}
	got := BuildNamespacedAlertMessage("ns", "c", "S1", "", "", hits)

	if !strings.Contains(got, strings.Repeat("日", maxInlineLogRunes)+"...") {
		t.Errorf("BuildNamespacedAlertMessage() did not truncate at %d runes, got %q", maxInlineLogRunes, got)
	}
	if !isValidUTF8(got) {
		t.Errorf("BuildNamespacedAlertMessage() produced invalid UTF-8")
	}
}
