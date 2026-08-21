package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/internal/license"
	"ops-cmdb-backend/logx"
	"ops-kit/licensekit"
)

// 授权状态与激活。规范见 ops/LICENSING.md §13.6。

type LicenseHandler struct {
	DB    *sql.DB
	Mgr   *license.Manager
	Store *license.Store
}

func NewLicenseHandler(db *sql.DB, mgr *license.Manager, st *license.Store) *LicenseHandler {
	return &LicenseHandler{DB: db, Mgr: mgr, Store: st}
}

func (h *LicenseHandler) Register(r *gin.RouterGroup) {
	// 读状态**不挂功能门控也不挂只读拦截**：授权过期时，
	// 「查看授权状态」恰恰是客户最需要打开的那一页。
	r.GET("/license", h.Status)
	r.POST("/license", h.Activate)
}

// licenseOut 授权状态。
//
// ⚠️ 七种状态各自的文案必须不同（LICENSING §5）。这里只返回状态码，
// 文案在前端语言包里 —— 后端拼好中文发过来的话，英文界面就永远漏中文，
// 而授权状态恰恰是最晚才出现在客户屏幕上的一类界面。
type licenseOut struct {
	// Status 七态之一：active / grace / expired / lapsed /
	// finger_mismat / not_activated / not_licensed
	Status   string `json:"status"`
	ReadOnly bool   `json:"read_only"`

	// DaysUntilExpiry 距到期还有几天。
	// ⚠️ 用指针：null = 不适用（永久授权 / 未激活），0 = 今天到期。
	// 压成 0 的话，永久授权会显示成"今天到期"。
	DaysUntilExpiry *int `json:"days_until_expiry"`
	ShouldRemind    bool `json:"should_remind"`
	Perpetual       bool `json:"perpetual"`

	Licensee  *licenseeOut `json:"licensee,omitempty"`
	ExpiresAt string       `json:"expires_at,omitempty"`
	LicenseID string       `json:"license_id,omitempty"`

	// Fingerprint 安装指纹，**完整值**。
	//
	//	界面上本来就要完整显示（客户申请授权时要报给我们），所以只有这一个字段。
	//
	//	🔴 这里原来同时返回截断版 `fingerprint` 和完整版 `fingerprint_full`。
	//	截断版一个消费方都没有，而"掩码 + 全量并存"这种写法会被后来的人
	//	照抄到真正的敏感字段上 —— 那时候它就是个**看起来有保护、实际没有**的
	//	假防线（OPSCMDB-031 P2-67）。
	//
	//	⚠️ 指纹不是密钥，掩码它没有安全意义；截断只在**日志**里有意义
	//	（见下方指纹不匹配那段，那里仍然用 ShortFingerprint）。
	Fingerprint string `json:"fingerprint"`

	Capacity license.Capacity `json:"capacity"`
	Usage    usageOut         `json:"usage"`
	// Exceeded 超限项。**返回非空不代表要拒绝操作** ——
	// 它只该被渲染成提示并计入续购报价（LICENSING §6）。
	// 客户临时扩容 20 台节点结果系统罢工，是会丢客户的设计。
	Exceeded []exceededOut `json:"exceeded"`

	// Features 已授权的功能码，前端据此做 EE 标记与入口显隐。
	Features []string `json:"features"`
	// FeaturesMissing 这一档**没有**的功能。只给"有什么"的话，
	// 客户看不出缺什么，分档就不可见了（P1-74）
	FeaturesMissing []string `json:"features_missing"`
	// FeaturesTotal 产品一共有多少个功能项 —— 让「5 / 12」这种双计数成为可能
	FeaturesTotal int `json:"features_total"`
}

// exceededOut 超限项。
//
// 用本地类型而不是直接暴露 licensekit.Exceeded：swag 只解析本模块，
// 跨模块的类型它找不到定义，会在生成时直接失败 ——
// 而失败信息（cannot find type definition）指向的是注解，
// 完全看不出真因是"这个类型不在本模块里"。
type exceededOut struct {
	Item    string `json:"item"`
	Current int64  `json:"current"`
	Limit   int64  `json:"limit"`
}

