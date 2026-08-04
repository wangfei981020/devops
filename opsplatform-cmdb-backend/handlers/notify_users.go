package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/notify"
)

// NotifyHandler 通知人（飞书 open_id @）管理 + 测试发送。
type NotifyHandler struct{ DB *sql.DB }

func NewNotifyHandler(db *sql.DB) *NotifyHandler { return &NotifyHandler{DB: db} }

func (h *NotifyHandler) Register(r *gin.RouterGroup) {
	r.GET("/notify-users", h.List)
	r.POST("/notify-users", h.Create)
	r.DELETE("/notify-users/:id", h.Delete)
	r.POST("/notify/test", h.Test)
}

func (h *NotifyHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, open_id, enabled FROM notify_users ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type userOut struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		OpenID  string `json:"open_id"`
		Enabled int    `json:"enabled"`
	}
	out := []userOut{}
	for rows.Next() {
		var u userOut
		if rows.Scan(&u.ID, &u.Name, &u.OpenID, &u.Enabled) == nil {
			out = append(out, u)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *NotifyHandler) Create(c *gin.Context) {
	var in struct {
		Name   string `json:"name"`
		OpenID string `json:"open_id"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.OpenID == "" {
		c.JSON(400, gin.H{"error": "姓名和 open_id 必填"})
		return
	}
	if _, err := h.DB.Exec(`INSERT INTO notify_users (name, open_id) VALUES (?, ?)`, in.Name, in.OpenID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, in.Name)
	c.JSON(201, gin.H{"ok": true})
}

func (h *NotifyHandler) Delete(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM notify_users WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// Test 用当前 webhook + 通知人 @ 发一条测试消息。
func (h *NotifyHandler) Test(c *gin.Context) {
	webhook := getSetting(h.DB, "feishu_webhook")
	if webhook == "" {
		c.JSON(400, gin.H{"error": "未配置飞书 Webhook"})
		return
	}
	if err := notify.SendFeishu(webhook, "【CMDB 测试】通知通道正常 ✅"+atMentions(h.DB)); err != nil {
		c.JSON(500, gin.H{"error": "发送失败：" + err.Error()})
		return
	}
	SetAuditTarget(c, "")
	c.JSON(200, gin.H{"ok": true, "msg": "已发送，去飞书群看"})
}
