package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/auth"
	"ops-sso-backend/internal/store"
)

// SessionCookie 会话 cookie 名。
//
// HttpOnly + SameSite=Lax：前端 JS 拿不到令牌，XSS 也偷不走；
// Lax 保证从邮件里点链接过来仍带上会话，同时挡掉跨站表单提交。
const SessionCookie = "oag_session"

// devHeaderAuth 是否允许用请求头冒充身份（只给本地联调）。
//
// 默认关闭。开着的时候每个请求都会在响应头里挂一个显眼的标记 ——
// 运维平台踩过的坑是 X-Operator 头可伪造却没人发现，这次让它藏不住。
var devHeaderAuth = false

func SetDevHeaderAuth(on bool) { devHeaderAuth = on }

// authRequired 会话认证中间件。
func authRequired(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tok := sessionToken(c); tok != "" {
			id, err := d.Auth.Authenticate(c.Request.Context(), tok)
			if err == nil {
				bind(c, id)
				c.Next()
				return
			}
			if err == auth.ErrDisabled {
				// 账号已停用（离职断权）：给一个不同的码，界面才能说清"不是你密码错了"
				c.AbortWithStatusJSON(http.StatusForbidden,
					apierr.New(http.StatusForbidden, apierr.CodeForbidden, nil))
				return
			}
		}

		if devHeaderAuth {
			uid, e1 := strconv.ParseInt(c.GetHeader("X-Gate-User-Id"), 10, 64)
			tid, e2 := strconv.ParseInt(c.GetHeader("X-Gate-Tenant-Id"), 10, 64)
			if e1 == nil && e2 == nil && uid > 0 && tid > 0 {
				c.Header("X-Gate-Dev-Auth", "on") // 藏不住的标记
				bind(c, auth.Identity{UserID: uid, TenantID: tid, Source: "dev-header"})
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, apierr.Unauthorized())
	}
}

// optionalAuth 认了就带上身份，没认也放行。
//
// 给 OIDC 的 authorize/logout 用：未登录时要**跳登录页**，
// 而不是回一个 401 —— 浏览器拿到 401 只会显示一坨 JSON。
func optionalAuth(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tok := sessionToken(c); tok != "" {
			if id, err := d.Auth.Authenticate(c.Request.Context(), tok); err == nil {
				bind(c, id)
			}
		}
		c.Next()
	}
}

func bind(c *gin.Context, id auth.Identity) {
	c.Set("identity", id)
	c.Request = c.Request.WithContext(store.WithTenant(c.Request.Context(), store.TenantID(id.TenantID)))
}

func sessionToken(c *gin.Context) string {
	if v, err := c.Cookie(SessionCookie); err == nil && v != "" {
		return v
	}
	h := c.GetHeader("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	return ""
}

func identity(c *gin.Context) auth.Identity {
	v, _ := c.Get("identity")
	id, _ := v.(auth.Identity)
	return id
}

func actorID(c *gin.Context) int64 { return identity(c).UserID }

func pathID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apierr.BadRequest(apierr.CodeInvalidParam, map[string]any{"field": "id"})
	}
	return id, nil
}

func clientIP(c *gin.Context) string { return c.ClientIP() }
