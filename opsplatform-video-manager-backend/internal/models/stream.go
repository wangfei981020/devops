// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

import "time"

// Stream represents a stream entity
type Stream struct {
	ID         int64     `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	Code       string    `json:"code" db:"code"`
	ProviderID *int64    `json:"provider_id,omitempty" db:"provider_id"` // NULL means match all providers
	Provider   *CDNProvider `json:"provider,omitempty" db:"-"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// CreateStreamRequest represents the request payload for creating a stream
type CreateStreamRequest struct {
	Name       string `json:"name" binding:"required,min=1,max=255"`
	Code       string `json:"code" binding:"required,min=1,max=100"`
	ProviderID *int64 `json:"provider_id,omitempty"` // Optional: NULL means match all providers
}

// UpdateStreamRequest represents the request payload for updating a stream
type UpdateStreamRequest struct {
	Name       string `json:"name" binding:"required,min=1,max=255"`
	Code       string `json:"code" binding:"required,min=1,max=100"`
	ProviderID *int64 `json:"provider_id,omitempty"` // Optional: NULL means match all providers
}

