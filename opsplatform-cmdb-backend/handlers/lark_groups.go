package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/notify"
)

// LarkGroupHandler Lark/飞书群（webhook）管理；定时任务按群发通知。
type LarkGroupHandler struct{ DB *sql.DB }

func NewLarkGroupHandler(db *sql.DB) *LarkGroupHandler { return &LarkGroupHandler{DB: db} }

func (h *LarkGroupHandler) Register(r *gin.RouterGroup) {
	r.GET("/lark-groups", h.List)
	r.POST("/lark-groups", h.Create)
	r.PUT("/lark-groups/:id", h.Update)
	r.DELETE("/lark-groups/:id", h.Delete)
	r.POST("/lark-groups/:id/test", h.Test)
}

func (h *LarkGroupHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, webhook FROM lark_groups ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type g struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Webhook string `json:"webhook"`
	}
	// ⚠️ webhook URL 就是凭据：不需要任何额外鉴权，谁拿到谁就能往这个群发消息。
	//	原来列表接口对所有人明发，只读账号也能拿到完整 URL 去投伪造告警
	//	（CMDB-034）。默认掩码，只有能管通知的人拿真值。
	//	在前端打码是假的——值已经在响应里了。
	full := HasPerm(c, "cmdb:manage_notify")
	out := []g{}
	for rows.Next() {
		var x g
		if rows.Scan(&x.ID, &x.Name, &x.Webhook) != nil {
			continue
		}
		if !full {
			x.Webhook = maskWebhookURL(x.Webhook)
		}
		out = append(out, x)
	}
	c.JSON(http.StatusOK, out)
}

func (h *LarkGroupHandler) Create(c *gin.Context) {
	var in struct {
		Name    string `json:"name"`
		Webhook string `json:"webhook"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.Webhook == "" {
		c.JSON(400, gin.H{"error": "群名和 webhook 必填"})
		return
	}
	if _, err := h.DB.Exec(`INSERT INTO lark_groups (name, webhook) VALUES (?, ?)`, in.Name, in.Webhook); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, in.Name)
	c.JSON(201, gin.H{"ok": true})
}

func (h *LarkGroupHandler) Update(c *gin.Context) {
	var in struct {
		Name    string `json:"name"`
		Webhook string `json:"webhook"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 同 settings：界面上显示的是掩码，原样提交回来不能当成"用户要改成这个"。
	// 留空也按"不改"处理（和其他凭据类字段的编辑约定一致）。
	if in.Webhook == "" || isMasked(in.Webhook) {
		if _, err := h.DB.Exec(`UPDATE lark_groups SET name=? WHERE id=?`, in.Name, c.Param("id")); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
		return
	}
	if _, err := h.DB.Exec(`UPDATE lark_groups SET name=?, webhook=? WHERE id=?`, in.Name, in.Webhook, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *LarkGroupHandler) Delete(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM lark_groups WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// Test 往该群发一条测试消息（不 @ 人）。
func (h *LarkGroupHandler) Test(c *gin.Context) {
	var webhook string
	if err := h.DB.QueryRow(`SELECT webhook FROM lark_groups WHERE id=?`, c.Param("id")).Scan(&webhook); err != nil || webhook == "" {
		c.JSON(400, gin.H{"error": "群不存在或 webhook 为空"})
		return
	}
	if err := notify.SendFeishu(webhook, "【CMDB 测试】该群通知通道正常 ✅"); err != nil {
		c.JSON(500, gin.H{"error": "发送失败：" + err.Error()})
		return
	}
	SetAuditTarget(c, c.Param("id"))
	c.JSON(200, gin.H{"ok": true, "msg": "已发送，去群里看"})
}
