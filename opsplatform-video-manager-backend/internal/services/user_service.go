// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"
	"errors"

	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
)

var (
	ErrCannotDeleteSelf   = errors.New("cannot delete current user")
	ErrLastAdminProtected = errors.New("cannot remove last admin user")
)

type UserService struct {
	userRepo *repositories.UserRepository
}

func NewUserService() *UserService {
	return &UserService{
		userRepo: repositories.NewUserRepository(),
	}
}

func (s *UserService) GetAll(ctx context.Context) ([]*models.User, error) {
	return s.userRepo.GetAll(ctx)
}

func (s *UserService) Create(ctx context.Context, req models.CreateUserRequest) (*models.User, error) {
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	return s.userRepo.Create(ctx, req.Username, hashedPassword, req.IsAdmin)
}

func (s *UserService) Update(ctx context.Context, id int64, req models.UpdateUserRequest) (*models.User, error) {
	current, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Prevent demoting the last admin account.
	if current.IsAdmin && !req.IsAdmin {
		adminCount, err := s.userRepo.CountAdmins(ctx)
		if err != nil {
			return nil, err
		}
		if adminCount <= 1 {
			return nil, ErrLastAdminProtected
		}
	}

	return s.userRepo.Update(ctx, id, req.Username, req.IsAdmin)
}

func (s *UserService) Delete(ctx context.Context, targetUserID, operatorUserID int64) error {
	if targetUserID == operatorUserID {
		return ErrCannotDeleteSelf
	}

	targetUser, err := s.userRepo.GetByID(ctx, targetUserID)
	if err != nil {
		return err
	}

	// Prevent deleting the last admin account.
	if targetUser.IsAdmin {
		adminCount, err := s.userRepo.CountAdmins(ctx)
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return ErrLastAdminProtected
		}
	}

	return s.userRepo.Delete(ctx, targetUserID)
}
