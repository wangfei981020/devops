package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/cdnsource"
)

// CDN 边缘安全事件查询。
//
// ⚠️ 与其它 /api/cdn/* 不同，这个接口**实时调 Cloudflare GraphQL，不读库**——
// 事件是流水，没有落库，也不该落库（量大且有保留期）。
//
// 它回答规则台账回答不了的那个问题：规则配在那儿，到底有没有命中过某个请求。
// 排查「对方说访问我们超时」这类跨公网问题时，这是把责任分到「对端出网」还是
// 「我方 CDN」的关键一步——没有它，只能靠猜或者让人去点控制台。

// CDNSecurityEvents GET /api/cdn/security-events
//
// 参数：zone(根域名,必填) host(主机名过滤) since/until(北京时间,可选)
//
//	minutes(时间窗,默认60,since 存在时忽略) limit(默认200)
func (h *CDNHandler) CDNSecurityEvents(c *gin.Context) {
	zone := strings.TrimSpace(c.Query("zone"))
	if zone == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false,
			"error": "必须指定 zone（根域名，如 g32cf.com）。用 list_cdn_zones 查有哪些站点", "error_key": "error.cdnZoneRequired"})
		return
	}

	var zoneID string
	var accountID int
	err := h.DB.QueryRow(`SELECT zone_id, account_id FROM cdn_zones WHERE name=? LIMIT 1`, zone).
		Scan(&zoneID, &accountID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{"ok": false,
			"error":     "CMDB 里没有这个站点: " + zone + "（若刚加的账号，先到「管理 → 云账号」同步一次）",
			"error_key": "error.cdnZoneUnknown", "error_params": gin.H{"zone": zone}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	since, until, timeNote, err := resolveWindow(c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))

	cli, err := h.clientFor(strconv.Itoa(accountID))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	events, err := cli.FirewallEvents(ctx, cdnsource.FirewallEventQuery{
		ZoneID: zoneID, Host: strings.TrimSpace(c.Query("host")),
		Since: since, Until: until, Limit: limit,
	})
	if err != nil {
		// 查询失败必须报错，不能返回空列表——
		// 「没查到事件」和「查不了」的结论完全相反，前者能给对方交代，后者不能。
		// ⚠️ 工具链只进 mcp_hint：界面上的人执行不了 cdn_token_check，
		//	把它写进 hint 等于给运维一句他做不到的下一步（check-mcp-text-leak）
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(),
			"hint_key": "error.cdnEventsNeedFirewallRead",
			"hint":     "若提示权限不足，令牌需要 Zone·Firewall Services·Read 权限",
			"mcp_hint": "若提示权限不足，用 cdn_token_check 看缺哪一项（需 Zone·Firewall Services·Read）"})
		return
	}

	// 按动作和规则聚合，先看全貌再看明细
	byAction := map[string]int{}
	byRule := map[string]int{}
	for _, e := range events {
		byAction[e.Action]++
		if e.RuleID != "" {
			byRule[e.RuleID+" ("+e.Source+")"]++
		}
	}

	out := gin.H{
		"ok": true, "realtime": true, "zone": zone,
		"host":       c.Query("host"),
		"window_cst": timeNote,
		"total":      len(events),
		"by_action":  sortedCount(byAction),
		"by_rule":    sortedCount(byRule),
		"events":     events,
	}
	if len(events) == 0 {
		// 空结果是结论，但要说清它的边界，否则会被当成「一定没被拦过」
		out["empty_meaning"] = "该时间窗内，CF 边缘对匹配条件的请求**没有**产生任何安全事件" +
			"（未被 block / challenge / 限速拦截）。注意两点：" +
			"(1) 只有触发了规则才会产生事件，正常放行的请求不在这里；" +
			"(2) firewallEventsAdaptive 有保留期，查太久以前会返回空——" +
			"先用一个近期时间窗验证能查到数据，再判断历史窗口的空结果"
	}
	if limit > 0 && len(events) >= limit {
		out["truncated"] = "结果已达 limit=" + strconv.Itoa(limit) + " 条，可能还有更多；调大 limit 或缩小时间窗"
	}
	c.JSON(http.StatusOK, out)
}

