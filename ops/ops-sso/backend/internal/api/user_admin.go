package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/auth"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/logx"
)

// ══════════════════════════════════════════════════════════════════
// 人员的写操作
// ══════════════════════════════════════════════════════════════════
//
// 这一组接口里的每一个都能把人**锁在系统外面**，所以边界写在最前面：
//
//	不能停用自己      —— 点完就登不进来，而他多半正在处理别的事
//	不能给自己降权    —— 同上，且降完就没人能改回去
//	不能动最后一个管理员 —— 停用/降权最后一个管理员 = 整个租户从此无人能配置，
//	                      只能去数据库里改。这是真实发生过的事故类型。
//
// 三条都在**服务端**判。前端置灰是体验，不是防线。

// createUserReq 建一个本地账号。
//
// 只建本地账号：来自身份源的用户由登录时自动创建（JIT），
// 在这里手工建一个同名的，会和 JIT 撞 `uq_users_source_username`，
// 而那个报错看起来像"用户名被占用"，让人以为是重名。
type createUserReq struct {
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	RoleCode    string  `json:"role_code"`
	GroupIDs    []int64 `json:"group_ids"`
}

func adminCreateUser(c *gin.Context, d Deps) (any, error) {
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	if req.RoleCode != auth.RoleAdmin {
		// 只认 admin，其余一律普通成员。**不照抄前端传来的字符串** ——
		// 那样传个 "superadmin" 进来，会存下一个谁也看不懂、
		// 而 IsAdmin() 判定为 false 的角色，看起来像权限丢了。
		req.RoleCode = "member"
	}

	ctx := c.Request.Context()
	id := identity(c)

	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return nil, err
	}

	// users 是全局表，不能用 Scoped 的租户注入（它会要求 tenant_id = ?）。
	// 租户归属写在 user_tenants 里。
	res, err := d.Store.Raw().ExecContext(ctx,
		`INSERT INTO users (username, display_name, email, source) VALUES (?, ?, ?, 'local')`,
		req.Username, req.DisplayName, req.Email)
	if err != nil {
		if isDupEntry(err) {
			return nil, apierr.New(http.StatusConflict, apierr.CodeUserExists,
				map[string]any{"username": req.Username})
		}
		return nil, err
	}
	uid, _ := res.LastInsertId()

	if _, err := d.Store.Raw().ExecContext(ctx,
		`INSERT INTO user_tenants (user_id, tenant_id, role_code) VALUES (?, ?, ?)`,
		uid, int64(id.TenantID), req.RoleCode); err != nil {
		return nil, err
	}

	// 初装口令：允许弱口令但**标记必须改密**。
	// 建号的人知道这个口令，所以它在被本人改掉之前不该能做任何事。
	if err := d.Auth.SetBootstrapPassword(ctx, uid, req.Password); err != nil {
		if errors.Is(err, auth.ErrWeakPassword) {
			return nil, apierr.BadRequest(apierr.CodeWeakPassword,
				map[string]any{"min": auth.MinPasswordLen})
		}
		return nil, err
	}

	if len(req.GroupIDs) > 0 {
		if err := setUserGroups(q, uid, req.GroupIDs); err != nil {
			// 组没设上不该让建号整个失败（号已经建好了），但必须说出来 ——
			// 悄悄少一个组 = 这个人少一批权限，而没人知道为什么。
			logx.Line("user", "建号成功但设置用户组失败: "+err.Error())
		}
	}

	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: actorID(c), ActorName: id.Username,
		Action: "user.create", ObjectType: "user", ObjectID: uid, ClientIP: clientIP(c),
		// ⚠️ 绝不记口令。审计会被导出、被转发、被截图
		Detail: map[string]any{"username": req.Username, "role": req.RoleCode},
	})
	return gin.H{"id": uid, "must_change_password": true}, nil
}

// adminSetUserGroups 改一个人的用户组归属。
func adminSetUserGroups(c *gin.Context, d Deps) (any, error) {
	uid, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var req struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ctx := c.Request.Context()
	if err := requireSameTenant(ctx, d, uid); err != nil {
		return nil, err
	}
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	if err := setUserGroups(q, uid, req.GroupIDs); err != nil {
		return nil, err
	}
	d.Audit.Write(ctx, auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "user.set_groups", ObjectType: "user", ObjectID: uid, ClientIP: clientIP(c),
		Detail: map[string]any{"group_ids": req.GroupIDs},
	})
	return gin.H{"id": uid}, nil
}

