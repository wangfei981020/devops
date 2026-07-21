package handlers

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"opsplatform/database"
	"opsplatform/models"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OIDCConfig OIDC配置
type OIDCConfig struct {
	Enabled       bool   `json:"enabled"`
	ProviderName  string `json:"provider_name"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	IssuerURL     string `json:"issuer_url"`
	AuthURL       string `json:"auth_url"`
	TokenURL      string `json:"token_url"`
	UserInfoURL   string `json:"userinfo_url"`
	RedirectURI   string `json:"redirect_uri"`
	Scopes        string `json:"scopes"`
	AutoRegister  bool   `json:"auto_register"`
	DefaultRole   string `json:"default_role"`
}

// OIDCDiscovery OIDC发现文档
type OIDCDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// 缓存发现的配置
var discoveryCache *OIDCDiscovery
var discoveryCacheTime time.Time

// GetOIDCConfig 获取OIDC配置
func GetOIDCConfig() (*OIDCConfig, error) {
	config := &OIDCConfig{
		Scopes:       "openid profile email",
		AutoRegister: true,
		DefaultRole:  "user",
	}

	rows, err := database.DB.Query(`SELECT setting_key, setting_value FROM system_settings WHERE setting_key LIKE 'oidc_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		switch key {
		case "oidc_enabled":
			config.Enabled = value == "true"
		case "oidc_provider_name":
			config.ProviderName = value
		case "oidc_client_id":
			config.ClientID = value
		case "oidc_client_secret":
			config.ClientSecret = value
		case "oidc_issuer_url":
			config.IssuerURL = value
		case "oidc_auth_url":
			config.AuthURL = value
		case "oidc_token_url":
			config.TokenURL = value
		case "oidc_userinfo_url":
			config.UserInfoURL = value
		case "oidc_redirect_uri":
			config.RedirectURI = value
		case "oidc_scopes":
			if value != "" {
				config.Scopes = value
			}
		case "oidc_auto_register":
			config.AutoRegister = value == "true"
		case "oidc_default_role":
			if value != "" {
				config.DefaultRole = value
			}
		}
	}

	return config, nil
}

// HandleGetOIDCConfig 获取OIDC配置（公开接口，不返回敏感信息）
func HandleGetOIDCConfig(w http.ResponseWriter, r *http.Request) {
	config, err := GetOIDCConfig()
	if err != nil {
		sendError(w, "获取OIDC配置失败", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":       config.Enabled,
		"provider_name": config.ProviderName,
	})
}

