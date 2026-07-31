// GKE 版本与升级信息的两个采集任务。
//
//	gke_schedule_sync —— 抓官网版本排期表（不依赖 GCP 凭据）
//	gke_upgrade_sync  —— 逐集群拉升级信息 / 节点池 / 升级历史 / 自动修复记录（需 SA key）
//
// 两个任务分开的原因：排期表是全局数据、一天变不了几次、且没有凭据也能跑；
// 集群采集依赖云凭据且更频繁。混在一起会让「没凭据」把排期表也一起拖垮。
package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// ---------------------------------------------------------------------------
// 任务一：官网版本排期同步
// ---------------------------------------------------------------------------

// gkeScheduleSyncCore 抓官网排期表并落库。
// 抓取或解析失败时**保留库里的旧数据**（返回 ok=false 让任务标失败），
// 绝不用半截结果覆盖——显示过期日期比显示错误日期安全得多。
func gkeScheduleSyncCore(ctx context.Context, db *sql.DB) (string, []TaskFailure, bool) {
	rows, err := k8ssource.FetchGKESchedule(ctx)
	if err != nil {
		logx.J("gke_schedule", "sync_failed", map[string]any{"err": err.Error(), "note": "保留库中上次数据"})
		return "抓取/解析官网排期表失败，已保留上次数据：" + err.Error(),
			[]TaskFailure{{Target: "release-schedule", Reason: err.Error()}}, false
	}

	var ins, upd, skipped int
	for _, r := range rows {
		// is_manual=1 的行是人工覆盖过的，同步不能冲掉
		res, e := db.Exec(`
			INSERT INTO gke_version_schedule
			  (minor_version, channel,
			   available_raw, available_at, available_precision,
			   auto_upgrade_raw, auto_upgrade_at, auto_upgrade_precision,
			   eos_standard_raw, eos_standard_at, eos_standard_precision,
			   eos_extended_raw, eos_extended_at, eos_extended_precision, is_manual)
			VALUES (?,?, ?,?,?, ?,?,?, ?,?,?, ?,?,?, 0)
			ON DUPLICATE KEY UPDATE
			  available_raw=IF(is_manual=1, available_raw, VALUES(available_raw)),
			  available_at=IF(is_manual=1, available_at, VALUES(available_at)),
			  available_precision=IF(is_manual=1, available_precision, VALUES(available_precision)),
			  auto_upgrade_raw=IF(is_manual=1, auto_upgrade_raw, VALUES(auto_upgrade_raw)),
			  auto_upgrade_at=IF(is_manual=1, auto_upgrade_at, VALUES(auto_upgrade_at)),
			  auto_upgrade_precision=IF(is_manual=1, auto_upgrade_precision, VALUES(auto_upgrade_precision)),
			  eos_standard_raw=IF(is_manual=1, eos_standard_raw, VALUES(eos_standard_raw)),
			  eos_standard_at=IF(is_manual=1, eos_standard_at, VALUES(eos_standard_at)),
			  eos_standard_precision=IF(is_manual=1, eos_standard_precision, VALUES(eos_standard_precision)),
			  eos_extended_raw=IF(is_manual=1, eos_extended_raw, VALUES(eos_extended_raw)),
			  eos_extended_at=IF(is_manual=1, eos_extended_at, VALUES(eos_extended_at)),
			  eos_extended_precision=IF(is_manual=1, eos_extended_precision, VALUES(eos_extended_precision)),
			  synced_at=NOW()`,
			r.MinorVersion, r.Channel,
			r.Available.Raw, nullDate(r.Available.Date), r.Available.Precision,
			r.AutoUpgrade.Raw, nullDate(r.AutoUpgrade.Date), r.AutoUpgrade.Precision,
			r.EOSStandard.Raw, nullDate(r.EOSStandard.Date), r.EOSStandard.Precision,
			r.EOSExtended.Raw, nullDate(r.EOSExtended.Date), r.EOSExtended.Precision)
		if e != nil {
			skipped++
			logx.J("gke_schedule", "upsert_failed", map[string]any{
				"version": r.MinorVersion, "channel": r.Channel, "err": e.Error(),
			})
			continue
		}
		// MySQL 的 ON DUPLICATE：插入 affected=1，更新 affected=2，无变化 affected=0
		if n, _ := res.RowsAffected(); n == 1 {
			ins++
		} else {
			upd++
		}
	}

	// 非 day 粒度的条数要说出来——这些日期官网自己都说会变，别让人当精确日期用
	var approx int
	_ = db.QueryRow(`SELECT COUNT(*) FROM gke_version_schedule WHERE auto_upgrade_precision IN ('month','quarter')`).Scan(&approx)

	summary := fmt.Sprintf("排期表同步完成：新增 %d，更新 %d，共 %d 行；其中 %d 行自动升级日期只到月/季度粒度（官网标注为近似值）",
		ins, upd, len(rows), approx)
	if skipped > 0 {
		summary += fmt.Sprintf("，%d 行写库失败", skipped)
		return summary, []TaskFailure{{Target: "upsert", Reason: fmt.Sprintf("%d 行写库失败，详见日志", skipped)}}, false
	}
	logx.J("gke_schedule", "sync_done", map[string]any{"insert": ins, "update": upd, "approx": approx})
	return summary, nil, true
}

