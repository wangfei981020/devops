// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
)

var ErrOIDCSettingsIncomplete = errors.New("oidc settings incomplete")

const (
	settingOIDCEnabled            = "oidc_enabled"
	settingOIDCIssuerURL          = "oidc_issuer_url"
	settingOIDCClientID           = "oidc_client_id"
	settingOIDCClientSecret       = "oidc_client_secret"
	settingOIDCRedirectURL        = "oidc_redirect_url"
	settingOIDCScopes             = "oidc_scopes"
	settingOIDCFrontendSuccessURL = "oidc_frontend_success_url"
)

type SystemSettingService struct {
	repo *repositories.SystemSettingRepository
}

func NewSystemSettingService() *SystemSettingService {
	return &SystemSettingService{
		repo: repositories.NewSystemSettingRepository(),
	}
}

func (s *SystemSettingService) GetOIDCSettings(ctx context.Context) (*models.OIDCSettings, error) {
	values, err := s.repo.GetByPrefix(ctx, "oidc_")
	if err != nil {
		return nil, err
	}

	settings := &models.OIDCSettings{
		Enabled:            false,
		IssuerURL:          strings.TrimSpace(values[settingOIDCIssuerURL]),
		ClientID:           strings.TrimSpace(values[settingOIDCClientID]),
		ClientSecret:       strings.TrimSpace(values[settingOIDCClientSecret]),
		RedirectURL:        strings.TrimSpace(values[settingOIDCRedirectURL]),
		Scopes:             strings.TrimSpace(values[settingOIDCScopes]),
		FrontendSuccessURL: strings.TrimSpace(values[settingOIDCFrontendSuccessURL]),
	}
	if values[settingOIDCEnabled] == "true" {
		settings.Enabled = true
	}

	// Fallback to env values when DB is not configured yet.
	if settings.IssuerURL == "" {
		settings.IssuerURL = strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL"))
	}
	if settings.ClientID == "" {
		settings.ClientID = strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID"))
	}
	if settings.ClientSecret == "" {
		settings.ClientSecret = strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET"))
	}
	if settings.RedirectURL == "" {
		settings.RedirectURL = strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL"))
	}
	settings.RedirectURL = normalizeOIDCRedirectURL(settings.RedirectURL)
	if settings.Scopes == "" {
		settings.Scopes = strings.TrimSpace(os.Getenv("OIDC_SCOPES"))
	}
	if settings.FrontendSuccessURL == "" {
		settings.FrontendSuccessURL = strings.TrimSpace(os.Getenv("OIDC_FRONTEND_SUCCESS_URL"))
	}
	if !settings.Enabled {
		settings.Enabled = strings.EqualFold(strings.TrimSpace(os.Getenv("OIDC_ENABLED")), "true")
	}

	return settings, nil
}

func (s *SystemSettingService) GetOIDCSettingsResponse(ctx context.Context) (*models.OIDCSettingsResponse, error) {
	settings, err := s.GetOIDCSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &models.OIDCSettingsResponse{
		Enabled:            settings.Enabled,
		IssuerURL:          settings.IssuerURL,
		ClientID:           settings.ClientID,
		RedirectURL:        settings.RedirectURL,
		Scopes:             settings.Scopes,
		FrontendSuccessURL: settings.FrontendSuccessURL,
		HasClientSecret:    settings.ClientSecret != "",
	}, nil
}

func (s *SystemSettingService) UpdateOIDCSettings(ctx context.Context, req models.UpdateOIDCSettingsRequest) error {
	current, err := s.GetOIDCSettings(ctx)
	if err != nil {
		return err
	}

	merged := mergeOIDCSettings(current, req)
	if merged.Enabled {
		if issues := OIDCConfigIssues(merged); len(issues) > 0 {
			return fmt.Errorf("%w: %s", ErrOIDCSettingsIncomplete, strings.Join(issues, ", "))
		}
		if err := validateOIDCFrontendSuccessURL(merged.FrontendSuccessURL); err != nil {
			return err
		}
	}

	if err := s.repo.Upsert(ctx, settingOIDCEnabled, boolToString(req.Enabled)); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, settingOIDCIssuerURL, strings.TrimSpace(req.IssuerURL)); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, settingOIDCClientID, strings.TrimSpace(req.ClientID)); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, settingOIDCRedirectURL, normalizeOIDCRedirectURL(req.RedirectURL)); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, settingOIDCScopes, strings.TrimSpace(req.Scopes)); err != nil {
		return err
	}
	if err := s.repo.Upsert(ctx, settingOIDCFrontendSuccessURL, strings.TrimSpace(req.FrontendSuccessURL)); err != nil {
		return err
	}

	if secret := strings.TrimSpace(req.ClientSecret); secret != "" {
		if err := s.repo.Upsert(ctx, settingOIDCClientSecret, secret); err != nil {
			return err
		}
	}
	return nil
}

func boolToString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func mergeOIDCSettings(current *models.OIDCSettings, req models.UpdateOIDCSettingsRequest) OIDCConfig {
	secret := strings.TrimSpace(current.ClientSecret)
	if v := strings.TrimSpace(req.ClientSecret); v != "" {
		secret = v
	}

	return OIDCConfig{
		Enabled:            req.Enabled,
		IssuerURL:          firstNonEmpty(strings.TrimSpace(req.IssuerURL), current.IssuerURL),
		ClientID:           firstNonEmpty(strings.TrimSpace(req.ClientID), current.ClientID),
		ClientSecret:       secret,
		RedirectURL:        firstNonEmpty(strings.TrimSpace(req.RedirectURL), current.RedirectURL),
		Scopes:             firstNonEmpty(strings.TrimSpace(req.Scopes), current.Scopes),
		FrontendSuccessURL: firstNonEmpty(strings.TrimSpace(req.FrontendSuccessURL), current.FrontendSuccessURL),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func validateOIDCFrontendSuccessURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("frontend_success_url must be a full URL to the SPA login page (e.g. https://video-manager.example.com/login)")
	}
	path := strings.ToLower(u.Path)
	if strings.Contains(path, "/api/auth/oidc") || strings.HasSuffix(path, "/oidc/login") {
		return fmt.Errorf("frontend_success_url must point to the frontend /login page, not an OIDC API route")
	}
	return nil
}
