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

type CDNProviderHandler struct {
	service *services.CDNProviderService
}

func NewCDNProviderHandler() *CDNProviderHandler {
	return &CDNProviderHandler{
		service: services.NewCDNProviderService(),
	}
}

// GetAll handles GET /api/cdn-providers
// @Summary Get all CDN providers
// @Description Retrieve all CDN providers
// @Tags cdn-providers
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]models.CDNProvider}
// @Failure 500 {object} response.Response
// @Router /api/cdn-providers [get]
func (h *CDNProviderHandler) GetAll(c *gin.Context) {
	providers, err := h.service.GetAll(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve providers")
		return
	}
	response.Success(c, providers)
}

// GetByID handles GET /api/cdn-providers/:id
// @Summary Get CDN provider by ID
// @Description Retrieve a CDN provider by its ID
// @Tags cdn-providers
// @Produce json
// @Security BearerAuth
// @Param id path int true "Provider ID"
// @Success 200 {object} response.Response{data=models.CDNProvider}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-providers/{id} [get]
func (h *CDNProviderHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	provider, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrProviderNotFound {
			response.NotFound(c, "Provider not found")
		} else {
			response.InternalServerError(c, "Failed to retrieve provider")
		}
		return
	}
	response.Success(c, provider)
}

// Create handles POST /api/cdn-providers
// @Summary Create a new CDN provider
// @Description Create a new CDN provider with the provided information
// @Tags cdn-providers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateCDNProviderRequest true "CDN Provider information"
// @Success 200 {object} response.Response{data=models.CDNProvider}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-providers [post]
func (h *CDNProviderHandler) Create(c *gin.Context) {
	var req models.CreateCDNProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	provider, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if err == repositories.ErrProviderCodeExists {
			response.BadRequest(c, "厂商代码已存在")
		} else if err == repositories.ErrProviderNameExists {
			response.BadRequest(c, "厂商名称已存在")
		} else if err == repositories.ErrInvalidCodeFormat {
			response.BadRequest(c, "代码只能包含字母、数字、下划线和连字符")
		} else {
			response.InternalServerError(c, "创建厂商失败")
		}
		return
	}
	response.Success(c, provider)
}

// Update handles PUT /api/cdn-providers/:id
// @Summary Update a CDN provider
// @Description Update an existing CDN provider by its ID
// @Tags cdn-providers
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Provider ID"
// @Param request body models.UpdateCDNProviderRequest true "Updated CDN Provider information"
// @Success 200 {object} response.Response{data=models.CDNProvider}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-providers/{id} [put]
func (h *CDNProviderHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	var req models.UpdateCDNProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	provider, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if err == repositories.ErrProviderNotFound {
			response.NotFound(c, "厂商不存在")
		} else if err == repositories.ErrProviderCodeExists {
			response.BadRequest(c, "厂商代码已存在")
		} else if err == repositories.ErrProviderNameExists {
			response.BadRequest(c, "厂商名称已存在")
		} else if err == repositories.ErrInvalidCodeFormat {
			response.BadRequest(c, "代码只能包含字母、数字、下划线和连字符")
		} else {
			response.InternalServerError(c, "更新厂商失败")
		}
		return
	}
	response.Success(c, provider)
}

// Delete handles DELETE /api/cdn-providers/:id
// @Summary Delete a CDN provider
// @Description Delete a CDN provider by its ID. This will also delete associated CDN lines and video stream endpoints.
// @Tags cdn-providers
// @Produce json
// @Security BearerAuth
// @Param id path int true "Provider ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/cdn-providers/{id} [delete]
func (h *CDNProviderHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrProviderNotFound {
			response.NotFound(c, "Provider not found")
		} else {
			response.BadRequest(c, err.Error())
		}
		return
	}
	response.Success(c, nil)
}

// GetLinesByProvider handles GET /api/cdn-providers/:id/lines
// @Summary Get CDN lines by provider
// @Description Retrieve all CDN lines associated with a specific provider
// @Tags cdn-providers
// @Produce json
// @Security BearerAuth
// @Param id path int true "Provider ID"
// @Success 200 {object} response.Response{data=[]models.CDNLine}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/cdn-providers/{id}/lines [get]
func (h *CDNProviderHandler) GetLinesByProvider(c *gin.Context) {
	providerID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid provider ID")
		return
	}

	// Verify provider exists
	_, err = h.service.GetByID(c.Request.Context(), providerID)
	if err != nil {
		if err == repositories.ErrProviderNotFound {
			response.NotFound(c, "Provider not found")
		} else {
			response.InternalServerError(c, "Failed to retrieve provider")
		}
		return
	}

	// Get lines for this provider
	lineService := services.NewCDNLineService()
	lines, err := lineService.GetAll(c.Request.Context(), &providerID)
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve lines")
		return
	}
	response.Success(c, lines)
}
