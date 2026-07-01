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

type VideoStreamEndpointHandler struct {
	service *services.VideoStreamEndpointService
}

func NewVideoStreamEndpointHandler() *VideoStreamEndpointHandler {
	return &VideoStreamEndpointHandler{
		service: services.NewVideoStreamEndpointService(),
	}
}

// GetAll handles GET /api/video-stream-endpoints
// @Summary Get all video stream endpoints
// @Description Retrieve all video stream endpoints, optionally filtered by line_id, domain_id, stream_id, provider_id, status, or table_id
// @Tags video-stream-endpoints
// @Produce json
// @Security BearerAuth
// @Param line_id query int false "Filter by line ID"
// @Param domain_id query int false "Filter by domain ID"
// @Param stream_id query int false "Filter by stream ID"
// @Param provider_id query int false "Filter by provider ID"
// @Param status query int false "Filter by status (0=disabled, 1=enabled)"
// @Param table_id query string false "Filter by table ID (桌台号)"
// @Param resolution query string false "Filter by resolution (普清, 高清, 超清)"
// @Success 200 {object} response.Response{data=[]models.VideoStreamEndpoint}
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints [get]
func (h *VideoStreamEndpointHandler) GetAll(c *gin.Context) {
	filters := &repositories.VideoStreamEndpointFilters{}

	if lineIDStr := c.Query("line_id"); lineIDStr != "" {
		if id, err := strconv.ParseInt(lineIDStr, 10, 64); err == nil {
			filters.LineID = &id
		}
	}
	if domainIDStr := c.Query("domain_id"); domainIDStr != "" {
		if id, err := strconv.ParseInt(domainIDStr, 10, 64); err == nil {
			filters.DomainID = &id
		}
	}
	if streamIDStr := c.Query("stream_id"); streamIDStr != "" {
		if id, err := strconv.ParseInt(streamIDStr, 10, 64); err == nil {
			filters.StreamID = &id
		}
	}
	if providerIDStr := c.Query("provider_id"); providerIDStr != "" {
		if id, err := strconv.ParseInt(providerIDStr, 10, 64); err == nil {
			filters.ProviderID = &id
		}
	}
	if statusStr := c.Query("status"); statusStr != "" {
		if status, err := strconv.Atoi(statusStr); err == nil {
			filters.Status = &status
		}
	}
	if tableIDStr := c.Query("table_id"); tableIDStr != "" {
		filters.TableID = &tableIDStr
	}
	if resolutionStr := c.Query("resolution"); resolutionStr != "" {
		filters.Resolution = &resolutionStr
	}

	endpoints, err := h.service.GetAll(c.Request.Context(), filters)
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve video stream endpoints")
		return
	}
	response.Success(c, endpoints)
}

// GetByID handles GET /api/video-stream-endpoints/:id
// @Summary Get video stream endpoint by ID
// @Description Retrieve a video stream endpoint by its ID
// @Tags video-stream-endpoints
// @Produce json
// @Security BearerAuth
// @Param id path int true "Endpoint ID"
// @Success 200 {object} response.Response{data=models.VideoStreamEndpoint}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints/{id} [get]
func (h *VideoStreamEndpointHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid endpoint ID")
		return
	}

	endpoint, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrVideoStreamEndpointNotFound {
			response.NotFound(c, "Video stream endpoint not found")
		} else {
			response.InternalServerError(c, "Failed to retrieve endpoint")
		}
		return
	}
	response.Success(c, endpoint)
}

// Create handles POST /api/video-stream-endpoints
// @Summary Create a new video stream endpoint
// @Description Create a new video stream endpoint with the provided information
// @Tags video-stream-endpoints
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateVideoStreamEndpointRequest true "Video Stream Endpoint information"
// @Success 200 {object} response.Response{data=models.VideoStreamEndpoint}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints [post]
func (h *VideoStreamEndpointHandler) Create(c *gin.Context) {
	var req models.CreateVideoStreamEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	endpoint, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if err == repositories.ErrVideoStreamEndpointExists {
			response.BadRequest(c, "Video stream endpoint already exists")
		} else {
			response.InternalServerError(c, "Failed to create endpoint")
		}
		return
	}
	response.Success(c, endpoint)
}

