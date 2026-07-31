package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/dnsource"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/notify"
)

// Scheduler 用 cron 调度可配置定时任务（scheduled_tasks 表）。
// 任务可在前端开关 / 改频率 / 立即运行；改配置后热重载。
// TaskFailure 单条失败明细（target=失败对象，可用于重试；reason=具体原因）。
type TaskFailure struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// ProgressFn 进度回调：done=已处理，total=总数。核心函数边跑边上报，前端实时看进度。
type ProgressFn func(done, total int)

// taskFn 核心函数签名：ctx 用于超时/取消（核心函数在循环里 select ctx.Done() 及时中止）；
// prog 上报进度；targets 非空=只处理这些对象（重试用），nil=全量。
type taskFn func(ctx context.Context, prog ProgressFn, targets []string) (string, []TaskFailure, bool)

type Scheduler struct {
	db      *sql.DB
	cipher  *crypto.Cipher
	mu      sync.Mutex
	cron    *cron.Cron
	funcs   map[string]taskFn
	running map[int64]context.CancelFunc // runID -> cancel（供「取消执行」中止运行中的任务）
	runMu   sync.Mutex
}

// taskTimeout 各任务硬超时（超过则中止并标「超时」，防止永久卡"运行中"）。
func taskTimeout(key string) time.Duration {
	switch key {
	case "inspect", "refresh_expiry", "dns_sync", "host_sync":
		return 25 * time.Minute // 逐个连 443/WHOIS/同步，量大给足
	case "gke_upgrade_sync":
		// 每集群 3 次 API + 每节点池 1 次 fetchNodePoolUpgradeInfo，4 集群约 10 个池，给足余量
		return 15 * time.Minute
	default:
		return 5 * time.Minute
	}
}

var sched *Scheduler // 全局单例，供 API 热重载 / 立即运行

// StartScheduler 初始化调度器并按 scheduled_tasks 注册 cron。非阻塞（cron 在后台 goroutine）。
func StartScheduler(db *sql.DB, cipher *crypto.Cipher, pool *k8ssource.Pool) {
	nodeHealthPool = pool // 节点健康任务要直连集群，见 node_health.go
	sched = &Scheduler{db: db, cipher: cipher, running: map[int64]context.CancelFunc{}}
	sched.funcs = map[string]taskFn{
		"refresh_expiry": func(ctx context.Context, p ProgressFn, t []string) (string, []TaskFailure, bool) {
			return refreshAllWhoisCore(ctx, db, p, t)
		},
		"auto_renew": func(ctx context.Context, _ ProgressFn, _ []string) (string, []TaskFailure, bool) {
			return renewDue(db, cipher)
		},
		"remind": func(context.Context, ProgressFn, []string) (string, []TaskFailure, bool) {
			return remindExpiry(db), nil, true
		},
		"inspect": func(ctx context.Context, p ProgressFn, t []string) (string, []TaskFailure, bool) {
			return inspectAllCertsCore(ctx, db, p, t)
		},
		"dns_sync": func(ctx context.Context, _ ProgressFn, _ []string) (string, []TaskFailure, bool) {
			return dnsSyncCore(ctx, db, cipher)
		},
		"host_sync": func(context.Context, ProgressFn, []string) (string, []TaskFailure, bool) {
			return SyncAllHostProjects(db, cipher)
		},
		// GKE 版本与升级：排期表同步不依赖云凭据，集群采集依赖 SA key，故拆成两个任务
		"gke_schedule_sync": func(ctx context.Context, _ ProgressFn, _ []string) (string, []TaskFailure, bool) {
			return gkeScheduleSyncCore(ctx, db)
		},
		"gke_upgrade_sync": func(ctx context.Context, p ProgressFn, t []string) (string, []TaskFailure, bool) {
			return gkeUpgradeSyncCore(ctx, db, cipher, p, t)
		},
		"gke_upgrade_remind": func(context.Context, ProgressFn, []string) (string, []TaskFailure, bool) {
			return gkeUpgradeRemindCore(db)
		},
		// 节点健康是分钟级任务，自己直连集群（k8s_nodes 表 120s 才刷一次，撑不起 3 分钟判定）
		"node_health_watch": func(ctx context.Context, p ProgressFn, _ []string) (string, []TaskFailure, bool) {
			return nodeHealthWatchCore(ctx, db, nodeHealthPool, cipher, p)
		},
	}
	// 自愈①：启动时，之前进程遗留的「运行中」记录一律标「中断」（那些进程已随重启死掉）
	if _, err := db.Exec(`UPDATE task_run_logs SET status='interrupted', summary='中断：服务重启', finished_at=NOW() WHERE status='running'`); err != nil {
		logx.Line("scheduler", fmt.Sprintf("[scheduler] 启动清理遗留 running 记录失败: %v", err))
	}
	// 自愈②：每 5 分钟把 running 超过硬超时上限(30min)仍未收尾的标「中断」（兜底任何卡死）
	go func() {
		t := time.NewTicker(5 * time.Minute)
		for range t.C {
			if _, err := db.Exec(`UPDATE task_run_logs SET status='interrupted', summary='中断：超时未收尾(自愈)', finished_at=NOW()
				WHERE status='running' AND TIMESTAMPDIFF(SECOND, started_at, NOW()) > 1800`); err != nil {
				logx.Line("scheduler", fmt.Sprintf("[scheduler] 定期自愈卡死记录失败: %v", err))
			}
		}
	}()
	sched.reload()
}

