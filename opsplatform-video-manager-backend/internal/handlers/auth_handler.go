// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
	"github.com/video-manager/backend/internal/services"
	"github.com/video-manager/backend/pkg/jwt"
	"github.com/video-manager/backend/pkg/logger"
	"github.com/video-manager/backend/pkg/response"
)

type AuthHandler struct {
	authService          *services.AuthService
	systemSettingService *services.SystemSettingService
}

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{
		authService:          services.NewAuthService(),
		systemSettingService: services.NewSystemSettingService(),
	}
}

// Login handles user login
// @Summary User login
// @Description Authenticate user and return JWT token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body models.LoginRequest true "Login credentials"
// @Success 200 {object} response.Response{data=models.LoginResponse}
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Router /api/auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	loginResp, err := h.authService.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		logger.Warn("Login failed", "username", req.Username, "ip", c.ClientIP(), "error", err)
		response.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	logger.Info("User logged in successfully", "username", loginResp.Username, "is_admin", loginResp.IsAdmin, "ip", c.ClientIP())
	response.Success(c, loginResp)
}

// ChangePassword handles password change
// @Summary Change password
// @Description Change user password
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.ChangePasswordRequest true "Password change request"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Router /api/auth/change-password [post]
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	err := h.authService.ChangePassword(
		c.Request.Context(),
		userID.(int64),
		req.OldPassword,
		req.NewPassword,
	)
	if err != nil {
		logger.Warn("Password change failed", "user_id", userID, "error", err)
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	logger.Info("Password changed successfully", "user_id", userID)
	response.Success(c, gin.H{"message": "password changed successfully"})
}

// GetCurrentUser returns current user information
// @Summary Get current user
// @Description Get current authenticated user information
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=object}
// @Failure 401 {object} response.Response
// @Router /api/auth/me [get]
func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	userID, _ := c.Get("userID")
	username, _ := c.Get("username")
	isAdmin, _ := c.Get("isAdmin")

	response.Success(c, gin.H{
		"id":       userID,
		"username": username,
		"is_admin": isAdmin,
	})
}

// GetTokenInfo returns current token information
// @Summary Get token information
// @Description Get current JWT token information including expiration time
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=object}
// @Failure 401 {object} response.Response
// @Router /api/auth/token-info [get]
func (h *AuthHandler) GetTokenInfo(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		response.Error(c, http.StatusUnauthorized, "authorization header required")
		return
	}

	// Extract token from "Bearer <token>"
	tokenString := ""
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		tokenString = authHeader[7:]
	} else {
		response.Error(c, http.StatusBadRequest, "invalid authorization header format")
		return
	}

	// Parse token to get claims (without validation since we already validated in middleware)
	claims, err := jwt.ParseTokenWithoutValidation(tokenString)
	if err != nil {
		response.Error(c, http.StatusBadRequest, "invalid token format")
		return
	}

	// Calculate expiration time
	expiresAt := time.Time{}
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}

	issuedAt := time.Time{}
	if claims.IssuedAt != nil {
		issuedAt = claims.IssuedAt.Time
	}

	response.Success(c, gin.H{
		"user_id":    claims.UserID,
		"username":   claims.Username,
		"is_admin":   claims.IsAdmin,
		"issued_at":  issuedAt,
		"expires_at": expiresAt,
		"expires_in": int64(time.Until(expiresAt).Seconds()),
		"issuer":     claims.Issuer,
		"subject":    claims.Subject,
	})
}

// CreateToken creates a new token
// @Summary Create token
// @Description Create a new JWT token with custom expiration
// @Tags auth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateTokenRequest true "Token creation request"
// @Success 200 {object} response.Response{data=models.LoginResponse}
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Router /api/auth/tokens [post]
func (h *AuthHandler) CreateToken(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	username, _ := c.Get("username")
	isAdmin, _ := c.Get("isAdmin")

	var req models.CreateTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	token, err := h.authService.CreateToken(
		c.Request.Context(),
		userID.(int64),
		username.(string),
		isAdmin.(bool),
		req,
	)
	if err != nil {
		if err == repositories.ErrTokenNameExists {
			response.BadRequest(c, "Token 名称已存在")
		} else {
			response.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	logger.InfoContext(c.Request.Context(), "Token created successfully", "user_id", userID, "name", req.Name, "never_expire", req.NeverExpire)
	response.Success(c, gin.H{
		"token":        token,
		"username":     username,
		"is_admin":     isAdmin,
		"name":         req.Name,
		"never_expire": req.NeverExpire,
	})
}

// GetTokens retrieves all tokens for the current user
// @Summary Get all tokens
// @Description Get all tokens created by the current user
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]models.Token}
// @Failure 401 {object} response.Response
// @Router /api/auth/tokens [get]
func (h *AuthHandler) GetTokens(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	tokens, err := h.authService.GetTokens(c.Request.Context(), userID.(int64))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, tokens)
}

