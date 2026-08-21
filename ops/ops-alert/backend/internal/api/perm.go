package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/api/middleware"
	"ops-alert-backend/logx"
)

// 接口级权限校验。
//
//	43 条路由逐个手挂中间件不现实——漏一个就是一个洞，而且新加路由必然会忘。
//	这里是表驱动：一张 路由→权限码 的映射表 + 一个全局中间件，
//	再加一个启动自检把没覆盖到的路由打出来。
//
//	设计要点（与 ops-cmdb 的 handlers/perm.go 保持同一套语义，
//	两个系统的权限模型不一致会让同一个人在两边得出不同结论）：
//
//	 1. 未映射的路由**拒绝**（fail-closed）。新接口忘配权限不会变成后门，
//	    而是启动日志里立刻 WARN + 该接口 403。
//	 2. 匹配用 gin 的 c.FullPath()，拿到的是注册时的路由模板
//	    （/api/v1/incidents/:id/ack），不是实际 URL，所以匹配精确、
//	    不会被路径里的用户数据干扰。
//	 3. 「读」用 menu: 码，「写」用 alert: 码。菜单权限同时就是该模块的读权限，
//	    拆成两个码会造出"菜单看得见、进去一片 403"。

// permRule 一条前缀规则：该前缀下的读操作要 Read，写操作要 Write。
// Write 留空表示该模块下的写操作也只需 Read（目前只有纯查询模块）。
type permRule struct {
	Prefix string
	Read   string
	Write  string
}

// 前缀规则，按 Prefix 最长匹配。顺序无所谓，查表时会先按长度排序。
var permPrefixRules = []permRule{
	{"/api/v1/incidents", "menu:alert_incidents", "alert:ack_incident"},
	{"/api/v1/rules", "menu:alert_rules", "alert:manage_rules"},
	{"/api/v1/backtests", "menu:alert_backtest", "alert:run_backtest"},
	{"/api/v1/silences", "menu:alert_silences", "alert:manage_silences"},
	{"/api/v1/noise", "menu:alert_noisetop", ""},
	{"/api/v1/datasources", "menu:alert_datasources", "alert:manage_datasources"},
	{"/api/v1/notifiers", "menu:alert_notifiers", "alert:manage_notifiers"},
	{"/api/v1/routes", "menu:alert_routes", "alert:manage_routes"},
	{"/api/v1/selfcheck", "menu:alert_selfcheck", ""},
	{"/api/v1/mcp/tokens", "menu:alert_mcp", "alert:manage_mcp"},
	{"/api/v1/audit", "menu:alert_audit", ""},
	{"/api/v1/report", "menu:alert_report", "alert:manage_report"},
	{"/api/v1/msg-templates", "menu:alert_templates_msg", "alert:manage_msg_templates"},
	// 授权页只给 admin（靠 unrestricted 生效，种子里不发给其他角色）。
	// 状态里含档次、到期日、安装指纹与客户名，属于商务信息 ——
	// 值班员和只读角色不该看到，更不该能激活
	{"/api/v1/license", "menu:alert_license", "alert:manage_license"},
	// SSO 配置只给 admin：这里能配的东西等于"谁能进这套系统"
	{"/api/v1/sso", "menu:alert_sso", "alert:manage_sso"},
	{"/api/v1/import", "menu:alert_rules", "alert:import"},
	// 用户与角色。管理账号是最敏感的一类写操作，单独一个权限码，
	// 不跟其它「平台」功能共用 —— 能看审计不等于能建账号
	{"/api/v1/users", "menu:alert_users", "alert:manage_users"},
	{"/api/v1/roles", "menu:alert_users", ""},
}

// 精确规则。用于**方法与语义不一致**的路由：
// 这几个都是 POST，但并不改变任何状态，只是把当前配置跑一遍看结果。
// 落进前缀规则的话会要求写权限，导致「只读的人无法预演」——
// 而预演恰恰是只读角色最该有的能力（不预演就只能拿生产验证）。
var permExactRules = map[string]string{
	"POST /api/v1/routes/simulate":  "menu:alert_routes", // 路由试算：给一组标签看会命中哪条分支
	"POST /api/v1/rules/dryrun":     "menu:alert_rules",  // 规则试运行：查一次数据源，不落事件
	"POST /api/v1/import/preflight": "menu:alert_rules",  // 导入预检：只报告会发生什么，不写库
	// 模板预览：只把参数翻成查询语句，不写库。只读角色必须能用 ——
	// 不给的话，只读的人连"这个模板会生成什么"都看不到，模板库对他就是三张空卡片
	"POST /api/v1/rules/templates/preview": "menu:alert_rules",
	// 日报预览：只生成内容不发送，只读角色要能用它确认日报里会写什么
	"POST /api/v1/report/preview": "menu:alert_report",
	// 日志检索：POST 但只读（查数据源，不写任何东西）。
	// 挂成写权限的话，只读角色就查不了原始日志 ——
	// 而"为什么没告警"恰恰是只读角色最需要自己回答的问题
	"POST /api/v1/explore": "menu:alert_explore",
	// 模板预览：只渲染不保存、不发送。只读角色要能用它看懂
	// "我收到的这条告警是怎么拼出来的"
	"POST /api/v1/msg-templates/preview": "menu:alert_templates_msg",
	// SSO 连通性测试：只探端点，不改配置
	"POST /api/v1/sso/test": "menu:alert_sso",
}