// HandleGetOIDCConfigAdmin 获取完整OIDC配置（管理员接口）
func HandleGetOIDCConfigAdmin(w http.ResponseWriter, r *http.Request) {
	log.Printf("[OIDC Admin] 获取配置请求")
	config, err := GetOIDCConfig()
	if err != nil {
		log.Printf("[OIDC Admin] 获取配置失败: %v", err)
		sendError(w, "获取OIDC配置失败", http.StatusInternalServerError)
		return
	}

	log.Printf("[OIDC Admin] 配置: enabled=%v, provider=%s, client_id=%s, issuer_url=%s, redirect_uri=%s",
		config.Enabled, config.ProviderName, config.ClientID, config.IssuerURL, config.RedirectURI)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// HandleSaveOIDCConfig 保存OIDC配置
func HandleSaveOIDCConfig(w http.ResponseWriter, r *http.Request) {
	var config OIDCConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		sendError(w, "无效的请求数据", http.StatusBadRequest)
		return
	}

	settings := map[string]string{
		"oidc_enabled":       fmt.Sprintf("%v", config.Enabled),
		"oidc_provider_name": config.ProviderName,
		"oidc_client_id":     config.ClientID,
		"oidc_client_secret": config.ClientSecret,
		"oidc_issuer_url":    config.IssuerURL,
		"oidc_redirect_uri":  config.RedirectURI,
		"oidc_scopes":        config.Scopes,
		"oidc_auto_register": fmt.Sprintf("%v", config.AutoRegister),
		"oidc_default_role":  config.DefaultRole,
	}

	// 清除发现缓存，下次请求时重新获取
	discoveryCache = nil

	for key, value := range settings {
		_, err := database.DB.Exec(`
			INSERT INTO system_settings (setting_key, setting_value, updated_at) 
			VALUES (?, ?, NOW()) 
			ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_at = NOW()
		`, key, value)
		if err != nil {
			log.Printf("[OIDC] 保存配置失败 %s: %v", key, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "OIDC配置已保存"})
}

// discoverOIDCEndpoints 从 Issuer URL 发现 OIDC 端点
func discoverOIDCEndpoints(issuerURL string) (*OIDCDiscovery, error) {
	// 检查缓存（5分钟有效）
	if discoveryCache != nil && time.Since(discoveryCacheTime) < 5*time.Minute {
		return discoveryCache, nil
	}

	// 构造发现端点 URL
	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"
	log.Printf("[OIDC] 发现端点: %s", discoveryURL)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("请求发现端点失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("发现端点返回错误: %d - %s", resp.StatusCode, string(body))
	}

	var discovery OIDCDiscovery
	if err := json.Unmarshal(body, &discovery); err != nil {
		return nil, fmt.Errorf("解析发现文档失败: %v", err)
	}

	log.Printf("[OIDC] 发现成功: auth=%s, token=%s, userinfo=%s",
		discovery.AuthorizationEndpoint, discovery.TokenEndpoint, discovery.UserinfoEndpoint)

	// 更新缓存
	discoveryCache = &discovery
	discoveryCacheTime = time.Now()

	return &discovery, nil
}

// getOIDCEndpoints 获取 OIDC 端点（优先使用 Discovery，否则直接使用 Issuer URL 作为授权端点）
func getOIDCEndpoints(config *OIDCConfig) (authURL, tokenURL, userinfoURL string, err error) {
	// 如果配置了 Issuer URL，使用自动发现
	if config.IssuerURL != "" {
		discovery, err := discoverOIDCEndpoints(config.IssuerURL)
		if err != nil {
			log.Printf("[OIDC] 自动发现失败: %v，直接使用 Issuer URL 作为授权端点", err)
			// 自动发现失败，直接使用 Issuer URL 作为授权端点（某些非标准 OIDC 系统）
			baseURL := strings.TrimSuffix(config.IssuerURL, "/")
			authURL = baseURL // 直接使用，不加 /authorize
			tokenURL = baseURL + "/token"
			userinfoURL = baseURL + "/userinfo"
			log.Printf("[OIDC] 使用 Issuer URL 作为端点: auth=%s, token=%s, userinfo=%s", authURL, tokenURL, userinfoURL)
			return authURL, tokenURL, userinfoURL, nil
		}
		return discovery.AuthorizationEndpoint, discovery.TokenEndpoint, discovery.UserinfoEndpoint, nil
	}

	// 回退到手动配置
	if config.AuthURL == "" || config.TokenURL == "" || config.UserInfoURL == "" {
		return "", "", "", fmt.Errorf("OIDC 配置不完整：需要配置 Issuer URL 或完整的端点地址")
	}

	return config.AuthURL, config.TokenURL, config.UserInfoURL, nil
}

// generateState 生成 state（优先 Redis 一次性存储，后备 HMAC 签名）
func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	nonce := hex.EncodeToString(b)

	if database.RedisEnabled {
		// Redis: 一次性 state，防重放
		err := database.RedisSet("oidc_state:"+nonce, "1", 10*time.Minute)
		if err == nil {
			return nonce
		}
		log.Printf("[OIDC] Redis 存储 state 失败: %v，降级为 HMAC", err)
	}

	// HMAC 后备: nonce.timestamp.signature
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	payload := nonce + "." + ts
	return payload + "." + hmacSign(payload)
}