// DeleteToken deletes a token by ID
// @Summary Delete token
// @Description Delete a token by ID (only if it belongs to the current user)
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Param id path int true "Token ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Router /api/auth/tokens/{id} [delete]
func (h *AuthHandler) DeleteToken(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		response.Error(c, http.StatusUnauthorized, "user not authenticated")
		return
	}

	tokenID := c.Param("id")
	if tokenID == "" {
		response.BadRequest(c, "token id is required")
		return
	}

	var id int64
	if _, err := fmt.Sscanf(tokenID, "%d", &id); err != nil {
		response.BadRequest(c, "invalid token id")
		return
	}

	err := h.authService.DeleteToken(c.Request.Context(), id, userID.(int64))
	if err != nil {
		if errors.Is(err, repositories.ErrTokenNotFound) {
			response.Error(c, http.StatusNotFound, "token not found")
			return
		}
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	logger.InfoContext(c.Request.Context(), "Token deleted successfully", "user_id", userID, "token_id", id)
	response.Success(c, gin.H{"message": "token deleted successfully"})
}

// OIDCStatus reports whether SSO login is ready (public).
// @Summary OIDC availability
// @Description Check if OIDC SSO login is configured and ready
// @Tags auth
// @Produce json
// @Success 200 {object} response.Response{data=object}
// @Router /api/auth/oidc/status [get]
func (h *AuthHandler) OIDCStatus(c *gin.Context) {
	settings, err := h.systemSettingService.GetOIDCSettings(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "failed to load oidc settings")
		return
	}
	cfg := services.OIDCConfig{
		Enabled:            settings.Enabled,
		IssuerURL:          settings.IssuerURL,
		ClientID:           settings.ClientID,
		ClientSecret:       settings.ClientSecret,
		RedirectURL:        settings.RedirectURL,
		Scopes:             settings.Scopes,
		FrontendSuccessURL: settings.FrontendSuccessURL,
	}
	issues := services.OIDCConfigIssues(cfg)
	ready := settings.Enabled && len(issues) == 0
	data := gin.H{
		"enabled": settings.Enabled,
		"ready":   ready,
		"issues":  issues,
	}
	if ready {
		data["redirect_url"] = cfg.RedirectURL
		data["client_id"] = cfg.ClientID
	}
	response.Success(c, data)
}

// OIDCLogin redirects user to OIDC provider authorization endpoint.
// @Summary OIDC login redirect
// @Description Redirect to OIDC provider for SSO login
// @Tags auth
// @Produce json
// @Success 302 {string} string "Redirect to OIDC provider"
// @Failure 503 {object} response.Response
// @Router /api/auth/oidc/login [get]
func (h *AuthHandler) OIDCLogin(c *gin.Context) {
	logger.Info("OIDC login requested",
		"ip", c.ClientIP(),
		"host", c.Request.Host,
		"forwarded_proto", c.GetHeader("X-Forwarded-Proto"),
	)

	oidcSvc, cfg, err := h.loadOIDCService(c.Request.Context())
	if err != nil {
		logger.Error("OIDC provider initialization failed",
			"error", err,
			"issuer_url", cfg.IssuerURL,
			"client_id", cfg.ClientID,
			"has_client_secret", cfg.ClientSecret != "",
			"redirect_url", cfg.RedirectURL,
			"ip", c.ClientIP(),
		)
		response.Error(c, http.StatusServiceUnavailable, "oidc provider discovery failed: "+err.Error())
		return
	}

	if !oidcSvc.IsEnabled() {
		respondOIDCNotReady(c, cfg, "incomplete configuration")
		return
	}

	if ssoErr := strings.TrimSpace(c.Query("sso_error")); ssoErr != "" {
		redirectOIDCError(c, cfg, ssoErr)
		return
	}

	state, err := generateOIDCState()
	if err != nil {
		logger.Error("OIDC state generation failed", "error", err, "ip", c.ClientIP())
		response.InternalServerError(c, "failed to initialize oidc state")
		return
	}

	authURL, err := oidcSvc.AuthCodeURL(state)
	if err != nil {
		respondOIDCNotReady(c, cfg, "auth_code_url: "+err.Error())
		return
	}

	logger.Info("OIDC login redirect",
		"issuer_url", cfg.IssuerURL,
		"redirect_url", cfg.RedirectURL,
		"ip", c.ClientIP(),
	)

	globalOIDCStateStore.register(state)
	setOIDCStateCookie(c, state)
	c.Redirect(http.StatusFound, authURL)
}

