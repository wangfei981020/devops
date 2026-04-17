package alert

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/lark"
	"opsplatform-alert-backend/models"
)

// processPerformanceHit handles a single hit in performance-alert mode.
// Returns true if the caller should proceed to send a realtime alert,
// false if the caller should skip this hit (continue).
//
// Side effects:
//   - Validates exactly one http(s):// URL in the raw line (else logs error and returns false)
//   - Accumulates (tid -> cost_ms) into the per-domain daily stats bucket (HSETNX dedup by tid)
//   - Tracks domains seen today in a Set for the daily-report scan
//   - Uses alert:sent_tids:{ruleID}:{YYYYMMDD} to enforce "one alert per tid per day"
//
// Template vars injected for realtime alert: threshold_ms, cost_ms (int), domain, tid, container, namespace
func processPerformanceHit(ctx context.Context, rule *models.AlertRule, hit map[string]interface{}, vars map[string]interface{}) bool {
	// Locate the raw log line
	lineStr := ""
	if v, ok := hit["line"].(string); ok {
		lineStr = v
	} else if v, ok := hit["message"].(string); ok {
		lineStr = v
	}

	// URL count must equal 1 (per spec: one log line = one URL)
	urlCount := strings.Count(lineStr, "http://") + strings.Count(lineStr, "https://")
	if urlCount != 1 {
		log.Printf("[Perf] Rule %d: URL count=%d, expected 1, skipped. line=%s", rule.ID, urlCount, truncate(lineStr, 200))
		return false
	}

	tid := strings.TrimSpace(fmt.Sprintf("%v", vars["tid"]))
	domain := strings.TrimSpace(fmt.Sprintf("%v", vars["domain"]))
	costStr := strings.TrimSpace(fmt.Sprintf("%v", vars["cost_ms"]))
	cost, err := strconv.Atoi(costStr)
	if tid == "" || tid == "<nil>" || domain == "" || domain == "<nil>" || err != nil || cost <= 0 {
		log.Printf("[Perf] Rule %d: extract incomplete tid=%s domain=%s cost=%s err=%v, skipped", rule.ID, tid, domain, costStr, err)
		return false
	}

	today := time.Now().Format("20060102")

	// Accumulate to daily stats bucket (HSETNX: same tid only counted once even across overlapping queries)
	if rule.ReportEnabled == 1 {
		statsKey := fmt.Sprintf("alert:daily_stats:%d:%s:%s", rule.ID, today, domain)
		domainsKey := fmt.Sprintf("alert:daily_stats_domains:%d:%s", rule.ID, today)
		database.RDB.HSetNX(ctx, statsKey, tid, cost)
		database.RDB.Expire(ctx, statsKey, 48*time.Hour)
		database.RDB.SAdd(ctx, domainsKey, domain)
		database.RDB.Expire(ctx, domainsKey, 48*time.Hour)
	}

	// Realtime alert gating
	if rule.RealtimeEnabled != 1 {
		return false // stats-only mode, no realtime alert
	}
	if rule.ThresholdMs > 0 && cost <= rule.ThresholdMs {
		return false // under threshold
	}
	// One alert per tid per day
	sentKey := fmt.Sprintf("alert:sent_tids:%d:%s", rule.ID, today)
	added, _ := database.RDB.SAdd(ctx, sentKey, tid).Result()
	database.RDB.Expire(ctx, sentKey, 48*time.Hour)
	if added == 0 {
		return false // already alerted today
	}

	// Inject convenience vars for template rendering
	vars["threshold_ms"] = rule.ThresholdMs
	vars["cost_ms"] = cost
	vars["domain"] = domain
	vars["tid"] = tid
	return true
}

// domainStats holds aggregated stats for one domain over one day
type domainStats struct {
	Domain  string `json:"domain"`
	Count   int    `json:"count"`
	MinMs   int    `json:"min_ms"`
	AvgMs   int    `json:"avg_ms"`
	MaxMs   int    `json:"max_ms"`
	Date    string `json:"date"`
	SendTime string `json:"send_time"`
}

