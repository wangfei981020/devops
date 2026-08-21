// SPDX-License-Identifier: LicenseRef-OpsAlert-Enterprise
//
// ╔══════════════════════════════════════════════════════════════════╗
// ║  企业版组件 —— 使用受 ee/LICENSE 约束，需要有效授权。            ║
// ╚══════════════════════════════════════════════════════════════════╝
//
// Package mcp 是 AI 接入（Model Context Protocol）的实现。
//
// # 为什么在 ee/ 而不是 internal/
//
// 这是本产品的**付费分界线**。源码会交付给部署方，所以门控在技术上
// 是可以被删掉重编译的 —— 它挡的是"按约定使用的人"，不是攻击者。
// 把它放在独立目录 + 独立 LICENSE 之下，是为了让"绕过门控"
// 从一次技术操作变成一次**违反许可条款**的行为。那才是真正的边界。
//
// 🔴 往这个目录里加文件之前先问：它是不是真的只属于企业版？
//
//	被社区版功能复用的东西（比如 internal/dataview 那层查询）
//	放进来会让社区版跟着变成需要授权，那是反向的伤害。
package mcp

// MCP（Model Context Protocol）端点：把 OpsAlert 的只读能力暴露给 AI。
//
// # 为什么这个产品特别适合接 MCP
//
// 判定链、回放、试运行这三样天生就是给"要解释清楚为什么"的场景用的。
// 值班的人半夜问「这条为什么没告警」，AI 能直接调 why_not_fired 拿到
// 八步判定链，而不是猜——这是别的告警系统给不了的。
//
// # 一期只读
//
// 建规则、改阈值、静默、认领都不给 AI。理由和降噪中心那条「系统不自动改阈值」
// 一样：让模型直接改告警配置，出问题时没人说得清是谁决定的。
// 令牌已经带角色位，等真要开写工具时不用改表。
//
// # 继承自 CMDB MCP 的一条硬教训
//
// 工具执行失败必须置 isError 并把错误原文给出去。早期 CMDB 的实现无条件
// 把响应体当成功结果返回，于是 403/500 的 JSON 错误对象被 AI 理解成
// "查到了，但是空的"，转述给人就成了"该项没有数据"。
// 这正是本产品要消灭的反模式：失败被渲染成正常——只不过这次的受害者是 AI，
// 而 AI 比人更不会追问。

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-alert-backend/internal/api/middleware"
	"ops-alert-backend/internal/dataview"
	"ops-alert-backend/internal/httpx"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
)

type mcpParam struct {
	Name, Type, Desc string
	Required         bool
}

type mcpTool struct {
	Name string
	Desc string
	Args []mcpParam
	// run 直接查库而不是回调自己的 REST 接口：
	// 桥接 HTTP 要多一次自调用和一套内部令牌，而这些工具里有一半
	// （why_not_fired、noise_top）在 REST 里本来就没有对应端点。
	run func(*gin.Context, *store.Scoped, map[string]any) (any, error)
}

