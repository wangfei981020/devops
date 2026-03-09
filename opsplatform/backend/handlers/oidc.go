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

// findOrCreateOIDCUser 查找或创建OIDC用户
// 只通过 OIDC sub 匹配用户，不匹配用户名/邮箱，避免误绑定现有用户
func findOrCreateOIDCUser(config *OIDCConfig, userInfo *UserInfo) (*models.User, error) {
	// 确定用户名：优先 preferred_username，其次 email，最后 sub
	username := userInfo.PreferredUsername
	if username == "" {
		username = userInfo.Email
	}
	if username == "" {
		username = userInfo.Sub
	}

	log.Printf("[OIDC] 查找用户，sub=%s, username=%s, email=%s", userInfo.Sub, username, userInfo.Email)

	// 只通过 OIDC sub 查找用户
	var user models.User
	err := database.DB.QueryRow(`
		SELECT id, username, display_name, email, role, status, mfa_enabled, COALESCE(mfa_secret, '') 
		FROM users WHERE oidc_sub = ?
	`, userInfo.Sub).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role, &user.Status, &user.MFAEnabled, &user.MFASecret)

	if err == nil {
		log.Printf("[OIDC] 找到已绑定用户: %s (ID: %s)", user.Username, user.ID)
		return &user, nil
	}

	// 用户不存在，检查是否允许自动注册
	if !config.AutoRegister {
		log.Printf("[OIDC] 用户不存在且未启用自动注册: sub=%s", userInfo.Sub)
		return nil, fmt.Errorf("用户不存在且未启用自动注册")
	}

	// 检查用户名是否已存在（避免冲突）
	var existingCount int
	database.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&existingCount)
	if existingCount > 0 {
		// 用户名已存在，添加后缀
		username = fmt.Sprintf("%s_sso_%s", username, userInfo.Sub[:8])
		log.Printf("[OIDC] 用户名已存在，使用新用户名: %s", username)
	}

	// 创建新用户
	newID := uuid.New().String()
	displayName := userInfo.Name
	if displayName == "" {
		displayName = username
	}

	log.Printf("[OIDC] 创建新用户: username=%s, role=%s", username, config.DefaultRole)

	_, err = database.DB.Exec(`
		INSERT INTO users (id, username, display_name, email, password, role, status, oidc_sub, auth_source, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, 'active', ?, 'sso', NOW(), NOW())
	`, newID, username, displayName, userInfo.Email, config.DefaultRole, userInfo.Sub)

	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %v", err)
	}

	// 查找默认角色的 ID 并插入 user_roles 关联
	var roleID string
	roleErr := database.DB.QueryRow(`SELECT id FROM roles WHERE code = ? OR name = ? LIMIT 1`, config.DefaultRole, config.DefaultRole).Scan(&roleID)
	if roleErr == nil && roleID != "" {
		userRoleID := fmt.Sprintf("ur_oidc_%d", time.Now().UnixNano())
		_, insertErr := database.DB.Exec(`INSERT INTO user_roles (id, user_id, role_id) VALUES (?, ?, ?)`, userRoleID, newID, roleID)
		if insertErr != nil {
			log.Printf("[OIDC] 插入用户角色关联失败: %v", insertErr)
		} else {
			log.Printf("[OIDC] 用户角色关联成功: user_id=%s, role_id=%s", newID, roleID)
		}
	} else {
		log.Printf("[OIDC] 未找到默认角色 '%s': %v", config.DefaultRole, roleErr)
	}

	log.Printf("[OIDC] 新用户创建成功: ID=%s, username=%s", newID, username)

	return &models.User{
		ID:          newID,
		Username:    username,
		DisplayName: displayName,
		Email:       userInfo.Email,
		Role:        config.DefaultRole,
		Status:      "active",
	}, nil
}
