package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/dnsource"
	"opsplatform-cmdb-backend/notify"
)

// Scheduler 用 cron 调度可配置定时任务（scheduled_tasks 表）。
// 任务可在前端开关 / 改频率 / 立即运行；改配置后热重载。
type Scheduler struct {
	db     *sql.DB
	cipher *crypto.Cipher
	mu     sync.Mutex
	cron   *cron.Cron
	funcs  map[string]func() (string, bool) // task_key -> 执行函数，返回(结果摘要, 是否成功)
}

var sched *Scheduler // 全局单例，供 API 热重载 / 立即运行

// StartScheduler 初始化调度器并按 scheduled_tasks 注册 cron。非阻塞（cron 在后台 goroutine）。
func StartScheduler(db *sql.DB, cipher *crypto.Cipher) {
	sched = &Scheduler{db: db, cipher: cipher}
	sched.funcs = map[string]func() (string, bool){
		"refresh_expiry": func() (string, bool) { return refreshAllWhoisCore(db) },
		"auto_renew":     func() (string, bool) { renewDue(db, cipher); return "已扫描并触发到期前 30 天证书续期", true },
		"remind":         func() (string, bool) { remindExpiry(db); return "已按阈值发送到期提醒", true },
		"inspect":        func() (string, bool) { return inspectAllCertsCore(db) },
		"dns_sync":       func() (string, bool) { return dnsSyncCore(db, cipher) },
	}
	sched.reload()
}

// ReloadScheduler 改了 scheduled_tasks 配置后重建 cron（供 API 调用）。
func ReloadScheduler() {
	if sched != nil {
		sched.reload()
	}
}

// RunTaskNow 立即异步跑一次指定任务（供 API「立即运行」调用）。
func RunTaskNow(key string) bool {
	if sched == nil || sched.funcs[key] == nil {
		return false
	}
	go sched.run(key)
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
		log.Printf("scheduler reload query: %v", err)
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
		if _, err := s.cron.AddFunc(schedule, func() { s.run(k) }); err != nil {
			log.Printf("scheduler add %s (%q): %v", key, schedule, err)
		}
	}
	s.cron.Start()
}

func (s *Scheduler) run(key string) {
	fn := s.funcs[key]
	if fn == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("scheduler task %s panic: %v", key, r)
			_, _ = s.db.Exec(`UPDATE scheduled_tasks SET last_run_at=NOW(), last_result=?, last_ok=0 WHERE task_key=?`, fmt.Sprintf("panic: %v", r), key)
			sendTaskNotify(s.db, key, fmt.Sprintf("panic: %v", r), false)
		}
	}()
	log.Printf("[cron] 运行任务 %s", key)
	result, ok := fn()
	okv := 0
	if ok {
		okv = 1
	}
	_, _ = s.db.Exec(`UPDATE scheduled_tasks SET last_run_at=NOW(), last_result=?, last_ok=? WHERE task_key=?`, truncate(result, 250), okv, key)
	sendTaskNotify(s.db, key, result, ok)
}

// ---- 任务核心函数 ----

// refreshAllWhoisCore 刷新所有域名的注册到期(WHOIS)。只管注册到期，不碰证书（证书由 inspect 统一）。
func refreshAllWhoisCore(db *sql.DB) (string, bool) {
	rows, err := db.Query(`SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain' AND d.stale=0`)
	if err != nil {
		return "查询域名失败: " + err.Error(), false
	}
	type item struct {
		id   int64
		name string
	}
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.name) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	n := 0
	for _, it := range items {
		if t := whoisExpiry(it.name); t != nil {
			_, _ = db.Exec(`UPDATE domains SET expiry_at=? WHERE ci_id=?`, *t, it.id)
			n++
		}
	}
	return fmt.Sprintf("已刷新 %d/%d 个域名的注册到期(WHOIS)", n, len(items)), true
}

