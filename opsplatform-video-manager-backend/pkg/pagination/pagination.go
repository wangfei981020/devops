// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package pagination

import (
	"strconv"
)

// PaginationParams represents pagination parameters
type PaginationParams struct {
	Page     int // 1-based page number
	PageSize int // Number of items per page
}

// ParsePagination parses pagination parameters from query string
func ParsePagination(pageStr, pageSizeStr string) PaginationParams {
	page := 1
	pageSize := 10

	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			// Limit page size to prevent abuse
			if ps > 100 {
				ps = 100
			}
			pageSize = ps
		}
	}

	return PaginationParams{
		Page:     page,
		PageSize: pageSize,
	}
}

// Offset calculates the SQL OFFSET value
func (p PaginationParams) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// Limit returns the SQL LIMIT value
func (p PaginationParams) Limit() int {
	return p.PageSize
}

// PaginatedResponse represents a paginated response
type PaginatedResponse[T any] struct {
	Data       []T   `json:"data"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int   `json:"total_pages"`
}

// NewPaginatedResponse creates a new paginated response
func NewPaginatedResponse[T any](data []T, total int64, page, pageSize int) *PaginatedResponse[T] {
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}

	return &PaginatedResponse[T]{
		Data:       data,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

