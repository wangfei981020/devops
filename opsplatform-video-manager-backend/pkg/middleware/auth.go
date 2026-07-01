// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/pkg/jwt"
	"github.com/video-manager/backend/pkg/logger"
	"github.com/video-manager/backend/pkg/response"
)

// AuthMiddleware validates JWT token and sets user context
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			logger.Warn("Authorization header missing", "path", c.Request.URL.Path, "ip", c.ClientIP())
			response.Error(c, http.StatusUnauthorized, "authorization header required")
			c.Abort()
			return
		}

		// Extract token from "Bearer <token>" or just "<token>"
		var token string
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			// Standard format: "Bearer <token>"
			token = parts[1]
		} else if len(parts) == 1 {
			// Direct token format (for Swagger UI compatibility)
			token = parts[0]
		} else {
			logger.Warn("Invalid authorization header format", "path", c.Request.URL.Path, "ip", c.ClientIP())
			response.Error(c, http.StatusUnauthorized, "invalid authorization header format")
			c.Abort()
			return
		}
		claims, err := jwt.ValidateToken(token)
		if err != nil {
			if err == jwt.ErrExpiredToken {
				logger.Warn("Token expired", "path", c.Request.URL.Path, "ip", c.ClientIP())
				response.Error(c, http.StatusUnauthorized, "token expired")
			} else {
				logger.Warn("Invalid token", "path", c.Request.URL.Path, "ip", c.ClientIP(), "error", err)
				response.Error(c, http.StatusUnauthorized, "invalid token")
			}
			c.Abort()
			return
		}

		logger.Debug("User authenticated", "user_id", claims.UserID, "username", claims.Username, "path", c.Request.URL.Path)

		// Set user information in context
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("isAdmin", claims.IsAdmin)

		c.Next()
	}
}

// AdminMiddleware checks if the user is an admin
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		isAdmin, exists := c.Get("isAdmin")
		if !exists || !isAdmin.(bool) {
			response.Error(c, http.StatusForbidden, "admin access required")
			c.Abort()
			return
		}
		c.Next()
	}
}

// ReadOnlyForNonAdminMiddleware allows only read operations for non-admin users.
func ReadOnlyForNonAdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		isAdmin, exists := c.Get("isAdmin")
		if exists && isAdmin.(bool) {
			c.Next()
			return
		}

		// Allow non-admin users to refresh endpoint data by triggering regeneration.
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/api/video-stream-endpoints/generate" {
			c.Next()
			return
		}

		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
		default:
			response.Error(c, http.StatusForbidden, "readonly access: admin required for write operations")
			c.Abort()
		}
	}
}
