package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
	"ops-cmdb-backend/notify"
)

// 磁盘水位巡检（task_key = disk_watch）。
//
// 起因 CMDB-012：2026-07-31 CMDB 自己的 MySQL 数据盘（10Gi PVC）被 binlog 写满，
// 全站查库接口挂死、Cloudflare 报 524，而**没有任何告警**——是人工登进 Pod
// 执行 df -h 才发现的。写放大已经修了，但"盘满没人知道"这个洞不补，
// 下次换个原因照样再来一遍。
//
// 覆盖两类对象：
//   - PVC：kubelet_volume_stats_*（数据库、消息队列这类有状态服务的盘都在这里）
//   - 节点根分区：node_filesystem_*（盘满会导致镜像拉不下来、Pod 被驱逐）
//
// 告警抑制：同一对象同一等级，6 小时内只发一次；等级升高（warn→crit）立即再发一次。
// 不做抑制的话 30 分钟一轮会把群刷炸，最后没人看——那等于没有告警。

const (
	pvcWarnPct     = 85.0
	pvcCriticalPct = 92.0
	// 同级别重复告警的静默期。选 6 小时：磁盘从 85% 涨到写满通常以天计，
	// 6 小时既不会淹没群，也不会让人整天想不起来这事。
	diskAlertRepeat = 6 * time.Hour
)

type diskAlertItem struct {
	target string // 形如 "<namespace>/<pvc-name>" 或 "node/<node-name>"
	kind   string // pvc / node
	level  string // warning / critical
	pct    float64
	detail string
}

