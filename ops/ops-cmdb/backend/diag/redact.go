package diag

import "regexp"

// 日志脱敏 —— 喂给 AI 之前、以及写进审计之前，都要过这一道。
//
// 🔴 与 handlers/mask.go 是两件事：
//
//	那边脱敏的是**结构化字段**（我们自己知道哪个字段是凭据）；
//	这边脱敏的是**自由文本**（业务日志里可能夹着任何东西，我们不知道在哪）。
//	所以那边能"默认掩码"，这边只能靠模式匹配 —— 天然不可能覆盖全。
//
// ⚠️ 因此这一层的定位是**减少泄露面，不是保证不泄露**。
//
//	不能因为有了它就认为"日志可以随便发出去"。
//	真正的边界是：AI 兜底默认关闭，开之前要人明确知道日志会出站。
//
// ⚠️ 顺序有讲究：先脱最长最具体的（JWT、私钥块），再脱通用的键值对。
//
//	反过来的话，通用规则会先把 JWT 的一部分吃掉，剩下的片段反而更难识别。
var redactors = []struct {
	re   *regexp.Regexp
	with string
}{
	// PEM 私钥块：整块换掉，绝不能只换首行
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
		"[已移除:私钥]"},
	// JWT：三段 base64url
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
		"[已移除:JWT]"},
	// Authorization 头
	{regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(bearer\s+)?\S+`), "${1}[已移除]"},
	// 连接串里的密码：scheme://user:pass@host
	{regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+:)[^\s@]+(@)`), "${1}[已移除]${2}"},
	// 通用键值：password/passwd/secret/token/api_key/apikey/access_key
	{regexp.MustCompile(`(?i)\b(pass(word|wd)?|secret|token|api[_-]?key|access[_-]?key)\b(\s*[:=]\s*)("?)[^\s"',;]{4,}("?)`),
		"${1}${3}[已移除]"},
	// Cloudflare / GCP 常见的长随机串（40+ 位 base62），宁可错杀
	{regexp.MustCompile(`\b[A-Za-z0-9_-]{40,}\b`), "[已移除:长随机串]"},
}

// Redact 对一段自由文本做脱敏。
//
// ⚠️ 返回的字符串可能仍含敏感信息（见上方定位说明）。
// 调用方不得据此认为"这段可以随便外发"。
func Redact(s string) string {
	for _, r := range redactors {
		s = r.re.ReplaceAllString(s, r.with)
	}
	return s
}
