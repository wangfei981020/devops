package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/logx"
)

// 与运维平台（opsplatform）对接的单点登录。
//
//	用户在运维平台点「CMDB」→ 带一次性 token 跳过来 → 这里拿 token 找运维平台换身份，
//	再把该用户的权限码一次性拉回来存进 auth_sessions，后续请求直接读会话里的快照。
//
//	为什么缓存而不是每次回头问：一是每个请求多一次跨服务调用太慢，
//	二是运维平台抖一下 CMDB 就整个不可用。权限变更由前端主动调
//	/api/refresh-permissions 刷新，管理员改完角色用户不必重新登录。
//
//	🔒 三道校验缺一不可，且任意一道"问不到答案"都拒登（fail-closed）：
//	  1. token 换得到身份吗
//	  2. 有 menu:cmdb 这个菜单权限吗
//	  3. 所在角色被授权访问 cmdb 这个应用吗（role_external_apps）
//	发布中心早期在第 3 步 fail-open 过——运维平台一挂，人人都是 admin。

const portalHTTPTimeout = 8 * time.Second

// RegisterPortal 注册免登录中间件的 portal 入口（必须在 Middleware 之前挂）
func (h *AuthHandler) RegisterPortal(r *gin.RouterGroup) {
	r.POST("/portal-auth", h.PortalAuth)
}

