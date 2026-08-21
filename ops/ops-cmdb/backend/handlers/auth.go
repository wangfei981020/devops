package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// gin context 里的鉴权信息 key。
// ctxUsername 沿用字面量 "username"：WriteAudit 等既有代码按这个名字取值。
const (
	ctxUsername   = "username"
	ctxUserID     = "user_id"
	ctxAuthSource = "auth_source"
	ctxIsAdmin    = "is_admin"
	ctxRole       = "role"
	ctxPerms      = "permissions"
	ctxTokenHash  = "token_hash"
	ctxPermCode   = "perm_code" // 本次请求命中的权限码，写进审计便于反查授权是否过宽
)

type AuthHandler struct {
	DB           *sql.DB
	Secret       []byte
	PortalURL    string
	SessionHours int
	Cipher       *crypto.Cipher // 加密会话里保存的 portal token（刷新权限时要用）
}

func NewAuthHandler(db *sql.DB, secret, portalURL string, sessionHours int, cipher *crypto.Cipher) *AuthHandler {
	if sessionHours <= 0 {
		sessionHours = 24
	}
	return &AuthHandler{
		DB: db, Secret: []byte(secret), PortalURL: portalURL,
		SessionHours: sessionHours, Cipher: cipher,
	}
}

// EnsureAdmin 首启 seed 一个本地 admin。密码取 ADMIN_PASSWORD。
//
//	这个账号是运维平台挂掉时的兜底通道，**权限校验对它全放行**，
//	所以它的登录（成功与失败）都要留审计。
//
//	🔴 没有默认口令。不设 ADMIN_PASSWORD 就不建这个账号 —— 理由见下方。
func (h *AuthHandler) EnsureAdmin() {
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username='admin'`).Scan(&n); err != nil {
		logx.Line("auth", fmt.Sprintf("EnsureAdmin check: %v", err))
		return
	}
	if n > 0 {
		// 🔴 存量账号：**只检测，不自动改**。
		//
		//	自动重置会把人锁在门外 —— 而这个账号往往正是"运维平台不可用时唯一的入口"。
		//	但也不能什么都不做：修复只作用于"admin 不存在时"，
		//	而生产那个 admin 是在此之前用旧逻辑（默认 admin123）建的，
		//	**那个口令至今仍然有效**（OPSCMDB-074）。
		//
		//	⚠️ 只打 WARN 是不够的：日志没人天天看，而这条要的是**被看见**。
		//	所以同时置一个进程内标志，由 /api/users 带给界面（见 AdminWeakPassword）。
		h.checkAdminWeakPassword()
		return
	}
	pw := os.Getenv("ADMIN_PASSWORD")
	if pw == "" {
		// 🔴 不给密码就**不建这个账号**，而不是退回一个默认口令。
		//
		//	这个账号"权限校验对它全放行"（见上方注释）——一个全放行的账号
		//	配一个写在源码里的口令，等于把整套系统的后门公开了。
		//	而且是**静默**的：部署时没人会注意到自己没设这个变量，
		//	日志里那句 "password from ADMIN_PASSWORD or default admin123"
		//	既没报错也没警告，读起来还像一切正常。
		//
		//	实测：生产 chart 里根本没有这个变量，所以线上 admin 用的
		//	就是源码里的 admin123（验收会话 NEW-2）。
		//
		//	⚠️ 不建账号是**安全的失败方式**：SSO / 运维平台通道照常可用，
		//	只是少了本地兜底入口；而"有一个人人都知道口令的全权账号"
		//	不是可以接受的失败方式。
		//	⚠️ 也不要改成随机口令后打进日志 —— 日志会被采集、会被转发。
		logx.Line("auth", "WARN 未设置 ADMIN_PASSWORD，**跳过创建本地 admin 账号**。"+
			"本地兜底登录通道因此不可用（SSO / 运维平台通道不受影响）。"+
			"需要它就设置 ADMIN_PASSWORD 后重启；绝不要使用默认口令。")
		return
	}
	// 口令强度下限。⚠️ 挡的是"随手填个 123"，不是完整的密码策略 ——
	//	一个全放行账号配一个弱口令，和没设密码差别不大。
	if len(pw) < 12 {
		logx.Line("auth", fmt.Sprintf("WARN ADMIN_PASSWORD 只有 %d 位，**跳过创建本地 admin 账号**。"+
			"这个账号权限校验全放行，至少要 12 位。", len(pw)))
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if _, err := h.DB.Exec(`INSERT INTO users (username, password_hash, display_name, is_admin, auth_source)
		VALUES ('admin', ?, '管理员', 1, 'local')`, string(hash)); err != nil {
		logx.Line("auth", fmt.Sprintf("EnsureAdmin seed: %v", err))
		return
	}
	logx.Line("auth", "已创建本地 admin 账号（口令取自 ADMIN_PASSWORD）")
}

// ensureAdminTenant 保证 admin 挂在默认租户下。
//
// # ⚠️ 为什么不能放在 EnsureAdmin 里
//
// EnsureAdmin 开头有 `if n > 0 { return }` —— 只有**首次创建 admin** 才往下走。
// 把归属种子写在它后面，对已存在的安装就永远不会执行。
//
// 这正是"升级路径"最典型的坑：全新部署一切正常，
// 老库升级上来的用户登录后每个接口都 403「当前账号不属于该租户」，
// 而日志里看不出任何异常 —— 种子代码明明写了，只是没被执行。
//
// 所以这类补数据的逻辑必须**独立、幂等、每次启动都跑**。
func (h *AuthHandler) EnsureAdminTenant() {
	res, err := h.DB.Exec(store.SeedAdminTenantQuery)
	if err != nil {
		logx.Line("auth", fmt.Sprintf("WARN 给 admin 建租户归属失败（登录后所有业务接口会 403）: %v", err))
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		logx.J("auth", "admin_tenant_seeded", map[string]any{"note": "已把 admin 挂到默认租户"})
	}

	// admin 之外的本地账号同样要有归属。
	//
	// 早期的建号接口不落 user_tenants，于是建出来的账号能登录、
	// 但每个业务接口都 403「当前账号不属于该租户」—— 而"租户"是建号的人
	// 从没碰过的概念，报障时只会说"新号打不开任何页面"。
	// 建号那头已经补上了，这里补的是**已经存在的那些**。
	if res, err := h.DB.Exec(store.BackfillLocalUserTenantsQuery); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			logx.J("auth", "local_user_tenant_backfilled", map[string]any{
				"count": n, "note": "这些本地账号原先缺租户归属，登录后会在每个业务接口 403",
			})
		}
	} else {
		logx.Line("auth", fmt.Sprintf("WARN 回填本地账号租户归属失败: %v", err))
	}
}

func (h *AuthHandler) RegisterPublic(r *gin.RouterGroup) {
	r.POST("/login", h.Login)
}

// RegisterAuthed 注册需要登录态的鉴权接口（挂在 Middleware 之后）
func (h *AuthHandler) RegisterAuthed(r *gin.RouterGroup) {
	r.GET("/me", h.Me)
	r.GET("/refresh-permissions", h.RefreshPermissions)
	r.POST("/logout", h.Logout)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var id int
	var hash, display, authSource, roleCode string
	err := h.DB.QueryRow(`SELECT id, password_hash, display_name, IFNULL(auth_source,'local'), IFNULL(role_code,'')
		FROM users WHERE username=?`, req.Username).Scan(&id, &hash, &display, &authSource, &roleCode)
	// portal 用户没有本地密码（password_hash 为空），不能走密码登录——
	// 否则空密码 hash 一旦被 bcrypt 意外匹配就是个后门。
	if err != nil || authSource != "local" || hash == "" ||
		bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		logx.Line("auth", fmt.Sprintf("WARN 本地登录失败 user=%s ip=%s", req.Username, c.ClientIP()))
		WriteAuditAs(h.DB, h.ctxWithUser(c, req.Username), "auth.login.failed", "用户名或密码错误")
		httpx.FailKey(c, httpx.CodeUnauthorized, "error.badCredentials", nil, nil)
		return
	}

	// 本地账号按角色发权限。
	//
	//	role_code 为空 = 升级前就有的老账号，保持原样不受约束（迁移 088 刻意不回填，
	//	否则升级这一下就可能把人锁在门外）。其余角色照权限表来。
	//	sessionRole 写进会话，中间件据此决定放不放行——不能再简单地
	//	"见到 local 就全放行"了。
	perms, unrestricted := permsOfLocalRole(h.DB, roleCode)
	sessionRole := roleCode
	if unrestricted {
		sessionRole = "admin" // 与 portal 超管同一个标记，中间件只认这一个值
	}
	var permArg map[string]bool
	if !unrestricted {
		permArg = perms
	}
	token, expires, err := h.issueSession(id, req.Username, sessionRole, "local", permArg, "")
	if err != nil {
		logx.Line("auth", fmt.Sprintf("issue session %s: %v", req.Username, err))
		httpx.FailKey(c, httpx.CodeInternal, "error.createSessionFailed", err, nil)
		return
	}
	h.DB.Exec(`UPDATE users SET last_login_at=NOW() WHERE id=?`, id)
	WriteAuditAs(h.DB, h.ctxWithUser(c, req.Username), "auth.login.success", "auth_source=local")
	c.JSON(http.StatusOK, gin.H{
		"token": token, "username": req.Username, "display_name": display,
		"auth_source": "local", "role": sessionRole, "role_code": roleCode,
		"expires_at": expires,
		// is_admin 决定前端走"全放行"还是"查权限表"，必须和后端 IsAdmin 一致
		"is_admin":    unrestricted,
		"permissions": perms,
	})
}

// Me GET /api/me —— 前端刷新页面后重建登录态用
func (h *AuthHandler) Me(c *gin.Context) {
	uname, _ := c.Get(ctxUsername)
	src, _ := c.Get(ctxAuthSource)
	var display string
	h.DB.QueryRow(`SELECT IFNULL(display_name,'') FROM users WHERE username=?`, uname).Scan(&display)
	c.JSON(http.StatusOK, gin.H{
		"username":     uname,
		"display_name": display,
		"auth_source":  src,
		"is_admin":     IsAdmin(c),
		"permissions":  permsFromCtx(c),
	})
}

// Logout 作废当前会话
func (h *AuthHandler) Logout(c *gin.Context) {
	if th := tokenHashFromCtx(c); th != "" {
		h.DB.Exec(`DELETE FROM auth_sessions WHERE token_hash=?`, th)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// issueSession 生成不透明 token 并落库。
//
//	不再用自包含 JWT：JWT 一旦签发就无法在过期前作废，而权限被取消、
//	用户被停用时必须能立刻踢掉会话（RefreshPermissions 里就要用）。
//	随机 token + 库里查，换来的正是这个可撤销性。
func (h *AuthHandler) issueSession(userID int, username, role, source string, perms map[string]bool, portalToken string) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(buf)
	expires := time.Now().Add(time.Duration(h.SessionHours) * time.Hour)

	var permJSON interface{}
	if perms != nil {
		b, _ := json.Marshal(perms)
		permJSON = string(b)
	}
	// portal token 加密后随会话走：刷新权限时要拿它去运维平台换最新权限
	// （运维平台的 /api/my/permissions 认用户身份，光带 X-Operator 头是 401）
	var portalEnc interface{}
	if portalToken != "" && h.Cipher != nil {
		if enc, err := h.Cipher.Encrypt(portalToken); err == nil {
			portalEnc = enc
		} else {
			logx.Line("auth", fmt.Sprintf("WARN 加密 portal token 失败 user=%s: %v（该会话将无法刷新权限）", username, err))
		}
	}
	// 会话必须带上当前租户，否则所有走 store 层的接口都会因
	// "上下文中没有租户标识" 而 400 —— 用户看到的是一堆打不开的页面，
	// 而登录本身是成功的，很难联想到会话里少了一个字段。
	activeTenant := h.resolveTenant(userID, username)
	_, err := h.DB.Exec(
		`INSERT INTO auth_sessions (user_id, username, token_hash, permissions, auth_source, role, expires_at, portal_token_enc, active_tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, username, hashToken(token), permJSON, source, role, expires, portalEnc, activeTenant)
	if err != nil {
		return "", time.Time{}, err
	}
	// 顺手清理过期会话，不另起定时任务（登录频率低，成本可忽略）
	h.DB.Exec(`DELETE FROM auth_sessions WHERE expires_at < NOW()`)
	return token, expires, nil
}