type licenseeOut struct {
	Org       string `json:"org"`
	Contact   string `json:"contact"`
	ScopeName string `json:"scope_name"`
}

type usageOut struct {
	Nodes         int64 `json:"nodes"`
	Clusters      int64 `json:"clusters"`
	CloudAccounts int64 `json:"cloud_accounts"`
	Seats         int64 `json:"seats"`
	Tenants       int64 `json:"tenants"`
	// RetentionDays 当前配置的保留天数。⚠️ 0 = 永不清理（无限），不是"零天"
	RetentionDays int64 `json:"retention_days"`
	// RetentionUnlimited 把上面那个 0 的语义**显式**说出来。
	//
	//	原来 usage 里压根没有保留天数这一项，界面因此渲染成「—」——
	//	看起来像"没数据"，而实际是配了 0（永不清理）。
	//	光给一个 0 也不够：前端还得自己知道"0 在这里表示无限"，
	//	而那种约定迟早会在某一处被忘掉（本产品已经在授权页忘过一次）。
	RetentionUnlimited bool `json:"retention_unlimited"`
}

// Status GET /api/license
//
//	@Summary		授权状态
//	@Description	七态、容量与用量、安装指纹。未激活时同样返回 200，状态为 not_activated。
//	@Tags			license
//	@Produce		json
//	@Success		200	{object}	handlers.licenseOut
//	@Router			/license [get]
func (h *LicenseHandler) Status(c *gin.Context) {
	fpFull, err := h.Store.Fingerprint(c.Request.Context())
	if err != nil {
		// 指纹算不出来是真故障（库连不上），不能兜底成空串 ——
		// 空指纹会让客户拿着它去申请授权，签出来的那份永远对不上
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}

	out := licenseOut{
		Status:          string(h.Mgr.Status()),
		ReadOnly:        h.Mgr.ReadOnly(),
		ShouldRemind:    h.Mgr.ShouldRemind(),
		Fingerprint:     fpFull,
		Capacity:        h.Mgr.Capacity(),
		Exceeded:        []exceededOut{},
		Features:        []string{},
		FeaturesMissing: []string{},
	}
	if d, ok := h.Mgr.DaysUntilExpiry(); ok {
		out.DaysUntilExpiry = &d
	}
	if p := h.Mgr.Payload(); p != nil {
		out.Perpetual = p.Perpetual
		out.LicenseID = p.LicenseID
		out.Licensee = &licenseeOut{
			Org:       p.Licensee.Org,
			Contact:   p.Licensee.Contact,
			ScopeName: p.Licensee.ScopeName,
		}
		if !p.Perpetual && !p.ExpiresAt.IsZero() {
			out.ExpiresAt = p.ExpiresAt.Format("2006-01-02")
		}
	}
	// ⚠️ 同时给出**有什么**和**缺什么**。
	//
	//	原来只列已启用的。于是客户看到 5 个 code，无法判断
	//	"我这一档比更高档少了哪些能力" —— 也就无从产生升级动机。
	//	而这是**分档产品最该说清楚的一件事**（企业版规范里
	//	"分档必须可见、Has() 不能写成全有全无"讲的就是它）。
	//
	//	对照 AI 接入页做对的地方：那里是「当前可用 87 个，产品共 87 个」双计数，
	//	受限时一眼看出差多少。授权页恰恰是最该这么做的地方，反而没做（P1-74）。
	for _, f := range license.AllFeatures() {
		if h.Mgr.Has(f) {
			out.Features = append(out.Features, string(f))
			continue
		}
		out.FeaturesMissing = append(out.FeaturesMissing, string(f))
	}
	out.FeaturesTotal = len(license.AllFeatures())

	u := h.usage()
	out.Usage = usageOut{
		Nodes: u.Nodes, Clusters: u.Clusters, CloudAccounts: u.CloudAccounts,
		Seats: u.Seats, Tenants: u.Tenants,
		RetentionDays:      u.RetentionDays,
		RetentionUnlimited: u.RetentionUnlimited,
	}
	for _, e := range h.Mgr.CheckCapacity(u) {
		out.Exceeded = append(out.Exceeded, exceededOut{Item: e.Item, Current: e.Current, Limit: e.Limit})
	}
	c.JSON(http.StatusOK, out)
}

