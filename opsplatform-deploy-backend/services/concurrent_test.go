package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 所有测试都应在 `go test -race` 下通过。存在任何 race warning 视为回归。

func TestBounded_AllComplete(t *testing.T) {
	jobs := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	results := RunBoundedConcurrent(context.Background(), jobs, 3,
		func(_ context.Context, n int) int { return n * n }, nil)
	for i, r := range results {
		want := jobs[i] * jobs[i]
		if r != want {
			t.Fatalf("results[%d]=%d want %d", i, r, want)
		}
	}
}

func TestBounded_RespectsLimit(t *testing.T) {
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	jobs := make([]int, 50)
	limit := 5
	_ = RunBoundedConcurrent(context.Background(), jobs, limit,
		func(_ context.Context, _ int) struct{} {
			cur := inflight.Add(1)
			for {
				// 以 CAS 方式记录峰值
				old := maxInflight.Load()
				if cur <= old || maxInflight.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inflight.Add(-1)
			return struct{}{}
		}, nil)
	if got := maxInflight.Load(); got > int32(limit) {
		t.Fatalf("max in-flight %d exceeded limit %d", got, limit)
	}
}

func TestBounded_OnEachProgressive(t *testing.T) {
	jobs := make([]int, 20)
	for i := range jobs {
		jobs[i] = i
	}
	var seen atomic.Int32
	var mu sync.Mutex
	snapshots := [][]int{}
	results := RunBoundedConcurrent(context.Background(), jobs, 4,
		func(_ context.Context, n int) int { return n + 100 },
		func(idx int, rs []int) {
			// rs 是 results 的引用；测试这里模拟调用方做快照
			cp := make([]int, len(rs))
			copy(cp, rs)
			mu.Lock()
			snapshots = append(snapshots, cp)
			mu.Unlock()
			seen.Add(1)
		})
	if int(seen.Load()) != len(jobs) {
		t.Fatalf("onEach called %d times, want %d", seen.Load(), len(jobs))
	}
	if len(results) != len(jobs) {
		t.Fatalf("results len %d, want %d", len(results), len(jobs))
	}
	for i, r := range results {
		if r != i+100 {
			t.Fatalf("results[%d]=%d want %d", i, r, i+100)
		}
	}
	// 最后一次快照应该完整
	last := snapshots[len(snapshots)-1]
	for i, v := range last {
		if v != i+100 {
			t.Fatalf("last snapshot[%d]=%d want %d", i, v, i+100)
		}
	}
}

func TestBounded_CancelPropagation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	jobs := make([]int, 100)
	startCount := atomic.Int32{}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_ = RunBoundedConcurrent(ctx, jobs, 5,
		func(c context.Context, _ int) int {
			startCount.Add(1)
			select {
			case <-time.After(200 * time.Millisecond):
				return 1
			case <-c.Done():
				return -1
			}
		}, nil)
	// 100 个 jobs，limit=5，20ms 后 cancel → 只有前几批能启动
	if got := startCount.Load(); got >= 100 {
		t.Fatalf("cancel 没起作用，100 个 job 全部启动了 (实际启动 %d)", got)
	}
}

func TestBounded_EmptyJobs(t *testing.T) {
	results := RunBoundedConcurrent(context.Background(), []int{}, 5,
		func(_ context.Context, _ int) int { return 1 }, nil)
	if len(results) != 0 {
		t.Fatalf("empty jobs should return empty results, got len=%d", len(results))
	}
}

func TestBounded_ZeroLimitClampedToOne(t *testing.T) {
	jobs := []int{1, 2, 3}
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	_ = RunBoundedConcurrent(context.Background(), jobs, 0,
		func(_ context.Context, _ int) int {
			cur := inflight.Add(1)
			for {
				old := maxInflight.Load()
				if cur <= old || maxInflight.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inflight.Add(-1)
			return 0
		}, nil)
	if maxInflight.Load() != 1 {
		t.Fatalf("limit=0 should clamp to 1, but max in-flight was %d", maxInflight.Load())
	}
}
