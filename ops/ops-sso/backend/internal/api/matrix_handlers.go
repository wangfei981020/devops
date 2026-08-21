package api

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/domain/access"
)

// matrixMaxUsers 矩阵一次最多算多少人。
//
// 上限不是为了省 CPU（内存里判定很快），是为了**页面还能看**：
// 几千行的矩阵人眼扫不了，真要审计那么多人应该导出而不是渲染。
// ⚠️ 截断时必须在响应里说明（见下），静默截断会被读成"就这么多人"。
const matrixMaxUsers = 200

// accessMatrix 授权关系：谁能进哪个应用。
//
// # 为什么单独做这一页
//
// 策略页回答的是「规则长什么样」，而审计问的是「**谁能进这个系统**」——
// 那是从规则反推出来的结果，人脑推不了：一个人可能同时命中全局拒绝、
// 分组放行、单应用强制拒绝三条，最终结论要跑一遍判定才知道。
//
// # 为什么按人算而不是按规则展开
//
// 规则的主体是「用户组 / 部门」这类集合，而「谁能进」问的是具体的人。
// 按规则展开只能得到「哪些组被放行」，回答不了「张三到底行不行」——
// 而张三可能同时在三个组里，其中两个被拒。
//
// # 性能
//
// 每人一次 LoadSubject（3 条查询），全部规则一次性载入，
// 判定在内存里做。N 人 × M 应用 = 3N + 1 条查询，不是 N×M 条。
func accessMatrix(c *gin.Context, d Deps) (any, error) {
	ctx := c.Request.Context()

	apps, err := d.AppCat.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := d.Access.LoadAllRules(ctx)
	if err != nil {
		return nil, err
	}

	users, total, err := listTenantUsers(ctx, d, matrixMaxUsers)
	if err != nil {
		return nil, err
	}

	evalApps := make([]access.App, 0, len(apps))
	appOut := make([]gin.H, 0, len(apps))
	for _, a := range apps {
		if a.Status != "active" {
			continue
		}
		evalApps = append(evalApps, access.App{ID: a.ID, GroupIDs: a.GroupIDs})
		appOut = append(appOut, gin.H{"id": a.ID, "name": a.Name, "env": a.Env, "code": a.Code})
	}

	rows := make([]gin.H, 0, len(users))
	for _, u := range users {
		sub, err := d.Access.LoadSubject(ctx, u.ID)
		if err != nil {
			// 单个用户解析失败不该让整张表失败，但**必须标出来** ——
			// 悄悄跳过会让那一行凭空消失，而消失的行看起来像"这个人没权限"。
			rows = append(rows, gin.H{
				"user_id": u.ID, "username": u.Username, "display_name": u.DisplayName,
				"error": true,
			})
			continue
		}
		decisions := access.VisibleApps(sub, evalApps, rules)
		cells := make(map[string]any, len(evalApps))
		for _, a := range evalApps {
			dec := decisions[a.ID]
			cells[strconv.FormatInt(a.ID, 10)] = gin.H{"allowed": dec.Allowed(), "reason": dec.Reason}
		}
		rows = append(rows, gin.H{
			"user_id": u.ID, "username": u.Username, "display_name": u.DisplayName,
			"cells": cells,
		})
	}

	return gin.H{
		"apps": appOut,
		"rows": rows,
		// ⚠️ 截断要说出来。CONVENTIONS：不做静默截断 ——
		// 少了的那些人不会有任何痕迹，而"看不到"会被读成"没有"。
		"total_users": total,
		"truncated":   total > len(users),
		"limit":       matrixMaxUsers,
	}, nil
}

type tenantUser struct {
	ID          int64
	Username    string
	DisplayName string
}

// listTenantUsers 取本租户的用户。
//
// users 是全局表，租户归属在 user_tenants 里 —— 不 JOIN 会把别的租户的人
// 也列进来，那是跨租户信息泄露。
func listTenantUsers(ctx context.Context, d Deps, limit int) ([]tenantUser, int, error) {
	q, err := d.Store.Tenant(ctx)
	if err != nil {
		return nil, 0, err
	}

	var total int
	if err := q.QueryRow(`SELECT COUNT(*) FROM users u
		JOIN user_tenants ut ON ut.user_id = u.id AND ut.tenant_id = ?
		WHERE u.deleted_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := q.Query(`SELECT u.id, u.username, u.display_name FROM users u
		JOIN user_tenants ut ON ut.user_id = u.id AND ut.tenant_id = ?
		WHERE u.deleted_at IS NULL ORDER BY u.id LIMIT ?`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []tenantUser
	for rows.Next() {
		var u tenantUser
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}
