package cluster

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeKeeper 可编程的租约后端，不碰数据库。
type fakeKeeper struct {
	mu        sync.Mutex
	lease     Lease
	held      bool
	err       error
	acquires  int
	releases  int
	onAcquire func(n int)
}

func (f *fakeKeeper) Acquire(_ context.Context, _ string, _ time.Duration) (Lease, bool, error) {
	f.mu.Lock()
	f.acquires++
	n := f.acquires
	cb := f.onAcquire
	f.mu.Unlock()
	if cb != nil {
		cb(n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lease, f.held, f.err
}

func (f *fakeKeeper) Release(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	return nil
}

func (f *fakeKeeper) set(l Lease, held bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lease, f.held, f.err = l, held, err
}

// ★ 抢到租约后要变成 leader，并记下栅栏令牌。
func TestLeaderBecomesLeader(t *testing.T) {
	f := &fakeKeeper{}
	f.set(Lease{Name: "sched", Owner: "pod-a", ExpiresAt: time.Now().Add(time.Minute), Fence: 7}, true, nil)
	l := NewLeader(f, "sched", 3*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)

	waitFor(t, func() bool { return l.IsLeader() }, "没能成为 leader")
	if got := l.Fence(); got != 7 {
		t.Errorf("Fence = %d, want 7", got)
	}
}

// ★ 抢不到时不是错误，只是不当 leader。
func TestLeaderStaysFollower(t *testing.T) {
	f := &fakeKeeper{}
	f.set(Lease{Name: "sched", Owner: "pod-b", ExpiresAt: time.Now().Add(time.Minute), Fence: 3}, false, nil)
	l := NewLeader(f, "sched", 3*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)

	time.Sleep(150 * time.Millisecond)
	if l.IsLeader() {
		t.Fatal("租约在别人手上，不该认为自己是 leader")
	}
	if l.Fence() != 0 {
		t.Errorf("非 leader 的 Fence 应为 0，得到 %d", l.Fence())
	}
}

// ★★ 续约失败但租约未到期时，**不能退位**。
//
// 退位会造成一段没人跑定时任务的空窗 —— 而这时别的副本多半也连不上库，
// 同样抢不到，于是谁都不干活。这个行为最容易被写反，所以单独测。
func TestLeaderKeepsRoleWhileLeaseStillValid(t *testing.T) {
	f := &fakeKeeper{}
	valid := Lease{Name: "sched", Owner: "pod-a", ExpiresAt: time.Now().Add(time.Minute), Fence: 1}
	f.set(valid, true, nil)
	l := NewLeader(f, "sched", 3*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	waitFor(t, func() bool { return l.IsLeader() }, "没能成为 leader")

	// 库挂了，但租约还有效
	f.set(valid, false, errors.New("dial tcp: connection refused"))
	time.Sleep(200 * time.Millisecond)
	if !l.IsLeader() {
		t.Fatal("续约失败但租约未到期，仍应是 leader")
	}
}

// ★★ 租约真的过期了，才退位。
func TestLeaderStepsDownWhenLeaseExpired(t *testing.T) {
	f := &fakeKeeper{}
	f.set(Lease{Name: "sched", Owner: "pod-a", ExpiresAt: time.Now().Add(time.Minute), Fence: 1}, true, nil)
	l := NewLeader(f, "sched", time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	waitFor(t, func() bool { return l.IsLeader() }, "没能成为 leader")

	// 库不可达，且租约已经过期
	f.set(Lease{Name: "sched", Owner: "pod-a", ExpiresAt: time.Now().Add(-time.Second), Fence: 1},
		false, errors.New("connection refused"))
	waitFor(t, func() bool { return !l.IsLeader() }, "租约过期后仍认为自己是 leader")
}

// ★ 退出时必须主动释放，否则滚动更新会留下一个 TTL 长的空窗。
func TestLeaderReleasesOnShutdown(t *testing.T) {
	f := &fakeKeeper{}
	f.set(Lease{Name: "sched", Owner: "pod-a", ExpiresAt: time.Now().Add(time.Minute), Fence: 1}, true, nil)
	l := NewLeader(f, "sched", 3*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	waitFor(t, func() bool { return l.IsLeader() }, "没能成为 leader")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run 没有在 ctx 取消后返回")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.releases == 0 {
		t.Error("退出时没有释放租约 —— 接班副本要白等一个 TTL")
	}
	if l.IsLeader() {
		t.Error("退出后仍认为自己是 leader")
	}
}

// ★ 状态变化要回调（用于打日志），且只在**变化**时回调一次。
func TestLeaderOnChangeFiresOnce(t *testing.T) {
	f := &fakeKeeper{}
	f.set(Lease{Name: "sched", Owner: "pod-a", ExpiresAt: time.Now().Add(time.Minute), Fence: 2}, true, nil)
	l := NewLeader(f, "sched", time.Second)

	var mu sync.Mutex
	var became []bool
	l.OnChange = func(b bool, _ Lease) {
		mu.Lock()
		became = append(became, b)
		mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	go l.Run(ctx)
	waitFor(t, func() bool { return l.IsLeader() }, "没能成为 leader")
	time.Sleep(500 * time.Millisecond) // 期间续约多次，状态没变
	cancel()
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(became) != 2 || became[0] != true || became[1] != false {
		t.Errorf("OnChange 应为 [true false]（当选 + 退出），得到 %v —— 每次续约都回调会把日志刷爆", became)
	}
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}
