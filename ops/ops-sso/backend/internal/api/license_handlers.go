package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-kit/licensekit"
	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/license"
	"ops-sso-backend/logx"
)

// 授权状态与激活。规范见 ops/LICENSING.md。

// licenseStatus 读授权状态。
//
// **不挂只读拦截也不挂功能门控**：授权过期时，「查看授权状态」
// 恰恰是客户最需要打开的那一页。
func licenseStatus(c *gin.Context, d Deps) (any, error) {
	fp, err := d.LicenseStore.Fingerprint(c.Request.Context())
	if err != nil {
		// 指纹算不出来是真故障（库连不上），不能兜底成空串 ——
		// 空指纹会让客户拿着它去申请授权，签出来的那份永远对不上
		return nil, err
	}

	out := gin.H{
		"status":    string(d.License.Status()),
		"can_write": d.License.CanWrite(),
		"features":  d.License.Report(),
		// 完整指纹供复制按钮取值；截断的那个只是给人看的，
		// 拿它去签发会签出一份永远对不上的授权
		"fingerprint":      licensekit.ShortFingerprint(fp),
		"fingerprint_full": fp,
	}
	if days, ok := d.License.DaysUntilExpiry(); ok {
		out["days_until_expiry"] = days
		out["should_remind"] = d.License.ShouldRemind()
	}
	// 容量：LICENSING §11 要求状态页展示。
	//
	// 上限和**当前用量**要一起给：只给上限，看的人答不出「还差多远撞线」，
	// 而那正是他打开这一页想知道的事。
	// ⚠️ 用量算不出来时给 null 而不是 0 —— 0 会被读成"一个都没用"，
	// 那是把"不知道"报告成"很空"。
	out["capacity"] = capacityReport(c, d)

	if p := d.License.Payload(); p != nil {
		out["activated"] = true
		out["license_id"] = p.LicenseID
		// ⚠️ 只给组织名这一个字符串，不是整个 Licensee 结构。
		//
		// 界面上这一栏就是"授权主体"，要的是一行能读的名字。
		// 把结构体整个丢过去，React 会渲染出 [object Object]（或直接抛错），
		// 而这类错只在**已激活**的实例上才出现 —— 开发时多半是未激活状态，
		// 那条分支根本不会走到。
		//
		// 另外 tax_id / email 属于客户的工商与联系信息，没有理由出现在
		// 一个所有登录用户都能打开的页面上。
		out["licensee"] = p.Licensee.Org
		out["perpetual"] = p.Perpetual
		if !p.Perpetual && !p.ExpiresAt.IsZero() {
			out["expires_at"] = p.ExpiresAt.Format("2006-01-02")
		}
	} else {
		out["activated"] = false
	}
	return out, nil
}

