package timezone

import (
	"strings"
	"testing"
	"time"
)

// The instant is the same; only its rendering moves. Changing the setting must
// therefore re-render history, never rewrite it.
func TestSameInstantRendersPerZone(t *testing.T) {
	instant := time.Date(2026, 9, 7, 20, 31, 42, 0, time.UTC)

	cases := map[string]string{
		"UTC":              "2026-09-07 20:31:42 (+00:00 UTC)",
		"Asia/Shanghai":    "2026-09-08 04:31:42 (+08:00 Asia/Shanghai)",
		"America/New_York": "2026-09-07 16:31:42 (-04:00 America/New_York)",
	}

	t.Cleanup(func() { Set("UTC") })
	for zone, want := range cases {
		if err := Set(zone); err != nil {
			t.Fatalf("Set(%q) failed: %v", zone, err)
		}
		if got := FormatWithZone(instant); got != want {
			t.Errorf("in %s: got %q, want %q", zone, got, want)
		}
	}
}

// A zone's offset changes across the year. Rendering has to come from the
// instant, not from a stored offset.
func TestOffsetFollowsDaylightSaving(t *testing.T) {
	t.Cleanup(func() { Set("UTC") })
	if err := Set("Europe/Berlin"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	summer := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	winter := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	if got, want := FormatWithZone(summer), "2026-07-01 14:00:00 (+02:00 Europe/Berlin)"; got != want {
		t.Errorf("summer: got %q, want %q", got, want)
	}
	if got, want := FormatWithZone(winter), "2026-01-01 13:00:00 (+01:00 Europe/Berlin)"; got != want {
		t.Errorf("winter: got %q, want %q", got, want)
	}
}

func TestSetRejectsUnknownZoneAndKeepsPrevious(t *testing.T) {
	t.Cleanup(func() { Set("UTC") })
	if err := Set("Asia/Shanghai"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := Set("Mars/Olympus_Mons"); err == nil {
		t.Fatal("expected an error for an unknown zone, got nil")
	}
	if Name() != "Asia/Shanghai" {
		t.Errorf("a rejected change must leave the previous zone in place, got %q", Name())
	}
}

// Unset must mean "the process's own zone", not UTC. An installation upgrading
// from a build without this setting has nothing stored, and defaulting to UTC
// would move every displayed timestamp and shift when scheduled rules fire on a
// system nobody asked to change.
func TestUnsetFollowsTheProcessZoneRatherThanUTC(t *testing.T) {
	t.Cleanup(func() { Set("") })
	if err := Set("Asia/Shanghai"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := Set(""); err != nil {
		t.Fatalf("Set(\"\") failed: %v", err)
	}
	if Name() != "" {
		t.Errorf("unset should report an empty name, got %q", Name())
	}
	if Location() != time.Local {
		t.Errorf("unset should resolve to time.Local, got %v", Location())
	}
	// The label carries the offset alone; "Local" would tell a reader nothing.
	got := FormatWithZone(time.Date(2026, 9, 7, 20, 31, 42, 0, time.UTC))
	if strings.Contains(got, "Local") || !strings.Contains(got, "(") {
		t.Errorf("unset label should be offset-only, got %q", got)
	}
}

func TestZeroTimeRendersEmpty(t *testing.T) {
	if got := Format(time.Time{}); got != "" {
		t.Errorf("Format(zero) = %q, want empty", got)
	}
	if got := FormatWithZone(time.Time{}); got != "" {
		t.Errorf("FormatWithZone(zero) = %q, want empty", got)
	}
}
