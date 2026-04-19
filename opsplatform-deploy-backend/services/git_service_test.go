package services

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLockMgr_SerializesSameKey(t *testing.T) {
	m := NewLockMgr()
	var inCS int32
	var maxInCS int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Acquire("repo-A")
			defer m.Release("repo-A")
			cur := atomic.AddInt32(&inCS, 1)
			if cur > atomic.LoadInt32(&maxInCS) {
				atomic.StoreInt32(&maxInCS, cur)
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inCS, -1)
		}()
	}
	wg.Wait()
	if atomic.LoadInt32(&maxInCS) != 1 {
		t.Fatalf("expected max 1 concurrent for same key, got %d", maxInCS)
	}
}

func TestLockMgr_ParallelsDifferentKeys(t *testing.T) {
	m := NewLockMgr()
	done := make(chan bool, 2)
	start := time.Now()
	for _, k := range []string{"A", "B"} {
		k := k
		go func() {
			m.Acquire(k)
			defer m.Release(k)
			time.Sleep(30 * time.Millisecond)
			done <- true
		}()
	}
	<-done
	<-done
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("different keys should run in parallel, took %v", elapsed)
	}
}
