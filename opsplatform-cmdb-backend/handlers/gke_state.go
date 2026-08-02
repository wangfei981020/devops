// GKE 集群升级状态的**唯一**计算来源。
//
// 为什么单独抽一层：看板和提醒曾经各写各的 EOS 逻辑——看板取「控制面与所有节点池最早」，
// 提醒只 JOIN gke_cluster_upgrade 压根没碰 gke_node_pools。结果 g32 三天后 35 个节点进 EOS，
// 看板是红的，飞书一条都不发。「提前发现」如果要人主动点开看板才行，提醒任务就没有存在意义。
// 同一语义分叉两次已经出过事，这里收敛成一处，两边都必须调这里。
//
// 三条互相独立的升级时间线（官方规则已查证）：
//
//	控制面自动升级 —— "control planes are upgraded on a regular basis, and cannot be disabled"
//	                  关不掉，只能用维护排除最多延 180 天
//	节点池自动升级 —— 控制面升完后 "typically a few days"；autoUpgrade 可以关
//	强制升级(EOS) —— "Regardless of your cluster's settings, GKE performs automatic upgrades
//	                  at the end of support"，关了 autoUpgrade 也拦不住，完全不可阻止
//
// 另有硬限制：节点 "no more than two minor versions behind the control plane"，
// 触及即出兼容问题，所以偏斜要单独判。
package handlers

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// poolState 节点池的升级相关状态。
type poolState struct {
	Name           string
	NodeCount      int
	Version        string
	AutoUpgrade    bool
	AutoRepair     bool
	EOSStandardAt  string
	EOSExtendedAt  string
	EffectiveEOSAt string // 按集群通道选出的硬期限（EXTENDED 取 extended，其余取 standard）
	EOSDaysLeft    *int
	SkewMinors     int  // 落后控制面几个小版本
	Stranded       bool // 落后且 autoUpgrade=false：平时不会自己升（但 EOS 时仍会被强制升）
	RepairOff      bool // autoRepair=false：节点故障时不会被自动重建，需人工介入
	PausedReason   string
	MinorTarget    string
	UpgradeRisk    string
	MaxUnavailable int
	MaxSurge       int
	Strategy       string
	BGPhase        string

	// BLUE_GREEN 批次与观察期参数（migration 070）。指针区分「API 没给」与「配成 0」——
	// 观察期常常是 BLUE_GREEN 里最长的一段，当成 0 会让耗时预估严重偏小。
	BGRolloutPolicy   string
	BGBatchNodeCount  *int
	BGBatchPercentage *float64
	BGBatchSoakSec    *int
	BGNodePoolSoakSec *int

	StartTime string
	Status    string
	riskNote  string
}

// clusterUpgradeState 一个集群的完整升级状态，看板/提醒/MCP 共用。
type clusterUpgradeState struct {
	ClusterID   int
	Name        string
	DisplayName string
	Env         string
	Provider    string
	Project     string
	Location    string

	ReleaseChannel string
	MasterVersion  string
	MinorTarget    string
	PatchTarget    string
	InferredTarget string

	PredictedAt        string
	PredictedPrecision string
	PredictedSource    string
	WindowText         string
	DaysMin, DaysMax   *int
	DaysLeft           *int // 仅 day 粒度

	AutoUpgradeStatus string
	PausedReason      string
	PauseKind         string
	PauseNote         string
	Blocked           bool

	ControlPlaneEOS     string // 控制面自己的支持截止
	ControlPlaneEOSDays *int
	EffectiveEOS        string // 控制面与所有节点池取最早 —— 这才是真实硬期限
	EffectiveEOSDays    *int
	EffectiveEOSSource  string
	EOSExtendedAt       string
	EOSBasis            string // 「标准支持」/「扩展支持」——有效期限是按哪个算的

	MaintenancePolicyJSON string
	Pools                 []poolState
	// 偏斜分「事实」与「预测」两层，别再混成一句（CMDB-024）：
	// 控制面升完、节点池没跟上的那段窗口里，偏斜**当下**就已经踩在硬限制上，
	// 而旧写法只说「再升一次会落后 3 个」，看的人会以为是以后的事。
	SkewCurrent  int    // 当前已落后几个小版本 —— 用 MasterVersion 算，是事实
	SkewPool     string // 落后最多的那个池
	SkewPoolVer  string // 该池的版本
	SkewCritical bool   // 当前已落后 >=2，或目标版本会让它落后 >=2
	SkewNote     string // 事实句（当前已落后…）
	SkewForecast string // 预测句（若升到 X 将落后…）

	LastError string
	SyncedAt  string
	Synced    bool
}

