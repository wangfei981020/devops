package engine

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ops-alert-backend/datasource"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
)

// TraceStep 是判定链的一步。
//
// 存的是**当时**的判据与配置：事后改了阈值不影响回溯。
// 这是「判定可解释」的数据基础——市面上的告警系统只告诉你触发了，
// 不告诉你凭什么触发，更回答不了「我以为该响的怎么没响」。
type TraceStep struct {
	Step   string         `json:"step"`
	OK     bool           `json:"ok"`
	Reason string         `json:"reason,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

const (
	stepQuery      = "query"
	stepThreshold  = "threshold"
	stepFor        = "for"
	stepInhibit    = "inhibit"
	stepSilence    = "silence"
	stepRoute      = "route"
	stepDeliver    = "deliver"
	verdictFired   = "fired"
	verdictNoFire  = "not_fired"
	verdictSilence = "silenced"
	verdictInhibit = "suppressed"
	verdictNoRoute = "no_route"
	verdictFailed  = "delivery_failed"
)

// executeRule 跑一条规则的一个周期。
//
// 无论成功失败都会写 rule_runs 与 rules.last_run_at：
// 「上次执行时间」停在半天前，是规则悄悄死掉的第一征兆。
func (e *Engine) executeRule(ctx context.Context, tenant store.TenantID, r dueRule) {
	jobCtx := store.ForJob(ctx, tenant, "detect")
	sc, err := e.st.Tenant(jobCtx)
	if err != nil {
		logx.J("engine", "no_tenant_ctx", map[string]any{"rule_id": r.ID, "error": err.Error()})
		return
	}

	started := time.Now()
	var steps []TraceStep

	adapter, dsName, err := e.openDatasource(sc, r.DSID)
	if err != nil {
		e.finishRun(sc, r, started, "error", 0, 0, 0, err)
		e.saveTrace(sc, r.ID, 0, "", verdictNoFire, append(steps, TraceStep{
			Step: stepQuery, OK: false, Reason: "数据源不可用: " + err.Error(),
		}))
		return
	}

	var spec ruleSpec
	if len(r.Spec) > 0 {
		if err := json.Unmarshal(r.Spec, &spec); err != nil {
			e.finishRun(sc, r, started, "error", 0, 0, 0, fmt.Errorf("规则配置无法解析: %w", err))
			return
		}
	}
	groupBy := decodeStrings(r.GroupBy)

	to := time.Now()
	from := to.Add(-time.Duration(r.Lookback) * time.Second)
	q := datasource.Query{
		Expr:    spec.Query,
		From:    from,
		To:      to,
		Limit:   spec.Limit,
		GroupBy: groupBy,
	}

	res, err := adapter.Query(jobCtx, q)
	if err != nil {
		// 查询失败是显性故障：写 last_error、累加连续失败次数。
		// 界面上「7 天触发 0 次」必须能和「查询一直在失败」区分开，
		// 否则安静会被读成太平。
		e.finishRun(sc, r, started, "error", 0, 0, 0, err)
		e.saveTrace(sc, r.ID, 0, "", verdictNoFire, append(steps, TraceStep{
			Step: stepQuery, OK: false, Reason: err.Error(),
			Detail: map[string]any{"datasource": dsName, "expr": spec.Query},
		}))
		return
	}

	steps = append(steps, TraceStep{
		Step: stepQuery, OK: true,
		Detail: map[string]any{
			"datasource": dsName, "expr": spec.Query, "hits": res.Total,
			"took_ms": res.TookMs, "truncated": res.Truncated,
			"window": fmt.Sprintf("%s ~ %s", from.Format(time.RFC3339), to.Format(time.RFC3339)),
		},
	})

	// 分组：无分组维度时整条规则算一个组（键为空串）。
	groups := res.Groups
	if len(groupBy) == 0 {
		groups = map[string]int{"": int(res.Total)}
	}

	// 期望分组（心跳场景）：登记过的分组即使这轮一条都没命中也要参与判定，
	// 否则「该出现却没出现」永远发现不了——没有数据就没有分组键。
	for _, g := range spec.ExpectedGroups {
		if _, ok := groups[g]; !ok {
			groups[g] = 0
		}
	}

	// ⚠️ 一条数据都没有时，如果没有任何分组参与判定，这一轮就等于什么都没做。
	//
	// 对「日志缺失」规则这是致命的：服务从一开始就没产出过日志时，
	// 查询结果为空 → 没有分组键 → 循环空转 → **永远不会告警**，
	// 而这恰恰是最该告警的情况。开了无数据告警的规则同理。
	// 兜一个空分组键进去，让判定至少发生一次。
	if len(groups) == 0 && (r.Kind == kindLogAbsent || r.NoDataAlert) {
		groups[""] = 0
	}

	// ⚠️ 上一轮见过的分组也必须参与判定，哪怕这轮查询一条都没返回。
	//
	// 分组键是从数据里长出来的：日志一停，wallet-api 这个分组就从结果里
	// 消失了，于是它的 miss_streak 永远不再累加，事件永远不会自动恢复——
	// 表现是「问题早好了，告警还挂在值班台上」，而且不报任何错。
	// 这个 bug 在本地验证时才现形：把假数据源改成返回 0 条，事件一直不恢复。
	for _, g := range e.knownGroups(sc, r.ID) {
		if _, ok := groups[g]; !ok {
			groups[g] = 0
		}
	}

	// 量突变要额外查一次基线窗口。查失败就整条规则记失败——
	// 拿不到基线时"按 0 基线算倍数"会让每个周期都判成突变。
	var baseline map[string]int
	if needsBaseline(r.Kind) {
		baseline, err = e.queryBaseline(jobCtx, adapter, r, spec, groupBy, to)
		if err != nil {
			e.finishRun(sc, r, started, "error", int(res.Total), len(groups), 0,
				fmt.Errorf("基线窗口查询失败: %w", err))
			return
		}
	}

	absent := r.Kind == kindLogAbsent
	made := 0
	for key, count := range groups {
		groupHits := hitsOfGroup(res.Hits, groupBy, key)
		v, err := e.evaluateGroup(jobCtx, r, spec, key, count, groupHits, baseline)
		if err != nil {
			// 判定本身出错（字段提不到、类型未实现）是显性故障：
			// 当成"未命中"会让规则安静地永远不触发。
			e.finishRun(sc, r, started, "error", int(res.Total), len(groups), made, err)
			e.saveTrace(sc, r.ID, 0, fingerprint(r.ID, key), verdictNoFire,
				append(steps, TraceStep{Step: stepThreshold, OK: false, Reason: err.Error()}))
			return
		}
		if made >= r.MaxEvents && v.Hit {
			// 超出上限的事件折叠为一条汇总，防止刷屏。
			// 但必须记下来——静默丢弃会让人以为只发生了 20 个分组的问题。
			logx.J("engine", "max_events_reached", map[string]any{
				"rule_id": r.ID, "max": r.MaxEvents, "groups": len(groups),
			})
			break
		}
		if e.handleGroup(jobCtx, sc, r, spec, key, count, v, groupHits, res, steps) {
			made++
		}
	}

	outcome := "ok"
	if res.Total == 0 && !absent {
		outcome = "no_data"
	}
	e.finishRun(sc, r, started, outcome, int(res.Total), len(groups), made, nil)
}

// handleGroup 处理单个分组的判定，返回是否产生/更新了事件。
func (e *Engine) handleGroup(ctx context.Context, sc *store.Scoped, r dueRule, spec ruleSpec,
	groupKey string, count int, v groupVerdict, groupHits []datasource.Hit,
	res *datasource.Result, base []TraceStep,
) bool {
	hit := v.Hit
	steps := append([]TraceStep{}, base...)
	detail := map[string]any{"group": groupKey, "kind": r.Kind}
	for k, dv := range v.Detail {
		detail[k] = dv
	}
	steps = append(steps, TraceStep{
		Step: stepThreshold, OK: hit,
		// 判定理由随类型而变：量突变说倍数，字段阈值说分位数值。
		// 统一写成"命中 N 条"会让人以为所有规则都是按条数判的。
		Reason: v.Reason,
		Detail: detail,
	})

	hitStreak, missStreak, err := e.bumpState(sc, r.ID, groupKey, hit)
	if err != nil {
		logx.J("engine", "state_error", map[string]any{"rule_id": r.ID, "error": err.Error()})
		return false
	}

	fp := fingerprint(r.ID, groupKey)

	if !hit {
		// 未命中：够久了就恢复事件。
		if missStreak >= r.Resolve {
			resolved, _ := e.resolveIncident(ctx, sc, r, spec, fp, groupKey)
			if resolved {
				steps = append(steps, TraceStep{Step: stepFor, OK: true,
					Reason: fmt.Sprintf("连续 %d 个周期未命中，事件已恢复", missStreak)})
				e.saveTrace(sc, r.ID, 0, fp, verdictNoFire, steps)
				// 恢复后清掉状态行：这个分组不再参与后续判定，
				// 直到它自己再次出现在数据里。否则每轮都要空跑一遍所有历史分组。
				e.dropState(sc, r.ID, groupKey)
				return false
			}
			// 没有活跃事件却一直未命中（例如规则刚建、或事件早已恢复）：
			// 状态行留着没有意义，同样清掉。
			if missStreak > r.Resolve*3 {
				e.dropState(sc, r.ID, groupKey)
			}
		}
		steps = append(steps, TraceStep{Step: stepFor, OK: false,
			Reason: fmt.Sprintf("未命中（连续 %d 次）", missStreak)})
		e.saveTrace(sc, r.ID, 0, fp, verdictNoFire, steps)
		return false
	}

	if hitStreak < r.ForPeriods {
		// Pending：命中了但还没连续够——单次抖动不叫人。
		steps = append(steps, TraceStep{Step: stepFor, OK: false,
			Reason: fmt.Sprintf("连续命中 %d 次，需要 %d 次", hitStreak, r.ForPeriods),
			Detail: map[string]any{"state": "pending"}})
		e.saveTrace(sc, r.ID, 0, fp, verdictNoFire, steps)
		return false
	}
	steps = append(steps, TraceStep{Step: stepFor, OK: true,
		Reason: fmt.Sprintf("连续命中 %d 次（要求 %d）", hitStreak, r.ForPeriods)})

	// 提取变量：既供通知模板，也并入标签，从而支持按字段值路由
	// （例如 error_code=502 → 网络组）。旧系统的 route_config 靠这个落地。
	vars := extractVars(groupHits, spec.Extract)
	labels := e.buildLabels(r, spec, groupKey)
	for k, val := range vars {
		if val != "" {
			labels[k] = val
		}
	}

	// 抑制：父事件存在时不通知（仍然记录事件）。
	if reason, suppressed := e.checkInhibition(sc, labels); suppressed {
		steps = append(steps, TraceStep{Step: stepInhibit, OK: false, Reason: reason})
		id, _ := e.upsertIncident(sc, r, spec, fp, groupKey, count, labels, vars, res, "suppressed")
		e.saveTrace(sc, r.ID, id, fp, verdictInhibit, steps)
		return true
	}
	steps = append(steps, TraceStep{Step: stepInhibit, OK: true, Reason: "未命中任何抑制规则"})

	// 静默：抑制的是通知不是检测，事件照样记录。
	if reason, silenced := e.checkSilence(sc, labels); silenced {
		steps = append(steps, TraceStep{Step: stepSilence, OK: false, Reason: reason})
		id, _ := e.upsertIncident(sc, r, spec, fp, groupKey, count, labels, vars, res, "suppressed")
		e.saveTrace(sc, r.ID, id, fp, verdictSilence, steps)
		return true
	}
	steps = append(steps, TraceStep{Step: stepSilence, OK: true, Reason: "未命中静默"})

	incidentID, isNew := e.upsertIncident(sc, r, spec, fp, groupKey, count, labels, vars, res, "firing")
	if incidentID == 0 {
		return false
	}

	// 路由匹配对新老事件都要做，判定链才是完整的八步。
	// 早期版本只在新事件时匹配，重复命中的事件判定链只有 5 步——
	// 看的人会以为流程断在那里，而实际上是「没到重复通知时间」。
	// 判定链的价值就在于每一步都说得清，缺步比说"没通知"更糟。
	route := e.matchRoute(sc, labels)
	verdict := verdictFired
	if len(route.NotifierIDs) == 0 {
		// 没有匹配到任何路由且没有兜底 —— 事件会静默消失。
		// 这必须是显性错误，不是"正常没通知"。
		steps = append(steps, TraceStep{Step: stepRoute, OK: false,
			Reason: "未匹配任何路由，且没有兜底路由：该事件不会通知任何人"})
		e.saveTrace(sc, r.ID, incidentID, fp, verdictNoRoute, steps)
		return true
	}
	steps = append(steps, TraceStep{Step: stepRoute, OK: true, Reason: route.Reason,
		Detail: map[string]any{"route_id": route.ID, "notifiers": route.NotifierIDs}})

	// 是否该投递：新事件立刻发；老事件等重复通知间隔到点再发。
	// 没有这一段的话，一条一直不恢复的事件只会通知一次——
	// 值班的人以为已经处理完了，而问题还在持续。
	send, why := isNew, "新事件，立即通知"
	if !isNew {
		send, why = e.repeatDue(sc, incidentID, route.RepeatSec)
	}
	if !send {
		steps = append(steps, TraceStep{Step: stepDeliver, OK: true, Reason: why})
		e.saveTrace(sc, r.ID, incidentID, fp, verdict, steps)
		return true
	}

	results := e.deliver(ctx, sc, incidentID, route.ID, route.NotifierIDs, r, spec, vars, labels, groupHits)
	okCount := 0
	for _, ok := range results {
		if ok {
			okCount++
		}
	}
	steps = append(steps, TraceStep{
		Step: stepDeliver, OK: okCount > 0,
		Reason: fmt.Sprintf("%s；投递 %d 成功 / %d 失败", why, okCount, len(results)-okCount),
		Detail: map[string]any{"results": results},
	})
	if okCount == 0 {
		// 一条都没送出去 = 没人知道这件事。
		// 判定终局记成 delivery_failed，值班台按「通知未送达」显性展示。
		verdict = verdictFailed
	}
	e.saveTrace(sc, r.ID, incidentID, fp, verdict, steps)
	return true
}

// repeatDue 判断是否到了重复通知的时间。
//
// 基准是「最近一次投递成功的时间」，不是事件创建时间：
// 用创建时间的话，一条投递一直失败的事件会被算成"刚通知过"，
// 于是永远等不到重发——恰恰是最需要重发的那种情况。
func (e *Engine) repeatDue(sc *store.Scoped, incidentID int64, repeatSec int) (bool, string) {
	if repeatSec <= 0 {
		return false, "该路由未设置重复通知间隔，仅首次通知"
	}
	var lastSent sql.NullTime
	err := sc.QueryRow(`SELECT MAX(sent_at) FROM notifications
		WHERE tenant_id = ? AND incident_id = ? AND status = 'sent'`, incidentID).Scan(&lastSent)
	if err != nil {
		// 查不到就重发一次：宁可多通知一次，不可让持续故障静音。
		logx.J("engine", "repeat_check_error", map[string]any{"incident_id": incidentID, "error": err.Error()})
		return true, "无法确认上次通知时间，按需要通知处理"
	}
	if !lastSent.Valid {
		return true, "此前没有任何一次投递成功，重新通知"
	}
	elapsed := time.Since(lastSent.Time)
	if elapsed >= time.Duration(repeatSec)*time.Second {
		return true, fmt.Sprintf("距上次通知 %s，已达重复间隔 %ds", elapsed.Round(time.Second), repeatSec)
	}
	return false, fmt.Sprintf("距上次通知 %s，未到重复间隔 %ds，本次不通知",
		elapsed.Round(time.Second), repeatSec)
}

// knownGroups 返回该规则上一轮还活着的分组。
//
// 「活着」= rule_states 里还有行。分组恢复后那一行会被删掉（见 handleGroup），
// 所以这张表只保留活跃分组，不会随时间无限膨胀——
// 一个按 pod 分组的规则，集群里滚动更新几个月就能攒出上万个死分组。
func (e *Engine) knownGroups(sc *store.Scoped, ruleID int64) []string {
	rows, err := sc.Query(`SELECT group_key FROM rule_states
		WHERE tenant_id = ? AND rule_id = ?`, ruleID)
	if err != nil {
		logx.J("engine", "known_groups_error", map[string]any{"rule_id": ruleID, "error": err.Error()})
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err == nil {
			out = append(out, k)
		}
	}
	return out
}

// dropState 删除某个分组的状态行。恢复后调用，让它不再参与后续判定。
func (e *Engine) dropState(sc *store.Scoped, ruleID int64, groupKey string) {
	if _, err := sc.Exec(`DELETE FROM rule_states
		WHERE tenant_id = ? AND rule_id = ? AND group_key = ?`, ruleID, groupKey); err != nil {
		logx.J("engine", "drop_state_error", map[string]any{"rule_id": ruleID, "error": err.Error()})
	}
}

// bumpState 累加连续命中/未命中计数，返回累加后的值。
func (e *Engine) bumpState(sc *store.Scoped, ruleID int64, groupKey string, hit bool) (int, int, error) {
	if hit {
		_, err := sc.Insert(`INSERT INTO rule_states (tenant_id, rule_id, group_key, hit_streak, miss_streak, last_hit_at)
			VALUES (?, ?, ?, 1, 0, NOW(3))
			ON DUPLICATE KEY UPDATE hit_streak = hit_streak + 1, miss_streak = 0, last_hit_at = NOW(3)`,
			ruleID, groupKey)
		if err != nil {
			return 0, 0, err
		}
	} else {
		_, err := sc.Insert(`INSERT INTO rule_states (tenant_id, rule_id, group_key, hit_streak, miss_streak)
			VALUES (?, ?, ?, 0, 1)
			ON DUPLICATE KEY UPDATE hit_streak = 0, miss_streak = miss_streak + 1`,
			ruleID, groupKey)
		if err != nil {
			return 0, 0, err
		}
	}
	var hs, ms int
	err := sc.QueryRow(`SELECT hit_streak, miss_streak FROM rule_states
		WHERE tenant_id = ? AND rule_id = ? AND group_key = ?`, ruleID, groupKey).Scan(&hs, &ms)
	return hs, ms, err
}

func (e *Engine) buildLabels(r dueRule, spec ruleSpec, groupKey string) map[string]string {
	labels := map[string]string{
		"rule":     r.Name,
		"severity": r.Severity,
	}
	for k, v := range decodeLabels(r.Labels) {
		labels[k] = v
	}
	parts := datasource.SplitGroupKey(groupKey)
	for i, g := range spec.groupNames {
		if i < len(parts) {
			labels[g] = parts[i]
		}
	}
	return labels
}

// finishRun 落一次执行记录，并更新规则的健康状态。
func (e *Engine) finishRun(sc *store.Scoped, r dueRule, started time.Time,
	outcome string, hits, groups, events int, runErr error,
) {
	msg := ""
	if runErr != nil {
		msg = truncate(runErr.Error(), 500)
	}
	if _, err := sc.Insert(`INSERT INTO rule_runs
		(tenant_id, rule_id, started_at, duration_ms, outcome, hits, group_count, events_made, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, started, int(time.Since(started).Milliseconds()), outcome, hits, groups, events, msg); err != nil {
		logx.J("engine", "rule_run_insert_error", map[string]any{"rule_id": r.ID, "error": err.Error()})
	}

	// 指标累计。UPSERT 到独立的 rule_metrics 表而不是靠 SUM(rule_runs)：
	// rule_runs 只留 30 天，清理一跑 counter 就回退，
	// Prometheus 会把回退当成计数器归零并产生一个凭空的 rate 尖峰。
	// 详见 013 迁移里的说明。
	//
	// ⚠️ 无论规则有没有开指标导出都累计。开关只管"导不导出"，
	// 不管"记不记"——否则中途打开开关的规则会从 0 开始，
	// 看板上表现为这条规则"刚刚才出现"。
	okN, ndN, errN := 0, 0, 0
	switch {
	case runErr != nil:
		errN = 1
	case outcome == "no_data":
		ndN = 1
	default:
		okN = 1
	}
	if _, err := sc.Exec(`INSERT INTO rule_metrics
		(tenant_id, rule_id, hits_total, runs_ok, runs_nodata, runs_error, events_total, last_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NOW(3))
		ON DUPLICATE KEY UPDATE
			hits_total = hits_total + VALUES(hits_total),
			runs_ok = runs_ok + VALUES(runs_ok),
			runs_nodata = runs_nodata + VALUES(runs_nodata),
			runs_error = runs_error + VALUES(runs_error),
			events_total = events_total + VALUES(events_total),
			last_run_at = VALUES(last_run_at)`,
		r.ID, hits, okN, ndN, errN, events); err != nil {
		logx.J("engine", "rule_metrics_error", map[string]any{"rule_id": r.ID, "error": err.Error()})
	}

	if runErr != nil {
		if _, err := sc.Exec(`UPDATE rules SET last_run_at = NOW(3), last_error = ?,
			consecutive_failures = consecutive_failures + 1 WHERE tenant_id = ? AND id = ?`,
			msg, r.ID); err != nil {
			logx.J("engine", "rule_update_error", map[string]any{"rule_id": r.ID, "error": err.Error()})
		}
		logx.J("engine", "rule_failed", map[string]any{"rule_id": r.ID, "name": r.Name, "error": msg})
		return
	}
	if _, err := sc.Exec(`UPDATE rules SET last_run_at = NOW(3), last_error = '',
		consecutive_failures = 0 WHERE tenant_id = ? AND id = ?`, r.ID); err != nil {
		logx.J("engine", "rule_update_error", map[string]any{"rule_id": r.ID, "error": err.Error()})
	}
}

