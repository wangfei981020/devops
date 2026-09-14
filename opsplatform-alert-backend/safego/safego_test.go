package safego

import (
	"sync"
	"testing"
	"time"
)

// If this package did not work, this file would not fail — the test binary
// would die. That is the whole point: a panic in a goroutine has no boundary,
// so surviving the run IS the assertion.
func TestGoContainsAPanic(t *testing.T) {
	done := make(chan struct{})
	Go("test panicker", func() {
		defer close(done)
		panic("boom")
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the goroutine never ran")
	}

	// Reaching here at all means the panic did not take the process with it.
	Go("test survivor", func() {})
	time.Sleep(50 * time.Millisecond)
}

// One worker failing must not stop its siblings. This is the grouped-rule and
// per-namespace shape: several workers on one semaphore, one of them bad.
func TestRecoverLetsSiblingWorkersFinish(t *testing.T) {
	const workers = 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	finished := []int{}

	for i := range workers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			defer Recover("worker")
			if n == 2 {
				var m map[string]string
				m["nil map write"] = "boom" // the realistic failure
			}
			mu.Lock()
			finished = append(finished, n)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if len(finished) != workers-1 {
		t.Errorf("%d of %d workers finished, want %d — one panicking worker must not stop the rest",
			len(finished), workers, workers-1)
	}
	for _, n := range finished {
		if n == 2 {
			t.Error("the panicking worker reported success")
		}
	}
}

// Recover releases whatever the frame deferred after it, so a worker that dies
// still gives its semaphore slot back. Without that, a single panic would wedge
// the pool for every later run.
func TestRecoverStillReleasesTheSemaphore(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer Recover("slot holder")
		defer func() { <-sem }()
		panic("die while holding the slot")
	}()
	<-done

	select {
	case sem <- struct{}{}:
	default:
		t.Fatal("the slot was never released; the pool would be wedged")
	}
}
