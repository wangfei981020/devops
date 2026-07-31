// GKE 版本与升级看板的只读接口。
//
// 两个页面：
//
//	GET /api/gke/upgrade/overview   升级看板（集群 + 节点池 + 汇总）
//	GET /api/gke/version-schedule   官网版本排期表（含「哪个集群锚在哪一格」）
//
// 手工覆盖排期（官网页面改版导致解析错时的兜底）：
//
//	PUT    /api/gke/version-schedule/:id
//	DELETE /api/gke/version-schedule/:id/override
package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

type GKEUpgradeHandler struct{ DB *sql.DB }

func NewGKEUpgradeHandler(db *sql.DB) *GKEUpgradeHandler { return &GKEUpgradeHandler{DB: db} }

func (h *GKEUpgradeHandler) Register(r *gin.RouterGroup) {
	r.GET("/gke/upgrade/overview", h.Overview)
	r.GET("/gke/version-schedule", h.Schedule)
	r.PUT("/gke/version-schedule/:id", h.OverrideSchedule)
	r.DELETE("/gke/version-schedule/:id/override", h.ClearOverride)
}

type gkePoolOut struct {
	Name                 string `json:"name"`
	NodeCount            int    `json:"node_count"`
	Version              string `json:"version"`
	Status               string `json:"status"`
	AutoUpgrade          bool   `json:"auto_upgrade"`
	AutoRepair           bool   `json:"auto_repair"`
	AutoUpgradeStartTime string `json:"auto_upgrade_start_time"`
	MaxSurge             int    `json:"max_surge"`
	MaxUnavailable       int    `json:"max_unavailable"`
	Strategy             string `json:"strategy"`
	BGPhase              string `json:"bg_phase"`
	UpgradeRisk          string `json:"upgrade_risk"`
	RiskNote             string `json:"risk_note"`
	PausedReason         string `json:"paused_reason"`
	MinorTargetVersion   string `json:"minor_target_version"`
	VersionSkew          string `json:"version_skew"` // 与控制面版本的偏斜说明，空=一致
}

type gkeClusterOut struct {
	ClusterID      int    `json:"cluster_id"`
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	Environment    string `json:"environment"`
	Project        string `json:"project_id"`
	Location       string `json:"location"`
	Synced         bool   `json:"synced"` // false=从没采集成功过，前端显示「未采集」而不是空白
	LastError      string `json:"last_error"`
	SyncedAt       string `json:"synced_at"`
	ReleaseChannel string `json:"release_channel"`
	MasterVersion  string `json:"current_master_version"`
	MinorTarget    string `json:"minor_target_version"`
	PatchTarget    string `json:"patch_target_version"`

	PredictedAt        string `json:"predicted_upgrade_at"`
	PredictedPrecision string `json:"predicted_precision"`
	PredictedSource    string `json:"predicted_source"`
	// DaysLeft 只在 day 粒度下给数字。月/季度粒度给单一数字会造成虚假紧迫感（见 dateWindow 注释），
	// 那时改用 WindowText + DaysMin/DaysMax 表达。
	DaysLeft   *int   `json:"days_left"`
	WindowText string `json:"predicted_window_text"`
	DaysMin    *int   `json:"days_min"` // 区间最早还有几天（阶段 4 提醒用它做保守触发）
	DaysMax    *int   `json:"days_max"`

	AutoUpgradeStatus string `json:"auto_upgrade_status"`
	PausedReason      string `json:"paused_reason"`
	Blocked           bool   `json:"blocked"`    // 仅指「用户主动设的维护排除」挡住，不含常态节流
	PauseKind         string `json:"pause_kind"` // excluded / throttled / ""
	PauseNote         string `json:"pause_note"` // 人话解释，前端直接显示

	EOSStandardAt string `json:"eos_standard_at"`
	EOSDaysLeft   *int   `json:"eos_days_left"`
	EOSExtendedAt string `json:"eos_extended_at"`

	MaintenancePolicy string       `json:"maintenance_policy_json"`
	Pools             []gkePoolOut `json:"pools"`
	Verdict           string       `json:"verdict"` // 一句话判断，不是纯数据
}

