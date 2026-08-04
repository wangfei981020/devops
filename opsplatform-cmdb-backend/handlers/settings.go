package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

type SettingsHandler struct {
	DB *sql.DB
}

func NewSettingsHandler(db *sql.DB) *SettingsHandler { return &SettingsHandler{DB: db} }

func (h *SettingsHandler) Register(r *gin.RouterGroup) {
	r.GET("/settings", h.Get)
	r.PUT("/settings", h.Update)
}

func (h *SettingsHandler) Get(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT k, COALESCE(v,'') FROM settings`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			out[k] = v
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *SettingsHandler) Update(c *gin.Context) {
	var in map[string]string
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	for k, v := range in {
		if _, err := h.DB.Exec(`INSERT INTO settings (k, v) VALUES (?, ?) ON DUPLICATE KEY UPDATE v=VALUES(v)`, k, v); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	SetAuditTarget(c, "")
	c.JSON(200, gin.H{"ok": true})
}
