package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/timezone"
)

// The dashboard's job is to answer one question: is the alerting itself
// healthy? An alerting platform's worst failure is the silent one — a rule
// stopped running, a channel's token expired, a source went down — because it
// looks exactly like "nothing is wrong". Everything below is derived from
// tables that already exist; nothing here needs a schema change.

// overdueGrace is the slack allowed on top of a missed cycle. A rule counts as
// overdue only once it has missed a whole interval and then some — measuring
// against the very next due time instead would flag a five-minute rule two
// minutes after its slot, which is what a process restart or a slightly
// off-boundary last run looks like, and the panel would cry wolf on a healthy
// platform.
const overdueGrace = 2 * time.Minute

// neverRunGrace keeps a rule created seconds ago out of the "never ran" list.
const neverRunGrace = 10 * time.Minute

// Timestamps leave this endpoint as instants, and the client renders them in
// the platform's display zone. Time windows are still written as SQL intervals
// rather than passed as parameters: that keeps each comparison inside a single
// clock, which is simpler to read and cannot be broken again by a driver or
// session zone drifting apart.

// attentionItem is one thing an operator may need to act on. Empty list means
// the platform believes it is healthy, which is the answer worth showing.
type attentionItem struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"` // critical | warning
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Detail   string `json:"detail"`
	Link     string `json:"link"`
}

type trendPoint struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type recentAlert struct {
	ID        int64     `json:"id"`
	RuleID    int       `json:"rule_id"`
	RuleName  string    `json:"rule_name"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type muteItem struct {
	RuleID   int    `json:"rule_id"`
	RuleName string `json:"rule_name"`
	GroupKey string `json:"group_key"`
	// MuteUntil is the server's wall clock; RemainingSeconds is what the UI
	// counts down, so the two clocks never have to agree.
	MuteUntil        time.Time `json:"mute_until"`
	RemainingSeconds int64     `json:"remaining_seconds"`
	Reason           string    `json:"reason"`
}

type noisyRule struct {
	RuleID   int    `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Count    int    `json:"count"`
}

// setupStep is one thing that has to exist before the platform can alert at all.
type setupStep struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Done  bool   `json:"done"`
	Link  string `json:"link"`
}

// setupState answers a question the attention list structurally cannot: is this
// platform configured?
//
// Every check in the attention list starts from the enabled rules, so an
// installation with no rules produces an empty list — and an empty list is
// rendered as "everything is fine". On a fresh install that is the worst thing
// the page could say: nothing is wrong precisely because nothing is set up, and
// no alert will ever fire. "Healthy" and "not configured yet" look identical
// from the inside and have to be told apart explicitly.
func setupState(counts map[string]int) map[string]interface{} {
	steps := []setupStep{
		{
			Key: "source", Label: "接入数据源（ES 或 Loki）", Link: "/es-connections",
			Done: counts["es_active"]+counts["loki_active"] > 0,
		},
		{
			Key: "channel", Label: "配置通知渠道（Lark 或 Telegram）", Link: "/notify-channels",
			Done: counts["lark_active"] > 0,
		},
		{
			Key: "rule", Label: "创建并启用一条告警规则", Link: "/alert-rules/create",
			Done: counts["rules_enabled"] > 0,
		},
	}

	done := 0
	for _, s := range steps {
		if s.Done {
			done++
		}
	}

	return map[string]interface{}{
		"configured": done == len(steps),
		"done":       done,
		"total":      len(steps),
		"steps":      steps,
	}
}

