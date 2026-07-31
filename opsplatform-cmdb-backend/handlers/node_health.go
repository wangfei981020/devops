// 节点健康预警：在 GKE 的 node auto-repair 动手之前把问题捅出来。
//
// GKE 的 auto-repair 默认开启，触发后 drain 节点并重建，drain 一小时未完成则强制关机，全程静默。
// 官方触发阈值（已查证）：
//
//	NotReady 连续不健康 / 完全不上报状态  ~10 分钟
//	boot disk 满                        ~30 分钟
//
// 所以能争取到的提前量是有上限的，这里如实按档设计，不做过度承诺：
//
//	NotReady  连续 3 分钟就告警 → 比 GKE 早 5~8 分钟，够人工 drain 保住有状态服务
//	磁盘      predict_linear 外推 24 小时内满盘 → 能提前几小时到几天（价值最大的一档）
//	突然宕机  无前兆，做不到提前发现，只能靠事后的 AUTO_REPAIR_NODES 记录
//
// ⚠️ 为什么直连集群而不读 k8s_nodes 表：那张表由 k8ssource 调度器每 120 秒刷一次
// （DefaultSyncIntervalSec），撑不起「3 分钟连续判定」的精度，读表会让预警本身迟到。
package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
	"opsplatform-cmdb-backend/notify"
)

const (
	notReadyAlertAfter = 3 * time.Minute  // 比 GKE 的 ~10 分钟阈值提前
	notReadyRepeatGap  = 30 * time.Minute // 同节点重复告警间隔（分钟级任务，不去重会炸群）
	diskRepeatGap      = 6 * time.Hour
	diskPredictWindow  = 24 * time.Hour // 预计多久内满盘就告警
	gkeRepairThreshold = 10 * time.Minute
)

// nodeHealthPool 由 StartScheduler 注入（main.go 里 Pool 的构造晚于调度器启动）。
var nodeHealthPool *k8ssource.Pool

type nodeAlert struct {
	ClusterID   int
	Cluster     string
	Provider    string
	Node        string
	Pool        string
	Kind        string // not_ready / disk_full
	Level       string // red / yellow
	Detail      string
	Suggestion  string
	NotReadyFor time.Duration
	DiskPct     float64
	DiskETA     time.Time
}