// usage 当前用量。
//
// 查不出来的项按 0 处理并打日志：容量核对是**提示性**的，
// 不该因为一条统计查询失败就让整个授权页打不开。
func (h *LicenseHandler) usage() license.Usage {
	var u license.Usage
	count := func(q string) int64 {
		var n int64
		if err := h.DB.QueryRow(q).Scan(&n); err != nil {
			logx.J("license", "usage_query_failed", map[string]any{
				"query": q, "err": err.Error(),
				"note": "该项用量按 0 计，容量提示会偏低",
			})
			return 0
		}
		return n
	}
	u.Nodes = count(`SELECT COUNT(*) FROM k8s_nodes`)
	u.Clusters = count(`SELECT COUNT(*) FROM k8s_clusters WHERE enabled=1`)
	u.CloudAccounts = count(`SELECT COUNT(*) FROM cloud_accounts`)
	u.Seats = count(`SELECT COUNT(*) FROM users`)
	u.Tenants = count(`SELECT COUNT(*) FROM tenants WHERE status='active' AND deleted_at IS NULL`)
	// 保留天数取**两个保留设置里更宽的那个**：授权卖的是"数据留多久"，
	// 只要有一类数据留得更久，实际占用就按那个算。
	//
	// ⚠️ 0 = 永不清理（无限），所以任何一项是 0，整体就是无限。
	// 拿 MAX() 去比会把 0 当成最小值，正好判反 —— 这里必须显式处理。
	u.RetentionDays, u.RetentionUnlimited, u.RetentionKnown = h.retention()
	return u
}

// retentionDays 当前生效的保留天数。0 = 永不清理（无限）。
//
//	读的是 settings 里那两个保留设置，取**更宽**的那个：
//	  audit_retention_days          审计日志
//	  audit_changes_retention_days  字段级变更
//
//	⚠️ 「更宽」不是 MAX()：0 表示无限，而 MAX 会把它当成最小值。
//	任何一项是 0 → 整体就是无限（返回 0）。
//
//	读不到设置时返回 0（无限）而不是某个默认天数：
//	那两个 key 缺失时后端的清理任务也不会跑，事实上就是不清理 ——
//	返回一个"看起来合规"的天数会把这个状态藏起来。
func (h *LicenseHandler) retention() (days int64, unlimited bool, known bool) {
	widest := int64(0)
	for _, k := range []string{"audit_retention_days", "audit_changes_retention_days"} {
		var v string
		if h.DB.QueryRow(`SELECT v FROM settings WHERE k=?`, k).Scan(&v) != nil {
			// 这一项压根没配 —— 后端的清理任务也就不会按它跑，事实上不清理。
			// 这是**已知的**"永不清理"，不是"不知道"
			return 0, true, true
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			logx.J("license", "retention_unparsable", map[string]any{
				"key": k, "value": v,
				"note": "保留天数解析不出来，按「永不清理」处理并计入超限 —— 不能当成合规",
			})
			return 0, true, true
		}
		if n <= 0 {
			return 0, true, true // 0 或负数都是永不清理
		}
		if n > widest {
			widest = n
		}
	}
	return widest, false, true
}

