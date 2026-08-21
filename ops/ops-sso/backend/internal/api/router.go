// Package api 是 HTTP 层：路由与 handler。
//
// 两条结构铁律（CI 会检查）：
//  1. 本层**不碰数据库** —— 不 import database/sql，一律经 domain 的 Repo
//  2. 不拼中文 —— 错误一律走 apierr 的码
package api

import (
	"crypto/ed25519"
	"net/http"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/domain/appcat"
	"ops-sso-backend/internal/domain/approval"
	"ops-sso-backend/internal/domain/auth"
	"ops-sso-backend/internal/domain/idp"
	"ops-sso-backend/internal/domain/mfa"
	"ops-sso-backend/internal/domain/oidcp"
	"ops-sso-backend/internal/domain/pathpolicy"
	"ops-sso-backend/internal/license"
	"ops-sso-backend/internal/secrets"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/logx"
)

// Deps handler 依赖。显式传进来而不是用包级变量 ——
// 包级变量会让测试之间互相污染，且看不出一个 handler 到底用了什么。
type Deps struct {
	Access   *access.Repo
	AppCat   *appcat.Repo
	Path     *pathpolicy.Repo
	Auth     *auth.Service
	Audit    *Auditor
	MFA      *mfa.Repo
	IdP      *idp.Repo
	Approval *approval.Repo
	OIDCP    *oidcp.Repo
	// Issuer 对外的 OIDC 签发方标识。下游用它校验 id_token 的 iss，
	// 也是 discovery 里所有端点的前缀 —— 配错的话所有下游全部验签失败。
	Issuer string
	// RunMode dev/prod。界面用它说明「这台的 issuer 有没有过启动校验」。
	RunMode string
	Store   *store.Store
	Secrets *secrets.Box
	License *license.Manager
	// LicenseStore 授权的库这一面：算指纹、读写激活码。
	// 与 License（内存状态）分开 —— 状态查询是热路径，不该每次碰库。
	LicenseStore *license.Store
	// AnchorKey 审计锚点私钥。nil = 不签锚点（每轮会记日志，不静默）
	AnchorKey ed25519.PrivateKey
	Version   string
	GitSHA    string
	Edition   string
}