// validateState 验证 state（优先 Redis，后备 HMAC）
func validateState(state string) bool {
	// 尝试 Redis 验证（短 state = Redis 格式）
	if database.RedisEnabled && !strings.Contains(state, ".") {
		key := "oidc_state:" + state
		exists, err := database.RedisExists(key)
		if err == nil && exists {
			database.RedisDelete(key) // 一次性，用完即删
			return true
		}
		log.Printf("[OIDC] Redis state 无效或已使用: %s", state[:8])
		return false
	}

	// HMAC 后备验证
	parts := strings.SplitN(state, ".", 3)
	if len(parts) != 3 {
		log.Printf("[OIDC] state 格式错误")
		return false
	}
	payload := parts[0] + "." + parts[1]
	if hmacSign(payload) != parts[2] {
		log.Printf("[OIDC] state 签名无效")
		return false
	}
	ts, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix()-ts > 600 {
		log.Printf("[OIDC] state 已过期")
		return false
	}
	return true
}

func hmacSign(payload string) string {
	secret, _ := getJWTSecret()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// HandleOIDCLogin 发起OIDC登录
func HandleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	config, err := GetOIDCConfig()
	if err != nil || !config.Enabled {
		sendError(w, "OIDC未启用", http.StatusBadRequest)
		return
	}

	if config.ClientID == "" {
		sendError(w, "OIDC配置不完整：缺少 Client ID", http.StatusBadRequest)
		return
	}

	// 获取 OIDC 端点
	authURL, _, _, err := getOIDCEndpoints(config)
	if err != nil {
		log.Printf("[OIDC] 获取端点失败: %v", err)
		sendError(w, "OIDC配置不完整: "+err.Error(), http.StatusBadRequest)
		return
	}

	state := generateState()

	params := url.Values{}
	params.Set("client_id", config.ClientID)
	params.Set("redirect_uri", config.RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", config.Scopes)
	params.Set("state", state)

	redirectURL := authURL + "?" + params.Encode()

	log.Printf("[OIDC] 重定向到身份提供商: %s", authURL)

	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// getLoginRedirectURL 获取登录页重定向 URL
func getLoginRedirectURL(config *OIDCConfig, errorParam string) string {
	baseURL := ""
	if config != nil && config.RedirectURI != "" {
		if idx := strings.Index(config.RedirectURI, "/api/"); idx > 0 {
			baseURL = config.RedirectURI[:idx]
		}
	}
	return baseURL + "/login?error=" + errorParam
}

// HandleOIDCCallback OIDC回调处理
func HandleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errParam := r.URL.Query().Get("error")

	// 先获取配置，用于构建正确的重定向 URL
	config, _ := GetOIDCConfig()

	if errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("[OIDC] 认证错误: %s - %s", errParam, errDesc)
		http.Redirect(w, r, getLoginRedirectURL(config, "oidc_"+errParam), http.StatusFound)
		return
	}

	if !validateState(state) {
		log.Printf("[OIDC] 无效的state")
		http.Redirect(w, r, getLoginRedirectURL(config, "invalid_state"), http.StatusFound)
		return
	}

	if code == "" {
		log.Printf("[OIDC] 缺少授权码")
		http.Redirect(w, r, getLoginRedirectURL(config, "no_code"), http.StatusFound)
		return
	}

	if config == nil {
		log.Printf("[OIDC] 获取配置失败")
		http.Redirect(w, r, getLoginRedirectURL(nil, "config_error"), http.StatusFound)
		return
	}

	// 获取 OIDC 端点
	_, tokenURL, userinfoURL, err := getOIDCEndpoints(config)
	if err != nil {
		log.Printf("[OIDC] 获取端点失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "config_error"), http.StatusFound)
		return
	}

	// 交换token
	tokenResp, err := exchangeToken(config, code, tokenURL)
	if err != nil {
		log.Printf("[OIDC] 交换token失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "token_exchange_failed"), http.StatusFound)
		return
	}

	// 获取用户信息
	userInfo, err := getUserInfo(userinfoURL, tokenResp.AccessToken)
	if err != nil {
		log.Printf("[OIDC] 获取用户信息失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "userinfo_failed"), http.StatusFound)
		return
	}

	log.Printf("[OIDC] 用户信息: sub=%s, email=%s, name=%s", userInfo.Sub, userInfo.Email, userInfo.Name)

	// 查找或创建用户
	user, err := findOrCreateOIDCUser(config, userInfo)
	if err != nil {
		log.Printf("[OIDC] 用户处理失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "user_error"), http.StatusFound)
		return
	}

	if user.Status != "active" {
		log.Printf("[OIDC] 用户已禁用: %s", user.Username)
		http.Redirect(w, r, getLoginRedirectURL(config, "user_disabled"), http.StatusFound)
		return
	}

	// 每次登录都按 app_roles 重新同步角色 —— 老用户同样要走。
	// 原先命中 oidc_sub 就直接 return，IdP 侧改了组这边永远不生效
	syncAppRoles(user.ID, userInfo.AppRoles)

	// 生成JWT token
	token, expiresAt, err := GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		log.Printf("[OIDC] 生成token失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "token_error"), http.StatusFound)
		return
	}

	// 设置Cookie
	SetAuthCookie(w, token, expiresAt)

	log.Printf("[OIDC] 登录成功: %s", user.Username)

	// 从 redirect_uri 推断前端 URL
	frontendURL := "/welcome"
	if config.RedirectURI != "" {
		// redirect_uri 格式如: https://example.com/api/oidc/callback
		// 需要提取基础 URL
		if idx := strings.Index(config.RedirectURI, "/api/"); idx > 0 {
			frontendURL = config.RedirectURI[:idx] + "/welcome"
		}
	}

	// 添加 SSO 登录标识参数
	if strings.Contains(frontendURL, "?") {
		frontendURL += "&sso_login=1"
	} else {
		frontendURL += "?sso_login=1"
	}

	log.Printf("[OIDC] 重定向到: %s", frontendURL)

	// 重定向到首页
	http.Redirect(w, r, frontendURL, http.StatusFound)
}

// TokenResponse OIDC token响应
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

// UserInfo OIDC用户信息
type UserInfo struct {
	Sub               string `json:"sub"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Picture           string `json:"picture"`
	// AppRoles: IdP 下发的组/角色, 形如 ["infra_team"]
	// 需要在 SSO 配置的 Scopes 里申请 app_roles 才会下发, 否则为空
	AppRoles []string `json:"app_roles"`
}

// exchangeToken 交换授权码获取token
func exchangeToken(config *OIDCConfig, code string, tokenURL string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", config.RedirectURI)
	data.Set("client_id", config.ClientID)
	data.Set("client_secret", config.ClientSecret)

	log.Printf("[OIDC] 交换token: %s", tokenURL)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token交换失败: %s", string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

// getUserInfo 获取用户信息
func getUserInfo(userinfoURL string, accessToken string) (*UserInfo, error) {
	log.Printf("[OIDC] 获取用户信息: %s", userinfoURL)

	req, err := http.NewRequest("GET", userinfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取用户信息失败: %s", string(body))
	}

	var userInfo UserInfo
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// syncAppRoles 按 IdP 下发的 app_roles 同步用户角色
//
// 规则(与需求逐条对应):
//  1. 组名按原样匹配 roles.code，不做大小写转换
//  2. 本地没有该角色 -> 自动创建，只挂欢迎页权限，之后由管理员手动调整
//  3. 本地已有该角色 -> 直接复用，权限一律不动(已经配好的不能被覆盖)
//  4. 一个用户可有多个组，全部挂上
//  5. 只操作 source='sso' 的关联，管理员手动配的(source='manual')永不触碰
//  6. SSO 侧已移除的组：不删记录，只打 sso_removed_at 标记备注
func syncAppRoles(userID string, appRoles []string) {
	if len(appRoles) == 0 {
		// 没拿到组信息属于异常，必须留痕：可能是 Scopes 没配 app_roles，
		// 也可能是 IdP 侧没给这个用户分配组
		log.Printf("[OIDC][角色同步] WARN 用户 %s 的 app_roles 为空，本次不做任何角色变更"+
			"(检查 SSO 配置的 Scopes 是否包含 app_roles，以及 IdP 侧是否给该用户分配了组)", userID)
		return
	}

	log.Printf("[OIDC][角色同步] 用户 %s 的 app_roles = %v", userID, appRoles)

	syncedRoleIDs := make([]string, 0, len(appRoles))

	for _, groupName := range appRoles {
		if groupName == "" {
			continue
		}

		// 1) 按原样精确匹配已有角色
		var roleID string
		err := database.DB.QueryRow(`SELECT id FROM roles WHERE code = ? LIMIT 1`, groupName).Scan(&roleID)

		if err == nil && roleID != "" {
			log.Printf("[OIDC][角色同步] 组 %q 命中已有角色 (id=%s)，复用其权限配置", groupName, roleID)
		} else {
			// 2) 本地没有 -> 新建，只给欢迎页权限
			roleID = uuid.New().String()
			_, createErr := database.DB.Exec(`
				INSERT INTO roles (id, code, name, description, is_system, status, source, created_at, updated_at)
				VALUES (?, ?, ?, ?, 0, 'active', 'sso', NOW(), NOW())
			`, roleID, groupName, groupName, fmt.Sprintf("SSO 自动创建 (app_roles: %s)，默认仅有欢迎页权限，请按需配置", groupName))
			if createErr != nil {
				log.Printf("[OIDC][角色同步] ERROR 创建角色 %q 失败: %v", groupName, createErr)
				continue
			}

			// 只挂欢迎页权限
			_, permErr := database.DB.Exec(`
				INSERT IGNORE INTO role_permissions (id, role_id, permission_id)
				SELECT ?, ?, id FROM permissions WHERE code = 'menu:welcome'
			`, "rp_sso_"+roleID, roleID)
			if permErr != nil {
				log.Printf("[OIDC][角色同步] WARN 角色 %q 挂欢迎页权限失败: %v", groupName, permErr)
			}

			log.Printf("[OIDC][角色同步] 组 %q 本地不存在，已自动创建角色 (id=%s，仅欢迎页权限)", groupName, roleID)
		}

		syncedRoleIDs = append(syncedRoleIDs, roleID)

		// 3) 挂到用户身上。已存在则复活(清掉 sso_removed_at)，
		//    但绝不把 source=manual 的记录改成 sso —— 手动配的优先级更高
		urID := "ur_sso_" + uuid.New().String()
		if _, err := database.DB.Exec(`
			INSERT INTO user_roles (id, user_id, role_id, source, created_at)
			VALUES (?, ?, ?, 'sso', NOW())
			ON DUPLICATE KEY UPDATE sso_removed_at = NULL
		`, urID, userID, roleID); err != nil {
			log.Printf("[OIDC][角色同步] ERROR 用户 %s 关联角色 %q 失败: %v", userID, groupName, err)
		}
	}

	// 4) SSO 侧已移除的组：只标记不删除
	if len(syncedRoleIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(syncedRoleIDs)), ",")
		args := []interface{}{userID}
		for _, id := range syncedRoleIDs {
			args = append(args, id)
		}
		query := fmt.Sprintf(`
			UPDATE user_roles SET sso_removed_at = NOW()
			WHERE user_id = ? AND source = 'sso' AND sso_removed_at IS NULL
			  AND role_id NOT IN (%s)
		`, placeholders)
		if res, err := database.DB.Exec(query, args...); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				log.Printf("[OIDC][角色同步] 用户 %s 有 %d 个角色已在 SSO 侧移除，已标记 sso_removed_at (记录保留)", userID, n)
			}
		} else {
			log.Printf("[OIDC][角色同步] WARN 标记已移除角色失败: %v", err)
		}
	}
}

// findOrCreateOIDCUser 查找或创建OIDC用户
//
// 匹配顺序:
//  1. 按 oidc_sub 精确匹配(已绑定过的用户)
//  2. 按用户名匹配(username 列是 utf8mb4_unicode_ci 大小写不敏感,
//     所以新 SSO 发大写 Cesar 会自动命中老的小写 cesar) ——
//     命中且是 SSO 账号则复用并绑定新 sub, 角色/权限/发布记录全保留;
//     命中的是本地账号(auth_source=local)则不复用, 避免 SSO 顶掉本地管理员
//  3. 都没有 -> 用小写用户名新建
func findOrCreateOIDCUser(config *OIDCConfig, userInfo *UserInfo) (*models.User, error) {
	// 确定用户名：优先 preferred_username，其次 email，最后 sub。统一转小写存储
	username := userInfo.PreferredUsername
	if username == "" {
		username = userInfo.Email
	}
	if username == "" {
		username = userInfo.Sub
	}
	username = strings.ToLower(username)

	log.Printf("[OIDC] 查找用户，sub=%s, username=%s, email=%s", userInfo.Sub, username, userInfo.Email)

	// 1) 按 OIDC sub 查找已绑定用户
	var user models.User
	err := database.DB.QueryRow(`
		SELECT id, username, display_name, email, role, status, mfa_enabled, COALESCE(mfa_secret, '')
		FROM users WHERE oidc_sub = ?
	`, userInfo.Sub).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role, &user.Status, &user.MFAEnabled, &user.MFASecret)

	if err == nil {
		log.Printf("[OIDC] 找到已绑定用户: %s (ID: %s)", user.Username, user.ID)
		return &user, nil
	}

	// 2) sub 没命中：按用户名找已有账号(collation 保证大小写不敏感)。
	//    命中且是 SSO 账号 -> 复用并把新 sub 绑上去, 保留其全部角色/权限。
	//    这解决了「换 SSO 后 sub 变了, 同一个人被拆成两个账号」的问题
	var existingAuthSource string
	uErr := database.DB.QueryRow(`
		SELECT id, username, display_name, email, role, status, mfa_enabled, COALESCE(mfa_secret, ''), COALESCE(auth_source, 'local')
		FROM users WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role, &user.Status, &user.MFAEnabled, &user.MFASecret, &existingAuthSource)

	if uErr == nil {
		if existingAuthSource == "sso" {
			// 复用老 SSO 账号：把当前 sub 绑上去(换 SSO 后 sub 会变)
			if _, e := database.DB.Exec(`UPDATE users SET oidc_sub = ? WHERE id = ?`, userInfo.Sub, user.ID); e != nil {
				log.Printf("[OIDC] WARN 复用账号时更新 oidc_sub 失败: %v", e)
			}
			log.Printf("[OIDC] 按用户名复用已有 SSO 账号: %s (ID: %s)，已绑定新 sub", user.Username, user.ID)
			return &user, nil
		}
		// 命中的是本地账号：拒绝自动绑定, 避免 SSO 顶掉本地账号(如 admin)
		log.Printf("[OIDC] WARN 用户名 %q 已被本地账号占用(auth_source=%s)，拒绝 SSO 自动绑定", username, existingAuthSource)
		return nil, fmt.Errorf("用户名 %q 已被本地账号占用，无法通过 SSO 登录，请管理员处理", username)
	}

	// 3) 用户不存在，检查是否允许自动注册
	if !config.AutoRegister {
		log.Printf("[OIDC] 用户不存在且未启用自动注册: sub=%s", userInfo.Sub)
		return nil, fmt.Errorf("用户不存在且未启用自动注册")
	}

	// 创建新用户(用户名已转小写)
	newID := uuid.New().String()
	displayName := userInfo.Name
	if displayName == "" {
		displayName = username
	}

	log.Printf("[OIDC] 创建新用户: username=%s (角色完全由 app_roles 决定, 不再挂默认角色)", username)

	// role 单字段留空 —— 角色完全交给随后的 syncAppRoles 按 app_roles 处理。
	// 不再硬塞 oidc_default_role(IT): SSO 传了什么组就用什么, 没传就零角色(仅欢迎页)
	_, err = database.DB.Exec(`
		INSERT INTO users (id, username, display_name, email, password, role, status, oidc_sub, auth_source, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', '', 'active', ?, 'sso', NOW(), NOW())
	`, newID, username, displayName, userInfo.Email, userInfo.Sub)

	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %v", err)
	}

	log.Printf("[OIDC] 新用户创建成功: ID=%s, username=%s (角色待 syncAppRoles 同步)", newID, username)

	return &models.User{
		ID:          newID,
		Username:    username,
		DisplayName: displayName,
		Email:       userInfo.Email,
		Status:      "active",
	}, nil
}
