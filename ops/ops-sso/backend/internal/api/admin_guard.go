package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/logx"
)

// adminOnly 只有当前租户的管理员能过。
//
// # 为什么必须有这道
//
// 在它之前，**任何登录用户都能调控制台的全部接口** —— 改策略、看全员会话、
// 踢别人下线、换 license（换一份不含本产品的授权就能把整套系统变成只读）。
// 前端把控制台放在 /console 只是"没给入口"，不是"进不去"：
// 直接调接口就行，而接口从不问你是谁。
//
// 界面上不显示入口是**体验**，中间件拦下来才是**权限**。
// 这和强制改密同一条纪律：能绕过的强制等于没有。
//
// # 为什么挂在路由组上而不是逐个 handler 里判
//
// 逐个判的漏法是**静默放行**：新加一个接口忘了写那两行，它从此不问权限，
// 没有报错、没有日志、界面完全正常。挂在组上则相反 —— 新接口默认被保护，
// 要放行必须显式挪出这个组，那是个看得见的动作。
//
// # 角色从哪来
//
// `user_tenants.role_code`，会话解析时一并取出（见 auth.Identity.RoleCode）。
// ⚠️ 空值按普通成员处理，不是管理员 —— 数据缺失时给最小权限。
func adminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := identity(c)
		if id.IsAdmin() {
			c.Next()
			return
		}
		// 记下来。被拒的管理操作是安全事件，哪怕只是有人点错了页面 ——
		// 真正的越权尝试和误点在日志里长得一样，但没有日志就两者都看不见。
		logx.J("authz", "denied", map[string]any{
			"user": id.Username, "user_id": id.UserID, "tenant": id.TenantID,
			"role": id.RoleCode, "path": c.Request.URL.Path, "method": c.Request.Method,
		})
		c.AbortWithStatusJSON(http.StatusForbidden,
			apierr.New(http.StatusForbidden, apierr.CodeNotAdmin, nil))
	}
}
