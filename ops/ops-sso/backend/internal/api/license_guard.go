package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
)

// licenseWriteGuard 授权过期后进入**只读**，而不是停服。
//
// # 这条规则是写进合同的
//
// 供应商不该有「随时关掉客户公司登录」这个能力。所以：
//
//	访问判定（网关那条路径）—— 根本不查 license，过期与否毫无影响
//	读接口                 —— 一律放行
//	写接口                 —— 过期后拒绝，提示去续期
//
// # 为什么白名单是"退出/会话"而不是"全部 auth"
//
// 登录、退出、下线设备在过期后必须还能用：
// 一个过期后连退出都点不动的系统，会让客户第一反应是"被绑架了"。
// 但签发应急口令属于配置变更，过期后不给做。
func licenseWriteGuard(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isWrite(c.Request.Method) {
			c.Next()
			return
		}
		if alwaysAllowed(c.Request.URL.Path) {
			c.Next()
			return
		}
		if d.License == nil || d.License.CanWrite() {
			c.Next()
			return
		}
		// 把状态一起给出去：前端要据此分文案。只说"只读"的话，客户不知道是
		// 过期了、超期了、还是这份授权不含本产品 —— 三种的下一步完全不同
		// （续费 / 重新采购 / 加购产品）。
		c.AbortWithStatusJSON(http.StatusPaymentRequired,
			apierr.New(http.StatusPaymentRequired, apierr.CodeLicenseReadOnly,
				map[string]any{"status": string(d.License.Status())}))
	}
}

func isWrite(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// alwaysAllowed 即使在只读期也必须能用的写操作。
//
// 判断用后缀匹配而不是前缀：`/api/v1/auth/logout` 与
// 未来可能出现的 `/api/v2/auth/logout` 应当一视同仁。
var readOnlySafe = []string{
	"/auth/logout",   // 退出：过期后连退出都点不动，客户会觉得被绑架了
	"/mfa/challenge", // 二次验证：正在操作的人不该被半路截断
	"/policies/simulate",
	"/path-rules/simulate", // 试算是只读的，只是用了 POST 传参
	// 贴激活码：**唯一的自救通道**。不放行的话，过期之后连续费都装不上，
	// 客户付了钱也只能找我们远程改库。
	// ⚠️ 后缀匹配到的是 POST /api/v1/license，GET 本来就不受只读影响。
	"/license",
}

func alwaysAllowed(path string) bool {
	for _, s := range readOnlySafe {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}
