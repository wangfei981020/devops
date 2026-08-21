package handlers

import (
	"regexp"
	"strings"
)

// ─────────────────────────────────────────────────────────────────
// 出站错误信息的脱敏。
//
// # 为什么需要
//
// 生产上实测：告警页环境筛选器报错，完整 SQL 错误被原样发到了前端，
// 并渲染进 DOM 的 title 属性：
//
//	Error 3065 (HY000): Expression #1 of ORDER BY clause is not in SELECT list,
//	references column 'ops_cmdb.obs_endpoints.env' which is not in SELECT list
//
// 一行里泄露了**库名 + 表名 + 列名**（验收会话 NEW-1）。企业版不该这样。
//
// 🔴 在**后端**收口，不在前端。前端脱敏是假的：值已经在 HTTP 响应里了，
//
//	F12 一看、或者拿 token 直接 curl 就是明文 ——
//	这和 CMDB-033/034 那两个 P0 是同一个道理（见 handlers/mask.go）。
//
// # 定位
//
// ⚠️ 这一层的目的是**不把内部结构讲给外面**，不是"隐藏错误"。
//
//	原始错误照常进日志（logx），排障的人拿得到；
//	发给前端的是"哪一步失败了"，不是"数据库的第几列不在 SELECT 里"。
//
// ⚠️ 也不能一律换成"系统错误"：那会把可自助处置的错误
//
//	（凭据过期、权限不足、目标不存在）也糊掉，人就只能来问我们。
//	所以按类型分档 —— 能安全说清的照常说清。
// ─────────────────────────────────────────────────────────────────

var (
	// MySQL/驱动的结构性错误：整句都可能带库名表名列名
	reSQLStructural = regexp.MustCompile(`(?i)\b(Error \d{4}|SQLSTATE|ORDER BY clause|SELECT list|` +
		`Unknown column|Table '[^']*' doesn't exist|Duplicate entry|near ['"]|syntax to use near)`)
	// 形如 'db.table.column' / `db`.`table`
	reQualifiedIdent = regexp.MustCompile("['\"`][A-Za-z0-9_]+\\.[A-Za-z0-9_]+(\\.[A-Za-z0-9_]+)?['\"`]")
	// 内网地址与端口。
	// ⚠️ 三个网段的**剩余段数不一样**：10.x 还剩 3 段、127.x 还剩 3 段、
	//	而 192.168 已经占了两段、只剩 2 段。写成同一个 `\.\d+` 重复次数
	//	会漏掉 192.168.x.x（单测抓到过）。所以分开写。
	rePrivateHost = regexp.MustCompile(
		`\b(?:(?:10|127)(?:\.\d{1,3}){3}|192\.168(?:\.\d{1,3}){2}|` +
			// 172.16.0.0/12
			`172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2})(?::\d+)?\b`)
	// k8s 集群内 DNS
	reClusterDNS = regexp.MustCompile(`\b[a-z0-9-]+\.[a-z0-9-]+\.svc(\.cluster\.local)?(:\d+)?\b`)
)

// SafeErr 把一个内部错误转成可以发给前端的说法。
//
// what 是给人看的那一步，如「查夜莺接入环境」。返回形如：
//
//	查夜莺接入环境失败（数据库错误，详见服务端日志）
//
// ⚠️ 调用方**仍然要把原始错误写进日志**。这个函数只管出站那一份 ——
//
//	两边都糊掉的话，排障就真的没法做了。
func SafeErr(what string, err error) string {
	if err == nil {
		return what + "失败"
	}
	msg := err.Error()

	// ① SQL 结构性错误：整句不外发。它几乎必然带库名表名列名，
	//	而对使用者来说这句话没有任何可操作性 —— 是我们的 bug，不是他的输入问题。
	if reSQLStructural.MatchString(msg) {
		return what + "失败（数据库错误，详见服务端日志）"
	}

	// ② 其余错误保留原文，但抹掉里面的内部标识符与地址。
	//	超时、连不上、401/403 这类对使用者是**有用**的（他能据此判断是不是自己配错了），
	//	糊成"系统错误"反而逼人来问我们。
	msg = reQualifiedIdent.ReplaceAllString(msg, "[已移除]")
	msg = rePrivateHost.ReplaceAllString(msg, "[内网地址]")
	msg = reClusterDNS.ReplaceAllString(msg, "[集群内地址]")

	// ③ 兜底长度。超长的错误串往往是把整个响应体拼进来了
	const max = 240
	if len(msg) > max {
		msg = strings.TrimSpace(msg[:max]) + "…"
	}
	return what + "失败：" + msg
}
