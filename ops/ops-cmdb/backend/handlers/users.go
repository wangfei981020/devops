package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
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

// minPasswordLen 本地账号口令长度下限。
//
// ⚠️ 这个数字**同时出现在校验和文案里**（"密码至少 N 位"）。
//
//	原来是三处写死的 8 加三句写死的中文 —— 改下限时必然漏掉某一处，
//	而漏掉的那一处不会报错，只会让提示和实际校验对不上。
const minPasswordLen = 8

func NewUserHandler(db *sql.DB) *UserHandler { return &UserHandler{DB: db} }

func (h *UserHandler) Register(r *gin.RouterGroup) {
	r.GET("/users", h.List)
	r.POST("/users", h.Create)
	r.PUT("/users/:id/role", h.ChangeRole)
	r.PUT("/users/:id/password", h.ChangePassword)
	// 自助改密：任何登录用户都能改自己的，不要 manage_users
	r.PUT("/me/password", h.ChangeOwnPassword)
	r.POST("/users/:id/kick", h.Kick)
	r.DELETE("/users/:id", h.Delete)
}

// List GET /api/users
func (h *UserHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT u.id, u.username, IFNULL(u.display_name,''), u.is_admin,
		IFNULL(u.auth_source,'local'), IFNULL(u.role_code,''),
		IFNULL((SELECT r.name FROM local_roles r WHERE r.code = u.role_code), ''),
		u.last_login_at, u.created_at,
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
		var username, display, src, roleCode, roleName string
		var lastLogin sql.NullTime
		var createdAt time.Time
		if err := rows.Scan(&id, &username, &display, &isAdmin, &src, &roleCode, &roleName,
			&lastLogin, &createdAt, &sessions); err != nil {
			continue
		}
		item := gin.H{
			"id": id, "username": username, "display_name": display,
			"is_admin": isAdmin == 1, "auth_source": src,
			"active_sessions": sessions,
			"role_code":       roleCode,
			// 角色为空 = 升级前建的老账号，语义上就是不受限。
			// 显示成"不受限"而不是留白——留白会被当成"还没配"，
			// 而它其实是**权限最大**的那一档，看反了很危险。
			"role_name": roleDisplayName(src, roleCode, roleName),
			// portal 用户的角色在运维平台，这里改不了
			"can_change_role": src == "local" && !(src == "local" && username == "admin"),
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
	// 🔴 「本地 admin 仍在用默认口令」必须显示在**界面**上。
	//
	//	只打 WARN 到日志是不够的 —— 日志没人天天看，而这个账号权限校验全放行。
	//	启动时自检一次（EnsureAdmin），这里把结论带出去（OPSCMDB-074）。
	//	⚠️ 字段恒发，false 也发：不发的话前端分不清"检查过没问题"和"没检查"。
	c.JSON(200, gin.H{"list": list, "admin_weak_password": AdminWeakPasswordDetected()})
}

// roleDisplayName 角色在列表里怎么显示。
//
//	三种情况必须区分开：
//	  portal 用户   —— 角色在运维平台，CMDB 这边不存也不显示具体角色
//	  role_code 为空 —— 升级前的老账号，**不受限**（权限最大的一档）
//	  其余          —— 角色名；查不到说明角色被删了，要明说而不是留白
func roleDisplayName(src, code, name string) string {
	if src != "local" {
		return "由运维平台分配"
	}
	if code == "" {
		// 升级前就存在的老账号：它**没有角色**，走的是"不设角色即全权限"
		// 那条兼容规则。写成「不受限（管理员）」会和行首的「不受限」徽章
		// 重复，而且把"该给它配个角色"这件事藏起来了（OPSCMDB-031 P2-61）。
		return "未设角色（沿用全权限）"
	}
	if name == "" {
		return "角色已不存在（" + code + "）"
	}
	return name
}

// Create POST /api/users  {"username","password","display_name","role_code"}
//
//	只能建**本地**账号。portal 用户是 SSO 首次登录时自动建的影子记录，
//	在这里手工造一条没有意义：运维平台那边没有对应的人，永远登不进来。
func (h *UserHandler) Create(c *gin.Context) {
	var in struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		RoleCode    string `json:"role_code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || len(in.Password) < minPasswordLen {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.userCreateInvalid", nil, map[string]any{"min": minPasswordLen})
		return
	}
	// 新建时同样要挡。⚠️ 这一处的校验写法和另外两处不同（用户名和口令合并判），
	//	批量加拦截时正是因为这个写法差异漏掉了它 —— 与"状态码两种写法"同一类坑。
	if isKnownWeakPassword(in.Password) {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordTooCommon", nil, nil)
		return
	}
	// 角色必须显式选。默认给个"不受限"太危险——建号的人未必意识到
	// 自己刚发出去的是一把万能钥匙。
	if in.RoleCode == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.roleRequired", nil, nil)
		return
	}
	var exists int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM local_roles WHERE code=?`, in.RoleCode).Scan(&exists); err != nil || exists == 0 {
		httpx.FailKey(c, httpx.CodeNotFound, "error.roleNotFound", nil, map[string]any{"code": in.RoleCode})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.FailKey(c, httpx.CodeInternal, "error.passwordHashFailed", err, nil)
		return
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	res, err := h.DB.Exec(
		`INSERT INTO users (username, password_hash, display_name, is_admin, role_code, auth_source)
		 VALUES (?, ?, ?, ?, ?, 'local')`,
		in.Username, string(hash), in.DisplayName, boolToInt(in.RoleCode == "cmdb_admin"), in.RoleCode)
	if err != nil {
		// 同名本地账号已存在。注意同名的 portal 影子账号是允许并存的
		// （唯一键是 username+auth_source），这里只会撞本地那条。
		httpx.FailKey(c, httpx.CodeConflict, "error.userCreateConflict", err, map[string]any{"name": in.Username})
		return
	}
	id, _ := res.LastInsertId()

	// 建号必须同时落租户归属，否则这个账号能登录、但**每个接口都 403**
	// 「当前账号不属于该租户」——
	// 登录时 resolveTenant 查不到归属会回落到默认租户并照常发会话，
	// 而请求时的租户中间件按 user_tenants 校验，两边判据不一致。
	// 建号的人完全不会想到"租户"这个他从没碰过的概念。
	if _, e := h.DB.Exec(store.SeedUserTenantQuery, id); e != nil {
		// 不回滚建号：账号已经建好了，回滚反而更难解释。
		// 但要明说后果，否则报障时只有一句"新号打不开任何页面"。
		logx.Line("users", fmt.Sprintf(
			"WARN 账号 %s 已建但租户归属没落上（该账号会在每个业务接口 403）: %v", in.Username, e))
	}

	SetAuditTarget(c, in.Username+" 角色="+in.RoleCode)
	AuditCreated(c, "users", id)
	logx.Line("users", "创建本地账号 "+in.Username+" 角色="+in.RoleCode)
	c.JSON(200, gin.H{"ok": true, "id": id})
}

// ChangeRole PUT /api/users/:id/role  {"role_code": "..."}
//
//	⚠️ 改角色**立即作废该用户的所有会话**。
//	不这么做的话，权限快照还留在旧会话里：把人从管理员降成只读，
//	他那个窗口照样能删东西，直到会话自然过期（默认 24 小时）。
//	降权不即时生效，等于没降。
func (h *UserHandler) ChangeRole(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		httpx.Invalid(c, "id", "number")
		return
	}
	var in struct {
		RoleCode string `json:"role_code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.RoleCode == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.roleRequired", nil, nil)
		return
	}
	var username, src string
	if err := h.DB.QueryRow(`SELECT username, IFNULL(auth_source,'local') FROM users WHERE id=?`, id).
		Scan(&username, &src); err != nil {
		httpx.NotFound(c, "user")
		return
	}
	if src != "local" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.roleManagedByPortal", nil, nil)
		return
	}
	// 最后一个兜底管理员不能降权——运维平台一挂就再也进不来了，
	// 而这个恰恰是本地账号存在的全部理由。
	if in.RoleCode != "cmdb_admin" {
		var others int
		h.DB.QueryRow(`SELECT COUNT(*) FROM users
			WHERE auth_source='local' AND id<>? AND (role_code='cmdb_admin' OR role_code='')`, id).Scan(&others)
		if others == 0 {
			httpx.FailKey(c, httpx.CodeBadRequest, "error.lastLocalAdmin", nil, nil)
			return
		}
	}
	var exists int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM local_roles WHERE code=?`, in.RoleCode).Scan(&exists); err != nil || exists == 0 {
		httpx.FailKey(c, httpx.CodeNotFound, "error.roleNotFound", nil, map[string]any{"code": in.RoleCode})
		return
	}

	// 改前快照由审计中间件自动抓（users 表在自动快照名单里），这里不用手工记
	if _, err := h.DB.Exec(`UPDATE users SET role_code=?, is_admin=? WHERE id=?`,
		in.RoleCode, boolToInt(in.RoleCode == "cmdb_admin"), id); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	// 见上：降权必须立刻生效
	r, _ := h.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=?`, id)
	killed := int64(0)
	if r != nil {
		killed, _ = r.RowsAffected()
	}
	SetAuditTarget(c, username+" → "+in.RoleCode)
	logx.Line("users", fmt.Sprintf("用户 %s 角色改为 %s，已作废 %d 个会话", username, in.RoleCode, killed))
	c.JSON(200, gin.H{"ok": true, "sessions_killed": killed,
		"msg_key": "users:msg.roleChanged", "msg_params": gin.H{"count": killed},
		"msg": fmt.Sprintf("已改为该角色，并作废了 %d 个在线会话（该用户需重新登录）", killed)})
}

// ChangeOwnPassword PUT /api/me/password  {"old_password","new_password"}
//
//	自助改密。**不需要 cmdb:manage_users**——那个权限的含义是"能管别人的账号"，
//	拿它来卡"改自己的密码"是错的：只读账号因此完全没有改密入口，
//	密码只能找管理员代改，而管理员代改意味着他知道了别人的新密码。
//
//	与管理员代改的两点不同：
//	  1. 必须验旧密码。管理员代改是"忘了密码"场景，自助改密是"我知道旧的想换新的"，
//	     不验旧密码等于任何拿到会话的人都能顺手锁死账号。
//	  2. 只能改自己的——用户 id 取自**会话**，不从请求体拿，避免越权改他人。
func (h *UserHandler) ChangeOwnPassword(c *gin.Context) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, nil, nil)
		return
	}
	if len(in.NewPassword) < minPasswordLen {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordTooShort", nil, map[string]any{"min": minPasswordLen})
		return
	}
	// 🔴 长度够不代表能用：admin123 正好 8 位，而它是曾经写死在源码里的那个。
	//	自检（EnsureAdmin）只能事后发现，这里是**事前**挡住 ——
	//	两件事都要做：已经设了的要能查出来，还没设的不该让他设进去（OPSCMDB-074）。
	if isKnownWeakPassword(in.NewPassword) {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordTooCommon", nil, nil)
		return
	}
	if in.NewPassword == in.OldPassword {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordSameAsOld", nil, nil)
		return
	}

	uid := UserIDFromCtx(c)
	if uid <= 0 {
		httpx.Fail(c, httpx.CodeUnauthorized, nil, nil)
		return
	}
	var username, src, hash string
	if err := h.DB.QueryRow(`SELECT username, IFNULL(auth_source,'local'), password_hash FROM users WHERE id=?`, uid).
		Scan(&username, &src, &hash); err != nil {
		httpx.NotFound(c, "user")
		return
	}
	if src != "local" {
		// hint 合进主文案：它原来是单独一个字段，而**前端一个地方都没读**——
		// 也就是说"改了也会被覆盖"这句最关键的话从来没显示过（OPSCMDB-054 迁移时发现）
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordManagedByPortalSelf", nil, nil)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.OldPassword)) != nil {
		logx.Line("users", fmt.Sprintf("WARN 自助改密旧密码错误 user=%s ip=%s", username, c.ClientIP()))
		httpx.FailKey(c, httpx.CodeBadRequest, "error.wrongOldPassword", nil, nil)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		httpx.FailKey(c, httpx.CodeInternal, "error.passwordHashFailed", err, nil)
		return
	}
	if _, err := h.DB.Exec(`UPDATE users SET password_hash=? WHERE id=?`, string(newHash), uid); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 同管理员代改：把该用户其余会话全踢掉（当前这个也一并作废，前端会跳登录页）。
	// 改密码的场景之一就是"怀疑密码泄露"，不踢会话等于没改。
	res, _ := h.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=?`, uid)
	killed := int64(0)
	if res != nil {
		killed, _ = res.RowsAffected()
	}
	SetAuditTarget(c, username+"（自助改密）")
	logx.Line("users", fmt.Sprintf("用户 %s 自助修改密码，已作废 %d 个会话", username, killed))
	c.JSON(200, gin.H{"ok": true, "msg_key": "users:msg.passwordChangedSelf", "msg": "密码已修改，请用新密码重新登录"})
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
		httpx.Fail(c, httpx.CodeBadRequest, nil, nil)
		return
	}
	if len(in.Password) < minPasswordLen {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordTooShort", nil, map[string]any{"min": minPasswordLen})
		return
	}
	// 🔴 长度够不代表能用：admin123 正好 8 位，而它是曾经写死在源码里的那个。
	//	自检（EnsureAdmin）只能事后发现，这里是**事前**挡住 ——
	//	两件事都要做：已经设了的要能查出来，还没设的不该让他设进去（OPSCMDB-074）。
	if isKnownWeakPassword(in.Password) {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordTooCommon", nil, nil)
		return
	}

	var username, src string
	if err := h.DB.QueryRow(`SELECT username, IFNULL(auth_source,'local') FROM users WHERE id=?`, id).
		Scan(&username, &src); err != nil {
		httpx.NotFound(c, "user")
		return
	}
	if src != "local" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.passwordManagedByPortal", nil, nil)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.FailKey(c, httpx.CodeInternal, "error.passwordHashFailed", err, nil)
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
		"msg_key": "users:msg.passwordResetByAdmin", "msg": "密码已更新，该用户的登录会话已全部失效，需要重新登录"})
}

// Kick POST /api/users/:id/kick —— 作废该用户的所有会话
func (h *UserHandler) Kick(c *gin.Context) {
	id := c.Param("id")
	var username string
	if err := h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, id).Scan(&username); err != nil {
		httpx.NotFound(c, "user")
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
		"msg_key":    "users:msg.sessionsKilled",
		"msg_params": gin.H{"count": n},
		// msg 保留给 MCP / 直接调 API 的人（那一侧读者是 AI 和运维）
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
		httpx.NotFound(c, "user")
		return
	}
	if src == "local" && username == "admin" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.cannotDeleteLocalAdmin", nil, nil)
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
