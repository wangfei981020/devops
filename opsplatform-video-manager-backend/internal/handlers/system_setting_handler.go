// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package handlers

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/services"
	"github.com/video-manager/backend/pkg/response"
)

type SystemSettingHandler struct {
	service *services.SystemSettingService
}

func NewSystemSettingHandler() *SystemSettingHandler {
	return &SystemSettingHandler{
		service: services.NewSystemSettingService(),
	}
}

// GetOIDCSettings handles GET /api/system-settings/oidc
// @Summary Get OIDC settings
// @Description Retrieve OIDC SSO configuration (admin only)
// @Tags system-settings
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=models.OIDCSettingsResponse}
// @Failure 500 {object} response.Response
// @Router /api/system-settings/oidc [get]
func (h *SystemSettingHandler) GetOIDCSettings(c *gin.Context) {
	data, err := h.service.GetOIDCSettingsResponse(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "failed to load oidc settings")
		return
	}
	response.Success(c, data)
}

// UpdateOIDCSettings handles PUT /api/system-settings/oidc
// @Summary Update OIDC settings
// @Description Update OIDC SSO configuration (admin only)
// @Tags system-settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.UpdateOIDCSettingsRequest true "OIDC settings request"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/system-settings/oidc [put]
func (h *SystemSettingHandler) UpdateOIDCSettings(c *gin.Context) {
	var req models.UpdateOIDCSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := h.service.UpdateOIDCSettings(c.Request.Context(), req); err != nil {
		if errors.Is(err, services.ErrOIDCSettingsIncomplete) {
			response.BadRequest(c, err.Error())
			return
		}
		response.InternalServerError(c, "failed to update oidc settings")
		return
	}
	response.Success(c, gin.H{"message": "oidc settings updated"})
}

// ProbeOIDCSettings handles POST /api/system-settings/oidc/probe
// @Summary Probe OIDC client credentials
// @Description Verify client_id/secret against the IdP token endpoint (admin only)
// @Tags system-settings
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 503 {object} response.Response
// @Router /api/system-settings/oidc/probe [post]
func (h *SystemSettingHandler) ProbeOIDCSettings(c *gin.Context) {
	settings, err := h.service.GetOIDCSettings(c.Request.Context())
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
	if issues := services.OIDCConfigIssues(cfg); len(issues) > 0 {
		response.BadRequest(c, "oidc settings incomplete: "+strings.Join(issues, ", "))
		return
	}

	svc, err := services.NewOIDCServiceFromConfig(context.Background(), cfg)
	if err != nil {
		response.Error(c, 503, "oidc provider discovery failed: "+err.Error())
		return
	}
	result, err := svc.ProbeClientCredentials(c.Request.Context())
	if err != nil && result == "unknown" {
		response.Error(c, 503, "oidc probe failed: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"probe_result":   result,
		"redirect_url":   svc.RedirectURL(),
		"token_endpoint": svc.TokenEndpoint(),
		"hint":           probeResultHint(result),
	})
}

func probeResultHint(result string) string {
	switch result {
	case "client_credentials_ok":
		return "Client ID and secret are accepted by ppu-sso. If SSO still fails, the issue is likely authorization-code storage (multi-node) or code reuse — contact the IdP team."
	case "invalid_client_credentials":
		return "Client secret is wrong. Update it in OIDC settings to match ppu-sso for infra-video-manager."
	default:
		return "Unexpected probe result; check issuer URL and network connectivity."
	}
}