// adminSetUserStatus 停用 / 恢复一个账号。
//
// 停用要**同时踢掉他所有会话**：只改 status 而不清会话的话，
// 已经登录的那个人能继续用到会话过期 —— 而"离职断权"要的正是立刻生效。
// （Authenticate 里也会查 status，两处都做是有意的：一处是即时收回，
// 一处是防止有会话从别的路径被造出来。）
func adminSetUserStatus(c *gin.Context, d Deps) (any, error) {
	uid, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var req struct {
		Disabled bool `json:"disabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ctx := c.Request.Context()
	id := identity(c)

	if uid == id.UserID {
		// 停用自己：点完就登不进来了，而他多半正在处理别的事。
		// 要退出有「退出登录」，语义清楚得多。
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, map[string]any{"reason": "self"})
	}
	if err := requireSameTenant(ctx, d, uid); err != nil {
		return nil, err
	}
	if req.Disabled {
		if err := requireNotLastAdmin(ctx, d, uid, id.TenantID); err != nil {
			return nil, err
		}
	}

	status := "active"
	if req.Disabled {
		status = "disabled"
	}
	if _, err := d.Store.Raw().ExecContext(ctx,
		`UPDATE users SET status = ? WHERE id = ? AND deleted_at IS NULL`, status, uid); err != nil {
		return nil, err
	}

	var revoked int64
	if req.Disabled {
		n, err := d.Auth.RevokeAllOfUser(ctx, uid, "disabled")
		if err != nil {
			// 会话没踢掉是**安全问题**，不能只当成一次失败的写操作：
			// 账号已经停用，而他手里的会话还活着。必须让人看见。
			logx.Line("user", "已停用但清理会话失败，该账号的现有会话可能仍然可用: "+err.Error())
			return nil, err
		}
		revoked = n
	}

	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: actorID(c), ActorName: id.Username,
		Action: "user.set_status", ObjectType: "user", ObjectID: uid, ClientIP: clientIP(c),
		Detail: map[string]any{"status": status, "revoked_sessions": revoked},
	})
	return gin.H{"id": uid, "status": status, "revoked_sessions": revoked}, nil
}

// adminSetUserRole 改角色（管理员 / 普通成员）。
func adminSetUserRole(c *gin.Context, d Deps) (any, error) {
	uid, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var req struct {
		RoleCode string `json:"role_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	role := "member"
	if req.RoleCode == auth.RoleAdmin {
		role = auth.RoleAdmin
	}

	ctx := c.Request.Context()
	id := identity(c)
	if uid == id.UserID && role != auth.RoleAdmin {
		// 给自己降权：降完就没人能改回去了（如果他是最后一个管理员），
		// 而即使不是，误点一次也要去找别人恢复。
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, map[string]any{"reason": "self"})
	}
	if err := requireSameTenant(ctx, d, uid); err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin {
		if err := requireNotLastAdmin(ctx, d, uid, id.TenantID); err != nil {
			return nil, err
		}
	}

	if _, err := d.Store.Raw().ExecContext(ctx,
		`UPDATE user_tenants SET role_code = ? WHERE user_id = ? AND tenant_id = ?`,
		role, uid, int64(id.TenantID)); err != nil {
		return nil, err
	}

	// ⚠️ 角色缓存在会话的 Identity 里，不踢会话的话**改了不生效**：
	// 降权的人手里那个会话仍然是管理员，直到它自己过期。
	// 提权同理（他要重新登录才拿得到新角色），但提权不生效只是不方便，
	// 降权不生效是安全问题 —— 两种都踢，行为一致更好解释。
	n, err := d.Auth.RevokeAllOfUser(ctx, uid, "role_changed")
	if err != nil {
		logx.Line("user", "角色已改但清理会话失败，旧会话里的角色仍是旧值: "+err.Error())
		return nil, err
	}

	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: actorID(c), ActorName: id.Username,
		Action: "user.set_role", ObjectType: "user", ObjectID: uid, ClientIP: clientIP(c),
		Detail: map[string]any{"role": role, "revoked_sessions": n},
	})
	return gin.H{"id": uid, "role_code": role, "revoked_sessions": n}, nil
}

