package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

// =========================================================================
//   deploy_agent CRUD —— VM agent 元数据（仿 argocd_instance）
//
// agent 跑在 ansible 控制机上，deploy-center 通过 url + token 调它的 HTTP API
// 触发 ansible 任务。token 加密存 DB；返回时脱敏成 ••••••••（同 lark/argocd 模式）
// =========================================================================

type deployAgentReq struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Token       string `json:"token"`
	Description string `json:"description"`
}

// GET /api/deploy-agents
func HandleListDeployAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, url, IFNULL(description,''), created_at, updated_at FROM deploy_agent ORDER BY id`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	out := []models.DeployAgent{}
	for rows.Next() {
		var a models.DeployAgent
		_ = rows.Scan(&a.ID, &a.Name, &a.URL, &a.Description, &a.CreatedAt, &a.UpdatedAt)
		a.Token = maskedTokenString // 列表脱敏
		out = append(out, a)
	}
	JSONSuccess(w, out)
}

// POST /api/deploy-agents
func HandleCreateDeployAgent(w http.ResponseWriter, r *http.Request) {
	var req deployAgentReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.URL == "" || req.Token == "" {
		JSONError(w, 40001, "name / url / token 必填")
		return
	}
	enc, err := crypto.Encrypt(req.Token)
	if err != nil {
		JSONError(w, 50000, "encrypt: "+err.Error())
		return
	}
	res, err := database.DB.Exec(
		`INSERT INTO deploy_agent (name, url, token, description) VALUES (?, ?, ?, ?)`,
		req.Name, req.URL, enc, req.Description)
	if err != nil {
		JSONError(w, 40001, "create: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "deploy_agent.create", "deploy_agent", req.Name, map[string]interface{}{"url": req.URL})
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/deploy-agents/{id}
func HandleUpdateDeployAgent(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req deployAgentReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	sets := []string{"url=?", "description=?"}
	args := []interface{}{req.URL, req.Description}
	if req.Name != "" {
		sets = append(sets, "name=?")
		args = append(args, req.Name)
	}
	// token 非 mask 占位才更新
	if req.Token != "" && req.Token != maskedTokenString {
		enc, err := crypto.Encrypt(req.Token)
		if err != nil {
			JSONError(w, 50000, "encrypt: "+err.Error())
			return
		}
		sets = append(sets, "token=?")
		args = append(args, enc)
	}
	args = append(args, id)
	q := "UPDATE deploy_agent SET " + joinComma(sets) + " WHERE id=?"
	if _, err := database.DB.Exec(q, args...); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "deploy_agent.update", "deploy_agent", strconv.FormatInt(id, 10), auditDetailFromReq(req))
	JSONSuccess(w, nil)
}

// DELETE /api/deploy-agents/{id}
func HandleDeleteDeployAgent(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	// 防误删：被 vm_project_env 引用就拒绝
	var refCount int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM vm_project_env WHERE agent_id=?`, id).Scan(&refCount)
	if refCount > 0 {
		JSONError(w, 40900, "agent 被 "+strconv.Itoa(refCount)+" 个 VM 项目环境引用，先删那些再删 agent")
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM deploy_agent WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "deploy_agent.delete", "deploy_agent", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// POST /api/deploy-agents/test
// body { id?, url?, token? }
// id 有 + 字段空 → 用 DB fallback；保存后/未保存都能测
func HandleTestDeployAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    *int64 `json:"id"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	url, token := strings.TrimSpace(req.URL), req.Token
	if req.ID != nil && *req.ID > 0 {
		if a, err := LoadDeployAgentDecrypted(*req.ID); err == nil {
			// 🔒 安全：改了 url（可能指向攻击者主机）必须重新提供 token，
			// 否则 DB 真 token 以 Bearer 头打到攻击者 → VM agent token 等同 ansible/root 权限直接外泄。
			urlChanged := url != "" && url != a.URL
			if urlChanged && token == "" {
				JSONError(w, 40000, "修改了 url 时必须重新提供 token（防止 VM agent token 外泄到不可信地址）")
				return
			}
			if url == "" {
				url = a.URL
			}
			if token == "" {
				token = a.Token
			}
		}
	}
	if url == "" || token == "" {
		JSONError(w, 40001, "url 和 token 必填")
		return
	}
	c := services.NewVmAgentClient(url, token)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	h, err := c.Health(ctx)
	if err != nil {
		JSONError(w, 50002, "agent health: "+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{
		"ok":             true,
		"status":         h.Status,
		"version":        h.Version,
		"ansible_root":   h.AnsibleRoot,
		"version_root":   h.VersionRoot,
		"max_concurrent": h.MaxConcurrent,
	})
}

// LoadDeployAgentDecrypted 内部使用：按 id 查 agent 并解密 token
func LoadDeployAgentDecrypted(id int64) (*models.DeployAgent, error) {
	var a models.DeployAgent
	var encToken string
	err := database.DB.QueryRow(
		`SELECT id, name, url, token, IFNULL(description,''), created_at, updated_at FROM deploy_agent WHERE id=?`,
		id).Scan(&a.ID, &a.Name, &a.URL, &encToken, &a.Description, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	tok, err := crypto.Decrypt(encToken)
	if err != nil {
		return nil, err
	}
	a.Token = tok
	return &a, nil
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