// CDNTraffic GET /api/cdn/traffic
//
// 参数：zone(必填) host path client_ip since/until(北京时间) minutes limit min_origin_ms
//
// 用途见 cdnsource/cloudflare_traffic.go 顶部：把「对方发出 → 我方应用读到」
// 这段总耗时，用边缘的 edgeStartTimestamp 切成「到达 CF 之前」和「CF 之后」两段。
func (h *CDNHandler) CDNTraffic(c *gin.Context) {
	zone := strings.TrimSpace(c.Query("zone"))
	if zone == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false,
			"error": "必须指定 zone（根域名，如 g32cf.com）", "error_key": "error.cdnZoneRequired"})
		return
	}

	var zoneID string
	var accountID int
	err := h.DB.QueryRow(`SELECT zone_id, account_id FROM cdn_zones WHERE name=? LIMIT 1`, zone).
		Scan(&zoneID, &accountID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "CMDB 里没有这个站点: " + zone})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	since, until, timeNote, err := resolveWindow(c)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	minDur, _ := strconv.Atoi(c.DefaultQuery("min_origin_ms", "0"))

	cli, err := h.clientFor(strconv.Itoa(accountID))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	// introspect：查这个数据集到底有哪些字段。
	// CF 的 GraphQL 与 Logpush 字段命名不是一套，靠猜就要反复发版——有了它当场能查。
	if t := strings.TrimSpace(c.Query("introspect")); t != "" {
		typeName := t
		if t == "1" || strings.EqualFold(t, "true") {
			typeName = "Zone"
		}
		names, err := cli.IntrospectFields(ctx, typeName)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "realtime": true,
			"type": typeName, "field_count": len(names), "fields": names,
			"hint": "拿到字段名后用 fields 参数指定，如 fields=datetime,clientIP,originResponseDurationMs"})
		return
	}

	var customFields []string
	if f := strings.TrimSpace(c.Query("fields")); f != "" {
		for _, s := range strings.Split(f, ",") {
			if s = strings.TrimSpace(s); s != "" {
				customFields = append(customFields, s)
			}
		}
	}

	reqs, err := cli.HTTPRequests(ctx, cdnsource.HTTPRequestQuery{
		ZoneID: zoneID, Host: strings.TrimSpace(c.Query("host")),
		Path: strings.TrimSpace(c.Query("path")), ClientIP: strings.TrimSpace(c.Query("client_ip")),
		Since: since, Until: until, Limit: limit, MinOriginMs: minDur,
		Fields: customFields,
	})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(),
			"hint_key": "error.cdnTrafficNeedAnalyticsRead",
			"hint": "若提示权限不足，令牌需要 Zone·Zone Analytics·Read 权限；" +
				"若提示字段未知，多半是该数据集在当前套餐不可用",
			"mcp_hint": "若提示权限不足需 Zone·Zone Analytics·Read（用 cdn_token_check 验）；" +
				"若提示字段未知，多半是该数据集在当前套餐不可用"})
		return
	}

	out := gin.H{
		"ok": true, "realtime": true, "zone": zone,
		"host": c.Query("host"), "path": c.Query("path"),
		"window_cst": timeNote, "total": len(reqs), "requests": reqs,
		"how_to_read": "datetime_cst = CF 边缘收到该请求的时刻（北京时间）。" +
			"把它与「对方发出的时刻」和「我方应用读到的时刻」三者对比：" +
			"边缘时刻≈我方应用时刻 → 延迟在到达 CF 之前（对端出网侧）；" +
			"边缘时刻≈对方发出时刻 → 延迟在 CF 内部（回源滞留）。" +
			"originResponseDurationMs 是回源耗时，可单独判断我方源站快慢",
	}
	if len(reqs) == 0 {
		out["empty_meaning"] = "该条件下没有请求记录。⚠️ 空结果**不等于没有请求**：" +
			"该数据集按套餐有采样与保留期限制，查询窗口超出保留期同样返回空。" +
			"下结论前先用一个近期窗口（如 minutes=60）验证能查到数据"
	}
	if limit > 0 && len(reqs) >= limit {
		out["truncated"] = "结果已达 limit=" + strconv.Itoa(limit) + " 条，可能还有更多"
	}
	c.JSON(http.StatusOK, out)
}

// resolveWindow 解析时间窗。
//
// since/until 一律按**北京时间**解析——群里对时都说北京时间，
// 让人手填 UTC 必然会有人少算 8 小时，那种错还很难发现。
func resolveWindow(c *gin.Context) (time.Time, time.Time, string, error) {
	cst := time.FixedZone("CST", 8*3600)
	const layout = "2006-01-02 15:04:05"

	parse := func(s string) (time.Time, bool) {
		s = strings.TrimSpace(strings.ReplaceAll(s, "T", " "))
		s = strings.TrimSuffix(s, "Z")
		for _, l := range []string{layout, "2006-01-02 15:04", "2006-01-02"} {
			if t, err := time.ParseInLocation(l, s, cst); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}

	sinceStr := c.Query("since")
	if sinceStr != "" {
		since, ok := parse(sinceStr)
		if !ok {
			return time.Time{}, time.Time{}, "", &cdnError{
				"since 格式不对，用北京时间，如 2026-08-14 15:00:00"}
		}
		until := time.Now().In(cst)
		if u := c.Query("until"); u != "" {
			t, ok := parse(u)
			if !ok {
				return time.Time{}, time.Time{}, "", &cdnError{
					"until 格式不对，用北京时间，如 2026-08-14 21:00:00"}
			}
			until = t
		}
		if !until.After(since) {
			return time.Time{}, time.Time{}, "", &cdnError{"until 必须晚于 since"}
		}
		return since, until, since.Format(layout) + " ~ " + until.Format(layout) + "（北京时间）", nil
	}

	minutes, _ := strconv.Atoi(c.DefaultQuery("minutes", "60"))
	if minutes <= 0 {
		minutes = 60
	}
	until := time.Now()
	since := until.Add(-time.Duration(minutes) * time.Minute)
	return since, until,
		since.In(cst).Format(layout) + " ~ " + until.In(cst).Format(layout) + "（北京时间，最近 " +
			strconv.Itoa(minutes) + " 分钟）", nil
}

func sortedCount(m map[string]int) []gin.H {
	out := make([]gin.H, 0, len(m))
	for k, v := range m {
		out = append(out, gin.H{"key": k, "count": v})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["count"].(int) > out[j]["count"].(int)
	})
	return out
}