// OIDCCallback validates state, exchanges the authorization code, and redirects to the frontend login page.
// @Summary OIDC login callback
// @Description Accept OIDC redirect, exchange code, redirect to frontend with sso_token or sso_error
// @Tags auth
// @Produce json
// @Param code query string true "OIDC authorization code"
// @Param state query string true "OIDC state"
// @Success 302 {string} string "Redirect to frontend login with sso_token"
// @Failure 302 {string} string "Redirect to frontend login with sso_error"
// @Failure 503 {object} response.Response
// @Router /api/auth/oidc/callback [get]
func (h *AuthHandler) OIDCCallback(c *gin.Context) {
	oidcSvc, cfg, err := h.loadOIDCService(c.Request.Context())
	if err != nil {
		logger.Warn("Failed to initialize OIDC service", "error", err)
		response.Error(c, http.StatusServiceUnavailable, "oidc provider discovery failed: "+err.Error())
		return
	}
	if !oidcSvc.IsEnabled() {
		respondOIDCNotReady(c, cfg, "incomplete configuration")
		return
	}

	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")

	if errStr := strings.TrimSpace(c.Query("error")); errStr != "" {
		desc := strings.TrimSpace(c.Query("error_description"))
		msg := errStr
		if desc != "" {
			msg = errStr + ": " + desc
		}
		logger.Warn("OIDC callback error from IdP", "error", msg, "ip", c.ClientIP())
		redirectOIDCError(c, cfg, msg)
		return
	}

	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	if code == "" || state == "" {
		redirectOIDCError(c, cfg, "missing oidc callback code/state")
		return
	}

	if !validateOIDCState(c, state) {
		cookieState, _ := c.Cookie(oidcStateCookieName)
		logger.Warn("OIDC callback rejected: state mismatch",
			"ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
			"has_cookie", cookieState != "",
			"cookie_matches", cookieState == state,
			"server_state_valid", globalOIDCStateStore.valid(state),
		)
		redirectOIDCError(c, cfg, "state 校验失败，请从登录页重新发起 SSO")
		return
	}

	logger.Info("OIDC callback exchanging code",
		"ip", c.ClientIP(),
		"redirect_url", cfg.RedirectURL,
		"code_len", len(code),
	)
	h.finishOIDCWithCode(c, oidcSvc, cfg, code, state)
}

func (h *AuthHandler) finishOIDCWithCode(c *gin.Context, oidcSvc *services.OIDCService, cfg services.OIDCConfig, code, state string) {
	info, exchangeErr := oidcSvc.ExchangeAndVerify(c.Request.Context(), code, state)
	if exchangeErr != nil {
		logger.Error("OIDC exchange failed",
			"error", exchangeErr,
			"ip", c.ClientIP(),
			"redirect_url", cfg.RedirectURL,
			"client_id", cfg.ClientID,
			"code_len", len(code),
			"user_agent", c.Request.UserAgent(),
		)
		redirectOIDCError(c, cfg, oidcExchangeUserMessage(exchangeErr))
		return
	}

	username := deriveOIDCUsername(info)
	if username == "" {
		redirectOIDCError(c, cfg, "oidc claims missing username")
		return
	}

	loginResp, err := h.authService.LoginOrCreateOIDCUser(c.Request.Context(), username)
	if err != nil {
		logger.Error("OIDC local login/create failed", "username", username, "error", err)
		redirectOIDCError(c, cfg, "failed to complete oidc login")
		return
	}

	redirectOIDCSuccess(c, cfg, loginResp.Token)
}

const oidcStateCookieName = "oidc_state"

func requestIsSecure(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

func setOIDCStateCookie(c *gin.Context, state string) {
	secure := requestIsSecure(c)
	c.SetSameSite(http.SameSiteNoneMode)
	c.SetCookie(oidcStateCookieName, state, 600, "/", "", secure, true)
}

func clearOIDCStateCookie(c *gin.Context) {
	secure := requestIsSecure(c)
	c.SetSameSite(http.SameSiteNoneMode)
	c.SetCookie(oidcStateCookieName, "", -1, "/", "", secure, true)
}

// validateOIDCState accepts cookie match (dvr-manager) or server-side state (cross-site IdP fallback).
func validateOIDCState(c *gin.Context, state string) bool {
	cookieState, err := c.Cookie(oidcStateCookieName)
	cookieOK := err == nil && cookieState != "" && cookieState == state

	if cookieOK {
		clearOIDCStateCookie(c)
		globalOIDCStateStore.consume(state)
		return true
	}
	if globalOIDCStateStore.consume(state) {
		clearOIDCStateCookie(c)
		return true
	}
	return false
}

func redirectOIDCSuccess(c *gin.Context, cfg services.OIDCConfig, token string) {
	c.Redirect(http.StatusFound, buildOIDCSuccessRedirect(resolveOIDCFrontendURL(c, cfg), token))
}

func redirectOIDCError(c *gin.Context, cfg services.OIDCConfig, message string) {
	c.Redirect(http.StatusFound, buildOIDCErrorRedirect(resolveOIDCFrontendURL(c, cfg), message))
}

func resolveOIDCFrontendURL(c *gin.Context, cfg services.OIDCConfig) string {
	raw := strings.TrimSpace(cfg.FrontendSuccessURL)
	if raw != "" {
		if u, err := url.Parse(raw); err == nil {
			path := strings.ToLower(u.Path)
			if strings.Contains(path, "/api/auth/oidc") || strings.HasSuffix(path, "/oidc/login") {
				logger.Warn("OIDC frontend_success_url points at API route; using /login instead",
					"configured", raw,
				)
				return defaultOIDCFrontendLoginURL(c)
			}
			return raw
		}
	}
	if raw != "" && !strings.HasPrefix(raw, "/api/") {
		return raw
	}
	return defaultOIDCFrontendLoginURL(c)
}

func defaultOIDCFrontendLoginURL(c *gin.Context) string {
	scheme := "https"
	if proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	} else if c.Request.TLS == nil {
		scheme = "http"
	}
	host := strings.TrimSpace(c.Request.Host)
	if host == "" {
		return "/login"
	}
	return scheme + "://" + host + "/login"
}

