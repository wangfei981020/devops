package services

import (
	"database/sql"
	"fmt"
	"time"

	"opsplatform-gke-version-backend/models"
)

// scrapedNode：scraper 收集到的、即将写库的 node 信息
type scrapedNode struct {
	NodepoolName string
	NodeName     string
	Zone         string
	Version      string
	GCPCreatedAt time.Time
}

// SaveNodes：把单个集群的 node 全量覆盖写库。
// 策略：事务内 DELETE WHERE cluster_id=? 再 INSERT 全部，
// 保证查询时永远看到一致的快照，部分失败回滚不污染 DB。
//
// 跟 cluster_snapshots 行为对齐——每次 scrape 该集群时调一次。
func SaveNodes(db *sql.DB, clusterID int, scraped []scrapedNode) error {
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // 已 Commit 的话 Rollback 是 no-op

	if _, err := tx.Exec(`DELETE FROM nodes WHERE cluster_id=?`, clusterID); err != nil {
		return fmt.Errorf("delete old nodes: %w", err)
	}

	if len(scraped) == 0 {
		return tx.Commit() // 空也算成功（节点池可能临时缩容到 0）
	}

	stmt, err := tx.Prepare(`INSERT INTO nodes
		(cluster_id, nodepool_name, node_name, zone, version, gcp_created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, n := range scraped {
		if _, err := stmt.Exec(
			clusterID, n.NodepoolName, n.NodeName, n.Zone, n.Version, n.GCPCreatedAt, now,
		); err != nil {
			return fmt.Errorf("insert node %s: %w", n.NodeName, err)
		}
	}
	return tx.Commit()
}

// ListNodesByCluster：handler 查询用。
// 返回该集群所有 node，按 nodepool_name + gcp_created_at（老的在前）排序。
func ListNodesByCluster(db *sql.DB, clusterID int) ([]models.Node, error) {
	rows, err := db.Query(`SELECT id, cluster_id, nodepool_name, node_name, zone, version,
		gcp_created_at, last_seen_at
		FROM nodes WHERE cluster_id=?
		ORDER BY nodepool_name ASC, gcp_created_at ASC`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.Node{}
	for rows.Next() {
		var n models.Node
		if err := rows.Scan(&n.ID, &n.ClusterID, &n.NodepoolName, &n.NodeName,
			&n.Zone, &n.Version, &n.GCPCreatedAt, &n.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
