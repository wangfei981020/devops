package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"opsplatform-cmdb-backend/logx"
)

// 关系图谱自动建边。
//
//	## 为什么要有这个任务
//
//	关系图谱页存在很久，但 ci_relations 里长期只有 5 条边——全是申请证书时
//	顺手写的「证书 protects 域名」。除此之外**没有任何自动建边的机制**，
//	要连线只能靠人一条条 POST，所以图上永远只有证书和域名两类点。
//	页面本身没坏，是它要画的数据从来没被生产出来过（CMDB-009）。
//
//	## 边从哪来
//
//	全部来自已经采到的字段，不新增任何采集：
//	  域名 --resolves_to--> 主机          解析记录的 origin_ip 命中主机内/外网 IP
//	  域名 --resolves_to--> 负载均衡      解析记录的 origin_ip 命中 LB 的 VIP
//	  负载均衡 --backs--> 主机            cloud_lb_backends 里的后端实例名
//
//	## ⚠️ 自动边和手工边必须分得开
//
//	重建时只能清掉自己上一轮建的（origin='auto'）。手工连的线是人的判断，
//	被一个定时任务默默删掉，人不会知道发生了什么。
const relAutoOrigin = "auto"

// 负载均衡在 cis 里的类型。LB 原来完全不在 CI 台账里，
// 而 ci_relations 两端都必须是 ci_id，所以先给它们建/补 CI 记录。
const ciTypeLB = "loadbalancer"

type autoEdge struct {
	src, dst int64
	relType  string
}