// 登录即可访问的公共接口。是显式清单，不做前缀通配。
var permPublicRead = map[string]bool{
	"GET /api/v1/rules/kinds": true, // 规则类型字典，几乎每个页面当下拉用；
	// 挂 menu:alert_rules 会造成"有权看事件却拉不出规则类型"的连坐
	"GET /api/v1/me": true, // 自己的身份与权限：不给的话前端连菜单都渲染不出来
	// 模板清单是建规则表单的字典，和 rules/kinds 同性质
	"GET /api/v1/rules/templates": true,
	// 授权状态：前端靠它决定哪些入口标「企业版」，不给的话所有人都看不到入口
	"GET /api/v1/license": true,
}

// 不走用户会话鉴权的路由，跳过本中间件与启动自检。
//
// ⚠️ 这里是**精确路径**，不是前缀。
// 第一版写成前缀 "/api/v1/mcp"，把 /api/v1/mcp/tokens 一起跳过了——
// 只读用户不但能看令牌列表，还能**创建 MCP 令牌**，拿到之后
// 就绕开了自己在界面上的所有限制。前缀跳过的作用域永远比看起来大，
// 凡是"跳过鉴权"的清单都必须精确到路径。
var permSkipExact = map[string]bool{
	"POST /api/v1/auth/login": true,
	// SSO 三个端点：用户还没有会话，正是来拿会话的
	"GET /api/v1/auth/oidc/config":   true,
	"GET /api/v1/auth/oidc/login":    true,
	"GET /api/v1/auth/oidc/callback": true, // 登录本身
	"POST /api/v1/mcp":               true, // MCP RPC 端点用 X-MCP-Token，权限在 mcp.go 里按工具判
}

var permSortedRules []permRule

func init() {
	permSortedRules = append(permSortedRules, permPrefixRules...)
	// 最长前缀优先：/api/v1/mcp/tokens 必须排在 /api/v1/mcp 之前，
	// 否则短的先命中，令牌管理接口会落到错误的权限码上
	sort.Slice(permSortedRules, func(i, j int) bool {
		return len(permSortedRules[i].Prefix) > len(permSortedRules[j].Prefix)
	})
}

func isWriteMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	}
	return false
}

// resolvePerm 解析某个路由需要的权限码。
//
//	code == "" && ok  → 公共接口，登录即可
//	code != "" && ok  → 需要该权限码
//	!ok               → 没有任何规则覆盖，调用方必须拒绝
func resolvePerm(method, fullPath string) (string, bool) {
	key := method + " " + fullPath
	if permPublicRead[key] {
		return "", true
	}
	if code, hit := permExactRules[key]; hit {
		return code, true
	}
	for _, r := range permSortedRules {
		if !strings.HasPrefix(fullPath, r.Prefix) {
			continue
		}
		if isWriteMethod(method) && r.Write != "" {
			return r.Write, true
		}
		return r.Read, true
	}
	return "", false
}

func permSkipped(method, path string) bool {
	return permSkipExact[method+" "+path]
}

// PermGuard 全局接口级校验。必须挂在 middleware.Auth 之后。
func (s *Server) PermGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		full := c.FullPath()
		if full == "" || permSkipped(c.Request.Method, full) { // 404 交给 gin；跳过的前缀有自己的鉴权
			c.Next()
			return
		}
		code, ok := resolvePerm(c.Request.Method, full)
		if !ok {
			// 走到这里说明有路由没进映射表。启动自检本该拦下，
			// 打 WARN 是为了万一（比如运行期动态注册的路由）也能被发现。
			logx.J("perm", "route_unmapped", map[string]any{
				"method": c.Request.Method, "path": full,
			})
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "perm_unmapped"})
			return
		}
		if !s.hasPerm(c, code) {
			s.denyPerm(c, code)
			return
		}
		c.Next()
	}
}

// hasPerm 当前用户是否拥有某权限码。
//
// ⚠️ 判据只有这一处。前端的 can() 只决定显不显示，拦截全在这里。
func (s *Server) hasPerm(c *gin.Context, code string) bool {
	if code == "" {
		return true
	}
	pc := s.permsOf(c)
	if pc.unrestricted {
		return true
	}
	return pc.codes[code]
}

