package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/api/middleware"
	"ops-alert-backend/internal/license"
	"ops-alert-backend/logx"
	"ops-kit/licensekit"
)

// 功能门控。
//
// # 与权限（perm.go）的分工
//
//	权限   这个**人**能不能做      → 403 forbidden
//	授权   这套**部署**买没买      → 402 payment_required
//
// 两者都过不了时先报授权：告诉用户"你没权限"而他其实是没买，
// 会让他去找管理员要权限，而管理员也给不了。
//
// # 为什么是表驱动而不是逐个挂中间件
//
// 与 perm.go 同一个理由：漏挂一个就是一个白送的功能，而且新加路由必然会忘。
// 这里**不做 fail-closed** —— 没列进表的路由属于 CE，本来就该所有人可用。
// 这与权限相反：权限漏配是安全问题，授权漏配只是少收一笔钱。
var featureRoutes = []struct {
	Prefix  string
	Feature license.Feature
}{
	{"/api/v1/backtests", license.FeatureBacktest},
	{"/api/v1/noise", license.FeatureNoise},
	// ⚠️ /api/v1/import **不在这里**，也是刻意的（第二次犯同类错误后改的）。
	// 导入旧规则是**迁移工具**：把它锁在付费墙后面，客户连数据都搬不进来，
	// 也就永远走不到"觉得好用愿意付费"那一步。
	// 回归测试当场抓到了这个 —— 三条导入相关的断言全变成 402。
	// ⚠️ /api/v1/silences **不在这里**，是刻意的。
	// 临时静默是值班的保命操作：半夜被同一条告警刷屏却压不住，
	// 人只会去关掉整个通知渠道 —— 那比给他静默危险得多。
	// EE 卖的是**降噪治理**（关联合并、抑制规则、噪音榜与调参建议），
	// 不是"能不能让一条告警闭嘴"。
}

// FeatureGuard 按功能拦截。挂在 PermGuard 之后。
func (s *Server) FeatureGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		full := c.FullPath()
		if full == "" || s.lic == nil {
			c.Next()
			return
		}
		for _, r := range featureRoutes {
			if !strings.HasPrefix(full, r.Prefix) {
				continue
			}
			if s.lic.Has(r.Feature) {
				break
			}
			// ⚠️ 402 而不是 403：前端要能把"没买"和"没权限"分开提示。
			// 混成同一个码，客户会去找管理员要权限，而管理员也给不了。
			logx.J("license", "feature_denied", map[string]any{
				"path": full, "feature": string(r.Feature),
			})
			c.AbortWithStatusJSON(http.StatusPaymentRequired, gin.H{
				"error":   "feature_not_licensed",
				"feature": string(r.Feature),
				// implemented=false 时是"这版还没做"，不是"没买"——
				// 两者的下一步完全不同（等版本 vs 去采购）
				"implemented": license.Implemented(r.Feature),
			})
			return
		}
		c.Next()
	}
}

// licenseStatus 授权状态。前端用它决定哪些入口要标"企业版"。
func (s *Server) licenseStatus(c *gin.Context) {
	if s.lic == nil {
		c.JSON(http.StatusOK, gin.H{"status": "community", "features": gin.H{}})
		return
	}
	feats := gin.H{}
	for _, f := range license.AllFeatures() {
		feats[string(f)] = gin.H{
			// has = 真的能用（授权里有 **且** 这版实现了）。业务判断只看它。
			"has": s.lic.Has(f),
			// 🔴 granted 与 implemented 必须**分别给出**，不能只给 has。
			//
			// Has() 已经把 implemented 折进去了：授权里买了、但这版还没做的功能，
			// Has() 返回 false。界面若只拿 has 判断，就会把这类显示成「需要授权」——
			// 于是客户去催采购，而他其实已经买了，该做的是等版本。
			// 实测撞到过：plan:enterprise 含 sso，界面却说"需要授权"。
			"granted":     s.lic.Granted(string(f)),
			"implemented": license.Implemented(f),
		}
	}
	days, hasExpiry := s.lic.DaysUntilExpiry()
	out := gin.H{
		"status":    string(s.lic.Status()),
		"read_only": s.lic.ReadOnly(),
		"features":  feats,
		// 提前多少天提醒由授权规范定（licensekit），**不要让前端自己算天数** ——
		// 前端定一个值就会和后端对不上，表现为"提醒了但状态还没到"或反过来
		"should_remind": s.lic.ShouldRemind(),
		// ⚠️ null = 不适用（永久授权 / 未激活），0 = 今天到期。
		// 压成 0 会让"永久授权"显示成"今天到期"
		"days_until_expiry": nil,
	}
	if hasExpiry {
		out["days_until_expiry"] = days
	}
	if p := s.lic.Payload(); p != nil {
		out["perpetual"] = p.Perpetual
		out["license_id"] = p.LicenseID
		out["licensee"] = p.Licensee
		if !p.ExpiresAt.IsZero() {
			out["expires_at"] = p.ExpiresAt
		}
		if g, ok := p.Products[license.ProductID]; ok {
			out["granted_features"] = g.Features
			out["capacity"] = g.Capacity
		}
	}
	// 容量：未激活时用 CE 上限，激活后用授权里的（0=不限）。
	// 两者都给出来，界面才说得清"当前上限是多少、为什么是这个数"
	out["ce_limits"] = license.CELimits()
	// 指纹放在单独的接口里（它要连库算），这里只说有没有 Store。
	// 前端据此决定要不要显示"申请授权"那一块
	out["can_activate"] = s.licStore != nil
	c.JSON(http.StatusOK, out)
}

