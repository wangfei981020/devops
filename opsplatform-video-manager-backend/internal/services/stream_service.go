// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"
	"strings"

	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
)

type StreamService struct {
	repo *repositories.StreamRepository
}

func NewStreamService() *StreamService {
	return &StreamService{
		repo: repositories.NewStreamRepository(),
	}
}

// GetAll retrieves all streams
func (s *StreamService) GetAll(ctx context.Context) ([]models.Stream, error) {
	return s.repo.GetAll(ctx)
}

// GetByID retrieves a stream by ID
func (s *StreamService) GetByID(ctx context.Context, id int64) (*models.Stream, error) {
	return s.repo.GetByID(ctx, id)
}

// Create creates a new stream
func (s *StreamService) Create(ctx context.Context, req models.CreateStreamRequest) (*models.Stream, error) {
	// Normalize input
	name := strings.TrimSpace(req.Name)
	code := strings.TrimSpace(req.Code)
	stream, err := s.repo.Create(ctx, name, code, req.ProviderID)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return stream, nil
}

// Update updates an existing stream
func (s *StreamService) Update(ctx context.Context, id int64, req models.UpdateStreamRequest) (*models.Stream, error) {
	// Normalize input
	name := strings.TrimSpace(req.Name)
	code := strings.TrimSpace(req.Code)
	stream, err := s.repo.Update(ctx, id, name, code, req.ProviderID)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return stream, nil
}

// Delete deletes a stream
func (s *StreamService) Delete(ctx context.Context, id int64) error {
	err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return nil
}

