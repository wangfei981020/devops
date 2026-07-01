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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/pkg/database"
)

var (
	ErrStreamNotFound   = errors.New("stream not found")
	ErrStreamCodeExists = errors.New("stream code already exists")
	ErrStreamNameExists = errors.New("stream name already exists")
)

type StreamRepository struct{}

func NewStreamRepository() *StreamRepository {
	return &StreamRepository{}
}

// GetAll retrieves all streams
func (r *StreamRepository) GetAll(ctx context.Context) ([]models.Stream, error) {
	query := `SELECT s.id, s.name, s.code, s.provider_id, s.created_at, s.updated_at,
	          p.id, p.name, p.code, p.created_at, p.updated_at
	          FROM streams s
	          LEFT JOIN cdn_providers p ON s.provider_id = p.id
	          ORDER BY s.created_at DESC`
	rows, err := database.DB.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []models.Stream
	for rows.Next() {
		var s models.Stream
		var providerID pgtype.Int8
		var pID pgtype.Int8
		var pName, pCode pgtype.Text
		var pCreatedAt, pUpdatedAt pgtype.Timestamptz
		err := rows.Scan(&s.ID, &s.Name, &s.Code, &providerID, &s.CreatedAt, &s.UpdatedAt,
			&pID, &pName, &pCode, &pCreatedAt, &pUpdatedAt)
		if err != nil {
			return nil, err
		}
		if providerID.Valid {
			s.ProviderID = &providerID.Int64
		} else {
			s.ProviderID = nil
		}
		if pID.Valid {
			var p models.CDNProvider
			p.ID = pID.Int64
			if pName.Valid {
				p.Name = pName.String
			}
			if pCode.Valid {
				p.Code = pCode.String
			}
			if pCreatedAt.Valid {
				p.CreatedAt = pCreatedAt.Time
			}
			if pUpdatedAt.Valid {
				p.UpdatedAt = pUpdatedAt.Time
			}
			s.Provider = &p
		}
		streams = append(streams, s)
	}

	return streams, rows.Err()
}

// GetByID retrieves a stream by ID
func (r *StreamRepository) GetByID(ctx context.Context, id int64) (*models.Stream, error) {
	query := `SELECT s.id, s.name, s.code, s.provider_id, s.created_at, s.updated_at,
	          p.id, p.name, p.code, p.created_at, p.updated_at
	          FROM streams s
	          LEFT JOIN cdn_providers p ON s.provider_id = p.id
	          WHERE s.id = $1`
	var s models.Stream
	var providerID pgtype.Int8
	var pID pgtype.Int8
	var pName, pCode pgtype.Text
	var pCreatedAt, pUpdatedAt pgtype.Timestamptz
	err := database.DB.QueryRow(ctx, query, id).Scan(&s.ID, &s.Name, &s.Code, &providerID, &s.CreatedAt, &s.UpdatedAt,
		&pID, &pName, &pCode, &pCreatedAt, &pUpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrStreamNotFound
		}
		return nil, err
	}
	if providerID.Valid {
		s.ProviderID = &providerID.Int64
	} else {
		s.ProviderID = nil
	}
	if pID.Valid {
		var p models.CDNProvider
		p.ID = pID.Int64
		if pName.Valid {
			p.Name = pName.String
		}
		if pCode.Valid {
			p.Code = pCode.String
		}
		if pCreatedAt.Valid {
			p.CreatedAt = pCreatedAt.Time
		}
		if pUpdatedAt.Valid {
			p.UpdatedAt = pUpdatedAt.Time
		}
		s.Provider = &p
	}
	return &s, nil
}

