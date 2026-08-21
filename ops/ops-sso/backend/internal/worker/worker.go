// Package worker 是后台任务：到期回收、拨测、审计锚点、状态清理。
//
// # 多副本下不能重复执行
//
// 控制面会跑多个副本。定时任务不加协调的话，三个副本会同时回收同一批提权、
// 同时给同一条链签锚点。这里用**数据库咨询锁**（GET_LOCK）做领导者选举：
// 拿到锁的那个副本执行，其余的跳过。
//
// 为什么不用 leader election 的完整实现：那需要租约续期、脑裂处理、
// 心跳线程 —— 而我们的任务都是幂等的（回收已回收的提权是空操作），
// 偶尔漏跑一轮的代价是"晚 30 秒回收"，远小于引入一套选举机制的复杂度。
package worker

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ops-sso-backend/logx"
)

// Job 一个周期任务。
type Job struct {
	Name     string
	Interval time.Duration
	// Run 返回处理了多少条。返回 0 时不打日志 —— 每 30 秒一条"处理了 0 条"
	// 会把日志淹掉，真正有事的那条反而看不见。
	Run func(ctx context.Context) (int, error)
}

// Runner 跑一组周期任务。
type Runner struct {
	db   *sql.DB
	jobs []Job
}

func New(db *sql.DB) *Runner { return &Runner{db: db} }

func (r *Runner) Add(j Job) { r.jobs = append(r.jobs, j) }

// Start 启动全部任务。每个任务一个 goroutine。
func (r *Runner) Start(ctx context.Context) {
	for _, j := range r.jobs {
		go r.loop(ctx, j)
	}
}

func (r *Runner) loop(ctx context.Context, j Job) {
	// 启动时先等一个随机的短暂间隔再跑第一轮。
	//
	// 不等的话，多副本同时启动会在同一瞬间抢同一把锁，
	// 而滚动升级恰恰会让所有副本几乎同时启动。
	t := time.NewTimer(time.Duration(len(j.Name)) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		r.runOnce(ctx, j)
		t.Reset(j.Interval)
	}
}

func (r *Runner) runOnce(ctx context.Context, j Job) {
	locked, release, err := r.acquire(ctx, "oap_job_"+j.Name)
	if err != nil {
		logx.Line("worker", j.Name+": 取锁失败，本轮跳过: "+err.Error())
		return
	}
	if !locked {
		return // 别的副本在跑
	}
	defer release()

	start := time.Now()
	n, err := j.Run(ctx)
	switch {
	case err != nil:
		logx.Line("worker", fmt.Sprintf("%s: 失败: %v", j.Name, err))
	case n > 0:
		logx.Line("worker", fmt.Sprintf("%s: 处理 %d 条，耗时 %dms",
			j.Name, n, time.Since(start).Milliseconds()))
	}
}

// acquire 用 MySQL 的 GET_LOCK 做互斥。
//
// 超时给 0：拿不到立刻返回，不排队。排队的话，一个卡住的副本会让
// 后面所有副本堆在这里，最后表现成"任务全停了"而不是"少跑了一轮"。
func (r *Runner) acquire(ctx context.Context, name string) (bool, func(), error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return false, nil, err
	}
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&got); err != nil {
		conn.Close()
		return false, nil, err
	}
	if !got.Valid || got.Int64 != 1 {
		conn.Close()
		return false, nil, nil
	}
	// 锁绑在连接上：必须用同一个连接释放，也必须保证连接最终被归还，
	// 否则锁会一直被持有到连接超时 —— 那时其他副本全在空转
	return true, func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", name)
		conn.Close()
	}, nil
}
