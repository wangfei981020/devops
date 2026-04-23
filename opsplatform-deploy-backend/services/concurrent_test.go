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
		func(_ context.Context, n int, _ func(int)) int { return n * n }, nil)
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
		func(_ context.Context, _ int, _ func(struct{})) struct{} {
			cur := inflight.Add(1)
			for {
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
		func(_ context.Context, n int, _ func(int)) int { return n + 100 },
		func(_ int, rs []int) {
			cp := make([]int, len(rs))
			copy(cp, rs)
			mu.Lock()
			snapshots = append(snapshots, cp)
			mu.Unlock()
			seen.Add(1)
		})
	// workFn 只通过最终返回值触发 1 次 publish，所以 onEach 调用 = jobs 数
	if int(seen.Load()) != len(jobs) {
		t.Fatalf("onEach called %d times, want %d", seen.Load(), len(jobs))
	}
	for i, r := range results {
		if r != i+100 {
			t.Fatalf("results[%d]=%d want %d", i, r, i+100)
		}
	}
	last := snapshots[len(snapshots)-1]
	for i, v := range last {
		if v != i+100 {
			t.Fatalf("last snapshot[%d]=%d want %d", i, v, i+100)
		}
	}
}

func TestBounded_ProgressiveMultiPublish(t *testing.T) {
	// 新能力测试：workFn 在执行过程中多次调用 publish 推送中间值
	jobs := []int{0, 1, 2}
	var onEachCount atomic.Int32
	var mu sync.Mutex
	allSnapshots := [][]int{}
	results := RunBoundedConcurrent(context.Background(), jobs, 3,
		func(_ context.Context, n int, publish func(int)) int {
			publish(n * 10)  // 中间状态 1
			publish(n * 100) // 中间状态 2
			return n * 1000  // 最终状态
		},
		func(_ int, rs []int) {
			onEachCount.Add(1)
			cp := make([]int, len(rs))
			copy(cp, rs)
			mu.Lock()
			allSnapshots = append(allSnapshots, cp)
			mu.Unlock()
		})
	// 每个 job 3 次 publish（2 中间 + 1 最终）→ onEach 共 9 次
	if int(onEachCount.Load()) != 9 {
		t.Fatalf("onEach count = %d, want 9 (3 publishes × 3 jobs)", onEachCount.Load())
	}
	// 最终结果应该是最后的 final 值
	want := []int{0, 1000, 2000}
	for i, r := range results {
		if r != want[i] {
			t.Fatalf("results[%d]=%d want %d", i, r, want[i])
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
		func(c context.Context, _ int, _ func(int)) int {
			startCount.Add(1)
			select {
			case <-time.After(200 * time.Millisecond):
				return 1
			case <-c.Done():
				return -1
			}
		}, nil)
	if got := startCount.Load(); got >= 100 {
		t.Fatalf("cancel 没起作用，100 个 job 全部启动了 (实际启动 %d)", got)
	}
}

func TestBounded_EmptyJobs(t *testing.T) {
	results := RunBoundedConcurrent(context.Background(), []int{}, 5,
		func(_ context.Context, _ int, _ func(int)) int { return 1 }, nil)
	if len(results) != 0 {
		t.Fatalf("empty jobs should return empty results, got len=%d", len(results))
	}
}

func TestBounded_ZeroLimitClampedToOne(t *testing.T) {
	jobs := []int{1, 2, 3}
	var inflight atomic.Int32
	var maxInflight atomic.Int32
	_ = RunBoundedConcurrent(context.Background(), jobs, 0,
		func(_ context.Context, _ int, _ func(int)) int {
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
