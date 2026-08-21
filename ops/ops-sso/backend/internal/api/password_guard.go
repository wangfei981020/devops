package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/auth"
)

// passwordChangeGuard 标记了「必须改密」的账号，在改密之前什么都不能做。
//
// # 为什么必须在服务端拦
//
// 只在前端跳转到改密页是**可以绕过的**：直接调接口就行。
// 而"强制改密"这件事的全部意义就在于它不能被绕过 ——
// 能绕过的强制，等于没有。
//
// # 放行清单为什么这么小
//
//	/auth/password  改密本身
//	/auth/logout    不让人退出会把他困死在页面上
//	/auth/me        前端要靠它知道"我处于必须改密的状态"
//
// 其余一律 428（还差一步），而不是 403（不行）——
// 前端据此弹改密页，而不是显示一个死胡同。
func passwordChangeGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := identity(c)
		if !id.MustChange {
			c.Next()
			return
		}
		if allowedWhileMustChange(c.Request.URL.Path) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusPreconditionRequired,
			apierr.New(http.StatusPreconditionRequired, apierr.CodeMustChangePassword, nil))
	}
}

var mustChangeAllowlist = []string{
	"/auth/password",
	"/auth/logout",
	"/auth/me",
}

func allowedWhileMustChange(path string) bool {
	for _, s := range mustChangeAllowlist {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

// changePassword 改自己的口令。
func changePassword(c *gin.Context, d Deps) (any, error) {
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	id := identity(c)
	err := d.Auth.ChangeOwnPassword(c.Request.Context(), id.UserID, req.OldPassword, req.NewPassword)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrBadCredential):
		// 旧口令不对。单独的码：让界面能说清是哪一栏填错了
		return nil, apierr.New(http.StatusUnauthorized, apierr.CodeOldPasswordWrong, nil)
	case errors.Is(err, auth.ErrSamePassword):
		return nil, apierr.BadRequest(apierr.CodeSamePassword, nil)
	case errors.Is(err, auth.ErrWeakPassword):
		return nil, apierr.BadRequest(apierr.CodeWeakPassword,
			map[string]any{"min": auth.MinPasswordLen, "reason": err.Error()})
	case errors.Is(err, auth.ErrNoLocalPassword):
		return nil, apierr.BadRequest(apierr.CodeNoLocalPassword, nil)
	default:
		return nil, err
	}

	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "auth.password_change", ObjectType: "user", ObjectID: id.UserID,
		ClientIP: clientIP(c),
		// ⚠️ 口令本身绝不写进审计
	})

	// 改密后**踢掉其他会话**。
	//
	// 改密的常见动机之一就是"我怀疑账号被盗"。只改口令而留着别人已有的会话，
	// 等于什么都没做 —— 对方还在里面。当前这个会话保留，否则人会被自己踢出去。
	if n, err := d.Auth.RevokeOthersOfUser(c.Request.Context(), id.UserID, id.SessionID,
		"password_changed"); err == nil && n > 0 {
		return gin.H{"ok": true, "revoked_sessions": n}, nil
	}
	return gin.H{"ok": true, "revoked_sessions": 0}, nil
}
