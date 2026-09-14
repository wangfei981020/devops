package alert

import (
	"log"
	"time"

	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/timezone"
)

// Alert history grows forever unless somebody remembers to press a button, and
// nobody remembers. The platform now prunes it on a schedule, keeping however
// many days an operator asked for.
//
// Retention is off by default. Turning it on is a decision to delete an
// installation's own audit trail, and that is the operator's to make — a value
// silently chosen here would quietly destroy history a customer may be relying
// on for post-incident review.

const (
	// retentionBatch bounds a single DELETE. A table left unpruned for a year
	// can hold millions of rows, and removing them in one statement would hold
	// locks long enough to stall the alerting that writes to the same table.
	retentionBatch = 5000

	// retentionMaxBatches stops one pass from running indefinitely. Whatever is
	// left is taken by the next pass rather than by a job that never returns.
	retentionMaxBatches = 200

	retentionLockKey = "alert:lock:retention"
)

// RetentionDays reads the configured retention, or 0 when pruning is off.
// A stored value that is not a positive number is treated as off, because the
// safe reading of a malformed setting is "do not delete anything".
func RetentionDays() int {
	return database.GetSettingInt(database.SettingLogRetentionDays, 0)
}

// PruneAlertLogs deletes alert_logs older than the configured retention and
// returns how many rows went. It is safe to call from several replicas: the
// same Redis lease the rule executions use keeps one pass at a time.
func PruneAlertLogs() (int64, error) {
	days := RetentionDays()
	if days <= 0 {
		return 0, nil
	}

	release, ok := tryLock(retentionLockKey, 30*time.Minute)
	if !ok {
		log.Printf("[Retention] another replica is pruning, skipping")
		return 0, nil
	}
	defer release()

	var total int64
	for range retentionMaxBatches {
		res, err := database.DB.Exec(
			`DELETE FROM alert_logs WHERE created_at < NOW() - INTERVAL ? DAY LIMIT ?`,
			days, retentionBatch)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < retentionBatch {
			break
		}
	}

	if total > 0 {
		log.Printf("[Retention] Removed %d alert_logs rows older than %d days", total, days)
	}
	return total, nil
}

// retentionSchedule is when the prune runs: 03:30 in the platform's own
// timezone, which is a quiet hour for an alerting system. A variable rather
// than a literal so a test can drive the registration end to end on a schedule
// that fires within a test's lifetime — a setting whose job never actually
// fires is indistinguishable from one that does not exist.
var retentionSchedule = "0 30 3 * * *"

// nextRetentionRun is when the prune is next due, in the platform's zone. Zero
// if the expression will not parse, which startRetentionJob reports as an error
// anyway.
func nextRetentionRun() time.Time {
	sched, err := ParseSchedule(retentionSchedule)
	if err != nil {
		return time.Time{}
	}
	return sched.Next(time.Now().In(timezone.Location()))
}

// startRetentionJob registers the daily prune. It is deliberately not tied to a
// rule's schedule: retention is a platform-level chore, not a rule's.
func (e *Engine) startRetentionJob() error {
	_, err := e.cron.AddFunc(retentionSchedule, func() {
		days := RetentionDays()
		if days <= 0 {
			return
		}
		log.Printf("[Retention] starting scheduled prune, keeping %d days", days)
		if _, err := PruneAlertLogs(); err != nil {
			log.Printf("[Retention] prune failed: %v", err)
		}
	})
	if err != nil {
		return err
	}

	// Say out loud that it is armed and when it next runs. Retention is
	// invisible by nature — nothing happens until it deletes something — so an
	// operator has no other way to tell a working schedule from a silent one.
	//
	// The time comes from the expression rather than from the registered entry:
	// cron only fills in an entry's Next once it is running, and this is called
	// before Start, so asking the entry would print a blank at exactly the
	// moment the line is meant to be read.
	next := nextRetentionRun()
	if days := RetentionDays(); days > 0 {
		log.Printf("[Retention] armed: keep %d days, next run %s", days, timezone.FormatWithZone(next))
	} else {
		log.Printf("[Retention] scheduled but disabled (retention = 0); next check %s",
			timezone.FormatWithZone(next))
	}
	return nil
}