// ---------------------------------------------------------------------------
// 任务二：集群升级信息采集
// ---------------------------------------------------------------------------

type gkeTarget struct {
	ClusterID int
	Name      string
	Project   string
	Location  string
	AccountID int
}

// gkeUpgradeSyncCore 逐个 GKE 集群采集升级信息、节点池、升级历史与自动修复记录。
// 单个集群失败不影响其他集群（记进 TaskFailure 供重试）。
func gkeUpgradeSyncCore(ctx context.Context, db *sql.DB, cipher *crypto.Cipher, prog ProgressFn, targets []string) (string, []TaskFailure, bool) {
	q := `SELECT id, name, project_id, location, cloud_account_id
	        FROM k8s_clusters WHERE provider='gke' AND enabled=1`
	rows, err := db.Query(q)
	if err != nil {
		return "查询 GKE 集群失败：" + err.Error(), []TaskFailure{{Target: "db", Reason: err.Error()}}, false
	}
	list := []gkeTarget{}
	for rows.Next() {
		var t gkeTarget
		if rows.Scan(&t.ClusterID, &t.Name, &t.Project, &t.Location, &t.AccountID) == nil {
			if len(targets) == 0 || containsStr(targets, t.Name) {
				list = append(list, t)
			}
		}
	}
	rows.Close()

	if len(list) == 0 {
		return "没有已启用的 GKE 集群（本地 CMDB 只纳管 docker-desktop 时属正常）", nil, true
	}

	var failures []TaskFailure
	var okCount, poolCount, histCount, repairCount int
	opsDone := map[string]bool{} // 同 project 的多个集群共用一次 operations.list

	for i, t := range list {
		select {
		case <-ctx.Done():
			return fmt.Sprintf("已中止：完成 %d/%d 个集群", i, len(list)), failures, false
		default:
		}
		prog(i, len(list))

		saJSON, e := gkeCred(db, cipher, t.AccountID, t.Project)
		if e != nil {
			failures = append(failures, TaskFailure{Target: t.Name, Reason: e.Error()})
			markClusterErr(db, t.ClusterID, e.Error())
			continue
		}

		snap, e := k8ssource.FetchClusterUpgrade(ctx, saJSON, t.Project, t.Location, t.Name)
		if e != nil {
			failures = append(failures, TaskFailure{Target: t.Name, Reason: e.Error()})
			markClusterErr(db, t.ClusterID, e.Error())
			continue
		}
		pools, e := k8ssource.FetchNodePools(ctx, saJSON, t.Project, t.Location, t.Name)
		if e != nil {
			// 节点池失败不算整个集群失败——集群级信息已经有价值，先存下来
			failures = append(failures, TaskFailure{Target: t.Name + "/nodePools", Reason: e.Error()})
			logx.J("gke_upgrade", "nodepools_failed", map[string]any{"cluster": t.Name, "err": e.Error()})
		}

		predAt, predPrec, predSrc := predictUpgradeDate(db, snap, pools)
		saveClusterUpgrade(db, t.ClusterID, snap, predAt, predPrec, predSrc)
		poolCount += saveNodePools(db, t.ClusterID, pools)
		histCount += saveUpgradeHistory(db, t.ClusterID, snap.UpgradeDetails)
		for _, p := range pools {
			histCount += saveUpgradeHistory(db, t.ClusterID, p.UpgradeDetails)
		}

		// operations.list 是 project 级的，同 project 只拉一次
		if !opsDone[t.Project] {
			opsDone[t.Project] = true
			if ops, e2 := k8ssource.ListGKEOperations(ctx, saJSON, t.Project); e2 != nil {
				failures = append(failures, TaskFailure{Target: t.Project + "/operations", Reason: e2.Error()})
			} else {
				h, r := saveOperations(db, list, t.Project, ops)
				histCount += h
				repairCount += r
			}
		}
		okCount++
	}
	prog(len(list), len(list))

	// ⚠️ histCount 是「处理条数」不是「落库行数」：同一升级事件会被 upgradeDetails 和
	// operations 两个来源各交一次，合并后落库只有一行。摘要不写清楚会被误读成去重没生效。
	var histRows int
	_ = db.QueryRow(`SELECT COUNT(*) FROM gke_upgrade_history`).Scan(&histRows)
	summary := fmt.Sprintf("采集完成：%d/%d 个集群成功，节点池 %d 个，升级记录处理 %d 条→库中 %d 个事件，自动修复记录 %d 条",
		okCount, len(list), poolCount, histCount, histRows, repairCount)
	logx.J("gke_upgrade", "sync_done", map[string]any{
		"ok": okCount, "total": len(list), "pools": poolCount,
		"history": histCount, "repairs": repairCount, "failures": len(failures),
	})
	return summary, failures, len(failures) == 0
}

