package alert

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
)

// The Redis lease makes two replicas sweeping at once unlikely; the conditional
// update is what makes a double notice impossible. They are different
// guarantees, and only the second one holds when the lease is unavailable —
// which it is by design, since tryLock fails open rather than skipping a sweep.
//
// So the claim is exercised here directly, with the lease out of the picture:
// many workers racing for one mute, exactly one may win.
//
//	ALERT_E2E=1 go test ./alert/ -run MuteReminderClaimRace -v
func TestMuteReminderClaimIsExclusiveUnderRace(t *testing.T) {
	if os.Getenv("ALERT_E2E") == "" {
		t.Skip("set ALERT_E2E=1 with MySQL reachable to run this")
	}

	cfg := config.Load()
	if err := database.InitMySQL(cfg); err != nil {
		t.Fatalf("mysql: %v", err)
	}

	const marker = "__mute_claim_race__"
	database.DB.Exec(`DELETE FROM alert_mutes WHERE group_key = ?`, marker)
	t.Cleanup(func() {
		database.DB.Exec(`DELETE FROM alert_mutes WHERE group_key = ?`, marker)
	})

	res, err := database.DB.Exec(
		`INSERT INTO alert_mutes (rule_id, group_key, mute_until, reason, created_by, created_at)
		 VALUES (1, ?, NOW() + INTERVAL 5 MINUTE, 'race', 'tester', NOW() - INTERVAL 60 MINUTE)`,
		marker)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	id, _ := res.LastInsertId()

	const racers = 12
	var winners atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them together
			r, err := database.DB.Exec(
				`UPDATE alert_mutes SET reminded_at = NOW() WHERE id = ? AND reminded_at IS NULL`, id)
			if err != nil {
				return
			}
			if n, _ := r.RowsAffected(); n == 1 {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := winners.Load(); got != 1 {
		t.Errorf("%d of %d racers claimed the same mute; exactly one may win, "+
			"or the group gets the notice that many times", got, racers)
	}

	// And it stays claimed: a later sweep must not pick it up again.
	due, err := dueMuteReminders()
	if err != nil {
		t.Fatalf("dueMuteReminders: %v", err)
	}
	for _, m := range due {
		if m.GroupKey == marker {
			t.Error("a claimed mute was selected again by a later sweep")
		}
	}
}
