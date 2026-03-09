package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"sso-backend/config"
	"sso-backend/crypto"
	"sso-backend/database"
)

// In-memory access token store (Phase 1 simplicity, move to DB in Phase 2)
var (
	accessTokenStore   = make(map[string]*AccessTokenInfo)
	accessTokenStoreMu sync.RWMutex
)

type AccessTokenInfo struct {
	UserID    string
	ClientID  string
	ExpiresAt time.Time
}

// HandleDiscovery returns OIDC discovery document
func HandleDiscovery(w http.ResponseWriter, r *http.Request) {
	internalBase := config.GetInternalIssuer()

	discovery := map[string]interface{}{
		"issuer":                                config.Issuer,
		"authorization_endpoint":                config.Issuer + "/authorize",
		"token_endpoint":                        internalBase + "/token",
		"userinfo_endpoint":                     internalBase + "/userinfo",
		"jwks_uri":                              internalBase + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"claims_supported":                      []string{"sub", "name", "email", "preferred_username"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(discovery)
}

// HandleJWKS returns JSON Web Key Set
func HandleJWKS(w http.ResponseWriter, r *http.Request) {
	pub := crypto.PrivateKey.PublicKey
	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": "key-1",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   "AQAB",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jwks)
}

// HandleAuthorize handles OIDC authorization request
func HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	responseType := r.URL.Query().Get("response_type")
	scope := r.URL.Query().Get("scope")

	log.Printf("[Authorize] client_id=%s, redirect_uri=%s, state=%s", clientID, redirectURI, state)

	// Validate client
	client, err := database.GetClientByClientID(clientID)
	if err != nil {
		http.Error(w, "无效的 client_id", http.StatusBadRequest)
		return
	}

	// Validate response_type
	if responseType != "code" {
		http.Error(w, "仅支持 response_type=code", http.StatusBadRequest)
		return
	}

	// Validate redirect_uri
	if !isValidRedirectURI(client.RedirectURIs, redirectURI) {
		http.Error(w, "无效的 redirect_uri", http.StatusBadRequest)
		return
	}

	// 每次 OIDC 授权都要求用户重新确认身份，避免旧 session cookie 导致用户错配
	// 如果有有效 session 且不要求强制登录，可以自动签发（prompt 参数控制）
	prompt := r.URL.Query().Get("prompt")
	if prompt != "login" {
		if cookie, err := r.Cookie("sso_session"); err == nil && cookie.Value != "" {
			tokenHash := database.HashToken(cookie.Value)
			userID, _, sessErr := database.ValidateSession(tokenHash)
			if sessErr == nil && userID != "" {
				user, userErr := database.GetUserByID(userID)
				if userErr == nil && user.Status == "active" {
					code := generateRandomCode()
					database.SaveAuthCode(code, clientID, userID, redirectURI, scope, state, "", time.Now().Add(10*time.Minute))
					callbackURL := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, url.QueryEscape(code), url.QueryEscape(state))
					log.Printf("[Authorize] Session found, issuing code for user %s (%s)", user.Username, userID)
					http.Redirect(w, r, callbackURL, http.StatusFound)
					return
				}
			}
		}
	}

	// No valid session or prompt=login, redirect to login page
	// Use SSO frontend login page with return params
	loginURL := fmt.Sprintf("/sso/login?client_id=%s&redirect_uri=%s&state=%s&scope=%s",
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
		url.QueryEscape(scope))

	http.Redirect(w, r, loginURL, http.StatusFound)
}