// loadClusterUpgradeStates 读取全部启用 GKE 集群的完整状态。看板与提醒都从这里取，不许各查各的。
func loadClusterUpgradeStates(db *sql.DB) ([]clusterUpgradeState, error) {
	today := localMidnight()
	rows, err := db.Query(`
		SELECT c.id, c.name, COALESCE(NULLIF(c.display_name,''), c.name), c.environment, c.provider,
		       c.project_id, c.location,
		       COALESCE(u.release_channel,''), COALESCE(u.current_master_version,''),
		       COALESCE(u.minor_target_version,''), COALESCE(u.patch_target_version,''),
		       u.predicted_upgrade_at, COALESCE(u.predicted_precision,''), COALESCE(u.predicted_source,''),
		       COALESCE(u.auto_upgrade_status,''), COALESCE(u.paused_reason,''),
		       u.eos_standard_at, u.eos_extended_at,
		       COALESCE(u.maintenance_policy_json,''), COALESCE(u.last_error,''), u.synced_at
		  FROM k8s_clusters c
		  LEFT JOIN gke_cluster_upgrade u ON u.cluster_id = c.id
		 WHERE c.provider='gke' AND c.enabled=1
		 ORDER BY FIELD(c.environment,'PROD','UAT','TEST','DEV'), c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []clusterUpgradeState{}
	for rows.Next() {
		var x clusterUpgradeState
		var predAt, eosStd, eosExt, syncedAt sql.NullString
		if rows.Scan(&x.ClusterID, &x.Name, &x.DisplayName, &x.Env, &x.Provider,
			&x.Project, &x.Location,
			&x.ReleaseChannel, &x.MasterVersion, &x.MinorTarget, &x.PatchTarget,
			&predAt, &x.PredictedPrecision, &x.PredictedSource,
			&x.AutoUpgradeStatus, &x.PausedReason, &eosStd, &eosExt,
			&x.MaintenancePolicyJSON, &x.LastError, &syncedAt) != nil {
			continue
		}
		x.PredictedAt = dateStr(predAt)
		x.ControlPlaneEOS, x.EOSExtendedAt = dateStr(eosStd), dateStr(eosExt)
		x.SyncedAt = dateTimeStr(syncedAt)
		x.Synced = x.SyncedAt != "" && x.MasterVersion != ""
		if x.MinorTarget == "" && x.PredictedSource == "inferred_next_minor" {
			x.InferredTarget = nextMinor(x.MasterVersion)
		}
		start, end := dateWindow(x.PredictedAt, x.PredictedPrecision)
		x.DaysMin, x.DaysMax = daysUntil(start, today), daysUntil(end, today)
		x.WindowText = windowText(x.PredictedAt, x.PredictedPrecision)
		if x.PredictedPrecision == "day" {
			x.DaysLeft = x.DaysMin
		}
		x.ControlPlaneEOSDays = daysUntil(x.ControlPlaneEOS, today)
		x.PauseKind, x.PauseNote = classifyPause(x.PausedReason)
		x.Blocked = x.PauseKind == "excluded"
		out = append(out, x)
	}

	pools, err := loadPoolStates(db, today)
	if err != nil {
		return nil, err
	}
	for i := range out {
		x := &out[i]
		x.Pools = pools[x.ClusterID]
		applyEffectiveEOS(x, today)
	}
	return out, nil
}

// loadPoolStates 读所有节点池，按 cluster_id 分组。
func loadPoolStates(db *sql.DB, today time.Time) (map[int][]poolState, error) {
	rows, err := db.Query(`
		SELECT cluster_id, name, node_count, version, status, auto_upgrade, auto_repair,
		       auto_upgrade_start_time, max_surge, max_unavailable, strategy, bg_phase,
		       bg_rollout_policy, bg_batch_node_count, bg_batch_percentage, bg_batch_soak_sec, bg_node_pool_soak_sec,
		       upgrade_risk, paused_reason, minor_target_version, eos_standard_at, eos_extended_at
		  FROM gke_node_pools ORDER BY cluster_id, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]poolState{}
	for rows.Next() {
		var cid int
		var p poolState
		var au, ar int
		var startTime, eos, eosExt sql.NullString
		var bgBatch, bgBatchSoak, bgPoolSoak sql.NullInt64
		var bgPct sql.NullFloat64
		if rows.Scan(&cid, &p.Name, &p.NodeCount, &p.Version, &p.Status, &au, &ar,
			&startTime, &p.MaxSurge, &p.MaxUnavailable, &p.Strategy, &p.BGPhase,
			&p.BGRolloutPolicy, &bgBatch, &bgPct, &bgBatchSoak, &bgPoolSoak,
			&p.UpgradeRisk, &p.PausedReason, &p.MinorTarget, &eos, &eosExt) != nil {
			continue
		}
		p.BGBatchNodeCount, p.BGBatchSoakSec = nullIntPtr(bgBatch), nullIntPtr(bgBatchSoak)
		p.BGNodePoolSoakSec = nullIntPtr(bgPoolSoak)
		if bgPct.Valid {
			v := bgPct.Float64
			p.BGBatchPercentage = &v
		}
		p.AutoUpgrade, p.AutoRepair = au == 1, ar == 1
		p.StartTime = dateTimeStr(startTime)
		p.EOSStandardAt, p.EOSExtendedAt = dateStr(eos), dateStr(eosExt)
		// EOSDaysLeft 在 applyEffectiveEOS 里按通道基准重算
		out[cid] = append(out[cid], p)
	}
	return out, nil
}

