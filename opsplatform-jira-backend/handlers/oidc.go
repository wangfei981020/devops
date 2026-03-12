package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"opsplatform-jira-backend/database"
	"opsplatform-jira-backend/models"
)

// OIDCConfig OIDC配置
type OIDCConfig struct {
	Enabled      bool   `json:"enabled"`
	ProviderName string `json:"provider_name"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	IssuerURL    string `json:"issuer_url"`
	AuthURL      string `json:"auth_url"`
	TokenURL     string `json:"token_url"`
	UserInfoURL  string `json:"userinfo_url"`
	RedirectURI  string `json:"redirect_uri"`
	Scopes       string `json:"scopes"`
	AutoRegister bool   `json:"auto_register"`
	DefaultRole  string `json:"default_role"`
}

// OIDCDiscovery OIDC发现文档
type OIDCDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

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

// HandleGetOIDCConfig 获取OIDC配置（公开接口）
func HandleGetOIDCConfig(w http.ResponseWriter, r *http.Request) {
	config, err := GetOIDCConfig()
	if err != nil {
		respondInternalError(w, "获取OIDC配置失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled":       config.Enabled,
		"provider_name": config.ProviderName,
	})
}

// HandleGetOIDCConfigAdmin 获取完整OIDC配置（管理员）
func HandleGetOIDCConfigAdmin(w http.ResponseWriter, r *http.Request) {
	config, err := GetOIDCConfig()
	if err != nil {
		respondInternalError(w, "获取OIDC配置失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// HandleSaveOIDCConfig 保存OIDC配置
func HandleSaveOIDCConfig(w http.ResponseWriter, r *http.Request) {
	var config OIDCConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		respondBadRequest(w, "无效的请求数据")
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

	discoveryCache = nil

	for key, value := range settings {
		database.DB.Exec(`
			INSERT INTO system_settings (setting_key, setting_value, updated_at)
			VALUES (?, ?, NOW())
			ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_at = NOW()
		`, key, value)
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "更新OIDC配置", "settings", "oidc", "修改OIDC/SSO配置", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "OIDC配置已保存"})
}

// discoverOIDCEndpoints 发现OIDC端点
func discoverOIDCEndpoints(issuerURL string) (*OIDCDiscovery, error) {
	if discoveryCache != nil && time.Since(discoveryCacheTime) < 5*time.Minute {
		return discoveryCache, nil
	}

	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"
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

	discoveryCache = &discovery
	discoveryCacheTime = time.Now()
	return &discovery, nil
}

func getOIDCEndpoints(config *OIDCConfig) (authURL, tokenURL, userinfoURL string, err error) {
	if config.IssuerURL != "" {
		discovery, err := discoverOIDCEndpoints(config.IssuerURL)
		if err != nil {
			baseURL := strings.TrimSuffix(config.IssuerURL, "/")
			return baseURL, baseURL + "/token", baseURL + "/userinfo", nil
		}
		return discovery.AuthorizationEndpoint, discovery.TokenEndpoint, discovery.UserinfoEndpoint, nil
	}

	if config.AuthURL == "" || config.TokenURL == "" || config.UserInfoURL == "" {
		return "", "", "", fmt.Errorf("OIDC 配置不完整")
	}
	return config.AuthURL, config.TokenURL, config.UserInfoURL, nil
}

func generateState() string {
	b := make([]byte, 32)
	rand.Read(b)
	state := base64.URLEncoding.EncodeToString(b)
	database.RedisSet("oidc_state:"+state, "1", 10*time.Minute)
	return state
}

func validateState(state string) bool {
	key := "oidc_state:" + state
	exists, err := database.RedisExists(key)
	if err != nil || !exists {
		return false
	}
	database.RedisDelete(key)
	return true
}

// HandleOIDCLogin 发起OIDC登录
func HandleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	config, err := GetOIDCConfig()
	if err != nil || !config.Enabled {
		respondBadRequest(w, "OIDC未启用")
		return
	}

	if config.ClientID == "" {
		respondBadRequest(w, "OIDC配置不完整：缺少 Client ID")
		return
	}

	authURL, _, _, err := getOIDCEndpoints(config)
	if err != nil {
		respondBadRequest(w, "OIDC配置不完整: "+err.Error())
		return
	}

	state := generateState()
	params := url.Values{}
	params.Set("client_id", config.ClientID)
	params.Set("redirect_uri", config.RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", config.Scopes)
	params.Set("state", state)

	http.Redirect(w, r, authURL+"?"+params.Encode(), http.StatusFound)
}

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

	config, _ := GetOIDCConfig()

	if errParam != "" {
		http.Redirect(w, r, getLoginRedirectURL(config, "oidc_"+errParam), http.StatusFound)
		return
	}

	if !validateState(state) {
		http.Redirect(w, r, getLoginRedirectURL(config, "invalid_state"), http.StatusFound)
		return
	}

	if code == "" {
		http.Redirect(w, r, getLoginRedirectURL(config, "no_code"), http.StatusFound)
		return
	}

	if config == nil {
		http.Redirect(w, r, getLoginRedirectURL(nil, "config_error"), http.StatusFound)
		return
	}

	_, tokenURL, userinfoURL, err := getOIDCEndpoints(config)
	if err != nil {
		http.Redirect(w, r, getLoginRedirectURL(config, "config_error"), http.StatusFound)
		return
	}

	tokenResp, err := exchangeToken(config, code, tokenURL)
	if err != nil {
		log.Printf("[OIDC] 交换token失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "token_exchange_failed"), http.StatusFound)
		return
	}

	userInfo, err := getUserInfo(userinfoURL, tokenResp.AccessToken)
	if err != nil {
		log.Printf("[OIDC] 获取用户信息失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "userinfo_failed"), http.StatusFound)
		return
	}

	user, err := findOrCreateOIDCUser(config, userInfo)
	if err != nil {
		log.Printf("[OIDC] 用户处理失败: %v", err)
		http.Redirect(w, r, getLoginRedirectURL(config, "user_error"), http.StatusFound)
		return
	}

	if user.Status != "active" {
		http.Redirect(w, r, getLoginRedirectURL(config, "user_disabled"), http.StatusFound)
		return
	}

	token, expiresAt, err := GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		http.Redirect(w, r, getLoginRedirectURL(config, "token_error"), http.StatusFound)
		return
	}

	SetAuthCookie(w, token, expiresAt)

	frontendURL := "/dashboard"
	if config.RedirectURI != "" {
		if idx := strings.Index(config.RedirectURI, "/api/"); idx > 0 {
			frontendURL = config.RedirectURI[:idx] + "/dashboard"
		}
	}

	if strings.Contains(frontendURL, "?") {
		frontendURL += "&sso_login=1"
	} else {
		frontendURL += "?sso_login=1"
	}

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
}

func exchangeToken(config *OIDCConfig, code string, tokenURL string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", config.RedirectURI)
	data.Set("client_id", config.ClientID)
	data.Set("client_secret", config.ClientSecret)

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

func getUserInfo(userinfoURL string, accessToken string) (*UserInfo, error) {
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

func findOrCreateOIDCUser(config *OIDCConfig, userInfo *UserInfo) (*models.User, error) {
	username := userInfo.PreferredUsername
	if username == "" {
		username = userInfo.Email
	}
	if username == "" {
		username = userInfo.Sub
	}

	var user models.User
	err := database.DB.QueryRow(`
		SELECT id, username, display_name, email, role, status
		FROM users WHERE oidc_sub = ?
	`, userInfo.Sub).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role, &user.Status)

	if err == nil {
		return &user, nil
	}

	if !config.AutoRegister {
		return nil, fmt.Errorf("用户不存在且未启用自动注册")
	}

	var existingCount int
	database.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&existingCount)
	if existingCount > 0 {
		username = fmt.Sprintf("%s_sso_%s", username, userInfo.Sub[:8])
	}

	newID := uuid.New().String()
	displayName := userInfo.Name
	if displayName == "" {
		displayName = username
	}

	_, err = database.DB.Exec(`
		INSERT INTO users (id, username, display_name, email, password, role, status, oidc_sub, auth_source, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, 'active', ?, 'sso', NOW(), NOW())
	`, newID, username, displayName, userInfo.Email, config.DefaultRole, userInfo.Sub)

	if err != nil {
		return nil, fmt.Errorf("创建用户失败: %v", err)
	}

	return &models.User{
		ID:          newID,
		Username:    username,
		DisplayName: displayName,
		Email:       userInfo.Email,
		Role:        config.DefaultRole,
		Status:      "active",
	}, nil
}