// rebuildAutoRelationsCore 重建自动关系边。
func rebuildAutoRelationsCore(ctx context.Context, db *sql.DB, p ProgressFn) (string, []TaskFailure, bool) {
	var failures []TaskFailure

	// ── 1. 给负载均衡补 CI 记录 ──
	lbCI, lbFail := ensureLBCIs(db)
	if lbFail != nil {
		failures = append(failures, *lbFail)
	}
	if p != nil {
		p(1, 4)
	}

	// ── 2. 收集边 ──
	edges := []autoEdge{}

	// 主机 IP → ci_id。内外网 IP 都收，一台机器两个都可能被解析指向。
	hostByIP := map[string]int64{}
	if rows, err := db.Query(`SELECT h.ci_id, COALESCE(h.internal_ip,''), COALESCE(h.external_ip,'')
		FROM hosts h WHERE h.stale=0`); err != nil {
		failures = append(failures, TaskFailure{Target: "hosts", Reason: "读取主机 IP 失败：" + err.Error()})
	} else {
		for rows.Next() {
			var id int64
			var in, ex string
			if rows.Scan(&id, &in, &ex) != nil {
				continue
			}
			for _, ip := range []string{in, ex} {
				if ip = strings.TrimSpace(ip); ip != "" {
					hostByIP[ip] = id
				}
			}
		}
		rows.Close()
	}

	// LB VIP → ci_id
	lbByVIP := map[string]int64{}
	if rows, err := db.Query(`SELECT l.cloud_account_id, l.project, l.name, COALESCE(l.vip,'') FROM cloud_loadbalancers l`); err != nil {
		failures = append(failures, TaskFailure{Target: "cloud_loadbalancers", Reason: "读取 LB 失败：" + err.Error()})
	} else {
		for rows.Next() {
			var aid int
			var project, name, vip string
			if rows.Scan(&aid, &project, &name, &vip) != nil {
				continue
			}
			if vip = strings.TrimSpace(vip); vip == "" {
				continue
			}
			if id, ok := lbCI[lbKey(aid, project, name)]; ok {
				lbByVIP[vip] = id
			}
		}
		rows.Close()
	}
	if p != nil {
		p(2, 4)
	}

	// 域名 → 主机 / LB：解析记录的 origin_ip 可能是逗号分隔的多个地址
	dnsHit, dnsMiss := 0, 0
	if rows, err := db.Query(`SELECT r.domain_ci_id, COALESCE(r.origin_ip,'') FROM domain_records r
		WHERE r.ignored=0 AND COALESCE(r.origin_ip,'')<>''`); err != nil {
		failures = append(failures, TaskFailure{Target: "domain_records", Reason: "读取解析记录失败：" + err.Error()})
	} else {
		for rows.Next() {
			var domainCI int64
			var origin string
			if rows.Scan(&domainCI, &origin) != nil {
				continue
			}
			matched := false
			for _, ip := range strings.Split(origin, ",") {
				ip = strings.TrimSpace(ip)
				if ip == "" {
					continue
				}
				if hid, ok := hostByIP[ip]; ok {
					edges = append(edges, autoEdge{domainCI, hid, "resolves_to"})
					matched = true
				}
				if lid, ok := lbByVIP[ip]; ok {
					edges = append(edges, autoEdge{domainCI, lid, "resolves_to"})
					matched = true
				}
			}
			if matched {
				dnsHit++
			} else {
				dnsMiss++
			}
		}
		rows.Close()
	}

	// LB → 后端主机
	lbHit, lbMiss := 0, 0
	if rows, err := db.Query(`SELECT b.cloud_account_id, b.project, b.lb_name, b.instance
		FROM cloud_lb_backends b`); err != nil {
		failures = append(failures, TaskFailure{Target: "cloud_lb_backends", Reason: "读取 LB 后端失败：" + err.Error()})
	} else {
		// 主机名 → ci_id（同名实例跨 project 极少见，取任一即可连线）
		hostByName := map[string]int64{}
		if hrows, e := db.Query(`SELECT c.id, c.name FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE c.type='host' AND h.stale=0`); e == nil {
			for hrows.Next() {
				var id int64
				var name string
				if hrows.Scan(&id, &name) == nil {
					hostByName[name] = id
				}
			}
			hrows.Close()
		}
		for rows.Next() {
			var aid int
			var project, lbName, inst string
			if rows.Scan(&aid, &project, &lbName, &inst) != nil {
				continue
			}
			lid, ok := lbCI[lbKey(aid, project, lbName)]
			hid, ok2 := hostByName[inst]
			if ok && ok2 {
				edges = append(edges, autoEdge{lid, hid, "backs"})
				lbHit++
			} else {
				lbMiss++
			}
		}
		rows.Close()
	}
	if p != nil {
		p(3, 4)
	}

	// ── 3. 落库：先清自己上一轮的，再整批写 ──
	tx, err := db.Begin()
	if err != nil {
		return "开启事务失败：" + err.Error(), failures, false
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.Exec(`DELETE FROM ci_relations WHERE origin=?`, relAutoOrigin); err != nil {
		return "清理上一轮自动关系失败：" + err.Error(), failures, false
	}
	written := 0
	for _, e := range edges {
		if e.src == 0 || e.dst == 0 || e.src == e.dst {
			continue
		}
		// INSERT IGNORE：同一条边可能从多个解析记录推出来（多条记录指向同一台机器），
		// 唯一键会挡住重复
		res, err := tx.Exec(`INSERT IGNORE INTO ci_relations (src_ci_id, dst_ci_id, rel_type, origin) VALUES (?,?,?,?)`,
			e.src, e.dst, e.relType, relAutoOrigin)
		if err != nil {
			return "写入关系失败：" + err.Error(), failures, false
		}
		if n, _ := res.RowsAffected(); n > 0 {
			written++
		}
	}
	if err := tx.Commit(); err != nil {
		return "提交失败：" + err.Error(), failures, false
	}
	committed = true
	if p != nil {
		p(4, 4)
	}

	// 命中/落空都要报出来：只报"建了 N 条边"的话，一旦 origin_ip 整列没采到，
	// 结果同样是"0 条边"，和"确实没有关联"长得一样
	logx.J("relations_auto", "rebuilt", map[string]any{
		"lb_ci": len(lbCI), "edges": written,
		"dns_matched": dnsHit, "dns_unmatched": dnsMiss,
		"lb_backend_matched": lbHit, "lb_backend_unmatched": lbMiss,
	})
	msg := fmt.Sprintf("自动关系重建：%d 条边（负载均衡 CI %d 个；解析记录命中 %d/未命中 %d；LB 后端命中 %d/未命中 %d）",
		written, len(lbCI), dnsHit, dnsMiss, lbHit, lbMiss)
	if dnsMiss > 0 || lbMiss > 0 {
		msg += "；未命中多为解析指向云外地址或后端实例不在台账内"
	}
	return msg, failures, true
}

// ensureLBCIs 保证每个负载均衡在 cis 里有一条记录，返回 lbKey → ci_id。
//
//	LB 名字在 GCP 里是 project 内唯一，跨 project 可能重名，
//	所以 CI 名带上 project 前缀，避免两个 project 的同名 LB 被合成一个点。
func ensureLBCIs(db *sql.DB) (map[string]int64, *TaskFailure) {
	out := map[string]int64{}
	rows, err := db.Query(`SELECT cloud_account_id, project, name FROM cloud_loadbalancers`)
	if err != nil {
		return out, &TaskFailure{Target: "cloud_loadbalancers", Reason: "读取失败：" + err.Error()}
	}
	type lb struct {
		aid           int
		project, name string
	}
	var all []lb
	for rows.Next() {
		var x lb
		if rows.Scan(&x.aid, &x.project, &x.name) == nil {
			all = append(all, x)
		}
	}
	rows.Close()

	// 现有 LB 类型的 CI
	existing := map[string]int64{}
	if erows, e := db.Query(`SELECT id, name FROM cis WHERE type=?`, ciTypeLB); e == nil {
		for erows.Next() {
			var id int64
			var name string
			if erows.Scan(&id, &name) == nil {
				existing[name] = id
			}
		}
		erows.Close()
	}

	live := map[string]bool{}
	for _, x := range all {
		ciName := x.project + "/" + x.name
		live[ciName] = true
		if id, ok := existing[ciName]; ok {
			out[lbKey(x.aid, x.project, x.name)] = id
			continue
		}
		res, e := db.Exec(`INSERT INTO cis (type, name, project, status) VALUES (?,?,?,'active')`,
			ciTypeLB, ciName, x.project)
		if e != nil {
			logx.J("relations_auto", "lb_ci_insert_fail", map[string]any{"ci": ciName, "err": e.Error()})
			continue
		}
		id, _ := res.LastInsertId()
		out[lbKey(x.aid, x.project, x.name)] = id
	}
	// 云上已经没有的 LB，把 CI 也收掉，免得图上留一堆连不上任何东西的孤点。
	// 只删这个类型、且确实不在当前 LB 列表里的。
	for name, id := range existing {
		if !live[name] {
			if _, e := db.Exec(`DELETE FROM cis WHERE id=? AND type=?`, id, ciTypeLB); e == nil {
				logx.J("relations_auto", "lb_ci_removed", map[string]any{"ci": name})
			}
		}
	}
	return out, nil
}
