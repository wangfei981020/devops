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
		mc, mcErr := pool.MetadataFor(x.id)
		if mcErr != nil {
			mc = nil // 取不到不影响其它资源采集，Secret 名录本轮跳过
		}
		results := SyncCluster(ctx, db, cs, dc, mc, x.id, x.label)
		cancel()
		// 失败的资源必须逐条打出来。原先只打 `failed:3`，光看日志根本不知道是哪三类、
		// 为什么失败——详情虽然写进了 k8s_sync_state 表，但排障时人是先看日志的，
		// 「3 类失败」这种计数除了让人去查库猜，没有任何用处。
		failedItems := []string{}
		for _, r := range results {
			if r.Err != nil {
				failedItems = append(failedItems, r.Resource)
				logx.J("k8s_sched", "resource_failed", map[string]any{
					"cluster_id": x.id, "resource": r.Resource, "err": truncErr(r.Err.Error()),
				})
			}
		}
		logx.J("k8s_sched", "synced", map[string]any{
			"cluster_id": x.id, "resources": len(results),
			"failed": len(failedItems), "failed_resources": failedItems,
		})
	}
}
