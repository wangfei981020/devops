package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
	"ops-alert-backend/notify"
)

// 后台常驻任务：数据源探测、未认领升级、保留期清理。
//
// 都由持有租约的那个副本执行，与检测同一个租约——
// 分开两个租约会出现「A 副本在检测、B 副本在升级」，
// 而升级要读检测刚写的数据，跨副本的时序很难推理。

// RunTasks 启动后台任务循环。
func (e *Engine) RunTasks(ctx context.Context) {
	probeTick := time.NewTicker(60 * time.Second)
	escalateTick := time.NewTicker(30 * time.Second)
	cleanupTick := time.NewTicker(6 * time.Hour)
	reportT := time.NewTicker(reportTick)
	defer probeTick.Stop()
	defer escalateTick.Stop()
	defer cleanupTick.Stop()
	defer reportT.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-probeTick.C:
			e.ifLeader(ctx, "probe", e.probeAll)
		case <-escalateTick.C:
			e.ifLeader(ctx, "escalate", e.escalateAll)
		case <-cleanupTick.C:
			e.ifLeader(ctx, "cleanup", e.cleanupAll)
		case <-reportT.C:
			// ⚠️ 必须走 ifLeader。两副本各发一份的话，收件人会收到两封
			// 一模一样的日报，很容易被当成"系统在乱发"进而被整个关掉。
			e.ifLeader(ctx, "report", e.runReports)
		}
	}
}

// ifLeader 只在持有租约时执行。不续租——续租由检测循环负责，
// 这里只读状态，避免两处都写租约互相顶掉。
func (e *Engine) ifLeader(ctx context.Context, job string, fn func(context.Context)) {
	var holder string
	p := e.st.Platform("task_lease")
	if err := p.QueryRow(ctx, `SELECT holder FROM leases WHERE name = ? AND expires_at > NOW(3)`,
		e.cfg.LeaseName).Scan(&holder); err != nil {
		if err != sql.ErrNoRows {
			logx.J("engine", "task_lease_error", map[string]any{"job": job, "error": err.Error()})
		}
		return
	}
	if holder != e.holder {
		return
	}
	fn(ctx)
}

// probeAll 探测所有数据源并落库。
//
// 探测结果落库是为了让「不可达」成为界面上的显性状态。
// 只在点"测试连接"时探测的话，坏掉的数据源在没人点的那几天里
// 表现为「一直没有告警」——正是本产品要根治的静默失明。
func (e *Engine) probeAll(ctx context.Context) {
	_, _ = store.ForEachTenant(ctx, e.st, "probe", func(sc *store.Scoped, _ store.TenantID) error {
		rows, err := sc.Query(`SELECT id FROM datasources WHERE tenant_id = ? AND deleted_at IS NULL`)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()

		for _, id := range ids {
			ad, name, err := e.openDatasource(sc, id)
			if err != nil {
				e.saveProbe(sc, id, "down", 0, err.Error())
				continue
			}
			started := time.Now()
			pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err = ad.Probe(pctx)
			cancel()
			ms := int(time.Since(started).Milliseconds())
			if err != nil {
				e.saveProbe(sc, id, "down", ms, err.Error())
				logx.J("engine", "datasource_down", map[string]any{"id": id, "name": name, "error": err.Error()})
				continue
			}
			e.saveProbe(sc, id, "up", ms, "")
		}
		return nil
	})
}

func (e *Engine) saveProbe(sc *store.Scoped, id int64, status string, ms int, errMsg string) {
	if _, err := sc.Exec(`UPDATE datasources SET status=?, probe_at=NOW(3), probe_ms=?, probe_error=?
		WHERE tenant_id = ? AND id = ?`, status, ms, truncate(errMsg, 500), id); err != nil {
		logx.J("engine", "probe_save_error", map[string]any{"id": id, "error": err.Error()})
	}
}

