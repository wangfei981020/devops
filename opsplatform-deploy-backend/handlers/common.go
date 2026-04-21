package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"opsplatform-deploy-backend/config"
)

var Cfg *config.Config

// MaxRequestBodyBytes 限制请求体大小，防止 OOM
const MaxRequestBodyBytes = 1 << 20 // 1 MiB

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
	case 40100:
		httpStatus = http.StatusUnauthorized
	case 40300:
		httpStatus = http.StatusForbidden
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
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		JSONError(w, 40000, "请求体格式错误或超限")
		log.Printf("[DecodeJSON] %s %s: %v", r.Method, r.URL.Path, err)
		return false
	}
	return true
}

// InternalErr 对客户端返回通用 500 错误，完整错误信息只进服务端日志
// 替换所有 JSONError(w, 50000, err.Error()) 的写法
func InternalErr(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("[INTERNAL] %s %s: %v", r.Method, r.URL.Path, err)
	JSONError(w, 50000, "内部错误，请联系管理员查看服务端日志")
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
