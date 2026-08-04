package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
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

// EnsureAdmin 首启 seed 一个本地 admin（密码取 ADMIN_PASSWORD，默认 admin123）。
//
//	这个账号是运维平台挂掉时的兜底通道，权限校验对它全放行，
//	所以它的登录（成功与失败）都要留审计。
func (h *AuthHandler) EnsureAdmin() {
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username='admin'`).Scan(&n); err != nil {
		logx.Line("auth", fmt.Sprintf("EnsureAdmin check: %v", err))
		return
	}
	if n > 0 {
		return
	}
	pw := os.Getenv("ADMIN_PASSWORD")
	if pw == "" {
		pw = "admin123"
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if _, err := h.DB.Exec(`INSERT INTO users (username, password_hash, display_name, is_admin, auth_source)
		VALUES ('admin', ?, '管理员', 1, 'local')`, string(hash)); err != nil {
		logx.Line("auth", fmt.Sprintf("EnsureAdmin seed: %v", err))
		return
	}
	logx.Line("auth", "seeded admin user (password from ADMIN_PASSWORD or default admin123)")
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
	var hash, display, authSource string
	err := h.DB.QueryRow(`SELECT id, password_hash, display_name, IFNULL(auth_source,'local')
		FROM users WHERE username=?`, req.Username).Scan(&id, &hash, &display, &authSource)
	// portal 用户没有本地密码（password_hash 为空），不能走密码登录——
	// 否则空密码 hash 一旦被 bcrypt 意外匹配就是个后门。
	if err != nil || authSource != "local" || hash == "" ||
		bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		logx.Line("auth", fmt.Sprintf("WARN 本地登录失败 user=%s ip=%s", req.Username, c.ClientIP()))
		WriteAuditAs(h.DB, h.ctxWithUser(c, req.Username), "auth.login.failed", "用户名或密码错误")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 本地账号权限表留空：Middleware 见到 auth_source=local 直接全放行
	token, expires, err := h.issueSession(id, req.Username, "admin", "local", nil, "")
	if err != nil {
		logx.Line("auth", fmt.Sprintf("issue session %s: %v", req.Username, err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}
	h.DB.Exec(`UPDATE users SET last_login_at=NOW() WHERE id=?`, id)
	WriteAuditAs(h.DB, h.ctxWithUser(c, req.Username), "auth.login.success", "auth_source=local")
	c.JSON(http.StatusOK, gin.H{
		"token": token, "username": req.Username, "display_name": display,
		"auth_source": "local", "role": "admin", "expires_at": expires,
		// 本地账号不受权限约束，给个空表让前端走 isAdmin 分支
		"permissions": map[string]bool{},
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
	_, err := h.DB.Exec(
		`INSERT INTO auth_sessions (user_id, username, token_hash, permissions, auth_source, role, expires_at, portal_token_enc)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, username, hashToken(token), permJSON, source, role, expires, portalEnc)
	if err != nil {
		return "", time.Time{}, err
	}
	// 顺手清理过期会话，不另起定时任务（登录频率低，成本可忽略）
	h.DB.Exec(`DELETE FROM auth_sessions WHERE expires_at < NOW()`)
	return token, expires, nil
}

// Middleware 校验会话并把权限快照放进 context。
func (h *AuthHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
			return
		}

		// 进程内自调用（MCP 的 internalGet 回调 127.0.0.1 上的自己）。
		// 这类请求不写会话表，中间件只认会话表的话，MCP 的 36 个工具
		// 会全部返回"登录已失效"——见 internal_auth.go 的说明。
		if isInternalCall(raw) {
			setInternalIdentity(c)
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
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效"})
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
		// 谁不受权限码约束：
		//   local —— 运维平台不可用时的兜底通道
		//   运维平台超管 —— 它在运维平台侧就是全通的，到了 CMDB 反而被自己的
		//     权限表挡住说不过去。**漏了这一条**，SSO 超管会被锁死在无权访问页。
		c.Set(ctxIsAdmin, authSource == "local" || role == "admin")
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

// UsernameFromCtx 当前操作人，取不到时返回 "-"（审计里能一眼看出是异常路径）
func UsernameFromCtx(c *gin.Context) string {
	if v, ok := c.Get(ctxUsername); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "-"
}
