package handlers

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

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
//	返回每个 env 的 name + env_type + kind（不泄露 git/argocd/secret 等）。
//	供运维平台管理页"发布中心环境权限"勾选用。
//
//	包含两类：
//	  - kind='k8s'：来自 project_env 表（既有）
//	  - kind='vm' ：来自 vm_project_env 表（新增）
//	env_type 一律小写（K8s 本来就 uat/prod，VM 是 UAT/LPT/PROD → ToLower），
//	让运维平台用同一套 chip CSS。注意 'lpt' 是新值，运维平台如果按 uat/prod
//	白名单过滤可能要加上。
//	id 在两张表里可能撞号，但 name 全局唯一（project 名跨表唯一），运维平台
//	只用 name 做权限键，id 仅参考。
func HandlePublicListProjectEnvs(w http.ResponseWriter, r *http.Request) {
	type item struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		EnvType string `json:"env_type"`
		Kind    string `json:"kind"` // 'k8s' | 'vm'
	}
	out := []item{}

	// K8s envs
	rows, err := database.DB.Query(`SELECT id, name, env_type FROM project_env ORDER BY name`)
	if err != nil {
		JSONError(w, 50000, "query k8s: "+err.Error())
		return
	}
	for rows.Next() {
		var it item
		it.Kind = "k8s"
		_ = rows.Scan(&it.ID, &it.Name, &it.EnvType)
		out = append(out, it)
	}
	rows.Close()

	// VM envs（env_type 大写 → 小写保持跟 K8s 一致）
	vmRows, err := database.DB.Query(`SELECT id, name, env_type FROM vm_project_env ORDER BY name`)
	if err != nil {
		JSONError(w, 50000, "query vm: "+err.Error())
		return
	}
	defer vmRows.Close()
	for vmRows.Next() {
		var it item
		it.Kind = "vm"
		_ = vmRows.Scan(&it.ID, &it.Name, &it.EnvType)
		it.EnvType = strings.ToLower(it.EnvType)
		out = append(out, it)
	}

	JSONSuccess(w, out)
}

// HandlePublicListProjects GET /api/public/projects
//
//	返回项目名列表（项目级 RBAC 用）。包括 project 表里的 + 从 project_env name
//	派生出来的（与 HandleListProjects 同源去重逻辑），但只返回最简字段。
func HandlePublicListProjects(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
	}
	byName := map[string]*item{}

	// 1. project 表所有
	rows, err := database.DB.Query(`SELECT name, display_name FROM project`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n, d string
			_ = rows.Scan(&n, &d)
			byName[n] = &item{Name: n, DisplayName: d}
		}
	}
	// 2. project_env 派生（去 -uat / -prod 后缀）
	envRows, err := database.DB.Query(`SELECT name, env_type FROM project_env`)
	if err == nil {
		defer envRows.Close()
		for envRows.Next() {
			var name, envType string
			_ = envRows.Scan(&name, &envType)
			projName := name
			if envType != "" && len(name) > len(envType)+1 &&
				name[len(name)-len(envType)-1:] == "-"+envType {
				projName = name[:len(name)-len(envType)-1]
			}
			if _, ok := byName[projName]; !ok {
				byName[projName] = &item{Name: projName, DisplayName: ""}
			}
		}
	}

	out := make([]*item, 0, len(byName))
	for _, v := range byName {
		out = append(out, v)
	}
	// 按 name 字典序排（简单冒泡，量级很小）
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Name < out[i].Name {
				out[i], out[j] = out[j], out[i]
			}
		}
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
		// 🔒 安全：常量时间比较防 timing attack
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			JSONError(w, 40300, "invalid internal token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

