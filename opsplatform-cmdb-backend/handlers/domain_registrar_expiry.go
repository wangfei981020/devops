package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/dnsource"
	"opsplatform-cmdb-backend/logx"
)

// 从注册商 API 同步域名到期日。
//
//	## 为什么要单独做这个
//
//	原有的 refresh_expiry 任务走的是 RDAP/WHOIS。那条路有两个硬伤：
//
//	  1. **续费后不会立刻反映**。WHOIS 是注册局的公开数据，续费后往往要
//	     几小时甚至一两天才更新。刚续完费去看台账，到期日还是旧的，
//	     容易让人以为"续费没成功"而重复操作——那是真金白银。
//	  2. **限流和不稳定**。很多 TLD 的 WHOIS 对频繁查询直接拒绝，
//	     结果就是一批域名刷不出来。
//
//	注册商 API（GoDaddy 的 /v1/domains）给的是**它自己账本上的到期日**，
//	续费成功当场就变，而且一次请求能拿回账户下全部域名，不受 WHOIS 限流影响。
//
//	## 和 refresh_expiry 的分工
//
//	  registrar_expiry_sync（本任务）：数据源里配了凭据的域名 —— 权威、快
//	  refresh_expiry（WHOIS）        ：手动录入、不属于任何数据源的域名 —— 兜底
//
//	两者不冲突：本任务只更新能在注册商账户里找到的域名。

// registrarExpirySyncCore 遍历所有启用的数据源，用注册商 API 刷新域名到期日。
func registrarExpirySyncCore(ctx context.Context, db *sql.DB, cipher *crypto.Cipher, p ProgressFn) (string, []TaskFailure, bool) {
	rows, err := db.Query(`SELECT id, name, provider FROM registrars WHERE enabled=1`)
	if err != nil {
		return "读取数据源失败：" + err.Error(), nil, false
	}
	type src struct {
		ID       int
		Name     string
		Provider string
	}
	var sources []src
	for rows.Next() {
		var s src
		if rows.Scan(&s.ID, &s.Name, &s.Provider) == nil {
			sources = append(sources, s)
		}
	}
	rows.Close()

	if len(sources) == 0 {
		return "没有启用的注册商数据源（到期日仍由 WHOIS 任务维护）", nil, true
	}

	var failures []TaskFailure
	updated, unchanged, notInAccount := 0, 0, 0

	for i, s := range sources {
		if p != nil {
			p(i, len(sources))
		}
		ad, err := adapterForRegistrar(db, cipher, s.ID)
		if err != nil {
			failures = append(failures, TaskFailure{Target: s.Name, Reason: "凭据不可用：" + err.Error()})
			continue
		}

		listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		domains, err := ad.ListDomains(listCtx)
		cancel()
		if err != nil {
			failures = append(failures, TaskFailure{Target: s.Name, Reason: "拉取域名列表失败：" + err.Error()})
			continue
		}

		for _, d := range domains {
			if d.ExpiresAt == nil {
				continue
			}
			name := strings.ToLower(d.Name)
			newExp := d.ExpiresAt.Format("2006-01-02")

			var ciID int64
			var oldExp sql.NullString
			err := db.QueryRow(`SELECT c.id, DATE_FORMAT(dm.expiry_at,'%Y-%m-%d')
				FROM cis c JOIN domains dm ON dm.ci_id = c.id
				WHERE c.type='domain' AND LOWER(c.name)=?`, name).Scan(&ciID, &oldExp)
			if err != nil {
				notInAccount++ // 注册商有、CMDB 台账里没有——由 dns_sync 负责补录，这里不管
				continue
			}
			if oldExp.Valid && oldExp.String == newExp {
				unchanged++
				continue
			}
			if _, e := db.Exec(`UPDATE domains SET expiry_at=? WHERE ci_id=?`, newExp, ciID); e != nil {
				failures = append(failures, TaskFailure{Target: name, Reason: "写库失败：" + e.Error()})
				continue
			}
			logx.Line("registrar_expiry", fmt.Sprintf("%s 到期日 %s → %s（来源：%s API）",
				name, oldExp.String, newExp, s.Provider))
			updated++
		}
	}

	msg := fmt.Sprintf("注册商到期日同步完成：更新 %d 个，无变化 %d 个", updated, unchanged)
	if notInAccount > 0 {
		msg += fmt.Sprintf("，注册商账户里另有 %d 个域名不在 CMDB 台账（由 DNS 同步任务补录）", notInAccount)
	}
	if len(failures) > 0 {
		msg += fmt.Sprintf("，%d 个数据源失败", len(failures))
	}
	return msg, failures, len(failures) == 0
}

// adapterForRegistrar 按数据源 id 取只读适配器。
// 复用 LoadCredential + LimiterFor：限流器是按数据源共享的，
// 自己 new 一个会绕过限流，把注册商 API 打爆。
func adapterForRegistrar(db *sql.DB, cipher *crypto.Cipher, id int) (dnsource.Adapter, error) {
	provider, cred, err := LoadCredential(db, cipher, id)
	if err != nil {
		return nil, err
	}
	return dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
}