// ReloadScheduler 改了 scheduled_tasks 配置后重建 cron（供 API 调用）。
func ReloadScheduler() {
	if sched != nil {
		sched.reload()
	}
}

// RunTaskNow 立即异步全量跑一次指定任务（供 API「立即运行」调用）。
func RunTaskNow(key string) bool {
	if sched == nil || sched.funcs[key] == nil {
		return false
	}
	go sched.run(key, "manual", nil)
	return true
}

// RunTaskRetry 只重试指定失败对象（供 API「重试失败项」调用），生成一条 trigger=retry 的新记录。
func RunTaskRetry(key string, targets []string) bool {
	if sched == nil || sched.funcs[key] == nil {
		return false
	}
	go sched.run(key, "retry", targets)
	return true
}

func (s *Scheduler) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cron != nil {
		s.cron.Stop()
	}
	s.cron = cron.New()
	rows, err := s.db.Query(`SELECT task_key, schedule FROM scheduled_tasks WHERE enabled=1`)
	if err != nil {
		logx.Line("scheduler", fmt.Sprintf("scheduler reload query: %v", err))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, schedule string
		if rows.Scan(&key, &schedule) != nil {
			continue
		}
		if s.funcs[key] == nil {
			continue
		}
		k := key
		if _, err := s.cron.AddFunc(schedule, func() { s.run(k, "cron", nil) }); err != nil {
			logx.Line("scheduler", fmt.Sprintf("scheduler add %s (%q): %v", key, schedule, err))
		}
	}
	s.cron.Start()
}

// exec 执行 UPDATE/DELETE 并在出错时打日志（不再吞错）。desc 用于日志定位。
func (s *Scheduler) exec(desc, query string, args ...any) {
	if _, err := s.db.Exec(query, args...); err != nil {
		logx.Line("scheduler", fmt.Sprintf("[scheduler] %s 失败: %v", desc, err))
	}
}

func (s *Scheduler) run(key, trigger string, targets []string) {
	fn := s.funcs[key]
	if fn == nil {
		return
	}
	start := time.Now()
	runID := s.startRunLog(key, trigger, start) // 先写「运行中」记录，前端可实时看进度/耗时
	// 硬超时 + 可取消：注册 cancel 供「取消执行」中止；超时/取消都会让核心函数在循环里退出
	ctx, cancel := context.WithTimeout(context.Background(), taskTimeout(key))
	if runID > 0 {
		s.runMu.Lock()
		s.running[runID] = cancel
		s.runMu.Unlock()
	}
	defer func() {
		cancel()
		if runID > 0 {
			s.runMu.Lock()
			delete(s.running, runID)
			s.runMu.Unlock()
		}
		if r := recover(); r != nil {
			logx.Line("scheduler", fmt.Sprintf("[scheduler] 任务 %s panic: %v", key, r))
			msg := fmt.Sprintf("panic: %v", r)
			s.exec("panic后更新scheduled_tasks", `UPDATE scheduled_tasks SET last_run_at=NOW(), last_result=?, last_ok=0 WHERE task_key=?`, truncate(msg, 250), key)
			ns, ng, na := sendTaskNotify(s.db, key, "fail", msg)
			s.finishRunLog(runID, "fail", msg, nil, start, ns, ng, na)
		}
	}()
	logx.Line("scheduler", fmt.Sprintf("[scheduler] 运行任务 %s (%s)", key, trigger))
	prog := func(done, total int) {
		s.exec("更新进度", `UPDATE task_run_logs SET progress=? WHERE id=?`, fmt.Sprintf("%d/%d", done, total), runID)
	}
	result, failures, ok := fn(ctx, prog, targets)

	// 状态判定：ctx 取消 → 已取消 / 超时；否则 ok/partial/fail
	status := "ok"
	switch {
	case ctx.Err() == context.Canceled:
		status, ok = "cancelled", false
		if result == "" {
			result = "已手动取消"
		}
	case ctx.Err() == context.DeadlineExceeded:
		status, ok = "timeout", false
		result = fmt.Sprintf("超时中止（上限 %s）；%s", taskTimeout(key), result)
	case !ok:
		status = "fail"
	case len(failures) > 0:
		status = "partial"
	}
	okv := 0
	if ok {
		okv = 1
	}
	s.exec("更新scheduled_tasks", `UPDATE scheduled_tasks SET last_run_at=NOW(), last_result=?, last_ok=? WHERE task_key=?`, truncate(result, 250), okv, key)
	ns, ng, na := sendTaskNotify(s.db, key, status, result)
	s.finishRunLog(runID, status, result, failures, start, ns, ng, na)

	// D 自动重试：仅 fail/timeout（不含 partial/cancelled/ok）且非自动重试触发时，延迟后重跑一次
	if (status == "fail" || status == "timeout") && trigger != "auto_retry" {
		logx.Line("scheduler", fmt.Sprintf("[scheduler] 任务 %s %s，10s 后自动重试一次", key, status))
		go func() { time.Sleep(10 * time.Second); s.run(key, "auto_retry", targets) }()
	}
}