// Create creates a new stream
func (r *StreamRepository) Create(ctx context.Context, name, code string, providerID *int64) (*models.Stream, error) {
	// Check if stream code already exists
	var codeExists bool
	err := database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM streams WHERE code = $1)`, code).Scan(&codeExists)
	if err != nil {
		return nil, err
	}
	if codeExists {
		return nil, ErrStreamCodeExists
	}

	// Check if stream name already exists
	var nameExists bool
	err = database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM streams WHERE name = $1)`, name).Scan(&nameExists)
	if err != nil {
		return nil, err
	}
	if nameExists {
		return nil, ErrStreamNameExists
	}

	// Verify provider exists if providerID is provided
	if providerID != nil {
		providerRepo := NewCDNProviderRepository()
		_, err := providerRepo.GetByID(ctx, *providerID)
		if err != nil {
			return nil, err
		}
	}

	query := `INSERT INTO streams (name, code, provider_id) VALUES ($1, $2, $3) RETURNING id, name, code, provider_id, created_at, updated_at`
	var s models.Stream
	err = database.DB.QueryRow(ctx, query, name, code, providerID).Scan(&s.ID, &s.Name, &s.Code, &s.ProviderID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "streams_name_unique") || strings.Contains(err.Error(), "idx_streams_name_unique") {
			return nil, ErrStreamNameExists
		}
		if strings.Contains(err.Error(), "streams_code_unique") || strings.Contains(err.Error(), "idx_streams_code") {
			return nil, ErrStreamCodeExists
		}
		return nil, err
	}

	// Load provider if providerID is set
	if s.ProviderID != nil {
		providerRepo := NewCDNProviderRepository()
		s.Provider, _ = providerRepo.GetByID(ctx, *s.ProviderID)
	}

	return &s, nil
}

// Update updates an existing stream
func (r *StreamRepository) Update(ctx context.Context, id int64, name, code string, providerID *int64) (*models.Stream, error) {
	// Check if stream exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check if new code already exists (excluding current stream)
	var codeExists bool
	err = database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM streams WHERE code = $1 AND id != $2)`, code, id).Scan(&codeExists)
	if err != nil {
		return nil, err
	}
	if codeExists {
		return nil, ErrStreamCodeExists
	}

	// Check if new name already exists (excluding current stream)
	var nameExists bool
	err = database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM streams WHERE name = $1 AND id != $2)`, name, id).Scan(&nameExists)
	if err != nil {
		return nil, err
	}
	if nameExists {
		return nil, ErrStreamNameExists
	}

	// Verify provider exists if providerID is provided
	if providerID != nil {
		providerRepo := NewCDNProviderRepository()
		_, err := providerRepo.GetByID(ctx, *providerID)
		if err != nil {
			return nil, err
		}
	}

	query := `UPDATE streams SET name = $1, code = $2, provider_id = $3, updated_at = NOW() WHERE id = $4 RETURNING id, name, code, provider_id, created_at, updated_at`
	var s models.Stream
	err = database.DB.QueryRow(ctx, query, name, code, providerID, id).Scan(&s.ID, &s.Name, &s.Code, &s.ProviderID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "streams_name_unique") || strings.Contains(err.Error(), "idx_streams_name_unique") {
			return nil, ErrStreamNameExists
		}
		if strings.Contains(err.Error(), "streams_code_unique") || strings.Contains(err.Error(), "idx_streams_code") {
			return nil, ErrStreamCodeExists
		}
		return nil, err
	}

	// Load provider if providerID is set
	if s.ProviderID != nil {
		providerRepo := NewCDNProviderRepository()
		s.Provider, _ = providerRepo.GetByID(ctx, *s.ProviderID)
	}

	return &s, nil
}

// Delete deletes a stream
func (r *StreamRepository) Delete(ctx context.Context, id int64) error {
	// Check if stream exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}


	// Delete associated video stream endpoints first (they are auto-generated)
	_, err = database.DB.Exec(ctx, `DELETE FROM video_stream_endpoints WHERE stream_id = $1`, id)
	if err != nil {
		return err
	}

	query := `DELETE FROM streams WHERE id = $1`
	_, err = database.DB.Exec(ctx, query, id)
	return err
}

