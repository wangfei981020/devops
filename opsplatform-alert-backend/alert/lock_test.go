package alert

import (
	"testing"
	"time"

	"opsplatform-alert-backend/database"
)

// The lease has to outlast an ordinary slow run of the rule it guards, without
// leaving a rule blocked for long after a process dies mid-run.
func TestLockTTLTracksTheRulesOwnInterval(t *testing.T) {
	cases := []struct {
		schedule string
		want     time.Duration
		why      string
	}{
		{"*/5 * * * *", 10 * time.Minute, "two intervals of a five-minute rule"},
		{"* * * * *", 2 * time.Minute, "the fastest schedule still clears the floor"},
		{"0 * * * *", maxLockTTL, "an hourly rule doubled exceeds the cap and is clamped"},
		{"0 3 * * *", maxLockTTL, "a daily rule is capped, not leased for two days"},
		{"not a cron", 5 * time.Minute, "an unparseable schedule falls back rather than panicking"},
	}
	for _, c := range cases {
		if got := lockTTL(c.schedule); got != c.want {
			t.Errorf("lockTTL(%q) = %v, want %v (%s)", c.schedule, got, c.want, c.why)
		}
	}
}

// Deduplication is a convenience; alerting is the product. With no Redis the
// run must proceed, because a duplicate alert is an annoyance and a silenced
// one is the failure this platform exists to prevent.
func TestLockFailsOpenWithoutRedis(t *testing.T) {
	saved := database.RDB
	database.RDB = nil
	t.Cleanup(func() { database.RDB = saved })

	release, ok := tryLock("alert:lock:rule:test", time.Minute)
	if !ok {
		t.Fatal("tryLock must succeed when Redis is absent, got refused")
	}
	if release == nil {
		t.Fatal("release must be callable even when nothing was locked")
	}
	release() // must not panic
}