// denyPerm 统一的 403：记审计（谁在试探什么）+ 打日志。
func (s *Server) denyPerm(c *gin.Context, code string) {
	u := middleware.CurrentUser(c)
	logx.J("perm", "denied", map[string]any{
		"user": u.Username, "method": c.Request.Method, "path": c.FullPath(), "need": code,
	})
	// 被拒的请求必须进审计：审计页要能回答"谁在试探什么"。
	// action 用独立的 perm.denied，混进正常操作里等于没记。
	//
	// ⚠️ audit_logs 是**租户表**不是平台表（store 白名单里点名说明过），
	// 所以走 Scoped.Insert —— tenant_id 由 store 层注入，这里不传。
	if sc, err := s.st.Tenant(c.Request.Context()); err == nil {
		s.audit(c, sc, "perm.denied", "route", 0, map[string]any{
			"method": c.Request.Method, "path": c.FullPath(), "need": code,
		})
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden", "need": code})
}

// permCtx 是一次请求内解析出的权限集合。
type permCtx struct {
	unrestricted bool
	codes        map[string]bool
}

const ctxPerms = "perms"

// permsOf 取当前请求的权限集合，每请求解析一次并缓存在 gin 上下文里。
//
// ⚠️ 从**库里现取**而不是从 JWT 里读。角色是会在服务端被改的：
// 管理员刚把某人降权，而他手里的 token 还带着旧角色——
// 从 token 读的话，降权要等到 token 过期才生效。
func (s *Server) permsOf(c *gin.Context) permCtx {
	if v, ok := c.Get(ctxPerms); ok {
		if pc, ok := v.(permCtx); ok {
			return pc
		}
	}
	pc := s.loadPerms(c.Request.Context(), middleware.CurrentUser(c).Username)
	c.Set(ctxPerms, pc)
	return pc
}

// loadPerms 按用户名读出角色与权限码。
//
// ⚠️ 任何一步出错都返回**空权限**而不是全权限。
// 反过来会让一次数据库抖动把所有人临时变成管理员，
// 而这种"临时全权限"在日志里和正常请求长得一模一样。
func (s *Server) loadPerms(ctx context.Context, username string) permCtx {
	out := permCtx{codes: map[string]bool{}}
	p := s.st.Platform("load_perms")
	if p == nil {
		return out
	}
	var (
		roleCode     string
		unrestricted int
	)
	err := p.QueryRow(ctx,
		`SELECT u.role_code, COALESCE(r.unrestricted, 0)
		   FROM users u LEFT JOIN roles r ON r.code = u.role_code
		  WHERE u.username = ? AND u.deleted_at IS NULL`, username).
		Scan(&roleCode, &unrestricted)
	if err != nil {
		logx.J("perm", "load_failed", map[string]any{"user": username, "error": err.Error()})
		return out
	}
	if unrestricted == 1 {
		out.unrestricted = true
		return out
	}
	// role_code 为空 = 受限，不是全权限。008_rbac.sql 已把存量空值收敛到 viewer，
	// 这里再兜一次：迁移之后新写入的空值不该悄悄变成超级用户。
	if roleCode == "" {
		return out
	}
	rows, err := p.Query(ctx,
		`SELECT perm_code FROM role_permissions WHERE role_code = ?`, roleCode)
	if err != nil {
		logx.J("perm", "load_failed", map[string]any{"user": username, "error": err.Error()})
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			logx.J("perm", "load_failed", map[string]any{"user": username, "error": err.Error()})
			return permCtx{codes: map[string]bool{}}
		}
		out.codes[code] = true
	}
	if err := rows.Err(); err != nil {
		logx.J("perm", "load_failed", map[string]any{"user": username, "error": err.Error()})
		return permCtx{codes: map[string]bool{}}
	}
	return out
}

// me 返回当前身份与权限。前端靠它决定菜单和按钮的可见性。
func (s *Server) me(c *gin.Context) {
	u := middleware.CurrentUser(c)
	pc := s.permsOf(c)
	perms := make(map[string]bool, len(pc.codes))
	for k := range pc.codes {
		perms[k] = true
	}
	var displayName, authSource string
	if p := s.st.Platform("me"); p != nil {
		if err := p.QueryRow(c.Request.Context(),
			`SELECT display_name, auth_source FROM users WHERE username = ? AND deleted_at IS NULL`,
			u.Username).Scan(&displayName, &authSource); err != nil {
			abortLog(c, err)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"username":     u.Username,
		"display_name": displayName,
		"auth_source":  authSource,
		"role":         u.Role,
		// 前端**只认这个字段**判断是否受限，禁止按 auth_source 之类自行推导
		"is_admin":    pc.unrestricted,
		"permissions": perms,
	})
}

// AuditPermCheck 启动自检：把没进映射表的路由打出来。
//
//	fail-closed 意味着漏配 = 接口直接 403。与其等用户报"页面打不开"，
//	不如启动时就在日志里把清单列出来。
func AuditPermCheck(routes gin.RoutesInfo) {
	var missing []string
	for _, r := range routes {
		if !strings.HasPrefix(r.Path, "/api/") || permSkipped(r.Method, r.Path) {
			continue
		}
		if _, ok := resolvePerm(r.Method, r.Path); !ok {
			missing = append(missing, r.Method+" "+r.Path)
		}
	}
	if len(missing) == 0 {
		logx.J("perm", "selfcheck_ok", map[string]any{"routes": len(routes)})
		return
	}
	sort.Strings(missing)
	logx.J("perm", "selfcheck_missing", map[string]any{
		"count": len(missing), "routes": missing,
	})
}
