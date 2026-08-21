package api

import (
	"context"
	"database/sql"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/logx"
)

// ══════════════════════════════════════════════════════════════════
// 接入指引：把「这个应用现在配到哪一步了」摊开
// ══════════════════════════════════════════════════════════════════
//
// # 为什么不是一份文档
//
// 文档的失败模式很固定：**手册里的值和实际跑着的值对不上**。
// 而 OIDC 接入最常卡住的三件事恰好全是"值" ——
// issuer 抄错、回调地址差一个字符、client_id 与实际不符。
// 一份会过期的文档解决不了这个。
//
// 所以这个接口只回**当前进程与当前库里的事实**，前端照着渲染步骤。
//
// # 「我们能验的」和「验不了的」必须分开
//
// 我们能验：issuer 是不是外部可达的形状、客户端建了没、回调地址登记了没、
// 策略会不会放行、**有没有真的收到过一次登录尝试**。
// 我们验不了：对方系统里填对没有 —— 那台机器不归我们管。
//
// 把两类混成一个绿勾，等于告诉客户"都好了"，而他一点就报错。
// 最后那条（有没有来过流量）是这一页比文档多出来的全部价值：
// 它能区分「配完了但从没人来过」和「来了但被拒」——两者的下一步完全不同。

func appOnboarding(c *gin.Context, d Deps) (any, error) {
	appID, err := pathID(c)
	if err != nil {
		return nil, err
	}
	ctx := c.Request.Context()
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return nil, err
	}

	var (
		name, code, connectType, baseURL, status string
	)
	err = q.QueryRow(`SELECT name, code, connect_type, base_url, status
		FROM apps WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, appID).
		Scan(&name, &code, &connectType, &baseURL, &status)
	if err == sql.ErrNoRows {
		return nil, apierr.CrossTenant()
	}
	if err != nil {
		return nil, err
	}

	out := gin.H{
		"app": gin.H{
			"id": appID, "name": name, "code": code,
			"connect_type": connectType, "base_url": baseURL, "status": status,
		},
		"issuer":        d.Issuer,
		"discovery_url": d.Issuer + "/.well-known/openid-configuration",
		"jwks_url":      d.Issuer + "/oidc/jwks",
		// ⚠️ 这一条是最容易被忽略、又最致命的：issuer 是回环地址时，
		// **任何下游都接不进来** —— 下游从自己的容器里拉 localhost，
		// 拉到的是它自己。而界面上一切正常，错误只会出现在对方那边。
		"issuer_is_local": issuerIsLocal(d.Issuer),
	}

	// ── OIDC 侧 ──
	clients := []gin.H{}
	rows, err := d.Store.Raw().QueryContext(ctx,
		`SELECT client_id, redirect_uris, enabled FROM oidc_clients
		 WHERE tenant_id = ? AND app_id = ?`, int64(identity(c).TenantID), appID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid, uris string
			var enabled int
			if err := rows.Scan(&cid, &uris, &enabled); err != nil {
				return nil, err
			}
			clients = append(clients, gin.H{
				"client_id": cid, "redirect_uris": splitList(uris), "enabled": enabled == 1,
			})
		}
	}
	out["oidc_clients"] = clients

	// ── 网关侧 ──
	routes := []gin.H{}
	rr, err := q.Query(`SELECT host, upstream FROM app_routes
		WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`, appID)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	for rr.Next() {
		var host, upstream string
		if err := rr.Scan(&host, &upstream); err != nil {
			return nil, err
		}
		routes = append(routes, gin.H{"host": host, "upstream": upstream})
	}
	out["routes"] = routes

	// 接口级规则数。**0 条 = 全部拒绝**（path_default_deny），
	// 而这时策略页会显示"能进" —— 三个界面互相矛盾，实测踩过。
	var pathRules int
	_ = q.QueryRow(`SELECT COUNT(*) FROM path_rules
		WHERE tenant_id = ? AND app_id = ? AND deleted_at IS NULL`, appID).Scan(&pathRules)
	out["path_rules"] = pathRules

	// ── 策略侧：**现在到底有几个人能进** ──
	//
	// ⚠️ 不能只数"有没有 allow 规则"。第一版就是那么写的，结果一条
	// 给别人的全局 allow 就让这一步打了绿勾 —— 而实际上没有任何人
	// 能进这个应用。绿勾说的是"配好了"，那就必须真的配好了。
	//
	// 所以真跑一遍判定：逐个用户算 Evaluate，数出能进的人数。
	// 贵一点，但这是唯一不会说谎的算法 —— 和授权关系矩阵同一套。
	allowedUsers, totalUsers, evalErr := countAllowedUsers(ctx, d, appID)
	policy := gin.H{"allowed_users": allowedUsers, "total_users": totalUsers}
	if evalErr != nil {
		// 算不出来 ≠ 没人能进。前者要显示"这一步查不了"，
		// 后者才是"还没配"。混在一起会让人去改一个没问题的策略。
		policy["unavailable"] = true
		logx.Line("onboarding", "算不出可访问人数: "+evalErr.Error())
	}
	out["policy"] = policy

	// ── 有没有真的来过流量 ──
	//
	// 这一条是这一页比文档多出来的全部价值：
	// 「配完了但从没人来过」和「来了但被拒」，下一步完全不同 ——
	// 前者去查对方系统的配置，后者去查这边的策略。
	tctx := store.WithTenant(ctx, store.TenantID(identity(c).TenantID))
	var events, denied int
	var lastAt sql.NullString
	_ = d.Store.Raw().QueryRowContext(tctx,
		`SELECT COUNT(*), SUM(decision = 'deny'), MAX(occurred_at)
		 FROM access_events WHERE tenant_id = ? AND app_id = ?`,
		int64(identity(c).TenantID), appID).Scan(&events, &denied, &lastAt)

	// OIDC 的登录尝试不进 access_events（那是网关的表），在审计里
	var oidcAttempts int
	_ = d.Store.Raw().QueryRowContext(tctx,
		`SELECT COUNT(*) FROM audit_logs
		 WHERE tenant_id = ? AND object_type = 'app' AND object_id = ?
		   AND action IN ('oidc.authorize','oidc.token','oidc.authorize_denied')`,
		int64(identity(c).TenantID), appID).Scan(&oidcAttempts)

	out["traffic"] = gin.H{
		"gateway_events": events, "gateway_denied": denied,
		"oidc_attempts": oidcAttempts, "last_at": lastAt.String,
	}
	return out, nil
}

// countAllowedUsers 数出现在有几个人能进这个应用。
//
// 与授权关系矩阵共用 access.VisibleApps —— 两处给出不同答案，
// 比给不出答案更糟。
func countAllowedUsers(ctx context.Context, d Deps, appID int64) (int, int, error) {
	apps, err := d.AppCat.ListApps(ctx)
	if err != nil {
		return 0, 0, err
	}
	var target *access.App
	for _, a := range apps {
		if a.ID == appID {
			target = &access.App{ID: a.ID, GroupIDs: a.GroupIDs}
			break
		}
	}
	if target == nil {
		return 0, 0, nil
	}
	rules, err := d.Access.LoadAllRules(ctx)
	if err != nil {
		return 0, 0, err
	}
	users, total, err := listTenantUsers(ctx, d, matrixMaxUsers)
	if err != nil {
		return 0, 0, err
	}
	n := 0
	for _, u := range users {
		sub, err := d.Access.LoadSubject(ctx, u.ID)
		if err != nil {
			// 单个用户算不出来就跳过，但整体不算失败：
			// 少数一个人不影响"有没有人能进"这个结论
			continue
		}
		if access.Evaluate(sub, *target, rules).Allowed() {
			n++
		}
	}
	return n, total, nil
}

// issuerIsLocal issuer 指向本机时，任何外部下游都接不进来。
func issuerIsLocal(issuer string) bool {
	u, err := url.Parse(issuer)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
