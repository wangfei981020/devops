package handlers

import (
	"encoding/json"
	"net/http"

	"opsplatform-deploy-backend/config"
)

var cfg *config.Config

func SetConfig(c *config.Config) {
	cfg = c
}

// Resp 通用响应
type Resp struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// Page 分页响应
type Page struct {
	Total int64       `json:"total"`
	List  interface{} `json:"list"`
}

func writeJSON(w http.ResponseWriter, code int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func jsonSuccess(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, Resp{Code: 0, Message: "success", Data: data})
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	httpStatus := http.StatusOK
	if code == 40400 {
		httpStatus = http.StatusNotFound
	} else if code == 40000 || code == 40001 {
		httpStatus = http.StatusBadRequest
	} else if code == 40100 {
		httpStatus = http.StatusUnauthorized
	} else if code == 40300 {
		httpStatus = http.StatusForbidden
	} else if code == 40900 {
		httpStatus = http.StatusConflict
	} else if code >= 50000 {
		httpStatus = http.StatusInternalServerError
	}
	writeJSON(w, httpStatus, Resp{Code: code, Message: msg, Data: nil})
}

func decodeJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}
