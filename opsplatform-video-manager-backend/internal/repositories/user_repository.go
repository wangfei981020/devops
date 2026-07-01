// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package repositories

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/pkg/database"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("user already exists")
)

type UserRepository struct{}

func NewUserRepository() *UserRepository {
	return &UserRepository{}
}

// GetByUsername retrieves a user by username
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	query := `SELECT id, username, password_hash, is_admin, created_at, updated_at
	          FROM users WHERE username = $1`

	err := database.DB.QueryRow(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.IsAdmin,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return &user, nil
}

// GetByID retrieves a user by ID
func (r *UserRepository) GetByID(ctx context.Context, id int64) (*models.User, error) {
	var user models.User
	query := `SELECT id, username, password_hash, is_admin, created_at, updated_at
	          FROM users WHERE id = $1`

	err := database.DB.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.IsAdmin,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return &user, nil
}

// UpdatePassword updates a user's password
func (r *UserRepository) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	query := `UPDATE users SET password_hash = $1, updated_at = NOW() WHERE id = $2`
	result, err := database.DB.Exec(ctx, query, passwordHash, userID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

// Create creates a new user (for admin to create users)
func (r *UserRepository) Create(ctx context.Context, username, passwordHash string, isAdmin bool) (*models.User, error) {
	var user models.User
	query := `INSERT INTO users (username, password_hash, is_admin)
	          VALUES ($1, $2, $3)
	          RETURNING id, username, password_hash, is_admin, created_at, updated_at`

	err := database.DB.QueryRow(ctx, query, username, passwordHash, isAdmin).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.IsAdmin,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		// Check for unique constraint violation
		if pgErr, ok := err.(interface{ Code() string }); ok && pgErr.Code() == "23505" {
			return nil, ErrUserExists
		}
		return nil, err
	}

	return &user, nil
}

// InitAdminUser initializes the admin user if it doesn't exist
func (r *UserRepository) InitAdminUser(ctx context.Context, username, passwordHash string) error {
	// Check if admin user already exists
	_, err := r.GetByUsername(ctx, username)
	if err == nil {
		// User already exists, skip
		return nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		// Some other error occurred
		return err
	}

	// Create admin user
	_, err = r.Create(ctx, username, passwordHash, true)
	return err
}

// GetAll retrieves all users.
func (r *UserRepository) GetAll(ctx context.Context) ([]*models.User, error) {
	query := `SELECT id, username, password_hash, is_admin, created_at, updated_at
	          FROM users
	          ORDER BY id ASC`
	rows, err := database.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*models.User, 0)
	for rows.Next() {
		var user models.User
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.PasswordHash,
			&user.IsAdmin,
			&user.CreatedAt,
			&user.UpdatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

// Update updates username and admin flag for a user.
func (r *UserRepository) Update(ctx context.Context, id int64, username string, isAdmin bool) (*models.User, error) {
	var user models.User
	query := `UPDATE users
	          SET username = $1, is_admin = $2, updated_at = NOW()
	          WHERE id = $3
	          RETURNING id, username, password_hash, is_admin, created_at, updated_at`

	err := database.DB.QueryRow(ctx, query, username, isAdmin, id).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.IsAdmin,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		if pgErr, ok := err.(interface{ Code() string }); ok && pgErr.Code() == "23505" {
			return nil, ErrUserExists
		}
		return nil, err
	}
	return &user, nil
}

// Delete deletes a user by ID.
func (r *UserRepository) Delete(ctx context.Context, id int64) error {
	query := `DELETE FROM users WHERE id = $1`
	result, err := database.DB.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// CountAdmins returns the number of admin users.
func (r *UserRepository) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	query := `SELECT COUNT(*) FROM users WHERE is_admin = true`
	if err := database.DB.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