// nodeHealthWatchCore 轮询各集群节点健康，异常达阈值就发飞书。
// 单集群失败不影响其他集群。
func nodeHealthWatchCore(ctx context.Context, db *sql.DB, pool *k8ssource.Pool, cipher *crypto.Cipher, prog ProgressFn) (string, []TaskFailure, bool) {
	type clusterRow struct {
		ID       int
		Name     string
		Provider string
		Env      string
	}
	rows, err := db.Query(`SELECT id, COALESCE(NULLIF(display_name,''), name), provider, environment
	                         FROM k8s_clusters WHERE enabled=1`)
	if err != nil {
		return "查询集群失败：" + err.Error(), []TaskFailure{{Target: "db", Reason: err.Error()}}, false
	}
	clusters := []clusterRow{}
	for rows.Next() {
		var c clusterRow
		if rows.Scan(&c.ID, &c.Name, &c.Provider, &c.Env) == nil {
			clusters = append(clusters, c)
		}
	}
	rows.Close()
	if len(clusters) == 0 {
		return "没有已启用的集群", nil, true
	}

	var failures []TaskFailure
	var alerts []nodeAlert
	totalNodes, notReadyNow := 0, 0
	now := time.Now()

	for i, cl := range clusters {
		select {
		case <-ctx.Done():
			return fmt.Sprintf("已中止：完成 %d/%d 个集群", i, len(clusters)), failures, false
		default:
		}
		prog(i, len(clusters))

		cs, e := pool.ClientFor(cl.ID)
		if e != nil {
			failures = append(failures, TaskFailure{Target: cl.Name, Reason: e.Error()})
			continue
		}
		// ResourceVersion:"0" 让 apiserver 从自己的 watch cache 返回，不穿透到 etcd。
		// 代价是数据可能落后几秒——对「NotReady 连续 3 分钟」这种判定完全无所谓，
		// 换来的是 apiserver 侧开销大幅下降（kubelet 等组件的标准做法）。
		nl, e := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{ResourceVersion: "0"})
		if e != nil {
			failures = append(failures, TaskFailure{Target: cl.Name, Reason: e.Error()})
			continue
		}
		totalNodes += len(nl.Items)

		for _, n := range nl.Items {
			ready := nodeReady(&n)
			pl := nodePool(&n)
			if ready {
				clearNotReady(db, cl.ID, n.Name)
				continue
			}
			notReadyNow++
			since := markNotReady(db, cl.ID, n.Name, now)
			dur := now.Sub(since)
			if dur < notReadyAlertAfter || !shouldAlert(db, cl.ID, n.Name, now, notReadyRepeatGap) {
				continue
			}
			a := nodeAlert{
				ClusterID: cl.ID, Cluster: cl.Name, Provider: cl.Provider,
				Node: n.Name, Pool: pl, Kind: "not_ready", Level: "red",
				NotReadyFor: dur,
				Detail:      "NotReady 已持续 " + humanDur(dur) + "；" + nodeConditionSummary(&n),
			}
			a.Suggestion = notReadySuggestion(cl.Provider, dur)
			alerts = append(alerts, a)
			markAlerted(db, cl.ID, n.Name, "not_ready", "red", now)
		}

		// 磁盘趋势：只有接了 Prometheus 的集群能做（infra-01/02 目前没接，会走 skip 分支）
		da, e := diskAlerts(db, cipher, cl.ID, cl.Name, cl.Provider, cl.Env, now)
		if e != nil {
			logx.J("node_health", "disk_check_skipped", map[string]any{
				"cluster": cl.Name, "reason": e.Error(),
				"note": "该集群无 Prometheus 数据源，磁盘趋势预警不可用，仅保留 NotReady 档",
			})
		}
		alerts = append(alerts, da...)
	}
	prog(len(clusters), len(clusters))

	if len(alerts) > 0 {
		sendNodeHealthAlert(db, alerts)
	}
	summary := fmt.Sprintf("检查 %d 个集群 %d 个节点：NotReady %d 个，本轮告警 %d 条",
		len(clusters), totalNodes, notReadyNow, len(alerts))
	logx.J("node_health", "checked", map[string]any{
		"clusters": len(clusters), "nodes": totalNodes,
		"not_ready": notReadyNow, "alerts": len(alerts), "failures": len(failures),
	})
	return summary, failures, len(failures) == 0
}

func nodeReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false // 没有 Ready condition 等同于「不上报状态」，GKE 同样会触发修复
}

// nodeConditionSummary 把压力位 condition 拼成一句话——它们往往是 NotReady 的前因。
func nodeConditionSummary(n *corev1.Node) string {
	bad := []string{}
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			continue
		}
		// 除 Ready 外，这些 condition 为 True 都表示异常
		if c.Status == corev1.ConditionTrue {
			bad = append(bad, string(c.Type))
		}
	}
	if len(bad) == 0 {
		return "无其他异常 condition"
	}
	sort.Strings(bad)
	return "同时存在 " + strings.Join(bad, "/")
}

func nodePool(n *corev1.Node) string {
	for _, k := range []string{"cloud.google.com/gke-nodepool", "node.kubernetes.io/instance-type", "agentpool"} {
		if v := n.Labels[k]; v != "" && strings.Contains(k, "nodepool") {
			return v
		}
	}
	return n.Labels["cloud.google.com/gke-nodepool"]
}

