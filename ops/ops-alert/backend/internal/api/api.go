// Package api 是 HTTP 接口层。
//
// 一期只开必要的读写接口：登录、数据源、规则、事件、判定链。
// 每个接口都走 store.Scoped（租户由中间件注入），没有例外。
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ops-alert-backend/config"
	"ops-alert-backend/crypto"
	"ops-alert-backend/datasource"
	eemcp "ops-alert-backend/ee/mcp"
	"ops-alert-backend/engine"
	"ops-alert-backend/internal/api/middleware"
	"ops-alert-backend/internal/dataview"
	"ops-alert-backend/internal/httpx"
	"ops-alert-backend/internal/license"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
)

type Server struct {
	st     *store.Store
	cfg    *config.Config
	cipher *crypto.Cipher
	// eng 供试运行、路由试算、回放复用检测引擎的实现。
	// 接口层不重写一遍判定逻辑——两份判定迟早会分叉，
	// 而分叉的表现是「试运行说会触发，实际不触发」，没人能解释。
	eng *engine.Engine
	// 授权管理器。nil = 未接入授权（开发期），此时不做任何门控 ——
	// 门控只在真正装了 licensekit 的部署里生效
	lic *license.Manager
	// licStore 授权的数据库那一面（激活落库、指纹、Watch）。
	// nil = 未接入授权模块，此时激活接口返回 503 而不是假装成功
	licStore *license.Store
	// version / metricsToken 供 /metrics 使用。
	// ⚠️ metricsToken 为空 = 端点匿名可读。指标里含规则名和用户填的标签，
	// 公网暴露前必须配上，启动自检会为此报一条。
	version      string
	metricsToken string
}

// WithLicense 注入授权的两半：内存状态 Manager 与持久化 Store。
//
// ⚠️ Manager **必须由 main 传进来**，不能让 api 自己 New 一个。
// 各持一个的后果是：main 那个被 Watch 更新、api 这个永远停在未激活，
// 于是"激活成功了但功能还是锁着"，而 /license 接口读的是 api 那个，
// 显示也一直是未激活 —— 两处都"正常"，问题却真实存在。
func (s *Server) WithLicense(mgr *license.Manager, st *license.Store) *Server {
	if mgr != nil {
		s.lic = mgr
	}
	s.licStore = st
	return s
}

// WithMetrics 注入 /metrics 需要的两项。分开设而不是塞进 New 的参数表：
// New 已经有四个参数，再加两个 string 就会出现"两个字符串传反了也能编译过"。
func (s *Server) WithMetrics(version, token string) *Server {
	s.version = version
	s.metricsToken = token
	return s
}

func New(st *store.Store, cfg *config.Config, cipher *crypto.Cipher, eng *engine.Engine) *Server {
	return &Server{
		lic: license.NewManager(), st: st, cfg: cfg, cipher: cipher, eng: eng}
}

// openDatasource 复用引擎的数据源装配（含凭据解密）。
func (s *Server) openDatasource(sc *store.Scoped, id int64) (datasource.Adapter, string, error) {
	return s.eng.OpenDatasource(sc, id)
}

// abortLog 记录不影响主流程的次要错误（审计写失败、状态清理失败等）。
// 不改变响应：主操作已经成功，这里失败不该让用户以为整件事没做成。
func abortLog(c *gin.Context, err error) {
	logx.J("api", "secondary_error", map[string]any{"path": c.FullPath(), "error": err.Error()})
}

// abortTenant / abortQuery 转调 httpx。
//
// 保留这两个包内小写别名而不是把上百处调用点全改成 httpx.AbortX：
// 这次抽取的目的是让 ee/ 能复用，不是重命名。改调用点会把一次
// 结构调整变成一次全文件 diff，评审时真正的改动会被淹掉。
func abortTenant(c *gin.Context, err error) { httpx.AbortTenant(c, err) }
func abortQuery(c *gin.Context, err error)  { httpx.AbortQuery(c, err) }

