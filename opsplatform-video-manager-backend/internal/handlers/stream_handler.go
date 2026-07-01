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

type StreamHandler struct {
	service *services.StreamService
}

func NewStreamHandler() *StreamHandler {
	return &StreamHandler{
		service: services.NewStreamService(),
	}
}

// GetAll handles GET /api/streams
// @Summary Get all streams
// @Description Retrieve all streams
// @Tags stream-regions
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]models.Stream}
// @Failure 500 {object} response.Response
// @Router /api/stream-regions [get]
func (h *StreamHandler) GetAll(c *gin.Context) {
	streams, err := h.service.GetAll(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve streams")
		return
	}
	response.Success(c, streams)
}

// GetByID handles GET /api/streams/:id
// @Summary Get stream by ID
// @Description Retrieve a stream by its ID
// @Tags stream-regions
// @Produce json
// @Security BearerAuth
// @Param id path int true "Stream ID"
// @Success 200 {object} response.Response{data=models.Stream}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/stream-regions/{id} [get]
func (h *StreamHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid stream ID")
		return
	}

	stream, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrStreamNotFound {
			response.NotFound(c, "Stream not found")
		} else {
			response.InternalServerError(c, "Failed to retrieve stream")
		}
		return
	}
	response.Success(c, stream)
}

// Create handles POST /api/streams
// @Summary Create a new stream
// @Description Create a new stream with the provided information
// @Tags stream-regions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateStreamRequest true "Stream information"
// @Success 200 {object} response.Response{data=models.Stream}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/stream-regions [post]
func (h *StreamHandler) Create(c *gin.Context) {
	var req models.CreateStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	stream, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if err == repositories.ErrStreamCodeExists {
			response.BadRequest(c, "视频流区域代码已存在")
		} else if err == repositories.ErrStreamNameExists {
			response.BadRequest(c, "视频流区域名称已存在")
		} else {
			response.InternalServerError(c, "创建视频流区域失败")
		}
		return
	}
	response.Success(c, stream)
}

// Update handles PUT /api/streams/:id
// @Summary Update a stream
// @Description Update an existing stream by its ID
// @Tags stream-regions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Stream ID"
// @Param request body models.UpdateStreamRequest true "Updated stream information"
// @Success 200 {object} response.Response{data=models.Stream}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/stream-regions/{id} [put]
func (h *StreamHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid stream ID")
		return
	}

	var req models.UpdateStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	stream, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if err == repositories.ErrStreamNotFound {
			response.NotFound(c, "视频流区域不存在")
		} else if err == repositories.ErrStreamCodeExists {
			response.BadRequest(c, "视频流区域代码已存在")
		} else if err == repositories.ErrStreamNameExists {
			response.BadRequest(c, "视频流区域名称已存在")
		} else {
			response.InternalServerError(c, "更新视频流区域失败")
		}
		return
	}
	response.Success(c, stream)
}

// Delete handles DELETE /api/streams/:id
// @Summary Delete a stream
// @Description Delete a stream by its ID. This will also delete associated stream paths and video stream endpoints.
// @Tags stream-regions
// @Produce json
// @Security BearerAuth
// @Param id path int true "Stream ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/stream-regions/{id} [delete]
func (h *StreamHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid stream ID")
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrStreamNotFound {
			response.NotFound(c, "Stream not found")
		} else {
			response.BadRequest(c, err.Error())
		}
		return
	}
	response.Success(c, nil)
}

