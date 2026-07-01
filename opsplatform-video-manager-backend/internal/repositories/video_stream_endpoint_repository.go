// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package repositories

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/pkg/database"
	"github.com/video-manager/backend/pkg/resolution"
	"github.com/video-manager/backend/pkg/url_generator"
)

var (
	ErrVideoStreamEndpointNotFound = errors.New("video stream endpoint not found")
	ErrVideoStreamEndpointExists   = errors.New("video stream endpoint already exists")
)

type VideoStreamEndpointRepository struct{}

func NewVideoStreamEndpointRepository() *VideoStreamEndpointRepository {
	return &VideoStreamEndpointRepository{}
}

// GetAll retrieves all video stream endpoints, optionally filtered
func (r *VideoStreamEndpointRepository) GetAll(ctx context.Context, filters *VideoStreamEndpointFilters) ([]models.VideoStreamEndpoint, error) {
	query := `
		SELECT
			vse.id, vse.provider_id, vse.line_id, vse.domain_id, vse.stream_id, vse.stream_path_id,
			vse.full_url, vse.status, vse.resolution, vse.created_at, vse.updated_at,
			p.id, p.name, p.code, p.created_at, p.updated_at,
			cl.id, cl.provider_id, cl.name, cl.code, cl.created_at, cl.updated_at,
			d.id, d.name, d.created_at, d.updated_at,
			s.id, s.name, s.code, s.created_at, s.updated_at,
			sp.id, sp.stream_id, sp.table_id, sp.full_path, sp.created_at, sp.updated_at
		FROM video_stream_endpoints vse
		JOIN cdn_providers p ON vse.provider_id = p.id
		JOIN cdn_lines cl ON vse.line_id = cl.id
		JOIN domains d ON vse.domain_id = d.id
		JOIN streams s ON vse.stream_id = s.id
		JOIN stream_paths sp ON vse.stream_path_id = sp.id
	`

	args := []interface{}{}
	argPos := 1

	if filters != nil {
		conditions := []string{}
		if filters.LineID != nil {
			conditions = append(conditions, fmt.Sprintf("vse.line_id = $%d", argPos))
			args = append(args, *filters.LineID)
			argPos++
		}
		if filters.DomainID != nil {
			conditions = append(conditions, fmt.Sprintf("vse.domain_id = $%d", argPos))
			args = append(args, *filters.DomainID)
			argPos++
		}
		if filters.StreamID != nil {
			conditions = append(conditions, fmt.Sprintf("vse.stream_id = $%d", argPos))
			args = append(args, *filters.StreamID)
			argPos++
		}
		if filters.ProviderID != nil {
			conditions = append(conditions, fmt.Sprintf("vse.provider_id = $%d", argPos))
			args = append(args, *filters.ProviderID)
			argPos++
		}
		if filters.Status != nil {
			conditions = append(conditions, fmt.Sprintf("vse.status = $%d", argPos))
			args = append(args, *filters.Status)
			argPos++
		}
		if filters.TableID != nil {
			conditions = append(conditions, fmt.Sprintf("sp.table_id = $%d", argPos))
			args = append(args, *filters.TableID)
			argPos++
		}
		if filters.Resolution != nil {
			conditions = append(conditions, fmt.Sprintf("vse.resolution = $%d", argPos))
			args = append(args, *filters.Resolution)
			argPos++
		}
		if len(conditions) > 0 {
			query += " WHERE " + conditions[0]
			for i := 1; i < len(conditions); i++ {
				query += " AND " + conditions[i]
			}
		}
	}

	query += " ORDER BY vse.created_at DESC"

	rows, err := database.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var endpoints []models.VideoStreamEndpoint
	for rows.Next() {
		var vse models.VideoStreamEndpoint
		var p models.CDNProvider
		var cl models.CDNLine
		var d models.Domain
		var s models.Stream
		var sp models.StreamPath

		err := rows.Scan(
			&vse.ID, &vse.ProviderID, &vse.LineID, &vse.DomainID, &vse.StreamID, &vse.StreamPathID,
			&vse.FullURL, &vse.Status, &vse.Resolution, &vse.CreatedAt, &vse.UpdatedAt,
			&p.ID, &p.Name, &p.Code, &p.CreatedAt, &p.UpdatedAt,
			&cl.ID, &cl.ProviderID, &cl.Name, &cl.Code, &cl.CreatedAt, &cl.UpdatedAt,
			&d.ID, &d.Name, &d.CreatedAt, &d.UpdatedAt,
			&s.ID, &s.Name, &s.Code, &s.CreatedAt, &s.UpdatedAt,
			&sp.ID, &sp.StreamID, &sp.TableID, &sp.FullPath, &sp.CreatedAt, &sp.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		sp.Series = models.StreamSeriesLabel(sp.TableID)
		vse.Provider = &p
		vse.Line = &cl
		vse.Domain = &d
		vse.Stream = &s
		vse.StreamPath = &sp
		endpoints = append(endpoints, vse)
	}

	return endpoints, rows.Err()
}

// GetByID retrieves a video stream endpoint by ID
func (r *VideoStreamEndpointRepository) GetByID(ctx context.Context, id int64) (*models.VideoStreamEndpoint, error) {
	query := `
		SELECT
			vse.id, vse.provider_id, vse.line_id, vse.domain_id, vse.stream_id, vse.stream_path_id,
			vse.full_url, vse.status, vse.resolution, vse.created_at, vse.updated_at,
			p.id, p.name, p.code, p.created_at, p.updated_at,
			cl.id, cl.provider_id, cl.name, cl.code, cl.created_at, cl.updated_at,
			d.id, d.name, d.created_at, d.updated_at,
			s.id, s.name, s.code, s.created_at, s.updated_at,
			sp.id, sp.stream_id, sp.table_id, sp.full_path, sp.created_at, sp.updated_at
		FROM video_stream_endpoints vse
		JOIN cdn_providers p ON vse.provider_id = p.id
		JOIN cdn_lines cl ON vse.line_id = cl.id
		JOIN domains d ON vse.domain_id = d.id
		JOIN streams s ON vse.stream_id = s.id
		JOIN stream_paths sp ON vse.stream_path_id = sp.id
		WHERE vse.id = $1
	`

	var vse models.VideoStreamEndpoint
	var p models.CDNProvider
	var cl models.CDNLine
	var d models.Domain
	var s models.Stream
	var sp models.StreamPath

	err := database.DB.QueryRow(ctx, query, id).Scan(
		&vse.ID, &vse.ProviderID, &vse.LineID, &vse.DomainID, &vse.StreamID, &vse.StreamPathID,
		&vse.FullURL, &vse.Status, &vse.Resolution, &vse.CreatedAt, &vse.UpdatedAt,
		&p.ID, &p.Name, &p.Code, &p.CreatedAt, &p.UpdatedAt,
		&cl.ID, &cl.ProviderID, &cl.Name, &cl.Code, &cl.CreatedAt, &cl.UpdatedAt,
		&d.ID, &d.Name, &d.CreatedAt, &d.UpdatedAt,
		&s.ID, &s.Name, &s.Code, &s.CreatedAt, &s.UpdatedAt,
		&sp.ID, &sp.StreamID, &sp.TableID, &sp.FullPath, &sp.CreatedAt, &sp.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrVideoStreamEndpointNotFound
		}
		return nil, err
	}

	sp.Series = models.StreamSeriesLabel(sp.TableID)
	vse.Provider = &p
	vse.Line = &cl
	vse.Domain = &d
	vse.Stream = &s
	vse.StreamPath = &sp
	return &vse, nil
}

