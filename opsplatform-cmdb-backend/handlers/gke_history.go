// GKE 升级历史与节点自动修复历史的只读接口。
//
//	GET /api/gke/upgrade/history   升级记录（区分 Google 自动升 vs 我们手动升）
//	GET /api/gke/repair-history    节点被自动 drain 重建的记录
//
// ⚠️ 关于「查不到记录」的解读（2026-07-31 实测踩到）：
// GCP 的 operations.list 保留期很短——三个 project 加起来只有 9 条 operation，
// AUTO_REPAIR_NODES 一条都没有。所以本页的空结果**不能读成「没发生过」**，
// 只能读成「GCP 已经不保留了 + 我们还没采到」。接口一律返回 coverage 元信息说明这一点，
// 前端必须显示，否则这个页面会给出错误的安全结论。
package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type GKEHistoryHandler struct{ DB *sql.DB }

func NewGKEHistoryHandler(db *sql.DB) *GKEHistoryHandler { return &GKEHistoryHandler{DB: db} }

func (h *GKEHistoryHandler) Register(r *gin.RouterGroup) {
	r.GET("/gke/upgrade/history", h.UpgradeHistory)
	r.GET("/gke/repair-history", h.RepairHistory)
}

// coverage 说明这份历史能覆盖多久，避免「空结果」被误读成「没发生过」。
func (h *GKEHistoryHandler) coverage(table string) gin.H {
	var total int
	var earliest, latest sql.NullString
	_ = h.DB.QueryRow(`SELECT COUNT(*), MIN(started_at), MAX(started_at) FROM ` + table).
		Scan(&total, &earliest, &latest)
	note := "GCP 的 operations 历史保留期很短且未文档化，实测三个 project 合计仅 9 条。" +
		"这里的记录靠定时采集往后累积，早于首次采集的历史已无法找回。"
	if total == 0 {
		// 前端是纯文本插值，不解析 markdown，所以这里不能用 ** 强调
		note = "目前一条都没有。这不代表没发生过——" + note
	}
	return gin.H{
		"total": total, "earliest": dateTimeStr(earliest), "latest": dateTimeStr(latest), "note": note,
	}
}