func buildOIDCSuccessRedirect(baseURL string, token string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "/login?sso_token=" + url.QueryEscape(token)
	}
	q := u.Query()
	q.Set("sso_token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func buildOIDCErrorRedirect(baseURL string, message string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "/login?sso_error=" + url.QueryEscape(message)
	}
	q := u.Query()
	q.Set("sso_error", message)
	u.RawQuery = q.Encode()
	return u.String()
}

func respondOIDCNotReady(c *gin.Context, cfg services.OIDCConfig, reason string) {
	issues := services.OIDCConfigIssues(cfg)
	msg := "oidc is not fully configured"
	if len(issues) > 0 {
		msg = msg + ": " + strings.Join(issues, ", ")
	}

	logger.Error("OIDC login not ready",
		"reason", reason,
		"issues", issues,
		"enabled", cfg.Enabled,
		"issuer_url", cfg.IssuerURL,
		"client_id", cfg.ClientID,
		"has_client_secret", cfg.ClientSecret != "",
		"redirect_url", cfg.RedirectURL,
		"ip", c.ClientIP(),
	)

	c.JSON(http.StatusServiceUnavailable, response.Response{
		Code:    http.StatusServiceUnavailable,
		Message: msg,
		Data: gin.H{
			"issues":            issues,
			"enabled":           cfg.Enabled,
			"has_client_secret": cfg.ClientSecret != "",
		},
	})
}

func (h *AuthHandler) loadOIDCService(ctx context.Context) (*services.OIDCService, services.OIDCConfig, error) {
	settings, err := h.systemSettingService.GetOIDCSettings(ctx)
	if err != nil {
		return nil, services.OIDCConfig{}, err
	}
	cfg := services.OIDCConfig{
		Enabled:            settings.Enabled,
		IssuerURL:          settings.IssuerURL,
		ClientID:           settings.ClientID,
		ClientSecret:       settings.ClientSecret,
		RedirectURL:        settings.RedirectURL,
		Scopes:             settings.Scopes,
		FrontendSuccessURL: settings.FrontendSuccessURL,
	}
	svc, err := services.NewOIDCServiceFromConfig(ctx, cfg)
	if err != nil {
		return nil, cfg, err
	}
	return svc, cfg, nil
}

func generateOIDCState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func deriveOIDCUsername(info *services.OIDCUserInfo) string {
	if info == nil {
		return ""
	}
	if u := strings.TrimSpace(info.PreferredUsername); u != "" {
		return sanitizeUsername(u)
	}
	if email := strings.TrimSpace(info.Email); email != "" {
		local := strings.Split(email, "@")[0]
		if local != "" {
			return sanitizeUsername(local)
		}
	}
	if sub := strings.TrimSpace(info.Subject); sub != "" {
		return sanitizeUsername("oidc_" + sub)
	}
	return ""
}

func sanitizeUsername(input string) string {
	var b strings.Builder
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == '.':
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), "._-")
	if out == "" {
		return ""
	}
	if len(out) > 255 {
		return out[:255]
	}
	return out
}

func oidcExchangeUserMessage(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(strings.ToLower(err.Error()), "authorization code not found") ||
		strings.Contains(strings.ToLower(err.Error()), "code not found") {
		return "SSO failed: IdP could not find the authorization code. This usually means the code expired, was already used, or ppu-sso nodes do not share the same code store. Start login again; if it persists, contact the ppu-sso team."
	}
	if strings.Contains(strings.ToLower(err.Error()), "invalid client") {
		return "SSO failed: invalid OIDC client credentials. Re-save the correct Client Secret in System Settings."
	}
	return "SSO failed: " + err.Error()
}
