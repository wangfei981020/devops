package handlers

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

func CORSMiddleware(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				log.Printf("[panic] %v\n%s", rv, debug.Stack())
				JSONError(w, 50000, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingRW wraps ResponseWriter to capture status code and (for 5xx) response body
//
//	也实现 http.Flusher，让 SSE handler（vm-logs / pod-logs）的 Flush 调用透传过去
type loggingRW struct {
	http.ResponseWriter
	status      int
	bodyBuf     bytes.Buffer // 只在 5xx 时收集前 1KB
	captureBody bool
}

func (lrw *loggingRW) WriteHeader(s int) {
	lrw.status = s
	if s >= 500 {
		lrw.captureBody = true
	}
	lrw.ResponseWriter.WriteHeader(s)
}

func (lrw *loggingRW) Write(b []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = 200
	}
	if lrw.captureBody && lrw.bodyBuf.Len() < 1024 {
		rem := 1024 - lrw.bodyBuf.Len()
		if rem > len(b) {
			rem = len(b)
		}
		lrw.bodyBuf.Write(b[:rem])
	}
	return lrw.ResponseWriter.Write(b)
}

// Flush 让底层 Flusher 透传，SSE 才能流式推送
func (lrw *loggingRW) Flush() {
	if f, ok := lrw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// LoggingMiddleware 入站访问日志：每个请求一行 stdout，5xx 还会把响应里的 message 字段抽出来。
//
//	格式：[req] <user> | METHOD path | status | duration[ | err="..."]
//	user 取自 AuthMiddleware 注入的 ctx；未登录请求 user=-
//	5xx 时尝试解析 JSON 响应的 message 字段（JSONError / InternalErr 都返这个 schema），让 stdout 直接看到根因
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingRW{ResponseWriter: w}
		next.ServeHTTP(lrw, r)

		user := UsernameFromCtx(r)
		if user == "" {
			user = "-"
		}
		status := lrw.status
		if status == 0 {
			status = 200
		}
		line := "[req] " + user + " | " + r.Method + " " + r.URL.Path +
			" | " + statusStr(status) + " | " + time.Since(start).Truncate(time.Millisecond).String()
		if status >= 500 && lrw.bodyBuf.Len() > 0 {
			if msg := extractRespMessage(lrw.bodyBuf.Bytes()); msg != "" {
				line += ` | err="` + msg + `"`
			}
		}
		log.Print(line)
	})
}

func statusStr(s int) string {
	// 避免 import strconv 仅为这一行；用 fmt 等价但 Sprintf 也得引；纯字符串拼接最便宜
	// 状态码三位数，固定长度
	d1 := byte('0' + (s/100)%10)
	d2 := byte('0' + (s/10)%10)
	d3 := byte('0' + s%10)
	return string([]byte{d1, d2, d3})
}

// extractRespMessage 解析 JSONError / InternalErr 返的 {code, message, ...}，提 message
func extractRespMessage(b []byte) string {
	var v struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return ""
	}
	// 截断超长消息（避免日志爆）
	if len(v.Message) > 500 {
		return v.Message[:500] + "..."
	}
	return v.Message
}