// Update handles PUT /api/video-stream-endpoints/:id
// @Summary Update a video stream endpoint
// @Description Update an existing video stream endpoint by its ID
// @Tags video-stream-endpoints
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Endpoint ID"
// @Param request body models.UpdateVideoStreamEndpointRequest true "Updated video stream endpoint information"
// @Success 200 {object} response.Response{data=models.VideoStreamEndpoint}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints/{id} [put]
func (h *VideoStreamEndpointHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid endpoint ID")
		return
	}

	var req models.UpdateVideoStreamEndpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	endpoint, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if err == repositories.ErrVideoStreamEndpointNotFound {
			response.NotFound(c, "Video stream endpoint not found")
		} else if err == repositories.ErrVideoStreamEndpointExists {
			response.BadRequest(c, "Video stream endpoint already exists")
		} else {
			response.InternalServerError(c, "Failed to update endpoint")
		}
		return
	}
	response.Success(c, endpoint)
}

// GenerateAll handles POST /api/video-stream-endpoints/generate
// This endpoint automatically generates all video stream endpoints based on
// all combinations of CDN lines, domains, and stream paths
// @Summary Generate all video stream endpoints
// @Description Automatically generate all video stream endpoints based on all combinations of CDN lines, domains, and stream paths. This will delete all existing endpoints and regenerate them.
// @Tags video-stream-endpoints
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=object} "Response contains message and count"
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints/generate [post]
func (h *VideoStreamEndpointHandler) GenerateAll(c *gin.Context) {
	count, err := h.service.GenerateAll(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "Failed to generate endpoints: "+err.Error())
		return
	}
	response.Success(c, gin.H{
		"message": "Endpoints generated successfully",
		"count":   count,
	})
}

// Delete handles DELETE /api/video-stream-endpoints/:id
// @Summary Delete a video stream endpoint
// @Description Delete a video stream endpoint by its ID
// @Tags video-stream-endpoints
// @Produce json
// @Security BearerAuth
// @Param id path int true "Endpoint ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints/{id} [delete]
func (h *VideoStreamEndpointHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid endpoint ID")
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrVideoStreamEndpointNotFound {
			response.NotFound(c, "Video stream endpoint not found")
		} else {
			response.InternalServerError(c, "Failed to delete endpoint")
		}
		return
	}
	response.Success(c, nil)
}

// UpdateStatus handles PATCH /api/video-stream-endpoints/:id/status
// @Summary Update video stream endpoint status
// @Description Update the status (enabled/disabled) of a video stream endpoint
// @Tags video-stream-endpoints
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Endpoint ID"
// @Param request body object{status=int} true "Status (0=disabled, 1=enabled)"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints/{id}/status [patch]
func (h *VideoStreamEndpointHandler) UpdateStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid endpoint ID")
		return
	}

	var req struct {
		Status int `json:"status" binding:"min=0,max=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	err = h.service.UpdateStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		if err == repositories.ErrVideoStreamEndpointNotFound {
			response.NotFound(c, "Video stream endpoint not found")
		} else {
			response.InternalServerError(c, "Failed to update status")
		}
		return
	}
	response.Success(c, nil)
}

// TestResolution handles POST /api/video-stream-endpoints/:id/test-resolution
// @Summary Test video stream and detect resolution
// @Description Test the video stream URL and automatically detect resolution, then update the endpoint
// @Tags video-stream-endpoints
// @Produce json
// @Security BearerAuth
// @Param id path int true "Endpoint ID"
// @Success 200 {object} response.Response{data=object{resolution=string}}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/video-stream-endpoints/{id}/test-resolution [post]
func (h *VideoStreamEndpointHandler) TestResolution(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid endpoint ID")
		return
	}

	detectedResolution, err := h.service.TestResolution(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrVideoStreamEndpointNotFound {
			response.NotFound(c, "Video stream endpoint not found")
		} else {
			response.InternalServerError(c, "Failed to test resolution: "+err.Error())
		}
		return
	}

	response.Success(c, gin.H{
		"resolution": detectedResolution,
		"message":    "Resolution detected and updated successfully",
	})
}

