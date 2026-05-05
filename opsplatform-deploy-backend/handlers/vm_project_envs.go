package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// =========================================================================
//   VM 项目环境 CRUD + 服务发现 + 版本查询代理
//
// VM 部署流程的"配置层" —— 跟 K8s project_env 平行的概念，但走 ansible/agent
// 链路。前端按 project.target_type 决定用哪一套 API。
// =========================================================================

type vmProjectEnvReq struct {
	ProjectID   int64  `json:"project_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	EnvType     string `json:"env_type"` // LPT / UAT / PROD
	AgentID     int64  `json:"agent_id"`
	AnsibleRoot string `json:"ansible_root"` // 留空走 /etc/ansible
	ProjectCode string `json:"project_code"` // G01 / G02
	LarkBotID   *int64 `json:"lark_bot_id"`  // 可选；NULL = 不发 Lark
}

// GET /api/vm-project-envs?project_id=<id>
//
//	可选按 project_id 过滤；不传则全列。
//	非 admin 用户按 env name 白名单过滤（admin 直通看全部）
func HandleListVmProjectEnvs(w http.ResponseWriter, r *http.Request) {
	q := `SELECT id, project_id, name, IFNULL(display_name,''), env_type, agent_id, ansible_root, project_code, lark_bot_id, created_at, updated_at FROM vm_project_env`
	conds := []string{}
	args := []interface{}{}
	if pidStr := r.URL.Query().Get("project_id"); pidStr != "" {
		conds = append(conds, "project_id=?")
		pid, _ := strconv.ParseInt(pidStr, 10, 64)
		args = append(args, pid)
	}
	// 数据级权限：非 admin 仅返回白名单内的 env
	if names, enforce := AllowedEnvNames(r); enforce {
		if len(names) == 0 {
			JSONSuccess(w, []models.VmProjectEnv{})
			return
		}
		ph := make([]string, len(names))
		for i, n := range names {
			ph[i] = "?"
			args = append(args, n)
		}
		conds = append(conds, "name IN ("+strings.Join(ph, ",")+")")
	}
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += ` ORDER BY name`
	rows, err := database.DB.Query(q, args...)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.VmProjectEnv{}
	for rows.Next() {
		var v models.VmProjectEnv
		_ = rows.Scan(&v.ID, &v.ProjectID, &v.Name, &v.DisplayName, &v.EnvType, &v.AgentID, &v.AnsibleRoot, &v.ProjectCode, &v.LarkBotID, &v.CreatedAt, &v.UpdatedAt)
		out = append(out, v)
	}
	JSONSuccess(w, out)
}

// GET /api/vm-project-envs/{id}
func HandleGetVmProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	// 🔒 安全：先 load 再校验权限，无权限时也返 404 防 ID 枚举（同 HandleGetProjectEnv 注释）
	v, err := loadVmProjectEnv(id)
	if err != nil || !IsVmEnvIDAllowed(r, id) {
		JSONError(w, 40400, "vm_project_env not found")
		return
	}
	JSONSuccess(w, v)
}

// POST /api/vm-project-envs
func HandleCreateVmProjectEnv(w http.ResponseWriter, r *http.Request) {
	var req vmProjectEnvReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if err := validateVmReq(&req); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	res, err := database.DB.Exec(
		`INSERT INTO vm_project_env (project_id, name, display_name, env_type, agent_id, ansible_root, project_code, lark_bot_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ProjectID, req.Name, req.DisplayName, req.EnvType, req.AgentID, req.AnsibleRoot, req.ProjectCode, req.LarkBotID)
	if err != nil {
		InternalErr(w, r, fmt.Errorf("create vm_project_env (name=%s): %w", req.Name, err))
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "vm_project_env.create", "vm_project_env", req.Name, auditDetailFromReq(req))
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/vm-project-envs/{id}
func HandleUpdateVmProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req vmProjectEnvReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if err := validateVmReq(&req); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	_, err := database.DB.Exec(
		`UPDATE vm_project_env SET display_name=?, env_type=?, agent_id=?, ansible_root=?, project_code=?, lark_bot_id=? WHERE id=?`,
		req.DisplayName, req.EnvType, req.AgentID, req.AnsibleRoot, req.ProjectCode, req.LarkBotID, id)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "vm_project_env.update", "vm_project_env", strconv.FormatInt(id, 10), auditDetailFromReq(req))
	JSONSuccess(w, nil)
}