// adminResetPassword 重置某人的口令。
//
// 走 bootstrap 语义：允许弱口令但标记必须改密 —— 重置的人知道这个口令，
// 所以它在被本人改掉之前不该能做任何事。
func adminResetPassword(c *gin.Context, d Deps) (any, error) {
	uid, err := pathID(c)
	if err != nil {
		return nil, err
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Password == "" {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	ctx := c.Request.Context()
	if err := requireSameTenant(ctx, d, uid); err != nil {
		return nil, err
	}
	if err := d.Auth.SetBootstrapPassword(ctx, uid, req.Password); err != nil {
		if errors.Is(err, auth.ErrNoLocalPassword) {
			// 身份源来的账号没有本地口令，重置它没有意义 ——
			// 说清楚，否则管理员会以为重置成功了而对方仍然登不进来
			return nil, apierr.BadRequest(apierr.CodeNoLocalPassword, nil)
		}
		return nil, err
	}
	// 重置口令后踢会话：口令被重置多半意味着账号有风险
	n, _ := d.Auth.RevokeAllOfUser(ctx, uid, "password_reset")

	d.Audit.Write(ctx, auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "user.reset_password", ObjectType: "user", ObjectID: uid, ClientIP: clientIP(c),
		// ⚠️ 只记发生过，不记口令
		Detail: map[string]any{"revoked_sessions": n, "must_change": true},
	})
	return gin.H{"id": uid, "must_change_password": true, "revoked_sessions": n}, nil
}

// ── 共用校验 ──────────────────────────────────────────────────────

// requireSameTenant 目标用户必须属于当前租户。
//
// 不校验的话，改一个 ID 就能动别的租户的人 —— 而 users 是全局表，
// ID 是连号的，猜都不用猜。
//
// 不属于本租户时返回 404 而不是 403：403 等于告诉对方"这个 ID 存在"，
// 那本身就是一条信息。
func requireSameTenant(ctx context.Context, d Deps, uid int64) error {
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return err
	}
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM user_tenants WHERE tenant_id = ? AND user_id = ?`,
		uid).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return apierr.CrossTenant()
	}
	return nil
}

// requireNotLastAdmin 不许停用/降权**最后一个**管理员。
//
// 少了这条，一次误点就能让整个租户从此无人能配置 —— 只能去数据库里改。
// 判定排除目标本人：算的是"除他之外还有没有别的管理员"。
//
// ⚠️ 还要排除已停用的账号：一个 disabled 的管理员救不了场，
// 把他算进来等于"看起来还有人，实际上没有"。
func requireNotLastAdmin(ctx context.Context, d Deps, uid int64, tenant int64) error {
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return err
	}
	var others int
	if err := q.QueryRow(`SELECT COUNT(*) FROM user_tenants ut
		JOIN users u ON u.id = ut.user_id
		WHERE ut.tenant_id = ? AND ut.role_code = ? AND ut.user_id <> ?
		  AND u.status = 'active' AND u.deleted_at IS NULL`,
		auth.RoleAdmin, uid).Scan(&others); err != nil {
		return err
	}
	if others == 0 {
		return apierr.BadRequest(apierr.CodeLastAdmin, nil)
	}
	return nil
}

func isDupEntry(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

// setUserGroups 覆盖式设置用户组归属：先清后插。
//
// ⚠️ INSERT 走 Insert() 而不是 Exec()：Scoped.Exec 要求语句里有
// `tenant_id = ?`（那是 WHERE 的写法），而 INSERT 的租户在列清单里。
// 用错方法会被 checkFilter 拒掉，报的是"缺租户过滤"——
// 而语句里明明写了 tenant_id，看着像误报。
func setUserGroups(q *store.Scoped, uid int64, groupIDs []int64) error {
	if _, err := q.Exec(`DELETE FROM user_group_members WHERE tenant_id = ? AND user_id = ?`, uid); err != nil {
		return err
	}
	for _, gid := range groupIDs {
		if _, err := q.Insert(
			`INSERT INTO user_group_members (tenant_id, group_id, user_id) VALUES (?, ?, ?)`,
			gid, uid); err != nil {
			return err
		}
	}
	return nil
}