// gkeCred 取该集群所属云账号项目的 SA key 并解密。
func gkeCred(db *sql.DB, cipher *crypto.Cipher, accountID int, project string) ([]byte, error) {
	var enc sql.NullString
	e := db.QueryRow(`SELECT cred_enc FROM cloud_account_projects WHERE account_id=? AND project_id=?`,
		accountID, project).Scan(&enc)
	if e != nil || !enc.Valid || enc.String == "" {
		return nil, fmt.Errorf("云账号项目 %s 未配 SA key（系统管理→云账号）", project)
	}
	s, e := cipher.Decrypt(enc.String)
	if e != nil {
		return nil, fmt.Errorf("解密 SA key 失败: %w", e)
	}
	return []byte(s), nil
}

func markClusterErr(db *sql.DB, clusterID int, msg string) {
	if _, e := db.Exec(`INSERT INTO gke_cluster_upgrade (cluster_id, last_error) VALUES (?,?)
		ON DUPLICATE KEY UPDATE last_error=VALUES(last_error), synced_at=NOW()`, clusterID, truncStr(msg, 500)); e != nil {
		logx.J("gke_upgrade", "mark_err_failed", map[string]any{"cluster_id": clusterID, "err": e.Error()})
	}
}

func saveClusterUpgrade(db *sql.DB, clusterID int, s *k8ssource.ClusterUpgradeSnapshot, predAt, predPrec, predSrc string) {
	_, e := db.Exec(`
		INSERT INTO gke_cluster_upgrade
		  (cluster_id, release_channel, current_master_version, minor_target_version, patch_target_version,
		   auto_upgrade_status, paused_reason, eos_standard_at, eos_extended_at, maintenance_policy_json,
		   predicted_upgrade_at, predicted_precision, predicted_source, last_error)
		VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?, '')
		ON DUPLICATE KEY UPDATE
		  release_channel=VALUES(release_channel), current_master_version=VALUES(current_master_version),
		  minor_target_version=VALUES(minor_target_version), patch_target_version=VALUES(patch_target_version),
		  auto_upgrade_status=VALUES(auto_upgrade_status), paused_reason=VALUES(paused_reason),
		  eos_standard_at=VALUES(eos_standard_at), eos_extended_at=VALUES(eos_extended_at),
		  maintenance_policy_json=VALUES(maintenance_policy_json),
		  predicted_upgrade_at=VALUES(predicted_upgrade_at), predicted_precision=VALUES(predicted_precision),
		  predicted_source=VALUES(predicted_source), last_error='', synced_at=NOW()`,
		clusterID, s.ReleaseChannel, s.CurrentMasterVersion, s.MinorTargetVersion, s.PatchTargetVersion,
		strings.Join(s.AutoUpgradeStatus, ","), strings.Join(s.PausedReason, ","),
		dateOnly(s.EOSStandard), dateOnly(s.EOSExtended), nullStr(s.MaintenancePolicyJSON),
		nullDate(predAt), predPrec, predSrc)
	if e != nil {
		logx.J("gke_upgrade", "save_cluster_failed", map[string]any{"cluster_id": clusterID, "err": e.Error()})
	}
}