// Overview 升级看板。
// 用 LEFT JOIN：没采集过的集群也要列出来（显示「未采集」），否则用户会以为集群丢了。
func (h *GKEUpgradeHandler) Overview(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT c.id, c.name, c.display_name, c.environment, c.project_id, c.location,
		       COALESCE(u.release_channel,''), COALESCE(u.current_master_version,''),
		       COALESCE(u.minor_target_version,''), COALESCE(u.patch_target_version,''),
		       u.predicted_upgrade_at, COALESCE(u.predicted_precision,''), COALESCE(u.predicted_source,''),
		       COALESCE(u.auto_upgrade_status,''), COALESCE(u.paused_reason,''),
		       u.eos_standard_at, u.eos_extended_at,
		       COALESCE(u.maintenance_policy_json,''), COALESCE(u.last_error,''),
		       u.synced_at
		  FROM k8s_clusters c
		  LEFT JOIN gke_cluster_upgrade u ON u.cluster_id = c.id
		 WHERE c.provider='gke' AND c.enabled=1
		 ORDER BY FIELD(c.environment,'PROD','UAT','TEST','DEV'), c.name`)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	today := localMidnight()
	out := []gkeClusterOut{}
	for rows.Next() {
		var x gkeClusterOut
		var predAt, eosStd, eosExt, syncedAt sql.NullString
		if e := rows.Scan(&x.ClusterID, &x.Name, &x.DisplayName, &x.Environment, &x.Project, &x.Location,
			&x.ReleaseChannel, &x.MasterVersion, &x.MinorTarget, &x.PatchTarget,
			&predAt, &x.PredictedPrecision, &x.PredictedSource,
			&x.AutoUpgradeStatus, &x.PausedReason, &eosStd, &eosExt,
			&x.MaintenancePolicy, &x.LastError, &syncedAt); e != nil {
			continue
		}
		x.PredictedAt = dateStr(predAt)
		x.EOSStandardAt, x.EOSExtendedAt = dateStr(eosStd), dateStr(eosExt)
		x.SyncedAt = dateTimeStr(syncedAt)
		x.Synced = x.SyncedAt != "" && x.MasterVersion != ""
		start, end := dateWindow(x.PredictedAt, x.PredictedPrecision)
		x.DaysMin, x.DaysMax = daysUntil(start, today), daysUntil(end, today)
		x.WindowText = windowText(x.PredictedAt, x.PredictedPrecision)
		if x.PredictedPrecision == "day" {
			x.DaysLeft = x.DaysMin
		}
		x.EOSDaysLeft = daysUntil(x.EOSStandardAt, today)
		x.PauseKind, x.PauseNote = classifyPause(x.PausedReason)
		x.Blocked = x.PauseKind == "excluded"
		out = append(out, x)
	}

	// 节点池按集群装配
	poolsBy := map[int][]gkePoolOut{}
	pr, err := h.DB.Query(`
		SELECT cluster_id, name, node_count, version, status, auto_upgrade, auto_repair,
		       auto_upgrade_start_time, max_surge, max_unavailable, strategy, bg_phase,
		       upgrade_risk, paused_reason, minor_target_version
		  FROM gke_node_pools ORDER BY cluster_id, name`)
	if err == nil {
		defer pr.Close()
		for pr.Next() {
			var cid int
			var p gkePoolOut
			var au, ar int
			var startTime sql.NullString
			if pr.Scan(&cid, &p.Name, &p.NodeCount, &p.Version, &p.Status, &au, &ar,
				&startTime, &p.MaxSurge, &p.MaxUnavailable, &p.Strategy, &p.BGPhase,
				&p.UpgradeRisk, &p.PausedReason, &p.MinorTargetVersion) != nil {
				continue
			}
			p.AutoUpgrade, p.AutoRepair = au == 1, ar == 1
			p.AutoUpgradeStartTime = dateTimeStr(startTime)
			p.RiskNote = riskNote(p)
			poolsBy[cid] = append(poolsBy[cid], p)
		}
	}

	due30, blocked, urgent := 0, 0, ""
	minDays := 1 << 30
	for i := range out {
		x := &out[i]
		x.Pools = poolsBy[x.ClusterID]
		for j := range x.Pools {
			x.Pools[j].VersionSkew = versionSkew(x.MasterVersion, x.Pools[j].Version)
		}
		x.Verdict = clusterVerdict(x)
		// 用区间「最早」那端做 30 天判定：宁可早提醒，也不能因为月/季度粒度而漏掉。
		// 但展示层不会把这个数字当成确定倒计时（见 WindowText）。
		if x.DaysMin != nil && *x.DaysMin >= 0 && *x.DaysMin <= 30 {
			due30++
			if *x.DaysMin < minDays {
				minDays, urgent = *x.DaysMin, x.Name
			}
		}
		if x.Blocked {
			blocked++
		}
	}

	// 排期表同步状态：解析失败时前端要标黄，所以把行数和最后同步时间一起给出去
	var schedRows, schedApprox int
	var schedSynced sql.NullString
	_ = h.DB.QueryRow(`SELECT COUNT(*), MAX(synced_at),
	       SUM(auto_upgrade_precision IN ('month','quarter'))
	    FROM gke_version_schedule`).Scan(&schedRows, &schedSynced, &schedApprox)

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"summary": gin.H{
			"clusters": len(out), "due_30d": due30, "blocked": blocked,
			"most_urgent": urgent, "most_urgent_days": ternInt(urgent != "", minDays, -1),
			"schedule_rows": schedRows, "schedule_approx": schedApprox,
			"schedule_synced_at": dateTimeStr(schedSynced),
		},
		"clusters": out,
	})
}

// Schedule 官网排期表 + 标注哪些集群锚在哪一格。
func (h *GKEUpgradeHandler) Schedule(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT id, minor_version, channel,
		       available_raw, available_precision,
		       auto_upgrade_raw, auto_upgrade_at, auto_upgrade_precision,
		       eos_standard_raw, eos_standard_at, eos_standard_precision,
		       eos_extended_raw, eos_extended_precision, is_manual, synced_at
		  FROM gke_version_schedule
		 ORDER BY CAST(SUBSTRING_INDEX(minor_version,'.',-1) AS UNSIGNED),
		          FIELD(channel,'RAPID','REGULAR','STABLE','EXTENDED')`)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	defer rows.Close()

	// 每个集群锚在 (目标小版本, 通道) 这一格上
	anchors := map[string][]string{}
	ar, e := h.DB.Query(`
		SELECT c.name, COALESCE(u.release_channel,''), COALESCE(u.minor_target_version,'')
		  FROM k8s_clusters c JOIN gke_cluster_upgrade u ON u.cluster_id=c.id
		 WHERE c.provider='gke' AND c.enabled=1`)
	if e == nil {
		defer ar.Close()
		for ar.Next() {
			var name, ch, target string
			if ar.Scan(&name, &ch, &target) != nil || target == "" {
				continue
			}
			ch = strings.ToUpper(ch)
			if ch == "" || ch == "UNSPECIFIED" {
				ch = "STABLE" // 未入通道按官方规则看 Stable 列
			}
			key := minorOf(target) + "|" + ch
			anchors[key] = append(anchors[key], name)
		}
	}

	today := localMidnight()
	out := []gin.H{}
	for rows.Next() {
		var id, isManual int
		var ver, ch, avRaw, avPrec, auRaw, auPrec, esRaw, esPrec, eeRaw, eePrec string
		var auAt, esAt, synced sql.NullString
		if rows.Scan(&id, &ver, &ch, &avRaw, &avPrec, &auRaw, &auAt, &auPrec,
			&esRaw, &esAt, &esPrec, &eeRaw, &eePrec, &isManual, &synced) != nil {
			continue
		}
		// 月/季度粒度只给区间和文案，不给单一天数——否则 `2026-08` 在 7/31 会显示成「1 天」
		auStart, auEnd := dateWindow(dateStr(auAt), auPrec)
		esStart, esEnd := dateWindow(dateStr(esAt), esPrec)
		row := gin.H{
			"id": id, "minor_version": ver, "channel": ch,
			"available_raw": avRaw, "available_precision": avPrec,
			"auto_upgrade_raw": auRaw, "auto_upgrade_at": dateStr(auAt), "auto_upgrade_precision": auPrec,
			"auto_upgrade_window":   windowText(dateStr(auAt), auPrec),
			"auto_upgrade_days":     nil,
			"auto_upgrade_days_min": daysUntil(auStart, today),
			"auto_upgrade_days_max": daysUntil(auEnd, today),
			"eos_standard_raw":      esRaw, "eos_standard_at": dateStr(esAt), "eos_standard_precision": esPrec,
			"eos_standard_window":   windowText(dateStr(esAt), esPrec),
			"eos_standard_days":     nil,
			"eos_standard_days_min": daysUntil(esStart, today),
			"eos_standard_days_max": daysUntil(esEnd, today),
			"eos_extended_raw":      eeRaw, "eos_extended_precision": eePrec,
			"is_manual": isManual == 1, "synced_at": dateTimeStr(synced),
			"anchored_clusters": anchors[ver+"|"+ch],
		}
		if auPrec == "day" {
			row["auto_upgrade_days"] = daysUntil(auStart, today)
		}
		if esPrec == "day" {
			row["eos_standard_days"] = daysUntil(esStart, today)
		}
		out = append(out, row)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "rows": out})
}

