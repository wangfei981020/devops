// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/video-manager/backend/internal/models"
	"github.com/video-manager/backend/internal/repositories"
	"github.com/video-manager/backend/internal/services"
	"github.com/video-manager/backend/pkg/response"
)

type UserHandler struct {
	service *services.UserService
}

func NewUserHandler() *UserHandler {
	return &UserHandler{
		service: services.NewUserService(),
	}
}

// GetAll handles GET /api/users
// @Summary Get all users
// @Description Retrieve all users (admin only)
// @Tags users
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Response{data=[]models.User}
// @Failure 403 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/users [get]
func (h *UserHandler) GetAll(c *gin.Context) {
	users, err := h.service.GetAll(c.Request.Context())
	if err != nil {
		response.InternalServerError(c, "Failed to retrieve users")
		return
	}
	response.Success(c, users)
}

// Create handles POST /api/users
// @Summary Create user
// @Description Create a new user (admin only)
// @Tags users
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body models.CreateUserRequest true "Create user request"
// @Success 200 {object} response.Response{data=models.User}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/users [post]
func (h *UserHandler) Create(c *gin.Context) {
	var req models.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	user, err := h.service.Create(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, repositories.ErrUserExists) {
			response.BadRequest(c, "user already exists")
			return
		}
		response.InternalServerError(c, "failed to create user")
		return
	}
	response.Success(c, user)
}

// Update handles PUT /api/users/:id
// @Summary Update user
// @Description Update username and role for a user (admin only)
// @Tags users
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "User ID"
// @Param request body models.UpdateUserRequest true "Update user request"
// @Success 200 {object} response.Response{data=models.User}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/users/{id} [put]
func (h *UserHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid user id")
		return
	}

	var req models.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	user, err := h.service.Update(c.Request.Context(), id, req)
	if err != nil {
		switch {
		case errors.Is(err, repositories.ErrUserNotFound):
			response.NotFound(c, "user not found")
		case errors.Is(err, repositories.ErrUserExists):
			response.BadRequest(c, "username already exists")
		case errors.Is(err, services.ErrLastAdminProtected):
			response.Error(c, http.StatusBadRequest, err.Error())
		default:
			response.InternalServerError(c, "failed to update user")
		}
		return
	}
	response.Success(c, user)
}

// Delete handles DELETE /api/users/:id
// @Summary Delete user
// @Description Delete a user (admin only)
// @Tags users
// @Produce json
// @Security BearerAuth
// @Param id path int true "User ID"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/users/{id} [delete]
func (h *UserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid user id")
		return
	}

	operatorIDRaw, _ := c.Get("userID")
	operatorID, _ := operatorIDRaw.(int64)

	err = h.service.Delete(c.Request.Context(), id, operatorID)
	if err != nil {
		switch {
		case errors.Is(err, repositories.ErrUserNotFound):
			response.NotFound(c, "user not found")
		case errors.Is(err, services.ErrCannotDeleteSelf), errors.Is(err, services.ErrLastAdminProtected):
			response.BadRequest(c, err.Error())
		default:
			response.InternalServerError(c, "failed to delete user")
		}
		return
	}
	response.Success(c, gin.H{"message": "user deleted successfully"})
}