// realNodeCounts 按节点池统计真实节点数。
// ⚠️ 不能用 NodePool.InitialNodeCount（GKE API 给的是「每个 zone 的初始节点数」）：
// regional 集群会 ×3 个 zone，开了自动扩缩容后更是与当前值无关。
// 实测 g32 看板显示 6/1/1/1/2（合计 11），真实是 16/4/5/4/6（合计 35）。
// 这不只是显示错——风险评分的「≤4 节点」判据会跟着错，把大池误判成小池。
func realNodeCounts(db *sql.DB, clusterID int) map[string]int {
	out := map[string]int{}
	rows, err := db.Query(`SELECT pool, COUNT(*) FROM k8s_nodes WHERE cluster_id=? AND pool<>'' GROUP BY pool`, clusterID)
	if err != nil {
		logx.J("gke_upgrade", "node_count_query_failed", map[string]any{"cluster_id": clusterID, "err": err.Error()})
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var pool string
		var n int
		if rows.Scan(&pool, &n) == nil {
			out[pool] = n
		}
	}
	return out
}

func saveNodePools(db *sql.DB, clusterID int, pools []k8ssource.NodePoolSnapshot) int {
	counts := realNodeCounts(db, clusterID)
	n := 0
	for i := range pools {
		p := &pools[i]
		if c, ok := counts[p.Name]; ok && c > 0 {
			p.NodeCount = c
		} else if p.NodeCount > 0 {
			// k8s_nodes 还没采到这个池（新建或节点标签缺失），沿用 API 值但要能看见
			logx.J("gke_upgrade", "node_count_fallback", map[string]any{
				"cluster_id": clusterID, "pool": p.Name, "api_initial_count": p.NodeCount,
				"note": "k8s_nodes 表里没有该池的节点，暂用 API 的 initialNodeCount（可能偏小）",
			})
		}
	}
	for _, p := range pools {
		_, e := db.Exec(`
			INSERT INTO gke_node_pools
			  (cluster_id, name, node_count, version, status, auto_upgrade, auto_repair,
			   auto_upgrade_start_time, upgrade_description, max_surge, max_unavailable, strategy,
			   bg_phase, upgrade_risk, auto_upgrade_status, paused_reason, minor_target_version, eos_standard_at)
			VALUES (?,?,?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE
			  node_count=VALUES(node_count), version=VALUES(version), status=VALUES(status),
			  auto_upgrade=VALUES(auto_upgrade), auto_repair=VALUES(auto_repair),
			  auto_upgrade_start_time=VALUES(auto_upgrade_start_time), upgrade_description=VALUES(upgrade_description),
			  max_surge=VALUES(max_surge), max_unavailable=VALUES(max_unavailable), strategy=VALUES(strategy),
			  bg_phase=VALUES(bg_phase), upgrade_risk=VALUES(upgrade_risk),
			  auto_upgrade_status=VALUES(auto_upgrade_status), paused_reason=VALUES(paused_reason),
			  minor_target_version=VALUES(minor_target_version), eos_standard_at=VALUES(eos_standard_at),
			  synced_at=NOW()`,
			clusterID, p.Name, p.NodeCount, p.Version, p.Status, boolInt(p.AutoUpgrade), boolInt(p.AutoRepair),
			nullDateTime(p.AutoUpgradeStartTime), truncStr(p.UpgradeDescription, 500),
			p.MaxSurge, p.MaxUnavailable, p.Strategy,
			p.BlueGreenPhase, poolUpgradeRisk(p), strings.Join(p.AutoUpgradeStatus, ","),
			strings.Join(p.PausedReason, ","), p.MinorTargetVersion, dateOnly(p.EOSStandard))
		if e != nil {
			logx.J("gke_upgrade", "save_pool_failed", map[string]any{"cluster_id": clusterID, "pool": p.Name, "err": e.Error()})
			continue
		}
		n++
	}
	return n
}

