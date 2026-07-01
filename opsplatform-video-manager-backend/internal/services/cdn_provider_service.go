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

type CDNProviderService struct {
	repo *repositories.CDNProviderRepository
}

func NewCDNProviderService() *CDNProviderService {
	return &CDNProviderService{
		repo: repositories.NewCDNProviderRepository(),
	}
}

// GetAll retrieves all CDN providers
func (s *CDNProviderService) GetAll(ctx context.Context) ([]models.CDNProvider, error) {
	return s.repo.GetAll(ctx)
}

// GetByID retrieves a CDN provider by ID
func (s *CDNProviderService) GetByID(ctx context.Context, id int64) (*models.CDNProvider, error) {
	return s.repo.GetByID(ctx, id)
}

// Create creates a new CDN provider
func (s *CDNProviderService) Create(ctx context.Context, req models.CreateCDNProviderRequest) (*models.CDNProvider, error) {
	// Normalize input
	name := strings.TrimSpace(req.Name)
	code := strings.TrimSpace(req.Code)

	// Validate code format (alphanumeric, underscore, hyphen only)
	if !isValidCode(code) {
		return nil, repositories.ErrInvalidCodeFormat
	}

	provider, err := s.repo.Create(ctx, name, code)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return provider, nil
}

// Update updates an existing CDN provider
func (s *CDNProviderService) Update(ctx context.Context, id int64, req models.UpdateCDNProviderRequest) (*models.CDNProvider, error) {
	// Normalize input
	name := strings.TrimSpace(req.Name)
	code := strings.TrimSpace(req.Code)

	// Validate code format
	if !isValidCode(code) {
		return nil, repositories.ErrInvalidCodeFormat
	}

	provider, err := s.repo.Update(ctx, id, name, code)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return provider, nil
}

// Delete deletes a CDN provider
func (s *CDNProviderService) Delete(ctx context.Context, id int64) error {
	err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return nil
}

// isValidCode validates that code contains only alphanumeric characters, underscores, and hyphens
func isValidCode(code string) bool {
	if len(code) == 0 {
		return false
	}
	for _, r := range code {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
