package notify

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/notify"
)

// UserHandler 通知人（飞书 open_id @）管理 + 测试发送。
//
// 迁移自 handlers/notify_users.go，按 registrar 样板做租户隔离。
type UserHandler struct {
	Store *store.Store

	// Setting 读全局设置（飞书 webhook）。设置本身还在旧包里，
	// 迁移后这个函数参数可以去掉。
	Setting func(key string) string
	// Mentions 拼接 @ 人的文本，同样是迁移期的桥接。
	Mentions func() string
}

func NewUsers(st *store.Store, setting func(string) string, mentions func() string) *UserHandler {
	return &UserHandler{Store: st, Setting: setting, Mentions: mentions}
}

func (h *UserHandler) Register(r *gin.RouterGroup) {
	r.GET("/notify-users", h.List)
	r.POST("/notify-users", h.Create)
	r.DELETE("/notify-users/:id", h.Delete)
	r.POST("/notify/test", h.Test)
}

type notifyUser struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	OpenID  string `json:"open_id"`
	Enabled int    `json:"enabled"`
}

func (h *UserHandler) List(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, name, open_id, enabled FROM notify_users WHERE tenant_id = ? ORDER BY id`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	out := []notifyUser{}
	for rows.Next() {
		var u notifyUser
		if rows.Scan(&u.ID, &u.Name, &u.OpenID, &u.Enabled) == nil {
			out = append(out, u)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *UserHandler) Create(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Name   string `json:"name"`
		OpenID string `json:"open_id"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.OpenID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "姓名和 open_id 必填"})
		return
	}
	if _, err := sc.Insert(`INSERT INTO notify_users (tenant_id, name, open_id) VALUES (?, ?, ?)`,
		in.Name, in.OpenID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"ok": true})
}

func (h *UserHandler) Delete(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 不合法"})
		return
	}
	res, err := sc.Exec(`DELETE FROM notify_users WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "通知人不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Test 用当前 webhook + 通知人 @ 发一条测试消息。
func (h *UserHandler) Test(c *gin.Context) {
	webhook := h.Setting("feishu_webhook")
	if webhook == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未配置飞书 Webhook"})
		return
	}
	if err := notify.SendFeishu(webhook, "【测试】通知通道正常 ✅"+h.Mentions()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "发送失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "msg": "已发送，去飞书群看"})
}
