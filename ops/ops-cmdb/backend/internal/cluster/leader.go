package cluster

import (
	"context"
	"sync/atomic"
	"time"
)

// Leader 维持一份租约，并对外暴露"我现在是不是 leader"。
//
// # 续约周期怎么定
//
// 关键是 TTL 与续约间隔的比值。间隔太接近 TTL，一次网络抖动就会丢租约，
// 定时任务跟着抖；间隔太小则白白压库。
//
// 这里用 TTL/3：允许连续两次续约失败仍不丢租约，同时每 TTL 只写 3 次。
// 与 Kubernetes 自己的 leader election 默认值同一个量级。
// leaseKeeper 是 Leader 依赖的那一小块能力。
//
// 抽成接口只为一件事：**让状态机能脱离数据库被测到**。
// 「续约连失败两次仍不退位」「ctx 取消后仍要释放」这类行为，
// 用真库测就得靠断网、改时钟，既慢又不稳；用假实现三行就能构造。
type leaseKeeper interface {
	Acquire(ctx context.Context, name string, ttl time.Duration) (Lease, bool, error)
	Release(ctx context.Context, name string) error
}

type Leader struct {
	m    leaseKeeper
	name string
	ttl  time.Duration

	// isLeader 用原子量而不是加锁：读它的地方（每个定时任务触发点）
	// 远多于写它的地方（每 TTL/3 一次）。
	isLeader atomic.Bool
	fence    atomic.Int64

	// OnChange 状态变化时回调，用来打日志。可为 nil。
	OnChange func(became bool, l Lease)
}

// NewLeader 构造。调用 Run 才真正开始参选。
func NewLeader(m leaseKeeper, name string, ttl time.Duration) *Leader {
	return &Leader{m: m, name: name, ttl: ttl}
}

// IsLeader 当前是否持有租约。
//
//	⚠️ 返回 true 只代表"最近一次续约成功且还没到期"。
//	见 Lease 的注释：这不足以让非幂等操作安全，
//	那类操作还要配合 Fence 或数据层幂等。
func (l *Leader) IsLeader() bool { return l.isLeader.Load() }

// Fence 当前租约的栅栏令牌。非 leader 时返回 0。
func (l *Leader) Fence() int64 {
	if !l.isLeader.Load() {
		return 0
	}
	return l.fence.Load()
}

// Run 参选并持续续约，直到 ctx 取消。阻塞，通常放在 goroutine 里。
//
// 退出时主动释放租约，让接班的副本不必等到自然过期 ——
// 滚动更新时这能把"没人跑定时任务"的空窗从一个 TTL 缩到几乎为零。
func (l *Leader) Run(ctx context.Context) {
	interval := l.ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	l.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			// 用独立的 context：此时 ctx 已取消，拿它发 SQL 会立刻失败，
			// 于是"优雅释放"变成一句空话。
			rc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = l.m.Release(rc, l.name)
			cancel()
			if l.isLeader.Swap(false) && l.OnChange != nil {
				l.OnChange(false, Lease{Name: l.name})
			}
			return
		case <-t.C:
			l.tick(ctx)
		}
	}
}

func (l *Leader) tick(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	lease, held, err := l.m.Acquire(c, l.name, l.ttl)
	if err != nil {
		// 抢占失败（多半是库不可达）**不能立刻放弃 leader 身份**：
		// 租约还没到期，此刻退位反而制造了一段没人干活的空窗，
		// 而别的副本也连不上库，同样抢不到。
		// 等到期自然失效即可 —— 那时 Held 会返回 false。
		if l.isLeader.Load() && !lease.Held(time.Now()) {
			l.setLeader(false, lease)
		}
		return
	}
	l.fence.Store(lease.Fence)
	l.setLeader(held, lease)
}

func (l *Leader) setLeader(v bool, lease Lease) {
	if l.isLeader.Swap(v) != v && l.OnChange != nil {
		l.OnChange(v, lease)
	}
}