func (s *Server) mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name: "selfcheck",
			// 描述里写清语义陷阱：AI 看到空列表会说"没有问题"，
			// 而它可能只是规则全挂了。这句话就是为了让它别这么说。
			Desc: "告警系统自检：数据源可达性、规则执行健康、通知投递失败数、停止上报的数据源。" +
				"⚠️ 判断「现在有没有告警」之前先调它：规则执行失败时事件列表也是空的，" +
				"空列表不等于系统健康。",
			run: func(c *gin.Context, sc *store.Scoped, _ map[string]any) (any, error) {
				return dataview.SelfCheck(sc)
			},
		},
		{
			Name: "list_rules",
			Desc: "列出检测规则，含类型、数据源、上次执行时间、连续失败次数。" +
				"⚠️ consecutive_failures > 0 表示这条规则查询一直在失败——它不会告警，但界面上不会自己喊。",
			run: func(c *gin.Context, sc *store.Scoped, _ map[string]any) (any, error) {
				return queryList(sc, `SELECT r.id, r.name, r.kind, r.severity, r.enabled,
						r.interval_sec, r.last_run_at, r.last_error, r.consecutive_failures, d.name
					FROM rules r LEFT JOIN datasources d ON d.id = r.datasource_id
					WHERE r.tenant_id = ? AND r.deleted_at IS NULL ORDER BY r.id`,
					func(rows *sql.Rows) (any, error) {
						var id int64
						var name, kind, sev, lastErr string
						var enabled, interval, failures int
						var lastRun sql.NullTime
						var ds sql.NullString
						if err := rows.Scan(&id, &name, &kind, &sev, &enabled, &interval,
							&lastRun, &lastErr, &failures, &ds); err != nil {
							return nil, err
						}
						return gin.H{"id": id, "name": name, "kind": kind, "severity": sev,
							"enabled": enabled == 1, "interval_sec": interval,
							"last_run_at": nullTime(lastRun), "last_error": lastErr,
							"consecutive_failures": failures, "datasource": ds.String}, nil
					})
			},
		},
		{
			Name: "get_rule",
			Desc: "取单条规则的完整配置（含查询语句、阈值、分组、场景特有配置 spec）",
			Args: []mcpParam{{"rule_id", "number", "规则 ID", true}},
			run: func(c *gin.Context, sc *store.Scoped, a map[string]any) (any, error) {
				id := argInt(a, "rule_id")
				var name, kind, sev, lastErr string
				var spec, groupBy []byte
				var interval, lookback, threshold, forP, failures int
				var lastRun sql.NullTime
				err := sc.QueryRow(`SELECT name, kind, severity, spec, group_by, interval_sec,
						lookback_sec, threshold, for_periods, last_run_at, last_error, consecutive_failures
					FROM rules WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, id).
					Scan(&name, &kind, &sev, &spec, &groupBy, &interval, &lookback,
						&threshold, &forP, &lastRun, &lastErr, &failures)
				if err == sql.ErrNoRows {
					return nil, fmt.Errorf("规则 %d 不存在", id)
				}
				if err != nil {
					return nil, err
				}
				return gin.H{"id": id, "name": name, "kind": kind, "severity": sev,
					"spec": json.RawMessage(spec), "group_by": dataview.RawOrNull(groupBy),
					"interval_sec": interval, "lookback_sec": lookback,
					"threshold": threshold, "for_periods": forP,
					"last_run_at": nullTime(lastRun), "last_error": lastErr,
					"consecutive_failures": failures}, nil
			},
		},
		{
			Name: "list_incidents",
			Desc: "列出事件。status: active(未恢复，含被抑制) / resolved / all，默认 active",
			Args: []mcpParam{{"status", "string", "active | resolved | all", false}},
			run: func(c *gin.Context, sc *store.Scoped, a map[string]any) (any, error) {
				cond := `status IN ('firing','acked','suppressed')`
				switch argStr(a, "status") {
				case "resolved":
					cond = `status = 'resolved'`
				case "all":
					cond = `1 = 1`
				}
				return queryList(sc, `SELECT id, title, severity, status, count, first_at, last_at, acked_by
					FROM incidents WHERE tenant_id = ? AND `+cond+` ORDER BY last_at DESC LIMIT 100`,
					func(rows *sql.Rows) (any, error) {
						var id int64
						var title, sev, st, acked string
						var count int
						var first, last time.Time
						if err := rows.Scan(&id, &title, &sev, &st, &count, &first, &last, &acked); err != nil {
							return nil, err
						}
						return gin.H{"id": id, "title": title, "severity": sev, "status": st,
							"count": count, "first_at": first, "last_at": last, "acked_by": acked}, nil
					})
			},
		},
		{
			Name: "get_incident",
			Desc: "事件详情：标签、时间线、通知投递记录。⚠️ 投递记录里 status=failed 表示这条告警其实没送到人手上",
			Args: []mcpParam{{"incident_id", "number", "事件 ID", true}},
			run: func(c *gin.Context, sc *store.Scoped, a map[string]any) (any, error) {
				return dataview.IncidentDetail(sc, argInt(a, "incident_id"))
			},
		},
		{
			Name: "why_fired",
			Desc: "某条事件为什么会触发：返回完整判定链（查询→阈值→持续→抑制→静默→路由→投递→升级），" +
				"每一步带当时的实际数值与判据",
			Args: []mcpParam{{"incident_id", "number", "事件 ID", true}},
			run: func(c *gin.Context, sc *store.Scoped, a map[string]any) (any, error) {
				return dataview.TraceOfIncident(sc, argInt(a, "incident_id"))
			},
		},
		{
			Name: "why_not_fired",
			// 这是整个 MCP 里最有价值的一个工具：排查漏告警在别的系统里只能靠人翻日志猜。
			Desc: "某条规则为什么没有告警：返回它最近一次判定的完整链路，" +
				"能区分五种原因——查询没命中 / 未达持续周期 / 被抑制 / 被静默 / 匹配不到路由。" +
				"若规则本身在报错，会直接给出错误原文。",
			Args: []mcpParam{{"rule_id", "number", "规则 ID", true}},
			run: func(c *gin.Context, sc *store.Scoped, a map[string]any) (any, error) {
				return dataview.WhyNotFired(sc, argInt(a, "rule_id"))
			},
		},
		{
			Name: "dry_run",
			Desc: "试运行一段查询（不产生事件、不发通知）：返回命中数、分组分布、样本，以及按给定阈值会触发哪些分组。" +
				"用来验证查询语句写得对不对。",
			Args: []mcpParam{
				{"datasource_id", "number", "数据源 ID", true},
				{"query", "string", "LogQL 或 ES 查询串", true},
				{"threshold", "number", "触发阈值，默认 1", false},
				{"lookback_sec", "number", "回看窗口秒数，默认 300", false},
				{"group_by", "string", "分组维度，逗号分隔", false},
			},
			run: func(c *gin.Context, sc *store.Scoped, a map[string]any) (any, error) {
				var groupBy []string
				if g := argStr(a, "group_by"); g != "" {
					for _, p := range strings.Split(g, ",") {
						groupBy = append(groupBy, strings.TrimSpace(p))
					}
				}
				return s.DryRun(c.Request.Context(), sc, int64(argInt(a, "datasource_id")),
					argStr(a, "query"), argIntDefault(a, "lookback_sec", 300),
					argIntDefault(a, "threshold", 1), 200, groupBy)
			},
		},
		{
			Name: "list_datasources",
			Desc: "数据源清单与探测状态。⚠️ status=down 时挂在它上面的规则全部处于失明状态，" +
				"此时「没有告警」不能理解为「没有问题」",
			run: func(c *gin.Context, sc *store.Scoped, _ map[string]any) (any, error) {
				return queryList(sc, `SELECT id, name, type, status, probe_ms, probe_error, last_data_at
					FROM datasources WHERE tenant_id = ? AND deleted_at IS NULL ORDER BY id`,
					func(rows *sql.Rows) (any, error) {
						var id int64
						var name, typ, st, perr string
						var ms int
						var lastData sql.NullTime
						if err := rows.Scan(&id, &name, &typ, &st, &ms, &perr, &lastData); err != nil {
							return nil, err
						}
						return gin.H{"id": id, "name": name, "type": typ, "status": st,
							"probe_ms": ms, "probe_error": perr, "last_data_at": nullTime(lastData)}, nil
					})
			},
		},
		{
			Name: "noise_top",
			Desc: "最近 7 天最吵的规则：触发次数、误报率、深夜叫醒次数与调参建议",
			run: func(c *gin.Context, sc *store.Scoped, _ map[string]any) (any, error) {
				return dataview.NoiseTop(sc)
			},
		},
	}
}

// ── JSON-RPC 端点 ─────────────────────────────────────────────

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Register 挂上 MCP 的路由。
//
// # 未授权时返回 404 而不是 402
//
// 需求是「不激活就**看不到**」，不是「看得到但用不了」。
// 402 会明确告诉调用方"这里有个功能，只是你没买"——那是给
// 想促成购买的场景用的（噪音治理、回放实验室就是那样）。
// AI 接入这一项要的是不可见，所以路由表现得像根本不存在。
//
// ⚠️ 判据在**每次请求时**求值，不是注册时。
// 注册时判的话，激活之后必须重启进程路由才出现 ——
// 而多副本下重启是滚动的，会出现「一半副本有、一半没有」的几分钟。
func Register(public *gin.RouterGroup, authed *gin.RouterGroup, s *Server) {
	gate := func(h gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if s.Licensed != nil && !s.Licensed() {
				c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
				c.Abort()
				return
			}
			h(c)
		}
	}
	// MCP 用自己的令牌鉴权，不走登录中间件
	public.POST("/mcp", gate(s.mcpRPC))
	// 令牌管理走登录态
	authed.GET("/mcp/tokens", gate(s.listMCPTokens))
	authed.POST("/mcp/tokens", gate(s.createMCPToken))
	authed.DELETE("/mcp/tokens/:id", gate(s.revokeMCPToken))
}

func (s *Server) mcpRPC(c *gin.Context) {
	raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if raw == "" {
		raw = c.GetHeader("X-MCP-Token")
	}
	tokenID, tenantID, name, role, ok := s.resolveMCPToken(raw)
	if !ok {
		// 不区分"令牌不对"和"服务没开"：区分了就等于送对方一个探测接口。
		logx.J("mcp", "auth_failed", map[string]any{"ip": c.ClientIP(), "hint": safeHint(raw)})
		c.JSON(http.StatusUnauthorized, gin.H{"error": "MCP token 无效"})
		return
	}
	s.touchMCPToken(tokenID, c.ClientIP())

	var req rpcReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, rpcErr(nil, -32700, "parse error"))
		return
	}

	switch req.Method {
	case "initialize":
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{
			"protocolVersion": "2024-11-05",
			"capabilities":    gin.H{"tools": gin.H{}},
			"serverInfo":      gin.H{"name": "opsalert-mcp", "version": "1.0"},
		}))
	case "notifications/initialized", "notifications/cancelled":
		c.Status(http.StatusOK)
	case "ping":
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{}))
	case "tools/list":
		tools := []gin.H{}
		for _, t := range s.mcpTools() {
			props := gin.H{}
			required := []string{}
			for _, p := range t.Args {
				props[p.Name] = gin.H{"type": p.Type, "description": p.Desc}
				if p.Required {
					required = append(required, p.Name)
				}
			}
			tools = append(tools, gin.H{
				"name": t.Name, "description": t.Desc,
				"inputSchema": gin.H{"type": "object", "properties": props, "required": required},
			})
		}
		c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{"tools": tools}))
	case "tools/call":
		s.mcpCall(c, req, tenantID, name, role)
	default:
		c.JSON(http.StatusOK, rpcErr(req.ID, -32601, "method not found: "+req.Method))
	}
}

func (s *Server) mcpCall(c *gin.Context, req rpcReq, tenantID int64, actor, role string) {
	var p struct {
		Name string         `json:"name"`
		Args map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		c.JSON(http.StatusOK, rpcErr(req.ID, -32602, "invalid params"))
		return
	}
	var tool *mcpTool
	for _, t := range s.mcpTools() {
		if t.Name == p.Name {
			tt := t
			tool = &tt
			break
		}
	}
	if tool == nil {
		c.JSON(http.StatusOK, rpcErr(req.ID, -32601, "unknown tool: "+p.Name))
		return
	}

	// 租户来自令牌，不来自请求参数——否则拿到一个令牌就能查所有租户。
	ctx := store.WithTenant(c.Request.Context(), store.TenantID(tenantID))
	c.Request = c.Request.WithContext(ctx)
	sc, err := s.St.Tenant(ctx)
	if err != nil {
		c.JSON(http.StatusOK, mcpToolError(req.ID, "租户上下文缺失: "+err.Error()))
		return
	}

	started := time.Now()
	out, err := tool.run(c, sc, p.Args)
	logx.J("mcp", "tool_call", map[string]any{
		"tool": p.Name, "actor": actor, "role": role,
		"ms": time.Since(started).Milliseconds(), "ok": err == nil,
	})
	if err != nil {
		// ⚠️ 必须置 isError。返回成功外壳装错误内容，AI 会把它当成
		// "查到了但是空的"，然后向人转述"该项没有数据"。
		c.JSON(http.StatusOK, mcpToolError(req.ID, err.Error()))
		return
	}
	blob, _ := json.MarshalIndent(out, "", "  ")
	c.JSON(http.StatusOK, rpcOK(req.ID, gin.H{
		"content": []gin.H{{"type": "text", "text": string(blob)}},
	}))
}

func mcpToolError(id json.RawMessage, msg string) gin.H {
	return gin.H{"jsonrpc": "2.0", "id": id, "result": gin.H{
		"isError": true,
		"content": []gin.H{{"type": "text", "text": "工具执行失败：" + msg}},
	}}
}

func rpcOK(id json.RawMessage, result any) gin.H {
	return gin.H{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcErr(id json.RawMessage, code int, msg string) gin.H {
	return gin.H{"jsonrpc": "2.0", "id": id, "error": gin.H{"code": code, "message": msg}}
}

// safeHint 只回显令牌的前 4 位，用于对账"是不是拿错了令牌"，
// 又不至于把有效凭据写进日志。
func safeHint(tok string) string {
	if len(tok) <= 4 {
		return "(空或过短)"
	}
	return tok[:4] + "…"
}

// ── 令牌 ──────────────────────────────────────────────────────

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func (s *Server) resolveMCPToken(raw string) (id, tenantID int64, name, role string, ok bool) {
	if raw == "" {
		return 0, 0, "", "", false
	}
	// ⚠️ 这是全库唯一一处绕过 Scoped 的读，理由：鉴权发生在拿到租户**之前**——
	// 先有令牌才知道是哪个租户，鸡生蛋。
	// 安全性由 token_hash 的唯一索引保证：一个哈希只对应一行，
	// 拿不到别人的令牌就查不出别人的租户。查出来之后的每一次业务查询
	// 都必须走 Scoped（见 mcpCall 里的 WithTenant）。
	err := s.St.Raw().QueryRow(`SELECT id, tenant_id, name, role_code FROM mcp_tokens
		WHERE token_hash = ? AND enabled = 1 AND deleted_at IS NULL`, hashToken(raw)).
		Scan(&id, &tenantID, &name, &role)
	if err != nil {
		return 0, 0, "", "", false
	}
	return id, tenantID, name, role, true
}

func (s *Server) touchMCPToken(id int64, ip string) {
	// 同上：这次写的是令牌自身的使用痕迹，按 id 定位，不涉及业务数据。
	if _, err := s.St.Raw().Exec(`UPDATE mcp_tokens SET last_used_at = NOW(3), last_used_ip = ?
		WHERE id = ?`, ip, id); err != nil {
		logx.J("mcp", "touch_failed", map[string]any{"error": err.Error()})
	}
}

func (s *Server) listMCPTokens(c *gin.Context) {
	sc, err := s.St.Tenant(c.Request.Context())
	if err != nil {
		httpx.AbortTenant(c, err)
		return
	}
	rows, err := sc.Query(`SELECT id, name, role_code, enabled, last_used_at, last_used_ip, created_by, created_at
		FROM mcp_tokens WHERE tenant_id = ? AND deleted_at IS NULL ORDER BY id`)
	if err != nil {
		httpx.AbortQuery(c, err)
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name, role, ip, by string
		var enabled int
		var lastUsed sql.NullTime
		var created time.Time
		if err := rows.Scan(&id, &name, &role, &enabled, &lastUsed, &ip, &by, &created); err != nil {
			httpx.AbortQuery(c, err)
			return
		}
		out = append(out, gin.H{"id": id, "name": name, "role": role, "enabled": enabled == 1,
			"last_used_at": nullTime(lastUsed), "last_used_ip": ip,
			"created_by": by, "created_at": created,
			// 从不回显令牌本身——能被接口读出来的密钥等于没有密钥
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (s *Server) createMCPToken(c *gin.Context) {
	sc, err := s.St.Tenant(c.Request.Context())
	if err != nil {
		httpx.AbortTenant(c, err)
		return
	}
	var req struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rand_failed"})
		return
	}
	plain := "opsalert_" + hex.EncodeToString(b)
	user := middleware.CurrentUser(c)
	res, err := sc.Insert(`INSERT INTO mcp_tokens (tenant_id, name, token_hash, role_code, created_by)
		VALUES (?, ?, ?, ?, ?)`, req.Name, hashToken(plain), defaultStr(req.Role, "viewer"), user.Username)
	if err != nil {
		httpx.AbortQuery(c, err)
		return
	}
	id, _ := res.LastInsertId()
	s.Audit(c, sc, "mcp_token.create", "mcp_token", id, gin.H{"name": req.Name})
	// 明文只在这一次返回。之后任何接口都拿不到——丢了就吊销重建。
	c.JSON(http.StatusOK, gin.H{"id": id, "token": plain,
		"note": "令牌只显示这一次，请立即保存；丢失只能吊销后重建"})
}

func (s *Server) revokeMCPToken(c *gin.Context) {
	sc, err := s.St.Tenant(c.Request.Context())
	if err != nil {
		httpx.AbortTenant(c, err)
		return
	}
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := sc.Exec(`UPDATE mcp_tokens SET deleted_at = NOW(3), enabled = 0
		WHERE tenant_id = ? AND id = ?`, id); err != nil {
		httpx.AbortQuery(c, err)
		return
	}
	s.Audit(c, sc, "mcp_token.revoke", "mcp_token", id, nil)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ── 工具用到的查询辅助 ────────────────────────────────────────

func queryList(sc *store.Scoped, q string, scan func(*sql.Rows) (any, error)) (any, error) {
	rows, err := sc.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return gin.H{"items": out, "count": len(out)}, nil
}

func nullTime(t sql.NullTime) any {
	if !t.Valid {
		return nil
	}
	return t.Time
}

func argStr(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}

func argInt(a map[string]any, k string) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func argIntDefault(a map[string]any, k string, def int) int {
	if n := argInt(a, k); n > 0 {
		return n
	}
	return def
}
