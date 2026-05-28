package services

import (
	"database/sql"
	"log"
	"sort"
	"time"

	"opsplatform-gke-version-backend/models"
)

// UpsertUpgradeEvents：scraper 拉到的 ops 推算 from/to 版本后写入 upgrade_events 表。
//
// 推断流程：
//  1. 甲：to_version 优先用 op.DetailToVersion（正则提取 detail 文本）
//  2. 乙：用 version_history 历史快照配对
//     - to_version: 找 nodepool/master 在 op.EndTime 之前最后一次 started_at 接近 EndTime 的 row
//     - from_version: 找在 op.EndTime 之前 ended_at 接近 EndTime 的 row
//
// 监控前发生的事件，乙 拿不到数据，from_version 留空（前端展示为 '-'）。
func UpsertUpgradeEvents(db *sql.DB, clusterID int, clusterName string, ops []UpgradeOp) error {
	// 预加载 version_history（按 nodepool 分组）——一次性查完，避免循环里反复 query
	hist, err := loadVersionHistory(db, clusterID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, op := range ops {
		// 不属于这个集群（同 location 下其他集群的 op）跳过
		if name := ClusterNameFromTarget(op.RawDetail); name != "" && name != clusterName {
			// raw_detail 一般不含 cluster 名，跳过靠 detail 的判断；这里靠 targetLink 已不可得
			// 保留兜底逻辑入口，但实际上调用方传进来的 ops 应该已经过滤
		}

		toVersion, toSource := inferToVersion(op, hist)
		fromVersion, fromSource := inferFromVersion(op, hist)

		_, err := db.Exec(`INSERT INTO upgrade_events
			(cluster_id, nodepool_name, operation_id, operation_type,
			 from_version, to_version, from_source, to_source,
			 status, started_at, ended_at, raw_detail, scraped_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE
			  -- 已有记录：只在新数据更靠谱（snapshot 优先 detail）时覆盖
			  from_version = IF(VALUES(from_source)='snapshot' OR from_source='empty', VALUES(from_version), from_version),
			  from_source  = IF(VALUES(from_source)='snapshot' OR from_source='empty', VALUES(from_source), from_source),
			  to_version   = IF(VALUES(to_source)='snapshot' OR to_source='empty', VALUES(to_version), to_version),
			  to_source    = IF(VALUES(to_source)='snapshot' OR to_source='empty', VALUES(to_source), to_source),
			  status       = VALUES(status),
			  scraped_at   = VALUES(scraped_at)`,
			clusterID, op.NodepoolName, op.OperationID, op.OperationType,
			fromVersion, toVersion, fromSource, toSource,
			op.Status, nullableTime(op.StartTime), nullableTime(op.EndTime),
			op.RawDetail, now)
		if err != nil {
			log.Printf("upsert upgrade_event %s: %v", op.OperationID, err)
		}
	}
	return nil
}

// loadVersionHistory：返回该集群所有 version_history 行，按 nodepool 分组（master 用空串 key）
func loadVersionHistory(db *sql.DB, clusterID int) (map[string][]vhRow, error) {
	rows, err := db.Query(`SELECT COALESCE(nodepool_name,''), version, started_at, ended_at
		FROM version_history WHERE cluster_id=? ORDER BY started_at ASC`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]vhRow{}
	for rows.Next() {
		var r vhRow
		var ended sql.NullTime
		if err := rows.Scan(&r.nodepool, &r.version, &r.started, &ended); err != nil {
			return nil, err
		}
		if ended.Valid {
			r.ended = &ended.Time
		}
		out[r.nodepool] = append(out[r.nodepool], r)
	}
	return out, rows.Err()
}

type vhRow struct {
	nodepool string
	version  string
	started  time.Time
	ended    *time.Time
}

// 乙之 to_version：在 op.EndTime ± 容忍窗内找 version_history 里"新开始"的行
//   - 升级完成后我们紧接着 scrape 应该会看到新版本，version_history 写一行 started_at ≈ EndTime
//   - 容忍窗：操作完成后 1 个 scrape 间隔内（默认 30min，这里给 12h 兜底）
//
// 甲之 to_version：直接用 op.DetailToVersion
//
// 优先级：乙 > 甲（乙是真实观察，甲是文本解析可能格式变）
func inferToVersion(op UpgradeOp, hist map[string][]vhRow) (string, string) {
	if !op.EndTime.IsZero() {
		win := 12 * time.Hour
		for _, r := range hist[op.NodepoolName] {
			diff := r.started.Sub(op.EndTime)
			if diff >= 0 && diff <= win {
				return r.version, "snapshot"
			}
		}
	}
	if op.DetailToVersion != "" {
		return op.DetailToVersion, "detail"
	}
	return "", "empty"
}

// 乙之 from_version：找在 op.EndTime 前结束（ended_at ≤ EndTime）的最近一行
// 监控前发生的升级，version_history 没数据，返回 ('', empty)
func inferFromVersion(op UpgradeOp, hist map[string][]vhRow) (string, string) {
	if op.EndTime.IsZero() {
		return "", "empty"
	}
	// 倒序找最近一条 ended_at <= EndTime 的
	rows := hist[op.NodepoolName]
	sort.Slice(rows, func(i, j int) bool {
		// 按 ended_at 倒序；nil 的（仍在跑）放最后
		var ei, ej time.Time
		if rows[i].ended != nil {
			ei = *rows[i].ended
		}
		if rows[j].ended != nil {
			ej = *rows[j].ended
		}
		return ei.After(ej)
	})
	win := 12 * time.Hour
	for _, r := range rows {
		if r.ended == nil {
			continue
		}
		diff := op.EndTime.Sub(*r.ended)
		if diff >= 0 && diff <= win {
			return r.version, "snapshot"
		}
	}
	return "", "empty"
}

// ListUpgradesByCluster：handler 查询用，按 nodepool 分组返回升级事件（含 master = ''）。
// 同一组内按 ended_at DESC（最近的升级在前）。
func ListUpgradesByCluster(db *sql.DB, clusterID int) ([]models.UpgradeEvent, error) {
	rows, err := db.Query(`SELECT id, cluster_id, nodepool_name, operation_id, operation_type,
		from_version, to_version, from_source, to_source,
		status, started_at, ended_at, raw_detail, scraped_at
		FROM upgrade_events WHERE cluster_id=?
		ORDER BY nodepool_name ASC, ended_at DESC`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.UpgradeEvent{}
	for rows.Next() {
		var e models.UpgradeEvent
		var started, ended sql.NullTime
		var rawDetail sql.NullString
		if err := rows.Scan(&e.ID, &e.ClusterID, &e.NodepoolName, &e.OperationID, &e.OperationType,
			&e.FromVersion, &e.ToVersion, &e.FromSource, &e.ToSource,
			&e.Status, &started, &ended, &rawDetail, &e.ScrapedAt); err != nil {
			return nil, err
		}
		if started.Valid {
			e.StartedAt = &started.Time
		}
		if ended.Valid {
			e.EndedAt = &ended.Time
		}
		if rawDetail.Valid {
			e.RawDetail = rawDetail.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullableTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}
