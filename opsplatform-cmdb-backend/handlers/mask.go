package handlers

import "strings"

// 面向"发给前端的响应"的脱敏。
//
//	## 为什么必须在后端做
//
//	前端打码是**假的**：值已经在 HTTP 响应里了，F12 一看、或者拿 token
//	直接 curl 接口就是明文。生产上实测到的两个 P0 都栽在这：
//	  CMDB-033  证书 deploy_token 明文渲染在输入框里 → 只读账号能导出私钥
//	  CMDB-034  Lark webhook URL 明文展示           → 拿到就能往运维群发伪造告警
//	两轮独立验证（只读账号 / 管理员）都撞到同一处，说明这不是渲染问题，
//	是**接口本身把凭据发给了不该看的人**。
//
//	约定：凭据类字段一律"默认掩码，有权限才给真值"，而不是反过来。
//	反过来写的话，新加一个字段忘了脱敏就直接泄露，且不会有任何报错。

// maskToken 掩码一个令牌/密钥：保留头 4 尾 4，中间固定长度的星号。
//
//	保留首尾是为了让人能核对"是不是我配的那个"，而固定星号长度是刻意的——
//	按真实长度打星会把长度泄露出去，对爆破是有用信息。
func maskToken(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "********"
	}
	return s[:4] + "********" + s[len(s)-4:]
}

// maskWebhookURL 掩码 webhook 地址。
//
//	Lark/飞书这类 webhook **URL 本身就是凭据**——不需要任何额外鉴权，
//	谁拿到谁就能往那个群发消息。所以要保留到"能认出是哪个群"为止，
//	把真正是密钥的那一段（路径末段的 hook id）打掉。
//
//	https://open.larksuite.com/open-apis/bot/v2/hook/abcd-1234-efgh
//	→ https://open.larksuite.com/open-apis/bot/v2/hook/abcd****efgh
func maskWebhookURL(u string) string {
	if u == "" {
		return ""
	}
	i := strings.LastIndex(u, "/")
	if i < 0 || i == len(u)-1 {
		return maskToken(u)
	}
	return u[:i+1] + maskToken(u[i+1:])
}

// isMasked 判断这个值是不是我们发出去的掩码（而不是用户真填的）。
//
//	必须有它：脱敏之后，界面上拿到的就是掩码串，用户不改这一项直接点保存，
//	提交回来的就是 `abcd********efgh`——原样入库的话，**一次保存就把 webhook
//	写坏了**，而且直到下次告警发不出去才会发现。
//	所以更新路径遇到掩码值一律当成"没改这一项"，保留库里的原值。
func isMasked(s string) bool {
	return strings.Contains(s, "********")
}
