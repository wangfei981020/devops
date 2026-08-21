// Package dataview 是**给人看的接口和给 AI 看的工具共用的那一层查询**。
//
// # 为什么单独一个包
//
// 这些函数原来是 internal/api 里 *Server 的方法（文件名叫 mcp_data.go），
// 但实测它们**一个都没用到 Server** —— 只是写成方法罢了。
// 而其中三个（自检、事件详情、噪音榜）同时被 REST 接口和 MCP 工具调用。
//
// 把 MCP 挪进 ee/ 之后，如果这一层还留在 internal/api，
// ee 包就要反向依赖 api，形成循环；而"各写一份"是更坏的选项——
// 同一个资源两条读路径必然分叉，这在事件详情上已经栽过一次
// （HTTP 和 MCP 各查一遍，加字段只改了一边，界面上字段凭空消失）。
//
// 🔴 **本包属于社区版**。它被 EE 的 MCP 工具复用，但它自己不是 EE 代码 ——
// 把它挪进 ee/ 会让 REST 接口一起变成企业版功能。
package dataview

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"ops-alert-backend/internal/store"
)

// 这些函数是 REST 接口与 MCP 工具**共用**的数据层。
//
// 不让两边各查一遍：同一个问题两份实现迟早漂移，而漂移的表现是
// "界面上说正常，AI 说有问题"——到时候没人知道该信哪个。

func SelfCheck(sc *store.Scoped) (map[string]any, error) {
	var dsTotal, dsDown, ruleTotal, ruleFailing, notifyFailed int
	if err := sc.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'down'),0) FROM datasources
		WHERE tenant_id = ? AND deleted_at IS NULL`).Scan(&dsTotal, &dsDown); err != nil {
		return nil, err
	}
	if err := sc.QueryRow(`SELECT COUNT(*), COALESCE(SUM(consecutive_failures > 0),0) FROM rules
		WHERE tenant_id = ? AND enabled = 1 AND deleted_at IS NULL`).Scan(&ruleTotal, &ruleFailing); err != nil {
		return nil, err
	}
	if err := sc.QueryRow(`SELECT COUNT(*) FROM notifications
		WHERE tenant_id = ? AND status = 'failed' AND created_at > DATE_SUB(NOW(3), INTERVAL 1 DAY)`).
		Scan(&notifyFailed); err != nil {
		return nil, err
	}

	// 连得上但很久没数据 = 静默故障。探测成功不代表数据还在流。
	stale := []string{}
	rows, err := sc.Query(`SELECT name FROM datasources
		WHERE tenant_id = ? AND deleted_at IS NULL AND status = 'up'
		  AND last_data_at IS NOT NULL AND last_data_at < DATE_SUB(NOW(3), INTERVAL 30 MINUTE)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			stale = append(stale, n)
		}
	}

	healthy := dsDown == 0 && ruleFailing == 0 && len(stale) == 0
	return map[string]any{
		"datasources":       map[string]any{"total": dsTotal, "down": dsDown},
		"rules":             map[string]any{"total": ruleTotal, "failing": ruleFailing},
		"notify_failed_24h": notifyFailed,
		"silent_sources":    stale,
		"healthy":           healthy,
		// 给 AI 一句直白结论：它比人更容易把"空"读成"好"
		"conclusion": conclusionText(healthy, dsDown, ruleFailing, len(stale)),
	}, nil
}

func conclusionText(healthy bool, dsDown, ruleFailing, stale int) string {
	if healthy {
		return "检测链路完整：此时「没有告警」可以理解为「确实没有问题」"
	}
	return fmt.Sprintf("检测链路有缺口（%d 个数据源不可达、%d 条规则执行失败、%d 个数据源停止上报）："+
		"此时「没有告警」不能理解为「没有问题」", dsDown, ruleFailing, stale)
}

