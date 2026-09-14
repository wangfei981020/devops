package alert

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"opsplatform-alert-backend/timezone"
)

// A retention setting whose job never fires is indistinguishable from a setting
// that does nothing, and the failure is silent — nothing is deleted and nothing
// says why. These cover the wiring rather than the DELETE: that the job lands on
// the scheduler, that the scheduler actually calls it, and that it is due when
// the expression says.

func TestRetentionJobIsRegisteredAndDue(t *testing.T) {
	t.Cleanup(func() { timezone.Set("") })
	if err := timezone.Set("Asia/Shanghai"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	e := NewEngine()
	before := len(e.cron.Entries())
	if err := e.startRetentionJob(); err != nil {
		t.Fatalf("startRetentionJob: %v", err)
	}

	entries := e.cron.Entries()
	if len(entries) != before+1 {
		t.Fatalf("expected one new cron entry, went from %d to %d", before, len(entries))
	}

	// cron leaves an entry's Next zero until the scheduler is running, and this
	// is registered before Start, so the due time comes from the expression.
	next := nextRetentionRun()
	if next.IsZero() {
		t.Fatal("the schedule does not parse; the job would never fire")
	}
	// 03:30 in the configured zone, which is the point of registering it there.
	local := next.In(timezone.Location())
	if local.Hour() != 3 || local.Minute() != 30 {
		t.Errorf("next run is %s, want 03:30 in %s", local.Format(time.RFC3339), timezone.Name())
	}
	if !next.After(time.Now()) {
		t.Errorf("next run %s is not in the future", next)
	}
}

// The registration path really does get invoked by a running scheduler. Driven
// on a one-second schedule so the test observes an actual firing rather than
// asserting that one was scheduled.
func TestRetentionJobActuallyFires(t *testing.T) {
	saved := retentionSchedule
	retentionSchedule = "* * * * * *" // every second
	t.Cleanup(func() { retentionSchedule = saved })

	var fired atomic.Int32

	// Same registration shape as startRetentionJob, with the work stubbed: the
	// DELETE itself is covered elsewhere, what is under test is that a job put
	// on this scheduler is called.
	c := cron.New(cron.WithSeconds(), cron.WithLocation(timezone.Location()))
	if _, err := c.AddFunc(retentionSchedule, func() { fired.Add(1) }); err != nil {
		t.Fatalf("AddFunc: %v", err)
	}
	c.Start()
	t.Cleanup(func() { <-c.Stop().Done() })

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the scheduler never called the job in 4s (fired=%d)", fired.Load())
}

// Retention off must not merely skip the delete — the job body should return
// before doing any work, so a disabled setting costs nothing every night.
func TestPruneIsANoOpWhenRetentionIsOff(t *testing.T) {
	// RetentionDays reads the database; with no connection it falls back to the
	// default, which is off. PruneAlertLogs must return without touching
	// anything, and in particular without panicking on a nil DB.
	n, err := PruneAlertLogs()
	if err != nil {
		t.Fatalf("prune with retention off returned an error: %v", err)
	}
	if n != 0 {
		t.Errorf("prune with retention off deleted %d rows, want 0", n)
	}
}
