package handlers

import (
	"encoding/json"
	"net/http"
)

// APIResponse 统一 API 响应结构
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// respondJSON 发送 JSON 响应
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondSuccess 发送成功响应
func respondSuccess(w http.ResponseWriter, data interface{}) {
	respondJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    data,
	})
}

// respondCreated 发送创建成功响应
func respondCreated(w http.ResponseWriter, data interface{}) {
	respondJSON(w, http.StatusCreated, APIResponse{
		Success: true,
		Message: "创建成功",
		Data:    data,
	})
}

// respondError 发送错误响应
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, APIResponse{
		Success: false,
		Error:   message,
	})
}

// respondBadRequest 发送 400 错误
func respondBadRequest(w http.ResponseWriter, message string) {
	respondError(w, http.StatusBadRequest, message)
}

// respondUnauthorized 发送 401 错误
func respondUnauthorized(w http.ResponseWriter, message string) {
	respondError(w, http.StatusUnauthorized, message)
}

// respondForbidden 发送 403 错误
func respondForbidden(w http.ResponseWriter, message string) {
	respondError(w, http.StatusForbidden, message)
}

// respondNotFound 发送 404 错误
func respondNotFound(w http.ResponseWriter, message string) {
	respondError(w, http.StatusNotFound, message)
}

// respondInternalError 发送 500 错误
func respondInternalError(w http.ResponseWriter, message string) {
	respondError(w, http.StatusInternalServerError, message)
}