// sendDailyReport aggregates yesterday's per-domain performance stats and pushes Lark card(s).
// Triggered by cron registered in addReportJob.
func sendDailyReport(ruleID int) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rule, err := getRuleByID(ruleID)
	if err != nil || rule.Status != 1 || rule.ReportEnabled != 1 {
		log.Printf("[Report] Rule %d: not eligible (err=%v status=%d report_enabled=%d)", ruleID, err, rule.Status, rule.ReportEnabled)
		return
	}

	// Yesterday (report is for the day before now)
	yesterday := time.Now().AddDate(0, 0, -1).Format("20060102")
	dateStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	sendTimeStr := time.Now().Format("2006-01-02 15:04:05")

	domainsKey := fmt.Sprintf("alert:daily_stats_domains:%d:%s", rule.ID, yesterday)
	domains, err := database.RDB.SMembers(ctx, domainsKey).Result()
	if err != nil || len(domains) == 0 {
		log.Printf("[Report] Rule %d: no domains for %s (err=%v)", rule.ID, yesterday, err)
		return
	}
	sort.Strings(domains)

	// Aggregate per domain
	statsList := make([]domainStats, 0, len(domains))
	for _, domain := range domains {
		statsKey := fmt.Sprintf("alert:daily_stats:%d:%s:%s", rule.ID, yesterday, domain)
		values, err := database.RDB.HVals(ctx, statsKey).Result()
		if err != nil || len(values) == 0 {
			continue
		}
		minV := -1
		maxV := 0
		total := 0
		count := 0
		for _, v := range values {
			n, e := strconv.Atoi(v)
			if e != nil {
				continue
			}
			if minV == -1 || n < minV {
				minV = n
			}
			if n > maxV {
				maxV = n
			}
			total += n
			count++
		}
		if count == 0 {
			continue
		}
		if minV < 0 {
			minV = 0
		}
		statsList = append(statsList, domainStats{
			Domain:   domain,
			Count:    count,
			MinMs:    minV,
			AvgMs:    total / count,
			MaxMs:    maxV,
			Date:     dateStr,
			SendTime: sendTimeStr,
		})
	}

	if len(statsList) == 0 {
		log.Printf("[Report] Rule %d: all domain buckets empty, nothing to send", rule.ID)
		cleanupReportKeys(ctx, rule.ID, yesterday, domains)
		return
	}

	larkCfg, err := getLarkConfigByID(rule.LarkConfigID)
	if err != nil {
		log.Printf("[Report] Rule %d: lark config error: %v", rule.ID, err)
		return
	}
	sender := lark.NewSender(*larkCfg)
	atUsers := resolveAtUsers(rule.AtUsers)
	atAll := rule.AtAll == 1

	title := rule.ReportTitle
	if title == "" {
		title = "每日性能报告"
	}
	mode := rule.ReportMode
	if mode == "" {
		mode = "separate"
	}

	if mode == "merged" {
		content := renderReportMerged(rule.ReportTemplate, statsList, dateStr, sendTimeStr)
		if resp, err := sender.SendCard(title, content, "info", atUsers, atAll); err != nil {
			log.Printf("[Report] Rule %d: send merged failed: %v", rule.ID, err)
		} else {
			log.Printf("[Report] Rule %d: merged report sent, resp=%s", rule.ID, resp)
		}
	} else {
		// separate: one card per domain
		for _, s := range statsList {
			content := renderReportSeparate(rule.ReportTemplate, s)
			if resp, err := sender.SendCard(title, content, "info", atUsers, atAll); err != nil {
				log.Printf("[Report] Rule %d: send separate (%s) failed: %v", rule.ID, s.Domain, err)
			} else {
				log.Printf("[Report] Rule %d: separate report for %s sent, resp=%s", rule.ID, s.Domain, resp)
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	cleanupReportKeys(ctx, rule.ID, yesterday, domains)
}

func renderReportSeparate(tmplStr string, s domainStats) string {
	if tmplStr == "" {
		return fmt.Sprintf("电子钱包性能统计\n\n**域名:** %s\n**交易:** %d 次\n**性能:** 最快 %dms  平均 %dms  最慢 %dms\n**日期:** %s  %s",
			s.Domain, s.Count, s.MinMs, s.AvgMs, s.MaxMs, s.Date, s.SendTime)
	}
	tmpl, err := template.New("report").Parse(tmplStr)
	if err != nil {
		return fmt.Sprintf("模板解析错误: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]interface{}{
		"domain":    s.Domain,
		"count":     s.Count,
		"min_ms":    s.MinMs,
		"avg_ms":    s.AvgMs,
		"max_ms":    s.MaxMs,
		"date":      s.Date,
		"send_time": s.SendTime,
	}); err != nil {
		return fmt.Sprintf("模板渲染错误: %v", err)
	}
	return buf.String()
}

func renderReportMerged(tmplStr string, stats []domainStats, date, sendTime string) string {
	if tmplStr == "" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("**日期:** %s  **发送时间:** %s\n\n", date, sendTime))
		for _, s := range stats {
			sb.WriteString(fmt.Sprintf("**域名:** %s\n**交易:** %d 次  最快 %dms  平均 %dms  最慢 %dms\n\n", s.Domain, s.Count, s.MinMs, s.AvgMs, s.MaxMs))
		}
		return sb.String()
	}
	tmpl, err := template.New("report").Parse(tmplStr)
	if err != nil {
		return fmt.Sprintf("模板解析错误: %v", err)
	}
	// Convert stats to slice of maps so templates can use .stats range
	statsMaps := make([]map[string]interface{}, len(stats))
	for i, s := range stats {
		statsMaps[i] = map[string]interface{}{
			"domain": s.Domain,
			"count":  s.Count,
			"min_ms": s.MinMs,
			"avg_ms": s.AvgMs,
			"max_ms": s.MaxMs,
		}
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]interface{}{
		"stats":     statsMaps,
		"date":      date,
		"send_time": sendTime,
	}); err != nil {
		return fmt.Sprintf("模板渲染错误: %v", err)
	}
	return buf.String()
}

func cleanupReportKeys(ctx context.Context, ruleID int, day string, domains []string) {
	// Delete stats hash for each domain and the domains set itself.
	for _, domain := range domains {
		database.RDB.Del(ctx, fmt.Sprintf("alert:daily_stats:%d:%s:%s", ruleID, day, domain))
	}
	database.RDB.Del(ctx, fmt.Sprintf("alert:daily_stats_domains:%d:%s", ruleID, day))
	database.RDB.Del(ctx, fmt.Sprintf("alert:sent_tids:%d:%s", ruleID, day))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