// PortalAuth POST /api/portal-auth  {"token": "<运维平台下发的 token>"}
func (h *AuthHandler) PortalAuth(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 token"})
		return
	}
	if h.PortalURL == "" {
		logx.Line("portal_auth", "PORTAL_API_URL 未配置，portal 登录不可用")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "未开启单点登录"})
		return
	}

	// ① 拿 token 找运维平台换身份
	username, displayName, role, ok := portalWhoAmI(h.PortalURL, req.Token)
	if !ok {
		WriteAuditAs(h.DB, h.ctxWithUser(c, "anonymous"), "auth.portal.failed", "token 无法换取身份")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登录凭证无效或已过期"})
		return
	}

	// ② 拉权限码 + ③ 应用准入。admin 跳过（运维平台超管天然全通）
	perms := map[string]bool{}
	if role != "admin" {
		p, ok := portalFetchPerms(h.PortalURL, req.Token, username)
		if !ok {
			// 拉不到 ≠ 没权限，但两者都不能放行：问不到答案就当没答案
			WriteAuditAs(h.DB, h.ctxWithUser(c, username), "auth.portal.denied", "权限服务不可达")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "权限服务暂不可用，请稍后重试"})
			return
		}
		perms = p
		if !perms["menu:cmdb"] {
			WriteAuditAs(h.DB, h.ctxWithUser(c, username), "auth.portal.denied", "缺少 menu:cmdb")
			c.JSON(http.StatusForbidden, gin.H{"error": "您没有 CMDB 的访问权限，请联系管理员"})
			return
		}
		allowed, ok := portalCanAccessApp(h.PortalURL, req.Token, "cmdb")
		if !ok {
			WriteAuditAs(h.DB, h.ctxWithUser(c, username), "auth.portal.denied", "应用准入服务不可达")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "权限服务暂不可用，请稍后重试"})
			return
		}
		if !allowed {
			WriteAuditAs(h.DB, h.ctxWithUser(c, username), "auth.portal.denied", "角色未授权访问 cmdb 应用")
			c.JSON(http.StatusForbidden, gin.H{"error": "您所在的角色未被授予访问 CMDB 的权限"})
			return
		}
	}

	userID, err := h.upsertPortalUser(username, displayName, role)
	if err != nil {
		logx.Line("portal_auth", fmt.Sprintf("upsert user %s: %v", username, err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败"})
		return
	}

	token, expires, err := h.issueSession(userID, username, role, "portal", perms, req.Token)
	if err != nil {
		logx.Line("portal_auth", fmt.Sprintf("issue session %s: %v", username, err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}

	logx.Line("portal_auth", fmt.Sprintf("login ok user=%s role=%s perms=%d", username, role, len(perms)))
	// 登录成功这条审计也该记成 portal——此刻会话刚建好，
	// 中间件还没跑过，ctx 里没有来源，不设就会落成默认的 local
	c.Set(ctxAuthSource, "portal")
	WriteAuditAs(h.DB, h.ctxWithUser(c, username), "auth.portal.success", fmt.Sprintf("role=%s", role))
	c.JSON(http.StatusOK, gin.H{
		"token":        token,
		"username":     username,
		"display_name": displayName,
		"role":         role,
		"auth_source":  "portal",
		"permissions":  perms,
		"expires_at":   expires,
	})
}

// RefreshPermissions GET /api/refresh-permissions
//
//	管理员在运维平台改完角色，用户点一下就生效，不必退出重登。
//	本地账号（portal 之外）没有可刷的权限，原样返回空表示"不受权限约束"。
func (h *AuthHandler) RefreshPermissions(c *gin.Context) {
	src, _ := c.Get(ctxAuthSource)
	if src != "portal" || h.PortalURL == "" {
		c.JSON(http.StatusOK, gin.H{"permissions": map[string]bool{}, "auth_source": src})
		return
	}
	username, _ := c.Get(ctxUsername)
	uname, _ := username.(string)

	// 用会话里存的 portal token 回头问运维平台要最新权限。
	// 注意不能走"内网免鉴权"路径：运维平台根本没有 /my/permissions 这个路由
	// （它挂在 /api 下），而 /api/my/permissions 认的是用户身份，
	// 光带 X-Operator 头会 401——那样刷新永远拿不到新权限，
	// 管理员撤了权限用户这边毫无感知，直到会话自然过期。
	ptoken := h.portalTokenOfSession(c)
	if ptoken == "" {
		logx.Line("portal_auth", fmt.Sprintf("WARN 会话内无 portal token user=%s，无法刷新权限", uname))
		c.JSON(http.StatusOK, gin.H{"permissions": permsFromCtx(c), "stale": true})
		return
	}
	perms, ok := portalFetchPerms(h.PortalURL, ptoken, uname)
	if !ok {
		// 拉不到就保持旧快照——刷新失败（网络抖动/portal token 过期）不该把人踢出去
		logx.Line("portal_auth", fmt.Sprintf("WARN refresh perms failed user=%s，沿用会话内旧快照", uname))
		c.JSON(http.StatusOK, gin.H{"permissions": permsFromCtx(c), "stale": true})
		return
	}
	// 刷新后失去 menu:cmdb = 已被取消 CMDB 访问权，直接失效会话
	if !perms["menu:cmdb"] {
		h.DB.Exec(`DELETE FROM auth_sessions WHERE token_hash=?`, tokenHashFromCtx(c))
		logx.Line("portal_auth", fmt.Sprintf("user=%s 已失去 menu:cmdb，会话作废", uname))
		c.JSON(http.StatusForbidden, gin.H{"error": "您的 CMDB 访问权限已被取消"})
		return
	}
	b, _ := json.Marshal(perms)
	h.DB.Exec(`UPDATE auth_sessions SET permissions=? WHERE token_hash=?`, string(b), tokenHashFromCtx(c))
	c.JSON(http.StatusOK, gin.H{"permissions": perms})
}

// portalTokenOfSession 取出并解密当前会话保存的 portal token。
// 拿不到就返回空串，调用方据此走"沿用旧快照"分支，而不是把用户踢下线。
func (h *AuthHandler) portalTokenOfSession(c *gin.Context) string {
	if h.Cipher == nil {
		return ""
	}
	th := tokenHashFromCtx(c)
	if th == "" {
		return ""
	}
	var enc sql.NullString
	if err := h.DB.QueryRow(`SELECT portal_token_enc FROM auth_sessions WHERE token_hash=?`, th).Scan(&enc); err != nil {
		return ""
	}
	if !enc.Valid || enc.String == "" {
		return ""
	}
	plain, err := h.Cipher.Decrypt(enc.String)
	if err != nil {
		logx.Line("portal_auth", fmt.Sprintf("WARN 解密 portal token 失败: %v", err))
		return ""
	}
	return plain
}

// upsertPortalUser portal 用户首登自动建号，之后每次同步显示名
func (h *AuthHandler) upsertPortalUser(username, displayName, role string) (int, error) {
	isAdmin := 0
	if role == "admin" {
		isAdmin = 1
	}
	var id int
	err := h.DB.QueryRow(`SELECT id FROM users WHERE username=? AND auth_source='portal'`, username).Scan(&id)
	if err == sql.ErrNoRows {
		res, err := h.DB.Exec(
			`INSERT INTO users (username, password_hash, display_name, is_admin, auth_source, last_login_at)
			 VALUES (?, '', ?, ?, 'portal', NOW())`, username, displayName, isAdmin)
		if err != nil {
			return 0, err
		}
		n, _ := res.LastInsertId()
		return int(n), nil
	}
	if err != nil {
		return 0, err
	}
	h.DB.Exec(`UPDATE users SET display_name=?, is_admin=?, last_login_at=NOW() WHERE id=?`,
		displayName, isAdmin, id)
	return id, nil
}

// ---- 运维平台调用 ----

// portalWhoAmI 拿 token 换身份。ok=false 表示"没换到"（token 无效或服务不可达），一律拒登。
func portalWhoAmI(portalURL, token string) (username, displayName, role string, ok bool) {
	raw, ok := portalGet(portalURL, "/api/users/me", token, "")
	if !ok {
		return "", "", "", false
	}
	// 兼容 {code,data:{...}} 和裸对象两种包装
	type identity struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	var wrapped struct {
		Data identity `json:"data"`
	}
	var direct identity
	var id identity
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Data.Username != "" {
		id = wrapped.Data
	} else if err := json.Unmarshal(raw, &direct); err == nil && direct.Username != "" {
		id = direct
	} else {
		logx.Line("portal_auth", fmt.Sprintf("WARN 无法解析身份响应: %.200s", string(raw)))
		return "", "", "", false
	}
	if id.DisplayName == "" {
		id.DisplayName = id.Username
	}
	r := id.Role
	if r == "admin" || r == "super_admin" || r == "超级管理员" {
		r = "admin"
	} else if r == "" {
		r = "user"
	}
	return id.Username, id.DisplayName, r, true
}

// portalFetchPerms 用用户 token 拉权限码；ok=false 表示没拉到（调用方 fail-closed）
func portalFetchPerms(portalURL, token, username string) (map[string]bool, bool) {
	raw, ok := portalGet(portalURL, "/api/my/permissions", token, username)
	if !ok {
		return nil, false
	}
	return parseCMDBPerms(raw), true
}

// portalCanAccessApp 角色是否被授权访问某个外部应用（role_external_apps）
//
//	返回 (allowed, ok)：ok=false 表示服务不可达，调用方必须 fail-closed，
//	绝不能把"问不到"当成"允许"。
func portalCanAccessApp(portalURL, token, appKey string) (allowed, ok bool) {
	raw, ok := portalGet(portalURL, "/api/external-apps/my", token, "")
	if !ok {
		return false, false
	}
	type appItem struct {
		AppKey string `json:"app_key"`
	}
	var wrapped struct {
		Data []appItem `json:"data"`
	}
	var direct []appItem
	var list []appItem
	if err := json.Unmarshal(raw, &wrapped); err == nil && len(wrapped.Data) > 0 {
		list = wrapped.Data
	} else if err := json.Unmarshal(raw, &direct); err == nil {
		list = direct
	}
	if len(list) == 0 {
		logx.Line("portal_auth", fmt.Sprintf("WARN external-apps/my 返回 0 条 (raw=%.200s)", string(raw)))
	}
	for _, a := range list {
		if a.AppKey == appKey {
			return true, true
		}
	}
	return false, true
}

// portalGet 统一的 GET，失败只返回 ok=false（调用方据此 fail-closed）
func portalGet(portalURL, path, token, operator string) ([]byte, bool) {
	u := strings.TrimRight(portalURL, "/") + path
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, false
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if operator != "" {
		req.Header.Set("X-Operator", operator)
	}
	resp, err := (&http.Client{Timeout: portalHTTPTimeout}).Do(req)
	if err != nil {
		logx.Line("portal_auth", fmt.Sprintf("WARN GET %s 失败: %v", path, err))
		return nil, false
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logx.Line("portal_auth", fmt.Sprintf("WARN GET %s HTTP %d: %.200s", path, resp.StatusCode, string(raw)))
		return nil, false
	}
	return raw, true
}

// parseCMDBPerms 只留 CMDB 自己的权限码。
//
//	运维平台返回的是全平台权限（含 jira:/alert:/deploy_center: 等几百条），
//	全存进会话既浪费又容易误判——比如某天有人加了 `menu:cmdb_like_something`
//	这种其他系统的码，前缀匹配会误伤。这里按精确前缀过滤。
func parseCMDBPerms(raw []byte) map[string]bool {
	var result struct {
		Permissions map[string]bool `json:"permissions"`
	}
	var wrapped struct {
		Data struct {
			Permissions map[string]bool `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Data.Permissions != nil {
		result.Permissions = wrapped.Data.Permissions
	} else {
		_ = json.Unmarshal(raw, &result)
	}

	out := make(map[string]bool)
	for code, granted := range result.Permissions {
		if !granted {
			continue
		}
		if code == "menu:cmdb" ||
			strings.HasPrefix(code, "menu:cmdb_") ||
			strings.HasPrefix(code, "cmdb:") {
			out[code] = true
		}
	}
	return out
}

// hashToken token 只存 sha256，库被读走也冒充不了登录
func hashToken(t string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(t)))
}
