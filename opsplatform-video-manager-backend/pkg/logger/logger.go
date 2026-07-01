// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

var (
	// Logger is the global logger instance
	Logger *slog.Logger
)

// LogLevel represents the log level
type LogLevel string

const (
	LevelDebug LogLevel = "DEBUG"
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
)

// Init initializes the logger with the specified level and format
func Init() {
	level := getLogLevel()
	format := getLogFormat()

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Remove source file path, keep only filename
			if a.Key == slog.SourceKey {
				if source, ok := a.Value.Any().(*slog.Source); ok {
					// Extract just the filename
					parts := strings.Split(source.File, "/")
					if len(parts) > 0 {
						source.File = parts[len(parts)-1]
					}
					return slog.Any(a.Key, source)
				}
			}
			return a
		},
	}

	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	Logger = slog.New(handler)
	slog.SetDefault(Logger)
}

// getLogLevel returns the log level from environment variable
func getLogLevel() slog.Level {
	levelStr := os.Getenv("LOG_LEVEL")
	if levelStr == "" {
		levelStr = "INFO"
	}

	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// getLogFormat returns the log format from environment variable
func getLogFormat() string {
	format := os.Getenv("LOG_FORMAT")
	if format == "" {
		format = "text"
	}
	return strings.ToLower(format)
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	Logger.Debug(msg, args...)
}

// Info logs an info message
func Info(msg string, args ...any) {
	Logger.Info(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	Logger.Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	Logger.Error(msg, args...)
}

// DebugContext logs a debug message with context
func DebugContext(ctx context.Context, msg string, args ...any) {
	Logger.DebugContext(ctx, msg, args...)
}

// InfoContext logs an info message with context
func InfoContext(ctx context.Context, msg string, args ...any) {
	Logger.InfoContext(ctx, msg, args...)
}

// WarnContext logs a warning message with context
func WarnContext(ctx context.Context, msg string, args ...any) {
	Logger.WarnContext(ctx, msg, args...)
}

// ErrorContext logs an error message with context
func ErrorContext(ctx context.Context, msg string, args ...any) {
	Logger.ErrorContext(ctx, msg, args...)
}

// With returns a logger with the given attributes
func With(args ...any) *slog.Logger {
	return Logger.With(args...)
}

// WithGroup returns a logger with the given group
func WithGroup(name string) *slog.Logger {
	return Logger.WithGroup(name)
}

