package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"opsplatform-deploy-backend/config"
)

var Cfg *config.Config

func SetConfig(c *config.Config) { Cfg = c }

type resp struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func JSONSuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp{Code: 0, Message: "success", Data: data})
}

func JSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	httpStatus := http.StatusOK
	switch code {
	case 40000, 40001:
		httpStatus = http.StatusBadRequest
	case 40400:
		httpStatus = http.StatusNotFound
	case 40900:
		httpStatus = http.StatusConflict
	default:
		if code >= 50000 {
			httpStatus = http.StatusInternalServerError
		}
	}
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(resp{Code: code, Message: msg})
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		JSONError(w, 40000, "invalid json: "+err.Error())
		return false
	}
	return true
}

func ParseID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func jsonUnmarshalImpl(raw []byte, v interface{}) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}
