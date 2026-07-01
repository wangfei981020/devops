// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

import "time"

// Domain represents a domain entity
type Domain struct {
	ID        int64     `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// CreateDomainRequest represents the request payload for creating a domain
type CreateDomainRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
}

// UpdateDomainRequest represents the request payload for updating a domain
type UpdateDomainRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
}

