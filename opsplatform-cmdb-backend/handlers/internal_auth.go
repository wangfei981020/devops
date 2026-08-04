package handlers

import (
	"crypto/rand"
	"crypto/subtle"
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

// setInternalIdentity 给进程内回调装上身份。
//
//	MCP 的 36 个工具全是只读的，所以这里放行读操作；
//	但**不是给它一张管理员通行证**——它拿到的是一个权限表为空、
//	仅 isAdmin 放行读路径的机器身份，来源标成 mcp 便于审计区分。
func setInternalIdentity(c *gin.Context) {
	c.Set(ctxUsername, "mcp")
	c.Set(ctxUserID, 0)
	c.Set(ctxAuthSource, "mcp")
	c.Set(ctxPerms, map[string]bool{})
	c.Set(ctxIsAdmin, true)
}
