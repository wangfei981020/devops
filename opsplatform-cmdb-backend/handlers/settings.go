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

// sensitiveSettingKeys 这些设置项的值本身就是凭据，不能随 /api/settings 明发。
//
//	新增设置项时如果它是"拿到就能干事"的（webhook / token / 密钥 / 带签名的 URL），
//	必须加进这张表。漏了不会有任何报错——只会安静地泄露出去。
var sensitiveSettingKeys = map[string]bool{
	"feishu_webhook": true,
}

func (h *SettingsHandler) Get(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT k, COALESCE(v,'') FROM settings`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	// ⚠️ /api/settings 是**公开读**接口（permPublicRead 白名单里），任何登录用户都能拿。
	//	原来它把 feishu_webhook 原样吐出来——webhook URL 本身就是发消息权限，
	//	拿到就能往运维群投伪造告警（CMDB-034，只读账号实测可读）。
	//	敏感键一律默认掩码，只有能管通知的人拿真值。
	full := HasPerm(c, "cmdb:manage_notify")
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		if !full && sensitiveSettingKeys[k] {
			v = maskWebhookURL(v)
		}
		out[k] = v
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
		// 掩码值 = 用户没改这一项（界面上显示的就是掩码），保留库里的原值。
		// 不判这一下的话，管理员点一次保存就把 webhook 写成 `https://...****`。
		if sensitiveSettingKeys[k] && isMasked(v) {
			continue
		}
		if _, err := h.DB.Exec(`INSERT INTO settings (k, v) VALUES (?, ?) ON DUPLICATE KEY UPDATE v=VALUES(v)`, k, v); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	SetAuditTarget(c, "")
	c.JSON(200, gin.H{"ok": true})
}
