package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/engine"
	"ops-alert-backend/internal/api/middleware"
	"ops-alert-backend/internal/dataview"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
)

func (s *Server) registerBacktest(g *gin.RouterGroup) {
	g.POST("/backtests", s.createBacktest)
	g.GET("/backtests", s.listBacktests)
	g.GET("/backtests/:id", s.getBacktest)
	g.POST("/import/preflight", s.importPreflight)
	g.POST("/import/apply", s.importApply)
	g.GET("/noise/top", s.noiseTop)
}

// createBacktest 发起一次回放。
//
// 回放在后台跑（可能几十秒到几分钟），接口立刻返回 ID，前端轮询结果。
// 同步等待会让浏览器超时，而超时的表现是「点了没反应」——
// 用户会再点几次，于是同一份回放跑了五遍。
func (s *Server) createBacktest(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		RuleID int64                `json:"rule_id"`
		Draft  engine.BacktestDraft `json:"draft"`
		Days   int                  `json:"days"`
		// Compare 为 true 时同时回放"现网配置"作为基线，用于对比。
		Compare   bool                 `json:"compare"`
		BaseDraft engine.BacktestDraft `json:"base_draft"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Draft.DatasourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	days := defaultInt(req.Days, 7)
	if days > 30 {
		// 上限是保护数据源：30 天 × 5 分钟步长已经是 8640 次查询。
		days = 30
	}
	to := time.Now()
	from := to.AddDate(0, 0, -days)

	draftBlob, _ := json.Marshal(req.Draft)
	var ruleID any
	if req.RuleID > 0 {
		ruleID = req.RuleID
	}
	user := middleware.CurrentUser(c)
	res, err := sc.Insert(`INSERT INTO backtests (tenant_id, rule_id, draft, range_from, range_to, created_by)
		VALUES (?, ?, ?, ?, ?, ?)`, ruleID, draftBlob, from, to, user.Username)
	if err != nil {
		abortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()

	tenant, _ := store.TenantFrom(c.Request.Context())
	go s.runBacktestAsync(tenant, id, req.Draft, req.BaseDraft, req.Compare, from, to)

	c.JSON(http.StatusOK, gin.H{"id": id, "status": "running", "days": days})
}

// runBacktestAsync 在后台执行回放。
//
// 用独立的 context 而不是请求的：请求早就返回了，
// 跟着请求 context 走会让回放在响应写出的瞬间被取消，
// 现象是「回放永远停在 running」。
func (s *Server) runBacktestAsync(tenant store.TenantID, id int64,
	draft, base engine.BacktestDraft, compare bool, from, to time.Time,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	jobCtx := store.ForJob(ctx, tenant, "backtest")
	sc, err := s.st.Tenant(jobCtx)
	if err != nil {
		logx.J("api", "backtest_no_tenant", map[string]any{"id": id, "error": err.Error()})
		return
	}
	result, err := s.eng.RunBacktest(jobCtx, sc, draft, from, to)
	if err != nil {
		s.eng.SaveBacktest(sc, id, nil, err)
		return
	}
	payload := map[string]any{"draft": result}
	if compare && base.DatasourceID > 0 {
		baseRes, baseErr := s.eng.RunBacktest(jobCtx, sc, base, from, to)
		if baseErr == nil {
			payload["base"] = baseRes
			payload["compare"] = engine.CompareBacktests(baseRes, result)
		} else {
			payload["base_error"] = baseErr.Error()
		}
	}
	blob, _ := json.Marshal(payload)
	if _, err := sc.Exec(`UPDATE backtests SET status='done', result=?, finished_at=NOW(3)
		WHERE tenant_id = ? AND id = ?`, blob, id); err != nil {
		logx.J("api", "backtest_save_error", map[string]any{"id": id, "error": err.Error()})
	}
}

func (s *Server) listBacktests(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT id, rule_id, status, range_from, range_to, created_by, created_at, finished_at
		FROM backtests WHERE tenant_id = ? ORDER BY id DESC LIMIT 50`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var ruleID sql.NullInt64
		var status, createdBy string
		var from, to, createdAt time.Time
		var finished sql.NullTime
		if err := rows.Scan(&id, &ruleID, &status, &from, &to, &createdBy, &createdAt, &finished); err != nil {
			abortQuery(c, err)
			return
		}
		item := gin.H{"id": id, "status": status, "range_from": from, "range_to": to,
			"created_by": createdBy, "created_at": createdAt}
		if ruleID.Valid {
			item["rule_id"] = ruleID.Int64
		}
		if finished.Valid {
			item["finished_at"] = finished.Time
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) getBacktest(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var status, errMsg string
	var result []byte
	if err := sc.QueryRow(`SELECT status, result, error FROM backtests
		WHERE tenant_id = ? AND id = ?`, id).Scan(&status, &result, &errMsg); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status, "result": rawOrNull(result), "error": errMsg})
}

// importPreflight 旧规则预检：只翻译不写库。
func (s *Server) importPreflight(c *gin.Context) {
	var req struct {
		Rules []engine.LegacyRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request",
			"detail": "需要旧系统 alert_rules 的导出 JSON 数组"})
		return
	}
	c.JSON(http.StatusOK, engine.Preflight(req.Rules))
}

// importApply 执行导入。导入的规则一律先停用，需要人工逐条确认后再启用。
func (s *Server) importApply(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	var req struct {
		DatasourceID   int64               `json:"datasource_id"`
		Rules          []engine.LegacyRule `json:"rules"`
		IncludeConfirm bool                `json:"include_confirm"`
		// 界面上逐条勾选后传回来的旧规则 ID。非空时只导这几条。
		LegacyIDs []int `json:"legacy_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DatasourceID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	n, err := engine.Import(sc, req.DatasourceID, req.Rules, req.IncludeConfirm, req.LegacyIDs)
	if err != nil {
		abortQuery(c, err)
		return
	}
	s.audit(c, sc, "rule.import", "rule", 0, gin.H{"imported": n, "total": len(req.Rules)})
	c.JSON(http.StatusOK, gin.H{"imported": n, "total": len(req.Rules),
		"note": "导入的规则默认停用，逐条确认查询语句与路由后再启用"})
}

// noiseTop 噪音榜。实现与 MCP 工具共用（见 mcp_data.go）：
// 两份实现迟早漂移，而漂移的表现是"界面说正常、AI 说有问题"。
func (s *Server) noiseTop(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	data, err := dataview.NoiseTop(sc)
	if err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}
