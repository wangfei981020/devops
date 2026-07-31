// 节点版本变更事件记录。
//
// k8s_nodes 是覆盖式的当前状态表——节点从 1.33 换成 1.35 的那一刻不留痕迹。
// 这个文件负责在覆盖之前把差异记成流水，用来还原「一次节点池升级的真实节奏」：
// 批次多大、批次之间隔多久、最慢的那台花了多久。这是把 UAT 实测外推到生产窗口的唯一依据。
//
// ⚠️ GKE 升级是销毁重建而非原地升级，节点名会变。所以主要事件是 added/removed，
// 不是 version_changed（后者留给 k3s 这类可能原地升 kubelet 的集群）。
package k8ssource

import (
	"database/sql"
	"time"

	"opsplatform-cmdb-backend/logx"
)

// nodeVersionRef 一个节点在某一轮采集时的版本归属。
type nodeVersionRef struct {
	Pool    string
	Version string
}

// recordNodeVersionEvents 比对上一轮与本轮的节点集合，把差异写进 k8s_node_version_events。
//
// 必须在 writeRows 覆盖 k8s_nodes 之前调用。
// 任何失败都只记日志不返回错误：这是辅助流水，不能因为它写不进去就让整轮节点采集失败。
func recordNodeVersionEvents(db *sql.DB, cid int, current map[string]nodeVersionRef) {
	prev, err := loadPrevNodeVersions(db, cid)
	if err != nil {
		logx.J("k8s_sync", "node_events_load_failed", map[string]any{
			"cluster_id": cid, "err": err.Error(),
			"hint": "本轮节点版本变更未记录，升级耗时分析会缺这一段",
		})
		return
	}

	// 首次采集：库里一条都没有，此时全部节点都会被判成 added。
	// 那不是变更而是建立基线，记下来只会在升级时间线里凭空多出一堆噪声。
	if len(prev) == 0 {
		logx.J("k8s_sync", "node_events_baseline", map[string]any{
			"cluster_id": cid, "nodes": len(current),
			"note": "首次采集，建立基线不记变更事件",
		})
		return
	}

	// 采集成功但一个节点都没返回：真发生了是天塌了的事，但更可能是上游异常。
	// 若照记，会一次性写入十几条 removed，把升级时间线彻底污染。
	// 所以跳过记录但大声报警——真出事时这条 WARN 比那些 removed 事件更容易被看见。
	if len(current) == 0 {
		logx.J("k8s_sync", "node_events_empty_snapshot", map[string]any{
			"cluster_id": cid, "prev_nodes": len(prev),
			"hint": "本轮采集到 0 个节点，已跳过变更记录以免污染时间线。若集群确实全挂，请看节点健康告警",
		})
		return
	}

	now := time.Now()
	type ev struct {
		node, pool, kind, from, to string
	}
	var evs []ev

	for name, cur := range current {
		old, existed := prev[name]
		switch {
		case !existed:
			evs = append(evs, ev{name, cur.Pool, "added", "", cur.Version})
		case old.Version != cur.Version:
			// 原地升级：GKE 走不到这里，k3s/自建集群可能
			evs = append(evs, ev{name, cur.Pool, "version_changed", old.Version, cur.Version})
		}
	}
	for name, old := range prev {
		if _, still := current[name]; !still {
			evs = append(evs, ev{name, old.Pool, "removed", old.Version, ""})
		}
	}
	if len(evs) == 0 {
		return
	}

	for _, e := range evs {
		// scope 显式写 'node'：同一张表还存控制面变更（scope=control_plane），
		// 两者的 detected_at 精度差一个数量级，靠列默认值区分太隐蔽
		if _, err := db.Exec(`INSERT INTO k8s_node_version_events
			(cluster_id,scope,node_name,pool,event,from_version,to_version,detected_at)
			VALUES (?,'node',?,?,?,?,?,?)`,
			cid, e.node, e.pool, e.kind, e.from, e.to, now); err != nil {
			logx.J("k8s_sync", "node_event_insert_failed", map[string]any{
				"cluster_id": cid, "node": e.node, "event": e.kind, "err": err.Error(),
			})
		}
	}

	// 节点增删在稳态下几乎不发生，一发生就值得知道——升级、扩缩容、auto-repair 静默重建
	// 都会在这里现形。auto-repair 重建此前只能靠 GCP operations.list 事后查，
	// 而那个接口保留期只有两周量级。
	logx.J("k8s_sync", "node_version_events", map[string]any{
		"cluster_id": cid, "events": len(evs),
		"prev_nodes": len(prev), "current_nodes": len(current),
	})
}

func loadPrevNodeVersions(db *sql.DB, cid int) (map[string]nodeVersionRef, error) {
	rows, err := db.Query(`SELECT name, COALESCE(pool,''), COALESCE(kubelet_version,'')
	                         FROM k8s_nodes WHERE cluster_id=?`, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]nodeVersionRef{}
	for rows.Next() {
		var name string
		var r nodeVersionRef
		if rows.Scan(&name, &r.Pool, &r.Version) != nil {
			continue
		}
		out[name] = r
	}
	return out, rows.Err()
}