// saveTrace 写判定链。
//
// 未触发的判定也要写，这正是反向解释（why-not-fired）的数据来源；
// 但只保留每条规则每个指纹的最近一条，否则一条 1 分钟周期的规则
// 一天就是 1440 行 × 分组数。清理由保留任务负责。
func (e *Engine) saveTrace(sc *store.Scoped, ruleID, incidentID int64, fp, verdict string, steps []TraceStep) {
	blob, err := json.Marshal(steps)
	if err != nil {
		return
	}
	if verdict == verdictNoFire {
		if _, err := sc.Exec(`DELETE FROM decision_traces
			WHERE tenant_id = ? AND rule_id = ? AND fingerprint = ? AND verdict = ?`,
			ruleID, fp, verdictNoFire); err != nil {
			logx.J("engine", "trace_cleanup_error", map[string]any{"error": err.Error()})
		}
	}
	var incID any
	if incidentID > 0 {
		incID = incidentID
	}
	if _, err := sc.Insert(`INSERT INTO decision_traces
		(tenant_id, rule_id, incident_id, fingerprint, verdict, steps)
		VALUES (?, ?, ?, ?, ?, ?)`, ruleID, incID, fp, verdict, blob); err != nil {
		logx.J("engine", "trace_insert_error", map[string]any{"error": err.Error()})
	}
}