// upgradeEventKey 与来源无关的事件键：同一次升级无论从 upgradeDetails 还是 operations 采到，
// 都落到同一行，避免历史条数虚高一倍（见 migration 068）。
// 截到分钟：两个来源的 startTime 有秒级抖动，而同一节点池同一分钟不会有两次升级。
func upgradeEventKey(scope, pool, startTime string) string {
	minute := ""
	if t, ok := k8ssource.ParseGKETime(startTime); ok {
		minute = t.UTC().Format("2006-01-02 15:04")
	} else if len(startTime) >= 16 {
		minute = startTime[:16]
	}
	return fmt.Sprintf("%s:%s:%s", scope, pool, minute)
}

func saveUpgradeHistory(db *sql.DB, clusterID int, recs []k8ssource.UpgradeRecord) int {
	n := 0
	for _, r := range recs {
		key := upgradeEventKey(r.Scope, r.Pool, r.StartTime)
		_, e := db.Exec(`
			INSERT INTO gke_upgrade_history
			  (cluster_id, dedup_key, scope, pool, start_type, state,
			   initial_version, target_version, started_at, ended_at, detail, source)
			VALUES (?,?,?,?,?,?, ?,?,?,?, '', 'upgradeDetails')
			ON DUPLICATE KEY UPDATE
			  start_type=VALUES(start_type), state=VALUES(state),
			  initial_version=VALUES(initial_version), target_version=VALUES(target_version),
			  ended_at=VALUES(ended_at), source='upgradeDetails', synced_at=NOW()`,
			clusterID, key, r.Scope, r.Pool, r.StartType, r.State,
			r.InitialVersion, r.TargetVersion, nullDateTime(r.StartTime), nullDateTime(r.EndTime))
		if e != nil {
			logx.J("gke_upgrade", "save_history_failed", map[string]any{"cluster_id": clusterID, "key": key, "err": e.Error()})
			continue
		}
		n++
	}
	return n
}