// licenseActivate 贴激活码。
//
// ⚠️ 这个接口必须在只读降级下**仍然可用**（见 license_guard.go 的
// readOnlySafe）—— 过期之后连激活新授权都做不到的话，客户付了钱也救不回来。
func licenseActivate(c *gin.Context, d Deps) (any, error) {
	var in struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		return nil, apierr.New(http.StatusBadRequest, apierr.CodeInvalidParam, nil)
	}
	// 粘贴时最常见的是首尾多了空白和换行，去掉再验，
	// 否则客户会对着一串看起来一模一样的码反复试
	in.Token = strings.TrimSpace(in.Token)
	if in.Token == "" {
		return nil, apierr.New(http.StatusBadRequest, apierr.CodeInvalidParam, nil)
	}

	id := identity(c)
	ctx := c.Request.Context()

	// 先验签再落库：把一串验不过的东西写进去，会让所有副本一起退回未激活，
	// 而原先那份好的已经被覆盖掉了 —— 一次手滑打掉整套系统的授权。
	p, err := licensekit.VerifyEmbedded(in.Token)
	if err != nil {
		logx.J("license", "activate_verify_failed", map[string]any{
			"actor": id.Username, "err": err.Error(),
			"note": "激活码验签不通过，按未激活处理（不是过期）；原有授权未受影响",
		})
		return nil, apierr.New(http.StatusUnprocessableEntity, apierr.CodeLicenseInvalid, nil)
	}

	// 不含本产品的授权**拒绝激活**。
	//
	// 验签通过不等于这张 license 是给本产品的。放进来的话，原先那张好授权
	// 被顶掉、状态转 not_licensed（只读），而旧 token 已被覆盖，救不回来。
	//
	// ⚠️ 与指纹不匹配的处理刻意相反：那个必须放行（客户刚迁完库，
	// 贴新激活码是唯一的自救路径），而"买的不是这个产品"贴进来
	// 无论如何都不会有正确结果。
	if _, ok := p.Grant(license.ProductID); !ok {
		got := make([]string, 0, len(p.Products))
		for name := range p.Products {
			got = append(got, name)
		}
		// 排序：map 遍历顺序随机，不排的话同一张激活码每次报出的产品顺序都不同
		sort.Strings(got)
		logx.J("license", "activate_wrong_product", map[string]any{
			"actor": id.Username, "license_id": p.LicenseID,
			"want": license.ProductID, "got": got,
			"note": "授权不含本产品，已拒绝激活；未落库，原有授权不受影响",
		})
		return nil, apierr.New(http.StatusUnprocessableEntity, apierr.CodeLicenseWrongProduct,
			map[string]any{"want": license.ProductID, "got": strings.Join(got, ", ")})
	}

	// 指纹对不上仍然允许激活：这正是"客户刚迁完库"的场景。
	// 装载后状态会显示 finger_mismat，界面据此提示他去换一份按新指纹签的授权。
	if fp, err := d.LicenseStore.Fingerprint(ctx); err == nil && p.InstallID != "" && p.InstallID != fp {
		logx.J("license", "activate_fingerprint_mismatch", map[string]any{
			"expect": licensekit.ShortFingerprint(p.InstallID),
			"actual": licensekit.ShortFingerprint(fp),
			"note":   "已接受激活，状态将显示指纹不匹配并进入宽限期",
		})
	}

	if err := d.LicenseStore.Activate(ctx, in.Token, p.LicenseID, id.UserID); err != nil {
		return nil, err
	}
	// 本副本立即生效；其余副本由 Watch 在 20s 内收敛。
	if err := d.LicenseStore.Reload(ctx, d.License); err != nil {
		return nil, err
	}

	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: id.UserID, ActorName: id.Username,
		Action: "license.activate", ObjectType: "license",
		ClientIP: clientIP(c),
		Detail: map[string]any{
			"license_id": p.LicenseID, "org": p.Licensee.Org,
			"status": string(d.License.Status()),
		},
	})
	logx.J("license", "activated", map[string]any{
		"actor": id.Username, "license_id": p.LicenseID,
		"org": p.Licensee.Org, "status": string(d.License.Status()),
	})
	return licenseStatus(c, d)
}

// capacityReport 各容量项的上限与当前用量。
//
// 上限来自 licensekit：有授权按授权给的，没有则回落社区版上限。
// ⚠️ 上限 0 是「不限」（LICENSING §0.1），不是「一个都不给」——
// 前端必须按这个语义渲染，写成 "0" 会让人以为产品被锁死了。
func capacityReport(c *gin.Context, d Deps) []gin.H {
	items := []struct {
		key   string
		count string // 数当前用量的 SQL；空表示这一项没法数
	}{
		{license.CapSeats, `SELECT COUNT(*) FROM users u
			JOIN user_tenants ut ON ut.user_id = u.id AND ut.tenant_id = ?
			WHERE u.deleted_at IS NULL`},
		{license.CapApplications, `SELECT COUNT(*) FROM apps WHERE tenant_id = ? AND status = 'active'`},
		// 租户是全局表，不在租户上下文里数。这是「不适用」，**不是「数失败」**——
		// 两者显示成同一个样子的话，一个正常状态会被当成故障去排查。
		{license.CapTenants, ""},
	}

	q, qerr := d.Store.Tenant(c.Request.Context())
	out := make([]gin.H, 0, len(items))
	for _, it := range items {
		h := gin.H{"item": it.key, "limit": d.License.Limit(it.key), "countable": it.count != ""}
		if it.count != "" && qerr == nil {
			var n int64
			if err := q.QueryRow(it.count).Scan(&n); err == nil {
				h["used"] = n
			} else {
				// 数不出来就明确说数不出来，不要落成 0
				logx.Line("license", "容量用量查询失败 item="+it.key+": "+err.Error())
			}
		}
		out = append(out, h)
	}
	return out
}
