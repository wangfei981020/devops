// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
	"github.com/video-manager/backend/pkg/jwt"
	"github.com/video-manager/backend/pkg/logger"
)

type AuthService struct {
	userRepo  *repositories.UserRepository
	tokenRepo *repositories.TokenRepository
}

func NewAuthService() *AuthService {
	return &AuthService{
		userRepo:  repositories.NewUserRepository(),
		tokenRepo: repositories.NewTokenRepository(),
	}
}

// Login authenticates a user and returns a JWT token
func (s *AuthService) Login(ctx context.Context, username, password string) (*models.LoginResponse, error) {
	logger.DebugContext(ctx, "Attempting login", "username", username)

	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repositories.ErrUserNotFound) {
			logger.DebugContext(ctx, "User not found", "username", username)
			return nil, errors.New("invalid username or password")
		}
		logger.ErrorContext(ctx, "Failed to get user", "username", username, "error", err)
		return nil, err
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		logger.DebugContext(ctx, "Invalid password", "username", username)
		return nil, errors.New("invalid username or password")
	}

	// Generate JWT token
	token, err := jwt.GenerateToken(user.ID, user.Username, user.IsAdmin)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to generate token", "username", username, "error", err)
		return nil, err
	}

	logger.InfoContext(ctx, "Login successful", "user_id", user.ID, "username", user.Username, "is_admin", user.IsAdmin)
	return &models.LoginResponse{
		Token:    token,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
	}, nil
}

// ChangePassword changes a user's password
func (s *AuthService) ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	// Verify old password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword))
	if err != nil {
		return errors.New("invalid old password")
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Update password
	return s.userRepo.UpdatePassword(ctx, userID, string(hashedPassword))
}

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

// InitAdminUser initializes the admin user
func (s *AuthService) InitAdminUser(ctx context.Context, username, password string) error {
	hashedPassword, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.userRepo.InitAdminUser(ctx, username, hashedPassword)
}

// CreateToken creates a new token for the current user
func (s *AuthService) CreateToken(ctx context.Context, userID int64, username string, isAdmin bool, req models.CreateTokenRequest) (string, error) {
	var expiration time.Duration
	var expiresAt *time.Time
	if req.NeverExpire {
		expiration = 0 // 0 means never expire
		expiresAt = nil
	} else {
		if req.ExpiresIn <= 0 {
			return "", errors.New("expires_in must be greater than 0 when never_expire is false")
		}
		expiration = time.Duration(req.ExpiresIn) * time.Second
		exp := time.Now().Add(expiration)
		expiresAt = &exp
	}

	token, err := jwt.GenerateTokenWithExpiration(userID, username, isAdmin, expiration)
	if err != nil {
		return "", err
	}

	// Create hash of token for storage (first 32 chars of SHA256)
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])[:32]

	// Save token metadata to database
	_, err = s.tokenRepo.Create(ctx, userID, req.Name, tokenHash, req.NeverExpire, expiresAt)
	if err != nil {
		if err == repositories.ErrTokenNameExists {
			return "", err
		}
		logger.ErrorContext(ctx, "Failed to save token metadata", "user_id", userID, "error", err)
		// Don't fail the request if we can't save metadata, but log it
	}

	return token, nil
}

// GetTokens retrieves all tokens for a user
func (s *AuthService) GetTokens(ctx context.Context, userID int64) ([]*models.Token, error) {
	return s.tokenRepo.GetByUserID(ctx, userID)
}

// DeleteToken deletes a token by ID (only if it belongs to the user)
func (s *AuthService) DeleteToken(ctx context.Context, tokenID, userID int64) error {
	token, err := s.tokenRepo.GetByID(ctx, tokenID)
	if err != nil {
		return err
	}

	// Verify token belongs to user
	if token.UserID != userID {
		return errors.New("token not found")
	}

	return s.tokenRepo.Delete(ctx, tokenID)
}

// LoginOrCreateOIDCUser logs in an OIDC user; creates a non-admin account when missing.
func (s *AuthService) LoginOrCreateOIDCUser(ctx context.Context, username string) (*models.LoginResponse, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		if !errors.Is(err, repositories.ErrUserNotFound) {
			return nil, err
		}

		passwordHash, hashErr := randomPasswordHash()
		if hashErr != nil {
			return nil, hashErr
		}

		createdUser, createErr := s.userRepo.Create(ctx, username, passwordHash, false)
		if createErr != nil {
			if errors.Is(createErr, repositories.ErrUserExists) {
				user, err = s.userRepo.GetByUsername(ctx, username)
				if err != nil {
					return nil, err
				}
			} else {
				return nil, createErr
			}
		} else {
			user = createdUser
		}
	}

	token, err := jwt.GenerateToken(user.ID, user.Username, user.IsAdmin)
	if err != nil {
		return nil, err
	}

	return &models.LoginResponse{
		Token:    token,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
	}, nil
}

func randomPasswordHash() (string, error) {
	const n = 32
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random password: %w", err)
	}
	return HashPassword(hex.EncodeToString(b))
}
