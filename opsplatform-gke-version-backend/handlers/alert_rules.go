package handlers

import (
	"database/sql"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/models"
)

type AlertRulesHandler struct{ DB *sql.DB }

func NewAlertRulesHandler(db *sql.DB) *AlertRulesHandler { return &AlertRulesHandler{DB: db} }

func (h *AlertRulesHandler) Register(r *gin.RouterGroup) {
	r.GET("/alert_rules", h.List)
	r.POST("/alert_rules", h.Create)
	r.PUT("/alert_rules/:id", h.Update)
	r.DELETE("/alert_rules/:id", h.Delete)
	r.GET("/alert_history", h.History)
}

func (h *AlertRulesHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, target, versions_behind_threshold, eol_days_threshold,
		COALESCE(cluster_ids, JSON_ARRAY()), webhook_id, COALESCE(mention_user_ids, JSON_ARRAY()),
		interval_minutes, enabled, created_at, updated_at FROM alert_rules ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []models.AlertRule{}
	for rows.Next() {
		var (
			r              models.AlertRule
			eolDays        sql.NullInt64
			clusterIDs     []byte
			mentionUserIDs []byte
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Target, &r.VersionsBehindThreshold, &eolDays,
			&clusterIDs, &r.WebhookID, &mentionUserIDs,
			&r.IntervalMinutes, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if eolDays.Valid {
			v := int(eolDays.Int64)
			r.EOLDaysThreshold = &v
		}
		_ = json.Unmarshal(clusterIDs, &r.ClusterIDs)
		_ = json.Unmarshal(mentionUserIDs, &r.MentionUserIDs)
		out = append(out, r)
	}
	c.JSON(200, out)
}

type ruleReq struct {
	Name                    string `json:"name" binding:"required"`
	Target                  string `json:"target" binding:"required"`
	VersionsBehindThreshold int    `json:"versions_behind_threshold" binding:"required"`
	EOLDaysThreshold        *int   `json:"eol_days_threshold"`
	ClusterIDs              []int  `json:"cluster_ids"`
	WebhookID               int    `json:"webhook_id" binding:"required"`
	MentionUserIDs          []int  `json:"mention_user_ids"`
	IntervalMinutes         int    `json:"interval_minutes"`
	Enabled                 int    `json:"enabled"`
}

func (h *AlertRulesHandler) Create(c *gin.Context) {
	var req ruleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.IntervalMinutes <= 0 {
		req.IntervalMinutes = 60
	}
	ci, _ := json.Marshal(orEmpty(req.ClusterIDs))
	mu, _ := json.Marshal(orEmpty(req.MentionUserIDs))
	res, err := h.DB.Exec(`INSERT INTO alert_rules
		(name, target, versions_behind_threshold, eol_days_threshold, cluster_ids, webhook_id, mention_user_ids, interval_minutes, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.Target, req.VersionsBehindThreshold, req.EOLDaysThreshold, ci, req.WebhookID, mu, req.IntervalMinutes, req.Enabled)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *AlertRulesHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req ruleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.IntervalMinutes <= 0 {
		req.IntervalMinutes = 60
	}
	ci, _ := json.Marshal(orEmpty(req.ClusterIDs))
	mu, _ := json.Marshal(orEmpty(req.MentionUserIDs))
	_, err := h.DB.Exec(`UPDATE alert_rules SET name=?, target=?, versions_behind_threshold=?, eol_days_threshold=?,
		cluster_ids=?, webhook_id=?, mention_user_ids=?, interval_minutes=?, enabled=? WHERE id=?`,
		req.Name, req.Target, req.VersionsBehindThreshold, req.EOLDaysThreshold, ci, req.WebhookID, mu, req.IntervalMinutes, req.Enabled, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *AlertRulesHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if _, err := h.DB.Exec(`DELETE FROM alert_rules WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *AlertRulesHandler) History(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, rule_id, cluster_id, COALESCE(nodepool_name, ''), COALESCE(versions_behind, 0),
		trigger_time, status, COALESCE(lark_response, '') FROM alert_history ORDER BY trigger_time DESC LIMIT 200`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []models.AlertHistory{}
	for rows.Next() {
		var h models.AlertHistory
		if err := rows.Scan(&h.ID, &h.RuleID, &h.ClusterID, &h.NodepoolName, &h.VersionsBehind,
			&h.TriggerTime, &h.Status, &h.LarkResponse); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		out = append(out, h)
	}
	c.JSON(200, out)
}

func orEmpty(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}
