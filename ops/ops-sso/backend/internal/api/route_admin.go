package api

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/apierr"
)

// ══════════════════════════════════════════════════════════════════
// 网关路由（零改造接入的必经一步）
// ══════════════════════════════════════════════════════════════════
//
// 一条路由就是「哪个域名的流量，转到哪个后端，按哪个应用判权限」。
// 在这之前它**只能直接写库** —— 而它是网关代管这条路上绕不过去的一步，
// 等于核心卖点在界面上走不完。
//
// ⚠️ 改动最多 30 秒生效：网关每 30s 重载一次路由表（热加载，
// 改配置不重启 —— 重启网关等于全公司瞬断）。界面必须说这句话，
// 否则人会以为没保存成功，然后再改一遍。

// routeJSON 一条路由。
func routeJSON(id, appID int64, host, upstream, mode, appName, connectType string) gin.H {
	return gin.H{
		"id": id, "app_id": appID, "host": host, "upstream": upstream,
		"inject_mode": mode, "app_name": appName, "connect_type": connectType,
	}
}

func listRoutes(c *gin.Context, d Deps) (any, error) {
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`SELECT r.id, r.app_id, r.host, r.upstream, r.inject_mode,
		       a.name, a.connect_type
		FROM app_routes r JOIN apps a ON a.id = r.app_id AND a.tenant_id = r.tenant_id
		WHERE r.tenant_id = ? AND r.deleted_at IS NULL AND a.deleted_at IS NULL
		ORDER BY r.host`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, appID int64
		var host, upstream, mode, name, ct string
		if err := rows.Scan(&id, &appID, &host, &upstream, &mode, &name, &ct); err != nil {
			return nil, err
		}
		out = append(out, routeJSON(id, appID, host, upstream, mode, name, ct))
	}
	return gin.H{"items": out, "total": len(out)}, rows.Err()
}

type routeReq struct {
	AppID      int64  `json:"app_id"`
	Host       string `json:"host"`
	Upstream   string `json:"upstream"`
	InjectMode string `json:"inject_mode"`
}