// UpgradeHistory 升级记录。start_type 是核心：AUTOMATIC=被 Google 自动升的，MANUAL=我们手动升的。
func (h *GKEHistoryHandler) UpgradeHistory(c *gin.Context) {
	q := `SELECT h.cluster_id, COALESCE(c.display_name, c.name, ''), h.scope, h.pool,
	             h.start_type, h.state, h.initial_version, h.target_version,
	             h.started_at, h.ended_at, h.detail, h.source
	        FROM gke_upgrade_history h
	        LEFT JOIN k8s_clusters c ON c.id = h.cluster_id
	       WHERE 1=1`
	args := []any{}
	if v := c.Query("cluster_id"); v != "" {
		q += " AND h.cluster_id=?"
		args = append(args, v)
	}
	if v := c.Query("start_type"); v != "" {
		q += " AND h.start_type=?"
		args = append(args, v)
	}
	if v := c.Query("scope"); v != "" {
		q += " AND h.scope=?"
		args = append(args, v)
	}
	q += " ORDER BY h.started_at DESC, h.id DESC LIMIT ?"
	args = append(args, limitOr(c.Query("limit"), 300))

	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	stat := map[string]int{"AUTOMATIC": 0, "MANUAL": 0, "UNKNOWN": 0, "FAILED": 0}
	for rows.Next() {
		var cid int
		var cname, scope, pool, st, state, iv, tv, detail, src string
		var startedAt, endedAt sql.NullString
		if rows.Scan(&cid, &cname, &scope, &pool, &st, &state, &iv, &tv,
			&startedAt, &endedAt, &detail, &src) != nil {
			continue
		}
		if st == "" {
			// operations 来源没有 startType，如实标 UNKNOWN 而不是假装是自动或手动
			stat["UNKNOWN"]++
		} else {
			stat[st]++
		}
		if state == "FAILED" {
			stat["FAILED"]++
		}
		out = append(out, gin.H{
			"cluster_id": cid, "cluster": cname, "scope": scope, "pool": pool,
			"start_type": st, "state": state,
			"initial_version": iv, "target_version": tv,
			"started_at": dateTimeStr(startedAt), "ended_at": dateTimeStr(endedAt),
			"duration": durationText(startedAt, endedAt),
			"detail":   detail, "source": src,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true, "rows": out, "stat": stat, "coverage": h.coverage("gke_upgrade_history"),
	})
}

// RepairHistory 节点自动修复记录。
// GKE 的 node auto-repair 默认开启，触发后 drain 并重建节点，全程静默无通知——
// 这是唯一能事后知道「哪个节点被重建过」的地方。
func (h *GKEHistoryHandler) RepairHistory(c *gin.Context) {
	q := `SELECT r.cluster_id, COALESCE(cl.display_name, cl.name, ''), r.op_name, r.pool, r.node_name,
	             r.repair_reason, r.status, r.started_at, r.ended_at, r.detail, r.status_message
	        FROM gke_repair_history r
	        LEFT JOIN k8s_clusters cl ON cl.id = r.cluster_id
	       WHERE 1=1`
	args := []any{}
	if v := c.Query("cluster_id"); v != "" {
		q += " AND r.cluster_id=?"
		args = append(args, v)
	}
	q += " ORDER BY r.started_at DESC, r.id DESC LIMIT ?"
	args = append(args, limitOr(c.Query("limit"), 300))

	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	noReason := 0
	for rows.Next() {
		var cid int
		var cname, op, pool, node, reason, status, detail, msg string
		var startedAt, endedAt sql.NullString
		if rows.Scan(&cid, &cname, &op, &pool, &node, &reason, &status,
			&startedAt, &endedAt, &detail, &msg) != nil {
			continue
		}
		if reason == "" {
			noReason++
		}
		out = append(out, gin.H{
			"cluster_id": cid, "cluster": cname, "op_name": op, "pool": pool, "node_name": node,
			"repair_reason": reason, "status": status,
			"started_at":    dateTimeStr(startedAt), "ended_at": dateTimeStr(endedAt),
			"duration": durationText(startedAt, endedAt),
			"detail":   detail, "status_message": msg,
		})
	}

	cov := h.coverage("gke_repair_history")
	// 原因解析不出来的条数要单独说：REST v1 的 Operation 没有 operationReason 字段，
	// 只能从 detail/statusMessage 文本猜，猜不出时留空——不能让人以为「没有原因」。
	cov["reason_unparsed"] = noReason
	if noReason > 0 {
		cov["reason_note"] = "有 " + strconv.Itoa(noReason) + " 条解析不出修复原因：" +
			"REST v1 的 Operation 结构没有 operationReason 字段（那是 gcloud CLI 的），" +
			"只能从 detail/statusMessage 文本提取。原文已保留在「详情」列。"
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "rows": out, "coverage": cov})
}

func limitOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 2000 {
		return n
	}
	return def
}

// parseMySQLTime 解析 driver 返回的时间串（可能是 RFC3339 也可能是 DATETIME 字面量）。
func parseMySQLTime(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// durationText 起止时间差的人话表述；缺任一端返回空。
func durationText(start, end sql.NullString) string {
	if !start.Valid || !end.Valid || start.String == "" || end.String == "" {
		return ""
	}
	s, e1 := parseMySQLTime(start.String)
	e, e2 := parseMySQLTime(end.String)
	if !e1 || !e2 || e.Before(s) {
		return ""
	}
	m := int(e.Sub(s).Minutes())
	if m < 60 {
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(m/60) + "h" + strconv.Itoa(m%60) + "m"
}