// isExtendedChannel 集群是否订了 EXTENDED 通道。
// ⚠️ 这决定「硬期限」取哪个日期，取错会产生最高优先级的红色误报：
// 非 EXTENDED → 标准支持结束时被强制升级，硬期限 = eos_standard
// EXTENDED    → 标准支持结束后仍可继续用，硬期限 = eos_extended
// 早期版本一律用 standard，切到 EXTENDED 后会在 standard 到期、extended 未到的那段时间里
// 红着报「强制升级：还有 N 天」，而 GKE 根本不会升。
func isExtendedChannel(channel string) bool {
	return strings.EqualFold(strings.TrimSpace(channel), "EXTENDED")
}

// eosBaseline 按通道选支持截止基准。extended 为空时退回 standard（不能因为没采到就把期限当成无限）。
func eosBaseline(channel, standardAt, extendedAt string) string {
	if isExtendedChannel(channel) && extendedAt != "" {
		return extendedAt
	}
	return standardAt
}

// applyEffectiveEOS 算有效 EOS、节点池偏斜与脱队状态、以及偏斜临界判定。
// 这是 P0-2/P0-3/P1-4 的核心语义，只此一份。
func applyEffectiveEOS(x *clusterUpgradeState, today time.Time) {
	ext := isExtendedChannel(x.ReleaseChannel)
	x.EOSBasis = "标准支持"
	if ext {
		x.EOSBasis = "扩展支持"
	}
	x.EffectiveEOS = eosBaseline(x.ReleaseChannel, x.ControlPlaneEOS, x.EOSExtendedAt)
	x.EffectiveEOSSource = "控制面"

	maxSkew, oldestPool := 0, ""
	for i := range x.Pools {
		p := &x.Pools[i]
		p.SkewMinors = minorGap(x.MasterVersion, p.Version)
		// 「脱队」只描述平时不会自己升；EOS 到了 GKE 仍会强制升，措辞不能说成「永远不会升」
		p.Stranded = p.SkewMinors > 0 && !p.AutoUpgrade
		if p.Stranded && p.UpgradeRisk != "red" {
			p.UpgradeRisk = "red"
			p.RiskNoteAppend("落后控制面且自动升级已关，平时不会自己跟上（但支持结束时 GKE 仍会强制升级）")
		}
		// 自动修复关闭同样是风险，且与自动升级关闭同等重要：
		// 节点坏了 GKE 不会自动 drain 重建，必须人工介入。之前只中性显示「关」，规格差太多。
		if !p.AutoRepair {
			p.RepairOff = true
			p.RiskNoteAppend("自动修复已关：节点故障时 GKE 不会自动重建，需人工介入")
			if p.UpgradeRisk == "" || p.UpgradeRisk == "green" {
				p.UpgradeRisk = "yellow"
			}
		}
		if p.SkewMinors > maxSkew {
			maxSkew, oldestPool = p.SkewMinors, p.Name
		}
		// 节点池的硬期限同样按集群通道选基准
		pe := eosBaseline(x.ReleaseChannel, p.EOSStandardAt, p.EOSExtendedAt)
		p.EffectiveEOSAt = pe
		p.EOSDaysLeft = daysUntil(pe, today)
		if pe != "" && (x.EffectiveEOS == "" || pe < x.EffectiveEOS) {
			x.EffectiveEOS, x.EffectiveEOSSource = pe, "节点池 "+p.Name
		}
	}
	x.EffectiveEOSDays = daysUntil(x.EffectiveEOS, today)

	// 偏斜：事实优先、预测次之（CMDB-024）
	oldestVer := oldestPoolVersion(x.Pools)
	// 必须点名是哪个池——EffectiveEOS 那边会写「节点池 app-pool-01」，两处口径要一致
	poolName := oldestPool
	if poolName == "" {
		poolName = "最旧节点池"
	} else {
		poolName = "节点池 " + poolName
	}
	x.SkewPool, x.SkewPoolVer = oldestPool, oldestVer

	// ① 事实：拿**当前控制面版本**比，回答「现在已经落后几个」
	if oldestVer != "" {
		x.SkewCurrent = minorGap(x.MasterVersion, oldestVer)
	}
	if x.SkewCurrent >= 2 {
		x.SkewCritical = true
		x.SkewNote = "当前：控制面 " + minorOf(x.MasterVersion) + " ｜ " + poolName + "（" + oldestVer +
			"）已落后 " + strconv.Itoa(x.SkewCurrent) + " 个小版本，已达 GKE「节点最多落后控制面 2 个小版本」的硬限制"
	} else if x.SkewCurrent > 0 {
		x.SkewNote = "当前：控制面 " + minorOf(x.MasterVersion) + " ｜ " + poolName + "（" + oldestVer +
			"）落后 " + strconv.Itoa(x.SkewCurrent) + " 个小版本（正常范围）"
	}

	// ② 预测：拿**推断的下一个版本**比，回答「再升一次会怎样」
	target := x.MinorTarget
	if target == "" {
		target = x.InferredTarget
	}
	if target != "" && maxSkew > 0 {
		if gap := minorGap(target, oldestVer); gap >= 2 {
			x.SkewCritical = true
			x.SkewForecast = "预测：控制面若升到 " + minorOf(target) + "，" + poolName +
				"将落后 " + strconv.Itoa(gap) + " 个小版本，GKE 将拒绝"
		}
	}
}