// saveOperations 把 project 级操作记录分派到各集群：升级类进 gke_upgrade_history，
// AUTO_REPAIR_NODES 进 gke_repair_history。集群归属靠 targetLink 里的集群名匹配。
func saveOperations(db *sql.DB, all []gkeTarget, project string, ops []k8ssource.OperationRecord) (hist, repair int) {
	byName := map[string]int{}
	for _, t := range all {
		if t.Project == project {
			byName[t.Name] = t.ClusterID
		}
	}
	for _, op := range ops {
		cid := 0
		for name, id := range byName {
			if strings.Contains(op.TargetLink, "/clusters/"+name) {
				cid = id
				break
			}
		}
		if cid == 0 {
			continue // 不属于我们纳管的集群
		}
		switch op.Type {
		case "AUTO_REPAIR_NODES":
			_, e := db.Exec(`
				INSERT INTO gke_repair_history
				  (cluster_id, op_name, pool, node_name, repair_reason, status, started_at, ended_at, detail, status_message)
				VALUES (?,?,?,'',?,?,?,?,?,?)
				ON DUPLICATE KEY UPDATE status=VALUES(status), ended_at=VALUES(ended_at),
				  detail=VALUES(detail), status_message=VALUES(status_message), synced_at=NOW()`,
				cid, op.Name, op.Pool, op.RepairReason, op.Status,
				nullDateTime(op.StartTime), nullDateTime(op.EndTime),
				truncStr(op.Detail, 1000), truncStr(op.StatusMessage, 500))
			if e == nil {
				repair++
			} else {
				logx.J("gke_upgrade", "save_repair_failed", map[string]any{"op": op.Name, "err": e.Error()})
			}
		case "UPGRADE_MASTER", "UPGRADE_NODES":
			scope := "control_plane"
			if op.Type == "UPGRADE_NODES" {
				scope = "nodepool"
			}
			// 与 upgradeDetails 共用事件键落到同一行。
			// ⚠️ 冲突时不能覆盖 start_type / 版本 / source —— 那些只有 upgradeDetails 有，
			// operations 这一侧全是空值，覆盖过去会把「🤖自动/👤手动」抹成「未知」。
			_, e := db.Exec(`
				INSERT INTO gke_upgrade_history
				  (cluster_id, dedup_key, scope, pool, start_type, state,
				   initial_version, target_version, started_at, ended_at, detail, source, op_name)
				VALUES (?,?,?,?,'',?, '','',?,?,?, 'operations', ?)
				ON DUPLICATE KEY UPDATE
				  ended_at=IFNULL(gke_upgrade_history.ended_at, VALUES(ended_at)),
				  detail=VALUES(detail), op_name=VALUES(op_name), synced_at=NOW()`,
				cid, upgradeEventKey(scope, op.Pool, op.StartTime), scope, op.Pool, op.Status,
				nullDateTime(op.StartTime), nullDateTime(op.EndTime), truncStr(op.Detail, 1000), op.Name)
			if e == nil {
				hist++
			} else {
				logx.J("gke_upgrade", "save_op_history_failed", map[string]any{"op": op.Name, "err": e.Error()})
			}
		}
	}
	return hist, repair
}

// ---------------------------------------------------------------------------
// 预计自动升级日期
// ---------------------------------------------------------------------------

var reMinor = regexp.MustCompile(`^(\d+\.\d+)`)

// minorOf 从 "1.34.5-gke.1278000" 提取 "1.34"。
func minorOf(v string) string {
	return reMinor.FindString(strings.TrimPrefix(v, "v"))
}