// diskWatchCore 巡检所有启用集群的磁盘水位。
// 返回的 summary 会写进任务历史；真正的告警走飞书，独立于任务通知。
func diskWatchCore(ctx context.Context, st *store.Store, db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	// 定时任务没有会话，租户只能从数据里读 —— 逐租户来，见 store.ForEachTenant。
	//
	//	⚠️ 这里**不能**用 Platform() 去查 k8s_clusters：那是租户表，
	//	Platform 只放行平台级白名单。原来就是这么写的，于是这个任务
	//	从多租户改造以后一次都没跑成功过，而界面上它一直显示「正常」。
	type cl struct {
		tenant store.TenantID
		id     int
		name   string
	}
	clusters := []cl{}
	tenantCount := store.CountActiveTenants(ctx, st, "disk_watch")
	failedTenants, err := store.ForEachTenant(ctx, st, "disk_watch", func(sc *store.Scoped, t store.TenantID) error {
		// 租户条件由 Scoped 自动加，这里不写 tenant_id
		rows, err := sc.Query(`SELECT id, COALESCE(display_name,name) FROM k8s_clusters
			WHERE tenant_id = ? AND enabled=1 ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c := cl{tenant: t}
			if rows.Scan(&c.id, &c.name) == nil {
				clusters = append(clusters, c)
			}
		}
		return nil
	})
	if err != nil {
		return "查集群列表失败: " + err.Error(), nil, false
	}
	// ⚠️ 所有租户都查不出来 = 任务失败，不是"检查了 0 个集群，一切正常"。
	// 后者读起来像好消息，而真相是这一轮什么都没检查
	if store.AllFailed(failedTenants, tenantCount) {
		return fmt.Sprintf("全部 %d 个租户都取不到集群列表，本轮没有检查任何集群（详见日志 tag=store）", tenantCount),
			[]TaskFailure{{Target: "所有租户", Reason: "取集群列表失败"}}, false
	}

	all := []diskAlertItem{}
	failures := []TaskFailure{}
	skipped := []string{} // 未接入监控、无从检查的集群——必须显式列出来，见下方注释
	checked := 0
	for _, c := range clusters {
		select {
		case <-ctx.Done():
			return fmt.Sprintf("已取消（已检查 %d/%d 个集群）", checked, len(clusters)), failures, false
		default:
		}
		tctx := store.ForJob(ctx, c.tenant, "disk_watch")
		items, covered, err := diskWatchCluster(tctx, st, db, cipher, c.id)
		if err != nil {
			// 取不到数据要显式记失败，不能当成"这个集群没问题"——
			// 那正是 CMDB-013 里被点名的那种假阴性。
			failures = append(failures, TaskFailure{Target: c.name, Reason: err.Error()})
			logx.J("disk_watch", "cluster_fail", map[string]any{"cluster_id": c.id, "cluster": c.name, "err": err.Error()})
			continue
		}
		if !covered {
			// 数据源连得上、但这个集群一条磁盘指标都没有（未部署监控，或集群标签值对不上）。
			// 这种情况**不算失败**（infra-b 就是还在测试阶段没部监控，属正常），
			// 但绝不能混进"已检查"里 —— 否则 summary 说"检查了 5 个集群"，
			// 其中一个其实什么都没查，又变成一次假阴性。
			skipped = append(skipped, c.name)
			AddFinding(ctx, TaskFinding{
				Level: "info", Target: c.name, Value: "未检查",
				Detail: "该集群在观测数据源里没有任何磁盘指标（未部署监控或集群标签值不匹配），本次未覆盖",
			})
			logx.J("disk_watch", "cluster_skipped", map[string]any{
				"cluster_id": c.id, "cluster": c.name,
				"reason": "数据源中无该集群的磁盘指标",
			})
			continue
		}
		checked++
		for i := range items {
			items[i].target = c.name + " · " + items[i].target
		}
		all = append(all, items...)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].pct > all[j].pct })
	fresh := filterDiskAlerts(db, all)
	if len(fresh) > 0 {
		sendDiskAlert(db, fresh)
	}

	// 每一项都落进执行记录：告警抑制期内飞书不会重发，页面上必须还能查到当前有哪些盘超标
	crit, warn := 0, 0
	critItems, warnItems := []TaskFinding{}, []TaskFinding{}
	for _, a := range all {
		f := TaskFinding{
			Level: a.level, Target: a.target,
			Value:  fmt.Sprintf("%.1f%%", a.pct),
			Detail: a.detail,
		}
		AddFinding(ctx, f)
		if a.level == "critical" {
			crit++
			critItems = append(critItems, f)
		} else {
			warn++
			warnItems = append(warnItems, f)
		}
	}

	// summary 必须带上具体对象。只给「危险 1 项」看不出要去处置什么，
	// 而任务历史列表页只显示这一行 —— 那正是本次改动的起因。
	// ⚠️ 一个集群都没检查时，**不能**说"无超阈值磁盘"。
	//
	// 空集合上的全称判断恒为真：0 个集群里当然找不到超阈值的盘。
	// 但那句话读起来是"我查过了，都正常"，而实际是"我一个都没查"。
	//
	// 这正是 internal/store/foreach.go:43 里写着的反面教材 ——
	// 「把一个响亮的失败改成静默的成功，是比不修更糟的修法」。
	// 那段注释是我们自己写的，而这里照样犯了：写下警告不等于代码里没有它。
	if checked == 0 {
		reason := "没有任何集群接入监控数据源"
		if len(skipped) > 0 {
			reason = fmt.Sprintf("%d 个集群全部未接入监控（%s）", len(skipped), strings.Join(skipped, "、"))
		}
		// 状态用 partial 而不是 ok：任务本身跑完了没报错，
		// 但它没能得出任何结论 —— 这两件事必须在界面上分得开
		return fmt.Sprintf("未检查任何集群：%s。这不是「磁盘都正常」，是没有数据可判", reason),
			[]TaskFailure{{Reason: ErrTaskSkipped.Error()}}, true
	}

	summary := fmt.Sprintf("检查 %d 个集群：", checked)
	if crit == 0 && warn == 0 {
		summary += "无超阈值磁盘"
	} else {
		segs := []string{}
		if crit > 0 {
			segs = append(segs, fmt.Sprintf("🔴危险(≥%.0f%%) %d 项【%s】", pvcCriticalPct, crit, SummarizeFindings(critItems, 2)))
		}
		if warn > 0 {
			segs = append(segs, fmt.Sprintf("🟠偏高(≥%.0f%%) %d 项【%s】", pvcWarnPct, warn, SummarizeFindings(warnItems, 2)))
		}
		summary += strings.Join(segs, "、")
	}
	if len(fresh) > 0 {
		summary += fmt.Sprintf("，本次推送 %d 条", len(fresh))
	}
	if len(skipped) > 0 {
		summary += fmt.Sprintf("；%d 个集群未接入监控未检查（%s）", len(skipped), strings.Join(skipped, "、"))
	}
	if len(failures) > 0 {
		summary += fmt.Sprintf("；%d 个集群取数失败", len(failures))
	}
	logx.J("disk_watch", "done", map[string]any{
		"clusters": checked, "critical": crit, "warning": warn,
		"notified": len(fresh), "skipped_clusters": len(skipped), "failed_clusters": len(failures),
	})
	return summary, failures, len(failures) == 0
}

// diskWatchCluster 取单个集群的 PVC + 节点磁盘水位，返回超阈值的对象。
//
// 第二个返回值 covered 回答的是「这个集群到底有没有被真正检查过」：
// 数据源连得上、但一条磁盘指标都查不到时为 false。这两种情况必须分开——
// 空结果被当成"没有超阈值的盘"，是磁盘告警最容易出的假阴性。
func diskWatchCluster(ctx context.Context, st *store.Store, db *sql.DB, cipher *crypto.Cipher, cid int) ([]diskAlertItem, bool, error) {
	_ = ctx // 租户已在 ctx 里，下沉查询迁到 Scoped 后即可用上
	obs := NewObsQueryHandler(st, db, cipher)
	base, token, clusterLabel, err := resolveEndpointFull(db, cipher, "prometheus", obs.clusterEnv(cid), cid)
	if err != nil {
		// 压根没配数据源：这是"没接监控"，不是"查失败"，交给上层当跳过处理
		logx.J("disk_watch", "no_datasource", map[string]any{"cluster_id": cid, "err": err.Error()})
		return nil, false, nil
	}
	sel := clusterSelector(db, clusterLabel, cid)
	out := []diskAlertItem{}
	seen := 0 // 采到的指标条数（不分是否超阈值）——用它判断覆盖与否

	// 1) PVC。共享文件系统的卷（k3s local-path / hostPath）要排除：
	// kubelet 对它们上报的是整个宿主机文件系统，同节点所有 PVC 数值一样，按 PVC 告警毫无意义。
	shared := (&ObsQueryHandler{DB: db}).sharedFSPVCs(fmt.Sprint(cid))
	lbl := promLabels(sel)
	used := map[string]float64{}
	if rs, err := promInstant(base, token, `kubelet_volume_stats_used_bytes`+lbl); err == nil {
		for _, s := range rs {
			used[s.Metric["namespace"]+"/"+s.Metric["persistentvolumeclaim"]] = s.Value
		}
	}
	if rs, err := promInstant(base, token, `kubelet_volume_stats_capacity_bytes`+lbl); err == nil {
		seen += len(rs)
		for _, s := range rs {
			k := s.Metric["namespace"] + "/" + s.Metric["persistentvolumeclaim"]
			if _, isShared := shared[k]; isShared || s.Value <= 0 {
				continue
			}
			pct := used[k] / s.Value * 100
			if lv := diskLevel(pct); lv != "" {
				out = append(out, diskAlertItem{
					target: "PVC " + k, kind: "pvc", level: lv, pct: pct,
					detail: fmt.Sprintf("%.1fGi / %.1fGi", used[k]/1073741824, s.Value/1073741824),
				})
			}
		}
	}

	// 2) 节点磁盘
	//
	// 🔴 这里以前写的是 mountpoint="/"，意味着**磁盘告警在三个 GKE 集群上从来没生效过**：
	//	COS 的 `/` 是只读启动镜像（1.93 GB），常年 74.07% 且永不变化 ——
	//	既不会触发告警，也不会报错，只是一直在回答另一个问题。
	//	口径已统一到 nodefs.go，那里写了三个必知的坑。
	if fs, err := nodeFsUsage(base, token, sel); err == nil {
		seen += len(fs)
		for name, v := range fs {
			if lv := diskLevel(v.Pct); lv != "" {
				out = append(out, diskAlertItem{
					target: "节点 " + name, kind: "node", level: lv, pct: v.Pct,
					// 带上挂载点和绝对量：光说「88%」没法判断要不要马上处置，
					// 而且不写挂载点的话，人无从知道我们量的到底是哪块盘（就是栽在这上面）。
					detail: fmt.Sprintf("%s %.0f%%（%.0f/%.0f GiB）", v.Mountpoint, v.Pct,
						v.UsedBytes/1073741824, v.TotalBytes/1073741824),
				})
			}
		}
	}
	return out, seen > 0, nil
}

func diskLevel(pct float64) string {
	switch {
	case pct >= pvcCriticalPct:
		return "critical"
	case pct >= pvcWarnPct:
		return "warning"
	}
	return ""
}

// filterDiskAlerts 做告警抑制，返回本轮真正需要推送的项，并更新状态表。
// 规则：等级变高立刻发；等级相同则距上次推送满 diskAlertRepeat 才再发。
func filterDiskAlerts(db *sql.DB, items []diskAlertItem) []diskAlertItem {
	out := []diskAlertItem{}
	for _, it := range items {
		var lastLevel string
		var lastAt time.Time
		err := db.QueryRow(`SELECT level, notified_at FROM disk_alert_state WHERE target=?`, it.target).Scan(&lastLevel, &lastAt)
		send := false
		switch {
		case err == sql.ErrNoRows:
			send = true
		case err != nil:
			// 状态表查不了就宁可发：漏报磁盘满的代价远大于多发一条
			logx.J("disk_watch", "state_query_fail", map[string]any{"target": it.target, "err": err.Error()})
			send = true
		case lastLevel != it.level && it.level == "critical":
			send = true // 升级为危险，立刻再报一次
		case time.Since(lastAt) >= diskAlertRepeat:
			send = true
		}
		if !send {
			continue
		}
		if _, err := db.Exec(`INSERT INTO disk_alert_state (target, level, pct, notified_at) VALUES (?,?,?,NOW())
			ON DUPLICATE KEY UPDATE level=VALUES(level), pct=VALUES(pct), notified_at=NOW()`,
			it.target, it.level, it.pct); err != nil {
			logx.J("disk_watch", "state_write_fail", map[string]any{"target": it.target, "err": err.Error()})
		}
		out = append(out, it)
	}
	return out
}

// sendDiskAlert 推飞书。走 disk_watch 任务自己配的群，和其它任务通知一致。
func sendDiskAlert(db *sql.DB, items []diskAlertItem) {
	webhook := taskWebhook(db, "disk_watch")
	if webhook == "" {
		logx.J("disk_watch", "no_webhook", map[string]any{
			"pending": len(items), "hint": "disk_watch 任务未绑定飞书群，磁盘告警发不出去",
		})
		return
	}
	// 关掉这类告警时巡检照跑（水位仍写库、看板可见），只是不投递飞书
	if !alertEnabled(db, "notify_disk_watch") {
		return
	}
	var b strings.Builder
	b.WriteString("【CMDB 磁盘水位告警】\n")
	for _, it := range items {
		dot := "🟠"
		if it.level == "critical" {
			dot = "🔴"
		}
		b.WriteString(fmt.Sprintf("%s %s —— %.0f%%（%s）\n", dot, it.target, it.pct, it.detail))
	}
	b.WriteString("\n盘满会直接打垮服务：数据库无法写 binlog/临时表，接口全部挂起。请尽快清理或扩容。")
	if err := notify.SendFeishu(webhook, b.String()+atMentionsForTask(db, "disk_watch")); err != nil {
		logx.J("disk_watch", "notify_fail", map[string]any{"err": err.Error(), "count": len(items)})
	}
}