// activateLicense 贴激活码。
//
// # 为什么验签在这里做，落库在 Store 做
//
// 无效的码**绝不能落库**。落了之后每个副本的 Watch 都会读到它、验签失败、
// 退回未激活 —— 于是"贴了一串错码"表现成"整套系统突然未激活"，
// 而管理员看到的只是一个模糊的失败提示，很难联想到是自己那一步。
// 在这里先验，不通过就原样退回，库里的旧授权一个字节都不动。
func (s *Server) activateLicense(c *gin.Context) {
	if s.lic == nil || s.licStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "license_unavailable", "detail": "本部署没有接入授权模块"})
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	// 粘贴时最常见的问题是带了首尾空白和换行（从邮件/聊天里复制）。
	// 不 trim 的话验签必然失败，而错误信息是"签名不通过"——
	// 指向的方向完全错了，人会去怀疑签发方而不是自己的剪贴板。
	token := strings.TrimSpace(req.Token)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "empty_token", "detail": "激活码是空的"})
		return
	}
	p, err := licensekit.VerifyEmbedded(token)
	if err != nil {
		// ⚠️ 说"验签不通过"，**别说"已过期"**。
		// 说成过期会把客户引去续费，而真正的问题是这串码本身不对
		// （粘贴时少了一段、换了签发环境、文件被改过）。
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  "invalid_token",
			"detail": "激活码验签不通过（不是过期）：" + err.Error() + "。请确认复制完整、且来自正确的签发环境",
		})
		return
	}
	// ⚠️ 一份 license 可以含多个产品。这里只要求**含本产品**，
	// 不要求"只含本产品"—— 同一份码装在几套系统上是正常用法。
	//
	// 但必须挡住"完全不含本产品"的那种：不挡的话会落库成功、验签通过、
	// 而一个功能都没开，表现为"激活成功了却没变化"，管理员会反复贴同一串码。
	grant, ok := p.Products[license.ProductID]
	if !ok {
		names := make([]string, 0, len(p.Products))
		for k := range p.Products {
			names = append(names, k)
		}
		sort.Strings(names)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "wrong_product",
			"detail": fmt.Sprintf("这串码里没有 %s 的授权（它含：%s）。请确认拿到的是本产品的激活码",
				license.ProductID, strings.Join(names, ", ")),
		})
		return
	}

	user := middleware.CurrentUser(c)
	if err := s.licStore.Activate(c.Request.Context(), token, user.Username); err != nil {
		abortQuery(c, err)
		return
	}
	// 本副本立刻装载，不等 Watch 的下一个周期 —— 否则管理员点完"激活"
	// 刷新一下还是未激活，会以为没成功而反复贴。其余副本由 Watch 在 20s 内收敛。
	if err := s.licStore.Reload(c.Request.Context(), s.lic); err != nil {
		logx.J("license", "reload_after_activate_failed", map[string]any{"err": err.Error()})
	}
	sc, err := s.st.Tenant(c.Request.Context())
	if err == nil {
		s.audit(c, sc, "license.activate", "license", 1,
			gin.H{"features": grant.Features, "expires_at": p.ExpiresAt, "licensee": p.Licensee})
	}
	c.JSON(http.StatusOK, gin.H{
		"status":   string(s.lic.Status()),
		"features": grant.Features,
		// ⚠️ 明确回一句"其余副本 20s 内生效"。不说的话，管理员在另一个
		// 浏览器标签（可能打到别的 Pod）看到仍是未激活，会以为激活失败了
		"note": "本副本已生效；其余副本将在 20 秒内收敛",
	})
}

// licenseFingerprint 安装指纹。签发授权时要把它给签发方。
func (s *Server) licenseFingerprint(c *gin.Context) {
	if s.licStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "license_unavailable"})
		return
	}
	fp, err := s.licStore.Fingerprint(c.Request.Context())
	if err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"fingerprint": fp})
}
