package cluster

import (
	"context"
	"database/sql"
	"time"
)

// Quota 跨副本的外部 API 调用配额，按分钟固定窗口。
//
// # 它解决什么
//
// 客户端限流器的职责是**不越过外部厂商的硬限制**（GoDaddy 写接口 60/分钟，
// 我们设 50 留缓冲）。这个算术只在"全平台一份计数器"的前提下成立。
//
// 原来的实现是 `var limiters sync.Map`——进程内。2 副本各算各的 = 100/分钟，
// 保护直接失效。撞上厂商限流之后的现象是批量续费里
// **"莫名其妙有几个没续上"**，而不是一句清楚的报错。
//
// # 与 Mutex 的区别
//
// Mutex 防的是"同一件事被做两次"；Quota 防的是"同类事在单位时间里做太多次"。
// 两者都需要，互不替代：续费既要防重复扣费（Mutex），也要防打爆厂商配额（Quota）。
type Quota struct {
	db *sql.DB
}

func NewQuota(db *sql.DB) *Quota { return &Quota{db: db} }

// Take 申请一次配额。放行返回 (used, true, nil)；超限返回 (used, false, nil)。
//
// ⚠️ 数据库出错时**放行**并把 error 一并返回，由调用方决定要不要记日志。
//
//	这与 Mutex 的选择相反，是刻意的：
//	Mutex 守的是扣钱，"锁挂了就当没锁"会造成重复扣费，所以必须拒绝；
//	Quota 守的是厂商配额，拒绝的代价是**正常的续费做不了**，
//	而放行的代价只是可能撞一次厂商限流（对方会拒绝，我们看得到 429）。
//	两害相权，这里放行更合理——但必须让调用方知道保护失效了。
func (q *Quota) Take(ctx context.Context, scope string, limit int) (used int, ok bool, err error) {
	win := time.Now().Unix() / 60

	// 先自增再判断：并发下两个副本同时读到 49 然后各自 +1 的竞态，
	// 靠"自增在数据库里原子完成"消除。多算的那次会被下一句判出来。
	if _, err = q.db.ExecContext(ctx, `
		INSERT INTO api_quota_windows (scope, window_min, used) VALUES (?, ?, 1)
		ON DUPLICATE KEY UPDATE used = used + 1`, scope, win); err != nil {
		return 0, true, err
	}
	if err = q.db.QueryRowContext(ctx,
		`SELECT used FROM api_quota_windows WHERE scope=? AND window_min=?`,
		scope, win).Scan(&used); err != nil {
		return 0, true, err
	}
	// 顺手清理老窗口。留 5 分钟是为了排障时还能看到刚才那几分钟打了多少次
	_, _ = q.db.ExecContext(ctx,
		`DELETE FROM api_quota_windows WHERE window_min < ?`, win-5)

	return used, used <= limit, nil
}

// Used 当前窗口已用多少（只读，不占配额）。用于界面展示。
func (q *Quota) Used(ctx context.Context, scope string) int {
	var used int
	_ = q.db.QueryRowContext(ctx,
		`SELECT used FROM api_quota_windows WHERE scope=? AND window_min=?`,
		scope, time.Now().Unix()/60).Scan(&used)
	return used
}
