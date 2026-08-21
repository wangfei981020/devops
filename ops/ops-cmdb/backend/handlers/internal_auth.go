package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"

	"github.com/gin-gonic/gin"
)

// 进程内自调用的鉴权。
//
//	## 这个文件为什么存在
//
//	MCP 的每个工具都是「把一个内部 API 再用 HTTP 调一遍」（mcp.go 的 internalGet
//	回调 127.0.0.1 上的自己）。原先这类回调自签一个 JWT 混过鉴权中间件。
//	后来鉴权从 JWT 换成会话表（auth_sessions），中间件只认「库里有这条会话」，
//	而进程内回调从来不写会话表 —— **MCP 的 36 个工具全部返回「登录已失效」**。
//	AI 拿不到任何数据，而且报错长得像 token 过期，很容易被误当成配置问题。
//
//	## 为什么不是把 JWT 加回来
//
//	加回 JWT 意味着系统里同时存在两套凭据体系：会话表能撤销、JWT 不能。
//	那正是当初换掉 JWT 的原因（撤权时踢不掉已签发的 token）。为了一个
//	进程内回调把它请回来，等于把刚堵上的口子又开一条缝。
//
//	## 现在的做法
//
//	进程启动时生成一个随机令牌，只存在**内存**里：
//	  - 不落库、不签发、不外发，重启即失效
//	  - 只有本进程的 internalGet 拿得到它
//	  - 中间件用常数时间比较认它，认过之后按「只读的机器身份」放行
//
//	它不是"另一套用户"，而是"进程在调自己"的标记。也因此，
//	用它进来的请求在审计里必须标成 mcp（见 mcp.go 的 c.Set(ctxAuthSource, "mcp")），
//	不能混进人的操作里。

// internalToken 进程内回调令牌。包级变量、进程生命周期内不变、不导出。
var internalToken = mustRandomToken()

func mustRandomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// 拿不到随机源就让进程起不来：静默退化成固定值等于没有这道门
		panic("生成进程内回调令牌失败: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// isInternalCall 判断是否本进程的自调用。
// 用常数时间比较，避免通过响应耗时逐字节猜令牌。
func isInternalCall(raw string) bool {
	if raw == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(raw), []byte(internalToken)) == 1
}

// internalRoleHeader 进程内回调用它声明"这次以谁的身份调"。
//
// ⚠️ 这个头**只在内部令牌校验通过之后**才被读取（见 Middleware）。
// 内部令牌只存在于本进程内存、不落库不外发，所以外部请求带上这个头没有任何作用。
// 顺序反了就是个提权漏洞：谁都能加一行 header 变成管理员。
const internalRoleHeader = "X-Internal-Role"

// internalActorHeader 真正的调用方标识（MCP 令牌的名字），只用于审计。
const internalActorHeader = "X-Internal-Actor"

// setInternalIdentity 给进程内回调装上身份。
//
//	## 为什么不能直接给管理员身份
//
//	这里原本写的是 `ctxIsAdmin = true`，理由是"MCP 的工具全是只读的"。
//	只读没错，但**只读不等于可以读全部**：成本、日志、IAM、凭据引用
//	都在只读接口里。给一张管理员通行证，等于任何一个接了 MCP 的 AI
//	都能读到这个客户的全部资产——而客户配的那套角色对这条路完全不生效。
//
//	自用时看不出问题（就一个人），做成商业产品就是硬伤：
//	给外包接一个 AI 助手 = 把全量资产一起给了。
//
//	现在按令牌绑定的角色走**与人完全相同**的权限码校验：
//	同一个权限表、同一个 PermGuard，没有第二条通道。
func setInternalIdentity(c *gin.Context, db *sql.DB) {
	actor := c.GetHeader(internalActorHeader)
	if actor == "" {
		actor = "mcp"
	}
	c.Set(ctxUsername, actor)
	c.Set(ctxUserID, 0)
	c.Set(ctxAuthSource, "mcp")

	role := c.GetHeader(internalRoleHeader)
	if role == "" {
		// 没声明角色 = 升级前留下的老令牌，保持原样不受限。
		// ⚠️ 不能改成"默认只读"：那会让正在用的接入方在升级这一下突然查不到东西，
		// 而现象是"AI 昨天还能查成本，今天说没权限"，没有任何地方提示发生过什么。
		// 降权必须是管理员在界面上做的决定，见 migration 109。
		c.Set(ctxPerms, map[string]bool{})
		c.Set(ctxIsAdmin, true)
		return
	}
	perms, unrestricted := permsOfLocalRole(db, role)
	c.Set(ctxPerms, perms)
	c.Set(ctxIsAdmin, unrestricted)
}
