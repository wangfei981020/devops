package handlers

import (
	"encoding/json"
	"net/http"
)

type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func respondJSON(w http.ResponseWriter, httpCode int, resp apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	json.NewEncoder(w).Encode(resp)
}

func respondSuccess(w http.ResponseWriter, data interface{}) {
	respondJSON(w, http.StatusOK, apiResponse{Code: 0, Message: "success", Data: data})
}

func respondBadRequest(w http.ResponseWriter, msg string) {
	respondJSON(w, http.StatusBadRequest, apiResponse{Code: 400, Message: msg})
}

func respondInternalError(w http.ResponseWriter, msg string) {
	respondJSON(w, http.StatusInternalServerError, apiResponse{Code: 500, Message: msg})
}

func respondError(w http.ResponseWriter, httpCode int, msg string) {
	respondJSON(w, httpCode, apiResponse{Code: httpCode, Message: msg})
}