func (s *Server) Register(r *gin.Engine) {
	public := r.Group("/api/v1")
	public.POST("/auth/login", s.login)
	// SSO 的三个端点都在 public 上：用户还没有会话，正是来拿会话的。
	// ⚠️ oidc/config 只回"要不要显示按钮"和按钮文案，
	// 不回 issuer / client_id —— 那等于向公网泄露身份源拓扑。
	public.GET("/auth/oidc/config", s.oidcPublicConfig)
	public.GET("/auth/oidc/login", s.oidcLogin)
	public.GET("/auth/oidc/callback", s.oidcCallback)

	// PermGuard 必须挂在 Auth 之后：它要拿到当前用户才能判权限。
	// 顺序反了的话每个请求都会以"无身份"被拒，表现为登录后全站 403。
	auth := r.Group("/api/v1", middleware.Auth(s.cfg.JWTSecret), s.PermGuard(), s.FeatureGuard())
	// MCP 的 RPC 端点用自己的令牌鉴权，所以挂在 public 上；
	// 令牌管理接口走登录态，挂 auth。
	// AI 接入（MCP）是**企业版组件**，实现在 ee/mcp。
	// 这里只做接线：把它需要的存储、审计、试运行和授权判据注入进去。
	//
	// ⚠️ 未授权时 ee/mcp 会让路由整体 404 —— 不是 402。
	// 402 等于告诉外面"这里有个功能只是你没买"，而需求是**看不到**。
	eemcp.Register(public, auth, &eemcp.Server{
		St:       s.st,
		Audit:    s.audit,
		Licensed: func() bool { return s.lic == nil || s.lic.Has(license.FeatureMCPFull) },
		DryRun: func(ctx context.Context, sc *store.Scoped, dsID int64, query string,
			lookbackSec, threshold, limit int, groupBy []string) (any, error) {
			return s.eng.DryRun(ctx, sc, dsID, query, lookbackSec, threshold, limit, groupBy)
		},
	})
	s.registerCRUD(auth)
	s.registerRules(auth)
	s.registerBacktest(auth)
	{
		auth.GET("/datasources", s.listDatasources)
		auth.GET("/rules", s.listRules)
		auth.GET("/incidents", s.listIncidents)
		auth.GET("/incidents/series", s.incidentSeries)
		auth.GET("/incidents/:id", s.getIncident)
		auth.GET("/incidents/:id/trace", s.getTrace)
		auth.POST("/incidents/:id/ack", s.ackIncident)
		auth.GET("/selfcheck", s.selfCheck)
		auth.GET("/report", s.getReportConfig)
		auth.PUT("/report", s.saveReportConfig)
		auth.POST("/report/preview", s.previewReport)
		// 试发是**写操作**（真的往外发消息），所以不进 permExactRules
		auth.POST("/report/send", s.sendReportNow)
		auth.POST("/explore", s.exploreLogs)
		auth.GET("/msg-templates", s.listMsgTemplates)
		auth.POST("/msg-templates", s.createMsgTemplate)
		auth.PUT("/msg-templates/:id", s.updateMsgTemplate)
		auth.DELETE("/msg-templates/:id", s.deleteMsgTemplate)
		auth.POST("/msg-templates/preview", s.previewMsgTemplate)
		auth.GET("/sso", s.getOIDCConfig)
		auth.PUT("/sso", s.saveOIDCConfig)
		auth.POST("/sso/test", s.testOIDC)
		auth.GET("/me", s.me)
		auth.GET("/license", s.licenseStatus)
		auth.GET("/license/fingerprint", s.licenseFingerprint)
		auth.POST("/license", s.activateLicense)
		s.registerUsers(auth)
	}
}

func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	p := s.st.Platform("login")
	var (
		id     int64
		hash   string
		role   string
		status string
	)
	err := p.QueryRow(c.Request.Context(),
		`SELECT id, password_hash, role_code, status FROM users
		 WHERE username = ? AND deleted_at IS NULL`, req.Username).
		Scan(&id, &hash, &role, &status)
	if err != nil || status != "active" ||
		bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		// 用户不存在与密码错误返回同一个响应：区分开等于告诉攻击者哪些账号存在。
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}

	var tenantID int64
	if err := p.QueryRow(c.Request.Context(),
		`SELECT tenant_id FROM user_tenants WHERE user_id = ? ORDER BY tenant_id LIMIT 1`, id).
		Scan(&tenantID); err != nil {
		// 登录时的租户解析与请求时的租户校验必须用同一份判据。
		// 这里回落到"任意租户"会造成登得进去、每个接口 403 的经典症状。
		c.JSON(http.StatusForbidden, gin.H{"error": "no_tenant"})
		return
	}

	token, err := middleware.Issue(s.cfg.JWTSecret, middleware.Claims{
		UserID: id, TenantID: tenantID, Username: req.Username, Role: role,
	}, time.Duration(s.cfg.SessionHours)*time.Hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "username": req.Username, "role": role})
}