// Create creates a new video stream endpoint
func (r *VideoStreamEndpointRepository) Create(ctx context.Context, providerID, lineID, domainID, streamID, streamPathID int64, status int) (*models.VideoStreamEndpoint, error) {
	// Verify all referenced entities exist
	lineRepo := NewCDNLineRepository()
	line, err := lineRepo.GetByID(ctx, lineID)
	if err != nil {
		return nil, err
	}

	domainRepo := NewDomainRepository()
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		return nil, ErrDomainNotFound
	}

	streamPathRepo := NewStreamPathRepository()
	streamPath, err := streamPathRepo.GetByID(ctx, streamPathID)
	if err != nil {
		return nil, ErrStreamPathNotFound
	}

	// Check if endpoint already exists
	var exists bool
	err = database.DB.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM video_stream_endpoints WHERE line_id = $1 AND domain_id = $2 AND stream_path_id = $3)`,
		lineID, domainID, streamPathID,
	).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrVideoStreamEndpointExists
	}

	// Generate full URL
	fullURL := url_generator.GenerateEndpointURL(line.Name, domain.Name, streamPath.FullPath)

	// Detect resolution from stream path
	detectedResolution := resolution.DetectResolutionFromPath(streamPath.FullPath)

	// Set default status if not provided
	if status == 0 {
		status = 1
	}

	query := `
		INSERT INTO video_stream_endpoints (provider_id, line_id, domain_id, stream_id, stream_path_id, full_url, status, resolution)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, provider_id, line_id, domain_id, stream_id, stream_path_id, full_url, status, resolution, created_at, updated_at
	`

	var vse models.VideoStreamEndpoint
	err = database.DB.QueryRow(ctx, query, providerID, lineID, domainID, streamID, streamPathID, fullURL, status, detectedResolution).Scan(
		&vse.ID, &vse.ProviderID, &vse.LineID, &vse.DomainID, &vse.StreamID, &vse.StreamPathID,
		&vse.FullURL, &vse.Status, &vse.Resolution, &vse.CreatedAt, &vse.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Load all related entities
	vse.Provider, _ = NewCDNProviderRepository().GetByID(ctx, providerID)
	vse.Line = line
	vse.Domain = domain
	vse.Stream, _ = NewStreamRepository().GetByID(ctx, streamID)
	vse.StreamPath = streamPath

	return &vse, nil
}

// Update updates an existing video stream endpoint
func (r *VideoStreamEndpointRepository) Update(ctx context.Context, id int64, providerID, lineID, domainID, streamID, streamPathID int64, status int) (*models.VideoStreamEndpoint, error) {
	// Check if endpoint exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Verify all referenced entities exist
	lineRepo := NewCDNLineRepository()
	line, err := lineRepo.GetByID(ctx, lineID)
	if err != nil {
		return nil, err
	}

	domainRepo := NewDomainRepository()
	domain, err := domainRepo.GetByID(ctx, domainID)
	if err != nil {
		return nil, ErrDomainNotFound
	}

	streamPathRepo := NewStreamPathRepository()
	streamPath, err := streamPathRepo.GetByID(ctx, streamPathID)
	if err != nil {
		return nil, ErrStreamPathNotFound
	}

	// Check if new combination already exists (excluding current endpoint)
	var exists bool
	err = database.DB.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM video_stream_endpoints WHERE line_id = $1 AND domain_id = $2 AND stream_path_id = $3 AND id != $4)`,
		lineID, domainID, streamPathID, id,
	).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrVideoStreamEndpointExists
	}

	// Regenerate full URL
	fullURL := url_generator.GenerateEndpointURL(line.Name, domain.Name, streamPath.FullPath)

	// Re-detect resolution from stream path
	detectedResolution := resolution.DetectResolutionFromPath(streamPath.FullPath)

	query := `
		UPDATE video_stream_endpoints
		SET provider_id = $1, line_id = $2, domain_id = $3, stream_id = $4, stream_path_id = $5, full_url = $6, status = $7, resolution = $8, updated_at = NOW()
		WHERE id = $9
		RETURNING id, provider_id, line_id, domain_id, stream_id, stream_path_id, full_url, status, resolution, created_at, updated_at
	`

	var vse models.VideoStreamEndpoint
	err = database.DB.QueryRow(ctx, query, providerID, lineID, domainID, streamID, streamPathID, fullURL, status, detectedResolution, id).Scan(
		&vse.ID, &vse.ProviderID, &vse.LineID, &vse.DomainID, &vse.StreamID, &vse.StreamPathID,
		&vse.FullURL, &vse.Status, &vse.Resolution, &vse.CreatedAt, &vse.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Load all related entities
	vse.Provider, _ = NewCDNProviderRepository().GetByID(ctx, providerID)
	vse.Line = line
	vse.Domain = domain
	vse.Stream, _ = NewStreamRepository().GetByID(ctx, streamID)
	vse.StreamPath = streamPath

	return &vse, nil
}

