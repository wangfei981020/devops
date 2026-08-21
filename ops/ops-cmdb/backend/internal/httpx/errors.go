package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIError 结构化错误响应。
//
// ⚠️ 后端**不返回给用户看的句子**，只返回 code + 参数，由前端翻译。
//
// 原因很实际：后端拼好中文句子发过去，英文界面就永远漏中文，
// 而且只在错误路径上出现 —— 正常测试根本走不到，能一直活到客户手里。
// 前端已经因为同样的问题踩过一次（错误态里混进中文），不能在后端再踩。
//
// Message 字段仍然保留，但它是**给运维看的英文技术描述**，不是给用户的文案。
type APIError struct {
	// Code 机器可读的错误码，如 "cluster_unreachable"
	Code string `json:"code"`
	// MessageKey 前端语言包里的 key，如 "error.clusterUnreachable"
	MessageKey string `json:"message_key"`
	// Params 插值参数，如 {"cluster":"g32-prod","timeout":10}
	Params map[string]any `json:"params,omitempty"`
	// Message 英文技术描述，给日志和运维排查用，前端不展示给终端用户
	Message string `json:"message,omitempty"`
	// RequestID 贯穿一次请求，用户截图里能看到，运维据此捞日志
	RequestID string `json:"request_id,omitempty"`
}

// 错误码常量。新增时同步在前端语言包加 error.<key>，
// check-i18n.mjs 会保证两种语言都补齐。
const (
	CodeBadRequest         = "bad_request"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeUpstreamTimeout    = "upstream_timeout"
	CodeUpstreamError      = "upstream_error"
	CodeInternal           = "internal"
	CodeReadOnly           = "read_only"         // license 过期降级
	CodeCapacityExceeded   = "capacity_exceeded" // 超出授权容量
	CodeFeatureNotLicensed = "feature_not_licensed"
)

// httpStatus 错误码到 HTTP 状态码的映射。
//
// 单独一张表而不是让调用方每次传：同一个错误在不同 handler 里返回不同状态码，
// 前端的重试策略就没法统一（该重试的没重试，不该重试的一直重试）。
var httpStatus = map[string]int{
	CodeBadRequest:         http.StatusBadRequest,
	CodeUnauthorized:       http.StatusUnauthorized,
	CodeForbidden:          http.StatusForbidden,
	CodeNotFound:           http.StatusNotFound,
	CodeConflict:           http.StatusConflict,
	CodeUpstreamTimeout:    http.StatusGatewayTimeout,
	CodeUpstreamError:      http.StatusBadGateway,
	CodeInternal:           http.StatusInternalServerError,
	CodeReadOnly:           http.StatusForbidden,
	CodeCapacityExceeded:   http.StatusForbidden,
	CodeFeatureNotLicensed: http.StatusForbidden,
}

// messageKey 错误码到前端语言包 key 的默认映射。
var messageKey = map[string]string{
	CodeBadRequest:         "error.badRequest",
	CodeUnauthorized:       "error.unauthorized",
	CodeForbidden:          "error.forbidden",
	CodeNotFound:           "error.notFound",
	CodeConflict:           "error.conflict",
	CodeUpstreamTimeout:    "error.upstreamTimeout",
	CodeUpstreamError:      "error.upstreamError",
	CodeInternal:           "error.unknown",
	CodeReadOnly:           "error.readOnly",
	CodeCapacityExceeded:   "error.capacityExceeded",
	CodeFeatureNotLicensed: "error.featureNotLicensed",
}

// ── 参数化模板 ──
//
// 这几个覆盖了存量里重复最多的句式（"cluster_id 必填" ×12、"xxx 不存在" ×20+）。
// 用模板而不是给每一句一个 key：315 条独立文案里有近三分之一是同一个句式换个名词，
// 逐条建 key 会让语言包膨胀成一张同义词表，翻译的人也无从判断哪些该保持一致。
//
// ⚠️ 参数值本身**不要塞中文**。`Required(c, "集群")` 会让英文界面显示
// "集群 is required" —— 中英混排比全中文更难读。传字段名（cluster_id）
// 或让前端按 what 的取值自己映射（见 KindKey）。