// predictUpgradeDate 算「预计自动升级日」，三级优先：
//  1. 节点池的 autoUpgradeStartTime —— 官方只在升级即将开始时才填，最准，精确到小时
//  2. 官网排期表 (目标小版本, 集群通道) 的 Auto Upgrade 日期 —— 远期，能提前一个月
//  3. 都没有 → 空，前端显示「排期未知」
//
// 通道为空时按官方规则回退取 STABLE 列（未入通道的集群自动升级日期同 Stable）。
func predictUpgradeDate(db *sql.DB, s *k8ssource.ClusterUpgradeSnapshot, pools []k8ssource.NodePoolSnapshot) (date, precision, source string) {
	// ① 临期精确时刻：取所有节点池里最早的一个
	earliest := ""
	for _, p := range pools {
		if p.AutoUpgradeStartTime == "" {
			continue
		}
		if earliest == "" || p.AutoUpgradeStartTime < earliest {
			earliest = p.AutoUpgradeStartTime
		}
	}
	if earliest != "" {
		if t, ok := k8ssource.ParseGKETime(earliest); ok {
			return t.Format("2006-01-02"), "day", "autoUpgradeStartTime"
		}
	}

	// ② 官网排期表：优先用 API 给的目标版本
	target := minorOf(s.MinorTargetVersion)
	source = "schedule_table"
	if target == "" {
		// ③ 兜底推断：minorTargetVersion 只在 GKE 已排期时才有值，
		// 实测 4 个集群全为空（都还没排期）。此时用「当前小版本的下一个」查排期表，
		// 否则看板上「预计自动升级」会永远是空的，等于这个功能白做。
		// 标记 source=inferred_next_minor，前端必须显示为「推断」而非确定值。
		target = nextMinor(s.CurrentMasterVersion)
		source = "inferred_next_minor"
		logx.J("gke_upgrade", "no_minor_target", map[string]any{
			"master": s.CurrentMasterVersion, "inferred": target,
			"note": "GKE 尚未排期(minorTargetVersion 为空)，按当前版本的下一个小版本推断",
		})
		if target == "" {
			return "", "unknown", "none"
		}
	}
	channel := strings.ToUpper(s.ReleaseChannel)
	if channel == "" || channel == "UNSPECIFIED" {
		channel = "STABLE" // 官方规则：未入通道的集群自动升级日期同 Stable
	}
	var at sql.NullString
	var prec string
	e := db.QueryRow(`SELECT auto_upgrade_at, auto_upgrade_precision
	                    FROM gke_version_schedule WHERE minor_version=? AND channel=?`,
		target, channel).Scan(&at, &prec)
	if e != nil || !at.Valid {
		logx.J("gke_upgrade", "schedule_miss", map[string]any{
			"minor": target, "channel": channel, "note": "排期表里没有这个版本/通道，先跑 gke_schedule_sync",
		})
		return "", "unknown", "none"
	}
	d := at.String
	if len(d) >= 10 {
		d = d[:10]
	}
	return d, prec, source
}

// nextMinor 由当前版本推下一个小版本："1.34.8-gke.1278000" → "1.35"。
// 用整数递增而非字符串拼接，否则 1.9 会推成 1.10 之外的错值。
func nextMinor(cur string) string {
	m := minorOf(cur)
	if m == "" {
		return ""
	}
	parts := strings.SplitN(m, ".", 2)
	if len(parts) != 2 {
		return ""
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil {
		return ""
	}
	return parts[0] + "." + strconv.Itoa(n+1)
}

// poolUpgradeRisk 节点池升级风险：maxUnavailable 占比越高，升级时同时不可用的节点越多。
// BLUE_GREEN 策略是先起新池再切，不存在这个问题。
func poolUpgradeRisk(p k8ssource.NodePoolSnapshot) string {
	if strings.EqualFold(p.Strategy, "BLUE_GREEN") {
		return "green"
	}
	if p.NodeCount <= 0 || p.MaxUnavailable <= 0 {
		return "green"
	}
	ratio := float64(p.MaxUnavailable) / float64(p.NodeCount)
	switch {
	case ratio >= 0.15:
		return "red"
	case ratio >= 0.10 || p.NodeCount <= 4:
		return "yellow"
	}
	return "green"
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func nullDate(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// dateOnly 把 RFC3339 或 YYYY-MM-DD 截成 YYYY-MM-DD；空/异常返回空串。
func dateOnly(s string) any {
	if s == "" {
		return nil
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return nil
}

// nullDateTime RFC3339 → MySQL DATETIME（UTC 转本地由 MySQL 会话时区处理，这里统一存 UTC 墙钟）。
func nullDateTime(s string) any {
	if s == "" {
		return nil
	}
	if t, ok := k8ssource.ParseGKETime(s); ok {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	return nil
}

// boolInt / truncStr 复用 cdn.go / event_center.go 里的同名 helper，此处不重复定义。

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

var _ = time.Now // 保留 time 导入供后续阶段使用