// escalateAll 处理未认领超时的事件。
//
// 一期不做值班排班，升级链指向联系人组；将来加排班时把
// group_id 换成「当班人」即可，这段逻辑不用重写。
func (e *Engine) escalateAll(ctx context.Context) {
	_, _ = store.ForEachTenant(ctx, e.st, "escalate", func(sc *store.Scoped, _ store.TenantID) error {
		rows, err := sc.Query(`SELECT i.id, i.title, i.severity, i.labels, i.first_at, r.escalation
			FROM incidents i
			JOIN routes r ON r.id = (
				SELECT n.route_id FROM notifications n
				WHERE n.tenant_id = i.tenant_id AND n.incident_id = i.id AND n.route_id IS NOT NULL
				ORDER BY n.id LIMIT 1)
			WHERE i.tenant_id = ? AND i.status = 'firing' AND i.acked_at IS NULL
			  AND r.escalation IS NOT NULL`)
		if err != nil {
			return err
		}
		defer rows.Close()

		type pending struct {
			id       int64
			title    string
			severity string
			labels   []byte
			firstAt  time.Time
			esc      []byte
		}
		var list []pending
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.title, &p.severity, &p.labels, &p.firstAt, &p.esc); err == nil {
				list = append(list, p)
			}
		}

		for _, p := range list {
			var levels []struct {
				AfterSec    int     `json:"after_sec"`
				GroupID     int64   `json:"group_id"`
				NotifierIDs []int64 `json:"notifier_ids"`
			}
			if err := json.Unmarshal(p.esc, &levels); err != nil {
				continue
			}
			elapsed := int(time.Since(p.firstAt).Seconds())
			for lvl, l := range levels {
				if elapsed < l.AfterSec {
					break
				}
				// 每一级只升一次：查这一级有没有发过。
				var done int
				if err := sc.QueryRow(`SELECT COUNT(*) FROM notifications
					WHERE tenant_id = ? AND incident_id = ? AND escalation_level = ?`,
					p.id, lvl+1).Scan(&done); err != nil || done > 0 {
					continue
				}
				labels := map[string]string{}
				_ = json.Unmarshal(p.labels, &labels)
				e.sendEscalation(ctx, sc, p.id, lvl+1, l.NotifierIDs, notify.Message{
					Title:    "[未认领升级] " + p.title,
					Severity: p.severity,
					Fields: []notify.Field{
						{Key: "已持续", Value: fmt.Sprintf("%d 分钟无人认领", elapsed/60)},
						{Key: "升级级别", Value: fmt.Sprintf("第 %d 级", lvl+1)},
					},
				})
			}
		}
		return nil
	})
}

func (e *Engine) sendEscalation(ctx context.Context, sc *store.Scoped, incidentID int64,
	level int, notifierIDs []int64, msg notify.Message,
) {
	for _, nid := range notifierIDs {
		var typ string
		var enc sql.NullString
		if err := sc.QueryRow(`SELECT type, config_enc FROM notifiers
			WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, nid).Scan(&typ, &enc); err != nil {
			continue
		}
		plain := ""
		if enc.Valid && enc.String != "" {
			var err error
			plain, err = e.cipher.Decrypt(enc.String)
			if err != nil {
				continue
			}
		}
		sender, err := notify.New(typ, []byte(plain))
		if err != nil {
			continue
		}
		started := time.Now()
		sendErr := sender.Send(ctx, msg)
		status, errMsg := "sent", ""
		if sendErr != nil {
			status, errMsg = "failed", sendErr.Error()
		}
		var sentAt any
		if status == "sent" {
			sentAt = time.Now()
		}
		if _, err := sc.Insert(`INSERT INTO notifications
			(tenant_id, incident_id, notifier_id, escalation_level, status, attempts, duration_ms, error, sent_at)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`,
			incidentID, nid, level, status, int(time.Since(started).Milliseconds()),
			truncate(errMsg, 500), sentAt); err != nil {
			logx.J("engine", "escalation_record_error", map[string]any{"error": err.Error()})
		}
		e.addTimeline(sc, incidentID, "escalated", "",
			fmt.Sprintf("升级到第 %d 级（%s）：%s", level, typ, statusText(status, errMsg)))
	}
}

func statusText(status, errMsg string) string {
	if status == "sent" {
		return "已送达"
	}
	return "投递失败：" + errMsg
}

// cleanupAll 按保留期清理历史数据。
//
// 分批删而不是一条 DELETE：一次删几百万行会长时间持锁，
// 表现为「凌晨某个点全站接口变慢」，而且很难联想到清理任务。
func (e *Engine) cleanupAll(ctx context.Context) {
	_, _ = store.ForEachTenant(ctx, e.st, "cleanup", func(sc *store.Scoped, _ store.TenantID) error {
		// rule_runs 不在这里：它的时间列是 started_at 不是 created_at，
		// 混进来会每次都报 Unknown column，而清理任务的报错没人盯着。
		batches := []struct {
			table string
			days  int
		}{
			{"decision_traces", 7},
			{"incident_events", 90},
			{"notifications", 90},
		}
		for _, b := range batches {
			for i := 0; i < 20; i++ { // 每轮最多删 20 批，剩下的下次再来
				res, err := sc.Exec(fmt.Sprintf(
					`DELETE FROM %s WHERE tenant_id = ? AND created_at < DATE_SUB(NOW(3), INTERVAL ? DAY) LIMIT 1000`,
					b.table), b.days)
				if err != nil {
					logx.J("engine", "cleanup_error", map[string]any{"table": b.table, "error": err.Error()})
					break
				}
				n, _ := res.RowsAffected()
				if n == 0 {
					break
				}
			}
		}
		// rule_runs 用 started_at 而不是 created_at，单独处理。
		for i := 0; i < 20; i++ {
			res, err := sc.Exec(`DELETE FROM rule_runs WHERE tenant_id = ?
				AND started_at < DATE_SUB(NOW(3), INTERVAL 30 DAY) LIMIT 1000`)
			if err != nil {
				break
			}
			if n, _ := res.RowsAffected(); n == 0 {
				break
			}
		}
		return nil
	})
}