func (s *Server) listDatasources(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	// ⚠️ 绝不 SELECT auth_enc：凭据不回显，哪怕是加密串。
	// 上一代的两个 P0 都是接口把凭据发给了不该看的人。
	rows, err := sc.Query(`SELECT id, name, type, endpoint, status, probe_at, probe_ms, probe_error, last_data_at
		FROM datasources WHERE tenant_id = ? AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID         int64      `json:"id"`
		Name       string     `json:"name"`
		Type       string     `json:"type"`
		Endpoint   string     `json:"endpoint"`
		Status     string     `json:"status"`
		ProbeAt    *time.Time `json:"probe_at"`
		ProbeMS    int        `json:"probe_ms"`
		ProbeError string     `json:"probe_error"`
		LastDataAt *time.Time `json:"last_data_at"`
	}
	// 先把规则读完再查执行历史：在 rows 未关闭时发第二个查询，
	// database/sql 会用另一条连接，连接池小的时候直接卡死
	out := []item{}
	for rows.Next() {
		var it item
		var probeAt, lastData sql.NullTime
		if err := rows.Scan(&it.ID, &it.Name, &it.Type, &it.Endpoint, &it.Status,
			&probeAt, &it.ProbeMS, &it.ProbeError, &lastData); err != nil {
			abortQuery(c, err)
			return
		}
		if probeAt.Valid {
			it.ProbeAt = &probeAt.Time
		}
		if lastData.Valid {
			it.LastDataAt = &lastData.Time
		}
		out = append(out, it)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) listRules(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT r.id, r.name, r.kind, r.severity, r.enabled, r.interval_sec,
			r.last_run_at, r.last_error, r.consecutive_failures, d.name, r.template
		FROM rules r LEFT JOIN datasources d ON d.id = r.datasource_id
		WHERE r.tenant_id = ? AND r.deleted_at IS NULL ORDER BY r.id`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID       int64      `json:"id"`
		Name     string     `json:"name"`
		Kind     string     `json:"kind"`
		Severity string     `json:"severity"`
		Enabled  bool       `json:"enabled"`
		Interval int        `json:"interval_sec"`
		LastRun  *time.Time `json:"last_run_at"`
		// LastError 与 Failures 是「规则健康」的判据：
		// 7 天触发 0 次可能健康，也可能是查询一直在失败，界面必须能区分。
		LastError  string `json:"last_error"`
		Failures   int    `json:"consecutive_failures"`
		Datasource string `json:"datasource"`
		// 最近 N 次执行的结果，新→旧。给列表里的「执行状况」条用。
		// ⚠️ 这是「7 天触发 0 次是健康还是坏了」的唯一答案：
		// 全 ok = 一直在跑确实没命中；后段 error = 已经不告警了；空 = 压根没跑。
		Runs []string `json:"runs"`
		// 来源模板。模板页按它统计"这个模板建了几条"，
		// 列表页也能一眼看出哪些是手写的（空串）
		Template string `json:"template"`
	}
	// 先把规则读完再查执行历史：在 rows 未关闭时发第二个查询，
	// database/sql 会用另一条连接，连接池小的时候直接卡死
	out := []item{}
	for rows.Next() {
		var it item
		var enabled int
		var lastRun sql.NullTime
		var ds sql.NullString
		if err := rows.Scan(&it.ID, &it.Name, &it.Kind, &it.Severity, &enabled, &it.Interval,
			&lastRun, &it.LastError, &it.Failures, &ds, &it.Template); err != nil {
			abortQuery(c, err)
			return
		}
		it.Enabled = enabled == 1
		if lastRun.Valid {
			it.LastRun = &lastRun.Time
		}
		it.Datasource = ds.String
		out = append(out, it)
	}
	// 最近 24 次执行，按规则分组。用窗口函数一次取回，
	// 逐条规则发查询的话 63 条规则就是 63 次往返
	runs := map[int64][]string{}
	rrows, err := sc.Query(`SELECT rule_id, outcome FROM (
			SELECT rule_id, outcome,
			       ROW_NUMBER() OVER (PARTITION BY rule_id ORDER BY id DESC) AS rn
			  FROM rule_runs WHERE tenant_id = ?
		) t WHERE rn <= 24 ORDER BY rule_id, rn DESC`)
	if err != nil {
		// 执行历史取不到不该让整个列表挂掉：规则本身的信息更重要。
		// 但要留日志——「条子全是空的」必须能查到原因
		abortLog(c, err)
	} else {
		defer rrows.Close()
		for rrows.Next() {
			var rid int64
			var outcome string
			if err := rrows.Scan(&rid, &outcome); err != nil {
				abortLog(c, err)
				break
			}
			runs[rid] = append(runs[rid], outcome)
		}
		if err := rrows.Err(); err != nil {
			abortLog(c, err)
		}
	}
	for i := range out {
		out[i].Runs = runs[out[i].ID]
		if out[i].Runs == nil {
			out[i].Runs = []string{} // 空数组而不是 null：前端要能区分"没跑过"和"字段缺失"
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) listIncidents(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	status := c.DefaultQuery("status", "active")
	// active 是「未恢复」的口径：firing + acked + suppressed。
	// 把 suppressed 排除掉会让维护窗口期间的问题从界面上消失，
	// 而它们其实还在发生——只是没吵人。
	cond := `status IN ('firing','acked','suppressed')`
	switch status {
	case "resolved":
		cond = `status = 'resolved'`
	case "all":
		cond = `1 = 1`
	case "firing", "acked", "suppressed":
		// 单状态筛选：界面上的筛选条直接传状态名
		cond = `status = ` + quoteStatus(status)
	}
	// 时间范围。⚠️ 只过滤 last_at，不过滤 first_at ——
	// 一条 3 天前触发、刚刚还在响的告警，属于"最近 1 小时"里该看见的东西；
	// 按 first_at 过滤会把它藏起来，而它恰恰是最该处理的那种。
	if sec := rangeSeconds(c.Query("range")); sec > 0 {
		cond += fmt.Sprintf(" AND last_at >= DATE_SUB(NOW(3), INTERVAL %d SECOND)", sec)
	}
	rows, err := sc.Query(`SELECT id, title, severity, status, count, first_at, last_at,
			acked_by, labels, COALESCE(rule_id, 0) FROM incidents
		WHERE tenant_id = ? AND ` + cond + ` ORDER BY last_at DESC LIMIT 200`)
	if err != nil {
		abortQuery(c, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID       int64             `json:"id"`
		Title    string            `json:"title"`
		Severity string            `json:"severity"`
		Status   string            `json:"status"`
		Count    int               `json:"count"`
		FirstAt  time.Time         `json:"first_at"`
		LastAt   time.Time         `json:"last_at"`
		AckedBy  string            `json:"acked_by"`
		Labels   map[string]string `json:"labels"`
		RuleID   int64             `json:"rule_id"`
		// 该规则最近 24 个周期的命中数，新→旧。列表里的趋势线用它。
		// ⚠️ 这是**规则级**的趋势，不是这一个分组的：rule_runs 只记录总命中数。
		// 同一规则的多个分组会共用一条曲线 —— 它回答的是"这条规则在恶化还是平息"，
		// 而不是"这个容器在恶化"。前端的 aria-label 里写清楚了这点。
		Trend []int `json:"trend"`
	}
	// 先把规则读完再查执行历史：在 rows 未关闭时发第二个查询，
	// database/sql 会用另一条连接，连接池小的时候直接卡死
	out := []item{}
	for rows.Next() {
		var it item
		var labels []byte
		if err := rows.Scan(&it.ID, &it.Title, &it.Severity, &it.Status, &it.Count,
			&it.FirstAt, &it.LastAt, &it.AckedBy, &labels, &it.RuleID); err != nil {
			abortQuery(c, err)
			return
		}
		_ = json.Unmarshal(labels, &it.Labels)
		it.Trend = []int{} // 先给空数组：漏填时前端画的是"没数据"而不是一条假的平线
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		abortQuery(c, err)
		return
	}

	trend := map[int64][]int{}
	trows, err := sc.Query(`SELECT rule_id, hits FROM (
			SELECT rule_id, hits,
			       ROW_NUMBER() OVER (PARTITION BY rule_id ORDER BY id DESC) AS rn
			  FROM rule_runs WHERE tenant_id = ?
		) t WHERE rn <= 24 ORDER BY rule_id, rn DESC`)
	if err != nil {
		// 趋势取不到不该让整个列表挂掉，但要留日志：
		// "所有趋势线都是空的"必须能查出原因
		abortLog(c, err)
	} else {
		defer trows.Close()
		for trows.Next() {
			var rid int64
			var hits int
			if err := trows.Scan(&rid, &hits); err != nil {
				abortLog(c, err)
				break
			}
			trend[rid] = append(trend[rid], hits)
		}
		if err := trows.Err(); err != nil {
			abortLog(c, err)
		}
	}
	for i := range out {
		if t := trend[out[i].RuleID]; t != nil {
			out[i].Trend = t
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

// rangeSeconds 把界面上的时间范围翻成秒。空/未知一律返回 0（不限制）——
// 猜一个默认范围会让人以为"就这么多告警"，而实际是被时间窗口截掉了。
func rangeSeconds(v string) int {
	switch v {
	case "1h":
		return 3600
	case "6h":
		return 6 * 3600
	case "24h":
		return 24 * 3600
	case "7d":
		return 7 * 24 * 3600
	}
	return 0
}

// quoteStatus 只允许白名单里的状态值进 SQL。
// ⚠️ 这里是拼进 WHERE 的，不能用参数占位符（条件是动态拼的），
// 所以必须自己保证不可注入 —— 白名单之外一律回落到最保守的条件。
func quoteStatus(v string) string {
	switch v {
	case "firing", "acked", "suppressed", "resolved":
		return "'" + v + "'"
	}
	return "''"
}

// getIncident 返回事件详情。
//
// 🔴 **委托给 incidentDetail，不要在这里再写一份查询。**
// 这里原来是一份独立实现，和 MCP 用的 incidentDetail 查的是同一张表、
// 拼的是同一个结构 —— 于是给详情加"相似历史"时只改了其中一份，
// 界面上字段凭空消失，而两边的代码都"看起来是对的"。
// 同一个资源两条读路径，迟早分叉；分叉的表现还都是静默的。
func (s *Server) getIncident(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	out, err := dataview.IncidentDetail(sc, id)
	if err != nil {
		// incidentDetail 对不存在的 id 返回自造的错误而不是 sql.ErrNoRows，
		// 所以这里按内容判断。给 404 而不是 500：让调用方能区分
		// "这条没了"和"库挂了"。
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// getTrace 返回判定链。这是「判定可解释」的接口。
func (s *Server) getTrace(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var verdict string
	var steps []byte
	var at time.Time
	err = sc.QueryRow(`SELECT verdict, steps, created_at FROM decision_traces
		WHERE tenant_id = ? AND incident_id = ? ORDER BY id DESC LIMIT 1`, id).
		Scan(&verdict, &steps, &at)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "no_trace"})
		return
	}
	if err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"verdict": verdict, "steps": json.RawMessage(steps), "at": at})
}

func (s *Server) ackIncident(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	user := middleware.CurrentUser(c)
	// acked_at 只在第一次认领时写：重复认领不该刷新 MTTA。
	res, err := sc.Exec(`UPDATE incidents SET status = 'acked',
		acked_at = COALESCE(acked_at, NOW(3)), acked_by = ?
		WHERE tenant_id = ? AND id = ? AND status IN ('firing','suppressed')`, user.Username, id)
	if err != nil {
		abortQuery(c, err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "not_ackable"})
		return
	}
	if _, err := sc.Insert(`INSERT INTO incident_events (tenant_id, incident_id, kind, actor, message)
		VALUES (?, ?, 'acked', ?, '已认领')`, id, user.Username); err != nil {
		logx.J("api", "timeline_error", map[string]any{"error": err.Error()})
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// selfCheck 是告警系统自检：数据源可达性、规则健康、投递成功率。
//
// 这一块回答的是「告警系统本身还活着吗」。开源方案普遍要靠外部
// Dead man's switch 手工搭，而「没有告警」和「告警系统坏了」
// 在界面上长得一模一样。
func (s *Server) selfCheck(c *gin.Context) {
	sc, err := s.st.Tenant(c.Request.Context())
	if err != nil {
		abortTenant(c, err)
		return
	}
	// 与 MCP 工具共用同一份实现：两边各查一遍迟早漂移，
	// 而漂移的表现是"界面说正常、AI 说有问题"，没人知道该信哪个。
	data, err := dataview.SelfCheck(sc)
	if err != nil {
		abortQuery(c, err)
		return
	}
	c.JSON(http.StatusOK, data)
}
