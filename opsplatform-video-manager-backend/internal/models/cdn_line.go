// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

import "time"

// CDNLine represents a CDN line entity
type CDNLine struct {
	ID         int64        `json:"id" db:"id"`
	ProviderID int64        `json:"provider_id" db:"provider_id"`
	Provider   *CDNProvider `json:"provider,omitempty" db:"-"`
	Name       string       `json:"name" db:"name"` // 名称 (原 display_name)
	Code       string       `json:"code" db:"code"` // 代码 (原 name)
	CreatedAt  time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at" db:"updated_at"`
}

// CreateCDNLineRequest represents the request payload for creating a CDN line
type CreateCDNLineRequest struct {
	ProviderID int64  `json:"provider_id" binding:"required"`
	Name       string `json:"name" binding:"required,min=1,max=255"` // 名称
	Code       string `json:"code" binding:"required,min=1,max=255"` // 代码
}

// UpdateCDNLineRequest represents the request payload for updating a CDN line
type UpdateCDNLineRequest struct {
	ProviderID int64  `json:"provider_id" binding:"required"`
	Name       string `json:"name" binding:"required,min=1,max=255"` // 名称
	Code       string `json:"code" binding:"required,min=1,max=255"` // 代码
}