// Register 挂载所有路由。
func Register(r *gin.Engine, d Deps) {
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	// 系统信息：客户报障时第一个要看的东西，不需要认证也不含敏感信息
	r.GET("/api/v1/system/info", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"product": license.ProductID,
			"version": d.Version,
			"gitsha":  d.GitSHA,
			"edition": d.Edition,
			"license": gin.H{
				"status":    d.License.Status(),
				"can_write": d.License.CanWrite(),
			},
			// ★ 如实上报：这个二进制**真正实现了**什么。
			// license 给了但没实现的，报 not_implemented 而不是 not_granted ——
			// 把没做的功能说成"未购买"，客户付完钱当天就会发现。
			"features": d.License.Report(),
		})
	})

	// ── OpenID Provider：下游应用来连我们 ──
	//
	// discovery 与 jwks 必须是**完全公开**的：客户端库在配置阶段就会拉它们，
	// 那时还没有任何凭证。
	// 协议端点挂在**根路径**下，不带 /api/v1 ——
	// 它们是对外的协议表面，不是我们自己的 REST API，不该跟着我们的版本走。
	// ⚠️ 这些路径必须与 oidcp.Discovery() 声明的完全一致：
	// 对不上的话，客户端库会照着 discovery 去调然后拿到 404，
	// 而对方的第一反应是"我配错了"。TestDiscoveryPathsAreRouted 钉住这一点。
	r.GET("/.well-known/openid-configuration", h(d, oidcDiscovery))
	r.GET("/oidc/jwks", h(d, oidcJWKS))
	// token 端点用 client_secret 认证，不走会话
	r.POST("/oidc/token", h(d, oidcToken))
	r.GET("/oidc/userinfo", h(d, oidcUserinfo))
	// authorize 需要人已登录 —— 但未登录时要跳登录页而不是报 401，
	// 所以用 optionalAuth 而不是 authRequired
	r.GET("/oidc/authorize", optionalAuth(d), h(d, oidcAuthorize))
	r.GET("/oidc/logout", optionalAuth(d), h(d, oidcLogout))

	// 登录不需要已认证
	pub := r.Group("/api/v1/auth")
	pub.POST("/login", h(d, login))
	pub.POST("/login/break-glass", h(d, loginBreakGlass))
	// 上游身份源：登录页要显示按钮、跳转、接回调，都在认证之前
	pub.GET("/idp-list", h(d, idpList))
	pub.GET("/oidc/start", h(d, idpStart))
	pub.GET("/oidc/callback", h(d, idpCallback))

	// ★ passwordChangeGuard 必须在**所有**业务接口之前。
	// 只在前端跳转到改密页是能绕过的（直接调接口即可），
	// 而"强制改密"的全部意义就在于它不能被绕过。
	v1 := r.Group("/api/v1", authRequired(d), passwordChangeGuard(), licenseWriteGuard(d))

	// ── 我 ──
	v1.GET("/auth/me", h(d, me))
	v1.POST("/auth/logout", h(d, logout))
	v1.POST("/auth/password", h(d, changePassword))
	v1.GET("/auth/sessions", h(d, mySessions))
	v1.DELETE("/auth/sessions/:id", h(d, revokeSession))

	// ── 二次验证 ──
	v1.GET("/mfa/status", h(d, mfaStatus))
	v1.POST("/mfa/enroll", h(d, mfaBeginEnroll))
	v1.POST("/mfa/enroll/confirm", h(d, mfaConfirmEnroll))
	v1.POST("/mfa/challenge", h(d, mfaChallenge))

	// ══════════════════════════════════════════════════════════════
	// 控制台：**仅租户管理员**
	// ══════════════════════════════════════════════════════════════
	//
	// 在这个组建立之前，任何登录用户都能调下面的全部接口 —— 改策略、
	// 看全员会话、踢别人下线、换 license。前端把控制台放在 /console
	// 只是"没给入口"，不是"进不去"：直接调接口就行，而接口从不问你是谁。
	//
	// ⚠️ 新增控制台接口一律加进这个组。挂在组上的好处是**默认被保护**，
	// 要放行必须显式挪到 v1 上，那是个看得见的动作；而逐个 handler 里判
	// 的漏法是静默放行 —— 忘了写那两行，接口从此不问权限，什么都不报。
	adm := v1.Group("", adminOnly())

	// ── 应用分组 ──
	adm.GET("/app-groups", h(d, listGroups))
	adm.POST("/app-groups", h(d, createGroup))
	adm.PUT("/app-groups/:id", h(d, updateGroup))
	adm.DELETE("/app-groups/:id", h(d, deleteGroup))
	adm.GET("/app-groups/:id/impact", h(d, groupDeleteImpact))

	// ── 应用 ──
	//
	// 普通用户看应用走 /portal/apps（只返回他看得见的那些）。
	// 这个返回的是**全部**应用，包括他无权访问的 —— 那是一份系统清单。
	adm.GET("/apps", h(d, listApps))
	adm.POST("/apps", h(d, createApp))
	adm.PUT("/apps/:id", h(d, updateApp))
	adm.DELETE("/apps/:id", h(d, deleteApp))
	// 删除前的影响面预览。单独一个接口而不是塞进 DELETE 的返回：
	// 界面要在**人点确认之前**就把数字摆出来。
	adm.GET("/apps/:id/dependents", h(d, appDependents))
	adm.PUT("/apps/:id/groups", h(d, setAppGroups))
	// 接入指引：把"这个应用配到哪一步了"摊开。只回**当前进程与库里的事实**，
	// 不回文档 —— 文档会过期，而接入卡住的三件事全是"值"。
	adm.GET("/apps/:id/onboarding", h(d, appOnboarding))

	// ── 访问授权 ──
	adm.GET("/policies", h(d, listPolicies))
	adm.POST("/policies", h(d, createPolicy))
	adm.DELETE("/policies/:id", h(d, deletePolicy))
	adm.POST("/policies/simulate", h(d, simulate))
	// 授权关系：谁能进哪个应用（从规则反推出来的结果，不是规则本身）
	adm.GET("/access-matrix", h(d, accessMatrix))

	// ── 总览 / 人员 / 在线会话 ──
	adm.GET("/overview", h(d, overview))
	adm.GET("/users", h(d, adminUsers))
	// 写操作。每一个都能把人锁在系统外面 —— 边界（不能动自己、
	// 不能动最后一个管理员）在 handler 里判，见 user_admin.go 开头。
	adm.POST("/users", h(d, adminCreateUser))
	adm.PUT("/users/:id/groups", h(d, adminSetUserGroups))
	adm.PUT("/users/:id/status", h(d, adminSetUserStatus))
	adm.PUT("/users/:id/role", h(d, adminSetUserRole))
	adm.POST("/users/:id/reset-password", h(d, adminResetPassword))
	adm.GET("/user-groups", h(d, adminUserGroups))
	adm.GET("/sessions", h(d, adminSessions))
	adm.GET("/provider-info", h(d, providerInfo))
	// ⚠️ 路径不能和 /auth/sessions/:id 混用：那个只能下线自己的，
	// 这个能下线任何人的。两种权限的东西共用一个入口迟早会串。
	adm.DELETE("/sessions/:id", h(d, adminRevokeSession))

	// ── 路径级策略（网关判定用）──
	adm.GET("/path-rules", h(d, listPathRules))
	adm.POST("/path-rules", h(d, createPathRule))
	adm.DELETE("/path-rules/:id", h(d, deletePathRule))
	adm.POST("/path-rules/simulate", h(d, simulatePath))

	// ── 拨测探针 ──
	//
	// 探针配的是"用哪个账号去登哪个站"，等于一份可用凭据的清单。
	adm.GET("/probes", h(d, listProbes))
	adm.POST("/probes", h(d, upsertProbe))
	adm.DELETE("/probes/:id", h(d, revokeProbe))

	// ── 审计 ──
	//
	// 审计里有全公司谁访问了什么。给普通用户看等于把行为日志公开。
	adm.GET("/access-events", h(d, listAccessEvents))
	adm.GET("/audit-logs", h(d, listAuditLogs))
	adm.GET("/audit-logs/verify", h(d, verifyAuditChain))
	// 锚点：哈希链发现不了"整链重算"，锚点能。
	// ⚠️ 但锚点存在本库里等于没锚 —— 导出到系统之外那一步才是它起作用的地方。
	adm.GET("/audit-anchors", h(d, listAnchors))
	adm.POST("/audit-anchors", h(d, createAnchor))
	adm.GET("/audit-anchors/export", h(d, exportAnchors))

	// ── 临时提权：审批侧 ──
	//
	// ⚠️ **申请（POST）不在这里**，它是普通用户的动作，挂在 v1 上。
	// 审批自己的申请是最经典的越权，两者必须分开。
	adm.POST("/access-requests/:id/decide", h(d, decideRequest))
	adm.DELETE("/access-requests/:id", h(d, revokeRequest))

	// ── 网关路由：哪个域名转到哪个后端，按哪个应用判权限 ──
	//
	// 这是零改造接入绕不过去的一步。在它之前只能直接写库 ——
	// 核心卖点在界面上走不完。
	adm.GET("/app-routes", h(d, listRoutes))
	adm.POST("/app-routes", h(d, saveRoute))
	adm.PUT("/app-routes/:id", h(d, saveRoute))
	adm.DELETE("/app-routes/:id", h(d, deleteRoute))

	// ── 身份源（飞书 / Teams / 其他 OIDC）──
	//
	// 登录用的公开接口早就有（/idp-list、/oidc/start、/oidc/callback），
	// 但一直没有管理接口 —— 要接一个身份源只能直接写库。
	adm.GET("/idp-configs", h(d, idpAdminList))
	adm.POST("/idp-configs", h(d, idpAdminSave))
	adm.PUT("/idp-configs/:id", h(d, idpAdminSave))
	adm.POST("/idp-configs/discover", h(d, idpDiscover))

	// ── OIDC 客户端管理 ──
	//
	// 建一个 OIDC 客户端 = 造一把能换 token 的钥匙。
	adm.GET("/oidc-clients", h(d, listOIDCClients))
	adm.POST("/oidc-clients", h(d, createOIDCClient))

	// ── 授权（License）──
	//
	// 读状态**不限管理员**：授权过期时整站转只读，普通用户也该能看到
	// "为什么突然改不了东西"，而不是对着一堆失败的操作猜。
	v1.GET("/license", h(d, licenseStatus))
	// 激活必须是管理员：换一份不含本产品的授权，就能把整套系统变成只读。
	// license 回答"这个功能买没买"，权限回答"这个人能不能操作"，两者正交，
	// licenseWriteGuard 挡不住这件事。
	adm.POST("/license", h(d, licenseActivate))

	// ⚠️ 应急通行码签发必须是管理员：这个接口接受**任意 user_id**，
	// 挂在普通组上等于谁都能给自己签一批"绕开 SSO 也能进"的口令。
	adm.POST("/auth/break-glass/issue", h(d, issueBreakGlass))

	// ── 普通用户也能做的 ──
	//
	// 提申请是每个人的权利；能不能批是另一回事（在 adm 组里）。
	v1.POST("/access-requests", h(d, createRequest))

	// 看申请列表也是每个人的权利 —— **但只能看自己的**。
	// 收紧到管理员是过度收紧：普通用户连"我提的那条批了没"都查不到，
	// 只能回去群里问，而这一整套流程存在的意义就是让它不必回到群里。
	// 归属过滤在 handler 里强制（非管理员一律按 mine=自己），
	// 不能靠前端传 mine=1 —— 那等于让调用方自己声明能看什么。
	v1.GET("/access-requests", h(d, listRequests))

	// ── 门户 ──
	v1.GET("/portal/apps", h(d, portalApps))
	v1.GET("/portal/grants", h(d, myGrants))
	v1.GET("/portal/status", h(d, portalStatus))
}

