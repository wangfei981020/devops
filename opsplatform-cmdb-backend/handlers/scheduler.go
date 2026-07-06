package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/robfig/cron/v3"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/notify"
)

// Scheduler 用 cron 调度可配置定时任务（scheduled_tasks 表）。
// 任务可在前端开关 / 改频率 / 立即运行；改配置后热重载。
type Scheduler struct {
	db     *sql.DB
	cipher *crypto.Cipher
	mu     sync.Mutex
	cron   *cron.Cron
	funcs  map[string]func() string // task_key -> 执行函数，返回结果摘要
}

var sched *Scheduler // 全局单例，供 API 热重载 / 立即运行

// StartScheduler 初始化调度器并按 scheduled_tasks 注册 cron。非阻塞（cron 在后台 goroutine）。
func StartScheduler(db *sql.DB, cipher *crypto.Cipher) {
	sched = &Scheduler{db: db, cipher: cipher}
	sched.funcs = map[string]func() string{
		"refresh_expiry": func() string { return refreshAllExpiryCore(db) },
		"auto_renew":     func() string { renewDue(db, cipher); return "已扫描并触发到期前 30 天证书续期" },
		"remind":         func() string { remindExpiry(db); return "已按阈值发送到期提醒" },
		"inspect":        func() string { return inspectAllExpiryCore(db) },
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
			_, _ = s.db.Exec(`UPDATE scheduled_tasks SET last_run_at=NOW(), last_result=? WHERE task_key=?`, fmt.Sprintf("panic: %v", r), key)
		}
	}()
	log.Printf("[cron] 运行任务 %s", key)
	result := fn()
	_, _ = s.db.Exec(`UPDATE scheduled_tasks SET last_run_at=NOW(), last_result=? WHERE task_key=?`, truncate(result, 250), key)
}

// ---- 任务核心函数 ----

// refreshAllExpiryCore 刷新所有域名的注册到期(WHOIS) + 主域名证书到期(443)。
func refreshAllExpiryCore(db *sql.DB) string {
	rows, err := db.Query(`SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain'`)
	if err != nil {
		return "查询域名失败: " + err.Error()
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
	for _, it := range items {
		refreshOneDomain(db, it.id, it.name)
	}
	return fmt.Sprintf("已刷新 %d 个域名的到期时间", len(items))
}

// inspectAllExpiryCore 逐条连 443 检测所有域名下所有解析记录的证书到期。
func inspectAllExpiryCore(db *sql.DB) string {
	rows, err := db.Query(`SELECT r.id, r.host, c.name FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id WHERE c.type='domain' AND r.ignored=0`)
	if err != nil {
		return "查询解析失败: " + err.Error()
	}
	type rec struct {
		id           int64
		host, domain string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if rows.Scan(&r.id, &r.host, &r.domain) == nil {
			recs = append(recs, r)
		}
	}
	rows.Close()

	var ok, fail int32
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, r := range recs {
		wg.Add(1)
		go func(id int64, host, domain string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fqdn := recordFQDN(host, domain)
			if t, cmsg := tlsCertExpiry(fqdn); t != nil {
				_, _ = db.Exec(`UPDATE domain_records SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg='' WHERE id=?`, *t, id)
				atomic.AddInt32(&ok, 1)
			} else {
				_, _ = db.Exec(`UPDATE domain_records SET cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, truncate(cmsg, 250), id)
				atomic.AddInt32(&fail, 1)
			}
		}(r.id, r.host, r.domain)
	}
	wg.Wait()
	return fmt.Sprintf("检测 %d 条解析证书，成功 %d / 失败 %d", len(recs), ok, fail)
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

	webhook := getSetting(db, "feishu_webhook")
	for _, j := range jobs {
		var acctID int
		if err := db.QueryRow(`SELECT id FROM acme_accounts WHERE ca=? ORDER BY id LIMIT 1`, j.ca).Scan(&acctID); err != nil {
			continue
		}
		log.Printf("auto-renew cert %s (ci %d)", j.cn, j.certCIID)
		if errMsg := issueCertCore(db, cipher, j.certCIID, j.domainCIID, acctID, false, "renew"); errMsg != "" {
			notifyEvent(db, webhook, "notify_renew_fail", fmt.Sprintf("❌ 证书自动续期失败：%s\n原因：%s", j.cn, errMsg))
		} else {
			notifyEvent(db, webhook, "notify_renew_success", fmt.Sprintf("✅ 证书自动续期成功：%s", j.cn))
		}
	}
}

// remindExpiry 证书/域名到期前命中阈值天数时发飞书提醒
func remindExpiry(db *sql.DB) {
	webhook := getSetting(db, "feishu_webhook")
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
				notifyEvent(db, webhook, "notify_cert_expiring", fmt.Sprintf("⚠️ 证书 %s 还有 %d 天到期，请关注续期", cn, days))
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
				notifyEvent(db, webhook, "notify_domain_expiring", fmt.Sprintf("⚠️ 域名 %s 还有 %d 天到期，请到注册商续费", name, days))
			}
		}
		drows.Close()
	}
}

// notifyEvent 按事件开关决定是否发送；发送时 @ 通知人（阶段②增强）。
// 事件 key 对应 settings：notify_cert_expiring / notify_renew_success / notify_renew_fail / notify_domain_expiring
func notifyEvent(db *sql.DB, webhook, eventKey, text string) {
	if webhook == "" {
		return
	}
	// 默认开（settings 没存该 key 时按开处理），显式 '0' 才关
	if getSetting(db, eventKey) == "0" {
		return
	}
	_ = notify.SendFeishu(webhook, text+atMentions(db))
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