// inspectAllCertsCore 连 443 检测所有证书到期：主域名(domains) + 所有业务域名解析(domain_records)，统一一处。
func inspectAllCertsCore(db *sql.DB) (string, bool) {
	type target struct {
		id     int64
		fqdn   string
		isMain bool // true=写 domains 表，false=写 domain_records 表
	}
	var targets []target
	drows, err := db.Query(`SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain' AND d.stale=0`)
	if err != nil {
		return "查询域名失败: " + err.Error(), false
	}
	for drows.Next() {
		var id int64
		var name string
		if drows.Scan(&id, &name) == nil {
			targets = append(targets, target{id, name, true})
		}
	}
	drows.Close()
	rrows, err := db.Query(`SELECT r.id, r.host, c.name FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id WHERE c.type='domain' AND r.ignored=0`)
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

	var ok, fail int32
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(tg target) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if ct, cmsg := tlsCertExpiry(tg.fqdn); ct != nil {
				if tg.isMain {
					_, _ = db.Exec(`UPDATE domains SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg='' WHERE ci_id=?`, *ct, tg.id)
				} else {
					_, _ = db.Exec(`UPDATE domain_records SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg='' WHERE id=?`, *ct, tg.id)
				}
				atomic.AddInt32(&ok, 1)
			} else {
				if tg.isMain {
					_, _ = db.Exec(`UPDATE domains SET cert_check_at=NOW(), cert_check_msg=? WHERE ci_id=?`, truncate(cmsg, 250), tg.id)
				} else {
					_, _ = db.Exec(`UPDATE domain_records SET cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, truncate(cmsg, 250), tg.id)
				}
				atomic.AddInt32(&fail, 1)
			}
		}(t)
	}
	wg.Wait()
	return fmt.Sprintf("检测 %d 张证书（主域名+业务域名），成功 %d / 失败 %d", len(targets), ok, fail), true
}

// dnsSyncCore 定时全量同步所有数据源的 DNS 记录（复用 SyncHandler 的方法）。
func dnsSyncCore(db *sql.DB, cipher *crypto.Cipher) (string, bool) {
	sh := NewSyncHandler(db, cipher)
	rows, err := db.Query(`SELECT id FROM registrars`)
	if err != nil {
		return "查询数据源失败: " + err.Error(), false
	}
	var ids []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return "没有配置数据源，跳过", true
	}
	totalD, totalR, totalImp, failSrc := 0, 0, 0, 0
	for _, id := range ids {
		provider, cred, err := LoadCredential(db, cipher, id)
		if err != nil {
			failSrc++
			continue
		}
		adapter, err := dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
		if err != nil {
			failSrc++
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		domains, err := adapter.ListDomains(ctx)
		if err != nil {
			cancel()
			failSrc++
			continue
		}
		present := map[string]bool{}
		for _, d := range domains {
			ciID, err := sh.upsertDomainCI(d.Name, id, d.ExpiresAt)
			if err != nil {
				continue
			}
			present[d.Name] = true
			totalD++
			recs, err := adapter.ListRecords(ctx, d.Name)
			if err != nil {
				continue
			}
			sh.refreshDNSRecords(ciID, id, recs)
			totalR += len(recs)
			totalImp += sh.importBusinessRecords(ciID, recs)
		}
		sh.markStaleDomains(id, present)
		cancel()
	}
	ok := !(totalD == 0 && failSrc == len(ids)) // 全部源都失败才算失败
	msg := fmt.Sprintf("同步 %d 域名 / %d DNS 记录 / 导入 %d 新解析", totalD, totalR, totalImp)
	if failSrc > 0 {
		msg += fmt.Sprintf("，%d/%d 个数据源失败", failSrc, len(ids))
	}
	return msg, ok
}

// sendTaskNotify 任务跑完发 Lark 卡片到该任务配置的群，带 ✅/❌ + 结果 + @人。
func sendTaskNotify(db *sql.DB, taskKey, result string, ok bool) {
	var name, notifyWhen string
	var notifyEnabled int
	var groupID sql.NullInt64
	if err := db.QueryRow(`SELECT name, notify_enabled, lark_group_id, notify_when FROM scheduled_tasks WHERE task_key=?`,
		taskKey).Scan(&name, &notifyEnabled, &groupID, &notifyWhen); err != nil {
		return
	}
	if notifyEnabled == 0 {
		return
	}
	if notifyWhen == "fail" && ok {
		return
	}
	if !groupID.Valid {
		return
	}
	var webhook string
	if db.QueryRow(`SELECT webhook FROM lark_groups WHERE id=?`, groupID.Int64).Scan(&webhook) != nil || webhook == "" {
		return
	}
	status := "✅ 执行成功"
	if !ok {
		status = "❌ 执行失败"
	}
	text := fmt.Sprintf("【CMDB 定时任务】%s %s\n时间：%s\n结果：%s", status, name, time.Now().Format("2006-01-02 15:04"), result)
	text += atMentionsForTask(db, taskKey)
	_ = notify.SendFeishu(webhook, text)
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

// renewDue 续期 auto_renew 且 expiry_at - now < renew_days 天的证书
func renewDue(db *sql.DB, cipher *crypto.Cipher) {
	rows, err := db.Query(`
		SELECT t.ci_id, COALESCE(r.dst_ci_id, 0), t.ca, t.cn
		FROM certificates t
		LEFT JOIN ci_relations r ON r.src_ci_id=t.ci_id AND r.rel_type='protects'
		WHERE t.auto_renew=1 AND t.status='active' AND t.expiry_at IS NOT NULL
		  AND t.expiry_at < DATE_ADD(NOW(), INTERVAL t.renew_days DAY)`)
	if err != nil {
		return
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

	webhook := taskWebhook(db, "auto_renew")
	for _, j := range jobs {
		var acctID int
		if err := db.QueryRow(`SELECT id FROM acme_accounts WHERE ca=? ORDER BY id LIMIT 1`, j.ca).Scan(&acctID); err != nil {
			continue
		}
		log.Printf("auto-renew cert %s (ci %d)", j.cn, j.certCIID)
		if errMsg := issueCertCore(db, cipher, j.certCIID, j.domainCIID, acctID, false, "renew"); errMsg != "" {
			notifyEvent(db, webhook, "auto_renew", "notify_renew_fail", fmt.Sprintf("❌ 证书自动续期失败：%s\n原因：%s", j.cn, errMsg))
		} else {
			notifyEvent(db, webhook, "auto_renew", "notify_renew_success", fmt.Sprintf("✅ 证书自动续期成功：%s", j.cn))
		}
	}
}

// taskWebhook 取某任务配置的 Lark 群 webhook。
func taskWebhook(db *sql.DB, taskKey string) string {
	var webhook string
	_ = db.QueryRow(`SELECT g.webhook FROM scheduled_tasks t JOIN lark_groups g ON g.id=t.lark_group_id WHERE t.task_key=?`, taskKey).Scan(&webhook)
	return webhook
}

// remindExpiry 证书/域名到期前命中阈值天数时发飞书提醒
func remindExpiry(db *sql.DB) {
	webhook := taskWebhook(db, "remind")
	if webhook == "" {
		return
	}
	thresholds := map[int]bool{}
	for _, s := range strings.Split(getSetting(db, "remind_days"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			thresholds[n] = true
		}
	}
	if len(thresholds) == 0 {
		return
	}

	// 证书
	crows, _ := db.Query(`
		SELECT t.cn, DATEDIFF(t.expiry_at, NOW())
		FROM certificates t WHERE t.expiry_at IS NOT NULL`)
	if crows != nil {
		for crows.Next() {
			var cn string
			var days int
			if crows.Scan(&cn, &days) == nil && thresholds[days] {
				notifyEvent(db, webhook, "remind", "notify_cert_expiring", fmt.Sprintf("⚠️ 证书 %s 还有 %d 天到期，请关注续期", cn, days))
			}
		}
		crows.Close()
	}

	// 域名
	drows, _ := db.Query(`
		SELECT c.name, DATEDIFF(d.expiry_at, NOW())
		FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.expiry_at IS NOT NULL`)
	if drows != nil {
		for drows.Next() {
			var name string
			var days int
			if drows.Scan(&name, &days) == nil && thresholds[days] {
				notifyEvent(db, webhook, "remind", "notify_domain_expiring", fmt.Sprintf("⚠️ 域名 %s 还有 %d 天到期，请到注册商续费", name, days))
			}
		}
		drows.Close()
	}
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
