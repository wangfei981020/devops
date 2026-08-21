package api

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/access"
)

// ══════════════════════════════════════════════════════════════════
// 在线会话（管理员视角）
// ══════════════════════════════════════════════════════════════════

// adminSessions 全租户的在线会话。
//
// # 和 /auth/sessions 的区别
//
// 那个是「我自己在哪几个地方登着」，这个是「现在谁在线」。
// 后者是出事时第一个要看的东西：某个账号被盗用、某人今天离职，
// 要能立刻看到他还有几个会话活着，并且**当场踢掉**。
//
// # 为什么不返回 token_hash
//
// 哪怕是哈希也不返回。它是会话令牌的唯一凭据形态，一旦泄露就等于泄露会话 ——
// 生产上出过的两个 P0 都是「接口把凭据发给了不该看的人」，不再重演。
func adminSessions(c *gin.Context, d Deps) (any, error) {
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	// 只列还活着的：已吊销/已过期的属于历史，混在一起会让人把死会话当成风险，
	// 也会让「他还在线吗」这个问题答不出来。
	rows, err := q.Query(`SELECT s.id, s.user_id, u.username,
		       IF(u.display_name = '', u.username, u.display_name),
		       s.source, s.client_ip, s.user_agent, s.created_at, s.last_seen_at, s.expires_at
		FROM auth_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.tenant_id = ? AND s.revoked_at IS NULL AND s.expires_at > NOW()
		ORDER BY s.last_seen_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cur := identity(c).SessionID
	out := []gin.H{}
	for rows.Next() {
		var (
			id, uid                int64
			username, display      string
			source, ip, ua         string
			created, seen, expires time.Time
		)
		if err := rows.Scan(&id, &uid, &username, &display, &source, &ip, &ua,
			&created, &seen, &expires); err != nil {
			return nil, err
		}
		out = append(out, gin.H{
			"id": id, "user_id": uid, "username": username, "display_name": display,
			"source": source, "client_ip": ip, "user_agent": ua,
			"created_at": created, "last_seen_at": seen, "expires_at": expires,
			// 标出「这就是你现在这个会话」——不标的话，管理员很容易把自己踢下线，
			// 而那在处理安全事件的当口是最糟的时机。
			"current": id == cur,
		})
	}
	return gin.H{"items": out, "total": len(out)}, rows.Err()
}

// adminRevokeSession 管理员强制下线某个会话。
func adminRevokeSession(c *gin.Context, d Deps) (any, error) {
	id, err := pathID(c)
	if err != nil {
		return nil, err
	}
	if id == identity(c).SessionID {
		// 不拦着不行：点完自己就登出了，而他大概率正在处理一起安全事件。
		// 要退出自己有「退出登录」，语义清楚得多。
		return nil, apierr.BadRequest(apierr.CodeInvalidParam,
			map[string]any{"reason": "self_session"})
	}

	// 先确认这个会话属于本租户 —— 直接按 id 更新的话，改个数字就能踢掉别的租户的人。
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	var uid int64
	err = q.QueryRow(`SELECT user_id FROM auth_sessions
		WHERE tenant_id = ? AND id = ? AND revoked_at IS NULL`, id).Scan(&uid)
	if err == sql.ErrNoRows {
		// 跨租户、已下线、已过期 —— 一律 404，不告诉对方「存在但你动不了」
		return nil, apierr.CrossTenant()
	}
	if err != nil {
		return nil, err
	}

	if err := d.Auth.Revoke(c.Request.Context(), id, "admin_revoked"); err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "session.revoke", ObjectType: "user", ObjectID: uid, ClientIP: clientIP(c),
		Detail: map[string]any{"session_id": id},
	})
	return nil, nil
}

// ══════════════════════════════════════════════════════════════════
// 人员与用户组
// ══════════════════════════════════════════════════════════════════

// adminUsers 本租户的人。
//
// 只读。用户的创建来自身份源同步或本地建号，不在这一页做 ——
// 这一页要回答的是「这个人是谁、在哪些组里、还有几个会话活着」，
// 也就是复核授权时真正要看的东西。
func adminUsers(c *gin.Context, d Deps) (any, error) {
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT u.id, u.username,
		       IF(u.display_name = '', u.username, u.display_name),
		       u.email, u.source, u.status, u.is_break_glass, u.last_login_at, ut.role_code,
		       (SELECT COUNT(*) FROM auth_sessions s
		          WHERE s.user_id = u.id AND s.revoked_at IS NULL AND s.expires_at > NOW())
		FROM users u JOIN user_tenants ut ON ut.user_id = u.id AND ut.tenant_id = ?
		WHERE u.deleted_at IS NULL
		ORDER BY u.id LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []gin.H{}
	ids := []int64{}
	for rows.Next() {
		var (
			id            int64
			username, dn  string
			email, source string
			status        string
			breakGlass    int
			lastLogin     sql.NullTime
			role          sql.NullString
			sessions      int
		)
		if err := rows.Scan(&id, &username, &dn, &email, &source, &status,
			&breakGlass, &lastLogin, &role, &sessions); err != nil {
			return nil, err
		}
		h := gin.H{
			"id": id, "username": username, "display_name": dn, "email": email,
			"source": source, "status": status, "is_break_glass": breakGlass == 1,
			// 空角色 = 普通成员。界面据此决定显示「提升为管理员」还是「降为普通成员」
			"role_code": strings.TrimSpace(role.String),
			"sessions":  sessions, "groups": []string{},
		}
		if lastLogin.Valid {
			h["last_login_at"] = lastLogin.Time
		}
		// last_login_at 为空就是**从没登录过**，不是"最近没登"。
		// 前端据此显示「从未登录」——一个从没用过的账号还开着，是复核时要清的。
		users = append(users, h)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 组名批量查，不做 N+1
	if len(ids) > 0 {
		ph, args := placeholders(ids)
		gr, err := q.Query(`SELECT m.user_id, g.name FROM user_group_members m
			JOIN user_groups g ON g.id = m.group_id AND g.tenant_id = m.tenant_id
			WHERE m.tenant_id = ? AND m.user_id IN`+ph, args...)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		byUser := map[int64][]string{}
		for gr.Next() {
			var uid int64
			var name string
			if err := gr.Scan(&uid, &name); err != nil {
				return nil, err
			}
			byUser[uid] = append(byUser[uid], name)
		}
		if err := gr.Err(); err != nil {
			return nil, err
		}
		for _, u := range users {
			if g := byUser[u["id"].(int64)]; len(g) > 0 {
				u["groups"] = g
			}
		}
	}
	return gin.H{"items": users, "total": len(users)}, nil
}

// adminUserGroups 用户组，带成员数与「这个组被多少条规则引用」。
//
// 引用数是关键：一个没有任何规则引用的组，配了也不起作用 ——
// 而它看起来和生效中的组一模一样。
func adminUserGroups(c *gin.Context, d Deps) (any, error) {
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT g.id, g.name,
		       (SELECT COUNT(*) FROM user_group_members m
		          WHERE m.tenant_id = g.tenant_id AND m.group_id = g.id),
		       (SELECT COUNT(*) FROM access_policies p
		          WHERE p.tenant_id = g.tenant_id AND p.deleted_at IS NULL
		            AND p.subject_type = 'group' AND p.subject_id = g.id)
		FROM user_groups g WHERE g.tenant_id = ? ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name string
		var members, rules int
		if err := rows.Scan(&id, &name, &members, &rules); err != nil {
			return nil, err
		}
		out = append(out, gin.H{"id": id, "name": name, "members": members, "rules": rules})
	}
	return gin.H{"items": out, "total": len(out)}, rows.Err()
}

// ══════════════════════════════════════════════════════════════════
// 总览
// ══════════════════════════════════════════════════════════════════

// overview 首屏。
//
// # 什么该上首屏
//
// 只放**需要有人动手**的东西。一屏漂亮的数字没有用 ——
// 看的人要能立刻答出「现在有没有问题、下一步做什么」。
// 所以这里每一项要么是异常计数，要么是一个能点进去的入口。
//
// # 单项失败不能让整屏失败
//
// 首屏聚了七八个查询，任何一个挂了都让整页报错的话，这一页会是最脆的一页。
// 每项独立取，失败的那项标 null 并计入 degraded —— 前端把它显示成
// 「这项没取到」而不是 0。**0 和「没取到」必须长得不一样**：
// 前者是"没问题"，后者是"不知道有没有问题"。
func overview(c *gin.Context, d Deps) (any, error) {
	ctx := c.Request.Context()
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return nil, err
	}

	// ⚠️ 必须初始化成空切片，不能是 nil。
	// Go 的 nil 切片序列化成 `null` 而不是 `[]`，前端 `degraded.length`
	// 直接抛 TypeError、整页白屏 —— 实测就是这么炸的。
	// 契约上凡是「列表」就必须永远是数组，空也是数组。
	degraded := []string{}
	num := func(name, sqlText string, args ...any) any {
		var n int
		if err := q.QueryRow(sqlText, args...).Scan(&n); err != nil {
			degraded = append(degraded, name)
			return nil
		}
		return n
	}

	out := gin.H{
		"apps":     num("apps", `SELECT COUNT(*) FROM apps WHERE tenant_id = ? AND status = 'active'`),
		"rules":    num("rules", `SELECT COUNT(*) FROM access_policies WHERE tenant_id = ? AND deleted_at IS NULL`),
		"users":    num("users", `SELECT COUNT(*) FROM users u JOIN user_tenants ut ON ut.user_id = u.id AND ut.tenant_id = ? WHERE u.deleted_at IS NULL`),
		"sessions": num("sessions", `SELECT COUNT(*) FROM auth_sessions WHERE tenant_id = ? AND revoked_at IS NULL AND expires_at > NOW()`),
		// 近 24h 的拦截数。突然变多通常意味着**有人改了规则**，
		// 而不是有人在攻击 —— 这一项是最常把人引到「配置变更」那一页的线索。
		"denied_24h": num("denied_24h", `SELECT COUNT(*) FROM access_events
			WHERE tenant_id = ? AND decision = 'deny' AND occurred_at > NOW() - INTERVAL 1 DAY`),
		// 没有任何规则引用的应用 = 谁都进不去（默认拒绝）。
		// 接入了却没配规则是最常见的"配了一半"，而界面上它看着一切正常。
		"apps_without_rules": num("apps_without_rules", `SELECT COUNT(*) FROM apps a
			WHERE a.tenant_id = ? AND a.status = 'active'
			  AND NOT EXISTS (SELECT 1 FROM access_policies p
			    WHERE p.tenant_id = a.tenant_id AND p.deleted_at IS NULL
			      AND ((p.scope = 'app' AND p.scope_id = a.id) OR p.scope = 'global'))`),
		"probes_failing": num("probes_failing", `SELECT COUNT(*) FROM probe_credentials
			WHERE tenant_id = ? AND revoked_at IS NULL AND last_result <> '' AND last_result <> 'ok'`),
		"probes_never_run": num("probes_never_run", `SELECT COUNT(*) FROM probe_credentials
			WHERE tenant_id = ? AND revoked_at IS NULL AND last_result = ''`),
	}

	// 审计链结论也放首屏：它是"这套记录还可信吗"的唯一答案，
	// 藏在二级页里等于没人会点。
	if res, err := d.Audit.VerifyChain(ctx, identity(c).TenantID, 1000); err != nil {
		degraded = append(degraded, "audit_chain")
		out["audit_chain"] = nil
	} else {
		h := gin.H{"ok": res.OK, "checked": res.Checked}
		if !res.OK {
			h["broken_at_seq"] = res.BrokenAt
			h["reason"] = res.Reason
		}
		out["audit_chain"] = h
	}

	// ⚠️ 必须把降级项列出来。少一项而不说，看的人会把 null 当成 0，
	// 也就是把"不知道"当成"没问题"。
	out["degraded"] = degraded
	return out, nil
}

// ══════════════════════════════════════════════════════════════════
// 接入信息
// ══════════════════════════════════════════════════════════════════

// providerInfo 接入方要填的那几行。
//
// # 为什么要有这个接口
//
// 「issuer 填什么」是每个接入方都会问的第一个问题，而唯一可信的答案是
// **这个进程里实际生效的值** —— 不是部署手册（可能过期）、
// 不是 values.yaml（可能不是这套环境的那一份）、也不是谁的记忆。
// 让人去猜，最常见的结果是填了个末尾多斜杠的版本，
// 然后在下游得到一句「iss 不匹配」，两边各查半天。
//
// 只读。issuer 是部署期配置，不在界面上改 —— 改它要同时改所有下游，
// 详见 config.ResolveIssuer 的注释。
func providerInfo(c *gin.Context, d Deps) (any, error) {
	local := strings.Contains(d.Issuer, "localhost") || strings.Contains(d.Issuer, "127.0.0.1")
	return gin.H{
		"issuer":        d.Issuer,
		"discovery_url": d.Issuer + "/.well-known/openid-configuration",
		"jwks_url":      d.Issuer + "/oidc/jwks",
		// 本机地址在界面上要标出来：它能跑通本地联调，
		// 但任何真实下游从**它自己的容器**里去拉 jwks 都会失败 ——
		// 而那时报错在下游，没人会怀疑到这里。
		"is_local": local,
		// 生产模式下 issuer 已经在启动时校验过（不合规直接拒启），
		// 这里把模式也带上，界面才能说清"这台是不是被校验过的那种"。
		"run_mode": d.RunMode,
	}, nil
}

// ══════════════════════════════════════════════════════════════════
// 门户：服务状态
// ══════════════════════════════════════════════════════════════════

// portalStatus 我能用的系统，现在是不是还活着。
//
// # 为什么不在打开页面时现场探测一次
//
// 那会把门户变成一个**放大器**：一百个人刷新页面就是一百轮对所有下游的请求，
// 而且探测目标来自库里的地址 —— 等于给了任何登录用户一个从服务端
// 发起任意内网请求的入口（SSRF）。
//
// 所以这里只**读探针最近一次的结果**：探测由后台按固定频率发起，
// 目标和频率都是管理员配的，页面只是把结论拿出来看。
//
// # 为什么必须按可见性过滤
//
// 只列这个人本来就能看到的系统。不过滤的话，这一页会把公司里
// 所有系统的存在与内网可用性告诉每一个能登录的人 ——
// 那是一份很好用的踩点清单。
//
// # 没配探针显示什么
//
// 「未覆盖」，不是「正常」。一个从没被探测过的系统显示成绿色，
// 是在拿"我们没在看"冒充"没问题"。
func portalStatus(c *gin.Context, d Deps) (any, error) {
	ctx := c.Request.Context()
	uid := actorID(c)

	apps, err := d.AppCat.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	sub, err := d.Access.LoadSubject(ctx, uid)
	if err != nil {
		return nil, err
	}
	rules, err := d.Access.LoadAllRules(ctx)
	if err != nil {
		return nil, err
	}

	evalApps := make([]access.App, 0, len(apps))
	for _, a := range apps {
		evalApps = append(evalApps, access.App{ID: a.ID, GroupIDs: a.GroupIDs})
	}
	decisions := access.VisibleApps(sub, evalApps, rules)

	// 探针结果一次取回，按 app_id 索引 —— 不做 N+1
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT app_id, last_probe_at, last_result, last_detail
		FROM probe_credentials WHERE tenant_id = ? AND revoked_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type probe struct {
		at     sql.NullTime
		result string
		detail string
	}
	byApp := map[int64]probe{}
	for rows.Next() {
		var id int64
		var p probe
		if err := rows.Scan(&id, &p.at, &p.result, &p.detail); err != nil {
			return nil, err
		}
		byApp[id] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []gin.H{}
	for _, a := range apps {
		if a.Status != "active" {
			continue
		}
		dec := decisions[a.ID]
		if !dec.Allowed() && !a.ShowWhenDenied {
			continue // 看不见的系统不出现在状态页上
		}
		item := gin.H{
			"id": a.ID, "code": a.Code, "name": a.Name, "env": a.Env,
			"icon_text": a.IconText, "icon_color": a.IconColor,
			"allowed": dec.Allowed(),
		}
		p, ok := byApp[a.ID]
		switch {
		case !ok:
			// 没配探针 —— 我们根本没在看这个系统
			item["state"] = "uncovered"
		case p.result == "":
			// 配了但一次都没跑起来。和"跑过、没问题"必须区分：
			// 前者说明探针本身可能就是坏的
			item["state"] = "never"
		case p.result == "ok":
			item["state"] = "ok"
		default:
			item["state"] = "failing"
			// 失败详情给用户看：他要判断的是"是我的问题还是它的问题"
			item["detail"] = p.detail
		}
		if p.at.Valid {
			item["checked_at"] = p.at.Time
		}
		out = append(out, item)
	}
	return gin.H{"items": out, "total": len(out)}, nil
}
