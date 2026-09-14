package alert

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/timezone"
)

// The Redis lease keeps two replicas from sweeping at once, but it fails open,
// so on a day Redis is down it is this claim alone that stops a daily report
// arriving once per replica.
//
//	ALERT_E2E=1 go test ./alert/ -run DailyReportClaim -v
func TestDailyReportClaimIsExclusiveAndPerDay(t *testing.T) {
	if os.Getenv("ALERT_E2E") == "" {
		t.Skip("set ALERT_E2E=1 with MySQL reachable to run this")
	}

	cfg := config.Load()
	if err := database.InitMySQL(cfg); err != nil {
		t.Fatalf("mysql: %v", err)
	}

	// A rule of its own, so a real one's state is never touched.
	res, err := database.DB.Exec(`INSERT INTO alert_rules
		(name, es_connection_id, lark_config_id, es_index, schedule, time_range, status)
		VALUES ('__report_claim_test__', 0, 0, '*', '*/5 * * * *', '5m', 0)`)
	if err != nil {
		t.Fatalf("insert rule: %v", err)
	}
	id64, _ := res.LastInsertId()
	ruleID := int(id64)
	t.Cleanup(func() { database.DB.Exec(`DELETE FROM alert_rules WHERE id = ?`, ruleID) })

	const day = "20260907"

	// Many replicas waking at the same minute.
	const racers = 12
	var winners atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if claimDailyReport(ruleID, day) {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := winners.Load(); got != 1 {
		t.Errorf("%d of %d replicas claimed the same day; the group would get the "+
			"report that many times", got, racers)
	}

	if claimDailyReport(ruleID, day) {
		t.Error("the same day was claimable twice")
	}

	// A new day has to be claimable, or the report sends once and never again —
	// a condition written as "only if never sent" would pass every test above
	// and silently stop reporting after the first day.
	if !claimDailyReport(ruleID, "20260908") {
		t.Error("the next day was not claimable; reporting would stop after one day")
	}
}

// The claim's day and the schedule's hour have to come from the same clock. If
// the report fires at 00:30 in the display zone while its date rolls over in the
// process zone, a report covers the wrong day — and near midnight, claims the
// wrong one too.
func TestReportDayKeyFollowsTheDisplayZone(t *testing.T) {
	t.Cleanup(func() { timezone.Set("") })

	if err := timezone.Set("UTC"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	utcToday := reportDayKey(0)

	if err := timezone.Set("Pacific/Kiritimati"); err != nil { // +14, always ahead
		t.Fatalf("Set: %v", err)
	}
	aheadToday := reportDayKey(0)

	if utcToday == aheadToday {
		// Only equal for the ten hours a day the two share a date; the point is
		// that the value tracks the setting at all.
		t.Logf("both zones are on %s right now; checking the offset instead", utcToday)
		want := time.Now().In(timezone.Location()).Format("20060102")
		if aheadToday != want {
			t.Errorf("reportDayKey = %s, want %s in %s", aheadToday, want, timezone.Name())
		}
		return
	}
	if aheadToday < utcToday {
		t.Errorf("a zone ahead of UTC produced an earlier day: %s < %s", aheadToday, utcToday)
	}

	// Yesterday must still be exactly one day back in whichever zone is set.
	if got, want := reportDayKey(-1),
		time.Now().In(timezone.Location()).AddDate(0, 0, -1).Format("20060102"); got != want {
		t.Errorf("reportDayKey(-1) = %s, want %s", got, want)
	}
}
