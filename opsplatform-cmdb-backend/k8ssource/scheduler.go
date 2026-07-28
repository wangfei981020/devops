package k8ssource

import (
	"context"
	"database/sql"
	"time"

	"opsplatform-cmdb-backend/logx"
)

// DefaultSyncIntervalSec 默认全量同步周期。数据新鲜度判定（handlers.SyncState）按它算 stale 阈值，
// 两处必须一致，所以定在这里由调用方引用，不要各写各的字面量。
const DefaultSyncIntervalSec = 120

// StartScheduler 周期全量同步所有启用集群（阶段3）。每 intervalSec 一轮，逐集群串行、各自超时隔离。
// 启动后先等 20s（让进程/DB 就绪）再首轮。
func StartScheduler(db *sql.DB, pool *Pool, intervalSec int) {
	if intervalSec <= 0 {
		intervalSec = 120
	}
	time.Sleep(20 * time.Second)
	for {
		syncAll(db, pool)
		time.Sleep(time.Duration(intervalSec) * time.Second)
	}
}

func syncAll(db *sql.DB, pool *Pool) {
	rows, err := db.Query(`SELECT id, COALESCE(nodepool_label,'') FROM k8s_clusters WHERE enabled=1`)
	if err != nil {
		logx.J("k8s_sched", "list_clusters_err", map[string]any{"err": err.Error()})
		return
	}
	type cl struct {
		id    int
		label string
	}
	var cls []cl
	for rows.Next() {
		var x cl
		if err := rows.Scan(&x.id, &x.label); err == nil {
			cls = append(cls, x)
		}
	}
	rows.Close()

	for _, x := range cls {
		cs, err := pool.ClientFor(x.id)
		if err != nil {
			logx.J("k8s_sched", "client_err", map[string]any{"cluster_id": x.id, "err": err.Error()})
			continue
		}
		dc, err := pool.DynamicFor(x.id)
		if err != nil {
			logx.J("k8s_sched", "dynamic_err", map[string]any{"cluster_id": x.id, "err": err.Error()})
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		results := SyncCluster(ctx, db, cs, dc, x.id, x.label)
		cancel()
		failed := 0
		for _, r := range results {
			if r.Err != nil {
				failed++
			}
		}
		logx.J("k8s_sched", "synced", map[string]any{"cluster_id": x.id, "resources": len(results), "failed": failed})
	}
}
