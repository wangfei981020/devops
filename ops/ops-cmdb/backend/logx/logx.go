// Package logx 结构化 JSON 日志：一行一个 JSON，便于 grep / 接日志系统。
// 用独立无前缀 logger 写 stdout（k8s 采集），ts 放进 JSON，不影响其它 log.Printf。
package logx

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
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

// —— 日志等级 ——
//
// 加这个是因为排查节点心跳误报时，光靠现有日志完全看不出判定过程：
// 只看到结果是「失联」，看不到用的哪个字段、原始时间是多少、跟哪个阈值比的。
// 这类判定必须能在生产打开细节，否则只能靠改代码重发来试。
//
// 用法：环境变量 LOG_LEVEL=debug|info|warn。
//
// ⚠️ 当前默认是 **debug**，是为了这轮排查。稳定后要改回 info ——
// debug 会把每个节点每轮采集都打一行，16 台节点 2 分钟一轮 = 每天约 1.2 万行。
const (
	LevelDebug = 0
	LevelInfo  = 1
	LevelWarn  = 2
)

var level = func() int {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "warn", "warning":
		return LevelWarn
	case "info":
		return LevelInfo
	case "debug", "":
		// 空 = debug：这轮排查期间的默认值。改回 info 时把这里也改掉，
		// 不要只改部署里的环境变量——两处不一致会让人以为关掉了其实没关
		return LevelDebug
	default:
		return LevelDebug
	}
}()

// Level 返回当前等级，供启动时打印确认（不然没人知道到底生效的是哪个）。
func Level() string {
	switch level {
	case LevelWarn:
		return "warn"
	case LevelInfo:
		return "info"
	default:
		return "debug"
	}
}

// D 调试日志。只在 LOG_LEVEL=debug 时输出，带 level:"debug" 字段便于过滤。
//
// ⚠️ 判定类的调试日志要把**输入、依据、结论**三样都打出来。
// 只打结论等于没打——那正是现在这个 bug 查了三个版本的原因。
func D(tag, event string, fields map[string]any) {
	if level > LevelDebug {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	fields["level"] = "debug"
	J(tag, event, fields)
}
