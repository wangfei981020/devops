package store

import "strings"

// # 租户参数该插在第几位
//
// 早期实现把租户号无脑放在参数列表**最前面**。对 SELECT / DELETE 是对的
// （`WHERE tenant_id = ? AND ...` 里它就是第一个占位符），但 UPDATE 不是：
//
//	UPDATE registrars SET name=?, provider=? WHERE tenant_id = ? AND id=?
//	                           1         2                  3        4
//
// 租户号是第 3 个占位符。放到最前面的话，绑定变成
// name=租户号、provider=name、tenant_id=provider —— 每一列都错位。
//
// # 为什么这个 bug 能活下来
//
// 跨租户测试**抓不到它**：tenant_id 被绑成一个字符串，WHERE 匹配不到任何行，
// 返回 0 行 → 404，和"正确地拒绝了越权"的表现一模一样。
// 而当时没有一条"改自己的行并逐字段核对"的用例，
// 于是一个把每列都写错的实现，测试全绿。
//
// 结论：**只测拒绝路径是不够的**，成功路径必须核对落库的值。

// placeholdersBefore 数 query 前 n 个字符里有多少个占位符 ?，
// 跳过字符串字面量与注释里的问号。
func placeholdersBefore(query string, n int) int {
	count := 0
	for i := 0; i < n && i < len(query); i++ {
		switch query[i] {
		case '\'', '"', '`':
			q := query[i]
			i++
			for i < n && i < len(query) {
				if query[i] == '\\' { // 转义：跳过下一个字符
					i++
				} else if query[i] == q {
					break
				}
				i++
			}
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				for i < len(query) && query[i] != '\n' {
					i++
				}
			}
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				if j := strings.Index(query[i+2:], "*/"); j >= 0 {
					i += 2 + j + 1
				} else {
					i = len(query)
				}
			}
		case '?':
			count++
		}
	}
	return count
}

// insertAt 把租户号插进 args 的第 pos 位。
func insertAt(id TenantID, args []any, pos int) []any {
	if pos < 0 || pos > len(args) {
		pos = 0 // 兜底：位置算不出来时退回原行为，由 checkFilter 保证语句里有租户条件
	}
	out := make([]any, 0, len(args)+1)
	out = append(out, args[:pos]...)
	out = append(out, int64(id))
	return append(out, args[pos:]...)
}

// tenantArgPos 算出租户号在 WHERE 型语句里该占的参数下标。
func tenantArgPos(query string) int {
	loc := tenantFilter.FindStringIndex(query)
	if loc == nil {
		return 0 // 调用方已经过 checkFilter，走不到这里
	}
	return placeholdersBefore(query, loc[0])
}

// insertTenantColPos 算出租户号在 INSERT 语句里该占的参数下标 ——
// 由 tenant_id 在**列清单**里的位置决定，不是由它在语句里的字符位置决定。
//
//	INSERT INTO hosts (name, tenant_id, zone) VALUES (?, ?, ?)
//	                                  ↑ 第 2 列 → 参数下标 1
//
// 现有代码都把 tenant_id 写在第一列，但把这条规则实现出来，
// 才不会在有人换个写法时静默错位。
func insertTenantColPos(query string) int {
	open := strings.Index(query, "(")
	if open < 0 {
		return 0
	}
	close := strings.Index(query[open:], ")")
	if close < 0 {
		return 0
	}
	cols := strings.Split(query[open+1:open+close], ",")
	for i, c := range cols {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(c), "`"), "tenant_id") {
			return i
		}
	}
	return 0
}