// Required 必填校验失败。field 传**接口参数名**（cluster_id），不是中文标签。
func Required(c *gin.Context, field string) {
	FailKey(c, CodeBadRequest, "error.required", nil, map[string]any{"field": field})
}

// RequiredAll 多个必填字段一起缺。
//
// ⚠️ 传**接口参数名**，不是中文标签。前端把它们连成
// "缺少必填参数 cluster_id、namespace"，中英各按自己的顿号规则拼。
func RequiredAll(c *gin.Context, fields ...string) {
	if len(fields) == 1 {
		Required(c, fields[0])
		return
	}
	FailKey(c, CodeBadRequest, "error.requiredAll", nil, map[string]any{"fields": fields})
}

// NotFound 对象不存在。kind 传对象种类的机器名（user/domain/cluster/…），
// 前端按 KindKey 翻译成"用户/域名/集群"。
func NotFound(c *gin.Context, kind string) {
	FailKey(c, CodeNotFound, "error.notFoundKind", nil, map[string]any{"kind": kind})
}

// Invalid 取值非法。field 是参数名，want 描述期望（如 "YYYY-MM"）。
//
// ⚠️ want 是**格式描述**不是中文句子：前端把它原样显示在
// "cluster_id must be YYYY-MM" 这种模板里，塞中文进去就穿帮了。
func Invalid(c *gin.Context, field, want string) {
	FailKey(c, CodeBadRequest, "error.invalidValue", nil, map[string]any{"field": field, "want": want})
}

// Fail 写一个结构化错误响应并中止后续处理。
//
//	httpx.Fail(c, httpx.CodeUpstreamTimeout, err, map[string]any{"cluster": name})
func Fail(c *gin.Context, code string, cause error, params map[string]any) {
	status, ok := httpStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	e := APIError{
		Code:       code,
		MessageKey: messageKey[code],
		Params:     params,
		RequestID:  RequestID(c),
	}
	if cause != nil {
		// 技术细节只进 message 和日志，不进 message_key ——
		// 原始 error 通常是英文栈信息，直接甩给用户既看不懂也不可行动
		e.Message = cause.Error()
	}
	c.AbortWithStatusJSON(status, e)
}

// FailKey 同 Fail，但用自定义的语言包 key（同一个 code 需要多种文案时）。
func FailKey(c *gin.Context, code, key string, cause error, params map[string]any) {
	status, ok := httpStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	e := APIError{Code: code, MessageKey: key, Params: params, RequestID: RequestID(c)}
	if cause != nil {
		e.Message = cause.Error()
	}
	c.AbortWithStatusJSON(status, e)
}

// FailKeyWith 同 FailKey，但可以附带额外字段。
//
// 🔴 存在的理由：有些失败**必须同时给出逐行明细**。
//
//	批量写 DNS 时后端会逐行校验，`errors` 数组里是"第几行为什么没做成"——
//	前端注释写得很清楚：「必须展示，否则"部分成功"看着像全成功」。
//	用 FailKey 就把那个数组丢了，界面上只剩一句"全部行校验不通过"，
//	而人下一个问题必然是"哪一行、为什么"。
//
// ⚠️ extra 里**不要**放给用户看的成句文案 —— 那又绕回后端拼中文了。
//
//	放的应该是结构化明细（行号、字段名、机器可读的原因码）。
func FailKeyWith(c *gin.Context, code, key string, params, extra map[string]any) {
	status, ok := httpStatus[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	body := map[string]any{
		"code":        code,
		"message_key": key,
		"request_id":  RequestID(c),
	}
	if len(params) > 0 {
		body["params"] = params
	}
	for k, v := range extra {
		// 不许覆盖结构化错误的固定字段：覆盖了前端就认不出这是个错误响应
		switch k {
		case "code", "message_key", "params", "request_id":
			continue
		}
		body[k] = v
	}
	c.AbortWithStatusJSON(status, body)
}

// RequestID 取当前请求的 ID。
// 现有中间件已经在响应头里写了 X-Request-Id，这里复用同一个值，
// 保证用户截图里的 ID 和日志里的能对上。
func RequestID(c *gin.Context) string {
	if v := c.Writer.Header().Get("X-Request-Id"); v != "" {
		return v
	}
	if v, ok := c.Get("request_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
