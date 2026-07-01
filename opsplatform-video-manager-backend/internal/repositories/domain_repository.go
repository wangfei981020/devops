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
	ErrDomainNotFound = errors.New("domain not found")
	ErrDomainNameExists = errors.New("domain name already exists")
)

type DomainRepository struct{}

func NewDomainRepository() *DomainRepository {
	return &DomainRepository{}
}

// GetAll retrieves all domains
func (r *DomainRepository) GetAll(ctx context.Context) ([]models.Domain, error) {
	query := `SELECT id, name, created_at, updated_at FROM domains ORDER BY created_at DESC`
	rows, err := database.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []models.Domain
	for rows.Next() {
		var d models.Domain
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}

	return domains, rows.Err()
}

// GetByID retrieves a domain by ID
func (r *DomainRepository) GetByID(ctx context.Context, id int64) (*models.Domain, error) {
	query := `SELECT id, name, created_at, updated_at FROM domains WHERE id = $1`
	var d models.Domain
	err := database.DB.QueryRow(ctx, query, id).Scan(&d.ID, &d.Name, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrDomainNotFound
		}
		return nil, err
	}
	return &d, nil
}

// Create creates a new domain
func (r *DomainRepository) Create(ctx context.Context, name string) (*models.Domain, error) {
	// Check if domain name already exists
	var exists bool
	err := database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domains WHERE name = $1)`, name).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDomainNameExists
	}

	query := `INSERT INTO domains (name) VALUES ($1) RETURNING id, name, created_at, updated_at`
	var d models.Domain
	err = database.DB.QueryRow(ctx, query, name).Scan(&d.ID, &d.Name, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Update updates an existing domain
func (r *DomainRepository) Update(ctx context.Context, id int64, name string) (*models.Domain, error) {
	// Check if domain exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check if new name already exists (excluding current domain)
	var exists bool
	err = database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM domains WHERE name = $1 AND id != $2)`, name, id).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDomainNameExists
	}

	query := `UPDATE domains SET name = $1, updated_at = NOW() WHERE id = $2 RETURNING id, name, created_at, updated_at`
	var d models.Domain
	err = database.DB.QueryRow(ctx, query, name, id).Scan(&d.ID, &d.Name, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Delete deletes a domain
func (r *DomainRepository) Delete(ctx context.Context, id int64) error {
	// Check if domain exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}


	// Delete associated video stream endpoints first (they are auto-generated)
	_, err = database.DB.Exec(ctx, `DELETE FROM video_stream_endpoints WHERE domain_id = $1`, id)
	if err != nil {
		return err
	}

	query := `DELETE FROM domains WHERE id = $1`
	_, err = database.DB.Exec(ctx, query, id)
	return err
}

