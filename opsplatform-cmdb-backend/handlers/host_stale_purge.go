package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"opsplatform-cmdb-backend/logx"
)

// 清理长期失效的主机记录。
//
//	## 为什么需要
//
//	同步时云上查不到的实例会被标 stale=1 但**不删记录**（保留历史）。
//	问题是从来没有清理：生产上已经积了 65 条，几乎全是 GKE 节点——
//	节点池轮换、扩缩容、升级换机时节点是销毁重建的，名字带随机后缀，
//	旧的每次都变成一条新的失效记录。只增不减，半年后会有几百条。
//
//	## ⚠️ 为什么不能"看到 stale=1 就删"
//
//	stale 是**推断**出来的，不是云上告诉我们的：同步时 GCP 没返回这台机器，
//	我们就认为它没了。但"GCP 没返回"有另一种可能——**这次同步本身就是坏的**：
//	凭据过期、API 被限流、权限被收回，任何一种都会让列表返回空或残缺，
//	于是**整个 project 的主机被一次性标 stale**。
//	这时候按 stale 清理，等于把完好的台账整个删掉。
//
//	所以两道闸：
//	  1. 只删**失效超过 N 天**的——一次抖动造成的误标，下一轮同步就会被
//	     upsert 回 stale=0，撑不过 N 天
//	  2. 该 project **最近一次同步必须是成功的**——同步一直在报错就别动它的数据

const staleKeepDays = 30

// purgeStaleHostsCore 定时任务入口。
func purgeStaleHostsCore(ctx context.Context, db *sql.DB, p ProgressFn) (string, []TaskFailure, bool) {
	// 取每个 project 的最近同步结果，判断能不能信任它的 stale 标记
	rows, err := db.Query(`SELECT ap.account_id, ap.project_id, COALESCE(ap.last_result,''), ap.last_sync_at
		FROM cloud_account_projects ap`)
	if err != nil {
		return "读取项目列表失败：" + err.Error(), nil, false
	}
	type proj struct {
		accountID  int
		projectID  string
		lastResult string
		lastSync   sql.NullTime
	}
	var projects []proj
	for rows.Next() {
		var x proj
		if rows.Scan(&x.accountID, &x.projectID, &x.lastResult, &x.lastSync) == nil {
			projects = append(projects, x)
		}
	}
	rows.Close()

	var failures []TaskFailure
	purged, skipped := 0, 0

	for i, pr := range projects {
		if p != nil {
			p(i, len(projects))
		}
		// 闸一：最近一次同步必须成功过。从没同步过、或结果里带 ⚠️/失败字样的，
		// 说明它的 stale 标记可能是同步坏了造成的，不能据此删数据。
		if !pr.lastSync.Valid {
			skipped++
			failures = append(failures, TaskFailure{
				Target: pr.projectID, Reason: "从未成功同步过，其失效标记不可信，跳过清理"})
			continue
		}
		if syncResultLooksBad(pr.lastResult) {
			skipped++
			failures = append(failures, TaskFailure{
				Target: pr.projectID,
				Reason: "最近一次同步有异常（" + truncate(pr.lastResult, 80) + "），跳过清理以免误删台账"})
			continue
		}

		// 闸二：只删失效超过 N 天的。一次抖动误标的，下一轮同步 upsert 回
		// stale=0，撑不过 N 天。
		res, e := db.Exec(`DELETE h FROM hosts h
			WHERE h.cloud_account_id=? AND h.project=? AND h.stale=1
			  AND h.synced_at IS NOT NULL AND h.synced_at < DATE_SUB(NOW(), INTERVAL ? DAY)`,
			pr.accountID, pr.projectID, staleKeepDays)
		if e != nil {
			failures = append(failures, TaskFailure{Target: pr.projectID, Reason: "删除失败：" + e.Error()})
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			// 主机记录删了，对应的 cis 行也要清，否则留下一堆没有主体的孤儿 CI
			db.Exec(`DELETE c FROM cis c LEFT JOIN hosts h ON h.ci_id=c.id
				WHERE c.type='host' AND h.ci_id IS NULL`)
			logx.Line("stale_purge", fmt.Sprintf("project=%s 清理失效超过 %d 天的主机 %d 台",
				pr.projectID, staleKeepDays, n))
			purged += int(n)
		}
	}

	msg := fmt.Sprintf("清理失效超过 %d 天的主机：删除 %d 台", staleKeepDays, purged)
	if skipped > 0 {
		msg += fmt.Sprintf("；%d 个项目因同步异常被跳过（详见失败明细）", skipped)
	}
	// 跳过不算失败——那是刻意的保护动作，报成失败会让人以为任务坏了
	return msg, failures, true
}

// syncResultLooksBad 从 last_result 文案判断这次同步是不是有问题。
//
//	用文案判断不够优雅，但 cloud_account_projects 没有独立的成功标志位，
//	而加一列要迁移。这里只做保守判断：**只要看着像有问题就跳过清理**——
//	跳过的代价是记录多留几天，误删的代价是台账没了，不对称。
func syncResultLooksBad(result string) bool {
	if result == "" {
		return true // 没有结果 = 不知道成没成功 = 不能信
	}
	low := strings.ToLower(result)
	for _, k := range []string{"⚠️", "失败", "error", "未同步", "超时", "timeout"} {
		if strings.Contains(low, strings.ToLower(k)) {
			return true
		}
	}
	return false
}
