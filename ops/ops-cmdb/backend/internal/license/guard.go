package license

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 只读降级与功能门控（LICENSING §5 / §13.5）。

// readMethods 不受授权状态影响的方法。
//
// ⚠️ 只读降级**只拦写操作**。把读也锁掉，客户连自己的数据都导不出来 ——
// 那不是催款，是扣押。而且被扣押的第一件事往往就是"导出数据去核对账单"。
func isWrite(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// alwaysWritable 即使在只读降级下也必须放行的写接口。
//
// 判据只有一条：**这个写操作是不是"恢复授权"本身需要的**。
// 不放行的话，过期之后连激活新授权都做不到 —— 客户付了钱也救不回来，
// 只能找我们远程改库。登录同理：进不去就贴不了激活码。
var alwaysWritable = map[string]bool{
	"POST /api/login":         true,
	"POST /api/logout":        true,
	"POST /api/license":       true, // 贴激活码
	"PUT /api/me/password":    true, // 改自己的密码，与授权无关
	"POST /api/refresh-token": true,
}

// ReadOnlyGuard 授权过期/超期时把系统降级成只读。
//
// 挂在鉴权之后、业务之前。
func ReadOnlyGuard(mgr *Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isWrite(c.Request.Method) || !mgr.ReadOnly() {
			c.Next()
			return
		}
		full := c.FullPath()
		if full == "" {
			full = c.Request.URL.Path
		}
		if alwaysWritable[c.Request.Method+" "+full] {
			c.Next()
			return
		}
		// 把状态一起给出去：前端要据此分文案。
		// 只说"只读"的话，客户不知道是过期了、超期了、还是没买这个产品 ——
		// 三种的下一步完全不同（续费 / 重新采购 / 加购产品）。
		httpx.Fail(c, httpx.CodeReadOnly, nil, map[string]any{
			"status": string(mgr.Status()),
		})
		c.Abort()
	}
}

// featureRoutes 路由 → 所需功能。**写接口**用这张表。
//
// 🔴 「只拦写」这条规则属于 ReadOnlyGuard，**不属于这里**。两者判的是两件事：
//
//	ReadOnlyGuard  授权**过期了** → 只拦写。读要放行 ——
//	               连自己的数据都导不出来那不是催款是扣押。
//	FeatureGuard   这个功能**没买** → 读写都要拦。
//	               放行读的话，价值全在"读"的功能（暴露面、合规报表）永远免费。
//
//	这两条规则一度混在一起（FeatureGuard 也写着"只拦写"），
//	后果是 exposure 这种**纯只读功能**在设计上就挡不住：
//	它在 features_missing 里、侧栏打了 EE 标记、而接口 200 返回全量数据
//	（生产 v0.122.0 实测，OPSCMDB-072）。
//	**标了 EE 却不拦，等于界面在说一句不成立的话。**
//
// # 为什么是一张表，而不是在每个 Register 里逐个挂
//
// 与 handlers/perm.go 同一种做法：表驱动才能被遍历、被测试、被一眼审计完。
// 逐路由挂的话，"这个功能到底管住了哪些接口"必须翻遍所有 handler 才答得上来，
// 而漏挂一条是没有任何迹象的 —— 那条接口就是白送。
//
// # 加新条目的前提
//
// 目标 feature 必须同时在 implemented 里点亮，否则 Has() 恒为 false，
// 接口对**所有人**关闭（包括已购的客户）。TestFeatureRoutesAreImplemented
// 会在两者不一致时直接失败。
//
// ⚠️ key 用的是路由模板（c.FullPath()），不是实际路径 ——
// "/api/gke/version-schedule/:id" 而不是 "/api/gke/version-schedule/7"。
var featureRoutes = map[string]Feature{
	// 成本归因：改单价、打快照都会影响账单口径，属 professional 档
	"POST /api/k8s/cost/node-override": FeatureCost,
	"POST /api/k8s/cost/snapshot":      FeatureCost,

	// 升级治理：改排期、存基线
	"PUT /api/gke/version-schedule/:id":             FeatureVersionUpgrade,
	"DELETE /api/gke/version-schedule/:id/override": FeatureVersionUpgrade,
	"POST /api/gke/upgrade/baseline":                FeatureVersionUpgrade,

	// 审计回滚：standard 档就有
	"POST /api/audit-changes/:cid/revert": FeatureAuditRollback,
	"POST /api/audit-logs/:id/revert":     FeatureAuditRollback,

	// SSO 接入：改身份源配置。
	//
	// ⚠️ 只挡**配置**，不挡登录链路（/api/auth/sso/* 是 GET，且注册在
	// 鉴权中间件之前，根本走不到这里）。这是刻意的：授权过期时把已经配好的
	// SSO 登录一起关掉，等于因为欠费把客户全员锁在门外 —— 那不是催款是扣押。
	"PUT /api/idp-config":           FeatureSSO,
	"POST /api/idp-config/discover": FeatureSSO,

	// MCP 令牌：发凭据属于集成能力。
	//
	// ⚠️ 同样只挡"发新令牌"，不挡已发出去的令牌调用（/api/mcp 是 POST，
	// 但它注册在鉴权中间件之前，走的是自己的令牌校验，到不了这里）。
	// CE 的基础工具集要照常可用，工具级分档在 handlers/mcp.go
	"POST /api/mcp/tokens": FeatureMCPFull,
}