// resolveTenant 决定这次登录归属哪个租户。
//
// # 规则
//
//	用户在 user_tenants 里有归属      → 用它（多个时取 id 最小的那个，
//	                                    切换由「租户切换」入口负责）
//	没有归属                          → 回落到默认租户，并打 WARN
//
// # 为什么回落而不是拒绝登录
//
// 一期只开一个租户，绝大多数用户不会有 user_tenants 记录。
// 这时拒绝登录等于系统不可用。
//
//	⚠️ 但回落**必须打日志**：二期打开多租户后，"没有归属"就不再是常态，
//	而是一个配置遗漏。静默回落会让这类遗漏一直藏着，
//	直到某个用户看到了不该看的租户的数据。
func (h *AuthHandler) resolveTenant(userID int, username string) int64 {
	var tid int64
	err := h.DB.QueryRow(
		`SELECT tenant_id FROM user_tenants WHERE user_id = ? ORDER BY tenant_id LIMIT 1`, userID).Scan(&tid)
	if err == nil && tid > 0 {
		return tid
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		logx.J("auth", "resolve_tenant_fail", map[string]any{
			"user": username, "err": err.Error(),
			"warn": "查租户归属失败，回落到默认租户",
		})
	}
	// 默认租户：取 status=active 里 id 最小的那个（迁移里种的是 id=1）
	var def int64
	if e := h.DB.QueryRow(
		`SELECT id FROM tenants WHERE status='active' AND deleted_at IS NULL ORDER BY id LIMIT 1`).Scan(&def); e != nil || def == 0 {
		logx.J("auth", "no_tenant_available", map[string]any{
			"user": username, "warn": "库里没有可用租户，该会话将无法访问任何业务接口",
		})
		return 0
	}
	// ⚠️ 这条回落是**会话能建、请求全 403** 的根源：
	// 这里回落到默认租户照常发会话，而请求时的租户中间件按 user_tenants
	// 严格校验归属，查不到就 403。两边判据不一致。
	// 所以日志要直接写出后果，别只说"已回落"——只说回落的话，
	// 排障的人看到一条 info 会以为它是正常路径而跳过去。
	logx.J("auth", "tenant_fallback_default", map[string]any{
		"user": username, "tenant_id": def,
		"warn": "该用户在 user_tenants 里没有归属，会话已按默认租户签发，" +
			"但业务接口会因归属校验不过而 403；本地账号由启动回填补上，portal 用户需对接方补",
	})
	return def
}

