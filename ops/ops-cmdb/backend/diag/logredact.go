package diag

import "regexp"

// 日志里的凭据脱敏（OPSCMDB-065）。
//
// # 为什么这件事必须做，尽管做不干净
//
// 打这些日志是**业务应用**的问题（应用自己 INFO 级打印了解析后的配置），
// 不是 CMDB 造成的。但 CMDB 作为只读工具把它**转发**出去，等于扩大了泄露面 ——
// 而同一个产品在另一处的态度完全相反：`get_manifest` 的说明里写着「Secret 拒绝返回」。
//
// **拒绝返回 Secret 对象、却原样返回日志里的 Secret 内容**，
// 对拿到 MCP 只读令牌的人来说，后者比前者更省事。
//
// # 为什么不追求"脱干净"
//
// 无法自动判断任意字符串是不是密码。而**过度脱敏会毁掉排障价值** ——
// 本轮正是靠 log_tails 才找到「库名含换行符」那个 bug（OPSCMDB-064）。
//
// 所以这里只做**已知形态**：覆盖不全，但能挡住绝大部分。
// 挡不住的那部分由调用方看到的提示说清楚（见 RedactLogTail 的返回值）。
//
// ⚠️ 千万不要为了这条把 log_tails 砍掉。它是 diagnose_pod 最有价值的部分。
var logCredPatterns = []*regexp.Regexp{
	// key=value / key: value 形式的口令。
	// ⚠️ 值不能匹配到行尾的空白与逗号 —— 日志常把多个字段拼在一行，
	//	贪婪匹配会把后面的字段一起吃掉，排障时就少了上下文。
	regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|api[_-]?key|auth|credential)[a-z0-9_]*\s*[=:]\s*)([^\s,;)}"']+)`),
	// 连接串里的 userinfo：只打口令段，保留 scheme/user/host（那两项是排障要看的）
	regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^:/@\s]+:)([^@/\s]+)(@)`),
	// Go 结构体打印出来的 `Password:xxx`（fmt 的 %+v 形态，没有空格也没有引号）
	regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token)\s*:)([^\s,}\]]+)`),
	// webhook / API 路径里的长随机段
	regexp.MustCompile(`((?i:/(?:hook|webhook|bot/v2/hook|services|token)/))([A-Za-z0-9_\-]{16,})`),
	// 🔴 Go 的 DSN：`user:pass@tcp(host:port)/db` —— **没有 scheme://**。
	//	上面那条 userinfo 正则是照着标准 URL 写的，对它完全无效。
	//	生产实测的形态就是这个（go-sql-driver/mysql 的连接串）。
	regexp.MustCompile(`([a-zA-Z0-9_.-]+:)([^@\s]+)(@(?:tcp|unix|udp)\()`),
}

// credVarThenValue 敏感变量名与值**分在两个键上**的形态。
//
// 🔴 生产实测：
//
//	parseConfLine …, var=REDIS_PASSWORD, …, realVal=<明文口令>
//
//	敏感词在 `var=` 的**值**里，口令在另一个键 `realVal=` 下 ——
//	"敏感词紧邻它的值"这个假设在这里根本不成立。
//	这类日志是配置解析器打的，形态很稳定，值得单独认。
//
// ⚠️ 只在**同一行**内关联。跨行关联会把不相干的值打掉。
var credVarThenValue = regexp.MustCompile(
	`(?i)(var\s*=\s*[a-z0-9_]*(?:password|passwd|pwd|secret|token|api[_-]?key)[a-z0-9_]*\b.*?` +
		`(?:realval|value|val)\s*=\s*)([^\s,;)}"']+)`)

// LogRedactedMark 脱敏后的占位符。与 manifest 那边保持一致，
// 让人在两个工具里看到同一个记号就知道是同一件事。
const LogRedactedMark = "***REDACTED***"

// RedactLogTail 脱敏一段日志，返回 (脱敏后的文本, 命中处数)。
//
// 命中处数要给调用方 —— 它决定要不要在输出里加那句
// 「日志里可能还有没识别出来的凭据」。**报 0 处不等于干净**，
// 只等于"已知形态里没有命中"。
func RedactLogTail(s string) (string, int) {
	if s == "" {
		return s, 0
	}
	n := 0
	out := s
	// 先处理"敏感变量名 + 另一个键上的值"，它比通用模式更具体 —— 放前面避免被通用规则抢先改坏
	out = credVarThenValue.ReplaceAllStringFunc(out, func(m string) string {
		sub := credVarThenValue.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		n++
		return sub[1] + LogRedactedMark
	})
	for _, re := range logCredPatterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) < 3 {
				return m
			}
			n++
			// 三段式（连接串、webhook）保留第三段；两段式直接接占位符
			if len(sub) >= 4 && sub[3] != "" {
				return sub[1] + LogRedactedMark + sub[3]
			}
			return sub[1] + LogRedactedMark
		})
	}
	return out, n
}
