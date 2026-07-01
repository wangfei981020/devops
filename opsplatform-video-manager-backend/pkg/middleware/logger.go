// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/pkg/logger"
)

// LoggerMiddleware returns a Gin middleware for logging HTTP requests
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Build log attributes
		attrs := []any{
			"status", c.Writer.Status(),
			"method", c.Request.Method,
			"path", path,
			"latency", latency.String(),
			"latency_ms", latency.Milliseconds(),
			"ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		}

		// Add query string if present
		if raw != "" {
			attrs = append(attrs, "query", raw)
		}

		// Add user info if available
		if userID, exists := c.Get("userID"); exists {
			attrs = append(attrs, "user_id", userID)
		}
		if username, exists := c.Get("username"); exists {
			attrs = append(attrs, "username", username)
		}

		// Add error if present
		if len(c.Errors) > 0 {
			attrs = append(attrs, "errors", c.Errors.String())
		}

		// Log based on status code
		status := c.Writer.Status()
		switch {
		case status >= 500:
			logger.Error("HTTP Request", attrs...)
		case status >= 400:
			logger.Warn("HTTP Request", attrs...)
		default:
			logger.Info("HTTP Request", attrs...)
		}
	}
}