// CancelTask 取消运行中的任务：有活 goroutine 就 cancel 其 ctx（会正常收尾为「已取消」）；
// 若已是僵尸（goroutine 没了）则直接强制把记录标「已取消」。返回是否处理。
func (s *Scheduler) CancelTask(runID int64) bool {
	s.runMu.Lock()
	cancel, alive := s.running[runID]
	s.runMu.Unlock()
	if alive {
		cancel()
		return true
	}
	// 僵尸：直接收尾
	res, err := s.db.Exec(`UPDATE task_run_logs SET status='cancelled', summary='已手动取消(强制收尾)', finished_at=NOW() WHERE id=? AND status='running'`, runID)
	if err != nil {
		logx.Line("scheduler", fmt.Sprintf("[scheduler] 强制取消记录 %d 失败: %v", runID, err))
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// startRunLog 任务开跑先插一条 running 记录，返回 id。
func (s *Scheduler) startRunLog(key, trigger string, start time.Time) int64 {
	var name string
	_ = s.db.QueryRow(`SELECT name FROM scheduled_tasks WHERE task_key=?`, key).Scan(&name)
	res, err := s.db.Exec(`INSERT INTO task_run_logs (task_key, name, status, trigger_by, started_at, finished_at)
		VALUES (?,?, 'running', ?, ?, ?)`, key, name, trigger, start, start)
	if err != nil {
		logx.Line("scheduler", fmt.Sprintf("[scheduler] 写运行记录失败(task=%s): %v", key, err))
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

// finishRunLog 任务跑完把 running 记录更新为终态（含失败明细/通知投递/耗时）。
func (s *Scheduler) finishRunLog(runID int64, status, summary string, failures []TaskFailure, start time.Time, notifyState, notifyGroup, notifyAt string) {
	var failJSON any
	if len(failures) > 0 {
		if b, err := json.Marshal(failures); err == nil {
			failJSON = string(b)
		}
	}
	dur := int(time.Since(start).Milliseconds())
	s.exec("收尾更新运行记录", `UPDATE task_run_logs SET status=?, summary=?, failures=?, duration_ms=?, notify_state=?, notify_group=?, notify_at=?, progress='', finished_at=NOW() WHERE id=?`,
		status, truncate(summary, 250), failJSON, dur, notifyState, notifyGroup, notifyAt, runID)
	// 保留策略：只留 90 天历史
	s.exec("清理90天前记录", `DELETE FROM task_run_logs WHERE finished_at < DATE_SUB(NOW(), INTERVAL 90 DAY)`)
	s.purgeHistory()
}

// purgeHistory 回收只增不删的历史表。
//
// task_run_logs 早就有 90 天保留，但 k8s_changes 和 cert_history 一直是纯追加、
// 没有任何回收——增速不快（实测 k8s_changes 约 78 条/天），短期不致命，
// 但和 CMDB-012 是同一类问题：没人给它设上界，就总有一天会撑满盘。
// 保留期按用途给：工作负载变更主要用于近期排障，证书历史要覆盖一个签发周期（一年）。
func (s *Scheduler) purgeHistory() {
	s.exec("清理180天前工作负载变更",
		`DELETE FROM k8s_changes WHERE changed_at < DATE_SUB(NOW(), INTERVAL 180 DAY)`)
	s.exec("清理365天前证书历史",
		`DELETE FROM cert_history WHERE at < DATE_SUB(NOW(), INTERVAL 365 DAY)`)
}

// ---- 任务核心函数 ----

// refreshAllWhoisCore 刷新域名注册到期。
// 关键优化：**数据源(origin=sync)域名跳过**——它们的到期日由 DNS 同步维护（GoDaddy API 权威值）；
// 只对 origin=manual 或到期日为空的域名走 RDAP→WHOIS。查询链路 domainExpiry，失败自动退避重试≤3 次。
// targets 非空=只刷这些域名（重试用）。prog 上报进度。
func refreshAllWhoisCore(ctx context.Context, db *sql.DB, prog ProgressFn, targets []string) (string, []TaskFailure, bool) {
	// 只查数据源覆盖不到的：手动录入，或(不知何故)没有到期日的
	q := `SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.stale=0 AND (d.origin='manual' OR d.expiry_at IS NULL)`
	rows, err := db.Query(q)
	if err != nil {
		return "查询域名失败: " + err.Error(), nil, false
	}
	type item struct {
		id   int64
		name string
	}
	var items []item
	tgSet := map[string]bool{}
	for _, t := range targets {
		tgSet[t] = true
	}
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.name) == nil {
			if len(targets) == 0 || tgSet[it.name] {
				items = append(items, it)
			}
		}
	}
	rows.Close()

	total := len(items)
	if total == 0 {
		var syncN int
		_ = db.QueryRow(`SELECT COUNT(*) FROM cis c JOIN domains d ON d.ci_id=c.id
			WHERE c.type='domain' AND d.stale=0 AND d.origin='sync'`).Scan(&syncN)
		return fmt.Sprintf("0 个手动域名需 WHOIS；数据源域名（%d 个）到期日已由「DNS 记录同步」用厂商权威到期日更新", syncN), nil, true
	}
	var mu sync.Mutex
	var failures []TaskFailure
	var done int32
	n := int32(0)
	sem := make(chan struct{}, 6) // 并发 6，防慢查询拖垮整体
	var wg sync.WaitGroup
	for _, it := range items {
		if ctx.Err() != nil { // 超时/取消：停止派发剩余
			break
		}
		wg.Add(1)
		go func(it item) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			t, reason := expiryWithRetry(it.name, 3) // 自动退避重试最多 3 次
			if t != nil {
				_, _ = db.Exec(`UPDATE domains SET expiry_at=? WHERE ci_id=?`, *t, it.id) // 成功才更新；失败保留旧值
				atomic.AddInt32(&n, 1)
			} else {
				mu.Lock()
				failures = append(failures, TaskFailure{Target: it.name, Reason: reason})
				mu.Unlock()
			}
			if prog != nil {
				prog(int(atomic.AddInt32(&done, 1)), total)
			}
		}(it)
	}
	wg.Wait()
	return fmt.Sprintf("已刷新 %d/%d 个域名的注册到期（数据源域名已跳过）", n, total), failures, true
}

// expiryWithRetry 查到期日，失败自动退避重试最多 maxRetry 次（2s→4s→8s）。
func expiryWithRetry(domain string, maxRetry int) (*time.Time, string) {
	backoff := 2 * time.Second
	var reason string
	for attempt := 0; attempt <= maxRetry; attempt++ {
		var t *time.Time
		t, reason = domainExpiry(domain)
		if t != nil {
			return t, ""
		}
		if attempt < maxRetry {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return nil, reason
}

// inspectAllCertsCore 连 443 检测证书到期：主域名(domains) + 所有业务域名解析(domain_records)。
// targets 非空=只检测这些 fqdn（重试用）。prog 上报进度。
func inspectAllCertsCore(ctx context.Context, db *sql.DB, prog ProgressFn, targetList []string) (string, []TaskFailure, bool) {
	type target struct {
		id     int64
		fqdn   string
		isMain bool // true=写 domains 表，false=写 domain_records 表
	}
	var targets []target
	drows, err := db.Query(`SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain' AND d.stale=0 AND d.ignored=0`)
	if err != nil {
		return "查询域名失败: " + err.Error(), nil, false
	}
	for drows.Next() {
		var id int64
		var name string
		if drows.Scan(&id, &name) == nil {
			targets = append(targets, target{id, name, true})
		}
	}
	drows.Close()
	rrows, err := db.Query(`SELECT r.id, r.host, c.name FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id JOIN domains dd ON dd.ci_id=r.domain_ci_id WHERE c.type='domain' AND r.ignored=0 AND dd.ignored=0`)
	if err == nil {
		for rrows.Next() {
			var id int64
			var host, domain string
			if rrows.Scan(&id, &host, &domain) == nil {
				targets = append(targets, target{id, recordFQDN(host, domain), false})
			}
		}
		rrows.Close()
	}
	// 重试：只保留指定 fqdn
	if len(targetList) > 0 {
		want := map[string]bool{}
		for _, t := range targetList {
			want[t] = true
		}
		var filtered []target
		for _, t := range targets {
			if want[t.fqdn] {
				filtered = append(filtered, t)
			}
		}
		targets = filtered
	}
	total := len(targets)

	var ok, fail, done int32
	var fmu sync.Mutex
	var failures []TaskFailure
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, t := range targets {
		if ctx.Err() != nil { // 超时/取消：停止派发剩余
			break
		}
		wg.Add(1)
		go func(tg target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			if ct, cmsg := tlsCertExpiry(tg.fqdn); ct != nil {
				if tg.isMain {
					_, _ = db.Exec(`UPDATE domains SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg=? WHERE ci_id=?`, *ct, truncate(cmsg, 250), tg.id)
				} else {
					_, _ = db.Exec(`UPDATE domain_records SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, *ct, truncate(cmsg, 250), tg.id)
				}
				atomic.AddInt32(&ok, 1)
			} else {
				if tg.isMain {
					_, _ = db.Exec(`UPDATE domains SET cert_check_at=NOW(), cert_check_msg=? WHERE ci_id=?`, truncate(cmsg, 250), tg.id)
				} else {
					_, _ = db.Exec(`UPDATE domain_records SET cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, truncate(cmsg, 250), tg.id)
				}
				atomic.AddInt32(&fail, 1)
				fmu.Lock()
				failures = append(failures, TaskFailure{Target: tg.fqdn, Reason: truncate(cmsg, 120)})
				fmu.Unlock()
			}
			if prog != nil {
				prog(int(atomic.AddInt32(&done, 1)), total)
			}
		}(t)
	}
	wg.Wait()
	msg := fmt.Sprintf("检测 %d 张证书（主域名+业务域名），成功 %d / 失败 %d", total, ok, fail)
	if len(failures) > 0 {
		const topN = 8
		msg += "\n失败 TOP（完整见执行记录）："
		for i, f := range failures {
			if i >= topN {
				break
			}
			msg += fmt.Sprintf("\n· %s — %s", f.Target, f.Reason)
		}
		if len(failures) > topN {
			msg += fmt.Sprintf("\n…另 %d 条，详见「执行记录」", len(failures)-topN)
		}
		msg += "\n提示：常年失败多为内网/无需证书的解析，可在到期巡检标「无需证书」不再计入"
	}
	return msg, failures, true
}

// dnsSyncCore 定时全量同步所有数据源的 DNS 记录（复用 SyncHandler 的方法）。
func dnsSyncCore(parent context.Context, db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	sh := NewSyncHandler(db, cipher)
	rows, err := db.Query(`SELECT id, name FROM registrars`)
	if err != nil {
		return "查询数据源失败: " + err.Error(), nil, false
	}
	type src struct {
		id   int
		name string
	}
	var srcs []src
	for rows.Next() {
		var s src
		if rows.Scan(&s.id, &s.name) == nil {
			srcs = append(srcs, s)
		}
	}
	rows.Close()
	if len(srcs) == 0 {
		return "没有配置数据源，跳过", nil, true
	}
	totalD, totalR, totalImp, migratedCnt := 0, 0, 0, 0
	var newRecList []string // 本次新增的业务解析（供摘要列出）
	var failures []TaskFailure
	for _, s := range srcs {
		if parent.Err() != nil { // 超时/取消：停止后续数据源
			break
		}
		id := s.id
		provider, cred, err := LoadCredential(db, cipher, id)
		if err != nil {
			failures = append(failures, TaskFailure{Target: s.name, Reason: "读取凭据失败"})
			continue
		}
		adapter, err := dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
		if err != nil {
			failures = append(failures, TaskFailure{Target: s.name, Reason: "初始化适配器失败：" + truncate(err.Error(), 120)})
			continue
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
		domains, err := adapter.ListDomains(ctx)
		if err != nil {
			cancel()
			failures = append(failures, TaskFailure{Target: s.name, Reason: "列域名失败：" + truncate(err.Error(), 120)})
			continue
		}
		ignoredSet := sh.ignoredDomainSet(id) // 已忽略的域名定时同步也跳过
		present := map[string]bool{}
		for _, d := range domains {
			if ignoredSet[d.Name] {
				continue
			}
			if isDomainGone(d.Status) {
				sh.markDomainGone(d.Name, id, d.Status)
				logx.Line("scheduler", fmt.Sprintf("[domain-sync] 域名 %s 判为已移出账号（GoDaddy status=%s）", d.Name, d.Status))
				continue
			}
			if !isDomainActive(d.Status) && !isDomainPending(d.Status) {
				logx.Line("scheduler", fmt.Sprintf("[domain-sync] WARN 域名 %s 状态未识别（GoDaddy status=%s），暂按活跃处理", d.Name, d.Status))
			}
			ciID, err := sh.upsertDomainCI(d.Name, id, d.ExpiresAt, d.Status)
			if err != nil {
				continue
			}
			present[d.Name] = true
			totalD++
			recs, err := adapter.ListRecords(ctx, d.Name)
			if err != nil {
				// 区分 DNS 已迁走(Cloudflare 等，正常) 与真失败
				if mig, reason := classifyRecordFetchErr(d.Name, err); mig {
					migratedCnt++
					_, _ = db.Exec(`UPDATE domains SET dns_migrated=1 WHERE ci_id=?`, ciID)
				} else {
					failures = append(failures, TaskFailure{Target: d.Name, Reason: "拉解析失败：" + reason})
				}
			} else {
				sh.refreshDNSRecords(ciID, id, recs)
				totalR += len(recs)
				imp := sh.importBusinessRecords(ciID, d.Name, recs)
				totalImp += len(imp)
				newRecList = append(newRecList, imp...)
				migrated := 0
				if len(recs) == 0 && dnsMigratedFromGoDaddy(d.Name) {
					migrated = 1
					migratedCnt++
				}
				_, _ = db.Exec(`UPDATE domains SET dns_migrated=? WHERE ci_id=?`, migrated, ciID)
			}
			// 扫到就更新同步时刻（records 成败都更）
			_, _ = db.Exec(`UPDATE domains SET last_synced_at=NOW() WHERE ci_id=?`, ciID)
		}
		sh.markStaleDomains(id, present)
		cancel()
	}
	ok := !(totalD == 0 && len(failures) == len(srcs)) // 全部源都失败才算失败
	msg := fmt.Sprintf("同步 %d 域名 / %d DNS 记录 / 新增 %d 条解析", totalD, totalR, totalImp)
	if migratedCnt > 0 {
		msg += fmt.Sprintf(" / %d 个DNS已迁走(Cloudflare等,正常)", migratedCnt)
	}
	if len(newRecList) > 0 {
		const topN = 10
		msg += "\n新增业务解析："
		for i, r := range newRecList {
			if i >= topN {
				break
			}
			msg += "\n· " + r
		}
		if len(newRecList) > topN {
			msg += fmt.Sprintf("\n…另 %d 条，详见「执行记录」", len(newRecList)-topN)
		}
	}
	if len(failures) > 0 {
		msg += fmt.Sprintf("\n%d/%d 个数据源失败", len(failures), len(srcs))
	}
	return msg, failures, ok
}

// sendTaskNotify 任务跑完发 Lark 卡片到该任务配置的群，带 ✅/⚠️/❌ + 结果 + @人。
// status: ok / partial / fail。返回(投递状态, 群名, @人名单)供历史记录。
// 投递状态：sent=已送达 / failed=Lark报错 / skipped=按配置不发 / none=未配置群
func sendTaskNotify(db *sql.DB, taskKey, status, result string) (state, groupName, atNames string) {
	var name, notifyWhen string
	var notifyEnabled int
	var groupID sql.NullInt64
	if err := db.QueryRow(`SELECT name, notify_enabled, lark_group_id, notify_when FROM scheduled_tasks WHERE task_key=?`,
		taskKey).Scan(&name, &notifyEnabled, &groupID, &notifyWhen); err != nil {
		return "none", "", ""
	}
	if notifyEnabled == 0 {
		return "skipped", "", ""
	}
	ok := status == "ok" || status == "partial"
	if notifyWhen == "fail" && ok {
		return "skipped", "", ""
	}
	if !groupID.Valid {
		return "none", "", ""
	}
	var webhook string
	if db.QueryRow(`SELECT name, webhook FROM lark_groups WHERE id=?`, groupID.Int64).Scan(&groupName, &webhook) != nil || webhook == "" {
		return "none", "", ""
	}
	statusText := "✅ 执行成功"
	if status == "partial" {
		statusText = "⚠️ 部分成功"
	} else if status == "fail" {
		statusText = "❌ 执行失败"
	}
	atSeg, atNames := atMentionsForTask2(db, taskKey)
	text := fmt.Sprintf("【CMDB 定时任务】%s %s\n时间：%s\n结果：%s", statusText, name, time.Now().Format("2006-01-02 15:04"), result)
	text += atSeg
	if err := notify.SendFeishu(webhook, text); err != nil {
		return "failed", groupName, atNames
	}
	return "sent", groupName, atNames
}

// atMentionsForTask2 同 atMentionsForTask，额外返回 @人名字（用于历史记录展示）。
func atMentionsForTask2(db *sql.DB, taskKey string) (seg, names string) {
	rows, err := db.Query(`SELECT u.name, u.open_id FROM task_notify_users t JOIN notify_users u ON u.id=t.user_id
		WHERE t.task_key=? AND u.enabled=1 AND u.open_id<>''`, taskKey)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	var b strings.Builder
	var ns []string
	for rows.Next() {
		var nm, oid string
		if rows.Scan(&nm, &oid) == nil {
			b.WriteString(fmt.Sprintf(`<at user_id="%s"></at>`, oid))
			ns = append(ns, nm)
		}
	}
	if b.Len() == 0 {
		// 没配 @人 时告警只会静静躺在群里。半夜的节点崩溃预警没人 @ 就等于没发出去，
		// 所以这里必须留痕，别让「配置漏了」表现得和「本来就不用 @」一样。
		logx.J("scheduler", "no_at_mentions", map[string]any{
			"task": taskKey,
			"note": "该任务未配置通知人，告警将不 @ 任何人；去「系统管理 → 通知」添加并在定时任务里关联",
		})
		return "", ""
	}
	return "\n" + b.String(), strings.Join(ns, "、")
}

// atMentionsForTask 拼该任务指定的 @人（飞书 open_id）。
func atMentionsForTask(db *sql.DB, taskKey string) string {
	rows, err := db.Query(`SELECT u.open_id FROM task_notify_users t JOIN notify_users u ON u.id=t.user_id
		WHERE t.task_key=? AND u.enabled=1 AND u.open_id<>''`, taskKey)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var oid string
		if rows.Scan(&oid) == nil {
			b.WriteString(fmt.Sprintf(`<at user_id="%s"></at>`, oid))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n" + b.String()
}

func getSetting(db *sql.DB, key string) string {
	var v string
	_ = db.QueryRow(`SELECT v FROM settings WHERE k=?`, key).Scan(&v)
	return v
}

// renewDue 续期 auto_renew 且 expiry_at - now < renew_days 天的证书。
// 返回摘要（列出续了哪几张 + 新到期日 + 需手动部署提示）、失败明细、ok（任务是否正常跑）。
func renewDue(db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	rows, err := db.Query(`
		SELECT t.ci_id, COALESCE(r.dst_ci_id, 0), t.ca, t.cn
		FROM certificates t
		LEFT JOIN ci_relations r ON r.src_ci_id=t.ci_id AND r.rel_type='protects'
		WHERE t.auto_renew=1 AND t.status='active' AND t.expiry_at IS NOT NULL
		  AND t.expiry_at < DATE_ADD(NOW(), INTERVAL t.renew_days DAY)
		  AND t.challenge NOT IN ('manual-dns','http-01')`)
	if err != nil {
		return "查询待续期证书失败：" + truncate(err.Error(), 160), nil, false
	}
	type job struct {
		certCIID, domainCIID int64
		ca, cn               string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.certCIID, &j.domainCIID, &j.ca, &j.cn) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	if len(jobs) == 0 {
		return "本次扫描无到期前阈值内的证书，未触发续期", nil, true
	}

	webhook := taskWebhook(db, "auto_renew")
	var okNames []string
	var failures []TaskFailure
	for _, j := range jobs {
		var acctID int
		if err := db.QueryRow(`SELECT id FROM acme_accounts WHERE ca=? ORDER BY id LIMIT 1`, j.ca).Scan(&acctID); err != nil {
			failures = append(failures, TaskFailure{Target: j.cn, Reason: "无对应 ACME 账户（ca=" + j.ca + "）"})
			continue
		}
		logx.Line("scheduler", fmt.Sprintf("auto-renew cert %s (ci %d)", j.cn, j.certCIID))
		if errMsg := issueCertCore(db, cipher, j.certCIID, j.domainCIID, acctID, false, "renew"); errMsg != "" {
			failures = append(failures, TaskFailure{Target: j.cn, Reason: truncate(errMsg, 160)})
			notifyEvent(db, webhook, "auto_renew", "notify_renew_fail", fmt.Sprintf("❌ 证书自动续期失败：%s\n原因：%s", j.cn, errMsg))
		} else {
			var newExp string
			_ = db.QueryRow(`SELECT DATE_FORMAT(expiry_at,'%Y-%m-%d') FROM certificates WHERE ci_id=?`, j.certCIID).Scan(&newExp)
			okNames = append(okNames, fmt.Sprintf("%s（%s，新到期 %s）", j.cn, j.ca, newExp))
			notifyEvent(db, webhook, "auto_renew", "notify_renew_success",
				fmt.Sprintf("✅ 证书自动续期成功：%s（%s，新到期 %s）\n⚠️ 已重新签发，请手动更新/部署到目标（K8s Secret / 服务器）", j.cn, j.ca, newExp))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "续期成功 %d / 失败 %d（共 %d 张到期）", len(okNames), len(failures), len(jobs))
	if len(okNames) > 0 {
		b.WriteString("\n已重签（⚠️ 需手动更新/部署到目标）：")
		for _, n := range okNames {
			b.WriteString("\n✅ " + n)
		}
	}
	if len(failures) > 0 {
		b.WriteString("\n失败：")
		for _, f := range failures {
			fmt.Fprintf(&b, "\n❌ %s — %s", f.Target, f.Reason)
		}
	}
	return b.String(), failures, true
}

// taskWebhook 取某任务配置的 Lark 群 webhook。
func taskWebhook(db *sql.DB, taskKey string) string {
	var webhook string
	_ = db.QueryRow(`SELECT g.webhook FROM scheduled_tasks t JOIN lark_groups g ON g.id=t.lark_group_id WHERE t.task_key=?`, taskKey).Scan(&webhook)
	return webhook
}

type remindItem struct {
	name   string
	days   int
	expiry string // 到期日 YYYY-MM-DD
}

// remindDot 按剩余天数给严重度色点：🔴已过期或≤7 / 🟠≤15 / 🟡其它
func remindDot(days int) string {
	switch {
	case days <= 7:
		return "🔴"
	case days <= 15:
		return "🟠"
	default:
		return "🟡"
	}
}

// remindPhrase 到期措辞：已过期 N 天 / 今天到期 / 还有 N 天到期
func remindPhrase(days int) string {
	switch {
	case days < 0:
		return fmt.Sprintf("已过期 %d 天", -days)
	case days == 0:
		return "今天到期"
	default:
		return fmt.Sprintf("还有 %d 天到期", days)
	}
}

// remindExpiry 证书/域名剩余天数 ≤ 最大阈值（含已过期）时发飞书提醒，每天一张汇总卡直到续期/续费。
// 返回一张按「证书/域名」分组、组内按剩余天数升序的多行汇总摘要（作为任务结果卡片发送）。
func remindExpiry(db *sql.DB) string {
	webhook := taskWebhook(db, "remind")
	if webhook == "" {
		return "未配置 Lark 群，跳过"
	}
	// 取最大阈值作为提醒窗口：剩余天数 ≤ maxTh（含已过期，DATEDIFF 为负）就每天提醒，直到续期/续费。
	// 不再按精确天数（== 1/7/15/30）命中，避免"错过当天=永不再提醒 + 已过期不报"。
	maxTh := 0
	for _, s := range strings.Split(getSetting(db, "remind_days"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > maxTh {
			maxTh = n
		}
	}
	if maxTh <= 0 {
		return "未配置提醒阈值(remind_days)，跳过"
	}

	var certs, doms []remindItem
	// 证书：≤maxTh 天（含已过期）
	crows, _ := db.Query(`
		SELECT t.cn, DATEDIFF(t.expiry_at, NOW()), DATE_FORMAT(t.expiry_at,'%Y-%m-%d')
		FROM certificates t WHERE t.expiry_at IS NOT NULL`)
	if crows != nil {
		for crows.Next() {
			var it remindItem
			if crows.Scan(&it.name, &it.days, &it.expiry) == nil && it.days <= maxTh {
				certs = append(certs, it)
			}
		}
		crows.Close()
	}

	// 域名（已忽略 / 已移出账号的不提醒）：≤maxTh 天（含已过期）
	drows, _ := db.Query(`
		SELECT c.name, DATEDIFF(d.expiry_at, NOW()), DATE_FORMAT(d.expiry_at,'%Y-%m-%d')
		FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.expiry_at IS NOT NULL AND COALESCE(d.ignored,0)=0 AND COALESCE(d.stale,0)=0`)
	if drows != nil {
		for drows.Next() {
			var it remindItem
			if drows.Scan(&it.name, &it.days, &it.expiry) == nil && it.days <= maxTh {
				doms = append(doms, it)
			}
		}
		drows.Close()
	}

	total := len(certs) + len(doms)
	if total == 0 {
		return fmt.Sprintf("正常：无 %d 天内到期项（含已过期）", maxTh)
	}
	sort.SliceStable(certs, func(i, j int) bool { return certs[i].days < certs[j].days })
	sort.SliceStable(doms, func(i, j int) bool { return doms[i].days < doms[j].days })

	// 汇总卡：命中 N 项 —— 证书 x · 域名 y + 分组多行（只发一张卡，避免逐条每天刷屏）
	var counts []string
	if len(certs) > 0 {
		counts = append(counts, fmt.Sprintf("证书 %d", len(certs)))
	}
	if len(doms) > 0 {
		counts = append(counts, fmt.Sprintf("域名 %d", len(doms)))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "命中 %d 项 —— %s（≤%d 天每天提醒，直到续期/续费）", total, strings.Join(counts, " · "), maxTh)
	if len(certs) > 0 {
		b.WriteString("\n\n证书")
		for _, it := range certs {
			fmt.Fprintf(&b, "\n%s %s %s（%s）", remindDot(it.days), it.name, remindPhrase(it.days), it.expiry)
		}
	}
	if len(doms) > 0 {
		b.WriteString("\n\n域名（请到注册商续费）")
		for _, it := range doms {
			fmt.Fprintf(&b, "\n%s %s %s（%s）", remindDot(it.days), it.name, remindPhrase(it.days), it.expiry)
		}
	}
	return b.String()
}

// notifyEvent 按事件开关决定是否发送；发送时 @ 通知人（阶段②增强）。
// 事件 key 对应 settings：notify_cert_expiring / notify_renew_success / notify_renew_fail / notify_domain_expiring
func notifyEvent(db *sql.DB, webhook, taskKey, eventKey, text string) {
	if webhook == "" {
		return
	}
	// 默认开（settings 没存该 key 时按开处理），显式 '0' 才关
	if getSetting(db, eventKey) == "0" {
		return
	}
	_ = notify.SendFeishu(webhook, text+atMentionsForTask(db, taskKey))
}

// atMentions 拼接启用的通知人 @（飞书 open_id）。阶段②：notify_users 表。
func atMentions(db *sql.DB) string {
	rows, err := db.Query(`SELECT open_id FROM notify_users WHERE enabled=1 AND open_id<>''`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var oid string
		if rows.Scan(&oid) == nil {
			b.WriteString(fmt.Sprintf(`<at user_id="%s"></at>`, oid))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n" + b.String()
}