func IncidentDetail(sc *store.Scoped, id int) (map[string]any, error) {
	var title, severity, status, ackedBy string
	var count int
	var firstAt, lastAt time.Time
	var labels, sample []byte
	var fingerprint string
	err := sc.QueryRow(`SELECT title, severity, status, count, first_at, last_at, acked_by, labels, sample, fingerprint
		FROM incidents WHERE tenant_id = ? AND id = ?`, id).
		Scan(&title, &severity, &status, &count, &firstAt, &lastAt, &ackedBy, &labels, &sample, &fingerprint)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("事件 %d 不存在", id)
	}
	if err != nil {
		return nil, err
	}

	// ⚠️ 时间线和投递都必须**截断**。
	//
	// 一条持续告警可以命中上千次，每次都写一条时间线 + 一条投递记录。
	// 实测有条事件积了 1593 次投递失败：不截断的话详情接口一次吐几 MB，
	// 界面右栏被一模一样的报错刷出几十屏 —— 而"失败了 1593 次"这个**结论**
	// 反而被淹没在流水里。结论用 total 单独给，流水只给最近几条。
	var timelineTotal int
	_ = sc.QueryRow(`SELECT COUNT(*) FROM incident_events WHERE tenant_id = ? AND incident_id = ?`, id).Scan(&timelineTotal)
	timeline := []map[string]any{}
	if rows, err := sc.Query(`SELECT kind, actor, message, created_at FROM incident_events
		WHERE tenant_id = ? AND incident_id = ? ORDER BY id DESC LIMIT 50`, id); err == nil {
		defer rows.Close()
		for rows.Next() {
			var kind, actor, msg string
			var at time.Time
			if err := rows.Scan(&kind, &actor, &msg, &at); err == nil {
				timeline = append(timeline, map[string]any{"kind": kind, "actor": actor, "message": msg, "at": at})
			}
		}
	}
	// 查的是倒序（要最近的），显示要正序
	for i, j := 0, len(timeline)-1; i < j; i, j = i+1, j-1 {
		timeline[i], timeline[j] = timeline[j], timeline[i]
	}

	// ⚠️ failed 必须用 COUNT 单独算，**不能数返回的那几条**。
	// 截断之后数出来的是"最近 20 条里失败了几条"，
	// 而横幅要说的是"这条告警一共漏送了多少次"——差着两个数量级。
	var deliveryTotal, failed int
	_ = sc.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status = 'failed'), 0) FROM notifications
		WHERE tenant_id = ? AND incident_id = ?`, id).Scan(&deliveryTotal, &failed)
	deliveries := []map[string]any{}
	if rows, err := sc.Query(`SELECT n.status, n.error, n.duration_ms, n.created_at, t.name, t.type
		FROM notifications n LEFT JOIN notifiers t ON t.id = n.notifier_id
		WHERE n.tenant_id = ? AND n.incident_id = ? ORDER BY n.id DESC LIMIT 20`, id); err == nil {
		defer rows.Close()
		for rows.Next() {
			var st, errMsg string
			var ms int
			var at time.Time
			var name, typ sql.NullString
			if err := rows.Scan(&st, &errMsg, &ms, &at, &name, &typ); err == nil {
				deliveries = append(deliveries, map[string]any{"status": st, "error": errMsg,
					"duration_ms": ms, "at": at, "notifier": name.String, "type": typ.String})
			}
		}
	}
	for i, j := 0, len(deliveries)-1; i < j; i, j = i+1, j-1 {
		deliveries[i], deliveries[j] = deliveries[j], deliveries[i]
	}

	// 相似历史：**同一指纹**过去发生过的事件。
	//
	// 值班时第一个问题是"这个以前出过吗、上次怎么好的"。
	// 这里只按指纹匹配，不做模糊相似度 —— 指纹相同意味着
	// 同一条规则 + 同一组标签，是可以直接类比的；
	// "标题看着像"则经常把不同服务的同名错误凑到一起，反而误导。
	//
	// resolved_at - first_at 就是上次的恢复耗时，比"上次也报过"有用得多。
	similar := []map[string]any{}
	if rows, err := sc.Query(`SELECT id, status, count, first_at, last_at, resolved_at
		FROM incidents
		WHERE tenant_id = ? AND fingerprint = ? AND id <> ?
		ORDER BY first_at DESC LIMIT 5`, fingerprint, id); err == nil {
		defer rows.Close()
		for rows.Next() {
			var sid, scount int
			var sstatus string
			var sfirst, slast time.Time
			var sresolved sql.NullTime
			if err := rows.Scan(&sid, &sstatus, &scount, &sfirst, &slast, &sresolved); err == nil {
				item := map[string]any{"id": sid, "status": sstatus, "count": scount, "first_at": sfirst, "last_at": slast}
				// ⚠️ 没恢复过就**不给** recovery_sec，而不是给 0。
				// 前端拿到 0 会渲染成"0 秒恢复"——把"至今没好"显示成最理想的结果。
				if sresolved.Valid {
					item["recovery_sec"] = int(sresolved.Time.Sub(sfirst).Seconds())
				}
				similar = append(similar, item)
			}
		}
	}

	var lbl map[string]string
	_ = json.Unmarshal(labels, &lbl)
	out := map[string]any{
		"id": id, "title": title, "severity": severity, "status": status, "count": count,
		"first_at": firstAt, "last_at": lastAt, "acked_by": ackedBy, "labels": lbl,
		"sample": RawOrNull(sample), "timeline": timeline, "deliveries": deliveries,
		"fingerprint": fingerprint, "similar": similar,
		"timeline_total": timelineTotal, "delivery_total": deliveryTotal, "delivery_failed": failed,
	}
	if failed > 0 {
		out["delivery_warning"] = fmt.Sprintf("有 %d 次投递失败：这条告警可能没有真正送到人手上", failed)
	}
	return out, nil
}

func TraceOfIncident(sc *store.Scoped, id int) (map[string]any, error) {
	var verdict string
	var steps []byte
	var at time.Time
	err := sc.QueryRow(`SELECT verdict, steps, created_at FROM decision_traces
		WHERE tenant_id = ? AND incident_id = ? ORDER BY id DESC LIMIT 1`, id).
		Scan(&verdict, &steps, &at)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("事件 %d 没有判定链记录（可能已过保留期，或由外部推送产生）", id)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"verdict": verdict, "verdict_text": verdictText(verdict),
		"steps": json.RawMessage(steps), "at": at}, nil
}

// whyNotFired 回答「这条规则为什么没告警」。
//
// 五种原因要能分辨：查询没命中 / 未达持续周期 / 被抑制 / 被静默 / 匹配不到路由。
// 再加一种最容易被忽略的：规则本身在报错——它不会告警，但界面上不会自己喊。
func WhyNotFired(sc *store.Scoped, ruleID int) (map[string]any, error) {
	var name, lastErr string
	var failures int
	var enabled int
	var lastRun sql.NullTime
	err := sc.QueryRow(`SELECT name, enabled, last_run_at, last_error, consecutive_failures
		FROM rules WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, ruleID).
		Scan(&name, &enabled, &lastRun, &lastErr, &failures)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("规则 %d 不存在", ruleID)
	}
	if err != nil {
		return nil, err
	}

	out := map[string]any{"rule_id": ruleID, "rule": name, "last_run_at": nullTime(lastRun)}

	// 按可能性从高到低排除，先给结论再给证据
	switch {
	case enabled == 0:
		out["answer"] = "这条规则已被停用，根本没有在执行"
		return out, nil
	case failures > 0:
		out["answer"] = fmt.Sprintf(
			"这条规则连续 %d 次执行失败，它不会告警。错误：%s", failures, lastErr)
		out["last_error"] = lastErr
		return out, nil
	case !lastRun.Valid:
		out["answer"] = "这条规则从未执行过（刚创建，或调度器没跑起来）"
		return out, nil
	case time.Since(lastRun.Time) > 10*time.Minute:
		out["answer"] = fmt.Sprintf(
			"这条规则上次执行是 %s，已经超过 10 分钟没跑——调度可能停了，先看系统自检",
			lastRun.Time.Format(time.RFC3339))
		return out, nil
	}

	// 规则在正常跑，那就看最近一次判定链
	var verdict string
	var steps []byte
	var at time.Time
	err = sc.QueryRow(`SELECT verdict, steps, created_at FROM decision_traces
		WHERE tenant_id = ? AND rule_id = ? ORDER BY id DESC LIMIT 1`, ruleID).
		Scan(&verdict, &steps, &at)
	if err == sql.ErrNoRows {
		out["answer"] = "规则在正常执行，但还没有留下判定链记录（等下一个执行周期再看）"
		return out, nil
	}
	if err != nil {
		return nil, err
	}

	var parsed []struct {
		Step   string `json:"step"`
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(steps, &parsed)
	answer := verdictText(verdict)
	// 找到第一个没通过的步骤——那就是"卡在哪"
	for _, st := range parsed {
		if !st.OK {
			answer = fmt.Sprintf("卡在「%s」这一步：%s", stepText(st.Step), st.Reason)
			break
		}
	}
	out["answer"] = answer
	out["verdict"] = verdict
	out["at"] = at
	out["steps"] = json.RawMessage(steps)
	return out, nil
}