// HandleLoginPage renders server-side login form (for OIDC flow)
func HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	scope := r.URL.Query().Get("scope")
	errorMsg := r.URL.Query().Get("error")

	errorHTML := ""
	if errorMsg != "" {
		errorHTML = fmt.Sprintf(`<div class="error">%s</div>`, errorMsg)
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SSO 登录</title>
    <style>
        * { margin:0; padding:0; box-sizing:border-box; }
        body { font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif; background:linear-gradient(135deg,#667eea 0%%,#764ba2 100%%); min-height:100vh; display:flex; align-items:center; justify-content:center; }
        .card { background:#fff; border-radius:16px; padding:40px; width:100%%; max-width:400px; box-shadow:0 20px 60px rgba(0,0,0,0.3); }
        .logo { text-align:center; margin-bottom:30px; }
        .logo h1 { font-size:28px; color:#333; margin-bottom:8px; }
        .logo p { color:#666; font-size:14px; }
        .form-group { margin-bottom:20px; }
        .form-group label { display:block; margin-bottom:8px; color:#333; font-weight:500; }
        .form-group input { width:100%%; padding:12px 16px; border:2px solid #e1e5eb; border-radius:8px; font-size:16px; }
        .form-group input:focus { outline:none; border-color:#667eea; }
        .error { background:#fee2e2; color:#dc2626; padding:12px; border-radius:8px; margin-bottom:20px; font-size:14px; }
        .btn { width:100%%; padding:14px; background:linear-gradient(135deg,#667eea 0%%,#764ba2 100%%); color:#fff; border:none; border-radius:8px; font-size:16px; font-weight:600; cursor:pointer; }
        .btn:hover { transform:translateY(-2px); box-shadow:0 4px 12px rgba(102,126,234,0.4); }
    </style>
</head>
<body>
    <div class="card">
        <div class="logo">
            <h1>SSO 登录</h1>
            <p>统一认证平台</p>
        </div>
        %s
        <form action="/sso/login/submit" method="POST">
            <input type="hidden" name="client_id" value="%s">
            <input type="hidden" name="redirect_uri" value="%s">
            <input type="hidden" name="state" value="%s">
            <input type="hidden" name="scope" value="%s">
            <div class="form-group">
                <label>用户名</label>
                <input type="text" name="username" placeholder="请输入用户名" required autofocus>
            </div>
            <div class="form-group">
                <label>密码</label>
                <input type="password" name="password" placeholder="请输入密码" required>
            </div>
            <button type="submit" class="btn">登录</button>
        </form>
    </div>
</body>
</html>`, errorHTML, clientID, redirectURI, state, scope)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// HandleLoginSubmit processes OIDC login form submission
func HandleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	username := r.FormValue("username")
	password := r.FormValue("password")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	scope := r.FormValue("scope")

	ip := GetClientIP(r)

	// Verify user
	user, err := database.GetUserByUsername(username)
	if err != nil || user.Status != "active" {
		redirectToLoginWithError(w, r, clientID, redirectURI, state, scope, "用户名或密码错误")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		database.SaveAuditLog(user.ID, username, "login_failed", "user", user.ID, "OIDC登录密码错误", ip, r.UserAgent(), "failure")
		redirectToLoginWithError(w, r, clientID, redirectURI, state, scope, "用户名或密码错误")
		return
	}

	// Create SSO session
	sessionToken := generateSessionToken()
	tokenHash := database.HashToken(sessionToken)
	sessionID := database.GenerateID("sess")
	database.CreateSession(sessionID, user.ID, tokenHash, ip, r.UserAgent(), time.Now().Add(time.Hour))

	http.SetCookie(w, &http.Cookie{
		Name:     "sso_session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   3600,
	})

	database.UpdateUserLastLogin(user.ID, ip)
	database.SaveAuditLog(user.ID, user.Username, "login", "user", user.ID, "OIDC流程登录", ip, r.UserAgent(), "success")

	// Generate auth code
	code := generateRandomCode()
	database.SaveAuthCode(code, clientID, user.ID, redirectURI, scope, state, "", time.Now().Add(10*time.Minute))

	log.Printf("[LoginSubmit] Success: user=%s, code=%s...", username, code[:8])

	callbackURL := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, url.QueryEscape(code), url.QueryEscape(state))
	http.Redirect(w, r, callbackURL, http.StatusFound)
}

// HandleToken handles OIDC token exchange
func HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonOIDCError(w, "invalid_request", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseForm()
	grantType := r.FormValue("grant_type")
	code := r.FormValue("code")
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")

	if grantType != "authorization_code" {
		jsonOIDCError(w, "unsupported_grant_type", "仅支持 authorization_code", http.StatusBadRequest)
		return
	}

	// Validate client
	client, err := database.GetClientByClientID(clientID)
	if err != nil {
		jsonOIDCError(w, "invalid_client", "无效的客户端", http.StatusUnauthorized)
		return
	}

	if client.ClientSecret != clientSecret {
		jsonOIDCError(w, "invalid_client", "客户端凭证错误", http.StatusUnauthorized)
		return
	}

	// Validate and consume auth code
	authCode, err := database.GetAndDeleteAuthCode(code)
	if err != nil {
		jsonOIDCError(w, "invalid_grant", "无效的授权码", http.StatusBadRequest)
		return
	}

	if authCode.ClientID != clientID {
		jsonOIDCError(w, "invalid_grant", "授权码与客户端不匹配", http.StatusBadRequest)
		return
	}

	// Get user
	user, err := database.GetUserByID(authCode.UserID)
	if err != nil {
		jsonOIDCError(w, "server_error", "用户不存在", http.StatusInternalServerError)
		return
	}

	// Generate ID Token (RS256 JWT)
	now := time.Now()
	expiry := time.Duration(client.TokenExpiry) * time.Second
	idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":                config.Issuer,
		"sub":                user.ID,
		"aud":                clientID,
		"exp":                now.Add(expiry).Unix(),
		"iat":                now.Unix(),
		"name":               user.DisplayName,
		"email":              user.Email,
		"preferred_username": user.Username,
	})
	idToken.Header["kid"] = "key-1"

	idTokenString, err := idToken.SignedString(crypto.PrivateKey)
	if err != nil {
		jsonOIDCError(w, "server_error", "Token签名失败", http.StatusInternalServerError)
		return
	}

	// Generate Access Token (random, store in memory for Phase 1)
	accessToken := generateRandomCode()
	accessTokenStoreMu.Lock()
	accessTokenStore[accessToken] = &AccessTokenInfo{
		UserID:    user.ID,
		ClientID:  clientID,
		ExpiresAt: now.Add(expiry),
	}
	accessTokenStoreMu.Unlock()

	log.Printf("[Token] Success: user=%s, client=%s", user.Username, clientID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   client.TokenExpiry,
		"id_token":     idTokenString,
	})
}

// HandleUserInfo returns user info for the access token
func HandleUserInfo(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	accessToken := strings.TrimPrefix(authHeader, "Bearer ")

	accessTokenStoreMu.RLock()
	tokenInfo, ok := accessTokenStore[accessToken]
	accessTokenStoreMu.RUnlock()

	if !ok || time.Now().After(tokenInfo.ExpiresAt) {
		http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := database.GetUserByID(tokenInfo.UserID)
	if err != nil {
		http.Error(w, "User not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sub":                user.ID,
		"name":               user.DisplayName,
		"email":              user.Email,
		"preferred_username": user.Username,
	})
}

// HandleRoot handles root path
func HandleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("client_id") != "" {
		HandleAuthorize(w, r)
		return
	}
	// Redirect to SSO frontend
	http.Redirect(w, r, "http://localhost:30901", http.StatusFound)
}

// --- helpers ---

func isValidRedirectURI(allowedJSON, uri string) bool {
	var allowed []string
	if err := json.Unmarshal([]byte(allowedJSON), &allowed); err != nil {
		return false
	}
	for _, a := range allowed {
		if a == uri {
			return true
		}
	}
	return false
}

func redirectToLoginWithError(w http.ResponseWriter, r *http.Request, clientID, redirectURI, state, scope, errorMsg string) {
	loginURL := fmt.Sprintf("/sso/login?client_id=%s&redirect_uri=%s&state=%s&scope=%s&error=%s",
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
		url.QueryEscape(scope),
		url.QueryEscape(errorMsg))
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func generateRandomCode() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:43]
}
