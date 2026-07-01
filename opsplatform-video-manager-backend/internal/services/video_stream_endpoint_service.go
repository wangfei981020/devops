// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"
	"time"

	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
	"github.com/video-manager/backend/pkg/resolution"
)

type VideoStreamEndpointService struct {
	repo *repositories.VideoStreamEndpointRepository
}

func NewVideoStreamEndpointService() *VideoStreamEndpointService {
	return &VideoStreamEndpointService{
		repo: repositories.NewVideoStreamEndpointRepository(),
	}
}

// GetAll retrieves all video stream endpoints, optionally filtered
func (s *VideoStreamEndpointService) GetAll(ctx context.Context, filters *repositories.VideoStreamEndpointFilters) ([]models.VideoStreamEndpoint, error) {
	return s.repo.GetAll(ctx, filters)
}

// GetByID retrieves a video stream endpoint by ID
func (s *VideoStreamEndpointService) GetByID(ctx context.Context, id int64) (*models.VideoStreamEndpoint, error) {
	return s.repo.GetByID(ctx, id)
}

// Create creates a new video stream endpoint
func (s *VideoStreamEndpointService) Create(ctx context.Context, req models.CreateVideoStreamEndpointRequest) (*models.VideoStreamEndpoint, error) {
	status := req.Status
	if status == 0 {
		status = 1 // Default to enabled
	}
	return s.repo.Create(ctx, req.ProviderID, req.LineID, req.DomainID, req.StreamID, req.StreamPathID, status)
}

// Update updates an existing video stream endpoint
func (s *VideoStreamEndpointService) Update(ctx context.Context, id int64, req models.UpdateVideoStreamEndpointRequest) (*models.VideoStreamEndpoint, error) {
	status := req.Status
	if status == 0 {
		status = 1 // Default to enabled
	}
	return s.repo.Update(ctx, id, req.ProviderID, req.LineID, req.DomainID, req.StreamID, req.StreamPathID, status)
}

// Delete deletes a video stream endpoint
func (s *VideoStreamEndpointService) Delete(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

// GenerateAll generates all possible video stream endpoints based on all combinations
func (s *VideoStreamEndpointService) GenerateAll(ctx context.Context) (int, error) {
	return s.repo.GenerateAll(ctx)
}

// UpdateStatus updates the status of a video stream endpoint
func (s *VideoStreamEndpointService) UpdateStatus(ctx context.Context, id int64, status int) error {
	return s.repo.UpdateStatus(ctx, id, status)
}

// TestResolution tests video stream and detects resolution, then updates the endpoint
func (s *VideoStreamEndpointService) TestResolution(ctx context.Context, id int64) (string, error) {
	// Get endpoint
	endpoint, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}

	// Try to detect resolution from stream (with 10 second timeout)
	detectedResolution, err := resolution.DetectResolutionFromStream(endpoint.FullURL, 10*time.Second)
	if err != nil {
		// If stream detection fails, fall back to path-based detection
		if endpoint.StreamPath != nil {
			detectedResolution = resolution.DetectResolutionFromPath(endpoint.StreamPath.FullPath)
		} else {
			detectedResolution = "普清" // Default
		}
	}

	// Update resolution in database
	err = s.repo.UpdateResolution(ctx, id, detectedResolution)
	if err != nil {
		return "", err
	}

	return detectedResolution, nil
}