// Middleware 校验会话并把权限快照放进 context。
func (h *AuthHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if raw == "" {
			// 结构化错误：界面按 message_key 翻译，中文那句留给 MCP / 直接调 API 的人
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": httpx.CodeUnauthorized, "message_key": "error.unauthorized",
				"error": "未登录"})
			return
		}

		// 进程内自调用（MCP 的 internalGet 回调 127.0.0.1 上的自己）。
		// 这类请求不写会话表，中间件只认会话表的话，MCP 的 36 个工具
		// 会全部返回"登录已失效"——见 internal_auth.go 的说明。
		if isInternalCall(raw) {
			setInternalIdentity(c, h.DB)
			c.Next()
			return
		}

		th := hashToken(raw)

		var userID int
		var username, authSource, role string
		var permJSON sql.NullString
		err := h.DB.QueryRow(
			`SELECT user_id, username, IFNULL(auth_source,'local'), IFNULL(role,''), permissions
			 FROM auth_sessions WHERE token_hash=? AND expires_at > NOW()`, th).
			Scan(&userID, &username, &authSource, &role, &permJSON)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code": httpx.CodeUnauthorized, "message_key": "error.sessionExpired",
				"error": "登录已失效"})
			return
		}

		perms := map[string]bool{}
		if permJSON.Valid && permJSON.String != "" {
			_ = json.Unmarshal([]byte(permJSON.String), &perms)
		}
		c.Set(ctxUsername, username)
		c.Set(ctxUserID, userID)
		c.Set(ctxAuthSource, authSource)
		c.Set(ctxPerms, perms)
		c.Set(ctxTokenHash, th)
		// 谁不受权限码约束——**只认会话里的 role**，不再看 auth_source。
		//
		//	原来写的是 `authSource == "local"`，即"本地账号一律全放行"。
		//	那样一来本地账号就只有全权限一档，开不出只读号（迁移 088 的由来）。
		//	现在本地账号也带角色：cmdb_admin / 空（老账号）→ role 落成 "admin"，
		//	其余角色按权限码查表。运维平台超管同样落 "admin"，两条路合一。
		//
		//	⚠️ 别改回按 auth_source 判断：那会让所有本地角色瞬间提权成管理员，
		//	而且不会有任何报错——只读号能删域名，你也看不出来。
		//
		//	role 为空只可能是**升级前建立的老会话**（新会话一定写值）。
		//	那种会话按老语义处理，否则一次发版就把当时在线的人全踢成无权限。
		isAdmin := role == "admin" || (role == "" && authSource == "local")
		c.Set(ctxIsAdmin, isAdmin)
		c.Set(ctxRole, role)
		c.Next()
	}
}

