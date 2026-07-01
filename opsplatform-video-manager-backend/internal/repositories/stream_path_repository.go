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
	ErrStreamPathNotFound      = errors.New("stream path not found")
	ErrStreamPathTableIDExists = errors.New("stream path table_id already exists")
)

type StreamPathRepository struct{}

func NewStreamPathRepository() *StreamPathRepository {
	return &StreamPathRepository{}
}

// GetAll retrieves all stream paths, optionally filtered by stream_id
func (r *StreamPathRepository) GetAll(ctx context.Context, streamID *int64) ([]models.StreamPath, error) {
	var query string
	var rows pgx.Rows
	var err error

	if streamID != nil {
		query = `
			SELECT sp.id, sp.stream_id, sp.table_id, sp.full_path, sp.created_at, sp.updated_at,
			       s.id, s.name, s.code, s.created_at, s.updated_at
			FROM stream_paths sp
			JOIN streams s ON sp.stream_id = s.id
			WHERE sp.stream_id = $1
			ORDER BY sp.created_at DESC
		`
		rows, err = database.DB.Query(ctx, query, *streamID)
	} else {
		query = `
			SELECT sp.id, sp.stream_id, sp.table_id, sp.full_path, sp.created_at, sp.updated_at,
			       s.id, s.name, s.code, s.created_at, s.updated_at
			FROM stream_paths sp
			JOIN streams s ON sp.stream_id = s.id
			ORDER BY sp.created_at DESC
		`
		rows, err = database.DB.Query(ctx, query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []models.StreamPath
	for rows.Next() {
		var sp models.StreamPath
		var s models.Stream
		err := rows.Scan(
			&sp.ID, &sp.StreamID, &sp.TableID, &sp.FullPath, &sp.CreatedAt, &sp.UpdatedAt,
			&s.ID, &s.Name, &s.Code, &s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		sp.Stream = &s
		sp.Series = models.StreamSeriesLabel(sp.TableID)
		paths = append(paths, sp)
	}

	return paths, rows.Err()
}

// LookupIDByTableID returns the stream path primary key for a table_id, if any.
func (r *StreamPathRepository) LookupIDByTableID(ctx context.Context, tableID string) (id int64, found bool, err error) {
	err = database.DB.QueryRow(ctx, `SELECT id FROM stream_paths WHERE table_id = $1`, tableID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return id, true, nil
}

// GetByID retrieves a stream path by ID
func (r *StreamPathRepository) GetByID(ctx context.Context, id int64) (*models.StreamPath, error) {
	query := `
		SELECT sp.id, sp.stream_id, sp.table_id, sp.full_path, sp.created_at, sp.updated_at,
		       s.id, s.name, s.code, s.created_at, s.updated_at
		FROM stream_paths sp
		JOIN streams s ON sp.stream_id = s.id
		WHERE sp.id = $1
	`
	var sp models.StreamPath
	var s models.Stream
	err := database.DB.QueryRow(ctx, query, id).Scan(
		&sp.ID, &sp.StreamID, &sp.TableID, &sp.FullPath, &sp.CreatedAt, &sp.UpdatedAt,
		&s.ID, &s.Name, &s.Code, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrStreamPathNotFound
		}
		return nil, err
	}
	sp.Stream = &s
	sp.Series = models.StreamSeriesLabel(sp.TableID)
	return &sp, nil
}

// Create creates a new stream path
func (r *StreamPathRepository) Create(ctx context.Context, streamID int64, tableID, fullPath string) (*models.StreamPath, error) {
	// Verify stream exists
	streamRepo := NewStreamRepository()
	_, err := streamRepo.GetByID(ctx, streamID)
	if err != nil {
		return nil, err
	}

	// Check if table_id already exists
	var tableIDExists bool
	err = database.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM stream_paths WHERE table_id = $1)`, tableID).Scan(&tableIDExists)
	if err != nil {
		return nil, err
	}
	if tableIDExists {
		return nil, ErrStreamPathTableIDExists
	}

	query := `INSERT INTO stream_paths (stream_id, table_id, full_path) VALUES ($1, $2, $3) RETURNING id, stream_id, table_id, full_path, created_at, updated_at`
	var sp models.StreamPath
	err = database.DB.QueryRow(ctx, query, streamID, tableID, fullPath).Scan(
		&sp.ID, &sp.StreamID, &sp.TableID, &sp.FullPath, &sp.CreatedAt, &sp.UpdatedAt,
	)
	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "stream_paths_table_id_unique") || strings.Contains(err.Error(), "idx_stream_paths_table_id_unique") {
			return nil, ErrStreamPathTableIDExists
		}
		return nil, err
	}

	// Load stream information
	sp.Stream, _ = streamRepo.GetByID(ctx, streamID)
	sp.Series = models.StreamSeriesLabel(sp.TableID)
	return &sp, nil
}

// Update updates an existing stream path
func (r *StreamPathRepository) Update(ctx context.Context, id int64, streamID int64, tableID, fullPath string) (*models.StreamPath, error) {
	// Check if path exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Verify stream exists
	streamRepo := NewStreamRepository()
	_, err = streamRepo.GetByID(ctx, streamID)
	if err != nil {
		return nil, err
	}

	// Check if table_id already exists for another path
	var existingTableID int64
	err = database.DB.QueryRow(ctx, `SELECT id FROM stream_paths WHERE table_id = $1`, tableID).Scan(&existingTableID)
	if err == nil && existingTableID != id {
		return nil, ErrStreamPathTableIDExists
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	query := `UPDATE stream_paths SET stream_id = $1, table_id = $2, full_path = $3, updated_at = NOW() WHERE id = $4 RETURNING id, stream_id, table_id, full_path, created_at, updated_at`
	var sp models.StreamPath
	err = database.DB.QueryRow(ctx, query, streamID, tableID, fullPath, id).Scan(
		&sp.ID, &sp.StreamID, &sp.TableID, &sp.FullPath, &sp.CreatedAt, &sp.UpdatedAt,
	)
	if err != nil {
		// Check for unique constraint violation
		if strings.Contains(err.Error(), "stream_paths_table_id_unique") || strings.Contains(err.Error(), "idx_stream_paths_table_id_unique") {
			return nil, ErrStreamPathTableIDExists
		}
		return nil, err
	}

	// Load stream information
	sp.Stream, _ = streamRepo.GetByID(ctx, streamID)
	sp.Series = models.StreamSeriesLabel(sp.TableID)
	return &sp, nil
}

// Delete deletes a stream path
func (r *StreamPathRepository) Delete(ctx context.Context, id int64) error {
	// Check if path exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// Delete associated video stream endpoints first (they are auto-generated)
	_, err = database.DB.Exec(ctx, `DELETE FROM video_stream_endpoints WHERE stream_path_id = $1`, id)
	if err != nil {
		return err
	}

	query := `DELETE FROM stream_paths WHERE id = $1`
	_, err = database.DB.Exec(ctx, query, id)
	return err
}
