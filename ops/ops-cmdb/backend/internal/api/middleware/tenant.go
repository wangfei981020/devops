// Package middleware 提供请求级的横切处理：租户上下文、审计、限流。
package middleware

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"
)

// gin 上下文里的键。与 handlers 包里那批保持同样的裸字符串风格。
const (
	CtxTenantID   = "tenant_id"
	CtxImpersonat = "impersonating" // 平台管理员代入租户时为 true
)

// TenantResolver 从请求解析出应当生效的租户。
//
// 单独抽成接口是为了让中间件可测：测试里塞一个假的，不用起数据库。
type TenantResolver interface {
	// Resolve 返回该请求的租户，以及是否处于代入状态。
	// 返回 0 表示没有租户上下文（平台管理员未代入）。
	Resolve(c *gin.Context) (store.TenantID, bool, error)
}

// SessionResolver 从会话表读当前活跃租户，并校验用户确实属于它。
//
// ⚠️ **必须校验归属**，不能直接信会话里的值。
// 会话里的 active_tenant_id 是用户自己切换出来的；如果不核对 user_tenants，
// 一个改了自己会话记录（或构造了请求）的用户就能看任意租户的数据。
type SessionResolver struct {
	DB *sql.DB
}

func (r *SessionResolver) Resolve(c *gin.Context) (store.TenantID, bool, error) {
	tokenHash, _ := c.Get("token_hash")
	th, _ := tokenHash.(string)
	if th == "" {
		// 进程内自调用（MCP 回调自己）没有会话，走默认租户。
		// 这类调用的权限在别处控制，不该因为没有会话就整个拒掉。
		return store.TenantID(1), false, nil
	}

	var active int64
	var userID int64
	err := r.DB.QueryRow(
		`SELECT active_tenant_id, user_id FROM auth_sessions WHERE token_hash = ?`, th).
		Scan(&active, &userID)
	if err != nil {
		return 0, false, err
	}
	if active == 0 {
		// 平台管理员登录后尚未代入任何租户 —— 允许访问平台级接口，
		// 业务接口会在 store 层因缺租户上下文而拒绝。
		return 0, false, nil
	}

	// 归属校验：这个用户真的属于这个租户吗
	var n int
	if err := r.DB.QueryRow(
		`SELECT COUNT(*) FROM user_tenants WHERE user_id = ? AND tenant_id = ?`,
		userID, active).Scan(&n); err != nil {
		return 0, false, err
	}
	if n == 0 {
		// 不属于 → 可能是被移出了租户，也可能是有人在伪造。
		// 两种都按"没有租户上下文"处理，让 store 层拒绝业务查询。
		return 0, false, errNotMember
	}
	return store.TenantID(active), false, nil
}

var errNotMember = errors.New("middleware: 用户不属于该租户")

// Tenant 把租户放进请求的 context，供 store 层使用。
//
// ⚠️ 必须挂在认证中间件**之后** —— 它依赖会话信息。
//
// 没有租户上下文时**不拦请求**：平台级接口（租户管理、license、系统信息）
// 本来就不需要租户。业务接口会在 store 层因缺上下文而报错，
// 那里的错误信息更精确（能说出是哪条查询）。
func Tenant(r TenantResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, impersonating, err := r.Resolve(c)
		if err != nil {
			if errors.Is(err, errNotMember) {
				// 越权尝试要明确拒绝，不能静默降级成"无租户" ——
				// 后者会让攻击者以为只是查不到数据，继续试别的路径。
				c.AbortWithStatusJSON(http.StatusForbidden,
					gin.H{"error": "当前账号不属于该租户"})
				return
			}
			// 其余错误（比如会话查不到）交给认证中间件的语义，这里放行，
			// 业务查询会在 store 层被拒。
			c.Next()
			return
		}

		if id != 0 {
			c.Set(CtxTenantID, int64(id))
			c.Set(CtxImpersonat, impersonating)
			// gin 的 Context 与标准 context 是两套。store 读的是后者，
			// 所以必须把它塞进 Request 的 context 里替换掉。
			c.Request = c.Request.WithContext(store.WithTenant(c.Request.Context(), id))
		}
		c.Next()
	}
}

// TenantOf 取当前请求的租户。没有则返回 0。
func TenantOf(c *gin.Context) store.TenantID {
	if v, ok := c.Get(CtxTenantID); ok {
		if id, ok := v.(int64); ok {
			return store.TenantID(id)
		}
	}
	return 0
}

// RequireTenant 用于必须有租户上下文的路由组（所有业务接口）。
//
// 与 store 层的拒绝是两道防线：这里给出的是**面向用户**的错误
// （"请先选择租户"），store 层给出的是**面向开发**的错误（哪条查询没声明）。
func RequireTenant() gin.HandlerFunc {
	return func(c *gin.Context) {
		if TenantOf(c) == 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest,
				gin.H{"error": "请先选择租户", "code": "tenant.not_selected"})
			return
		}
		c.Next()
	}
}
