// Package engine 是检测引擎：按调度执行规则，把命中收敛成事件，再交给投递。
//
// # 三条设计约束
//
//  1. 引擎不认识具体数据源，只认 datasource.Adapter；不认识具体渠道，只认 notify.Sender。
//  2. 每一次判定都留下判定链（decision_traces），正向解释「凭什么触发」，
//     反向解释「我以为该响的怎么没响」。
//  3. 失败必须是显性的：查询失败要落到 rules.last_error 与 rule_runs.outcome，
//     而不是安静地跳过——安静跳过的表现是「7 天没有告警」，会被读成「太平」。
package engine

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	"ops-alert-backend/config"
	"ops-alert-backend/crypto"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
)

type Engine struct {
	st     *store.Store
	cfg    *config.Config
	cipher *crypto.Cipher

	// sem 是全局查询并发闸，所有规则共用。
	// 单条规则的并发再小，几十条规则同时到点也能把数据源打垮，
	// 而数据源被打垮的表现是「查询超时」——看起来像我们自己的 bug。
	sem chan struct{}

	holder string // 本副本标识，用于租约
	mu     sync.Mutex
	// running 防止同一条规则的上一轮还没跑完就被下一轮再次触发。
	// 慢查询 + 短周期时这种重入会指数级堆积，最终把数据源和自己一起拖垮。
	running map[int64]bool
}

func New(st *store.Store, cfg *config.Config, cipher *crypto.Cipher) *Engine {
	host, _ := os.Hostname()
	return &Engine{
		st:      st,
		cfg:     cfg,
		cipher:  cipher,
		sem:     make(chan struct{}, cfg.QueryConcurrency),
		holder:  fmt.Sprintf("%s-%d", host, os.Getpid()),
		running: map[int64]bool{},
	}
}

// Start 启动调度循环，直到 ctx 取消。
//
// 多副本下只有持有租约的副本执行检测，其余空转等待。
// 用租约（有过期时间）而不是永久锁：持有者被 kill 掉后不会永久卡死，
// 下一个副本会在租约过期后接管，最坏损失一个租约周期。
func (e *Engine) Start(ctx context.Context) {
	const tick = 10 * time.Second
	t := time.NewTicker(tick)
	defer t.Stop()

	logx.Line("engine", "检测引擎已启动，等待租约")
	for {
		select {
		case <-ctx.Done():
			logx.Line("engine", "检测引擎停止")
			return
		case <-t.C:
			ok, err := e.acquireLease(ctx, 30*time.Second)
			if err != nil {
				// 租约拿不到不是致命错误（可能是另一个副本持有），
				// 但拿租约时报错必须记：数据库不可用时这是最早的信号。
				logx.J("engine", "lease_error", map[string]any{"error": err.Error()})
				continue
			}
			if !ok {
				continue
			}
			e.runDue(ctx)
		}
	}
}

// acquireLease 抢占/续租。同一个 holder 续租总是成功。
func (e *Engine) acquireLease(ctx context.Context, ttl time.Duration) (bool, error) {
	p := e.st.Platform("engine_lease")
	// 单条 UPSERT 完成「抢占或续租」：
	// 只有租约已过期、或持有者就是自己时才更新。
	// 分成 SELECT + UPDATE 两步会有竞态窗口，两个副本可能同时认为自己拿到了。
	res, err := p.Exec(ctx, `INSERT INTO leases (name, holder, expires_at)
		VALUES (?, ?, DATE_ADD(NOW(3), INTERVAL ? SECOND))
		ON DUPLICATE KEY UPDATE
			holder = IF(expires_at < NOW(3) OR holder = VALUES(holder), VALUES(holder), holder),
			expires_at = IF(holder = VALUES(holder), VALUES(expires_at), expires_at)`,
		e.cfg.LeaseName, e.holder, int(ttl.Seconds()))
	if err != nil {
		return false, err
	}
	_ = res
	var holder string
	if err := p.QueryRow(ctx, `SELECT holder FROM leases WHERE name = ?`, e.cfg.LeaseName).Scan(&holder); err != nil {
		return false, err
	}
	return holder == e.holder, nil
}

