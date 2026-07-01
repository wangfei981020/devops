// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
	"github.com/video-manager/backend/internal/services"
	"github.com/video-manager/backend/pkg/response"
)

type StreamPathHandler struct {
	service *services.StreamPathService
}

func NewStreamPathHandler() *StreamPathHandler {
	return &StreamPathHandler{
		service: services.NewStreamPathService(),
	}
}

// GetAll handles GET /api/stream-paths
// @Summary Get all stream paths
// @Description Retrieve all stream paths, optionally filtered by stream ID
// @Tags stream-paths
// @Produce json
// @Security BearerAuth
// @Param stream_id query int false "Filter by stream ID"
// @Success 200 {object} response.Response{data=[]models.StreamPath}
// @Failure 500 {object} response.Response
// @Router /api/stream-paths [get]
func (h *StreamPathHandler) GetAll(c *gin.Context) {
	var streamID *int64
	if streamIDStr := c.Query("stream_id"); streamIDStr != "" {
		if id, err := strconv.ParseInt(streamIDStr, 10, 64); err == nil {
			streamID = &id
		}
	}

	paths, err := h.service.GetAll(c.Request.Context(), streamID)
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve stream paths")
		return
	}
	response.Success(c, paths)
}

// GetByID handles GET /api/stream-paths/:id
// @Summary Get stream path by ID
// @Description Retrieve a stream path by its ID
// @Tags stream-paths
// @Produce json
// @Security BearerAuth
// @Param id path int true "Stream Path ID"
// @Success 200 {object} response.Response{data=models.StreamPath}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/stream-paths/{id} [get]
func (h *StreamPathHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid stream path ID")
		return
	}

	path, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrStreamPathNotFound {
			response.NotFound(c, "Stream path not found")
		} else {
			response.InternalServerError(c, "Failed to retrieve stream path")
		}
		return
	}
	response.Success(c, path)
}

// Create handles POST /api/stream-paths
// @Summary Create a new stream path
// @Description Create a new stream path with the provided information
// @Tags stream-paths
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateStreamPathRequest true "Stream Path information"
// @Success 200 {object} response.Response{data=models.StreamPath}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/stream-paths [post]
func (h *StreamPathHandler) Create(c *gin.Context) {
	var req models.CreateStreamPathRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	path, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if err == repositories.ErrStreamNotFound {
			response.BadRequest(c, "视频流区域不存在")
		} else if err == repositories.ErrStreamPathTableIDExists {
			response.BadRequest(c, "桌台号已存在")
		} else {
			response.InternalServerError(c, "创建流路径失败")
		}
		return
	}
	response.Success(c, path)
}

// Update handles PUT /api/stream-paths/:id
// @Summary Update a stream path
// @Description Update an existing stream path by its ID
// @Tags stream-paths
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Stream Path ID"
// @Param request body models.UpdateStreamPathRequest true "Updated stream path information"
// @Success 200 {object} response.Response{data=models.StreamPath}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/stream-paths/{id} [put]
func (h *StreamPathHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid stream path ID")
		return
	}

	var req models.UpdateStreamPathRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	path, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if err == repositories.ErrStreamPathNotFound {
			response.NotFound(c, "流路径不存在")
		} else if err == repositories.ErrStreamNotFound {
			response.BadRequest(c, "视频流区域不存在")
		} else if err == repositories.ErrStreamPathTableIDExists {
			response.BadRequest(c, "桌台号已存在")
		} else {
			response.InternalServerError(c, "更新流路径失败")
		}
		return
	}
	response.Success(c, path)
}

// Delete handles DELETE /api/stream-paths/:id
// @Summary Delete a stream path
// @Description Delete a stream path by its ID. This will also delete associated video stream endpoints.
// @Tags stream-paths
// @Produce json
// @Security BearerAuth
// @Param id path int true "Stream Path ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/stream-paths/{id} [delete]
func (h *StreamPathHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid stream path ID")
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrStreamPathNotFound {
			response.NotFound(c, "Stream path not found")
		} else {
			response.BadRequest(c, err.Error())
		}
		return
	}
	response.Success(c, nil)
}

