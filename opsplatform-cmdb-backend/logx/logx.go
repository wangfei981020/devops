// Package logx 结构化 JSON 日志：一行一个 JSON，便于 grep / 接日志系统。
// 用独立无前缀 logger 写 stdout（k8s 采集），ts 放进 JSON，不影响其它 log.Printf。
package logx

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"
)

var l = log.New(os.Stdout, "", 0)

// ctxKey 请求 id 在 context 里的键（避免碰撞用独立类型）。
type ctxKey struct{}

// WithRequestID 把 request_id 放进 context，供下游（含 dnsource）日志携带。
func WithRequestID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, ctxKey{}, rid)
}

// RequestID 从 context 取 request_id（无则空串）。
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// Line 把一条文本日志包成 JSON（{tag,event:"log",msg}）。用于把零散的 log.Printf 统一成 JSON 格式，
// 不必逐条拆成结构化字段；关键操作日志仍用 J/JCtx 的富字段形式。
func Line(tag, msg string) {
	J(tag, "log", map[string]any{"msg": msg})
}

// JCtx 带 request_id 的 JSON 日志（从 ctx 取 request_id 自动加进字段）。
func JCtx(ctx context.Context, tag, event string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	if rid := RequestID(ctx); rid != "" {
		fields["request_id"] = rid
	}
	J(tag, event, fields)
}

// J 输出一条 JSON 日志。tag 标类别（dns_write / godaddy_write / domain_renew / db …），
// event 标事件（create_start / success / fail …），其余字段随传。
func J(tag, event string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["ts"] = time.Now().Format("2006-01-02T15:04:05Z07:00")
	fields["tag"] = tag
	fields["event"] = event
	b, err := json.Marshal(fields)
	if err != nil {
		l.Printf(`{"tag":%q,"event":%q,"log_err":%q}`, tag, event, err.Error())
		return
	}
	l.Println(string(b))
}
