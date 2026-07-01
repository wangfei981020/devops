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

type DomainService struct {
	repo *repositories.DomainRepository
}

func NewDomainService() *DomainService {
	return &DomainService{
		repo: repositories.NewDomainRepository(),
	}
}

// GetAll retrieves all domains
func (s *DomainService) GetAll(ctx context.Context) ([]models.Domain, error) {
	return s.repo.GetAll(ctx)
}

// GetByID retrieves a domain by ID
func (s *DomainService) GetByID(ctx context.Context, id int64) (*models.Domain, error) {
	return s.repo.GetByID(ctx, id)
}

// Create creates a new domain
func (s *DomainService) Create(ctx context.Context, req models.CreateDomainRequest) (*models.Domain, error) {
	// Normalize input
	name := strings.TrimSpace(req.Name)
	domain, err := s.repo.Create(ctx, name)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return domain, nil
}

// Update updates an existing domain
func (s *DomainService) Update(ctx context.Context, id int64, req models.UpdateDomainRequest) (*models.Domain, error) {
	// Normalize input
	name := strings.TrimSpace(req.Name)
	domain, err := s.repo.Update(ctx, id, name)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return domain, nil
}

// Delete deletes a domain
func (s *DomainService) Delete(ctx context.Context, id int64) error {
	err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return nil
}

