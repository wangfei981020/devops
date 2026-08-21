package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/auth"
)

// ── 登录 ─────────────────────────────────────────────────────────

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code"`      // 应急通道用
	TenantID int64  `json:"tenant_id"` // 不传默认 1
}

func login(c *gin.Context, d Deps) (any, error) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	if req.TenantID == 0 {
		req.TenantID = 1
	}
	tok, id, err := d.Auth.LoginLocal(c.Request.Context(), req.Username, req.Password,
		req.TenantID, clientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		return nil, loginErr(err)
	}
	setSessionCookie(c, tok)
	return identityJSON(id), nil
}

// loginBreakGlass 应急通道登录：只要一次性口令，不依赖短信/上游 IdP。
//
// 这是 P0-3 的落地 —— 上次演练里应急通道的验证码走的正是被断开的那条链路。
func loginBreakGlass(c *gin.Context, d Deps) (any, error) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	if req.TenantID == 0 {
		req.TenantID = 1
	}
	tok, id, err := d.Auth.LoginBreakGlass(c.Request.Context(), req.Username, req.Code,
		req.TenantID, clientIP(c), c.GetHeader("User-Agent"))
	if err != nil {
		return nil, loginErr(err)
	}
	setSessionCookie(c, tok)

	// 应急登录必须留痕并通知。通知**不是登录的前置条件** ——
	// 前置就等于又把应急通道绑在一个外部系统上，环形依赖会重新长回来。
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "auth.break_glass_login", ObjectType: "user", ObjectID: id.UserID,
		ClientIP: clientIP(c),
		Detail:   map[string]any{"source": "break_glass"},
	})

	out := identityJSON(id)
	out["break_glass"] = true
	return out, nil
}

func loginErr(err error) error {
	switch {
	case errors.Is(err, auth.ErrBadCredential):
		return apierr.New(http.StatusUnauthorized, apierr.CodeBadCredential, nil)
	case errors.Is(err, auth.ErrDisabled):
		return apierr.New(http.StatusForbidden, apierr.CodeAccountDisabled, nil)
	case errors.Is(err, auth.ErrCodeUsed):
		return apierr.New(http.StatusUnauthorized, apierr.CodeBreakGlassUsed, nil)
	case errors.Is(err, auth.ErrCodeExpired):
		return apierr.New(http.StatusUnauthorized, apierr.CodeBreakGlassExpired, nil)
	case errors.Is(err, auth.ErrCodeInvalid):
		return apierr.New(http.StatusUnauthorized, apierr.CodeBadCredential, nil)
	case errors.Is(err, auth.ErrBackend):
		// 503 + 独立错误码：界面说"认证服务暂时不可用，不是你口令错了"
		return apierr.New(http.StatusServiceUnavailable, apierr.CodeAuthBackend, nil)
	}
	return err
}

func setSessionCookie(c *gin.Context, tok string) {
	// Secure 由部署决定：本地 http 调试时置 false，生产必须 true。
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(SessionCookie, tok, int(auth.SessionTTL.Seconds()), "/", "", secure, true)
}

func logout(c *gin.Context, d Deps) (any, error) {
	id := identity(c)
	if id.SessionID > 0 {
		if err := d.Auth.Revoke(c.Request.Context(), id.SessionID, "user_logout"); err != nil {
			return nil, err
		}
	}
	c.SetCookie(SessionCookie, "", -1, "/", "", false, true)
	return gin.H{"ok": true}, nil
}

func me(c *gin.Context, d Deps) (any, error) {
	return identityJSON(identity(c)), nil
}

func identityJSON(id auth.Identity) gin.H {
	return gin.H{
		"user_id": id.UserID, "tenant_id": id.TenantID,
		"username": id.Username, "display_name": id.DisplayName,
		"source": id.Source, "is_break_glass": id.IsBreakGlass,
		// 前端据此把人送去改密页。**这只是体验层** ——
		// 真正的强制在 passwordChangeGuard 中间件里，绕不过去。
		"must_change_password": id.MustChange,
		// 角色给前端用来决定**显不显示**管理入口。
		// ⚠️ 这只是体验层：真正的拦截在 adminOnly 中间件里。
		// 前端拿它做展示可以，绝不能拿它当权限判据 —— 改一个 JS 变量
		// 就能让按钮出现，而按钮出现不等于接口会放行。
		"role":     id.RoleCode,
		"is_admin": id.IsAdmin(),
	}
}

// ── 我的会话与设备 ────────────────────────────────────────────────

func mySessions(c *gin.Context, d Deps) (any, error) {
	list, err := d.Auth.ListSessionsOfUser(c.Request.Context(), actorID(c))
	if err != nil {
		return nil, err
	}
	cur := identity(c).SessionID
	for _, s := range list {
		s["current"] = s["id"] == cur
	}
	return gin.H{"items": list, "total": len(list)}, nil
}

func revokeSession(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	// 只能下线自己的会话。不校验归属的话，改个 ID 就能把别人踢下线。
	list, err := d.Auth.ListSessionsOfUser(c.Request.Context(), actorID(c))
	if err != nil {
		return nil, err
	}
	var mine bool
	for _, s := range list {
		if s["id"] == id {
			mine = true
		}
	}
	if !mine {
		return nil, apierr.CrossTenant()
	}
	if err := d.Auth.Revoke(c.Request.Context(), id, "user_revoked"); err != nil {
		return nil, err
	}
	return nil, nil
}

// ── 应急口令签发（控制台）─────────────────────────────────────────

func issueBreakGlass(c *gin.Context, d Deps) (any, error) {
	var req struct {
		UserID int64 `json:"user_id"`
		Count  int   `json:"count"`
		Days   int   `json:"days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID <= 0 {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	if req.Days <= 0 {
		req.Days = 180
	}
	codes, err := d.Auth.IssueBreakGlassCodes(c.Request.Context(), req.UserID, req.Count,
		time.Duration(req.Days)*24*time.Hour, actorID(c), time.Now().Format("20060102"))
	if err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "auth.break_glass_issue", ObjectType: "user", ObjectID: req.UserID,
		ClientIP: clientIP(c),
		// ⚠️ 只记数量，绝不记口令本身 —— 审计日志会被导出、被转发、被截图
		Detail: map[string]any{"count": len(codes), "days": req.Days},
	})
	return gin.H{
		"codes": codes,
		// 明文只在这一次返回。前端必须提示「打印封存，关掉就再也看不到」。
		"warning_code": "break_glass.print_and_seal",
	}, nil
}