// Activate POST /api/license  {"token": "..."}
//
//	@Summary		激活授权
//	@Description	验签通过才落库。库里换了之后，其余副本在 20s 内自行收敛。
//	@Tags			license
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	handlers.licenseOut
//	@Failure		400	{object}	httpx.APIError
//	@Router			/license [post]
func (h *LicenseHandler) Activate(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	// 粘贴时最常见的是首尾多了空白和换行，去掉再验，
	// 否则客户会对着一串看起来一模一样的码反复试
	in.Token = strings.TrimSpace(in.Token)
	if in.Token == "" {
		httpx.Fail(c, httpx.CodeBadRequest, nil, nil)
		return
	}

	// 先验签再落库：把一串验不过的东西写进去，会让所有副本一起退回未激活，
	// 而原先那份好的已经被覆盖掉了 —— 一次手滑打掉整套系统的授权
	p, err := licensekit.VerifyEmbedded(in.Token)
	if err != nil {
		logx.J("license", "activate_verify_failed", map[string]any{
			"user": UsernameFromCtx(c), "err": err.Error(),
		})
		httpx.FailKey(c, httpx.CodeBadRequest, "error.licenseInvalid", err, nil)
		return
	}

	// 不含本产品的授权**拒绝激活**。
	//
	// 库里只有一行授权（id=1），激活即覆盖。一张只含别的产品的 license 贴进来，
	// 验签是通得过的，于是原先那张好授权被顶掉、状态转 not_licensed ——
	// 而 not_licensed 是只读（CEWritable 只放行 not_activated），
	// 且旧 token 已经被 UPDATE 覆盖，库里没有留底，救不回来。
	// 这就是上面那句"一次手滑打掉整套系统的授权"，验签只挡住了一半。
	//
	// ⚠️ 与指纹不匹配的处理刻意相反：那个必须放行（客户刚迁完库，
	// 贴新激活码是唯一的自救路径），而"买的不是这个产品"贴进来
	// 无论如何都不会有正确结果，越早拒绝越好。
	if _, ok := p.Grant(license.ProductID); !ok {
		got := make([]string, 0, len(p.Products))
		for name := range p.Products {
			got = append(got, name)
		}
		// 排序：map 遍历顺序随机，不排的话同一张激活码每次报出的产品顺序都不同，
		// 客户截图来问的时候两次内容对不上
		sort.Strings(got)
		logx.J("license", "activate_wrong_product", map[string]any{
			"user": UsernameFromCtx(c), "license_id": p.LicenseID,
			"want": license.ProductID, "got": got,
			"note": "授权不含本产品，已拒绝激活；未落库，原有授权不受影响",
		})
		httpx.FailKey(c, httpx.CodeBadRequest, "error.licenseWrongProduct", nil,
			map[string]any{"want": license.ProductID, "got": strings.Join(got, ", ")})
		return
	}

	// 指纹对不上也允许激活：这正是"客户刚迁完库"的场景，
	// 拒绝激活等于把唯一的自救路径也堵死。装载后状态会显示 finger_mismat，
	// 界面据此提示他去换一份按新指纹签的授权。
	fp, err := h.Store.Fingerprint(c.Request.Context())
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	if p.InstallID != "" && p.InstallID != fp {
		logx.J("license", "activate_fingerprint_mismatch", map[string]any{
			"expect": licensekit.ShortFingerprint(p.InstallID),
			"actual": licensekit.ShortFingerprint(fp),
			"note":   "已接受激活，状态将显示指纹不匹配并进入宽限期",
		})
	}

	if err := h.Store.Activate(c.Request.Context(), in.Token, UsernameFromCtx(c)); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	// 本副本立即生效；其余副本由 Watch 在 20s 内收敛。
	// 不在这里等其他副本 —— 等也没有可靠的确认手段，
	// 而"激活按钮转了 20 秒"会让人以为卡住了
	if err := h.Store.Reload(c.Request.Context(), h.Mgr); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	SetAuditTarget(c, "license "+p.LicenseID)
	logx.J("license", "activated", map[string]any{
		"user": UsernameFromCtx(c), "license_id": p.LicenseID,
		"org": p.Licensee.Org, "status": string(h.Mgr.Status()),
	})
	h.Status(c)
}