// runDue 遍历所有租户，执行到期的规则。
//
// 逐租户执行而不是造一个「能看所有租户」的上下文：后者只要有一处漏掉
// tenant 条件就会静默跨租户，而逐租户的写法让越权在 store 层就被拦住。
func (e *Engine) runDue(ctx context.Context) {
	failed, err := store.ForEachTenant(ctx, e.st, "detect", func(sc *store.Scoped, tenant store.TenantID) error {
		return e.runTenant(ctx, sc, tenant)
	})
	if err != nil {
		logx.J("engine", "foreach_tenant_error", map[string]any{"error": err.Error()})
	}
	if failed > 0 {
		logx.J("engine", "tenant_failures", map[string]any{"failed": failed})
	}
}

type dueRule struct {
	ID          int64
	Name        string
	Kind        string
	DSID        int64
	Spec        []byte
	Interval    int
	Lookback    int
	Threshold   int
	ForPeriods  int
	GroupBy     []byte
	Severity    string
	Labels      []byte
	Resolve     int
	NotifyResol bool
	NoDataAlert bool
	RouteID     sql.NullInt64
	MaxEvents   int
}

func (e *Engine) runTenant(ctx context.Context, sc *store.Scoped, tenant store.TenantID) error {
	// 到期判定放在 SQL 里：last_run_at 为空（从没跑过）或已过一个周期。
	// 放在 Go 里筛需要把全部规则拉回来，规则多了就是无谓的往返。
	rows, err := sc.Query(`SELECT id, name, kind, datasource_id, spec, interval_sec, lookback_sec,
			threshold, for_periods, group_by, severity, labels, resolve_after,
			notify_resolved, nodata_is_alert, route_id, max_events
		FROM rules
		WHERE tenant_id = ? AND enabled = 1 AND deleted_at IS NULL
		  AND (last_run_at IS NULL OR last_run_at <= DATE_SUB(NOW(3), INTERVAL interval_sec SECOND))
		ORDER BY id`)
	if err != nil {
		return fmt.Errorf("查询到期规则: %w", err)
	}
	defer rows.Close()

	var due []dueRule
	for rows.Next() {
		var r dueRule
		var notifyResolved, nodata int
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.DSID, &r.Spec, &r.Interval, &r.Lookback,
			&r.Threshold, &r.ForPeriods, &r.GroupBy, &r.Severity, &r.Labels, &r.Resolve,
			&notifyResolved, &nodata, &r.RouteID, &r.MaxEvents); err != nil {
			return fmt.Errorf("扫描规则: %w", err)
		}
		r.NotifyResol = notifyResolved == 1
		r.NoDataAlert = nodata == 1
		due = append(due, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	for _, r := range due {
		if !e.markRunning(r.ID) {
			// 上一轮还在跑。这不是错误，但要能看见：
			// 持续重入意味着规则周期比查询耗时还短，需要调周期或缩窗口。
			logx.J("engine", "rule_still_running", map[string]any{"rule_id": r.ID, "name": r.Name})
			continue
		}
		wg.Add(1)
		go func(r dueRule) {
			defer wg.Done()
			defer e.clearRunning(r.ID)
			select {
			case e.sem <- struct{}{}:
				defer func() { <-e.sem }()
			case <-ctx.Done():
				return
			}
			e.executeRule(ctx, tenant, r)
		}(r)
	}
	wg.Wait()
	return nil
}

func (e *Engine) markRunning(id int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[id] {
		return false
	}
	e.running[id] = true
	return true
}

func (e *Engine) clearRunning(id int64) {
	e.mu.Lock()
	delete(e.running, id)
	e.mu.Unlock()
}
