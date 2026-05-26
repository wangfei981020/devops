package handlers

import (
	"database/sql"
	"strconv"

	"github.com/gin-gonic/gin"
)

type SettingsHandler struct {
	DB *sql.DB
}

func NewSettingsHandler(db *sql.DB) *SettingsHandler {
	return &SettingsHandler{DB: db}
}

func (h *SettingsHandler) Register(r *gin.RouterGroup) {
	r.GET("/settings", h.List)
	r.PUT("/settings", h.Update)
}

func (h *SettingsHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT k, v FROM settings`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		_ = rows.Scan(&k, &v)
		out[k] = v
	}
	c.JSON(200, out)
}

type updateReq struct {
	K string `json:"k" binding:"required"`
	V string `json:"v" binding:"required"`
}

func (h *SettingsHandler) Update(c *gin.Context) {
	var req updateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.K == "scrape_interval_minutes" {
		n, err := strconv.Atoi(req.V)
		if err != nil || n < 5 || n > 1440 {
			c.JSON(400, gin.H{"error": "scrape_interval_minutes must be int in [5,1440]"})
			return
		}
	}
	_, err := h.DB.Exec(`INSERT INTO settings (k, v) VALUES (?, ?) ON DUPLICATE KEY UPDATE v=VALUES(v)`, req.K, req.V)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
