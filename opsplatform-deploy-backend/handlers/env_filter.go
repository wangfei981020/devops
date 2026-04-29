package handlers

import (
	"net/http"

	"opsplatform-deploy-backend/database"
)

// =========================================================================
//   按 env_name 白名单过滤 — 数据级权限控制
//
// 来源：portal-auth 时调用运维平台 /api/my/deploy-envs 拿到当前用户的
// env_name 白名单（来自 role_deploy_envs 表）。AuthMiddleware 已经把它
// 放进 ctx (ctxAllowedEnvs *[]string)。
//
// 语义：
//   ctx[allowedEnvs] == nil          → admin / 调用 portal 失败 → 不过滤（看全部）
//   ctx[allowedEnvs] == &[](empty)   → 该用户没分到任何 env → 全部隐藏
//   ctx[allowedEnvs] == &["g32-uat"] → 只能看这些 env
// =========================================================================

// AllowedEnvNames 返回白名单切片 + 是否启用过滤。
// enforce=false → 不过滤（admin 或拉取失败的 fail-open）
// enforce=true + 空切片 → 用户无任何 env 权限
func AllowedEnvNames(r *http.Request) (names []string, enforce bool) {
	v := r.Context().Value(ctxAllowedEnvs)
	ptr, ok := v.(*[]string)
	if !ok || ptr == nil {
		return nil, false
	}
	return *ptr, true
}

// AllowedEnvIDs 把白名单 env_name 解析成 K8s project_env.id 列表。
//   每次请求查一次 DB（开销小，一行 IN 查询）；不预存以避免 name→id 映射陈旧。
//   enforce=false → 不需要过滤；返回的 ids 应被忽略。
//   注意：只查 K8s 表。VM env 用 AllowedVmEnvIDs。
func AllowedEnvIDs(r *http.Request) (ids []int64, enforce bool) {
	return resolveEnvIDs(r, "project_env")
}

// AllowedVmEnvIDs 同 AllowedEnvIDs 但查 vm_project_env 表。
//   一个 env_name 不会同时在两张表（project 名跨表唯一），所以直接 IN 查不会撞号。
func AllowedVmEnvIDs(r *http.Request) (ids []int64, enforce bool) {
	return resolveEnvIDs(r, "vm_project_env")
}

// resolveEnvIDs 共用查询逻辑，table 必须是固定枚举值（不允许外部传入避免 SQL 注入）
func resolveEnvIDs(r *http.Request, table string) (ids []int64, enforce bool) {
	if table != "project_env" && table != "vm_project_env" {
		return nil, false
	}
	names, enforce := AllowedEnvNames(r)
	if !enforce {
		return nil, false
	}
	if len(names) == 0 {
		return []int64{}, true
	}
	placeholders := make([]byte, 0, len(names)*2-1)
	args := make([]interface{}, len(names))
	for i, n := range names {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = n
	}
	// table 已经枚举校验过，可安全拼接
	rows, err := database.DB.Query(
		"SELECT id FROM "+table+" WHERE name IN ("+string(placeholders)+")", args...)
	if err != nil {
		return []int64{}, true
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		out = append(out, id)
	}
	return out, true
}

// IsEnvIDAllowed 判断某个 K8s env_id 是否在用户白名单中。
// admin / 不过滤 → 总是 true。
func IsEnvIDAllowed(r *http.Request, envID int64) bool {
	ids, enforce := AllowedEnvIDs(r)
	if !enforce {
		return true
	}
	for _, id := range ids {
		if id == envID {
			return true
		}
	}
	return false
}

// IsVmEnvIDAllowed 同 IsEnvIDAllowed 但针对 VM env（查 vm_project_env 表）。
func IsVmEnvIDAllowed(r *http.Request, envID int64) bool {
	ids, enforce := AllowedVmEnvIDs(r)
	if !enforce {
		return true
	}
	for _, id := range ids {
		if id == envID {
			return true
		}
	}
	return false
}

// IsEnvNameAllowed 判断某个 env_name 是否在用户白名单中。
func IsEnvNameAllowed(r *http.Request, envName string) bool {
	names, enforce := AllowedEnvNames(r)
	if !enforce {
		return true
	}
	for _, n := range names {
		if n == envName {
			return true
		}
	}
	return false
}

// computeEnvTypeFlags 根据 env_name 列表查 DB 得到「该用户是否有任一 UAT / PROD env」。
// 用于前端 UI 守门：没有 PROD env 的用户隐藏所有 PROD 入口。
// 空列表 → 都返回 false（用户什么都看不到）。
// 出错 → 返回 (true, true) fail-open，避免误把 admin/普通用户的入口锁死。
//
// VM 表也得查：env_type 大写（UAT/LPT/PROD），LPT 业务上等同 UAT，归一化到 hasUAT。
func computeEnvTypeFlags(envNames []string) (hasUAT, hasPROD bool) {
	if len(envNames) == 0 {
		return false, false
	}
	placeholders := make([]byte, 0, len(envNames)*2-1)
	args := make([]interface{}, len(envNames))
	for i, n := range envNames {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = n
	}
	// UNION K8s + VM。VM env_type 大写，外层用 LOWER() 归一化
	q := `SELECT DISTINCT LOWER(env_type) FROM project_env WHERE name IN (` + string(placeholders) + `)
	  UNION
	  SELECT DISTINCT LOWER(env_type) FROM vm_project_env WHERE name IN (` + string(placeholders) + `)`
	// args 复用两次（K8s + VM 各一组）
	allArgs := make([]interface{}, 0, len(args)*2)
	allArgs = append(allArgs, args...)
	allArgs = append(allArgs, args...)
	rows, err := database.DB.Query(q, allArgs...)
	if err != nil {
		return true, true
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		_ = rows.Scan(&t)
		switch t {
		case "uat", "lpt": // LPT 当 UAT 处理（用户认定 LPT 是 UAT 的迁移命名）
			hasUAT = true
		case "prod":
			hasPROD = true
		}
	}
	return
}

// envTypeFlagsFromCtx 给已登录请求计算 has_any_uat / has_any_prod。
// 不过滤（admin / fail-open）→ 都 true。
// 启用过滤但白名单空 → 都 false。
// 启用过滤 → 查 DB。
func envTypeFlagsFromCtx(r *http.Request) (hasUAT, hasPROD bool) {
	names, enforce := AllowedEnvNames(r)
	if !enforce {
		return true, true
	}
	return computeEnvTypeFlags(names)
}