// notReadySuggestion 给出可执行建议。GKE 才有 auto-repair，别对非 GKE 集群说会被自动重建。
func notReadySuggestion(provider string, dur time.Duration) string {
	if !strings.EqualFold(provider, "gke") {
		return "该集群非 GKE，无 auto-repair 托底，需人工介入排查"
	}
	left := gkeRepairThreshold - dur
	if left <= 0 {
		return "已超过 GKE auto-repair 阈值（约 10 分钟），节点可能正在被 drain 重建；" +
			"注意有 PDB 的有状态服务：drain 一小时未完成会被强制关机"
	}
	return fmt.Sprintf("预计 %s 后 GKE 触发自动修复（阈值约 10 分钟）。"+
		"建议现在手动 drain 并迁移有状态服务，别等被动重建——drain 一小时未完成会被强制关机", humanDur(left))
}

// diskAlerts 用 predict_linear 外推 boot disk 何时写满。
// 比自己存历史采样点简单也准确得多，Prometheus 原生支持。
func diskAlerts(db *sql.DB, cipher *crypto.Cipher, cid int, cname, provider, env string, now time.Time) ([]nodeAlert, error) {
	base, token, clusterLabel, err := resolveEndpointFull(db, cipher, "prometheus", env, cid)
	if err != nil {
		return nil, err
	}
	sel := `mountpoint="/",fstype!~"tmpfs|overlay"`
	if clusterLabel != "" {
		sel = clusterLabel + "," + sel
	}
	// 24 小时内 avail 会跌破 0 = 预计满盘
	q := fmt.Sprintf(`predict_linear(node_filesystem_avail_bytes{%s}[6h], %d) < 0`,
		sel, int(diskPredictWindow.Seconds()))
	samples, err := promInstant(base, token, q)
	if err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, nil
	}
	// 再取一次当前使用率，让告警带上「现在多满」这个人能判断的数字
	pctBy := map[string]float64{}
	pq := fmt.Sprintf(`100 - node_filesystem_avail_bytes{%s} / node_filesystem_size_bytes{%s} * 100`, sel, sel)
	if ps, e := promInstant(base, token, pq); e == nil {
		for _, s := range ps {
			pctBy[promNodeName(s.Metric)] = s.Value
		}
	}

	out := []nodeAlert{}
	for _, s := range samples {
		node := promNodeName(s.Metric)
		if node == "" {
			logx.J("node_health", "disk_sample_no_node_label", map[string]any{
				"cluster": cname, "labels": fmt.Sprintf("%v", s.Metric),
			})
			continue
		}
		if !shouldAlert(db, cid, node, now, diskRepeatGap) {
			continue
		}
		pct := pctBy[node]
		a := nodeAlert{
			ClusterID: cid, Cluster: cname, Provider: provider, Node: node,
			Kind: "disk_full", Level: "yellow", DiskPct: pct,
			Detail: fmt.Sprintf("boot disk 已用 %.1f%%，按当前写入速率预计 %s 内写满",
				pct, humanDur(diskPredictWindow)),
			Suggestion: "满盘约 30 分钟后 GKE 会触发自动修复重建节点。" +
				"建议先清 containerd 镜像缓存或扩盘",
		}
		out = append(out, a)
		markAlerted(db, cid, node, "disk_full", "yellow", now)
	}
	return out, nil
}

