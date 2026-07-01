// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package repositories

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/pkg/database"
)

var (
	ErrProviderNotFound   = errors.New("cdn provider not found")
	ErrProviderCodeExists = errors.New("cdn provider code already exists")
	ErrProviderNameExists = errors.New("cdn provider name already exists")
	ErrInvalidCodeFormat  = errors.New("code can only contain letters, numbers, underscores and hyphens")
)

type CDNProviderRepository struct{}

func NewCDNProviderRepository() *CDNProviderRepository {
	return &CDNProviderRepository{}
}

// GetAll retrieves all CDN providers
func (r *CDNProviderRepository) GetAll(ctx context.Context) ([]models.CDNProvider, error) {
	query := `SELECT id, name, code, created_at, updated_at FROM cdn_providers ORDER BY created_at DESC`
	rows, err := database.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.CDNProvider
	for rows.Next() {
		var p models.CDNProvider
		if err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}

	return providers, rows.Err()
}

// GetByID retrieves a CDN provider by ID
func (r *CDNProviderRepository) GetByID(ctx context.Context, id int64) (*models.CDNProvider, error) {
	query := `SELECT id, name, code, created_at, updated_at FROM cdn_providers WHERE id = $1`
	var p models.CDNProvider
	err := database.DB.QueryRow(ctx, query, id).Scan(
		&p.ID, &p.Name, &p.Code, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProviderNotFound
		}
		return nil, err
	}
	return &p, nil
}

// Create creates a new CDN provider
func (r *CDNProviderRepository) Create(ctx context.Context, name, code string) (*models.CDNProvider, error) {
	// Check if code already exists
	var codeExists bool
	err := database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cdn_providers WHERE code = $1)`, code).Scan(&codeExists)
	if err != nil {
		return nil, err
	}
	if codeExists {
		return nil, ErrProviderCodeExists
	}

	// Check if name already exists
	var nameExists bool
	err = database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cdn_providers WHERE name = $1)`, name).Scan(&nameExists)
	if err != nil {
		return nil, err
	}
	if nameExists {
		return nil, ErrProviderNameExists
	}

	query := `INSERT INTO cdn_providers (name, code, created_at, updated_at)
	          VALUES ($1, $2, NOW(), NOW()) RETURNING id, name, code, created_at, updated_at`
	var p models.CDNProvider
	err = database.DB.QueryRow(ctx, query, name, code).Scan(
		&p.ID, &p.Name, &p.Code, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "cdn_providers_name_unique") || strings.Contains(err.Error(), "idx_cdn_providers_name_unique") {
			return nil, ErrProviderNameExists
		}
		if strings.Contains(err.Error(), "cdn_providers_code_unique") || strings.Contains(err.Error(), "idx_cdn_providers_code") {
			return nil, ErrProviderCodeExists
		}
		return nil, err
	}
	return &p, nil
}

// Update updates an existing CDN provider
func (r *CDNProviderRepository) Update(ctx context.Context, id int64, name, code string) (*models.CDNProvider, error) {
	// Check if provider exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check if code already exists for another provider
	var existingCodeID int64
	err = database.DB.QueryRow(ctx, `SELECT id FROM cdn_providers WHERE code = $1`, code).Scan(&existingCodeID)
	if err == nil && existingCodeID != id {
		return nil, ErrProviderCodeExists
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Check if name already exists for another provider
	var existingNameID int64
	err = database.DB.QueryRow(ctx, `SELECT id FROM cdn_providers WHERE name = $1`, name).Scan(&existingNameID)
	if err == nil && existingNameID != id {
		return nil, ErrProviderNameExists
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	query := `UPDATE cdn_providers SET name = $1, code = $2, updated_at = NOW()
	          WHERE id = $3 RETURNING id, name, code, created_at, updated_at`
	var p models.CDNProvider
	err = database.DB.QueryRow(ctx, query, name, code, id).Scan(
		&p.ID, &p.Name, &p.Code, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "cdn_providers_name_unique") || strings.Contains(err.Error(), "idx_cdn_providers_name_unique") {
			return nil, ErrProviderNameExists
		}
		if strings.Contains(err.Error(), "cdn_providers_code_unique") || strings.Contains(err.Error(), "idx_cdn_providers_code") {
			return nil, ErrProviderCodeExists
		}
		return nil, err
	}
	return &p, nil
}

// Delete deletes a CDN provider
func (r *CDNProviderRepository) Delete(ctx context.Context, id int64) error {
	// Check if provider exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Check if provider has associated lines
	var lineCount int
	err = database.DB.QueryRow(ctx, `SELECT COUNT(*) FROM cdn_lines WHERE provider_id = $1`, id).Scan(&lineCount)
	if err != nil {
		return err
	}
	if lineCount > 0 {
		return errors.New("无法删除厂商：该厂商下还有关联的线路，请先删除所有关联的线路")
	}

	// Delete associated video stream endpoints first (they are auto-generated)
	_, err = database.DB.Exec(ctx, `DELETE FROM video_stream_endpoints WHERE provider_id = $1`, id)
	if err != nil {
		return err
	}

	query := `DELETE FROM cdn_providers WHERE id = $1`
	_, err = database.DB.Exec(ctx, query, id)
	return err
}