// ctxWithUser 造一个带 username 的上下文副本，供还没有登录态时写审计（登录成功/失败）
func (h *AuthHandler) ctxWithUser(c *gin.Context, username string) *gin.Context {
	c.Set(ctxUsername, username)
	return c
}

// ---- context 读取 ----

func permsFromCtx(c *gin.Context) map[string]bool {
	if v, ok := c.Get(ctxPerms); ok {
		if m, ok := v.(map[string]bool); ok {
			return m
		}
	}
	return map[string]bool{}
}

func tokenHashFromCtx(c *gin.Context) string {
	if v, ok := c.Get(ctxTokenHash); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// IsAdmin 当前请求是否来自本地兜底账号（不受权限码约束）
func IsAdmin(c *gin.Context) bool {
	if v, ok := c.Get(ctxIsAdmin); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// UserIDFromCtx 当前登录用户的 id；取不到返回 0。
//
//	自助改密这类"只能操作自己"的接口必须用它，**不能从请求体里取 id**——
//	那样任何人都能改别人的密码。
func UserIDFromCtx(c *gin.Context) int {
	if v, ok := c.Get(ctxUserID); ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	return 0
}

// UsernameFromCtx 当前操作人，取不到时返回 "-"（审计里能一眼看出是异常路径）
func UsernameFromCtx(c *gin.Context) string {
	if v, ok := c.Get(ctxUsername); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "-"
}

// knownWeakPasswords 已知的默认/弱口令。
//
// ⚠️ 这张表只用来**自检**，不用来阻止用户设置 —— 设置那边的规则是长度下限
// （minPasswordLen），两件事不要混：一个是"你现在是不是危险的"，
// 一个是"你能不能这么设"。
var knownWeakPasswords = []string{
	"admin123", "admin", "123456", "password", "Admin@123", "admin@123",
	"12345678", "administrator", "root", "cmdb123",
}

// adminWeakPassword 本地 admin 是否仍在用已知弱口令。
//
// 进程内标志：每次启动检测一次，由 /api/users 带给界面。
// ⚠️ 不落库 —— 落库要迁移，而这个事实随时可能变（改完口令就不成立了），
// 重启一次即可刷新。
var adminWeakPassword atomic.Bool

// AdminWeakPasswordDetected 给 handler 读。
func AdminWeakPasswordDetected() bool { return adminWeakPassword.Load() }

// checkAdminWeakPassword 用 bcrypt 比对已知弱口令。
//
// 🔴 这是**产品自己检查自己**，不是攻击：口令哈希本来就在自己的库里，
//
//	不涉及任何登录尝试，也不会锁账号。
//	不做这件事的后果是：产品对"我的后门是否公开"这个问题**没有任何答案**
//	（实测 grep：代码里没有 must_change_password / weak_password 任何逻辑）。
func (h *AuthHandler) checkAdminWeakPassword() {
	var hash string
	if err := h.DB.QueryRow(`SELECT password_hash FROM users WHERE username='admin'`).Scan(&hash); err != nil {
		return
	}
	if hash == "" {
		return
	}
	for _, known := range knownWeakPasswords {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(known)) == nil {
			adminWeakPassword.Store(true)
			logx.Line("auth", "WARN 🔴 本地 admin 账号仍在使用已知的默认/弱口令。"+
				"这个账号权限校验全放行——请立即更换，并踢掉现有会话。"+
				"（界面上的用户管理页会一并提示）")
			return
		}
	}
	adminWeakPassword.Store(false)
}

// isKnownWeakPassword 这个口令是不是已知的默认/弱口令。
//
// ⚠️ 与 checkAdminWeakPassword 用的是同一张表，但用途相反：
//
//	这里是**事前**拦住（还没设进去），那里是**事后**发现（已经在用了）。
//	两件都要做 —— 只做事前，存量账号永远查不出来；
//	只做事后，等于允许用户再种一个同样的雷。
func isKnownWeakPassword(pw string) bool {
	for _, w := range knownWeakPasswords {
		if strings.EqualFold(pw, w) {
			return true
		}
	}
	return false
}
