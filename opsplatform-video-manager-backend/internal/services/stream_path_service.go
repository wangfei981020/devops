// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
)

type StreamPathService struct {
	repo *repositories.StreamPathRepository
}

func NewStreamPathService() *StreamPathService {
	return &StreamPathService{
		repo: repositories.NewStreamPathRepository(),
	}
}

// GetAll retrieves all stream paths, optionally filtered by stream_id
func (s *StreamPathService) GetAll(ctx context.Context, streamID *int64) ([]models.StreamPath, error) {
	return s.repo.GetAll(ctx, streamID)
}

// GetByID retrieves a stream path by ID
func (s *StreamPathService) GetByID(ctx context.Context, id int64) (*models.StreamPath, error) {
	return s.repo.GetByID(ctx, id)
}

// Create creates a new stream path
func (s *StreamPathService) Create(ctx context.Context, req models.CreateStreamPathRequest) (*models.StreamPath, error) {
	// Normalize input
	tableID := strings.TrimSpace(req.TableID)
	fullPath := strings.TrimSpace(req.FullPath)
	path, err := s.repo.Create(ctx, req.StreamID, tableID, fullPath)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return path, nil
}

// Update updates an existing stream path
func (s *StreamPathService) Update(ctx context.Context, id int64, req models.UpdateStreamPathRequest) (*models.StreamPath, error) {
	// Normalize input
	tableID := strings.TrimSpace(req.TableID)
	fullPath := strings.TrimSpace(req.FullPath)
	path, err := s.repo.Update(ctx, id, req.StreamID, tableID, fullPath)
	if err != nil {
		return nil, err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return path, nil
}

// Delete deletes a stream path
func (s *StreamPathService) Delete(ctx context.Context, id int64) error {
	err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Auto-regenerate video stream endpoints
	endpointService := NewVideoStreamEndpointService()
	_, _ = endpointService.GenerateAll(ctx) // Ignore errors in auto-generation

	return nil
}

// ImportStreamPaths upserts stream paths keyed by table_id (creates if missing, updates otherwise).
// Rows with duplicate table_id in the same batch keep the first occurrence; later lines are reported as errors.
func (s *StreamPathService) ImportStreamPaths(ctx context.Context, items []models.StreamPathImportItem) (*models.StreamPathImportResult, error) {
	result := &models.StreamPathImportResult{Errors: []models.StreamPathImportRowError{}}
	if len(items) == 0 {
		return result, nil
	}

	streamRepo := repositories.NewStreamRepository()
	streams, err := streamRepo.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	idOK := make(map[int64]struct{}, len(streams))
	nameToID := make(map[string]int64, len(streams))
	for _, st := range streams {
		idOK[st.ID] = struct{}{}
		name := strings.TrimSpace(st.Name)
		if _, exists := nameToID[name]; !exists {
			nameToID[name] = st.ID
		}
	}

	firstLineByTable := make(map[string]int)
	var touched bool

	for _, item := range items {
		line := item.Line
		tableID := strings.TrimSpace(item.TableID)
		fullPath := strings.TrimLeft(strings.TrimSpace(item.FullPath), "/")

		if tableID == "" || fullPath == "" {
			result.Errors = append(result.Errors, models.StreamPathImportRowError{
				Line: line, TableID: tableID, Message: "桌台号与路径不能为空",
			})
			continue
		}

		if prevLine, dup := firstLineByTable[tableID]; dup {
			result.Errors = append(result.Errors, models.StreamPathImportRowError{
				Line: line, TableID: tableID,
				Message: fmt.Sprintf("与第 %d 行桌台号重复，已跳过", prevLine),
			})
			continue
		}
		firstLineByTable[tableID] = line

		var streamID int64
		if item.StreamID > 0 {
			if _, ok := idOK[item.StreamID]; !ok {
				result.Errors = append(result.Errors, models.StreamPathImportRowError{
					Line: line, TableID: tableID, Message: fmt.Sprintf("视频流区域 ID %d 不存在", item.StreamID),
				})
				continue
			}
			streamID = item.StreamID
		} else {
			name := strings.TrimSpace(item.StreamName)
			if name == "" {
				result.Errors = append(result.Errors, models.StreamPathImportRowError{
					Line: line, TableID: tableID, Message: "未指定视频流区域（流区域列或 stream_id 列）",
				})
				continue
			}
			id, ok := nameToID[name]
			if !ok {
				result.Errors = append(result.Errors, models.StreamPathImportRowError{
					Line: line, TableID: tableID, Message: fmt.Sprintf("找不到视频流区域: %s", name),
				})
				continue
			}
			streamID = id
		}

		existingID, found, err := s.repo.LookupIDByTableID(ctx, tableID)
		if err != nil {
			return nil, err
		}

		if !found {
			_, err = s.repo.Create(ctx, streamID, tableID, fullPath)
			if err != nil {
				if err == repositories.ErrStreamPathTableIDExists {
					result.Errors = append(result.Errors, models.StreamPathImportRowError{
						Line: line, TableID: tableID, Message: "桌台号已存在（并发导入冲突）",
					})
					continue
				}
				if errors.Is(err, repositories.ErrStreamNotFound) {
					result.Errors = append(result.Errors, models.StreamPathImportRowError{
						Line: line, TableID: tableID, Message: "视频流区域不存在",
					})
					continue
				}
				result.Errors = append(result.Errors, models.StreamPathImportRowError{
					Line: line, TableID: tableID, Message: err.Error(),
				})
				continue
			}
			result.Created++
			touched = true
			continue
		}

		_, err = s.repo.Update(ctx, existingID, streamID, tableID, fullPath)
		if err != nil {
			if err == repositories.ErrStreamPathTableIDExists {
				result.Errors = append(result.Errors, models.StreamPathImportRowError{
					Line: line, TableID: tableID, Message: "桌台号冲突",
				})
				continue
			}
			if errors.Is(err, repositories.ErrStreamNotFound) {
				result.Errors = append(result.Errors, models.StreamPathImportRowError{
					Line: line, TableID: tableID, Message: "视频流区域不存在",
				})
				continue
			}
			result.Errors = append(result.Errors, models.StreamPathImportRowError{
				Line: line, TableID: tableID, Message: err.Error(),
			})
			continue
		}
		result.Updated++
		touched = true
	}

	if touched {
		endpointService := NewVideoStreamEndpointService()
		_, _ = endpointService.GenerateAll(ctx)
	}

	return result, nil
}

