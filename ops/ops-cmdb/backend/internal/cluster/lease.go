// Package cluster 提供多副本运行所需的协调原语。
//
// # 为什么需要它
//
// 单副本时，「谁来跑定时任务」不是问题 —— 只有一个进程。
// 一旦扩到多副本，同一个 cron 表达式会在**每个副本上各触发一次**：
//
//   - 主机同步：3 副本 = 3 倍云 API 调用，撞限流，还会并发写同一批行
//   - 证书续期：同一张证书被申请 3 次，ACME 侧会限速甚至封禁
//   - 域名续费：**非幂等写，等于扣 3 次钱**
//
// 最后一条是这个包存在的直接原因。
package cluster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"ops-cmdb-backend/internal/store"
)

// Lease 一段有期限的独占权。
//
// # ⚠️ 租约不是互斥锁
//
// 这一点必须说清楚，否则会被当成万无一失的锁来用。
//
// 持有者在租约到期**之前**可能已经失去了实际的独占权：
//
//   - 进程被 GC 或 CPU 抢占停顿了几秒，醒来时租约早已过期并被别人接走，
//     但它自己不知道，仍在写数据
//   - 各副本的系统时钟有偏差
//   - 网络分区：它连不上库，但仍在跑
//
// 也就是说：**租约能大幅降低重复执行的概率，但不能消除它。**
//
// 所以凡是非幂等的操作（尤其是花钱的），除了拿租约，还必须：
//
//   - 自己在数据层面做幂等（先查状态再动手，且查和写在同一个事务里），或者
//   - 用 Fence 做栅栏令牌：写的时候带上它，数据层拒绝比已知值更小的令牌
//
// 只靠租约就去扣费，是把「概率很小」当成了「不会发生」。
type Lease struct {
	Name      string
	Owner     string
	ExpiresAt time.Time
	// Fence 单调递增的栅栏令牌。每次租约易主都 +1。
	// 拿它去做条件写，就能挡住"以为自己还是 leader"的旧持有者。
	Fence int64
}

// Held 判断这份租约在给定时刻是否仍然有效。
func (l Lease) Held(now time.Time) bool {
	return !l.ExpiresAt.IsZero() && now.Before(l.ExpiresAt)
}

// Manager 租约管理器。
//
//	走 store.Platform 而不是裸 *sql.DB：leases 在平台表白名单里，
//	Platform 会校验这一点。等于多一道"这张表确实是平台级"的确认，
//	也让 internal/ 下"不许直接持有 *sql.DB"的规则不用为它开例外 ——
//	规则一旦开了第一个例外，后面就会有第二个。
type Manager struct {
	st    *store.Store
	owner string
	now   func() time.Time
}

// Options 构造参数。
type Options struct {
	// Owner 本副本的标识。用 Pod 名最合适（K8s 里 Pod 名天然唯一）。
	// 留空会退化成一个固定串，那样多个副本会互相当成自己 —— 必须传。
	Owner string
	// Now 注入时钟，只给测试用。
	Now func() time.Time
}

// New 构造管理器。owner 为空返回错误 —— 这种配置错误必须当场炸，
// 而不是等到两个副本互相认成自己、同一个任务跑两遍才发现。
func New(st *store.Store, opts Options) (*Manager, error) {
	if opts.Owner == "" {
		return nil, fmt.Errorf("cluster: Owner 不能为空（多副本下会互相认成自己）")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{st: st, owner: opts.Owner, now: now}, nil
}

// Owner 返回本副本标识。
func (m *Manager) Owner() string { return m.owner }

// Acquire 抢占或续约。拿到返回 (lease, true, nil)。
//
// 语义：
//   - 租约不存在 → 创建并持有
//   - 租约已过期 → 抢过来，Fence +1
//   - 租约是自己的 → 续期，Fence 不变（还是同一段持有期）
//   - 租约在别人手上且未过期 → 拿不到，返回 false（不是错误）
//
// 「拿不到」返回 false 而不是 error：那是正常状态，不是故障。
// 混在一起的话，日志里会满是"错误"，真正的故障反而被淹没。
func (m *Manager) Acquire(ctx context.Context, name string, ttl time.Duration) (Lease, bool, error) {
	now := m.now()
	exp := now.Add(ttl)

	// 一条语句完成"抢占或续约"，避免先查后写的竞态。
	//
	//	IF 的三个分支对应上面三种情况：
	//	  - 已过期或本来就是自己的 → 改成自己，续到新的到期时间
	//	  - 否则保持原样（rows affected 会是 0）
	//	fence 只在**易主**时 +1；自己续约不动它，否则每次心跳都会让
	//	之前发出去的栅栏令牌失效。
	res, err := m.st.Platform("cluster").Exec(ctx, `
		INSERT INTO leases (name, owner, expires_at, fence)
		VALUES (?, ?, ?, 1)
		ON DUPLICATE KEY UPDATE
			fence      = IF(owner <> VALUES(owner) AND expires_at <= VALUES(expires_at) - INTERVAL ? SECOND, fence + 1, fence),
			owner      = IF(expires_at <= ? OR owner = VALUES(owner), VALUES(owner), owner),
			expires_at = IF(owner = VALUES(owner), VALUES(expires_at), expires_at)`,
		name, m.owner, exp, int(ttl.Seconds()), now)
	if err != nil {
		return Lease{}, false, fmt.Errorf("cluster: 抢占租约 %s: %w", name, err)
	}
	_ = res

	// 回查确认到底归谁 —— ExecContext 的 rows-affected 在
	// ON DUPLICATE KEY UPDATE 下语义含糊（未变更时是 0，变更是 2），
	// 拿它判断归属会出错。多一次查询换一个明确的答案。
	return m.Get(ctx, name)
}

// Get 读当前租约状态，第二个返回值表示是否由本副本持有且未过期。
func (m *Manager) Get(ctx context.Context, name string) (Lease, bool, error) {
	var l Lease
	err := m.st.Platform("cluster").
		QueryRow(ctx, `SELECT name, owner, expires_at, fence FROM leases WHERE name = ?`, name).
		Scan(&l.Name, &l.Owner, &l.ExpiresAt, &l.Fence)
	if errors.Is(err, sql.ErrNoRows) {
		return Lease{Name: name}, false, nil
	}
	if err != nil {
		return Lease{}, false, fmt.Errorf("cluster: 读租约 %s: %w", name, err)
	}
	return l, l.Owner == m.owner && l.Held(m.now()), nil
}

// Release 主动释放。
//
// 优雅退出时调用，好处是接班的副本不用等租约自然过期 ——
// 否则每次滚动更新都会有一个 TTL 长的空窗，定时任务在那段时间没人跑。
//
// 只释放自己持有的：条件里带 owner，避免把别人刚抢到的租约释放掉。
func (m *Manager) Release(ctx context.Context, name string) error {
	_, err := m.st.Platform("cluster").Exec(ctx,
		`UPDATE leases SET expires_at = ? WHERE name = ? AND owner = ?`,
		m.now().Add(-time.Second), name, m.owner)
	if err != nil {
		return fmt.Errorf("cluster: 释放租约 %s: %w", name, err)
	}
	return nil
}