func promNodeName(m map[string]string) string {
	for _, k := range []string{"node", "instance", "nodename", "kubernetes_node"} {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// ---- 告警状态机（k8s_node_alert_state）----

// markNotReady 记录首次 NotReady 时刻，返回该节点从何时开始 NotReady。
func markNotReady(db *sql.DB, cid int, node string, now time.Time) time.Time {
	var since sql.NullString
	err := db.QueryRow(`SELECT not_ready_since FROM k8s_node_alert_state WHERE cluster_id=? AND node_name=?`,
		cid, node).Scan(&since)
	if err == nil && since.Valid && since.String != "" {
		if t, ok := parseMySQLTime(since.String); ok {
			return t
		}
	}
	if _, e := db.Exec(`INSERT INTO k8s_node_alert_state (cluster_id, node_name, not_ready_since, alert_kind, alert_level)
		VALUES (?,?,?,'not_ready','')
		ON DUPLICATE KEY UPDATE not_ready_since=IFNULL(not_ready_since, VALUES(not_ready_since))`,
		cid, node, now.Format("2006-01-02 15:04:05")); e != nil {
		logx.J("node_health", "mark_not_ready_failed", map[string]any{"node": node, "err": e.Error()})
	}
	return now
}

// clearNotReady 节点恢复后清空计时，否则下次异常会被算成「已持续很久」而误报。
func clearNotReady(db *sql.DB, cid int, node string) {
	if _, e := db.Exec(`UPDATE k8s_node_alert_state SET not_ready_since=NULL, alert_level='green'
	                     WHERE cluster_id=? AND node_name=? AND not_ready_since IS NOT NULL`, cid, node); e != nil {
		logx.J("node_health", "clear_not_ready_failed", map[string]any{"node": node, "err": e.Error()})
	}
}

// shouldAlert 距上次告警是否已超过间隔。90 秒一轮，不去重会把群刷爆。
func shouldAlert(db *sql.DB, cid int, node string, now time.Time, gap time.Duration) bool {
	var last sql.NullString
	if db.QueryRow(`SELECT last_alert_at FROM k8s_node_alert_state WHERE cluster_id=? AND node_name=?`,
		cid, node).Scan(&last) != nil {
		return true
	}
	if !last.Valid || last.String == "" {
		return true
	}
	t, ok := parseMySQLTime(last.String)
	return !ok || now.Sub(t) >= gap
}

func markAlerted(db *sql.DB, cid int, node, kind, level string, now time.Time) {
	if _, e := db.Exec(`INSERT INTO k8s_node_alert_state (cluster_id, node_name, alert_kind, alert_level, last_alert_at)
		VALUES (?,?,?,?,?)
		ON DUPLICATE KEY UPDATE alert_kind=VALUES(alert_kind), alert_level=VALUES(alert_level),
		                        last_alert_at=VALUES(last_alert_at)`,
		cid, node, kind, level, now.Format("2006-01-02 15:04:05")); e != nil {
		logx.J("node_health", "mark_alerted_failed", map[string]any{"node": node, "err": e.Error()})
	}
}

// ---- 飞书告警 ----

// sendNodeHealthAlert 节点告警是分钟级的，走自己的投递而不是任务完成通知
// （任务每 90 秒跑一次，用 sendTaskNotify 会每轮都发）。群仍由 scheduled_tasks 配置，
// 这样用户能在「定时任务」页把它和升级提醒分到两个群。
func sendNodeHealthAlert(db *sql.DB, alerts []nodeAlert) {
	webhook, group := larkWebhookForTask(db, "node_health_watch")
	if webhook == "" {
		logx.J("node_health", "alert_no_group", map[string]any{
			"alerts": len(alerts), "note": "node_health_watch 未配飞书群，告警只进日志",
		})
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🔴 节点健康预警（%d 条）\n", len(alerts))
	for _, a := range alerts {
		icon := "🔴"
		if a.Level == "yellow" {
			icon = "🟡"
		}
		fmt.Fprintf(&b, "\n%s %s / %s", icon, a.Cluster, a.Node)
		if a.Pool != "" {
			fmt.Fprintf(&b, "（%s）", a.Pool)
		}
		fmt.Fprintf(&b, "\n   %s\n   ▸ %s\n", a.Detail, a.Suggestion)
	}
	if err := notify.SendFeishu(webhook, b.String()); err != nil {
		logx.J("node_health", "alert_send_failed", map[string]any{"group": group, "err": err.Error()})
		return
	}
	logx.J("node_health", "alert_sent", map[string]any{"group": group, "alerts": len(alerts)})
}

// larkWebhookForTask 取某个定时任务绑定的飞书群 webhook。
func larkWebhookForTask(db *sql.DB, taskKey string) (webhook, group string) {
	var gid sql.NullInt64
	if db.QueryRow(`SELECT lark_group_id FROM scheduled_tasks WHERE task_key=?`, taskKey).Scan(&gid) != nil || !gid.Valid {
		return "", ""
	}
	if db.QueryRow(`SELECT name, webhook FROM lark_groups WHERE id=?`, gid.Int64).Scan(&group, &webhook) != nil {
		return "", ""
	}
	return webhook, group
}

func humanDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d 分 %d 秒", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", int(d.Hours()), int(d.Minutes())%60)
}

// ---- 查询接口（前端「节点健康与自动修复」Tab）----

// NodeHealthState 当前告警状态 + 任务运行状态。
// 任务默认是关的（见 migration 067），所以必须把「任务没开」和「一切正常」区分开——
// 否则用户会把「没开监控」误读成「没有异常」。
func (h *GKEHistoryHandler) NodeHealthState(c *gin.Context) {
	var enabled int
	var schedule, lastResult string
	var lastRun sql.NullString
	taskFound := h.DB.QueryRow(`SELECT enabled, schedule, COALESCE(last_result,''), last_run_at
	                              FROM scheduled_tasks WHERE task_key='node_health_watch'`).
		Scan(&enabled, &schedule, &lastResult, &lastRun) == nil

	rows, err := h.DB.Query(`
		SELECT s.cluster_id, COALESCE(NULLIF(cl.display_name,''), cl.name, ''), COALESCE(cl.provider,''),
		       s.node_name, s.not_ready_since, s.disk_pct, s.alert_level, s.alert_kind, s.last_alert_at
		  FROM k8s_node_alert_state s
		  LEFT JOIN k8s_clusters cl ON cl.id = s.cluster_id
		 WHERE s.not_ready_since IS NOT NULL OR s.alert_level IN ('red','yellow')
		 ORDER BY FIELD(s.alert_level,'red','yellow','green'), s.not_ready_since`)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	now := time.Now()
	out := []gin.H{}
	for rows.Next() {
		var cid int
		var cluster, provider, node, level, kind string
		var since, lastAlert sql.NullString
		var diskPct float64
		if rows.Scan(&cid, &cluster, &provider, &node, &since, &diskPct, &level, &kind, &lastAlert) != nil {
			continue
		}
		item := gin.H{
			"cluster_id": cid, "cluster": cluster, "provider": provider, "node_name": node,
			"alert_level": level, "alert_kind": kind,
			"disk_pct": diskPct, "last_alert_at": dateTimeStr(lastAlert),
			"not_ready_since": dateTimeStr(since),
		}
		// NotReady 持续时长和「距 GKE 自动修复还有多久」是这一页最该给的两个数字
		if since.Valid && since.String != "" {
			if t, ok := parseMySQLTime(since.String); ok {
				d := now.Sub(t)
				item["not_ready_seconds"] = int(d.Seconds())
				item["not_ready_text"] = humanDur(d)
				if strings.EqualFold(provider, "gke") {
					if left := gkeRepairThreshold - d; left > 0 {
						item["repair_in_text"] = humanDur(left)
					} else {
						item["repair_in_text"] = "已超阈值"
					}
				}
			}
		}
		out = append(out, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true, "rows": out,
		"task": gin.H{
			"found": taskFound, "enabled": enabled == 1, "schedule": schedule,
			"last_run_at": dateTimeStr(lastRun), "last_result": lastResult,
			"note": nodeHealthTaskNote(taskFound, enabled == 1),
		},
		"thresholds": gin.H{
			"not_ready_alert_after": "3 分钟",
			"gke_repair_threshold":  "约 10 分钟",
			"disk_predict_window":   "24 小时内满盘",
			"note": "NotReady 只能比 GKE 的自动修复早 5~8 分钟；磁盘趋势能提前几小时到几天；" +
				"突然宕机无前兆，做不到提前发现，只能靠事后的自动修复记录。",
		},
	})
}

func nodeHealthTaskNote(found, enabled bool) string {
	if !found {
		return "未找到 node_health_watch 任务，检查迁移 067 是否已执行"
	}
	if !enabled {
		return "任务当前是关闭的——这里的「无异常」不代表节点健康，只代表没在监控。" +
			"去「系统管理 → 定时任务」打开 node_health_watch，并给它配一个独立的告警群。"
	}
	return ""
}
