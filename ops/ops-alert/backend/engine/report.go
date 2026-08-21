package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
	"ops-alert-backend/notify"
)

// 日报。
//
// # 为什么是租户级一份，而不是像旧系统那样每条规则一个开关
//
// 旧系统的 report_enabled 挂在规则上，模板按 domain 硬编码。
// 二十条规则都打开的话，值班的人早上会收到二十封各自只讲一条规则的邮件 ——
// 而他真正要回答的问题是"昨天整体怎么样、有没有我漏掉的"。
// 那个问题需要横向汇总，逐规则的邮件恰恰答不了。
//
// # 日报不是"昨天的事件列表"
//
// 只列事件的日报，在一切正常的日子里是空的，于是很快就没人看了；
// 而"没人看"的日报在真出事那天也不会被看。所以这份日报永远有内容：
// 检测链路是否完整、哪些规则从没命中过、哪些投递失败了 ——
// 这些在"没有告警"的日子里恰恰是最该确认的东西。

// reportTick 决定检查频率。比"每分钟"稀疏是因为发送时刻只精确到分钟，
// 而比"每小时"密是因为整点检查会错过 09:30 这类配置。
const reportTick = 5 * time.Minute

// runReports 检查各租户是否到了发送时刻。由持有租约的副本调用。
//
// ⚠️ 必须在 ifLeader 里跑。两个副本各发一份的话，收件人会收到两封
// 一模一样的日报 —— 而这种重复很容易被当成"系统在乱发"，
// 进而让人把整个日报关掉。
func (e *Engine) runReports(ctx context.Context) {
	failed, err := store.ForEachTenant(ctx, e.st, "report", func(sc *store.Scoped, _ store.TenantID) error {
		return e.maybeSendReport(ctx, sc)
	})
	if err != nil {
		logx.J("engine", "report_tenants_error", map[string]any{"error": err.Error()})
		return
	}
	if failed > 0 {
		logx.J("engine", "report_partial_failure", map[string]any{"failed": failed})
	}
}

func (e *Engine) maybeSendReport(ctx context.Context, sc *store.Scoped) error {
	var enabled int
	var sendAt string
	var rawNotifiers []byte
	var lastSent sql.NullTime
	err := sc.QueryRow(`SELECT enabled, send_at, notifier_ids, last_sent_on
		FROM report_configs WHERE tenant_id = ?`).Scan(&enabled, &sendAt, &rawNotifiers, &lastSent)
	if err == sql.ErrNoRows {
		return nil // 没配过 = 不发。这是正常状态，不是错误
	}
	if err != nil {
		return err
	}
	if enabled != 1 {
		return nil
	}

	// ⚠️ 墙上时钟判定必须在**配置的业务时区**里做，不能用进程本地时区。
	// 进程本地时区取决于镜像里的 /etc/localtime（曾经硬编码成 Asia/Manila），
	// 那意味着"每天 09:00 发日报"的实际时刻由镜像决定而不是由配置决定 ——
	// 而这件事没有任何一处会说出来。
	now := time.Now().In(e.cfg.Location)
	today := now.Format("2006-01-02")
	// 当天已发过就不再发。判据是日期而不是"距上次超过 24 小时"：
	// 后者会让发送时刻每天往后漂几分钟，漂几周之后就跑到别的时段去了。
	if lastSent.Valid && lastSent.Time.Format("2006-01-02") == today {
		return nil
	}
	due, ok := parseSendAt(sendAt)
	if !ok {
		return e.markReportError(sc, fmt.Sprintf("发送时刻 %q 格式不对，应形如 09:00", sendAt))
	}
	if now.Hour()*60+now.Minute() < due {
		return nil // 还没到点
	}

	var notifierIDs []int64
	_ = json.Unmarshal(rawNotifiers, &notifierIDs)
	if len(notifierIDs) == 0 {
		// ⚠️ 这里必须落一条错误而不是静默跳过。
		// "开了日报却从没收到"是最难查的一类：界面上开关是绿的，
		// 日志里什么都没有，而真相只是没选投递渠道。
		return e.markReportError(sc, "日报已开启但没有选择投递渠道，没有发送")
	}

	fields, detail, err := e.buildReport(sc, now)
	if err != nil {
		return e.markReportError(sc, "生成日报失败："+err.Error())
	}

	// 关键数字走 Fields（各渠道会渲染成表格），长文走 Sample。
	// 全塞进一段纯文本的话，飞书卡片里是一坨等宽字，扫不出重点。
	msg := notify.Message{
		Title:  fmt.Sprintf("OpsAlert 日报 · %s", now.AddDate(0, 0, -1).Format("2006-01-02")),
		Fields: fields,
		Sample: detail,
	}
	sent, lastErr := 0, ""
	for _, nid := range notifierIDs {
		if err := e.sendVia(ctx, sc, nid, msg); err != nil {
			lastErr = err.Error()
			continue
		}
		sent++
	}
	if sent == 0 {
		// 一个都没发出去 = 失败。不能因为"任务跑完了"就记成成功，
		// 那样 last_sent_on 会被推进，明天也不会补发。
		return e.markReportError(sc, "全部投递渠道都失败："+lastErr)
	}
	_, err = sc.Exec(`UPDATE report_configs SET last_sent_on = CURRENT_DATE(), last_error = ?
		WHERE tenant_id = ?`, partialErr(sent, len(notifierIDs), lastErr))
	logx.J("engine", "report_sent", map[string]any{
		"sent": sent, "total": len(notifierIDs), "last_error": lastErr})
	return err
}

