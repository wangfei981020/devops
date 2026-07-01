// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package repositories

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/pkg/database"
)

var (
	ErrTokenNotFound   = errors.New("token not found")
	ErrTokenNameExists = errors.New("token name already exists for this user")
)

type TokenRepository struct{}

func NewTokenRepository() *TokenRepository {
	return &TokenRepository{}
}

// Create creates a new token record
func (r *TokenRepository) Create(ctx context.Context, userID int64, name, tokenHash string, neverExpire bool, expiresAt *time.Time) (*models.Token, error) {
	// Check if token name already exists for this user
	var nameExists bool
	err := database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tokens WHERE user_id = $1 AND name = $2)`, userID, name).Scan(&nameExists)
	if err != nil {
		return nil, err
	}
	if nameExists {
		return nil, ErrTokenNameExists
	}

	var token models.Token
	query := `INSERT INTO tokens (user_id, name, token_hash, never_expire, expires_at)
	          VALUES ($1, $2, $3, $4, $5)
	          RETURNING id, user_id, name, token_hash, never_expire, expires_at, created_at, updated_at`

	err = database.DB.QueryRow(ctx, query, userID, name, tokenHash, neverExpire, expiresAt).Scan(
		&token.ID,
		&token.UserID,
		&token.Name,
		&token.TokenHash,
		&token.NeverExpire,
		&token.ExpiresAt,
		&token.CreatedAt,
		&token.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "tokens_user_name_unique") || strings.Contains(err.Error(), "idx_tokens_user_name_unique") {
			return nil, ErrTokenNameExists
		}
		return nil, err
	}

	return &token, nil
}

// GetByUserID retrieves all tokens for a user
func (r *TokenRepository) GetByUserID(ctx context.Context, userID int64) ([]*models.Token, error) {
	query := `SELECT id, user_id, name, token_hash, never_expire, expires_at, created_at, updated_at
	          FROM tokens
	          WHERE user_id = $1
	          ORDER BY created_at DESC`

	rows, err := database.DB.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*models.Token
	for rows.Next() {
		var token models.Token
		err := rows.Scan(
			&token.ID,
			&token.UserID,
			&token.Name,
			&token.TokenHash,
			&token.NeverExpire,
			&token.ExpiresAt,
			&token.CreatedAt,
			&token.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, &token)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return tokens, nil
}

// GetByID retrieves a token by ID
func (r *TokenRepository) GetByID(ctx context.Context, id int64) (*models.Token, error) {
	var token models.Token
	query := `SELECT id, user_id, name, token_hash, never_expire, expires_at, created_at, updated_at
	          FROM tokens WHERE id = $1`

	err := database.DB.QueryRow(ctx, query, id).Scan(
		&token.ID,
		&token.UserID,
		&token.Name,
		&token.TokenHash,
		&token.NeverExpire,
		&token.ExpiresAt,
		&token.CreatedAt,
		&token.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}

	return &token, nil
}

// Delete deletes a token by ID
func (r *TokenRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM tokens WHERE id = $1`
	result, err := database.DB.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrTokenNotFound
	}

	return nil
}

// DeleteByUserID deletes all tokens for a user
func (r *TokenRepository) DeleteByUserID(ctx context.Context, userID int64) error {
	query := `DELETE FROM tokens WHERE user_id = $1`
	_, err := database.DB.Exec(ctx, query, userID)
	return err
}

