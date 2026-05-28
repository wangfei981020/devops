package services

import (
	"database/sql"
	"log"
	"time"

	"opsplatform-gke-version-backend/models"
)

// RecordTransition: scrape 完一个集群后，按"集群+每个节点池"维度检测版本变更，
// 写入 version_history 表。
// - 新对象首次出现：INSERT 起始记录
// - 同对象版本不变：不动
// - 同对象版本变了：UPDATE 老行 ended_at；INSERT 新行
// - 节点池消失（被删）：UPDATE 老行 ended_at（视为退役）
func RecordTransition(db *sql.DB, cl *models.Cluster, snap *models.ClusterSnapshot) {
	now := time.Now()

	// 集群层
	if snap.CurrentVersion != "" {
		upsertVersion(db, cl.ID, "", snap.CurrentVersion, now)
	}

	// 节点池层（先记当前活跃的）
	seenNodepools := map[string]bool{}
	for _, np := range snap.NodePools {
		if np.Name == "" || np.CurrentVersion == "" {
			continue
		}
		seenNodepools[np.Name] = true
		upsertVersion(db, cl.ID, np.Name, np.CurrentVersion, now)
	}

	// 检测被删除的节点池：DB 里 ended_at IS NULL 但本次没在 seenNodepools 里
	retireDisappearedNodepools(db, cl.ID, seenNodepools, now)
}

func upsertVersion(db *sql.DB, clusterID int, nodepoolName, version string, now time.Time) {
	var (
		curID      int
		curVersion string
	)
	q := `SELECT id, version FROM version_history WHERE cluster_id=? AND `
	args := []any{clusterID}
	if nodepoolName == "" {
		q += "nodepool_name IS NULL "
	} else {
		q += "nodepool_name=? "
		args = append(args, nodepoolName)
	}
	q += "AND ended_at IS NULL ORDER BY started_at DESC LIMIT 1"

	err := db.QueryRow(q, args...).Scan(&curID, &curVersion)

	if err == sql.ErrNoRows {
		// 首次出现该对象
		var np any = nil
		if nodepoolName != "" {
			np = nodepoolName
		}
		if _, err := db.Exec(`INSERT INTO version_history (cluster_id, nodepool_name, version, started_at) VALUES (?, ?, ?, ?)`,
			clusterID, np, version, now); err != nil {
			log.Printf("version_history insert: %v", err)
		}
		return
	}
	if err != nil {
		log.Printf("version_history lookup: %v", err)
		return
	}

	if curVersion == version {
		return // 版本没变，不动
	}

	// 版本变了：关闭老行 + 开新行
	tx, err := db.Begin()
	if err != nil {
		log.Printf("version_history tx: %v", err)
		return
	}
	if _, err := tx.Exec(`UPDATE version_history SET ended_at=? WHERE id=?`, now, curID); err != nil {
		_ = tx.Rollback()
		log.Printf("version_history close old: %v", err)
		return
	}
	var np any = nil
	if nodepoolName != "" {
		np = nodepoolName
	}
	if _, err := tx.Exec(`INSERT INTO version_history (cluster_id, nodepool_name, version, started_at) VALUES (?, ?, ?, ?)`,
		clusterID, np, version, now); err != nil {
		_ = tx.Rollback()
		log.Printf("version_history insert new: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("version_history commit: %v", err)
	}
}

func retireDisappearedNodepools(db *sql.DB, clusterID int, seen map[string]bool, now time.Time) {
	rows, err := db.Query(`SELECT id, nodepool_name FROM version_history WHERE cluster_id=? AND nodepool_name IS NOT NULL AND ended_at IS NULL`, clusterID)
	if err != nil {
		log.Printf("version_history retire query: %v", err)
		return
	}
	type tomb struct {
		id   int
		name string
	}
	tombs := []tomb{}
	for rows.Next() {
		var t tomb
		if err := rows.Scan(&t.id, &t.name); err == nil {
			tombs = append(tombs, t)
		}
	}
	rows.Close()
	for _, t := range tombs {
		if !seen[t.name] {
			if _, err := db.Exec(`UPDATE version_history SET ended_at=? WHERE id=?`, now, t.id); err != nil {
				log.Printf("version_history retire %s: %v", t.name, err)
			}
		}
	}
}
