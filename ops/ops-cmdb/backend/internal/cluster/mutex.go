package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"ops-cmdb-backend/internal/store"
)

// Mutex 跨副本的短期互斥，复用租约表。
//
// # 它解决什么
//
// 「同一个域名不要同时续两次费」这类去重，原本用进程内的 sync.Map 实现。
// 单副本时够用；多副本下双击的两个请求会落到不同 Pod，
// 各自看自己那份 map 都是空的，于是**都放行 —— 扣两次钱**。
//
// # ⚠️ 为什么不复用 Manager.Acquire
//
// Acquire 的语义里有一条「owner 相同 = 续约」—— 那是 leader 选举需要的：
// leader 必须能反复续自己的租约。
//
// 但互斥要的恰恰相反：**同一个 owner 也不能重入**。
// 一个 Pod 内的两次双击共用同一个 owner，若走 Acquire，
// 两边都会被判成"续自己的约"而双双放行 —— 比原来的 sync.Map 还差。
//
// 所以这里每次加锁生成一个一次性 token 作为持有者标识，
// 只有"不存在"或"已过期"两种情况能拿到锁。
//
// # 它不解决什么
//
//	见 Lease 的注释：租约不是真正的互斥锁。
//	持有者停顿到租约过期而不自知，仍可能与新持有者并发。
//
// 对花钱的操作，这一层是**减少**重复的第一道防线，不是唯一防线。
// 真正兜底的是业务侧的幂等校验（续费前回查厂商侧到期日，
// 确认这一年是不是已经续上了）。把两者分清很重要 ——
// 否则会误以为加了锁就可以省掉回查。
type Mutex struct {
	st *store.Store
}

// NewMutex 构造。
//
// 不需要 owner：互斥的持有者是**每次加锁生成的一次性 token**，
// 而不是副本身份。这也意味着它不受 POD_NAME 是否注入的影响。
func NewMutex(st *store.Store) *Mutex { return &Mutex{st: st} }

// ErrBusy 表示锁被别人持有。这是**预期内的状态**，不是故障 ——
// 调用方应转成 409，而不是 500。
var ErrBusy = errors.New("cluster: 该资源正在被其它操作占用")

// Lock 获取互斥，返回释放函数。拿不到返回 ErrBusy。
//
// ttl 要略大于「这段临界区最长可能跑多久」：
// 太短会在操作还没做完时就把锁放掉；太长则进程崩了以后别人要白等。
// 续费走外部 API，30~60s 是合理量级。
func (mu *Mutex) Lock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	name := "mutex:" + key
	pf := mu.st.Platform("cluster")

	// 只有两种情况能拿到：行不存在，或已过期。
	// owner 相同**不**放行 —— 这正是与 Acquire 的关键区别。
	res, err := pf.Exec(ctx, `
		INSERT INTO leases (name, owner, expires_at, fence)
		VALUES (?, ?, DATE_ADD(NOW(6), INTERVAL ? MICROSECOND), 1)
		ON DUPLICATE KEY UPDATE
			owner      = IF(expires_at <= NOW(6), VALUES(owner), owner),
			expires_at = IF(expires_at <= NOW(6), VALUES(expires_at), expires_at)`,
		name, token, ttl.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("cluster: 加锁 %s: %w", key, err)
	}
	_ = res

	// 回查确认锁到底在不在自己手上。
	// 不用 rows-affected 判断：ON DUPLICATE KEY UPDATE 下它的语义
	// （未变更 0 / 更新 2 / 插入 1）区分不出"我抢到了"和"值恰好没变"。
	var owner string
	if err := pf.QueryRow(ctx, `SELECT owner FROM leases WHERE name = ?`, name).Scan(&owner); err != nil {
		return nil, fmt.Errorf("cluster: 确认锁 %s: %w", key, err)
	}
	if owner != token {
		return nil, ErrBusy
	}

	return func() {
		// 释放用独立 context：请求的 ctx 此时可能已结束，
		// 拿它发 SQL 会失败，锁就要一直留到自然过期。
		rc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// 条件带 owner=token：只释放自己这一次拿到的锁。
		// 否则超时后别人已经拿到锁，自己收尾时会把人家的锁放掉。
		_, _ = mu.st.Platform("cluster").Exec(rc,
			`UPDATE leases SET expires_at = DATE_SUB(NOW(6), INTERVAL 1 SECOND) WHERE name = ? AND owner = ?`,
			name, token)
	}, nil
}

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cluster: 生成锁 token: %w", err)
	}
	return "lock-" + hex.EncodeToString(b), nil
}
