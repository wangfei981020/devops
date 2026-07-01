// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package models

import "time"

// User represents a user entity
type User struct {
	ID           int64     `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"`
	IsAdmin      bool      `json:"is_admin" db:"is_admin"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// LoginRequest represents the request payload for user login
type LoginRequest struct {
	Username string `json:"username" binding:"required,min=1,max=255"`
	Password string `json:"password" binding:"required,min=1"`
}

// LoginResponse represents the response payload for user login
type LoginResponse struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

// ChangePasswordRequest represents the request payload for changing password
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required,min=1"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

// CreateTokenRequest represents the request payload for creating a new token
type CreateTokenRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=255"` // Token name/description
	NeverExpire bool   `json:"never_expire"`                          // If true, token never expires
	ExpiresIn   int64  `json:"expires_in"`                            // Expiration time in seconds (ignored if never_expire is true)
}

// CreateUserRequest represents the request payload for creating a user.
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=1,max=255"`
	Password string `json:"password" binding:"required,min=6"`
	IsAdmin  bool   `json:"is_admin"`
}

// UpdateUserRequest represents the request payload for updating a user.
type UpdateUserRequest struct {
	Username string `json:"username" binding:"required,min=1,max=255"`
	IsAdmin  bool   `json:"is_admin"`
}
