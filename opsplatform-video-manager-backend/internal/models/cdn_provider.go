// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

import "time"

// CDNProvider represents a CDN provider entity
type CDNProvider struct {
	ID        int64     `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Code      string    `json:"code" db:"code"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// CreateCDNProviderRequest represents the request payload for creating a CDN provider
type CreateCDNProviderRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
	Code string `json:"code" binding:"required,min=1,max=100"`
}

// UpdateCDNProviderRequest represents the request payload for updating a CDN provider
type UpdateCDNProviderRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
	Code string `json:"code" binding:"required,min=1,max=100"`
}