func (r *routeReq) normalize() error {
	// host 统一小写：网关查表用的是小写 host（见 gateway reload），
	// 这里不归一化的话，配 "Grafana.Local" 会永远匹配不到，且没有任何报错。
	r.Host = strings.ToLower(strings.TrimSpace(r.Host))
	r.Upstream = strings.TrimSpace(r.Upstream)
	if r.Host == "" || r.Upstream == "" || r.AppID <= 0 {
		return apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	// host 只能是主机名（可带端口），不能带 scheme 或路径 ——
	// 带了的话网关按 Host 头查表永远查不到，而界面上看着是配好的
	if strings.Contains(r.Host, "/") || strings.Contains(r.Host, ":") && strings.Contains(r.Host, "//") {
		return apierr.BadRequest(apierr.CodeRouteBadHost, map[string]any{"host": r.Host})
	}
	// upstream 必须带 scheme：不带的话 ReverseProxy 拼不出目标
	if !strings.HasPrefix(r.Upstream, "http://") && !strings.HasPrefix(r.Upstream, "https://") {
		r.Upstream = "http://" + r.Upstream
	}
	u, err := url.Parse(r.Upstream)
	if err != nil || u.Host == "" {
		return apierr.BadRequest(apierr.CodeRouteBadUpstream, map[string]any{"upstream": r.Upstream})
	}
	// 只放行已经能用的注入方式。formfill 的凭据目前没有配置入口，
	// 让人选中它 = 网关必然 503，那是"提供一个走不通的选项"。
	if r.InjectMode != "header" {
		r.InjectMode = "header"
	}
	return nil
}

func saveRoute(c *gin.Context, d Deps) (any, error) {
	var req routeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, apierr.BadRequest(apierr.CodeInvalidParam, nil)
	}
	if err := req.normalize(); err != nil {
		return nil, err
	}

	ctx := c.Request.Context()
	id := identity(c)
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return nil, err
	}

	// 应用必须属于本租户，否则改个 app_id 就能给别人的应用挂路由
	var ct string
	err = q.QueryRow(`SELECT connect_type FROM apps
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, req.AppID).Scan(&ct)
	if err == sql.ErrNoRows {
		return nil, apierr.CrossTenant()
	}
	if err != nil {
		return nil, err
	}

	idStr := c.Param("id")
	if idStr == "" {
		res, err := q.Insert(`INSERT INTO app_routes (tenant_id, app_id, host, upstream, inject_mode)
			VALUES (?, ?, ?, ?, ?)`, req.AppID, req.Host, req.Upstream, req.InjectMode)
		if err != nil {
			if isDupEntry(err) {
				// host 的唯一索引是**全局**的（uq_route_host），不带 tenant_id ——
				// 一个域名只能路由到一处，这是对的（网关按 Host 查表），
				// 但意味着冲突可能来自别的租户，报错不能说"你已经配过了"。
				return nil, apierr.New(http.StatusConflict, apierr.CodeRouteHostTaken,
					map[string]any{"host": req.Host})
			}
			return nil, err
		}
		newID, _ := res.LastInsertId()
		d.Audit.Write(ctx, auditEntry{
			TenantID: id.TenantID, ActorID: actorID(c), ActorName: id.Username,
			Action: "route.create", ObjectType: "route", ObjectID: newID, ClientIP: clientIP(c),
			Detail: map[string]any{"host": req.Host, "upstream": req.Upstream, "app_id": req.AppID},
		})
		return gin.H{"id": newID, "connect_type": ct}, nil
	}

	rid, err := pathID(c)
	if err != nil {
		return nil, err
	}
	// ⚠️ 先查存在性，别拿 RowsAffected 当"找到没有"用：
	// MySQL 数的是被改动的行数，原样保存（什么都没改）会返回 0，
	// 于是一条明明在列表里的路由被报成「不存在」。
	var exists int
	if err := q.QueryRow(`SELECT COUNT(*) FROM app_routes
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, rid).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, apierr.CrossTenant()
	}
	if _, err := q.Exec(`UPDATE app_routes SET app_id = ?, host = ?, upstream = ?, inject_mode = ?
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`,
		req.AppID, req.Host, req.Upstream, req.InjectMode, rid); err != nil {
		if isDupEntry(err) {
			return nil, apierr.New(http.StatusConflict, apierr.CodeRouteHostTaken,
				map[string]any{"host": req.Host})
		}
		return nil, err
	}
	d.Audit.Write(ctx, auditEntry{
		TenantID: id.TenantID, ActorID: actorID(c), ActorName: id.Username,
		Action: "route.update", ObjectType: "route", ObjectID: rid, ClientIP: clientIP(c),
		Detail: map[string]any{"host": req.Host, "upstream": req.Upstream, "app_id": req.AppID},
	})
	return gin.H{"id": rid, "connect_type": ct}, nil
}

// deleteRoute 软删。
//
// 删掉一条路由 = 这个域名的流量立刻没人接（最多 30 秒后生效）。
// 软删而不是硬删：三个月后复盘"那天谁把入口摘了"，硬删之后无从查起。
func deleteRoute(c *gin.Context, d Deps) (any, error) {
	rid, err := pathID(c)
	if err != nil {
		return nil, err
	}
	q, err := d.Store.Tenant(c.Request.Context())
	if err != nil {
		return nil, err
	}
	var host string
	err = q.QueryRow(`SELECT host FROM app_routes
		WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL`, rid).Scan(&host)
	if err == sql.ErrNoRows {
		return nil, apierr.CrossTenant()
	}
	if err != nil {
		return nil, err
	}
	// del_key 必须一起写：只写 deleted_at 的话这个 host 仍被占着，
	// 删掉之后同一个域名再也配不回来，而报错说的是"已存在"（见迁移 010）
	if _, err := q.Exec(`UPDATE app_routes SET deleted_at = NOW(), del_key = id
		WHERE tenant_id = ? AND id = ?`, rid); err != nil {
		return nil, err
	}
	d.Audit.Write(c.Request.Context(), auditEntry{
		TenantID: identity(c).TenantID, ActorID: actorID(c), ActorName: identity(c).Username,
		Action: "route.delete", ObjectType: "route", ObjectID: rid, ClientIP: clientIP(c),
		// 记下 host：删掉之后表里查不到了，而复盘要问的正是"哪个入口被摘了"
		Detail: map[string]any{"host": host},
	})
	return nil, nil
}
