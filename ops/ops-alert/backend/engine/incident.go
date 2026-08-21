package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"ops-alert-backend/datasource"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
	"ops-alert-backend/notify"
)

// openDatasource 组装适配器。凭据在这里解密，解密后只存在于内存。
func (e *Engine) openDatasource(sc *store.Scoped, id int64) (datasource.Adapter, string, error) {
	var (
		name, kind, endpoint string
		authEnc              sql.NullString
		specRaw              []byte
		skipTLS              int
	)
	err := sc.QueryRow(`SELECT name, type, endpoint, auth_enc, spec, skip_tls
		FROM datasources WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id).
		Scan(&name, &kind, &endpoint, &authEnc, &specRaw, &skipTLS)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("数据源 %d 不存在或已删除", id)
	}
	if err != nil {
		return nil, "", err
	}

	cfg := datasource.Config{ID: id, Name: name, Kind: kind, Endpoint: endpoint, SkipTLS: skipTLS == 1}
	if authEnc.Valid && authEnc.String != "" {
		plain, err := e.cipher.Decrypt(authEnc.String)
		if err != nil {
			// 解密失败通常意味着 AES 密钥换了。必须显性报错：
			// 静默用空凭据去连，会得到 401，然后被当成"数据源配置错了"，
			// 排查方向整个偏掉。
			return nil, name, fmt.Errorf("凭据解密失败（AES 密钥是否变更？）: %w", err)
		}
		if err := json.Unmarshal([]byte(plain), &cfg.Auth); err != nil {
			return nil, name, fmt.Errorf("凭据格式错误: %w", err)
		}
	}
	if len(specRaw) > 0 {
		_ = json.Unmarshal(specRaw, &cfg.Spec)
	}
	ad, err := datasource.New(cfg)
	return ad, name, err
}

// upsertIncident 新建或更新事件，返回事件 ID 与是否新建。
//
// active_key 是「同一指纹同时只有一条活跃事件」的保证：
// 活跃时等于 fingerprint，恢复时置 NULL。MySQL 的 NULL 不参与唯一性比较，
// 所以同一指纹可以恢复很多次再复发。直接对 fingerprint 建唯一索引的话，
// 第二次故障会插不进去，现象是「问题复发了但界面上什么都没有」。
func (e *Engine) upsertIncident(sc *store.Scoped, r dueRule, spec ruleSpec,
	fp, groupKey string, count int, labels, vars map[string]string,
	res *datasource.Result, status string,
) (int64, bool) {
	// 标题同样要渲染 {{变量}}：只在通知里渲染的话，
	// 事件列表和值班台上显示的就是原始的 {{elapsed}}，看着像坏了。
	title := renderTemplate(spec.MessageTitle, vars)
	if title == "" {
		title = r.Name
		if groupKey != "" {
			title = fmt.Sprintf("%s · %s", r.Name, groupKey)
		}
	}
	labelBlob, _ := json.Marshal(labels)
	sampleBlob := sampleOf(res)

	var id int64
	var oldStatus string
	err := sc.QueryRow(`SELECT id, status FROM incidents
		WHERE tenant_id = ? AND active_key = ?`, fp).Scan(&id, &oldStatus)
	if err == nil {
		// 已有活跃事件：累加计数、更新最后发生时间。
		if _, err := sc.Exec(`UPDATE incidents SET count = count + 1, last_at = NOW(3),
			status = IF(status = 'resolved', 'firing', status), sample = ?
			WHERE tenant_id = ? AND id = ?`, sampleBlob, id); err != nil {
			logx.J("engine", "incident_update_error", map[string]any{"id": id, "error": err.Error()})
		}
		e.addTimeline(sc, id, "repeated", "", fmt.Sprintf("再次命中 %d 条", count))
		return id, false
	}
	if err != sql.ErrNoRows {
		logx.J("engine", "incident_query_error", map[string]any{"error": err.Error()})
		return 0, false
	}

	ruleID := any(r.ID)
	resInsert, err := sc.Insert(`INSERT INTO incidents
		(tenant_id, rule_id, source, fingerprint, active_key, title, severity, status,
		 labels, count, first_at, last_at, sample)
		VALUES (?, ?, 'rule', ?, ?, ?, ?, ?, ?, 1, NOW(3), NOW(3), ?)`,
		ruleID, fp, fp, truncate(title, 500), r.Severity, status, labelBlob, sampleBlob)
	if err != nil {
		logx.J("engine", "incident_insert_error", map[string]any{"error": err.Error()})
		return 0, false
	}
	id, _ = resInsert.LastInsertId()
	e.addTimeline(sc, id, "created", "", fmt.Sprintf("首次命中 %d 条", count))
	return id, true
}

// resolveIncident 恢复事件。返回是否确实恢复了一条。
func (e *Engine) resolveIncident(ctx context.Context, sc *store.Scoped, r dueRule,
	spec ruleSpec, fp, groupKey string,
) (bool, error) {
	var id int64
	err := sc.QueryRow(`SELECT id FROM incidents WHERE tenant_id = ? AND active_key = ?`, fp).Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// active_key 置 NULL 让这条不再占用「活跃唯一」的位置。
	if _, err := sc.Exec(`UPDATE incidents SET status = 'resolved', resolved_at = NOW(3),
		active_key = NULL WHERE tenant_id = ? AND id = ?`, id); err != nil {
		return false, err
	}
	e.addTimeline(sc, id, "resolved", "", "连续多个周期未命中，自动恢复")

	if !r.NotifyResol {
		return true, nil
	}
	// 恢复通知走与告警**同一条路由**：值班的人要在同一个群里看到闭环，
	// 否则只看到"响了"没看到"好了"，事情感觉永远没完。
	labels := e.buildLabels(r, spec, groupKey)
	route := e.matchRoute(sc, labels)
	if len(route.NotifierIDs) == 0 {
		logx.J("engine", "resolved_no_route", map[string]any{"incident_id": id})
		return true, nil
	}
	title := "[已恢复] " + r.Name
	if spec.RecoveryTitle != "" {
		title = renderTemplate(spec.RecoveryTitle, map[string]string{})
	}
	msg := notify.Message{
		Title:    title,
		Severity: "info",
		Fields: []notify.Field{
			{Key: "对象", Value: groupKey},
			{Key: "状态", Value: "连续多个周期未再命中，已自动恢复"},
		},
	}
	for _, nid := range route.NotifierIDs {
		e.sendOne(ctx, sc, id, nid, 0, msg)
	}
	return true, nil
}

// sendOne 向单个渠道发一条消息并记录投递结果。
// 告警、升级、恢复三条路径共用它——三份各写一遍的投递记录逻辑必然漂移。
func (e *Engine) sendOne(ctx context.Context, sc *store.Scoped, incidentID, notifierID int64,
	level int, msg notify.Message,
) bool {
	var typ string
	var enc sql.NullString
	if err := sc.QueryRow(`SELECT type, config_enc FROM notifiers
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL AND status = 'active'`, notifierID).
		Scan(&typ, &enc); err != nil {
		e.recordDelivery(sc, incidentID, notifierID, 0, "failed", 0, fmt.Sprintf("渠道不可用: %v", err))
		return false
	}
	plain := ""
	if enc.Valid && enc.String != "" {
		var err error
		plain, err = e.cipher.Decrypt(enc.String)
		if err != nil {
			e.recordDelivery(sc, incidentID, notifierID, 0, "failed", 0, "配置解密失败（AES 密钥是否变更？）")
			return false
		}
	}
	sender, err := notify.New(typ, []byte(plain))
	if err != nil {
		e.recordDelivery(sc, incidentID, notifierID, 0, "failed", 0, err.Error())
		return false
	}

	// 文案模板挂在这里 —— 告警、升级、恢复三条路径都经过 sendOne，
	// 一处生效三处都对。挂在各自的 buildMessage 里的话，
	// 加一条新的通知路径就会漏掉模板，而漏掉的表现只是"这类通知没套模板"，
	// 没人会当成故障报。
	//
	// ⚠️ 模板是**按渠道**取的：同一条事件发到飞书和 webhook 可以用不同文案。
	if tv := msg.TemplateVars; len(tv) > 0 {
		applyTemplate(&msg, loadTemplate(sc, notifierID), tv)
	}

	started := time.Now()
	sendErr := sender.Send(ctx, msg)
	ms := int(time.Since(started).Milliseconds())
	if sendErr != nil {
		e.recordDelivery(sc, incidentID, notifierID, 0, "failed", ms, sendErr.Error())
		e.addTimeline(sc, incidentID, "notify_failed", "", fmt.Sprintf("%s 投递失败：%v", typ, sendErr))
		return false
	}
	e.recordDelivery(sc, incidentID, notifierID, 0, "sent", ms, "")
	e.addTimeline(sc, incidentID, "notified", "", fmt.Sprintf("%s 投递成功（%dms）", typ, ms))
	return true
}

func (e *Engine) addTimeline(sc *store.Scoped, incidentID int64, kind, actor, msg string) {
	if _, err := sc.Insert(`INSERT INTO incident_events (tenant_id, incident_id, kind, actor, message)
		VALUES (?, ?, ?, ?, ?)`, incidentID, kind, actor, truncate(msg, 1000)); err != nil {
		logx.J("engine", "timeline_error", map[string]any{"incident_id": incidentID, "error": err.Error()})
	}
}

// checkInhibition 检查是否被父事件抑制。
func (e *Engine) checkInhibition(sc *store.Scoped, labels map[string]string) (string, bool) {
	rows, err := sc.Query(`SELECT name, source_matchers, target_matchers, equal_labels
		FROM inhibitions WHERE tenant_id = ? AND enabled = 1 AND deleted_at IS NULL`)
	if err != nil {
		// 查不了抑制规则时**不抑制**：宁可多告一次，不可少告一次。
		logx.J("engine", "inhibition_query_error", map[string]any{"error": err.Error()})
		return "", false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var srcRaw, tgtRaw, eqRaw []byte
		if err := rows.Scan(&name, &srcRaw, &tgtRaw, &eqRaw); err != nil {
			continue
		}
		var target []Matcher
		_ = json.Unmarshal(tgtRaw, &target)
		if !matchLabels(labels, target) {
			continue
		}
		// 目标匹配上了，再看父事件在不在。
		var srcMatchers []Matcher
		_ = json.Unmarshal(srcRaw, &srcMatchers)
		equal := decodeStrings(eqRaw)
		if e.parentExists(sc, srcMatchers, equal, labels) {
			return fmt.Sprintf("被抑制规则「%s」抑制：父事件存在", name), true
		}
	}
	return "", false
}

// parentExists 查是否有满足条件的活跃父事件。
func (e *Engine) parentExists(sc *store.Scoped, matchers []Matcher, equal []string, labels map[string]string) bool {
	rows, err := sc.Query(`SELECT labels FROM incidents
		WHERE tenant_id = ? AND status IN ('firing','acked') AND active_key IS NOT NULL
		ORDER BY last_at DESC LIMIT 200`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		parent := map[string]string{}
		_ = json.Unmarshal(raw, &parent)
		if !matchLabels(parent, matchers) {
			continue
		}
		// equal_labels：父子必须在这些标签上相等（例 node），
		// 否则 A 节点故障会把 B 节点上的告警一起抑制掉。
		same := true
		for _, k := range equal {
			if parent[k] != labels[k] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}

// checkSilence 检查静默与维护窗口。
func (e *Engine) checkSilence(sc *store.Scoped, labels map[string]string) (string, bool) {
	rows, err := sc.Query(`SELECT id, kind, matchers, comment FROM silences
		WHERE tenant_id = ? AND deleted_at IS NULL
		  AND starts_at <= NOW(3) AND ends_at > NOW(3)`)
	if err != nil {
		// 同上：查不了静默时不静默。少告警的代价远高于多告警。
		logx.J("engine", "silence_query_error", map[string]any{"error": err.Error()})
		return "", false
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var kind, comment string
		var raw []byte
		if err := rows.Scan(&id, &kind, &raw, &comment); err != nil {
			continue
		}
		var matchers []Matcher
		_ = json.Unmarshal(raw, &matchers)
		if matchLabels(labels, matchers) {
			return fmt.Sprintf("命中静默 #%d（%s）%s", id, kind, comment), true
		}
	}
	return "", false
}

// RouteMatch 是一次路由匹配的结果。
//
// 用结构体而不是多返回值：这里已经有 4 个含义不同的返回项，
// 再加一个就要数位置了，而位置数错在 Go 里是编译不出错的同类型互换。
type RouteMatch struct {
	ID          int64
	NotifierIDs []int64
	RepeatSec   int
	Reason      string
}

// matchRoute 自上而下匹配路由，命中即停（除非 continue_on）。
func (e *Engine) matchRoute(sc *store.Scoped, labels map[string]string) RouteMatch {
	rows, err := sc.Query(`SELECT id, name, matchers, continue_on, notifier_ids, is_fallback, repeat_sec
		FROM routes WHERE tenant_id = ? AND deleted_at IS NULL
		ORDER BY is_fallback ASC, sort_order ASC, id ASC`)
	if err != nil {
		logx.J("engine", "route_query_error", map[string]any{"error": err.Error()})
		return RouteMatch{Reason: "路由查询失败：" + err.Error()}
	}
	defer rows.Close()

	var matched, fallback RouteMatch
	for rows.Next() {
		var id int64
		var name string
		var mRaw, nRaw []byte
		var cont, isFallback, repeatSec int
		if err := rows.Scan(&id, &name, &mRaw, &cont, &nRaw, &isFallback, &repeatSec); err != nil {
			continue
		}
		ids := decodeInts(nRaw)
		if isFallback == 1 {
			fallback = RouteMatch{ID: id, NotifierIDs: ids, RepeatSec: repeatSec,
				Reason: "未匹配具体分支，走兜底路由"}
			continue
		}
		var matchers []Matcher
		_ = json.Unmarshal(mRaw, &matchers)
		if !matchLabels(labels, matchers) {
			continue
		}
		matched.ID = id
		matched.NotifierIDs = append(matched.NotifierIDs, ids...)
		matched.Reason = fmt.Sprintf("命中路由「%s」", name)
		// continue 时沿用第一个命中分支的重复间隔：
		// 多个分支各有间隔时取第一个，避免同一事件按两套节奏重发。
		if matched.RepeatSec == 0 {
			matched.RepeatSec = repeatSec
		}
		if cont == 0 {
			break
		}
	}
	if len(matched.NotifierIDs) == 0 {
		if len(fallback.NotifierIDs) > 0 {
			return fallback
		}
		return RouteMatch{Reason: "未匹配任何路由，且没有兜底路由"}
	}
	return matched
}

// deliver 投递通知，返回每个渠道的成败。
//
// 每条投递都写 notifications 表：渠道限流导致的漏告警必须看得见，
// 否则值班以为「没人叫我」就是「没事」。
func (e *Engine) deliver(ctx context.Context, sc *store.Scoped, incidentID, routeID int64,
	notifierIDs []int64, r dueRule, spec ruleSpec, vars map[string]string,
	labels map[string]string, hits []datasource.Hit,
) map[int64]bool {
	out := map[int64]bool{}
	for _, nid := range notifierIDs {
		var typ string
		var cfgEnc sql.NullString
		err := sc.QueryRow(`SELECT type, config_enc FROM notifiers
			WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL AND status = 'active'`, nid).
			Scan(&typ, &cfgEnc)
		if err != nil {
			e.recordDelivery(sc, incidentID, nid, routeID, "failed", 0, fmt.Sprintf("渠道不可用: %v", err))
			out[nid] = false
			continue
		}
		var plain string
		if cfgEnc.Valid && cfgEnc.String != "" {
			plain, err = e.cipher.Decrypt(cfgEnc.String)
			if err != nil {
				e.recordDelivery(sc, incidentID, nid, routeID, "failed", 0, "配置解密失败（AES 密钥是否变更？）")
				out[nid] = false
				continue
			}
		}
		sender, err := notify.New(typ, []byte(plain))
		if err != nil {
			e.recordDelivery(sc, incidentID, nid, routeID, "failed", 0, err.Error())
			out[nid] = false
			continue
		}

		msg := buildMessage(r, spec, vars, labels, hits)
		started := time.Now()
		sendErr := sender.Send(ctx, msg)
		ms := int(time.Since(started).Milliseconds())
		if sendErr != nil {
			e.recordDelivery(sc, incidentID, nid, routeID, "failed", ms, sendErr.Error())
			e.addTimeline(sc, incidentID, "notify_failed", "", fmt.Sprintf("%s 投递失败：%v", typ, sendErr))
			out[nid] = false
			continue
		}
		e.recordDelivery(sc, incidentID, nid, routeID, "sent", ms, "")
		e.addTimeline(sc, incidentID, "notified", "", fmt.Sprintf("%s 投递成功（%dms）", typ, ms))
		out[nid] = true
	}
	// 首次送达时间用于算 MTTA。只在第一次成功时写。
	for _, ok := range out {
		if ok {
			if _, err := sc.Exec(`UPDATE incidents SET notified_at = COALESCE(notified_at, NOW(3))
				WHERE tenant_id = ? AND id = ?`, incidentID); err != nil {
				logx.J("engine", "notified_at_error", map[string]any{"error": err.Error()})
			}
			break
		}
	}
	return out
}

func (e *Engine) recordDelivery(sc *store.Scoped, incidentID, notifierID, routeID int64,
	status string, ms int, errMsg string,
) {
	var rid any
	if routeID > 0 {
		rid = routeID
	}
	var sentAt any
	if status == "sent" {
		sentAt = time.Now()
	}
	if _, err := sc.Insert(`INSERT INTO notifications
		(tenant_id, incident_id, notifier_id, route_id, status, attempts, duration_ms, error, sent_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		incidentID, notifierID, rid, status, ms, truncate(errMsg, 500), sentAt); err != nil {
		logx.J("engine", "delivery_record_error", map[string]any{"error": err.Error()})
	}
}

// buildMessage 组装通知内容。
//
// 标题与正文支持 {{变量}}，变量来自字段提取——旧系统的 message_template
// 就是靠这个迁过来的。没配模板时回落到规则名 + 结构化字段，
// 而不是发一条只有规则名的空通知。
func buildMessage(r dueRule, spec ruleSpec, vars map[string]string,
	labels map[string]string, hits []datasource.Hit,
) notify.Message {
	title := r.Name
	if spec.MessageTitle != "" {
		title = renderTemplate(spec.MessageTitle, vars)
	}
	msg := notify.Message{
		Title:    title,
		Severity: r.Severity,
	}
	if spec.MessageTemplate != "" {
		msg.Sample = renderTemplate(spec.MessageTemplate, vars)
	}
	// 提取到的变量一并展示：值班的人最先看的就是这些业务字段
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if vars[k] != "" {
			msg.Fields = append(msg.Fields, notify.Field{Key: k, Value: vars[k]})
		}
	}
	// 字段顺序固定：map 遍历顺序随机会让同一条告警每次字段顺序都不同，
	// 值班的人没法形成肌肉记忆。
	for _, k := range []string{"cluster", "namespace", "container", "service", "team", "env"} {
		if v, ok := labels[k]; ok && v != "" {
			msg.Fields = append(msg.Fields, notify.Field{Key: k, Value: v})
		}
	}
	msg.Fields = append(msg.Fields, notify.Field{Key: "命中", Value: fmt.Sprintf("%d 条", len(hits))})
	if msg.Sample == "" {
		msg.Sample = sampleText(hits)
	}
	// 变量表跟着消息走，由 sendOne 在知道"发给哪个渠道"之后才套模板 ——
	// 在这里套的话，同一条事件发给两个渠道就只能用同一份文案。
	msg.TemplateVars = templateVars(
		r.Name, r.Severity, groupKeyOf(labels), "", len(hits),
		time.Time{}, time.Local, msg.Sample, vars, labels)
	return msg
}

// groupKeyOf 从标签里拼出人看的分组描述。没有分组维度时给一句话，
// 而不是留空 —— 空值在模板里渲染成 "对象：" 后面什么都没有，读起来像坏了。
func groupKeyOf(labels map[string]string) string {
	var parts []string
	for _, k := range []string{"cluster", "namespace", "service", "container"} {
		if v, ok := labels[k]; ok && v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) == 0 {
		return "（未分组）"
	}
	return strings.Join(parts, " ")
}

func sampleText(hits []datasource.Hit) string {
	const maxLines = 3
	var out string
	for i, h := range hits {
		if i >= maxLines {
			break
		}
		line := h.Line
		if line == "" && len(h.Fields) > 0 {
			b, _ := json.Marshal(h.Fields)
			line = string(b)
		}
		out += truncate(line, 300) + "\n"
	}
	return out
}

func sampleOf(res *datasource.Result) []byte {
	if len(res.Hits) == 0 {
		return nil
	}
	b, _ := json.Marshal(res.Hits[0])
	return b
}

func decodeInts(raw []byte) []int64 {
	if len(raw) == 0 {
		return nil
	}
	var out []int64
	_ = json.Unmarshal(raw, &out)
	return out
}