// Delete deletes a video stream endpoint
func (r *VideoStreamEndpointRepository) Delete(ctx context.Context, id int64) error {
	// Check if endpoint exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	query := `DELETE FROM video_stream_endpoints WHERE id = $1`
	_, err = database.DB.Exec(ctx, query, id)
	return err
}

// GenerateAll generates all possible video stream endpoints based on all combinations of
// CDN lines, domains, and stream paths. It deletes all existing endpoints first, then
// creates new ones based on current data.
func (r *VideoStreamEndpointRepository) GenerateAll(ctx context.Context) (int, error) {
	// First, delete all existing endpoints to ensure we start fresh
	// This ensures that endpoints for deleted providers/lines/domains/streams/paths are removed
	_, err := database.DB.Exec(ctx, `DELETE FROM video_stream_endpoints`)
	if err != nil {
		return 0, err
	}

	// Get all lines, domains, and stream paths
	lineRepo := NewCDNLineRepository()
	lines, err := lineRepo.GetAll(ctx, nil)
	if err != nil {
		return 0, err
	}

	domainRepo := NewDomainRepository()
	domains, err := domainRepo.GetAll(ctx)
	if err != nil {
		return 0, err
	}

	streamPathRepo := NewStreamPathRepository()
	streamPaths, err := streamPathRepo.GetAll(ctx, nil)
	if err != nil {
		return 0, err
	}

	// Get stream repository to match stream paths with streams
	streamRepo := NewStreamRepository()
	streams, err := streamRepo.GetAll(ctx)
	if err != nil {
		return 0, err
	}

	// Create a map of stream_id -> stream for quick lookup
	streamMap := make(map[int64]*models.Stream)
	for i := range streams {
		streamMap[streams[i].ID] = &streams[i]
	}

	// Create a map of stream_path_id -> stream_id
	streamPathMap := make(map[int64]int64)
	for i := range streamPaths {
		streamPathMap[streamPaths[i].ID] = streamPaths[i].StreamID
	}

	// Generate endpoints for all combinations
	generatedCount := 0
	for _, line := range lines {
		// Match line with stream based on line name
		for _, streamPath := range streamPaths {
			streamID := streamPathMap[streamPath.ID]
			stream, ok := streamMap[streamID]
			if !ok {
				continue
			}

			// Check if line code matches stream code (e.g., line code "kkw" matches stream code "kkw")
			if line.Code != stream.Code {
				continue
			}

			// Check provider match: if stream.provider_id is NULL, match all providers
			// if stream.provider_id is set, only match lines from that provider
			if stream.ProviderID != nil && line.ProviderID != *stream.ProviderID {
				continue
			}

			for _, domain := range domains {
				// Generate full URL
				fullURL := url_generator.GenerateEndpointURL(line.Name, domain.Name, streamPath.FullPath)

				// Detect resolution from stream path
				detectedResolution := resolution.DetectResolutionFromPath(streamPath.FullPath)

				// Create new endpoint (we already deleted all existing ones)
				_, err := database.DB.Exec(ctx,
					`INSERT INTO video_stream_endpoints (provider_id, line_id, domain_id, stream_id, stream_path_id, full_url, status, resolution)
					 VALUES ($1, $2, $3, $4, $5, $6, 1, $7)`,
					line.ProviderID, line.ID, domain.ID, streamID, streamPath.ID, fullURL, detectedResolution,
				)
				if err == nil {
					generatedCount++
				}
			}
		}
	}

	return generatedCount, nil
}

// UpdateStatus updates the status of a video stream endpoint
func (r *VideoStreamEndpointRepository) UpdateStatus(ctx context.Context, id int64, status int) error {
	// Check if endpoint exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	query := `UPDATE video_stream_endpoints SET status = $1, updated_at = NOW() WHERE id = $2`
	_, err = database.DB.Exec(ctx, query, status, id)
	return err
}

// UpdateResolution updates the resolution of a video stream endpoint
func (r *VideoStreamEndpointRepository) UpdateResolution(ctx context.Context, id int64, resolution string) error {
	// Check if endpoint exists
	_, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	query := `UPDATE video_stream_endpoints SET resolution = $1, updated_at = NOW() WHERE id = $2`
	_, err = database.DB.Exec(ctx, query, resolution, id)
	return err
}

// VideoStreamEndpointFilters represents filters for querying endpoints
type VideoStreamEndpointFilters struct {
	ProviderID *int64
	LineID     *int64
	DomainID   *int64
	StreamID   *int64
	Status     *int
	TableID    *string // Filter by table_id (桌台号)
	Resolution *string // Filter by resolution (普清, 高清, 超清)
}