// DELETE /api/vm-project-envs/{id}
func HandleDeleteVmProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	_, _ = database.DB.Exec(`DELETE FROM vm_service WHERE vm_project_env_id=?`, id)
	if _, err := database.DB.Exec(`DELETE FROM vm_project_env WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "vm_project_env.delete", "vm_project_env", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// POST /api/vm-project-envs/{id}/scan-services
//
//	调 agent /v1/services 拿当前 ansible 仓库里的 service 列表（playbook 文件 + inventory），
//	跟 vm_service 表做 upsert：新加的入库、删了的清掉。
//	幂等，可重复跑。
func HandleScanVmServices(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	if !IsVmEnvIDAllowed(r, id) {
		JSONError(w, 40300, "无权扫描该 VM 环境")
		return
	}
	v, err := loadVmProjectEnv(id)
	if err != nil {
		JSONError(w, 40400, "vm_project_env not found")
		return
	}
	agent, err := LoadDeployAgentDecrypted(v.AgentID)
	if err != nil {
		JSONError(w, 40001, "agent not found: "+err.Error())
		return
	}
	cli := services.NewVmAgentClient(agent.URL, agent.Token)
	// agent 端在扫盘前会先跑 git pull（默认行为，含 0/2/5s 智能退避重试，最坏 ~7s）
	// + 扫盘 ~1s + ListServices RPC 自身开销，留 60s 给整个链路足够裕度
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	svcs, err := cli.ListServices(ctx, v.ProjectCode, v.EnvType)
	if err != nil {
		InternalErr(w, r, fmt.Errorf("agent list services (env=%s): %w", v.Name, err))
		return
	}

	// 构建当前数据库里已存在的 service 名集合
	existing := map[string]int64{}
	rows, _ := database.DB.Query(`SELECT id, name FROM vm_service WHERE vm_project_env_id=?`, id)
	for rows.Next() {
		var sid int64
		var name string
		_ = rows.Scan(&sid, &name)
		existing[name] = sid
	}
	rows.Close()

	now := time.Now()
	seen := map[string]bool{}
	for _, s := range svcs {
		seen[s.Name] = true
		hostsJSON, _ := json.Marshal(s.Hosts)
		if sid, ok := existing[s.Name]; ok {
			_, _ = database.DB.Exec(
				`UPDATE vm_service SET ansible_group=?, hosts=?, last_scanned_at=? WHERE id=?`,
				s.AnsibleGroup, string(hostsJSON), now, sid)
		} else {
			_, _ = database.DB.Exec(
				`INSERT INTO vm_service (vm_project_env_id, name, ansible_group, hosts, last_scanned_at)
				 VALUES (?, ?, ?, ?, ?)`,
				id, s.Name, s.AnsibleGroup, string(hostsJSON), now)
		}
	}
	// 删掉数据库里有但 ansible 仓库已经没的（playbook 删了）
	for name, sid := range existing {
		if !seen[name] {
			_, _ = database.DB.Exec(`DELETE FROM vm_service WHERE id=?`, sid)
		}
	}

	Audit(r, "vm_project_env.scan_services", "vm_project_env", strconv.FormatInt(id, 10),
		map[string]interface{}{"count": len(svcs)})
	JSONSuccess(w, map[string]interface{}{
		"count":    len(svcs),
		"services": svcs,
	})
}

// GET /api/vm-project-envs/{id}/services
//
//	返回当前 DB 里登记的 vm_service 列表（不调 agent，纯查 DB）
//	前端列服务下拉用
func HandleListVmServices(w http.ResponseWriter, r *http.Request) {
	envID := ParseID(mux.Vars(r)["id"])
	if !IsVmEnvIDAllowed(r, envID) {
		JSONError(w, 40300, "无权访问该 VM 环境")
		return
	}
	rows, err := database.DB.Query(
		`SELECT id, vm_project_env_id, name, IFNULL(ansible_group,''), IFNULL(hosts,'[]'),
		        IFNULL(current_version,''), last_scanned_at, created_at, updated_at
		 FROM vm_service WHERE vm_project_env_id=? ORDER BY name`, envID)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.VmService{}
	for rows.Next() {
		var s models.VmService
		var hostsRaw string
		_ = rows.Scan(&s.ID, &s.VmProjectEnvID, &s.Name, &s.AnsibleGroup, &hostsRaw,
			&s.CurrentVersion, &s.LastScannedAt, &s.CreatedAt, &s.UpdatedAt)
		_ = json.Unmarshal([]byte(hostsRaw), &s.Hosts)
		out = append(out, s)
	}
	JSONSuccess(w, out)
}

// ---- helpers ----

func validateVmReq(req *vmProjectEnvReq) error {
	if req.ProjectID == 0 || req.Name == "" || req.EnvType == "" || req.AgentID == 0 || req.ProjectCode == "" {
		return fmt.Errorf("project_id / name / env_type / agent_id / project_code 必填")
	}
	if req.EnvType != "LPT" && req.EnvType != "UAT" && req.EnvType != "PROD" {
		return fmt.Errorf("env_type 必须是 LPT / UAT / PROD")
	}
	if req.AnsibleRoot == "" {
		req.AnsibleRoot = "/etc/ansible"
	}
	return nil
}

func loadVmProjectEnv(id int64) (*models.VmProjectEnv, error) {
	var v models.VmProjectEnv
	err := database.DB.QueryRow(
		`SELECT id, project_id, name, IFNULL(display_name,''), env_type, agent_id, ansible_root, project_code, lark_bot_id, created_at, updated_at
		 FROM vm_project_env WHERE id=?`, id).
		Scan(&v.ID, &v.ProjectID, &v.Name, &v.DisplayName, &v.EnvType, &v.AgentID, &v.AnsibleRoot, &v.ProjectCode, &v.LarkBotID, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
