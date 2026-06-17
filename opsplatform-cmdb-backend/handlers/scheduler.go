package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/notify"
)

// StartScheduler 每日跑：① 自动续期将到期证书 ② 证书/域名到期提醒（飞书）。阻塞，用 go 调用。
func StartScheduler(db *sql.DB, cipher *crypto.Cipher) {
	run := func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("scheduler panic: %v", r)
			}
		}()
		renewDue(db, cipher)
		remindExpiry(db)
	}
	run()
	t := time.NewTicker(24 * time.Hour)
	for range t.C {
		run()
	}
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
			_ = notify.SendFeishu(webhook, fmt.Sprintf("❌ 证书自动续期失败：%s\n原因：%s", j.cn, errMsg))
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
				_ = notify.SendFeishu(webhook, fmt.Sprintf("⚠️ 证书 %s 还有 %d 天到期，请关注续期", cn, days))
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
				_ = notify.SendFeishu(webhook, fmt.Sprintf("⚠️ 域名 %s 还有 %d 天到期，请到注册商续费", name, days))
			}
		}
		drows.Close()
	}
}