// OverrideSchedule 手工覆盖某一格的自动升级日期（官网解析出错时的兜底）。
// 打上 is_manual=1 后，定时同步不再冲掉这一行。
func (h *GKEUpgradeHandler) OverrideSchedule(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		AutoUpgradeRaw string `json:"auto_upgrade_raw"`
		AutoUpgradeAt  string `json:"auto_upgrade_at"`
		Precision      string `json:"auto_upgrade_precision"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "参数错误"})
		return
	}
	if in.Precision == "" {
		in.Precision = "day"
	}
	if _, err := h.DB.Exec(`UPDATE gke_version_schedule
		   SET auto_upgrade_raw=?, auto_upgrade_at=?, auto_upgrade_precision=?, is_manual=1
		 WHERE id=?`, in.AutoUpgradeRaw, nullDate(in.AutoUpgradeAt), in.Precision, id); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	logx.J("gke_schedule", "manual_override", map[string]any{
		"id": id, "raw": in.AutoUpgradeRaw, "at": in.AutoUpgradeAt, "precision": in.Precision,
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ClearOverride 取消手工覆盖，下次同步会用官网值覆盖回来。
func (h *GKEUpgradeHandler) ClearOverride(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := h.DB.Exec(`UPDATE gke_version_schedule SET is_manual=0 WHERE id=?`, id); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	logx.J("gke_schedule", "manual_override_cleared", map[string]any{"id": id})
	c.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已取消覆盖，下次同步会用官网值"})
}

// ---------------------------------------------------------------------------
// 判断逻辑：看板上每块底下那句话，不是纯数据
// ---------------------------------------------------------------------------

// classifyPause 把 pausedReason 分成运维含义完全不同的两类。
//
// ⚠️ 这里踩过坑：实测 4 个生产集群的 pausedReason 全是 MAINTENANCE_WINDOW，
// 早期代码把「有 pausedReason」一律当成「已挡住」，结果 4 个集群全显示绿色「已挡住」——
// 这是虚假的安全感。官方定义 MAINTENANCE_WINDOW 是 "outside customer maintenance window"，
// 即「当前不在维护窗口内」，是**常态节流**，一到窗口照样自动升级。
// 只有 MAINTENANCE_EXCLUSION_*（用户主动设的维护排除）才是真正挡住了升级。
func classifyPause(reason string) (kind, note string) {
	r := strings.TrimSpace(reason)
	if r == "" {
		return "", ""
	}
	switch {
	case strings.Contains(r, "MAINTENANCE_EXCLUSION_NO_MINOR_UPGRADES"):
		return "excluded", "已设维护排除（禁小版本升级），升级被真正挡住"
	case strings.Contains(r, "MAINTENANCE_EXCLUSION_NO_UPGRADES"):
		return "excluded", "已设维护排除（禁所有升级），升级被真正挡住"
	case strings.Contains(r, "MAINTENANCE_WINDOW"):
		return "throttled", "当前不在维护窗口内（常态，到窗口就会自动升级，不等于被挡住）"
	case strings.Contains(r, "CLUSTER_DISRUPTION_BUDGET"):
		return "throttled", "超出集群中断预算，暂缓升级（预算恢复后继续）"
	case strings.Contains(r, "SYSTEM_CONFIG"):
		return "throttled", "GKE 系统配置原因暂缓升级"
	}
	// 未登记的枚举必须让人看见，否则新枚举会被静默当成「不重要」
	logx.J("gke_upgrade", "unknown_paused_reason", map[string]any{"reason": r})
	return "throttled", "未识别的暂停原因：" + r
}

func clusterVerdict(x *gkeClusterOut) string {
	if !x.Synced {
		if x.LastError != "" {
			return "采集失败：" + x.LastError
		}
		return "尚未采集。需要为该集群所属云账号项目配置 SA key，然后运行「GKE 集群升级信息采集」任务"
	}
	// 硬期限优先：支持结束比自动升级更要命
	if x.EOSDaysLeft != nil {
		switch {
		case *x.EOSDaysLeft < 0:
			return "当前小版本标准支持已于 " + x.EOSStandardAt + " 结束，不再有安全补丁，应尽快升级"
		case *x.EOSDaysLeft <= 30:
			return "当前小版本标准支持将在 " + strconv.Itoa(*x.EOSDaysLeft) + " 天后（" + x.EOSStandardAt + "）结束，这是硬期限"
		}
	}
	if x.Blocked {
		return "升级已被维护排除挡住（" + x.PausedReason + "），可按计划安排手动升级；注意排除期结束后会恢复自动升级"
	}
	// 不确定性有两层，都必须在措辞里说出来，否则用户会把估算值当确定日期：
	//   ① 版本不确定：GKE 尚未排期，目标版本是按「当前小版本 +1」推断的
	//   ② 日期不确定：官网只给到月/季度粒度，我们归一化到了首日
	qualifiers := []string{}
	if x.PredictedSource == "inferred_next_minor" {
		qualifiers = append(qualifiers, "GKE 尚未排期，目标版本按当前版本的下一个小版本推断")
	}
	if x.PredictedPrecision == "month" || x.PredictedPrecision == "quarter" {
		qualifiers = append(qualifiers, "官网只给到"+precisionCN(x.PredictedPrecision)+"粒度，实际日期在"+x.WindowText)
	}
	guess := ""
	if len(qualifiers) > 0 {
		guess = "（" + strings.Join(qualifiers, "；") + "）"
	}
	// 判定用区间最早端（保守），但只有 day 粒度才把数字说成确定的倒计时
	if d := x.DaysMin; d != nil {
		when := strconv.Itoa(*d) + " 天后"
		if x.PredictedPrecision != "day" {
			when = "最早 " + strconv.Itoa(*d) + " 天后"
		}
		switch {
		case *d < 0:
			return "官网自动升级日期已过但仍未升级，需查明原因（rollout 未到 / 升级失败 / 通道配置）" + guess
		case *d <= 7:
			return "距自动升级" + when + "，且未设维护排除，届时会自动升级" + guess
		case *d <= 30:
			if x.Environment == "PROD" {
				return "生产集群且未设维护排除，" + when + "会被自动升级，建议先挡住再计划内升级" + guess
			}
			return when + "会自动升级" + guess
		}
	}
	if x.PredictedAt == "" {
		if x.MinorTarget == "" {
			return "GKE 尚未为该集群排期小版本升级（minorTargetVersion 为空），且排期表里查不到当前版本的下一个小版本"
		}
		return "排期未知：官网排期表里没有目标版本对应的行，先运行「GKE 官网版本排期同步」"
	}
	// 有日期但超过 30 天：常态，节流原因作为补充信息给出去
	if x.PauseKind == "throttled" {
		return x.PauseNote
	}
	return ""
}

func riskNote(p gkePoolOut) string {
	if p.UpgradeRisk == "green" || p.NodeCount <= 0 || p.MaxUnavailable <= 0 {
		return ""
	}
	pct := float64(p.MaxUnavailable) * 100 / float64(p.NodeCount)
	return "升级时同时不可用 " + strconv.Itoa(p.MaxUnavailable) + "/" + strconv.Itoa(p.NodeCount) +
		" 个节点（" + strconv.FormatFloat(pct, 'f', 1, 64) + "%）"
}

// versionSkew 比较节点池版本与控制面版本。GKE 有版本偏斜限制，
// 节点落后控制面超过 2 个小版本会出故障，所以小版本差异必须显著提示。
func versionSkew(master, pool string) string {
	m, p := minorOf(strings.TrimPrefix(master, "v")), minorOf(strings.TrimPrefix(pool, "v"))
	if m == "" || p == "" || m == p {
		// 小版本相同再比补丁
		if master != "" && pool != "" && master != pool {
			return "补丁版本与控制面不一致"
		}
		return ""
	}
	return "落后控制面一个小版本以上（控制面 " + m + "，节点池 " + p + "）"
}

// ---------------------------------------------------------------------------

func dateStr(v sql.NullString) string {
	if !v.Valid || v.String == "" {
		return ""
	}
	if len(v.String) >= 10 {
		return v.String[:10]
	}
	return v.String
}

// dateTimeStr 统一成 "2006-01-02 15:04:05"。
// driver 可能回 RFC3339（带 T 和 +08:00），直接透给前端会显示成 2026-07-31T08:26:29+08:00 这种原始串。
func dateTimeStr(v sql.NullString) string {
	if !v.Valid || v.String == "" {
		return ""
	}
	if t, ok := parseMySQLTime(v.String); ok {
		return t.Format("2006-01-02 15:04:05")
	}
	return v.String
}

// dateWindow 按粒度把「归一化到首日的日期」还原成它真正代表的区间 [start, end]。
//
// ⚠️ 这是必须的，不能只用首日。官网对月/季度粒度只承诺「2026 年 8 月」这种范围，
// 我们为了排序把它归一化成 2026-08-01；若直接拿首日算倒计时，会系统性提前：
// 月粒度最多误报 30 天，季度粒度（2026-Q4 → 2026-10-01，实际可能 12-31）最多 89 天。
// 实测就出现过 `1.32 EXTENDED Auto Upgrade: 2026-08 · 1 天` 这种虚假紧迫感。
func dateWindow(date, precision string) (start, end string) {
	if date == "" {
		return "", ""
	}
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return date, date
	}
	switch precision {
	case "month":
		return date, t.AddDate(0, 1, -1).Format("2006-01-02")
	case "quarter":
		return date, t.AddDate(0, 3, -1).Format("2006-01-02")
	}
	return date, date // day 粒度：区间退化成一个点
}

func precisionCN(p string) string {
	switch p {
	case "month":
		return "月"
	case "quarter":
		return "季度"
	case "day":
		return "日"
	}
	return p
}

// windowText 区间的人话表述。非 day 粒度绝不能只给一个数字。
func windowText(date, precision string) string {
	if date == "" {
		return ""
	}
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return date
	}
	switch precision {
	case "month":
		return t.Format("2006 年 1 月内")
	case "quarter":
		return fmt.Sprintf("%d 年第 %d 季度内", t.Year(), (int(t.Month())-1)/3+1)
	}
	return date
}

// localMidnight 本地时区的今天零点。
// ⚠️ 不能用 time.Now().Truncate(24*time.Hour)——Truncate 是按 UTC 对齐的，
// 在 +08:00 下会截到本地 08:00，导致所有倒计时少算一天（T-30 提醒会晚一天触发）。
func localMidnight() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.Local)
}

// daysUntil 距离目标日期还有几天（负数=已过）。空日期返回 nil，前端显示「未知」而不是 0。
func daysUntil(date string, today time.Time) *int {
	if date == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return nil
	}
	d := int(t.Sub(today).Hours() / 24)
	return &d
}

func ternInt(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}
