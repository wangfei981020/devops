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

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondSuccess(w http.ResponseWriter, data interface{}) {
	respondJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    data,
	})
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, APIResponse{
		Success: false,
		Error:   message,
	})
}

func respondBadRequest(w http.ResponseWriter, message string) {
	respondError(w, http.StatusBadRequest, message)
}

func respondUnauthorized(w http.ResponseWriter, message string) {
	respondError(w, http.StatusUnauthorized, message)
}

func respondForbidden(w http.ResponseWriter, message string) {
	respondError(w, http.StatusForbidden, message)
}

func respondInternalError(w http.ResponseWriter, message string) {
	respondError(w, http.StatusInternalServerError, message)
}
