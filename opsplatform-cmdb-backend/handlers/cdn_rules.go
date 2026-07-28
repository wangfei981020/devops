package handlers

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/cdnsource"
)

// CDN 规则台账与优化分析。
//
// ⚠️ 采集侧（cdnsource/cloudflare_rules.go）未经真实账号验证。这里的查询接口
// 在没有数据时明确区分「确实没配规则」和「没采到」——两者结论完全相反：
// 前者说明缓存/HTTPS 策略缺失，后者说明 token 权限不够。

func (h *CDNHandler) RegisterRules(r *gin.RouterGroup) {
	r.GET("/cdn/rules", h.ListRules)           // zone?, source?
	r.GET("/cdn/rule-analysis", h.RuleAudit)   // zone?
	r.GET("/cdn/certificates", h.ListCDNCerts) // zone?
}

// ListRules 规则台账。
func (h *CDNHandler) ListRules(c *gin.Context) {
	q := `SELECT zone_name,source,rule_id,name,phase,kind,priority,status,
	        COALESCE(expression,''),COALESCE(actions,''),last_updated
	      FROM cdn_rules WHERE 1=1`
	args := []any{}
	if z := c.Query("zone"); z != "" {
		q += " AND zone_name=?"
		args = append(args, z)
	}
	if s := c.Query("source"); s != "" {
		q += " AND source=?"
		args = append(args, s)
	}
	rows, err := h.DB.Query(q+" ORDER BY zone_name, source, priority", args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var zn, src, rid, name, phase, kind, status, expr, acts, upd string
		var pri int
		if rows.Scan(&zn, &src, &rid, &name, &phase, &kind, &pri, &status, &expr, &acts, &upd) != nil {
			continue
		}
		items = append(items, gin.H{"zone": zn, "source": src, "rule_id": rid, "name": name,
			"phase": phase, "kind": kind, "priority": pri, "status": status,
			"expression": expr, "actions": acts, "last_updated": upd})
	}
	out := gin.H{"total": len(items), "rules": items}
	if len(items) == 0 {
		out["empty_hint"] = "没有规则数据。两种可能且结论相反：(1) 该账号确实没配任何 Page Rule / Ruleset；" +
			"(2) token 缺 Page Rules·Read 或 Config·Read 权限，压根没采到。" +
			"区分方法：查采集日志里 [cf-rules] 开头的行——有 ERROR 就是第 2 种"
	}
	c.JSON(http.StatusOK, out)
}

type ruleFinding struct {
	Severity string `json:"severity"`
	Zone     string `json:"zone"`
	Issue    string `json:"issue"`
	Action   string `json:"action"`
}

// RuleAudit 规则优化分析。
//
// 只做有确定依据的判定：数量逼近套餐上限、禁用规则堆积、同一匹配式重复配置。
// 不去猜「这条规则该不该存在」——那要懂业务，CMDB 不该替人做这个判断。
func (h *CDNHandler) RuleAudit(c *gin.Context) {
	zoneFilter := c.Query("zone")

	// 各 zone 的套餐，用来算 Page Rules 上限
	plans := map[string]string{}
	if rows, err := h.DB.Query(`SELECT name, COALESCE(plan,'') FROM cdn_zones`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var n, p string
			if rows.Scan(&n, &p) == nil {
				plans[n] = p
			}
		}
	}

	type zoneStat struct {
		pageRules, disabled int
		exprCount           map[string]int
		hasHTTPS            bool
	}
	stats := map[string]*zoneStat{}

	q := `SELECT zone_name,source,status,COALESCE(expression,''),COALESCE(actions,'') FROM cdn_rules`
	args := []any{}
	if zoneFilter != "" {
		q += " WHERE zone_name=?"
		args = append(args, zoneFilter)
	}
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var zn, src, status, expr, acts string
		if rows.Scan(&zn, &src, &status, &expr, &acts) != nil {
			continue
		}
		total++
		st := stats[zn]
		if st == nil {
			st = &zoneStat{exprCount: map[string]int{}}
			stats[zn] = st
		}
		if src == "pagerule" {
			st.pageRules++
			if expr != "" {
				st.exprCount[expr]++
			}
		}
		if strings.EqualFold(status, "disabled") {
			st.disabled++
		}
		low := strings.ToLower(acts)
		if strings.Contains(low, "always_use_https") || strings.Contains(low, "automatic_https_rewrites") {
			st.hasHTTPS = true
		}
	}

	findings := []ruleFinding{}
	for zn, st := range stats {
		if limit := cdnsource.PageRuleLimit(plans[zn]); limit > 0 {
			switch {
			case st.pageRules >= limit:
				findings = append(findings, ruleFinding{"high", zn,
					"Page Rules 已达套餐上限（" + itoa(st.pageRules) + "/" + itoa(limit) + "）",
					"再加规则会直接失败。合并同类规则，或把老 Page Rule 迁到 Rulesets（新体系不占这个额度）"})
			case float64(st.pageRules) >= float64(limit)*0.8:
				findings = append(findings, ruleFinding{"medium", zn,
					"Page Rules 接近套餐上限（" + itoa(st.pageRules) + "/" + itoa(limit) + "）",
					"提前规划：合并同类规则，或迁到 Rulesets"})
			}
		}
		if st.disabled > 0 {
			findings = append(findings, ruleFinding{"low", zn,
				"有 " + itoa(st.disabled) + " 条已禁用的规则",
				"禁用的 Page Rule 仍占用套餐额度。确认不再需要就删掉"})
		}
		for expr, n := range st.exprCount {
			if n > 1 {
				findings = append(findings, ruleFinding{"medium", zn,
					"匹配式 " + expr + " 上有 " + itoa(n) + " 条 Page Rule",
					"Page Rules 只有优先级最高的一条生效，其余是无效配置——确认哪条该留，删掉其余"})
			}
		}
		if !st.hasHTTPS {
			findings = append(findings, ruleFinding{"low", zn,
				"没有发现强制 HTTPS 相关规则（always_use_https / automatic_https_rewrites）",
				"若站点已通过 Zone Settings 的 Always Use HTTPS 开关启用，则此项可忽略——" +
					"该开关不体现在规则里，需要单独确认"})
		}
	}
	sort.SliceStable(findings, func(i, j int) bool {
		rank := map[string]int{"high": 0, "medium": 1, "low": 2}
		if rank[findings[i].Severity] != rank[findings[j].Severity] {
			return rank[findings[i].Severity] < rank[findings[j].Severity]
		}
		return findings[i].Zone < findings[j].Zone
	})

	sum := map[string]int{}
	for _, f := range findings {
		sum[f.Severity]++
	}
	out := gin.H{"total_rules": total, "zones_analyzed": len(stats), "summary": sum, "findings": findings}
	if total == 0 {
		out["not_analyzable"] = "没有任何规则数据，本次分析不成立——「0 个问题」在这种情况下不代表规则配置没问题。" +
			"先确认是没配规则，还是 token 权限不够没采到（查 [cf-rules] 日志）"
	}
	c.JSON(http.StatusOK, out)
}

