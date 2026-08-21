package handlers

import (
	"database/sql"
	"errors"
	"os"
	"time"

	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// 数据源同步进度的跨副本存取。
//
// # 为什么不能放在进程内
//
// 原实现是 `syncStore map[int]*syncState`。多副本下：
//
//   - 前端轮询进度会打到**没跑这次同步的那个 Pod** → 显示"没在同步"，
//     用户以为失败了，再点一次
//   - "该数据源正在同步中"的互斥只在单个 Pod 内有效
//
// 两个症状都只在扩容后出现，本地怎么点都是对的。

// syncStaleAfter 心跳多久没更新就认为跑它的副本已经死了。
//
//	定得比一轮同步的最长间隔宽裕：同步过程中每处理一个域名都会刷心跳，
//	但单个域名可能因为厂商 API 慢而卡上一阵。
//	太短会把还活着的同步误判成死的，于是两个副本同时跑同一个数据源。
const syncStaleAfter = 3 * time.Minute

// replicaOwner 本副本标识，用于在进度里记"是谁在跑"。
func replicaOwner() string {
	if v := os.Getenv("POD_NAME"); v != "" {
		return v
	}
	h, _ := os.Hostname()
	return h
}

type syncProgress struct {
	Running                     bool
	Total, Done                 int
	Synced, Records, Imported   int
	Stale                       int
	Err, Owner                  string
	StartedAt, FinishedAt, Beat sql.NullTime
	Started                     bool
}

// live 判断这条进度是不是真的还在跑。
//
//	⚠️ 只看 running 是不够的：副本被杀时 running 会永远停在 1。
//	必须结合心跳新鲜度 —— 否则界面永远显示"同步中"，
//	而且因为互斥判据也是它，这个数据源再也同步不了了。
func (p syncProgress) live(now time.Time) bool {
	if !p.Running {
		return false
	}
	return p.Beat.Valid && now.Sub(p.Beat.Time) < syncStaleAfter
}

// loadSyncProgress 读进度。第二个返回值表示这条记录是否存在。
func loadSyncProgress(sc *store.Scoped, sourceID int) (syncProgress, bool, error) {
	var p syncProgress
	err := sc.QueryRow(`SELECT running, total, done, synced, records, imported, stale, err, owner,
		started_at, finished_at, updated_at
		FROM sync_progress WHERE tenant_id = ? AND source_id = ?`, sourceID).
		Scan(&p.Running, &p.Total, &p.Done, &p.Synced, &p.Records, &p.Imported, &p.Stale,
			&p.Err, &p.Owner, &p.StartedAt, &p.FinishedAt, &p.Beat)
	if errors.Is(err, sql.ErrNoRows) {
		return syncProgress{}, false, nil
	}
	if err != nil {
		return syncProgress{}, false, err
	}
	p.Started = true
	return p, true, nil
}

// beginSync 抢占某数据源的同步资格。
//
// 返回 false 表示已经有人在跑（且心跳新鲜）。
//
//	用 INSERT ... ON DUPLICATE KEY UPDATE 一条语句完成"检查 + 占位"，
//	避免先查后写之间被另一个副本插进来。
//	条件里的心跳判断让"跑到一半死掉"的记录能被接管。
func beginSync(sc *store.Scoped, sourceID int, owner string) (bool, error) {
	res, err := sc.Insert(`INSERT INTO sync_progress
			(tenant_id, source_id, running, total, done, synced, records, imported, stale, err, owner, started_at, finished_at)
		VALUES (?, ?, 1, 0, 0, 0, 0, 0, 0, '', ?, NOW(), NULL)
		ON DUPLICATE KEY UPDATE
			running     = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, 1, running),
			owner       = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, VALUES(owner), owner),
			started_at  = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, NOW(), started_at),
			finished_at = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, NULL, finished_at),
			total       = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, 0, total),
			done        = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, 0, done),
			synced      = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, 0, synced),
			records     = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, 0, records),
			imported    = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, 0, imported),
			stale       = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, 0, stale),
			err         = IF(running = 0 OR updated_at < NOW() - INTERVAL ? SECOND, '', err)`,
		sourceID, owner,
		int(syncStaleAfter.Seconds()), int(syncStaleAfter.Seconds()), int(syncStaleAfter.Seconds()),
		int(syncStaleAfter.Seconds()), int(syncStaleAfter.Seconds()), int(syncStaleAfter.Seconds()),
		int(syncStaleAfter.Seconds()), int(syncStaleAfter.Seconds()), int(syncStaleAfter.Seconds()),
		int(syncStaleAfter.Seconds()), int(syncStaleAfter.Seconds()))
	if err != nil {
		return false, err
	}
	_ = res
	// 回查确认占位的是不是自己 —— 与 cluster.Mutex 同样的理由：
	// ON DUPLICATE KEY UPDATE 的 rows-affected 区分不出"我抢到了"和"值没变"。
	p, _, err := loadSyncProgress(sc, sourceID)
	if err != nil {
		return false, err
	}
	if p.Owner != owner {
		logx.J("dns_sync", "already_running", map[string]any{
			"source_id": sourceID, "owner": p.Owner, "note": "另一个副本正在同步该数据源",
		})
		return false, nil
	}
	return true, nil
}

// bumpSync 刷计数与心跳。
//
//	每次都带上 running=1：进度更新本身就是"我还活着"的证据，
//	省掉一次单独的心跳写。
func bumpSync(sc *store.Scoped, sourceID int, f func(*syncProgress)) {
	var d syncProgress
	f(&d)
	if _, err := sc.Exec(`UPDATE sync_progress SET
			total = total + ?, done = done + ?, synced = synced + ?,
			records = records + ?, imported = imported + ?, stale = stale + ?
		WHERE tenant_id = ? AND source_id = ?`,
		d.Total, d.Done, d.Synced, d.Records, d.Imported, d.Stale, sourceID); err != nil {
		// 进度写不进去不该中断同步本身 —— 同步是正事，进度是观感。
		// 但要留日志，否则"进度不动"会被当成同步卡住。
		logx.J("dns_sync", "progress_write_fail", map[string]any{
			"source_id": sourceID, "err": err.Error(),
		})
	}
}

// setSyncStale 直接设置失效域名数（这个值是算出来的，不是累加的）。
func setSyncStale(sc *store.Scoped, sourceID, stale int) {
	if _, err := sc.Exec(`UPDATE sync_progress SET stale = ? WHERE tenant_id = ? AND source_id = ?`,
		stale, sourceID); err != nil {
		logx.J("dns_sync", "progress_write_fail", map[string]any{"source_id": sourceID, "err": err.Error()})
	}
}

// finishSync 收尾。errMsg 为空表示成功。
func finishSync(sc *store.Scoped, sourceID int, errMsg string) {
	if _, err := sc.Exec(`UPDATE sync_progress SET running = 0, err = ?, finished_at = NOW()
		WHERE tenant_id = ? AND source_id = ?`, truncate(errMsg, 500), sourceID); err != nil {
		logx.J("dns_sync", "progress_finish_fail", map[string]any{
			"source_id": sourceID, "err": err.Error(),
			"warn": "进度停在运行中，界面会一直显示同步中",
		})
	}
}
