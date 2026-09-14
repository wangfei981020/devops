package alert

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
)

// tryLock is the one thing standing between several replicas and several copies
// of the same alert, the same daily report and the same retention pass. Its unit
// tests cover the fail-open path and the lease arithmetic; these cover the part
// that only shows up against a real Redis under contention.
//
//	ALERT_E2E=1 go test ./alert/ -run 'Lock.*Race|LockKey|LockRelease' -v

func requireRedis(t *testing.T) {
	t.Helper()
	if os.Getenv("ALERT_E2E") == "" {
		t.Skip("set ALERT_E2E=1 with Redis reachable to run this")
	}
	if database.RDB == nil {
		if err := database.InitRedis(config.Load()); err != nil {
			t.Fatalf("redis: %v", err)
		}
	}
}

// Many callers, one key: exactly one may hold it. Without this the daily report
// would go out once per replica, which is the most visible duplicate the
// platform can produce — it lands in the group N times.
func TestLockIsExclusiveUnderRace(t *testing.T) {
	requireRedis(t)

	// The key the daily-report job itself builds, not a copy of the expression.
	key := reportLockKey(90001)
	t.Cleanup(func() { database.RDB.Del(t.Context(), key) })

	const racers = 16
	var holders, peak atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, ok := tryLock(key, time.Minute)
			if !ok {
				return
			}
			// Count concurrent holders rather than winners: a lock that hands
			// out one lease at a time but lets a second in before the first
			// releases would still be broken.
			n := holders.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(30 * time.Millisecond)
			holders.Add(-1)
			release()
		}()
	}
	close(start)
	wg.Wait()

	if got := peak.Load(); got != 1 {
		t.Errorf("%d holders at once out of %d racers; the lease must admit one", got, racers)
	}
}

// Each rule's report has its own key. Sharing one would serialise every rule's
// report behind the first, and the rest would be silently skipped for the day.
func TestLockKeysAreScopedPerRule(t *testing.T) {
	requireRedis(t)

	a := reportLockKey(90002)
	b := reportLockKey(90003)
	t.Cleanup(func() { database.RDB.Del(t.Context(), a, b) })

	releaseA, okA := tryLock(a, time.Minute)
	if !okA {
		t.Fatal("could not take the first lease")
	}
	defer releaseA()

	releaseB, okB := tryLock(b, time.Minute)
	if !okB {
		t.Fatal("a second rule was blocked by the first rule's lease; every report " +
			"after the first would be skipped")
	}
	releaseB()
}

// A holder that overran its lease must not delete the lease a later run
// legitimately owns — otherwise a slow pass would unlock its own successor and
// let a third in alongside it.
func TestReleaseDoesNotStealASuccessorsLease(t *testing.T) {
	requireRedis(t)

	key := reportLockKey(90004)
	t.Cleanup(func() { database.RDB.Del(t.Context(), key) })

	// A lease short enough to lapse while its holder is still "working".
	releaseSlow, ok := tryLock(key, 300*time.Millisecond)
	if !ok {
		t.Fatal("could not take the short lease")
	}
	time.Sleep(500 * time.Millisecond) // it expires here

	releaseNext, ok := tryLock(key, time.Minute)
	if !ok {
		t.Fatal("the expired lease was never released by Redis")
	}
	defer releaseNext()

	releaseSlow() // the overrun holder finishes and tries to clean up

	if n, err := database.RDB.Exists(t.Context(), key).Result(); err != nil {
		t.Fatalf("redis: %v", err)
	} else if n != 1 {
		t.Error("the overrun holder deleted its successor's lease; two runs could " +
			"then hold it at once")
	}
}

// A rule's own run and its daily report are separate jobs on separate schedules;
// sharing a lease would mean the report silently skipped whenever the rule
// happened to be running, which for a once-a-day job means skipped for the day.
func TestRuleAndReportLeasesAreIndependent(t *testing.T) {
	requireRedis(t)

	const ruleID = 90005
	rk, pk := ruleLockKey(ruleID), reportLockKey(ruleID)
	if rk == pk {
		t.Fatalf("a rule and its report share the key %q", rk)
	}
	t.Cleanup(func() { database.RDB.Del(t.Context(), rk, pk) })

	releaseRule, ok := tryLock(rk, time.Minute)
	if !ok {
		t.Fatal("could not take the rule lease")
	}
	defer releaseRule()

	releaseReport, ok := tryLock(pk, time.Minute)
	if !ok {
		t.Fatal("the report was blocked by its own rule's run; a daily report " +
			"would be dropped for the day whenever the two overlapped")
	}
	releaseReport()
}
