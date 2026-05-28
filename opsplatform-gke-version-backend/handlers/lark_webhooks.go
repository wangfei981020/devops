package handlers

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/models"
)

type LarkWebhooksHandler struct{ DB *sql.DB }

func NewLarkWebhooksHandler(db *sql.DB) *LarkWebhooksHandler { return &LarkWebhooksHandler{DB: db} }

func (h *LarkWebhooksHandler) Register(r *gin.RouterGroup) {
	r.GET("/lark_webhooks", h.List)
	r.POST("/lark_webhooks", h.Create)
	r.PUT("/lark_webhooks/:id", h.Update)
	r.DELETE("/lark_webhooks/:id", h.Delete)
}

func (h *LarkWebhooksHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, url, COALESCE(remark, ''), created_at, updated_at FROM lark_webhooks ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []models.LarkWebhook{}
	for rows.Next() {
		var w models.LarkWebhook
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Remark, &w.CreatedAt, &w.UpdatedAt); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		out = append(out, w)
	}
	c.JSON(200, out)
}

type webhookReq struct {
	Name   string `json:"name" binding:"required"`
	URL    string `json:"url" binding:"required"`
	Remark string `json:"remark"`
}

func (h *LarkWebhooksHandler) Create(c *gin.Context) {
	var req webhookReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO lark_webhooks (name, url, remark) VALUES (?, ?, ?)`, req.Name, req.URL, req.Remark)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *LarkWebhooksHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req webhookReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE lark_webhooks SET name=?, url=?, remark=? WHERE id=?`, req.Name, req.URL, req.Remark, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *LarkWebhooksHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if _, err := h.DB.Exec(`DELETE FROM lark_webhooks WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
