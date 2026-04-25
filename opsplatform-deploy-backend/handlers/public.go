package handlers

import (
	"net/http"
	"os"

	"opsplatform-deploy-backend/database"
)

// =========================================================================
//   /api/public/* — 供运维平台管理页（系统间调用）使用的只读接口
//
// 不走用户 JWT 鉴权；靠"内网共享 token"（env: DEPLOY_CENTER_INTERNAL_TOKEN）
// 验证调用方身份。运维平台的 HandleProxyDeployCenterEnvs 在 header 里带
// X-Internal-Token，发布中心这边对比。
//
// **必须配 env**，否则默认拒绝所有调用（避免 token 为空时 "no-token" 撞上
// 攻击者猜测导致绕过）。
// =========================================================================

// HandlePublicListProjectEnvs GET /api/public/project-envs
//
//	仅返回每个 env 的 name + env_type（不泄露 git/argocd/secret 等）。
//	供运维平台管理页"发布中心环境权限"勾选用。
func HandlePublicListProjectEnvs(w http.ResponseWriter, r *http.Request) {
	// 简洁起见，列出 name + env_type + id（id 前端不用，但万一以后需要）
	rows, err := database.DB.Query(
		`SELECT id, name, env_type FROM project_env ORDER BY name`)
	if err != nil {
		JSONError(w, 50000, "query: "+err.Error())
		return
	}
	defer rows.Close()

	type item struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		EnvType string `json:"env_type"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		_ = rows.Scan(&it.ID, &it.Name, &it.EnvType)
		out = append(out, it)
	}
	JSONSuccess(w, out)
}

// InternalTokenMiddleware 校验 X-Internal-Token header 与 env 中配置的值一致
//
//	未配 env 或 token 不匹配 → 403。运维平台通过 HandleProxyDeployCenterEnvs
//	带 X-Internal-Token 调本接口。
func InternalTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := os.Getenv("DEPLOY_CENTER_INTERNAL_TOKEN")
		if expected == "" {
			JSONError(w, 50003, "internal token not configured on server")
			return
		}
		got := r.Header.Get("X-Internal-Token")
		if got == "" || got != expected {
			JSONError(w, 40300, "invalid internal token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