// RiskNoteAppend 追加风险说明，保留已有内容。
func (p *poolState) RiskNoteAppend(s string) {
	if p.riskNote == "" {
		p.riskNote = s
		return
	}
	p.riskNote = s + "；" + p.riskNote
}

func (p *poolState) RiskNote() string { return p.riskNote }

// minorGap 算 a 比 b 高几个小版本（同主版本）。b 比 a 新或无法解析时返回 0。
func minorGap(a, b string) int {
	ma, mb := minorOf(strings.TrimPrefix(a, "v")), minorOf(strings.TrimPrefix(b, "v"))
	if ma == "" || mb == "" {
		return 0
	}
	pa, pb := strings.SplitN(ma, ".", 2), strings.SplitN(mb, ".", 2)
	if len(pa) != 2 || len(pb) != 2 || pa[0] != pb[0] {
		return 0
	}
	na, _ := strconv.Atoi(pa[1])
	nb, _ := strconv.Atoi(pb[1])
	if na <= nb {
		return 0
	}
	return na - nb
}

// oldestPoolVersion 最旧的节点池版本，用于偏斜临界判定。
func oldestPoolVersion(pools []poolState) string {
	oldest := ""
	for _, p := range pools {
		if p.Version == "" {
			continue
		}
		if oldest == "" || minorGap(oldest, p.Version) > 0 {
			oldest = p.Version
		}
	}
	return oldest
}
