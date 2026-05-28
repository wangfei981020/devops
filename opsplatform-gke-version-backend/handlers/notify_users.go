package handlers

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"opsplatform-gke-version-backend/models"
)

type NotifyUsersHandler struct{ DB *sql.DB }

func NewNotifyUsersHandler(db *sql.DB) *NotifyUsersHandler { return &NotifyUsersHandler{DB: db} }

func (h *NotifyUsersHandler) Register(r *gin.RouterGroup) {
	r.GET("/notify_users", h.List)
	r.POST("/notify_users", h.Create)
	r.PUT("/notify_users/:id", h.Update)
	r.DELETE("/notify_users/:id", h.Delete)
}

func (h *NotifyUsersHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, lark_id, COALESCE(remark, ''), created_at, updated_at FROM notify_users ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []models.NotifyUser{}
	for rows.Next() {
		var u models.NotifyUser
		if err := rows.Scan(&u.ID, &u.Name, &u.LarkID, &u.Remark, &u.CreatedAt, &u.UpdatedAt); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		out = append(out, u)
	}
	c.JSON(200, out)
}

type notifyUserReq struct {
	Name   string `json:"name" binding:"required"`
	LarkID string `json:"lark_id" binding:"required"`
	Remark string `json:"remark"`
}

func (h *NotifyUsersHandler) Create(c *gin.Context) {
	var req notifyUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO notify_users (name, lark_id, remark) VALUES (?, ?, ?)`, req.Name, req.LarkID, req.Remark)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	c.JSON(201, gin.H{"id": id})
}

func (h *NotifyUsersHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req notifyUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	_, err := h.DB.Exec(`UPDATE notify_users SET name=?, lark_id=?, remark=? WHERE id=?`, req.Name, req.LarkID, req.Remark, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *NotifyUsersHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if _, err := h.DB.Exec(`DELETE FROM notify_users WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