// dashboardCounts is the inventory both /dashboard and /stats report. It lives
// in one place so the two endpoints cannot answer the same question with
// different numbers.
func dashboardCounts() map[string]int {
	counts := map[string]int{}
	scanCount := func(key, query string, args ...interface{}) {
		var n int
		if err := database.DB.QueryRow(query, args...).Scan(&n); err == nil {
			counts[key] = n
		}
	}

	// "Today" has to mean today where the team is. CURDATE() is the database
	// session's day, which is UTC here — at 14:00 in +08 that window is only six
	// hours old, so the card read 12 while the 24-hour chart beside it read 68.
	// Compute the day boundary in the display zone instead. It is also a faster
	// query: DATE(created_at) = CURDATE() puts a function on the column and
	// cannot use idx_created_at, while a plain range can.
	nowLocal := time.Now().In(timezone.Location())
	startOfDay := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, timezone.Location())

	scanCount("rules_total", "SELECT COUNT(*) FROM alert_rules")
	scanCount("rules_enabled", "SELECT COUNT(*) FROM alert_rules WHERE status = 1")
	scanCount("today_alerts", "SELECT COUNT(*) FROM alert_logs WHERE created_at >= ?", startOfDay)
	scanCount("today_success", "SELECT COUNT(*) FROM alert_logs WHERE created_at >= ? AND status = 'success'", startOfDay)
	// A partial delivery reached some channels and not others, so it is a
	// failure an operator should see.
	scanCount("today_failed", "SELECT COUNT(*) FROM alert_logs WHERE created_at >= ? AND status IN ('failed', 'partial')", startOfDay)
	scanCount("es_connections", "SELECT COUNT(*) FROM es_connections")
	scanCount("es_active", "SELECT COUNT(*) FROM es_connections WHERE status = 1")
	scanCount("loki_connections", "SELECT COUNT(*) FROM loki_connections")
	scanCount("loki_active", "SELECT COUNT(*) FROM loki_connections WHERE status = 1")
	scanCount("lark_configs", "SELECT COUNT(*) FROM notify_channels")
	scanCount("lark_active", "SELECT COUNT(*) FROM notify_channels WHERE status = 1")

	return counts
}

