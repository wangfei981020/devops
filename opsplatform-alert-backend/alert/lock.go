package alert

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"

	"opsplatform-alert-backend/database"
)

// One run of a rule at a time, across the whole platform.
//
// The scheduler lives inside the process, so two things can put the same rule
// on the wire twice. Running more than one replica gives every replica its own
// scheduler and every alert goes out N times. And within a single replica, cron
// starts the next run on schedule whether or not the previous one has finished
// — a Loki query slower than the rule's own interval overlaps itself and fires
// the same alert again. A short-lived lock in Redis covers both, and Redis is
// already a dependency.
//
// The lock deliberately FAILS OPEN. If Redis is unreachable the run proceeds.
// For an alerting platform a duplicate alert is an annoyance and a missed one
// is the failure the whole product exists to prevent, so an outage in the
// deduplication layer must never silence alerting.

// jobGuards wrap every scheduled job.
//
// cron runs each job as a bare goroutine, so a panic anywhere inside one takes
// the whole process down with it — one malformed template or one nil map in a
// single rule would stop every other rule from ever firing again, which for an
// alerting platform is the worst possible blast radius. Recover contains it to
// the job that panicked.
//
// SkipIfStillRunning is the local half of the overlap guard. The Redis lease
// below is the authoritative one because it also covers other replicas, but it
// fails open by design: when Redis is unreachable this keeps a slow rule from
// piling up on itself inside this process.
var jobGuards = []cron.JobWrapper{
	cron.Recover(cron.DefaultLogger),
	cron.SkipIfStillRunning(cron.DefaultLogger),
}

// Keys are built through these rather than concatenated at the call site, so a
// test can assert on the key a job actually takes instead of on a second copy
// of the same expression. Scoping matters as much as the lock itself: one key
// shared across rules would serialise every rule behind the first and silently
// drop the rest for that window.
func ruleLockKey(ruleID int) string   { return ruleLockPrefix + strconv.Itoa(ruleID) }
func reportLockKey(ruleID int) string { return reportLockPrefix + strconv.Itoa(ruleID) }

const (
	ruleLockPrefix   = "alert:lock:rule:"
	reportLockPrefix = "alert:lock:report:"

	// Bounds on the lease. Too short and a slow run loses its own lock while
	// still working; too long and a process that dies mid-run blocks the rule
	// until the lease expires.
	minLockTTL = time.Minute
	maxLockTTL = 30 * time.Minute
)

// releaseIfMine deletes the key only when it still holds our token, so a run
// that overran its lease cannot delete the lock a later run legitimately owns.
var releaseIfMine = redis.NewScript(`
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		return redis.call("DEL", KEYS[1])
	end
	return 0
`)

// lockTTL derives the lease from the rule's own schedule: two intervals, so an
// ordinary slow run keeps its lock, clamped at both ends.
func lockTTL(schedule string) time.Duration {
	sched, err := ParseSchedule(schedule)
	if err != nil {
		return 5 * time.Minute
	}
	now := time.Now()
	first := sched.Next(now)
	interval := sched.Next(first).Sub(first)

	ttl := interval * 2
	if ttl < minLockTTL {
		return minLockTTL
	}
	if ttl > maxLockTTL {
		return maxLockTTL
	}
	return ttl
}

// tryLock takes the lock and returns a release function. ok is false only when
// another run genuinely holds it — never because Redis is unavailable.
func tryLock(key string, ttl time.Duration) (release func(), ok bool) {
	noop := func() {}
	if database.RDB == nil {
		return noop, true
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return noop, true
	}
	token := hex.EncodeToString(buf)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	acquired, err := database.RDB.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		log.Printf("[Lock] %s: redis unavailable (%v); running anyway", key, err)
		return noop, true
	}
	if !acquired {
		return noop, false
	}

	return func() {
		rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer rcancel()
		if err := releaseIfMine.Run(rctx, database.RDB, []string{key}, token).Err(); err != nil && err != redis.Nil {
			// The lease expires on its own, so this is worth seeing but not
			// worth failing the run over.
			log.Printf("[Lock] %s: release failed (%v); lease will expire", key, err)
		}
	}, true
}