// partialErr 部分成功时仍要把失败记下来。
// 清空 last_error 会让"三个渠道里有一个一直发不出去"永远不被发现。
func partialErr(sent, total int, lastErr string) string {
	if sent == total {
		return ""
	}
	return fmt.Sprintf("%d/%d 个渠道投递失败：%s", total-sent, total, lastErr)
}

func (e *Engine) markReportError(sc *store.Scoped, msg string) error {
	logx.J("engine", "report_error", map[string]any{"error": msg})
	_, err := sc.Exec(`UPDATE report_configs SET last_error = ? WHERE tenant_id = ?`, msg)
	return err
}

// parseSendAt 解析 "09:00"，返回当天第几分钟。
func parseSendAt(s string) (int, bool) {
	var h, m int
	if n, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); n != 2 || err != nil {
		return 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// buildReport 生成昨天的日报。
// 返回关键数字（渲染成表格）与详细段落（渲染成文本块）。
func (e *Engine) buildReport(sc *store.Scoped, now time.Time) ([]notify.Field, string, error) {
	// 在这里统一转成业务时区，而不是要求每个调用方自己转。
	// 预览接口传的是 time.Now()（进程本地），忘了转的后果是
	// 预览里的「昨天」和实际发出去的那一份不是同一天，而两边都不报错。
	now = now.In(e.cfg.Location)
	var fields []notify.Field
	var b strings.Builder
	day := now.AddDate(0, 0, -1).Format("2006-01-02")
	fmt.Fprintf(&b, "统计区间：%s 00:00 ~ 23:59\n\n", day)

	// ── 1. 检测链路是否完整 ──
	//
	// 这一段排在最前面，因为它决定了后面所有数字**能不能信**。
	// 数据源断了的那天，"新增 0 条告警"和"确实很太平"长得一模一样。
	var dsTotal, dsDown, rulesTotal, rulesFailing int
	_ = sc.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status <> 'ok'),0) FROM datasources
		WHERE tenant_id = ? AND deleted_at IS NULL`).Scan(&dsTotal, &dsDown)
	_ = sc.QueryRow(`SELECT COUNT(*), COALESCE(SUM(consecutive_failures > 0),0) FROM rules
		WHERE tenant_id = ? AND deleted_at IS NULL AND enabled = 1`).Scan(&rulesTotal, &rulesFailing)

	// ⚠️ 这一条排在 Fields 第一位，因为它决定后面所有数字能不能信。
	if dsDown == 0 && rulesFailing == 0 {
		fields = append(fields, notify.Field{Key: "检测链路", Value: "完整，没有告警=确实没问题"})
	} else {
		fields = append(fields, notify.Field{Key: "检测链路",
			Value: fmt.Sprintf("⚠️ 有缺口（%d 个数据源不可达 / %d 条规则执行失败），下面的数字不完整",
				dsDown, rulesFailing)})
	}

	b.WriteString("【检测链路】\n")
	if dsDown == 0 && rulesFailing == 0 {
		fmt.Fprintf(&b, "  完整。%d 个数据源可达，%d 条规则正常执行。\n", dsTotal, rulesTotal)
		b.WriteString("  → 下面的「没有告警」可以理解为「确实没有问题」。\n")
	} else {
		fmt.Fprintf(&b, "  ⚠️ 有缺口：%d/%d 个数据源不可达，%d/%d 条规则执行失败。\n",
			dsDown, dsTotal, rulesFailing, rulesTotal)
		b.WriteString("  → 下面的数字是不完整的，「没有告警」不能理解为「没有问题」。\n")
	}
	b.WriteString("\n")

	// ── 2. 昨天的事件 ──
	b.WriteString("【昨天的事件】\n")
	rows, err := sc.Query(`SELECT severity, COUNT(*),
			COALESCE(SUM(status = 'resolved'),0)
		FROM incidents
		WHERE tenant_id = ? AND first_at >= ? AND first_at < ?
		GROUP BY severity ORDER BY FIELD(severity,'critical','warning','info')`,
		day+" 00:00:00", day+" 23:59:59.999")
	if err != nil {
		return nil, "", err
	}
	total := 0
	for rows.Next() {
		var sev string
		var n, resolved int
		if err := rows.Scan(&sev, &n, &resolved); err != nil {
			rows.Close()
			return nil, "", err
		}
		total += n
		fmt.Fprintf(&b, "  %-9s 新增 %d 条，其中 %d 条已恢复\n", sev, n, resolved)
	}
	rows.Close()
	if total == 0 {
		b.WriteString("  没有新增事件。\n")
	}

	// 仍未恢复的：不管是不是昨天新增的，它们是今天要处理的
	var stillFiring int
	_ = sc.QueryRow(`SELECT COUNT(*) FROM incidents
		WHERE tenant_id = ? AND status <> 'resolved'`).Scan(&stillFiring)
	fmt.Fprintf(&b, "  当前仍未恢复：%d 条\n\n", stillFiring)
	fields = append(fields,
		notify.Field{Key: "昨天新增", Value: fmt.Sprintf("%d 条", total)},
		notify.Field{Key: "仍未恢复", Value: fmt.Sprintf("%d 条", stillFiring)})

	// ── 3. 最吵的规则 ──
	b.WriteString("【最吵的规则（昨天）】\n")
	noisy, err := sc.Query(`SELECT r.name, COUNT(*) AS n FROM incidents i
		JOIN rules r ON r.id = i.rule_id AND r.tenant_id = i.tenant_id
		WHERE i.tenant_id = ? AND i.first_at >= ? AND i.first_at < ?
		GROUP BY r.name ORDER BY n DESC LIMIT 5`, day+" 00:00:00", day+" 23:59:59.999")
	if err != nil {
		return nil, "", err
	}
	any := false
	for noisy.Next() {
		var name string
		var n int
		if err := noisy.Scan(&name, &n); err == nil {
			any = true
			fmt.Fprintf(&b, "  %d 次  %s\n", n, name)
		}
	}
	noisy.Close()
	if !any {
		b.WriteString("  没有规则产生事件。\n")
	}
	b.WriteString("\n")

	// ── 4. 投递失败 ──
	//
	// 这一段是日报里最容易被忽略、却最该看的：
	// 投递失败意味着告警**根本没送到人手上**。
	// 它在事件列表里不显眼，因为事件本身看起来是正常触发的。
	var failedDeliveries int
	_ = sc.QueryRow(`SELECT COUNT(*) FROM notifications
		WHERE tenant_id = ? AND status = 'failed' AND created_at >= ? AND created_at < ?`,
		day+" 00:00:00", day+" 23:59:59.999").Scan(&failedDeliveries)
	if failedDeliveries > 0 {
		fields = append(fields, notify.Field{Key: "投递失败",
			Value: fmt.Sprintf("⚠️ %d 次，这些告警可能没送到人手上", failedDeliveries)})
		fmt.Fprintf(&b, "【投递失败】\n  ⚠️ 昨天有 %d 次通知投递失败，这些告警可能没有送到人手上。\n\n",
			failedDeliveries)
	}

	// ── 5. 从没命中过的规则 ──
	//
	// "一直没响"有两种可能：确实没问题，或者规则写错了从来匹配不上。
	// 这两者只有放在一起看才分得开，而日常没人会主动去翻这份名单。
	silent, err := sc.Query(`SELECT r.name FROM rules r
		LEFT JOIN rule_metrics m ON m.rule_id = r.id AND m.tenant_id = r.tenant_id
		WHERE r.tenant_id = ? AND r.deleted_at IS NULL AND r.enabled = 1
		  AND COALESCE(m.hits_total,0) = 0 AND COALESCE(m.runs_ok,0) > 0
		ORDER BY r.name LIMIT 10`)
	if err != nil {
		return nil, "", err
	}
	var names []string
	for silent.Next() {
		var n string
		if err := silent.Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	silent.Close()
	if len(names) > 0 {
		sort.Strings(names)
		b.WriteString("【从未命中过的规则】\n")
		b.WriteString("  这些规则一直在正常执行，但从来没有匹配到任何数据。\n")
		b.WriteString("  可能是确实没问题，也可能是查询条件写错了 —— 值得确认一次。\n")
		for _, n := range names {
			fmt.Fprintf(&b, "  · %s\n", n)
		}
	}

	return fields, b.String(), nil
}

// sendVia 通过一个通知渠道发送。
func (e *Engine) sendVia(ctx context.Context, sc *store.Scoped, nid int64, msg notify.Message) error {
	var typ string
	var enc sql.NullString
	if err := sc.QueryRow(`SELECT type, config_enc FROM notifiers
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, nid).Scan(&typ, &enc); err != nil {
		return fmt.Errorf("渠道 %d 不可用: %w", nid, err)
	}
	plain := ""
	if enc.Valid && enc.String != "" {
		var err error
		plain, err = e.cipher.Decrypt(enc.String)
		if err != nil {
			return fmt.Errorf("渠道 %d 凭据解密失败: %w", nid, err)
		}
	}
	sender, err := notify.New(typ, []byte(plain))
	if err != nil {
		return fmt.Errorf("渠道 %d 类型 %s 不支持: %w", nid, typ, err)
	}
	return sender.Send(ctx, msg)
}
