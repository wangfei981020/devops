package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/notify"
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
	target string // 形如 "cesar/opsplatform-mysql-0-data" 或 "node/gke-xxx"
	kind   string // pvc / node
	level  string // warning / critical
	pct    float64
	detail string
}

// diskWatchCore 巡检所有启用集群的磁盘水位。
// 返回的 summary 会写进任务历史；真正的告警走飞书，独立于任务通知。
func diskWatchCore(ctx context.Context, db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	rows, err := db.Query(`SELECT id, COALESCE(display_name,name) FROM k8s_clusters WHERE enabled=1`)
	if err != nil {
		return "查集群列表失败: " + err.Error(), nil, false
	}
	type cl struct {
		id   int
		name string
	}
	clusters := []cl{}
	for rows.Next() {
		var c cl
		if rows.Scan(&c.id, &c.name) == nil {
			clusters = append(clusters, c)
		}
	}
	rows.Close()

	all := []diskAlertItem{}
	failures := []TaskFailure{}
	checked := 0
	for _, c := range clusters {
		select {
		case <-ctx.Done():
			return fmt.Sprintf("已取消（已检查 %d/%d 个集群）", checked, len(clusters)), failures, false
		default:
		}
		items, err := diskWatchCluster(db, cipher, c.id)
		if err != nil {
			// 取不到数据要显式记失败，不能当成"这个集群没问题"——
			// 那正是 CMDB-013 里被点名的那种假阴性。
			failures = append(failures, TaskFailure{Target: c.name, Reason: err.Error()})
			logx.J("disk_watch", "cluster_fail", map[string]any{"cluster_id": c.id, "cluster": c.name, "err": err.Error()})
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

	crit, warn := 0, 0
	for _, a := range all {
		if a.level == "critical" {
			crit++
		} else {
			warn++
		}
	}
	summary := fmt.Sprintf("检查 %d 个集群：危险(≥%.0f%%) %d 项、偏高(≥%.0f%%) %d 项",
		checked, pvcCriticalPct, crit, pvcWarnPct, warn)
	if len(fresh) > 0 {
		summary += fmt.Sprintf("，本次推送 %d 条", len(fresh))
	}
	if len(failures) > 0 {
		summary += fmt.Sprintf("；%d 个集群未取到水位数据", len(failures))
	}
	logx.J("disk_watch", "done", map[string]any{
		"clusters": checked, "critical": crit, "warning": warn,
		"notified": len(fresh), "failed_clusters": len(failures),
	})
	return summary, failures, len(failures) == 0
}

// diskWatchCluster 取单个集群的 PVC + 节点磁盘水位，返回超阈值的对象。
func diskWatchCluster(db *sql.DB, cipher *crypto.Cipher, cid int) ([]diskAlertItem, error) {
	obs := NewObsQueryHandler(db, cipher)
	base, token, clusterLabel, err := resolveEndpointFull(db, cipher, "prometheus", obs.clusterEnv(cid), cid)
	if err != nil {
		return nil, fmt.Errorf("未配置可用的 Prometheus 数据源: %w", err)
	}
	sel := clusterSelector(db, clusterLabel, cid)
	out := []diskAlertItem{}

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

	// 2) 节点根分区
	nlbl := promLabels(sel, `mountpoint="/"`, `fstype!~"tmpfs|overlay|squashfs|iso9660"`)
	if rs, err := promInstant(base, token,
		`(1 - sum by(node,instance)(node_filesystem_avail_bytes`+nlbl+`) / sum by(node,instance)(node_filesystem_size_bytes`+nlbl+`)) * 100`); err == nil {
		for _, s := range rs {
			name := s.Metric["node"]
			if name == "" {
				name = s.Metric["instance"]
			}
			if name == "" {
				continue
			}
			if lv := diskLevel(s.Value); lv != "" {
				out = append(out, diskAlertItem{
					target: "节点 " + name, kind: "node", level: lv, pct: s.Value,
					detail: fmt.Sprintf("根分区 %.0f%%", s.Value),
				})
			}
		}
	}
	return out, nil
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
