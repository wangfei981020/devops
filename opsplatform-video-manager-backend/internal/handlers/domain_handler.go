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

type DomainHandler struct {
	service *services.DomainService
}

func NewDomainHandler() *DomainHandler {
	return &DomainHandler{
		service: services.NewDomainService(),
	}
}

// GetAll handles GET /api/domains
// @Summary Get all domains
// @Description Retrieve all domains
// @Tags domains
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]models.Domain}
// @Failure 500 {object} response.Response
// @Router /api/domains [get]
func (h *DomainHandler) GetAll(c *gin.Context) {
	domains, err := h.service.GetAll(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve domains")
		return
	}
	response.Success(c, domains)
}

// GetByID handles GET /api/domains/:id
// @Summary Get domain by ID
// @Description Retrieve a domain by its ID
// @Tags domains
// @Produce json
// @Security BearerAuth
// @Param id path int true "Domain ID"
// @Success 200 {object} response.Response{data=models.Domain}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/domains/{id} [get]
func (h *DomainHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid domain ID")
		return
	}

	domain, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrDomainNotFound {
			response.NotFound(c, "Domain not found")
		} else {
			response.InternalServerError(c, "Failed to retrieve domain")
		}
		return
	}
	response.Success(c, domain)
}

// Create handles POST /api/domains
// @Summary Create a new domain
// @Description Create a new domain with the provided information
// @Tags domains
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateDomainRequest true "Domain information"
// @Success 200 {object} response.Response{data=models.Domain}
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/domains [post]
func (h *DomainHandler) Create(c *gin.Context) {
	var req models.CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	domain, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if err == repositories.ErrDomainNameExists {
			response.BadRequest(c, "域名名称已存在")
		} else {
			response.InternalServerError(c, "创建域名失败")
		}
		return
	}
	response.Success(c, domain)
}

// Update handles PUT /api/domains/:id
// @Summary Update a domain
// @Description Update an existing domain by its ID
// @Tags domains
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Domain ID"
// @Param request body models.UpdateDomainRequest true "Updated domain information"
// @Success 200 {object} response.Response{data=models.Domain}
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/domains/{id} [put]
func (h *DomainHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid domain ID")
		return
	}

	var req models.UpdateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	domain, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		if err == repositories.ErrDomainNotFound {
			response.NotFound(c, "域名不存在")
		} else if err == repositories.ErrDomainNameExists {
			response.BadRequest(c, "域名名称已存在")
		} else {
			response.InternalServerError(c, "更新域名失败")
		}
		return
	}
	response.Success(c, domain)
}

// Delete handles DELETE /api/domains/:id
// @Summary Delete a domain
// @Description Delete a domain by its ID. This will also delete associated video stream endpoints.
// @Tags domains
// @Produce json
// @Security BearerAuth
// @Param id path int true "Domain ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/domains/{id} [delete]
func (h *DomainHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid domain ID")
		return
	}

	err = h.service.Delete(c.Request.Context(), id)
	if err != nil {
		if err == repositories.ErrDomainNotFound {
			response.NotFound(c, "Domain not found")
		} else {
			response.BadRequest(c, err.Error())
		}
		return
	}
	response.Success(c, nil)
}

