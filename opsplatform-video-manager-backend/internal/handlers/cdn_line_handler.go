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

type CDNLineHandler struct {
	service *services.CDNLineService
}

func NewCDNLineHandler() *CDNLineHandler {
	return &CDNLineHandler{
		service: services.NewCDNLineService(),
	}
}

// GetAll handles GET /api/cdn-lines
// @Summary Get all CDN lines
// @Description Retrieve all CDN lines, optionally filtered by provider ID
// @Tags cdn-lines
// @Produce json
// @Security BearerAuth
// @Param provider_id query int false "Filter by provider ID"
// @Success 200 {object} response.Response{data=[]models.CDNLine}
// @Failure 500 {object} response.Response
// @Router /api/cdn-lines [get]
func (h *CDNLineHandler) GetAll(c *gin.Context) {
	var providerID *int64
	if providerIDStr := c.Query("provider_id"); providerIDStr != "" {
		id, err := strconv.ParseInt(providerIDStr, 10, 64)
		if err == nil {
			providerID = &id
		}
	}

	lines, err := h.service.GetAll(c.Request.Context(), providerID)
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve lines")
		return
	}
	response.Success(c, lines)
}

// GetByID handles GET /api/cdn-lines/:id
// @Summary Get CDN line by ID
// @Description Retrieve a CDN line by its ID
// @Tags cdn-lines
// @Produce json
// @Security BearerAuth
// @Param id path int true "Line ID"
// @Success 200 {object} response.Response{data=models.CDNLine}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-lines/{id} [get]
func (h *CDNLineHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid line ID")
		return
	}

	line, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrLineNotFound {
			response.NotFound(c, "Line not found")
		} else {
			response.InternalServerError(c, "Failed to retrieve line")
		}
		return
	}
	response.Success(c, line)
}

// Create handles POST /api/cdn-lines
// @Summary Create a new CDN line
// @Description Create a new CDN line with the provided information
// @Tags cdn-lines
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateCDNLineRequest true "CDN Line information"
// @Success 200 {object} response.Response{data=models.CDNLine}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-lines [post]
func (h *CDNLineHandler) Create(c *gin.Context) {
	var req models.CreateCDNLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	line, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if err == repositories.ErrProviderNotFound {
			response.BadRequest(c, "厂商不存在")
		} else if err == repositories.ErrLineNameExists {
			response.BadRequest(c, "线路名称已存在")
		} else {
			response.InternalServerError(c, "创建线路失败")
		}
		return
	}
	response.Success(c, line)
}

// Update handles PUT /api/cdn-lines/:id
// @Summary Update a CDN line
// @Description Update an existing CDN line by its ID
// @Tags cdn-lines
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Line ID"
// @Param request body models.UpdateCDNLineRequest true "Updated CDN Line information"
// @Success 200 {object} response.Response{data=models.CDNLine}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-lines/{id} [put]
func (h *CDNLineHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid line ID")
		return
	}

	var req models.UpdateCDNLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	line, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if err == repositories.ErrLineNotFound {
			response.NotFound(c, "线路不存在")
		} else if err == repositories.ErrProviderNotFound {
			response.BadRequest(c, "厂商不存在")
		} else if err == repositories.ErrLineNameExists {
			response.BadRequest(c, "线路名称已存在")
		} else {
			response.InternalServerError(c, "更新线路失败")
		}
		return
	}
	response.Success(c, line)
}

// Delete handles DELETE /api/cdn-lines/:id
// @Summary Delete a CDN line
// @Description Delete a CDN line by its ID. This will also delete associated video stream endpoints.
// @Tags cdn-lines
// @Produce json
// @Security BearerAuth
// @Param id path int true "Line ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-lines/{id} [delete]
func (h *CDNLineHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid line ID")
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrLineNotFound {
			response.NotFound(c, "Line not found")
		} else {
			response.InternalServerError(c, "Failed to delete line")
		}
		return
	}
	response.Success(c, nil)
}

