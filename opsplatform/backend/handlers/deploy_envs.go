package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"opsplatform/database"
)

// getDeployCenterInternalToken 从 env 读 opsplatform 与发布中心共享的 internal token
// 空串时返回"no-token"占位；发布中心端也看 env，对得上才放行
func getDeployCenterInternalToken() string {
	if t := os.Getenv("DEPLOY_CENTER_INTERNAL_TOKEN"); t != "" {
		return t
	}
	return "no-token"
}

// newDefaultHTTPClient 内网短 timeout，10s 足够
func newDefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// copyBody 原样转发 body
func copyBody(src io.Reader, dst http.ResponseWriter) {
	_, _ = io.Copy(dst, src)
}

// =========================================================================
//   发布中心环境权限（role_deploy_envs）
//
// 运维平台这边只存 "role_id → env_name" 映射（env_name 是字符串，来自发布中心
// project_env.name）。env 列表由前端打开管理页时调发布中心的
// GET /api/public/project-envs 拉实时获取——所以运维平台不需要知道发布中心
// 的业务数据，两边弱耦合。
// =========================================================================

// HandleGetRoleDeployEnvs GET /api/admin/role-deploy-envs/{roleID}
//
//	返回某角色被授权访问的发布中心 env_name 列表（用于管理页勾选状态回显）。
func HandleGetRoleDeployEnvs(w http.ResponseWriter, r *http.Request) {
	roleID := mux.Vars(r)["roleID"]
	rows, err := database.DB.Query(
		`SELECT env_name FROM role_deploy_envs WHERE role_id = ? ORDER BY env_name`, roleID)
	if err != nil {
		respondInternalError(w, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	envs := []string{}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		envs = append(envs, name)
	}
	respondSuccess(w, envs)
}

// HandleUpdateRoleDeployEnvs PUT /api/admin/role-deploy-envs/{roleID}
//
//	整体覆盖某角色的 env 访问权：body = {"env_names": ["g32-uat", "g50-uat", ...]}
func HandleUpdateRoleDeployEnvs(w http.ResponseWriter, r *http.Request) {
	roleID := mux.Vars(r)["roleID"]
	var body struct {
		EnvNames []string `json:"env_names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	tx, err := database.DB.Begin()
	if err != nil {
		respondInternalError(w, "开启事务失败")
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM role_deploy_envs WHERE role_id = ?`, roleID); err != nil {
		respondInternalError(w, "清空旧记录失败: "+err.Error())
		return
	}
	for _, name := range body.EnvNames {
		if name == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT IGNORE INTO role_deploy_envs (role_id, env_name) VALUES (?, ?)`,
			roleID, name); err != nil {
			respondInternalError(w, "插入失败: "+err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, "提交事务失败")
		return
	}

	_, username, _ := GetUserFromContext(r)
	log.Printf("[DeployEnvs] role=%s updated by %s, envs=%v", roleID, username, body.EnvNames)
	respondSuccess(w, map[string]interface{}{
		"role_id":   roleID,
		"env_names": body.EnvNames,
	})
}

// HandleGetMyDeployEnvs GET /api/my/deploy-envs
//
//	发布中心 portal-auth 时调这个接口，拿当前登录用户通过角色关联的 env 白名单。
//	非 admin 用户走正常角色关联查；admin 返回空 + flag "admin=true"，发布中心
//	看到 admin=true 就跳过过滤。
//
//	AND 语义：返回的 env_names 必须同时满足 (env 在 role_deploy_envs ∪) AND
//	(env 所属 project 在 role_deploy_projects ∪)。env 勾了但项目没勾 → 不返回。
func HandleGetMyDeployEnvs(w http.ResponseWriter, r *http.Request) {
	_, username, role := GetUserFromContext(r)
	if username == "" {
		respondUnauthorized(w, "未登录")
		return
	}

	// 管理员 bypass
	if role == "admin" || role == "super_admin" || role == "超级管理员" {
		respondSuccess(w, map[string]interface{}{
			"admin":     true,
			"env_names": []string{},
		})
		return
	}

	// 查用户的角色
	var userID string
	if err := database.DB.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&userID); err != nil || userID == "" {
		respondSuccess(w, map[string]interface{}{"admin": false, "env_names": []string{}})
		return
	}

	// AND 语义：env_name 必须在 role_deploy_envs 里，且其推导出来的项目名（去 -uat/-prod 后缀）
	// 必须在该用户所有角色的 role_deploy_projects 里。子查询拿用户允许的项目集合。
	rows, err := database.DB.Query(`
		SELECT DISTINCT rde.env_name
		  FROM role_deploy_envs rde
		  INNER JOIN user_roles ur ON ur.role_id = rde.role_id
		 WHERE ur.user_id = ?
		   AND REGEXP_REPLACE(rde.env_name, '-(uat|prod|lpt)$', '') IN (
		     SELECT DISTINCT rdp.project_name
		       FROM role_deploy_projects rdp
		       INNER JOIN user_roles ur2 ON ur2.role_id = rdp.role_id
		      WHERE ur2.user_id = ?
		   )
		 ORDER BY rde.env_name`, userID, userID)
	if err != nil {
		log.Printf("[DeployEnvs] query failed: %v", err)
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	envs := []string{}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		envs = append(envs, name)
	}
	respondSuccess(w, map[string]interface{}{
		"admin":     false,
		"env_names": envs,
	})
}

// HandleGetRoleDeployProjects GET /api/admin/role-deploy-projects/{roleID}
//
//	返回某角色被授权访问的项目名列表（管理页勾选状态回显）。
func HandleGetRoleDeployProjects(w http.ResponseWriter, r *http.Request) {
	roleID := mux.Vars(r)["roleID"]
	rows, err := database.DB.Query(
		`SELECT project_name FROM role_deploy_projects WHERE role_id = ? ORDER BY project_name`, roleID)
	if err != nil {
		respondInternalError(w, "查询失败: "+err.Error())
		return
	}
	defer rows.Close()

	projects := []string{}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		projects = append(projects, name)
	}
	respondSuccess(w, projects)
}

// HandleUpdateRoleDeployProjects PUT /api/admin/role-deploy-projects/{roleID}
//
//	整体覆盖某角色的项目访问权：body = {"project_names": ["g32", "g50", ...]}
func HandleUpdateRoleDeployProjects(w http.ResponseWriter, r *http.Request) {
	roleID := mux.Vars(r)["roleID"]
	var body struct {
		ProjectNames []string `json:"project_names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	tx, err := database.DB.Begin()
	if err != nil {
		respondInternalError(w, "开启事务失败")
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM role_deploy_projects WHERE role_id = ?`, roleID); err != nil {
		respondInternalError(w, "清空旧记录失败: "+err.Error())
		return
	}
	for _, name := range body.ProjectNames {
		if name == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT IGNORE INTO role_deploy_projects (role_id, project_name) VALUES (?, ?)`,
			roleID, name); err != nil {
			respondInternalError(w, "插入失败: "+err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		respondInternalError(w, "提交事务失败")
		return
	}

	_, username, _ := GetUserFromContext(r)
	log.Printf("[DeployProjects] role=%s updated by %s, projects=%v", roleID, username, body.ProjectNames)
	respondSuccess(w, map[string]interface{}{
		"role_id":       roleID,
		"project_names": body.ProjectNames,
	})
}

// HandleProxyDeployCenterProjects GET /api/admin/deploy-center-projects
//
//	代理调发布中心的 GET /api/projects 拿实时项目列表。同 HandleProxyDeployCenterEnvs
//	一样走 X-Internal-Token 鉴权。
func HandleProxyDeployCenterProjects(w http.ResponseWriter, r *http.Request) {
	baseURL := os.Getenv("DEPLOY_CENTER_INTERNAL_URL")
	if baseURL == "" {
		_ = database.DB.QueryRow(
			`SELECT url FROM external_apps WHERE app_key = 'deploy_center' LIMIT 1`).Scan(&baseURL)
	}
	if baseURL == "" {
		respondInternalError(w, "deploy_center 内网 URL 未配置")
		return
	}
	internalToken := getDeployCenterInternalToken()

	client := newDefaultHTTPClient()
	req, _ := http.NewRequest("GET", baseURL+"/api/public/projects", nil)
	req.Header.Set("X-Internal-Token", internalToken)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[DeployProjects] proxy call %s failed: %v", baseURL, err)
		respondInternalError(w, "调发布中心失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	copyBody(resp.Body, w)
}

// HandleProxyDeployCenterEnvs GET /api/admin/deploy-center-envs
//
//	代理调发布中心的 GET /api/public/project-envs 拿实时 env 列表。
//
// URL 来源优先级：
//  1. env DEPLOY_CENTER_INTERNAL_URL（集群内 Service URL，如 http://opsplatform-deploy-backend:8080）
//  2. external_apps.url（前端外网 URL，从 pod 内可能不通；只是兜底）
func HandleProxyDeployCenterEnvs(w http.ResponseWriter, r *http.Request) {
	baseURL := os.Getenv("DEPLOY_CENTER_INTERNAL_URL")
	if baseURL == "" {
		_ = database.DB.QueryRow(
			`SELECT url FROM external_apps WHERE app_key = 'deploy_center' LIMIT 1`).Scan(&baseURL)
	}
	if baseURL == "" {
		respondInternalError(w, "deploy_center 内网 URL 未配置（DEPLOY_CENTER_INTERNAL_URL 或 external_apps）")
		return
	}
	internalToken := getDeployCenterInternalToken()

	client := newDefaultHTTPClient()
	req, _ := http.NewRequest("GET", baseURL+"/api/public/project-envs", nil)
	req.Header.Set("X-Internal-Token", internalToken)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[DeployEnvs] proxy call %s failed: %v", baseURL, err)
		respondInternalError(w, "调发布中心失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	copyBody(resp.Body, w)
}