// fingerprint 事件指纹：规则 + 分组。
//
// 同一指纹的多次命中收敛成一条事件；指纹变了就是另一件事。
// 不把时间、命中数放进指纹——那会让每个周期都产生新事件，
// 收敛就失效了，而失效的表现是"告警风暴"，很容易被当成业务真的出了大事。
func fingerprint(ruleID int64, groupKey string) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%d|%s", ruleID, groupKey)))
	return hex.EncodeToString(h[:])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func decodeStrings(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var out []string
	_ = json.Unmarshal(raw, &out)
	return out
}

func decodeLabels(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]string{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// matchLabels 判断标签是否满足匹配条件。
func matchLabels(labels map[string]string, matchers []Matcher) bool {
	for _, m := range matchers {
		v := labels[m.Key]
		switch m.Op {
		case "=", "":
			if v != m.Value {
				return false
			}
		case "!=":
			if v == m.Value {
				return false
			}
		case "=~":
			if !strings.Contains(v, m.Value) {
				return false
			}
		default:
			// 不认识的操作符按不匹配处理并留下日志：
			// 当成匹配会让静默/抑制意外生效，把该响的告警吞掉。
			logx.J("engine", "unknown_matcher_op", map[string]any{"op": m.Op, "key": m.Key})
			return false
		}
	}
	return true
}

// Matcher 是标签匹配条件。
type Matcher struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

const (
	kindLogKeyword = "log_keyword"
	kindLogAbsent  = "log_absent"
	kindLogSpike   = "log_spike"
	kindLogField   = "log_field_threshold"
)

// ruleSpec 是场景特有配置（rules.spec）。
// 公共字段在列上，特有配置在这里——加场景不改表结构。
type ruleSpec struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit,omitempty"`
	ExpectedGroups []string `json:"expected_groups,omitempty"`

	// 通知模板。旧系统的 message_title / message_template 迁到这里，
	// 支持 {{变量}}，变量来自 Extract。
	MessageTitle    string `json:"message_title,omitempty"`
	MessageTemplate string `json:"message_template,omitempty"`
	RecoveryTitle   string `json:"recovery_title,omitempty"`

	// 字段提取（旧系统的 extract_fields）。既供模板渲染，
	// 也会成为事件标签，从而支持按字段值路由。
	Extract []extractRule `json:"extract,omitempty"`

	// log_spike：与基线比倍数
	Baseline     string  `json:"baseline,omitempty"`       // previous（默认）/ week_ago
	SpikeRatio   float64 `json:"spike_ratio,omitempty"`    // 默认 3 倍
	SpikeMinBase int     `json:"spike_min_base,omitempty"` // 基线样本下限，默认 10

	// log_field_threshold：提数值再聚合比阈值
	Field          string  `json:"field,omitempty"`
	FieldPattern   string  `json:"field_pattern,omitempty"` // 从日志行取数值的正则
	Agg            string  `json:"agg,omitempty"`           // p50/p95/p99/avg/max/sum
	FieldThreshold float64 `json:"field_threshold,omitempty"`

	// groupNames 由 rules.group_by 填充，便于把分组键还原成标签
	groupNames []string
}

// extractRule 一条字段提取规则：从 path 指向的字段（或整行）用 pattern 抽出变量。
type extractRule struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

var _ = context.Background // 保留 context 依赖（deliver 在 notify.go 内使用）