// ListCDNCerts CDN 边缘证书。
//
// 与「证书」「到期巡检」两个模块查的是不同东西：那两个管的是我方部署在源站的证书，
// 这里是 Cloudflare 边缘上的证书。两者到期时间互相独立——边缘证书没过期
// 不代表源站证书没过期，反过来源站过期时若 SSL 模式不是 strict，用户侧甚至看不出异常。
func (h *CDNHandler) ListCDNCerts(c *gin.Context) {
	q := `SELECT zone_name,pack_id,type,COALESCE(hosts,''),issuer,status,expires_on
	      FROM cdn_certificates WHERE 1=1`
	args := []any{}
	if z := c.Query("zone"); z != "" {
		q += " AND zone_name=?"
		args = append(args, z)
	}
	rows, err := h.DB.Query(q+" ORDER BY expires_on IS NULL, expires_on, zone_name", args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	sum := map[string]int{}
	for rows.Next() {
		var zn, pid, typ, hosts, issuer, status string
		var exp *time.Time
		if rows.Scan(&zn, &pid, &typ, &hosts, &issuer, &status, &exp) != nil {
			continue
		}
		it := gin.H{"zone": zn, "pack_id": pid, "type": typ, "hosts": hosts,
			"issuer": issuer, "status": status}
		if exp != nil {
			days := int(time.Until(*exp).Hours() / 24)
			it["expires_on"], it["days_left"] = exp.Format("2006-01-02"), days
			switch {
			case days < 0:
				it["severity"] = "high"
				it["issue"] = "边缘证书已过期"
				sum["high"]++
			case days <= 14:
				it["severity"] = "medium"
				it["issue"] = "边缘证书 " + itoa(days) + " 天后到期（Universal SSL 通常自动续期，未续说明有问题）"
				sum["medium"]++
			}
		} else {
			// 到期时间为空要说出来，否则会被当成「不会过期」
			it["expires_on"] = ""
			it["note"] = "未取到到期时间（证书可能处于签发中，或响应里没有该字段）"
		}
		items = append(items, it)
	}
	out := gin.H{"total": len(items), "summary": sum, "certificates": items}
	if len(items) == 0 {
		out["empty_hint"] = "没有 CDN 边缘证书数据。最可能的原因是 token 缺 Zone·SSL and Certificates·Read 权限——" +
			"启用了 Cloudflare 代理的站点一定有 Universal SSL 证书，所以空结果基本等于没采到。" +
			"查采集日志里 [cf-cert] 开头的行确认。注意：本项为空不代表站点没有证书"
	}
	out["note"] = "这里是 Cloudflare 边缘上的证书，与「证书 / 到期巡检」里我方源站的证书是两套，" +
		"到期时间互相独立，不能用一个推另一个"
	c.JSON(http.StatusOK, out)
}
