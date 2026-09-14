package alert

import (
	"os"
	"testing"
	"time"

	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
)

// Proves the whole chain rather than its parts: a setting is stored, the cron
// entry fires on its own, the lock is taken, and a row actually disappears.
// Every other retention test stubs something; this one stubs nothing.
//
// It needs a live MySQL and Redis, so it only runs when asked:
//
//	ALERT_E2E=1 go test ./alert/ -run RetentionEndToEnd -v
//
// Safety: it inserts one marker row dated well beyond any realistic retention
// window and keeps 365 days, so nothing else in the database is old enough to
// qualify. The setting is restored on the way out.
func TestRetentionEndToEndDeletesOnSchedule(t *testing.T) {
	if os.Getenv("ALERT_E2E") == "" {
		t.Skip("set ALERT_E2E=1 with MySQL and Redis reachable to run this")
	}

	cfg := config.Load()
	if err := database.InitMySQL(cfg); err != nil {
		t.Fatalf("mysql: %v", err)
	}
	if err := database.InitRedis(cfg); err != nil {
		t.Fatalf("redis: %v", err)
	}

	const marker = "__retention_e2e_marker__"
	countMarker := func() int {
		var n int
		database.DB.QueryRow(`SELECT COUNT(*) FROM alert_logs WHERE rule_name = ?`, marker).Scan(&n)
		return n
	}

	// A row far older than the window, so only it qualifies.
	if _, err := database.DB.Exec(`INSERT INTO alert_logs
		(rule_id, rule_name, severity, message, status, created_at)
		VALUES (0, ?, 'S3', 'retention end-to-end marker', 'success', NOW() - INTERVAL 400 DAY)`,
		marker); err != nil {
		t.Fatalf("insert marker: %v", err)
	}
	t.Cleanup(func() {
		database.DB.Exec(`DELETE FROM alert_logs WHERE rule_name = ?`, marker)
	})

	if countMarker() != 1 {
		t.Fatal("marker row was not inserted")
	}

	previous := database.GetSetting(database.SettingLogRetentionDays, "")
	if err := database.PutSetting(database.SettingLogRetentionDays, "365", "e2e-test"); err != nil {
		t.Fatalf("store retention: %v", err)
	}
	t.Cleanup(func() {
		database.PutSetting(database.SettingLogRetentionDays, previous, "e2e-test")
	})

	// Count what else is in range, so the assertion can tell "the job ran" from
	// "the job deleted more than it should have".
	var otherOld int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_logs
		WHERE created_at < NOW() - INTERVAL 365 DAY AND rule_name <> ?`, marker).Scan(&otherOld)
	var totalBefore int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_logs`).Scan(&totalBefore)

	savedSchedule := retentionSchedule
	retentionSchedule = "*/1 * * * * *" // every second, so the test can watch it
	t.Cleanup(func() { retentionSchedule = savedSchedule })

	// Only the retention entry is registered — Start() would also schedule every
	// alert rule, and this test must not put real alerts on the wire.
	e := NewEngine()
	if err := e.startRetentionJob(); err != nil {
		t.Fatalf("startRetentionJob: %v", err)
	}
	e.cron.Start()
	t.Cleanup(func() { <-e.cron.Stop().Done() })

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if countMarker() == 0 {
			var totalAfter int
			database.DB.QueryRow(`SELECT COUNT(*) FROM alert_logs`).Scan(&totalAfter)
			want := totalBefore - 1 - otherOld
			if totalAfter != want {
				t.Errorf("scheduled prune removed the wrong rows: total %d -> %d, want %d",
					totalBefore, totalAfter, want)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the scheduled job never deleted the marker row within 8s")
}