func NoiseTop(sc *store.Scoped) (map[string]any, error) {
	rows, err := sc.Query(`SELECT r.id, r.name, COUNT(i.id) AS fires,
			COALESCE(SUM(i.false_positive),0) AS fps,
			COALESCE(SUM(HOUR(i.first_at) < 6),0) AS night
		FROM rules r JOIN incidents i ON i.rule_id = r.id AND i.tenant_id = r.tenant_id
		WHERE r.tenant_id = ? AND i.first_at > DATE_SUB(NOW(3), INTERVAL 7 DAY)
		GROUP BY r.id, r.name ORDER BY fires DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var name string
		var fires, fps, night int
		if err := rows.Scan(&id, &name, &fires, &fps, &night); err != nil {
			return nil, err
		}
		fpRate := 0.0
		if fires > 0 {
			fpRate = float64(fps) / float64(fires) * 100
		}
		out = append(out, map[string]any{"rule_id": id, "name": name, "fires_7d": fires,
			"false_positives": fps, "fp_rate": fpRate, "night_fires": night,
			"advice": noiseAdvice(fpRate, fires, night)})
	}
	return map[string]any{"items": out, "count": len(out)}, nil
}

// noiseAdvice 给的是判断依据，不是要执行的动作。
// 系统不会自动改阈值——让系统自己决定少告警，是最不该自动化的一件事。
func noiseAdvice(fpRate float64, fires, night int) string {
	switch {
	case fpRate >= 50:
		return "误报过半：提高阈值或增加持续周期，先用回放验证不会漏报"
	case night > 0 && fires > 20:
		return "深夜有叫醒且总量偏高：考虑夜间提高阈值或降级为提示"
	case fires > 50:
		return "触发量大：检查是否该按分组收敛或加抑制规则"
	default:
		return "保持观察"
	}
}

func verdictText(v string) string {
	return map[string]string{
		"fired":           "已触发并通知",
		"not_fired":       "未触发",
		"suppressed":      "被抑制（存在父事件）",
		"silenced":        "被静默",
		"no_route":        "未匹配到任何路由——这条事件不会通知任何人",
		"delivery_failed": "触发了，但一条都没送出去",
	}[v]
}

func stepText(s string) string {
	return map[string]string{
		"query": "数据查询", "threshold": "阈值判定", "for": "持续判定",
		"inhibit": "抑制检查", "silence": "静默检查", "route": "路由匹配", "deliver": "通知投递",
	}[s]
}

// rawOrNull 把 JSON 列的原始字节按原样透传，空值给 null。
//
// ⚠️ 空字节切片必须给 null 而不是 `{}`：`{}` 在界面上渲染成一个空对象，
// 读起来像"这条记录有标签但标签是空的"，而真相是这一列压根没写过。
func RawOrNull(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}

// nullTime 把可空时间转成 JSON 友好的值。NULL → null，不是零值时间。
// 给零值时间的话前端会渲染成 0001-01-01，看起来像一条真实但荒谬的数据。
func nullTime(t sql.NullTime) any {
	if !t.Valid {
		return nil
	}
	return t.Time
}