// featureReadRoutes 只读功能的路由 → 所需功能。
//
// 单独一张表而不是和 featureRoutes 合并：合并之后就分不清
// 「这条读接口是有意挡的」和「这条写接口顺带把读也挡了」——
// 而误挡一条读接口的表现是**已购客户打开页面一片空白**，
// 排查时会先怀疑数据没采到，最后才想到授权。
//
// ⚠️ 用**前缀**匹配（路由模板的前缀），因为一个只读功能通常是一整片接口：
//	暴露面有列表、facets、详情、导出，逐条列会漏。
//	但前缀必须写得足够长 —— `/api/e` 这种会误伤一大片。
//
// ⚠️ 加条目前先想清楚：**已经在用这个功能的客户会立刻失去它**。
//	那不是 bug，是门控生效的正常表现，但要有人提前知道（LICENSING §5）。
var featureReadPrefixes = []struct {
	Prefix  string
	Feature Feature
}{
	// 暴露面与合规：整片都是只读，价值全在"看得见"。
	// 这是第一个真正意义上的只读付费功能，也是暴露"只拦写"设计缺陷的那一个。
	{"/api/exposure-list", FeatureExposure},
}

// featureOfRead 这条读路由属于哪个功能。不属于任何功能时返回空。
func featureOfRead(path string) (Feature, bool) {
	for _, r := range featureReadPrefixes {
		if strings.HasPrefix(path, r.Prefix) {
			return r.Feature, true
		}
	}
	return "", false
}

// notRouteGated 已点亮、但**故意**不做路由级门控的功能。
//
// 空着是好事。往里加一条，就是往外白送一个功能 ——
// 所以必须写清楚为什么路由级挡不住它，由 TestFeatureRoutesAreImplemented 强制。
var notRouteGated = map[Feature]string{
	// MCP 的分档是**工具级**的：CE 给基础只读工具（本身可用），
	// EE 给全量。路由级只能整条 /api/mcp 一起开关，
	// 那样 CE 的 MCP 会完全不可用 —— 而 CE 能用正是它获客的方式。
	// 判定在 handlers/mcp.go 的 fullAllowed()。
	// （发新令牌这个写接口仍然做了路由级门控，见上。）
	FeatureMCPFull: "分档在工具级，见 handlers/mcp.go 的 mcpTool.EE",
}

// FeatureGuard 功能级门控：这个功能买没买。
//
// 挂在 ReadOnlyGuard 之后：先回答"系统是不是只读"，再回答"这个功能买没买"。
// 反过来的话，一个过期的实例会先被告知"没买这个功能"，
// 而他其实买了，只是过期了 —— 下一步动作完全不同（续费 vs 加购）。
//
// 🔴 读写都拦。**"只拦写"是 ReadOnlyGuard 的规则，不是这里的**（见 featureRoutes 的注释）。
func FeatureGuard(mgr *Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := PathOf(c)
		var f Feature
		var ok bool
		if isWrite(c.Request.Method) {
			f, ok = featureRoutes[c.Request.Method+" "+path]
		} else {
			// 只读功能：整片接口按前缀归属
			f, ok = featureOfRead(path)
		}
		if !ok || mgr.Has(f) {
			c.Next()
			return
		}
		httpx.Fail(c, httpx.CodeFeatureNotLicensed, nil, map[string]any{
			"feature": string(f),
			// 顺带给出当前状态，客户才知道是"没买这个功能"还是"授权过期了"，
			// 而不是拿着一个功能名去问销售
			"status": string(mgr.Status()),
		})
		c.Abort()
	}
}

// UnmatchedFeatureRoutes 返回 featureRoutes 里在实际路由表中找不到的条目。
//
// # 为什么必须在启动时核对
//
// 门控靠字符串匹配路由模板。路径改名、多一个前缀、`:id` 写成 `:cid` ——
// 表项就永远命中不了，而**失效方向是放行**：那条接口从此不问授权，
// 谁都能用，且没有任何报错、没有任何日志、界面上一切正常。
// 唯一能发现它的时刻就是启动时把两边对一遍。
//
// 返回值而不是直接打日志/panic：调用方（main）决定怎么处理。
// 不 panic 是刻意的 —— 一条表项写错不该让整个服务起不来，
// 那会把一个"少收一份钱"的问题放大成全站故障。
func UnmatchedFeatureRoutes(routes []gin.RouteInfo) []string {
	live := make(map[string]bool, len(routes))
	for _, r := range routes {
		live[r.Method+" "+r.Path] = true
	}
	var missing []string
	for key := range featureRoutes {
		if !live[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing) // 顺序稳定，否则每次启动日志都不一样
	return missing
}

// PathOf 取路由模板，测试与日志用。
func PathOf(c *gin.Context) string {
	if p := c.FullPath(); p != "" {
		return p
	}
	return strings.TrimSuffix(c.Request.URL.Path, "/")
}