// HandleGetDashboard returns everything the dashboard renders in one call.
// It is a superset of /stats, which stays as it is for existing callers.
func HandleGetDashboard(w http.ResponseWriter, r *http.Request) {
	counts := dashboardCounts()

	attention := []attentionItem{}
	add := func(item attentionItem) {
		if item.Count > 0 {
			attention = append(attention, item)
		}
	}

	// A rule that errored last run is not alerting, whatever its status says.
	var ruleErrors int
	var firstErrorRule sql.NullString
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_rules
		WHERE status = 1 AND last_error IS NOT NULL AND last_error <> ''`).Scan(&ruleErrors)
	if ruleErrors > 0 {
		database.DB.QueryRow(`SELECT name FROM alert_rules
			WHERE status = 1 AND last_error IS NOT NULL AND last_error <> ''
			ORDER BY id LIMIT 1`).Scan(&firstErrorRule)
		add(attentionItem{
			Kind: "rule_error", Severity: "critical", Label: "规则执行报错", Count: ruleErrors,
			Detail: describeFirst(firstErrorRule, ruleErrors), Link: "/alert-rules",
		})
	}

	overdue, neverRun, overdueName, neverRunName := scanRuleSchedules()
	add(attentionItem{
		Kind: "rule_overdue", Severity: "critical", Label: "规则超期未执行", Count: overdue,
		Detail: describeFirst(sqlStr(overdueName), overdue), Link: "/alert-rules",
	})
	add(attentionItem{
		Kind: "rule_never_run", Severity: "warning", Label: "规则从未执行", Count: neverRun,
		Detail: describeFirst(sqlStr(neverRunName), neverRun), Link: "/alert-rules",
	})

	// A rule with no channel bound produces alerts that go nowhere.
	var noChannel int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_rules r
		WHERE r.status = 1
		  AND NOT EXISTS (SELECT 1 FROM alert_rule_channels c WHERE c.rule_id = r.id)`).Scan(&noChannel)
	add(attentionItem{
		Kind: "rule_no_channel", Severity: "critical", Label: "规则未绑定通知渠道", Count: noChannel,
		Detail: "告警会照常触发，但发不出去", Link: "/alert-rules",
	})

	// Every enabled channel a rule depends on has to be enabled too.
	var mutedChannel int
	database.DB.QueryRow(`SELECT COUNT(DISTINCT r.id) FROM alert_rules r
		JOIN alert_rule_channels c ON c.rule_id = r.id
		JOIN notify_channels n ON n.id = c.channel_id
		WHERE r.status = 1 AND n.status = 0`).Scan(&mutedChannel)
	add(attentionItem{
		Kind: "channel_disabled", Severity: "warning", Label: "规则绑定了已停用的渠道", Count: mutedChannel,
		Detail: "该渠道会被跳过", Link: "/notify-channels",
	})

	// A rule pointing at a data source that is disabled or gone cannot query.
	var sourceDown int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_rules r
		WHERE r.status = 1 AND r.data_source_type = 'es'
		  AND NOT EXISTS (SELECT 1 FROM es_connections e WHERE e.id = r.es_connection_id AND e.status = 1)`).Scan(&sourceDown)
	var lokiDown int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_rules r
		WHERE r.status = 1 AND r.data_source_type = 'loki'
		  AND NOT EXISTS (SELECT 1 FROM loki_connections l WHERE l.id = r.loki_connection_id AND l.status = 1)`).Scan(&lokiDown)
	add(attentionItem{
		Kind: "source_unavailable", Severity: "critical", Label: "规则的数据源不可用", Count: sourceDown + lokiDown,
		Detail: "连接被停用或已删除", Link: "/es-connections",
	})

	// Delivery failures in the last day, including partial fan-outs.
	var sendFailed int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_logs
		WHERE created_at >= NOW() - INTERVAL 24 HOUR AND status IN ('failed', 'partial')`).Scan(&sendFailed)
	add(attentionItem{
		Kind: "send_failed", Severity: "critical", Label: "24 小时内发送失败", Count: sendFailed,
		Detail: "含部分渠道失败", Link: "/alert-logs",
	})

	jsonSuccess(w, map[string]interface{}{
		"checked_at":    time.Now().UTC(),
		"counts":        counts,
		"setup":         setupState(counts),
		"attention":     attention,
		"trend_24h":     trend24h(),
		"recent_alerts": recentAlerts(),
		"active_mutes":  activeMutes(),
		"noisy_rules":   noisyRules(),
	})
}

// describeFirst names one example so the operator has somewhere to start.
func describeFirst(name sql.NullString, count int) string {
	if !name.Valid || name.String == "" {
		return ""
	}
	if count > 1 {
		return fmt.Sprintf("%s 等 %d 条", name.String, count)
	}
	return name.String
}

func sqlStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// scanRuleSchedules compares each enabled rule's last run against what its own
// cron expression says should already have happened. A rule whose expression
// will not parse is left alone — that is a configuration problem the rules
// page reports, not a scheduler failure.
func scanRuleSchedules() (overdue, neverRun int, overdueName, neverRunName string) {
	now := time.Now()

	rows, err := database.DB.Query(`SELECT name, schedule, last_run_at, created_at
		FROM alert_rules WHERE status = 1`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name, schedule string
		var lastRun, createdAt sql.NullTime
		if err := rows.Scan(&name, &schedule, &lastRun, &createdAt); err != nil {
			continue
		}

		if !lastRun.Valid {
			if createdAt.Valid && now.Sub(createdAt.Time) > neverRunGrace {
				neverRun++
				if neverRunName == "" {
					neverRunName = name
				}
			}
			continue
		}

		// Same parser and same five-to-six-field normalization the scheduler
		// uses, so "overdue" means overdue by the rule's real schedule.
		sched, err := alert.ParseSchedule(schedule)
		if err != nil {
			continue
		}
		// cron.Schedule.Next evaluates the expression in the location of the
		// time it is given, and the scheduler registers jobs in the display
		// zone. Handing it a UTC instant would evaluate "0 3 * * *" against the
		// wrong wall clock and report a rule overdue that is running on time.
		due := sched.Next(lastRun.Time.In(timezone.Location()))
		afterThat := sched.Next(due)
		if now.After(afterThat.Add(overdueGrace)) {
			overdue++
			if overdueName == "" {
				overdueName = name
			}
		}
	}
	// A truncated sweep would under-report, and under-reporting here reads as
	// "no rule is overdue" — the exact false all-clear this panel exists to
	// prevent. Report nothing instead.
	if err := rows.Err(); err != nil {
		log.Printf("[Dashboard] rule schedule scan cut short: %v", err)
		return 0, 0, "", ""
	}
	return
}

// trend24h buckets the last 24 hours by hour, always returning 24 points so
// the chart has a fixed shape whether or not anything fired.
// Bucket keys are built in the same zone MySQL's DATE_FORMAT used to group the
// rows — the session's, which the DSN pins to UTC — so a row can never land in
// a bucket the axis does not show. The axis labels are a separate concern and
// are rendered in the display zone, which is what an operator reads.
func trend24h() []trendPoint {
	// DATE_FORMAT returns a string, and parseTime only converts real date and
	// time columns — scanning it straight into a time.Time fails and silently
	// empties the chart. Read it as text and parse it here, against the session
	// zone the DSN pins to UTC.
	var startStr string
	if err := database.DB.QueryRow(
		`SELECT DATE_FORMAT(NOW() - INTERVAL 23 HOUR, '%Y-%m-%d %H:00:00')`).Scan(&startStr); err != nil {
		return []trendPoint{}
	}
	start, err := time.ParseInLocation("2006-01-02 15:04:05", startStr, time.UTC)
	if err != nil {
		return []trendPoint{}
	}

	byHour := map[string]int{}
	rows, err := database.DB.Query(`SELECT DATE_FORMAT(created_at, '%Y-%m-%d %H'), COUNT(*)
		FROM alert_logs WHERE created_at >= NOW() - INTERVAL 23 HOUR
		GROUP BY DATE_FORMAT(created_at, '%Y-%m-%d %H')`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var bucket string
			var n int
			if err := rows.Scan(&bucket, &n); err == nil {
				byHour[bucket] = n
			}
		}
		// Missing buckets do not leave a gap, they draw shorter bars — a wrong
		// chart rather than an obviously absent one. Fall back to the empty
		// state instead.
		if err := rows.Err(); err != nil {
			log.Printf("[Dashboard] trend scan cut short: %v", err)
			return []trendPoint{}
		}
	}

	points := make([]trendPoint, 0, 24)
	for i := range 24 {
		t := start.Add(time.Duration(i) * time.Hour)
		points = append(points, trendPoint{
			Label: t.In(timezone.Location()).Format("15:04"),
			Count: byHour[t.UTC().Format("2006-01-02 15")],
		})
	}
	return points
}

func recentAlerts() []recentAlert {
	out := []recentAlert{}
	rows, err := database.DB.Query(`SELECT id, rule_id, rule_name, severity, status, created_at
		FROM alert_logs ORDER BY created_at DESC, id DESC LIMIT 5`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a recentAlert
		if err := rows.Scan(&a.ID, &a.RuleID, &a.RuleName, &a.Severity, &a.Status, &a.CreatedAt); err != nil {
			continue
		}
		out = append(out, a)
	}
	// This panel is openly a sample of the newest few, so a short read is
	// honest; it is still worth knowing it happened.
	if err := rows.Err(); err != nil {
		log.Printf("[Dashboard] recent alerts scan cut short: %v", err)
	}
	return out
}

// activeMutes surfaces deliberate silences. They are the easiest thing to set
// and forget, and a forgotten mute is a real missed alert.
func activeMutes() map[string]interface{} {
	var count int
	database.DB.QueryRow(`SELECT COUNT(*) FROM alert_mutes WHERE mute_until > NOW()`).Scan(&count)

	items := []muteItem{}
	// remaining_seconds is computed by the database too, so the countdown the
	// UI shows does not depend on the viewer's clock agreeing with the server's.
	rows, err := database.DB.Query(`SELECT m.rule_id, COALESCE(r.name, ''), m.group_key,
			m.mute_until,
			TIMESTAMPDIFF(SECOND, NOW(), m.mute_until),
			m.reason
		FROM alert_mutes m LEFT JOIN alert_rules r ON r.id = m.rule_id
		WHERE m.mute_until > NOW() ORDER BY m.mute_until LIMIT 5`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m muteItem
			if err := rows.Scan(&m.RuleID, &m.RuleName, &m.GroupKey, &m.MuteUntil, &m.RemainingSeconds, &m.Reason); err != nil {
				continue
			}
			items = append(items, m)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Dashboard] active mutes scan cut short: %v", err)
		}
	}
	return map[string]interface{}{"count": count, "items": items}
}

// noisyRules ranks the last week's alert volume, which is where rule tuning
// (dedup window, thresholds) pays off first.
func noisyRules() []noisyRule {
	out := []noisyRule{}
	rows, err := database.DB.Query(`SELECT l.rule_id, COALESCE(NULLIF(l.rule_name, ''), r.name, ''), COUNT(*) AS n
		FROM alert_logs l LEFT JOIN alert_rules r ON r.id = l.rule_id
		WHERE l.created_at >= NOW() - INTERVAL 7 DAY
		GROUP BY l.rule_id, l.rule_name, r.name
		ORDER BY n DESC LIMIT 5`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var n noisyRule
		if err := rows.Scan(&n.RuleID, &n.RuleName, &n.Count); err != nil {
			continue
		}
		out = append(out, n)
	}
	// A ranking missing its rows is not a shorter ranking, it is a wrong one.
	if err := rows.Err(); err != nil {
		log.Printf("[Dashboard] noisy rules scan cut short: %v", err)
		return []noisyRule{}
	}
	return out
}
