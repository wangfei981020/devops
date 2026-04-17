package handlers

import (
	"encoding/json"
	"net/http"
)

func jsonResponse(w http.ResponseWriter, code int, msg string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    code,
		"message": msg,
		"data":    data,
	})
}

func jsonSuccess(w http.ResponseWriter, data interface{}) {
	jsonResponse(w, 0, "success", data)
}

func jsonError(w http.ResponseWriter, httpCode int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":    -1,
		"message": msg,
	})
}
