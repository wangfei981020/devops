package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/api/middleware"
)

// 一期支持的规则类型。前端的"新建"入口按这个列表渲染；
// 二期类型不在这里出现——不做点进去是空页面的菜单。
var supportedKinds = map[string]string{
	"log_keyword":         "日志关键词",
	"log_absent":          "日志缺失（心跳）",
	"log_spike":           "日志量突变",
	"log_field_threshold": "字段阈值 / 分位数",
}

func (s *Server) registerRules(g *gin.RouterGroup) {
	g.GET("/rules/kinds", s.listKinds)
	g.GET("/rules/templates", s.listTemplates)
	g.POST("/rules/templates/preview", s.previewTemplate)
	g.GET("/rules/:id", s.getRule)
	g.POST("/rules", s.createRule)
	g.PUT("/rules/:id", s.updateRule)
	g.DELETE("/rules/:id", s.deleteRule)
	g.POST("/rules/:id/toggle", s.toggleRule)
	g.POST("/rules/dryrun", s.dryRun)
	g.GET("/rules/:id/runs", s.listRuleRuns)
	g.GET("/rules/:id/quality", s.ruleQuality)
}

func (s *Server) listKinds(c *gin.Context) {
	out := []gin.H{}
	for k, label := range supportedKinds {
		out = append(out, gin.H{"kind": k, "label": label})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type ruleReq struct {
	Name           string            `json:"name"`
	Kind           string            `json:"kind"`
	DSID           int64             `json:"datasource_id"`
	Spec           json.RawMessage   `json:"spec"`
	IntervalSec    int               `json:"interval_sec"`
	LookbackSec    int               `json:"lookback_sec"`
	Threshold      int               `json:"threshold"`
	ForPeriods     int               `json:"for_periods"`
	GroupBy        []string          `json:"group_by"`
	Severity       string            `json:"severity"`
	Labels         map[string]string `json:"labels"`
	ResolveAfter   int               `json:"resolve_after"`
	NotifyResolved bool              `json:"notify_resolved"`
	RouteID        int64             `json:"route_id"`
	MaxEvents      int               `json:"max_events"`
	// 来源模板与当时填的业务参数。手写规则时为空。
	// 存它是为了以后能用原参数回填表单，而不是让人去反推生成好的 LogQL
	Template       string          `json:"template"`
	TemplateParams json.RawMessage `json:"params"`
	// 指标导出。对应旧系统的 prometheus_config。
	// MetricsLabels 的键名必须是合法的 Prometheus 标签名，保存时校验 ——
	// 一条非法标签会让整份 /metrics 解析失败，连累所有规则的指标。
	MetricsEnabled bool              `json:"metrics_enabled"`
	MetricsLabels  map[string]string `json:"metrics_labels"`
}

func (s *Server) createRule(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req ruleReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	// 场景必填项在保存时校验，不留到运行期。
	// 运行期才发现的后果是：规则在列表里显示"运行中"，实际每周期都失败，
	// 而人以为它在守着——本产品要根治的正是这类"看起来正常"。
	if msg := validateAbsent(req.Kind, req.GroupBy, req.Spec); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_spec", "detail": msg})
		return
	}
	if msg := validateSpec(req.Kind, req.Spec); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_spec", "detail": msg})
		return
	}
	if _, ok := supportedKinds[req.Kind]; !ok {
		// 二期类型（指标阈值、SLO 等）会走到这里。明说"尚未实现"，
		// 不要静默存下来——存下来的规则永远不会被执行，而列表上看着是正常的。
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_kind",
			"detail": "一期只支持日志侧四类检测；指标类由夜莺负责，见路线图"})
		return
	}
	groupBy, _ := json.Marshal(defaultSlice(req.GroupBy))
	labels, _ := json.Marshal(req.Labels)
	spec := req.Spec
	if len(spec) == 0 {
		spec = json.RawMessage("{}")
	}
	var routeID any
	if req.RouteID > 0 {
		routeID = req.RouteID
	}
	if err := ValidateMetricLabels(req.MetricsLabels); err != nil {
		// 400 而不是静默丢弃：用户填了标签却在指标里找不到，
		// 会以为是抓取端配错了，然后往错的方向查很久
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_metrics_labels", "detail": err.Error()})
		return
	}
	metricsLabels := jsonOrNull(req.MetricsLabels)
	user := middleware.CurrentUser(c)
	tplParams := req.TemplateParams
	if len(tplParams) == 0 {
		tplParams = nil // 手写规则不写空对象，NULL 更能表达"没有模板参数"
	}
	res, err := sc.Insert(`INSERT INTO rules (tenant_id, name, kind, datasource_id, spec,
			interval_sec, lookback_sec, threshold, for_periods, group_by, severity, labels,
			resolve_after, notify_resolved, route_id, max_events, created_by,
			template, template_params, metrics_enabled, metrics_labels)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Kind, req.DSID, []byte(spec),
		defaultInt(req.IntervalSec, 60), defaultInt(req.LookbackSec, 300),
		defaultInt(req.Threshold, 1), defaultInt(req.ForPeriods, 1),
		groupBy, defaultStr(req.Severity, "warning"), labels,
		defaultInt(req.ResolveAfter, 3), boolToInt(req.NotifyResolved), routeID,
		defaultInt(req.MaxEvents, 20), user.Username,
		req.Template, tplParams, boolToInt(req.MetricsEnabled), metricsLabels)
	if err != nil {
		abortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()
	s.audit(c, sc, "rule.create", "rule", id,
		gin.H{"name": req.Name, "kind": req.Kind, "template": req.Template})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (s *Server) getRule(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var (
		name, kind, severity, lastError string
		dsID                            int64
		spec, groupBy, labels           []byte
		interval, lookback, threshold   int
		forPeriods, resolveAfter        int
		notifyResolved, enabled         int
		maxEvents, failures             int
		routeID                         sql.NullInt64
		lastRun                         sql.NullTime
		template                        string
		tplParams                       []byte
		metricsEnabled                  int
		metricsLabels                   []byte
	)
	err = sc.QueryRow(`SELECT name, kind, datasource_id, spec, interval_sec, lookback_sec,
			threshold, for_periods, group_by, severity, labels, resolve_after, notify_resolved,
			route_id, max_events, enabled, last_run_at, last_error, consecutive_failures,
			template, template_params, metrics_enabled, metrics_labels
		FROM rules WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id).
		Scan(&name, &kind, &dsID, &spec, &interval, &lookback, &threshold, &forPeriods,
			&groupBy, &severity, &labels, &resolveAfter, &notifyResolved, &routeID,
			&maxEvents, &enabled, &lastRun, &lastError, &failures, &template, &tplParams,
			&metricsEnabled, &metricsLabels)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if err != nil {
		abortQuery(c, err)
		return
	}
	out := gin.H{
		"id": id, "name": name, "kind": kind, "datasource_id": dsID,
		"spec": json.RawMessage(spec), "interval_sec": interval, "lookback_sec": lookback,
		"threshold": threshold, "for_periods": forPeriods, "group_by": rawOrNull(groupBy),
		"severity": severity, "labels": rawOrNull(labels), "resolve_after": resolveAfter,
		"notify_resolved": notifyResolved == 1, "max_events": maxEvents, "enabled": enabled == 1,
		"metrics_enabled": metricsEnabled == 1, "metrics_labels": rawOrNull(metricsLabels),
		"last_error": lastError, "consecutive_failures": failures,
		// 编辑时用它回填模板表单。空串表示手写规则，前端退回手写表单 ——
		// 让人对着生成好的 LogQL 反推当初填了什么，正是模板要消除的事
		"template": template, "params": rawOrNull(tplParams),
	}
	if routeID.Valid {
		out["route_id"] = routeID.Int64
	}
	if lastRun.Valid {
		out["last_run_at"] = lastRun.Time
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) updateRule(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req ruleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	groupBy, _ := json.Marshal(defaultSlice(req.GroupBy))
	labels, _ := json.Marshal(req.Labels)
	var routeID any
	if req.RouteID > 0 {
		routeID = req.RouteID
	}
	tplParams := req.TemplateParams
	if len(tplParams) == 0 {
		tplParams = nil
	}
	if _, err := sc.Exec(`UPDATE rules SET name=?, spec=?, interval_sec=?, lookback_sec=?,
			threshold=?, for_periods=?, group_by=?, severity=?, labels=?, resolve_after=?,
			notify_resolved=?, route_id=?, max_events=?, template=?, template_params=?
		WHERE tenant_id = ? AND id = ?`,
		req.Name, []byte(req.Spec), defaultInt(req.IntervalSec, 60), defaultInt(req.LookbackSec, 300),
		defaultInt(req.Threshold, 1), defaultInt(req.ForPeriods, 1), groupBy,
		defaultStr(req.Severity, "warning"), labels, defaultInt(req.ResolveAfter, 3),
		boolToInt(req.NotifyResolved), routeID, defaultInt(req.MaxEvents, 20),
		req.Template, tplParams, id); err != nil {
		abortQuery(c, err)
		return
	}
	// 改了分组维度后，老的 group_key 会永远留在状态表里并被当成"上一轮见过的分组"，
	// 每周期都空跑一遍。规则一改就清掉，让状态从头长。
	if _, err := sc.Exec(`DELETE FROM rule_states WHERE tenant_id = ? AND rule_id = ?`, id); err != nil {
		abortLog(c, err)
	}
	s.audit(c, sc, "rule.update", "rule", id, gin.H{"name": req.Name})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) deleteRule(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := sc.Exec(`UPDATE rules SET deleted_at = NOW(3), enabled = 0
		WHERE tenant_id = ? AND id = ?`, id); err != nil {
		abortQuery(c, err)
		return
	}
	if _, err := sc.Exec(`DELETE FROM rule_states WHERE tenant_id = ? AND rule_id = ?`, id); err != nil {
		abortLog(c, err)
	}
	s.audit(c, sc, "rule.delete", "rule", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) toggleRule(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	_ = c.ShouldBindJSON(&req)
	if _, err := sc.Exec(`UPDATE rules SET enabled = ? WHERE tenant_id = ? AND id = ?`,
		boolToInt(req.Enabled), id); err != nil {
		abortQuery(c, err)
		return
	}
	// 停用时清状态：重新启用后从头计数，不会拿着几天前的 streak 立刻触发。
	if !req.Enabled {
		if _, err := sc.Exec(`DELETE FROM rule_states WHERE tenant_id = ? AND rule_id = ?`, id); err != nil {
			abortLog(c, err)
		}
	}
	s.audit(c, sc, "rule.toggle", "rule", id, gin.H{"enabled": req.Enabled})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// dryRun 拿真实数据跑一遍判定，但不产生事件、不发通知。
func (s *Server) dryRun(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		DSID        int64    `json:"datasource_id"`
		Query       string   `json:"query"`
		LookbackSec int      `json:"lookback_sec"`
		Threshold   int      `json:"threshold"`
		Limit       int      `json:"limit"`
		GroupBy     []string `json:"group_by"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DSID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	res, err := s.eng.DryRun(c.Request.Context(), sc, req.DSID, req.Query,
		defaultInt(req.LookbackSec, 300), defaultInt(req.Threshold, 1),
		defaultInt(req.Limit, 200), req.GroupBy)
	if err != nil {
		// 查询写错时把数据源的原始报错带回去：LogQL/DSL 的错误提示很具体，
		// 吞掉它只留"试运行失败"会让人无从改起。
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": res})
}

func (s *Server) listRuleRuns(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	rows, err := sc.Query(`SELECT started_at, duration_ms, outcome, hits, group_count, events_made, error
		FROM rule_runs WHERE tenant_id = ? AND rule_id = ? ORDER BY id DESC LIMIT 50`, id)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var at time.Time
		var ms, hits, groups, events int
		var outcome, errMsg string
		if err := rows.Scan(&at, &ms, &outcome, &hits, &groups, &events, &errMsg); err != nil {
			abortQuery(c, err)
			return
		}
		out = append(out, gin.H{"at": at, "duration_ms": ms, "outcome": outcome,
			"hits": hits, "groups": groups, "events": events, "error": errMsg})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// ruleQuality 规则质量分：触发次数、误报率、平均恢复时长。
//
// 「7 天触发 0 次」可能是健康，也可能是查询一直在失败——
// 所以这里必须把 error_runs 一并给出，界面才能区分安静与失明。
func (s *Server) ruleQuality(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	var totalRuns, errorRuns int
	_ = sc.QueryRow(`SELECT COUNT(*), SUM(outcome = 'error') FROM rule_runs
		WHERE tenant_id = ? AND rule_id = ? AND started_at > DATE_SUB(NOW(3), INTERVAL 7 DAY)`, id).
		Scan(&totalRuns, &errorRuns)

	var incidents, falsePositives int
	var avgMTTR sql.NullFloat64
	_ = sc.QueryRow(`SELECT COUNT(*), SUM(false_positive),
			AVG(TIMESTAMPDIFF(SECOND, first_at, resolved_at))
		FROM incidents WHERE tenant_id = ? AND rule_id = ?
		  AND first_at > DATE_SUB(NOW(3), INTERVAL 7 DAY)`, id).
		Scan(&incidents, &falsePositives, &avgMTTR)

	// 质量分：噪音与误报扣分，执行失败重扣。
	// 公式简单是刻意的——能解释清楚的分数才有人信，
	// 复杂加权算出来的分只会被当成玄学忽略掉。
	score := 100
	if incidents > 0 {
		score -= min(40, incidents*2)
		score -= int(float64(falsePositives) / float64(incidents) * 40)
	}
	if totalRuns > 0 && errorRuns > 0 {
		score -= int(float64(errorRuns) / float64(totalRuns) * 50)
	}
	if score < 0 {
		score = 0
	}
	grade := "A"
	switch {
	case score < 40:
		grade = "F"
	case score < 60:
		grade = "C"
	case score < 80:
		grade = "B"
	}
	mttr := 0
	if avgMTTR.Valid {
		mttr = int(avgMTTR.Float64)
	}
	c.JSON(http.StatusOK, gin.H{
		"runs_7d": totalRuns, "error_runs_7d": errorRuns,
		"incidents_7d": incidents, "false_positives_7d": falsePositives,
		"avg_mttr_sec": mttr, "score": score, "grade": grade,
	})
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func defaultSlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// validateSpec 检查各场景的必填配置，返回空串表示通过。
func validateSpec(kind string, raw json.RawMessage) string {
	var spec struct {
		Query          string  `json:"query"`
		Field          string  `json:"field"`
		FieldThreshold float64 `json:"field_threshold"`
		SpikeRatio     float64 `json:"spike_ratio"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &spec); err != nil {
			return "spec 不是合法 JSON：" + err.Error()
		}
	}
	if spec.Query == "" {
		return "缺少查询语句（spec.query）"
	}
	switch kind {
	case "log_field_threshold":
		if spec.Field == "" {
			return "字段阈值规则必须指定要取哪个字段的数值（spec.field）"
		}
		if spec.FieldThreshold <= 0 {
			return "字段阈值规则必须指定阈值（spec.field_threshold）"
		}
	case "log_spike":
		if spec.SpikeRatio <= 0 {
			return "日志量突变规则必须指定倍数阈值（spec.spike_ratio），例如 3 表示涨到基线的 3 倍"
		}
	}
	return ""
}

// validateAbsent 拦住「配了分组却没登记期望分组」的缺失检测规则。
//
// 分组键是从数据里长出来的：某个实例从一开始就不产日志时，它根本不会
// 出现在查询结果里，规则也就永远不会为它告警——而那正是最该告警的情况。
// 期望分组是唯一能让"从没见过的实例"被发现的办法，所以这里强制要求。
func validateAbsent(kind string, groupBy []string, raw json.RawMessage) string {
	if kind != "log_absent" || len(groupBy) == 0 {
		return ""
	}
	var spec struct {
		ExpectedGroups []string `json:"expected_groups"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &spec)
	}
	if len(spec.ExpectedGroups) == 0 {
		return "按分组做缺失检测时必须登记期望分组（spec.expected_groups）：" +
			"没登记的话，一个从头到尾都不产日志的实例不会出现在查询结果里，规则永远不会为它告警"
	}
	return ""
}
