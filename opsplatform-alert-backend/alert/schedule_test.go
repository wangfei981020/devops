package alert

import (
	"testing"
	"time"
)

func TestNormalizeSchedulePrependsSecondsOnlyForFiveFields(t *testing.T) {
	cases := map[string]string{
		"*/5 * * * *":   "0 */5 * * * *", // what users type, and what the DB holds
		"0 */5 * * * *": "0 */5 * * * *", // already six fields
		"@hourly":       "@hourly",       // descriptor, not a field list
	}
	for in, want := range cases {
		if got := NormalizeSchedule(in); got != want {
			t.Errorf("NormalizeSchedule(%q) = %q, want %q", in, got, want)
		}
	}
}

// The dashboard calls a rule overdue by comparing its last run against the next
// fire its own expression implies, so that计算 has to agree with the scheduler
// for both the five- and six-field forms.
func TestParseScheduleAgreesAcrossFieldCounts(t *testing.T) {
	last := time.Date(2026, 9, 8, 4, 0, 0, 0, time.Local)

	for _, expr := range []string{"*/5 * * * *", "0 */5 * * * *"} {
		sched, err := ParseSchedule(expr)
		if err != nil {
			t.Fatalf("ParseSchedule(%q) failed: %v", expr, err)
		}
		next := sched.Next(last)
		want := time.Date(2026, 9, 8, 4, 5, 0, 0, time.Local)
		if !next.Equal(want) {
			t.Errorf("ParseSchedule(%q).Next(%v) = %v, want %v", expr, last, next, want)
		}
	}
}

func TestParseScheduleRejectsGarbage(t *testing.T) {
	if _, err := ParseSchedule("not a cron"); err == nil {
		t.Error("expected an error for an unparseable expression, got nil")
	}
}

// The dashboard calls a rule overdue only once it has missed a whole cycle plus
// a grace period. Measuring against the next due time alone would flag a
// five-minute rule barely two minutes after its slot — which is what a restart
// looks like — so the two cases below have to come out differently.
func TestOverdueNeedsAWholeMissedCycle(t *testing.T) {
	sched, err := ParseSchedule("*/5 * * * *")
	if err != nil {
		t.Fatalf("ParseSchedule failed: %v", err)
	}
	const grace = 2 * time.Minute

	// overdue mirrors the dashboard's test: past the run after the next one.
	overdue := func(now, lastRun time.Time) bool {
		due := sched.Next(lastRun)
		return now.After(sched.Next(due).Add(grace))
	}

	cases := []struct {
		name         string
		lastRun, now time.Time
		want         bool
	}{
		{
			// Ran at 04:04, checked at 04:07: 04:05 was due and may be moments
			// away from being recorded. Not overdue.
			name:    "just past its slot",
			lastRun: time.Date(2026, 9, 8, 4, 4, 0, 0, time.UTC),
			now:     time.Date(2026, 9, 8, 4, 7, 0, 0, time.UTC),
			want:    false,
		},
		{
			// Ran at 04:04; 04:05 and 04:10 have both gone by, plus grace.
			name:    "missed a full cycle",
			lastRun: time.Date(2026, 9, 8, 4, 4, 0, 0, time.UTC),
			now:     time.Date(2026, 9, 8, 4, 13, 0, 0, time.UTC),
			want:    true,
		},
		{
			name:    "silent for an hour",
			lastRun: time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC),
			now:     time.Date(2026, 9, 8, 4, 7, 0, 0, time.UTC),
			want:    true,
		},
	}
	for _, c := range cases {
		if got := overdue(c.now, c.lastRun); got != c.want {
			t.Errorf("%s: overdue = %v, want %v", c.name, got, c.want)
		}
	}
}

// A daily rule gets a whole extra day of slack, which is the same rule applied
// to a much longer interval rather than a fixed number of minutes.
func TestOverdueScalesWithTheInterval(t *testing.T) {
	sched, err := ParseSchedule("0 3 * * *")
	if err != nil {
		t.Fatalf("ParseSchedule failed: %v", err)
	}
	lastRun := time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC)
	due := sched.Next(lastRun)
	afterThat := sched.Next(due)

	sameDayEvening := time.Date(2026, 9, 8, 20, 0, 0, 0, time.UTC)
	if sameDayEvening.After(afterThat.Add(2 * time.Minute)) {
		t.Error("a daily rule that ran this morning is not overdue the same evening")
	}
	threeDaysLater := time.Date(2026, 9, 11, 4, 0, 0, 0, time.UTC)
	if !threeDaysLater.After(afterThat.Add(2 * time.Minute)) {
		t.Error("a daily rule silent for three days is overdue")
	}
}
