package alert

import (
	"os"
	"testing"
	"time"

	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/timezone"
)

func TestMuteReminderJobIsRegistered(t *testing.T) {
	e := NewEngine()
	before := len(e.cron.Entries())
	if err := e.startMuteReminderJob(); err != nil {
		t.Fatalf("startMuteReminderJob: %v", err)
	}
	if got := len(e.cron.Entries()); got != before+1 {
		t.Fatalf("expected one new entry, went from %d to %d", before, got)
	}

	// The sweep has to run often enough that a notice lands close to its
	// intended lead rather than up to a poll period early.
	sched, err := ParseSchedule(muteReminderSchedule)
	if err != nil {
		t.Fatalf("schedule does not parse: %v", err)
	}
	now := time.Now()
	gap := sched.Next(sched.Next(now)).Sub(sched.Next(now))
	if gap > time.Minute {
		t.Errorf("sweep runs every %v; a reminder could be that much early", gap)
	}
	if muteReminderLead < 5*time.Minute {
		t.Errorf("lead of %v leaves no time to extend a mute before it lapses", muteReminderLead)
	}
}

// Selection is the part that decides whether anyone is bothered and whether
// anyone is bothered twice, so it runs against a real database.
//
//	ALERT_E2E=1 go test ./alert/ -run MuteReminderSelects -v
func TestMuteReminderSelectsOnlyWhatIsDue(t *testing.T) {
	if os.Getenv("ALERT_E2E") == "" {
		t.Skip("set ALERT_E2E=1 with MySQL and Redis reachable to run this")
	}

	cfg := config.Load()
	if err := database.InitMySQL(cfg); err != nil {
		t.Fatalf("mysql: %v", err)
	}
	timezone.Set("Asia/Shanghai")

	const marker = "__mute_reminder_test__"
	database.DB.Exec(`DELETE FROM alert_mutes WHERE group_key LIKE ?`, marker+"%")
	t.Cleanup(func() {
		database.DB.Exec(`DELETE FROM alert_mutes WHERE group_key LIKE ?`, marker+"%")
	})

	// created_at is set explicitly: a mute's whole life, not just what is left
	// of it, decides whether a heads-up carries anything.
	rows := []struct {
		key            string
		createdMinsAgo int
		untilMins      int
		already        bool
	}{
		{marker + "-due", 60, 5, false},     // an hour-long mute, 5 minutes left
		{marker + "-far", 10, 120, false},   // not close enough yet
		{marker + "-lapsed", 60, -5, false}, // already over
		{marker + "-done", 60, 5, true},     // already announced
		{marker + "-short", 1, 5, false},    // a six-minute mute; the setter is watching it
	}
	for _, r := range rows {
		reminded := "NULL"
		if r.already {
			reminded = "NOW()"
		}
		if _, err := database.DB.Exec(
			`INSERT INTO alert_mutes (rule_id, group_key, mute_until, reason, created_by, created_at, reminded_at)
			 VALUES (1, ?, NOW() + INTERVAL ? MINUTE, 'test', 'tester', NOW() - INTERVAL ? MINUTE, `+reminded+`)`,
			r.key, r.untilMins, r.createdMinsAgo); err != nil {
			t.Fatalf("insert %s: %v", r.key, err)
		}
	}

	due, err := dueMuteReminders()
	if err != nil {
		t.Fatalf("dueMuteReminders: %v", err)
	}

	var keys []string
	for _, m := range due {
		if len(m.GroupKey) >= len(marker) && m.GroupKey[:len(marker)] == marker {
			keys = append(keys, m.GroupKey)
		}
	}
	if len(keys) != 1 || keys[0] != marker+"-due" {
		t.Errorf("selected %v, want exactly [%s-due] — a mute that is far off, already "+
			"lapsed, already announced, or too short to be worth announcing must not "+
			"be picked up", keys, marker)
	}
}
