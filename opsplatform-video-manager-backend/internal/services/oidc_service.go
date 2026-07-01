// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const oidcHTTPTimeout = 30 * time.Second

var oidcHTTPClient = &http.Client{Timeout: oidcHTTPTimeout}

var ErrOIDCDisabled = errors.New("oidc is not configured")

// OIDCConfigIssues returns human-readable missing fields when OIDC cannot run.
func OIDCConfigIssues(cfg OIDCConfig) []string {
	if !cfg.Enabled {
		return []string{"oidc is not enabled"}
	}
	var missing []string
	if strings.TrimSpace(cfg.IssuerURL) == "" {
		missing = append(missing, "issuer_url")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		missing = append(missing, "client_id")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		missing = append(missing, "client_secret")
	}
	if strings.TrimSpace(cfg.RedirectURL) == "" {
		missing = append(missing, "redirect_url")
	}
	return missing
}

type OIDCUserInfo struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
}

type OIDCService struct {
	enabled            bool
	provider           *oidc.Provider
	verifier           *oidc.IDTokenVerifier
	oauth2Config       *oauth2.Config
	frontendSuccessURL string
}

type OIDCConfig struct {
	Enabled            bool
	IssuerURL          string
	ClientID           string
	ClientSecret       string
	RedirectURL        string
	Scopes             string
	FrontendSuccessURL string
}

func NewOIDCService(ctx context.Context) (*OIDCService, error) {
	cfg := OIDCConfig{
		Enabled:            strings.EqualFold(strings.TrimSpace(os.Getenv("OIDC_ENABLED")), "true"),
		IssuerURL:          strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL")),
		ClientID:           strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret:       strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET")),
		RedirectURL:        strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL")),
		Scopes:             strings.TrimSpace(os.Getenv("OIDC_SCOPES")),
		FrontendSuccessURL: strings.TrimSpace(os.Getenv("OIDC_FRONTEND_SUCCESS_URL")),
	}
	return NewOIDCServiceFromConfig(ctx, cfg)
}

func NewOIDCServiceFromConfig(ctx context.Context, cfg OIDCConfig) (*OIDCService, error) {
	issuer := strings.TrimSpace(cfg.IssuerURL)
	clientID := strings.TrimSpace(cfg.ClientID)
	clientSecret := strings.TrimSpace(cfg.ClientSecret)
	redirectURL := normalizeOIDCRedirectURL(cfg.RedirectURL)

	service := &OIDCService{
		enabled: false,
	}

	if issues := OIDCConfigIssues(cfg); len(issues) > 0 {
		return service, nil
	}

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize oidc provider: %w", err)
	}

	scopes := []string{oidc.ScopeOpenID, "profile", "email"}
	if rawScopes := strings.TrimSpace(cfg.Scopes); rawScopes != "" {
		customScopes := strings.Fields(rawScopes)
		if len(customScopes) > 0 {
			scopes = customScopes
		}
	}

	frontendSuccessURL := strings.TrimSpace(cfg.FrontendSuccessURL)
	if frontendSuccessURL == "" {
		frontendSuccessURL = "/login"
	}

	endpoint := provider.Endpoint()
	// ppu-sso and similar providers often require client_id/client_secret in POST body.
	endpoint.AuthStyle = oauth2.AuthStyleInParams

	service.enabled = true
	service.provider = provider
	service.verifier = provider.Verifier(&oidc.Config{ClientID: clientID})
	service.oauth2Config = &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}
	service.frontendSuccessURL = frontendSuccessURL

	return service, nil
}

func (s *OIDCService) IsEnabled() bool {
	return s != nil && s.enabled
}

func (s *OIDCService) AuthCodeURL(state string) (string, error) {
	if !s.IsEnabled() {
		return "", ErrOIDCDisabled
	}
	return s.oauth2Config.AuthCodeURL(state), nil
}

func (s *OIDCService) ExchangeAndVerify(ctx context.Context, code, state string) (*OIDCUserInfo, error) {
	if !s.IsEnabled() {
		return nil, ErrOIDCDisabled
	}

	token, rawIDToken, err := s.exchangeAuthorizationCode(ctx, strings.TrimSpace(code), strings.TrimSpace(state))
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(rawIDToken) != "" {
		idToken, err := s.verifier.Verify(ctx, rawIDToken)
		if err != nil {
			return nil, fmt.Errorf("id_token verification failed: %w", err)
		}
		var info OIDCUserInfo
		if err := idToken.Claims(&info); err != nil {
			return nil, fmt.Errorf("failed to parse id_token claims: %w", err)
		}
		return &info, nil
	}

	// Fallback when the IdP returns access_token only (no id_token in body).
	if s.provider.UserInfoEndpoint() == "" {
		return nil, errors.New("id_token not found in token response and userinfo endpoint unavailable")
	}
	ui, err := s.provider.UserInfo(ctx, s.oauth2Config.TokenSource(ctx, token))
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	return userInfoFromOIDC(ui)
}

func userInfoFromOIDC(ui *oidc.UserInfo) (*OIDCUserInfo, error) {
	if ui == nil {
		return nil, errors.New("empty userinfo")
	}
	var claims struct {
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := ui.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo claims: %w", err)
	}
	return &OIDCUserInfo{
		Subject:           ui.Subject,
		Email:             ui.Email,
		PreferredUsername: claims.PreferredUsername,
		Name:              claims.Name,
	}, nil
}

// normalizeOIDCRedirectURL keeps authorize + token exchange redirect_uri identical.
func normalizeOIDCRedirectURL(raw string) string {
	return strings.TrimSuffix(strings.TrimSpace(raw), "/")
}

