package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"opsplatform-cmdb-backend/logx"
)

// 用户管理。
//
//	CMDB 里的用户分两类，界面上必须一眼分得出来，因为**能对它们做的事完全不同**：
//
//	  local  —— 本地账号。密码存在 CMDB 自己的库里，不受运维平台权限约束
//	             （见 perm.go：本地账号一律放行）。它是运维平台挂掉时的兜底通道。
//	  portal —— 运维平台 SSO 用户。CMDB 这边只是一条**影子记录**：
//	             首次 SSO 登录时自动建，用来挂会话和写审计。
//	             它的身份、密码、权限**全在运维平台**，CMDB 改不了也不该改。
//
//	所以对 portal 用户，这里只提供"看"和"踢下线"，不提供改密码/改显示名——
//	提供了也是假的：下次 SSO 登录就被运维平台的值覆盖回去。

type UserHandler struct{ DB *sql.DB }

func NewUserHandler(db *sql.DB) *UserHandler { return &UserHandler{DB: db} }

func (h *UserHandler) Register(r *gin.RouterGroup) {
	r.GET("/users", h.List)
	r.PUT("/users/:id/password", h.ChangePassword)
	r.POST("/users/:id/kick", h.Kick)
	r.DELETE("/users/:id", h.Delete)
}

// List GET /api/users
func (h *UserHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT u.id, u.username, IFNULL(u.display_name,''), u.is_admin,
		IFNULL(u.auth_source,'local'), u.last_login_at, u.created_at,
		(SELECT COUNT(*) FROM auth_sessions s WHERE s.user_id = u.id AND s.expires_at > NOW()) AS active_sessions
		FROM users u ORDER BY u.auth_source, u.username`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	list := []gin.H{}
	for rows.Next() {
		var id, isAdmin, sessions int
		var username, display, src string
		var lastLogin sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&id, &username, &display, &isAdmin, &src, &lastLogin, &createdAt, &sessions); err != nil {
			continue
		}
		item := gin.H{
			"id": id, "username": username, "display_name": display,
			"is_admin": isAdmin == 1, "auth_source": src,
			"active_sessions": sessions,
			"created_at":      createdAt.Format("2006-01-02 15:04:05"),
			// 这三个 flag 由后端算，前端不必自己推断规则——
			// 规则散在两处迟早会不一致
			"can_change_password": src == "local",
			"can_delete":          !(src == "local" && username == "admin"),
			"editable_in":         map[string]string{"local": "CMDB", "portal": "运维平台"}[src],
		}
		if lastLogin.Valid {
			item["last_login_at"] = lastLogin.Time.Format("2006-01-02 15:04:05")
		}
		list = append(list, item)
	}
	c.JSON(200, gin.H{"list": list})
}

// ChangePassword PUT /api/users/:id/password  {"password": "..."}
//
//	只对本地账号有意义：portal 用户的密码在运维平台，这里改了也白改。
func (h *UserHandler) ChangePassword(c *gin.Context) {
	id := c.Param("id")
	var in struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if len(in.Password) < 8 {
		c.JSON(400, gin.H{"error": "密码至少 8 位"})
		return
	}

	var username, src string
	if err := h.DB.QueryRow(`SELECT username, IFNULL(auth_source,'local') FROM users WHERE id=?`, id).
		Scan(&username, &src); err != nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}
	if src != "local" {
		c.JSON(400, gin.H{
			"error": "这是运维平台的 SSO 账号，密码请到运维平台修改",
			"hint":  "CMDB 这边只是一条影子记录，改了也会在下次登录时被覆盖",
		})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(500, gin.H{"error": "密码加密失败"})
		return
	}
	if _, err := h.DB.Exec(`UPDATE users SET password_hash=? WHERE id=?`, string(hash), id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 改完密码把该用户的会话全踢掉：否则旧密码泄露的场景下，
	// 攻击者手上那个已登录会话照样能用，改密码等于没改
	res, _ := h.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=?`, id)
	killed := int64(0)
	if res != nil {
		killed, _ = res.RowsAffected()
	}
	logx.Line("users", "改密码 user="+username+"，同时作废其全部会话")
	SetAuditTarget(c, username)
	c.JSON(200, gin.H{"ok": true, "sessions_killed": killed,
		"msg": "密码已更新，该用户的登录会话已全部失效，需要重新登录"})
}

// Kick POST /api/users/:id/kick —— 作废该用户的所有会话
func (h *UserHandler) Kick(c *gin.Context) {
	id := c.Param("id")
	var username string
	if err := h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&username); err != nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}
	res, err := h.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=?`, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	logx.Line("users", "踢下线 user="+username)
	SetAuditTarget(c, username)
	c.JSON(200, gin.H{"ok": true, "sessions_killed": n,
		"msg": "已作废 " + strconv.FormatInt(n, 10) + " 个会话"})
}

// Delete DELETE /api/users/:id
//
//	portal 影子账号删掉没关系——下次 SSO 登录会自动重建。
//	本地 admin 不能删：它是运维平台不可用时唯一的入口。
func (h *UserHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	var username, src string
	if err := h.DB.QueryRow(`SELECT username, IFNULL(auth_source,'local') FROM users WHERE id=?`, id).
		Scan(&username, &src); err != nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}
	if src == "local" && username == "admin" {
		c.JSON(400, gin.H{
			"error": "本地 admin 不能删除",
			"hint":  "它是运维平台不可用时唯一的登录入口，删掉就把自己锁在门外了",
		})
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`DELETE FROM users WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logx.Line("users", "删除用户 "+username+"（"+src+"）")
	SetAuditTarget(c, username)
	msg := "已删除"
	if src == "portal" {
		msg = "已删除该影子记录；此人若仍有运维平台权限，下次 SSO 登录会自动重建"
	}
	c.JSON(200, gin.H{"ok": true, "msg": msg})
}

var _ = http.StatusOK