// handlerFunc 是本项目 handler 的统一签名：返回 (body, error)，
// 由 h() 统一转成 HTTP。这样 handler 里不会散落 c.JSON(500, ...) 这种写法，
// 错误格式也就不会各处不一致。
type handlerFunc func(c *gin.Context, d Deps) (any, error)

func h(d Deps, fn handlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := fn(c, d)
		if err != nil {
			writeErr(c, err)
			return
		}
		if body == nil {
			c.Status(http.StatusNoContent)
			return
		}
		c.JSON(http.StatusOK, body)
	}
}

func writeErr(c *gin.Context, err error) {
	if e, ok := err.(*apierr.Error); ok {
		c.JSON(e.Status, e)
		return
	}
	if err == store.ErrNoTenantContext {
		c.JSON(http.StatusUnauthorized, apierr.New(http.StatusUnauthorized, apierr.CodeNoTenant, nil))
		return
	}
	// 兜底：不把原始错误直接吐给客户端 —— 里面可能有表名、SQL、内网地址。
	//
	// 但**服务端必须留痕**。第一版只调了 c.Error()，而 gin.New() 没挂日志中间件，
	// 于是 500 在服务端一个字都不留 —— 客户报障时我们什么都查不到，
	// 只能让他"再试一次看看"。这比错误本身更伤。
	logx.Line("api", "500 "+c.Request.Method+" "+c.Request.URL.Path+": "+err.Error())
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, apierr.Internal(""))
}