type oidcTokenError struct {
	status  int
	body    string
	message string
}

func (e *oidcTokenError) Error() string {
	if e.message != "" {
		return e.message
	}
	return fmt.Sprintf("token endpoint returned HTTP %d: %s", e.status, e.body)
}

func isOIDCCodeNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "authorization code not found") ||
		strings.Contains(msg, "code not found")
}

// exchangeAuthorizationCode uses an explicit form POST (client_secret_post) so the
// redirect_uri matches authorize exactly and we avoid oauth2 auto-retry auth styles
// that can invalidate one-time codes on some providers.
func (s *OIDCService) exchangeAuthorizationCode(ctx context.Context, code, state string) (*oauth2.Token, string, error) {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		token, idToken, err := s.postTokenExchange(ctx, code, state, oauth2.AuthStyleInParams)
		if err == nil {
			return token, idToken, nil
		}
		lastErr = err

		if !isOIDCCodeNotFoundErr(err) || attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(time.Duration(attempt) * 150 * time.Millisecond):
		}
	}
	return nil, "", fmt.Errorf("token exchange failed: %w", lastErr)
}

func (s *OIDCService) postTokenExchange(ctx context.Context, code, state string, authStyle oauth2.AuthStyle) (*oauth2.Token, string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", s.oauth2Config.RedirectURL)
	if state != "" {
		form.Set("state", state)
	}
	if authStyle == oauth2.AuthStyleInHeader {
		raw := s.oauth2Config.ClientID + ":" + s.oauth2Config.ClientSecret
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.oauth2Config.Endpoint.TokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(raw)))
		return s.doTokenRequest(req)
	}

	form.Set("client_id", s.oauth2Config.ClientID)
	form.Set("client_secret", s.oauth2Config.ClientSecret)
	encoded := form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.oauth2Config.Endpoint.TokenURL, strings.NewReader(encoded))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return s.doTokenRequest(req)
}

func (s *OIDCService) doTokenRequest(req *http.Request) (*oauth2.Token, string, error) {

	resp, err := oidcHTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := parseOIDCTokenErrorMessage(body)
		return nil, "", &oidcTokenError{
			status:  resp.StatusCode,
			body:    strings.TrimSpace(string(body)),
			message: msg,
		}
	}

	return parseOIDCTokenResponse(body)
}

func parseOIDCTokenErrorMessage(body []byte) string {
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Detail           string `json:"detail"`
		Message          string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body))
	}
	switch {
	case payload.Detail != "":
		return payload.Detail
	case payload.ErrorDescription != "":
		return payload.ErrorDescription
	case payload.Message != "":
		return payload.Message
	case payload.Error != "":
		return payload.Error
	default:
		return strings.TrimSpace(string(body))
	}
}

func parseOIDCTokenResponse(body []byte) (*oauth2.Token, string, error) {
	var payload struct {
		AccessToken  string          `json:"access_token"`
		TokenType    string          `json:"token_type"`
		RefreshToken string          `json:"refresh_token"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
		IDToken      string          `json:"id_token"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", fmt.Errorf("failed to parse token response: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" && strings.TrimSpace(payload.IDToken) == "" {
		return nil, "", errors.New("token response missing access_token and id_token")
	}

	token := &oauth2.Token{
		AccessToken:  payload.AccessToken,
		TokenType:    payload.TokenType,
		RefreshToken: payload.RefreshToken,
	}
	if len(payload.ExpiresIn) > 0 {
		var expiresIn int64
		if err := json.Unmarshal(payload.ExpiresIn, &expiresIn); err == nil && expiresIn > 0 {
			token.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
		} else {
			var expiresStr string
			if err := json.Unmarshal(payload.ExpiresIn, &expiresStr); err == nil {
				if n, err := strconv.ParseInt(expiresStr, 10, 64); err == nil && n > 0 {
					token.Expiry = time.Now().Add(time.Duration(n) * time.Second)
				}
			}
		}
	}
	return token, payload.IDToken, nil
}

func (s *OIDCService) FrontendSuccessURL() string {
	if s == nil || s.frontendSuccessURL == "" {
		return "/login"
	}
	return s.frontendSuccessURL
}

// ProbeClientCredentials posts a dummy code to the token endpoint to verify client_id/secret.
// ppu-sso returns "Invalid client credentials" when secret is wrong, and
// "authorization code not found" when credentials are accepted.
func (s *OIDCService) ProbeClientCredentials(ctx context.Context) (string, error) {
	if !s.IsEnabled() {
		return "", ErrOIDCDisabled
	}
	_, _, err := s.postTokenExchange(ctx, "probe-invalid-code", "", oauth2.AuthStyleInParams)
	if err == nil {
		return "unexpected_success", nil
	}
	tokenErr, ok := err.(*oidcTokenError)
	if !ok {
		return "unknown", err
	}
	msg := strings.ToLower(tokenErr.message)
	switch {
	case strings.Contains(msg, "invalid client"):
		return "invalid_client_credentials", nil
	case strings.Contains(msg, "authorization code not found"), strings.Contains(msg, "code not found"):
		return "client_credentials_ok", nil
	default:
		return "unknown", err
	}
}

func (s *OIDCService) RedirectURL() string {
	if s == nil || s.oauth2Config == nil {
		return ""
	}
	return s.oauth2Config.RedirectURL
}

func (s *OIDCService) TokenEndpoint() string {
	if s == nil || s.oauth2Config == nil {
		return ""
	}
	return s.oauth2Config.Endpoint.TokenURL
}
