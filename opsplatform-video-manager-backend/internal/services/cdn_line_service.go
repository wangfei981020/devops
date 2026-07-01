// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"

	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
)

type CDNLineService struct {
	repo *repositories.CDNLineRepository
}

func NewCDNLineService() *CDNLineService {
	return &CDNLineService{
		repo: repositories.NewCDNLineRepository(),
	}
}

// GetAll retrieves all CDN lines with optional provider filter
func (s *CDNLineService) GetAll(ctx context.Context, providerID *int64) ([]models.CDNLine, error) {
	return s.repo.GetAll(ctx, providerID)
}

// GetByID retrieves a CDN line by ID
func (s *CDNLineService) GetByID(ctx context.Context, id int64) (*models.CDNLine, error) {
	return s.repo.GetByID(ctx, id)
}

// Create creates a new CDN line
func (s *CDNLineService) Create(ctx context.Context, req models.CreateCDNLineRequest) (*models.CDNLine, error) {
	line, err := s.repo.Create(ctx, req.ProviderID, req.Name, req.Code)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return line, nil
}

// Update updates an existing CDN line
func (s *CDNLineService) Update(ctx context.Context, id int64, req models.UpdateCDNLineRequest) (*models.CDNLine, error) {
	line, err := s.repo.Update(ctx, id, req.ProviderID, req.Name, req.Code)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return line, nil
}

// Delete deletes a CDN line
func (s *CDNLineService) Delete(ctx context.Context, id int64) error {
	err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return nil
}

