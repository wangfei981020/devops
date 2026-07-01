// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

type OIDCSettings struct {
	Enabled            bool   `json:"enabled"`
	IssuerURL          string `json:"issuer_url"`
	ClientID           string `json:"client_id"`
	ClientSecret       string `json:"-"`
	RedirectURL        string `json:"redirect_url"`
	Scopes             string `json:"scopes"`
	FrontendSuccessURL string `json:"frontend_success_url"`
}

type OIDCSettingsResponse struct {
	Enabled            bool   `json:"enabled"`
	IssuerURL          string `json:"issuer_url"`
	ClientID           string `json:"client_id"`
	RedirectURL        string `json:"redirect_url"`
	Scopes             string `json:"scopes"`
	FrontendSuccessURL string `json:"frontend_success_url"`
	HasClientSecret    bool   `json:"has_client_secret"`
}

type UpdateOIDCSettingsRequest struct {
	Enabled            bool   `json:"enabled"`
	IssuerURL          string `json:"issuer_url" binding:"omitempty,url"`
	ClientID           string `json:"client_id"`
	ClientSecret       string `json:"client_secret"`
	RedirectURL        string `json:"redirect_url" binding:"omitempty,url"`
	Scopes             string `json:"scopes"`
	FrontendSuccessURL string `json:"frontend_success_url"`
}
