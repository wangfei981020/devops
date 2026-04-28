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

// GET /api/argocd-instances
func HandleListArgocdInstances(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, url, token, description, created_at, updated_at FROM argocd_instance ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []models.ArgocdInstance{}
	for rows.Next() {
		var a models.ArgocdInstance
		_ = rows.Scan(&a.ID, &a.Name, &a.URL, &a.Token, &a.Description, &a.CreatedAt, &a.UpdatedAt)
		a.Token = maskToken(a.Token)
		list = append(list, a)
	}
	JSONSuccess(w, list)
}

type argoInstReq struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Token       string `json:"token"`
	Description string `json:"description"`
}

// POST /api/argocd-instances
func HandleCreateArgocdInstance(w http.ResponseWriter, r *http.Request) {
	var req argoInstReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	if err := ValidateName(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
		return
	}
	if req.URL == "" {
		JSONError(w, 40001, "url 必填")
		return
	}
	if req.Token == "" {
		JSONError(w, 40001, "token 必填")
		return
	}
	encToken, err := crypto.Encrypt(req.Token)
	if err != nil {
		JSONError(w, 50000, "encrypt: "+err.Error())
		return
	}
	res, err := database.DB.Exec(`INSERT INTO argocd_instance (name, url, token, description)
		VALUES (?, ?, ?, ?)`, req.Name, req.URL, encToken, strings.TrimSpace(req.Description))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "argocd_instance.create", "argocd_instance", req.Name, nil)
	JSONSuccess(w, map[string]interface{}{"id": id})
}

// PUT /api/argocd-instances/{id}
func HandleUpdateArgocdInstance(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req argoInstReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	sets := []string{"url=?", "description=?"}
	args := []interface{}{strings.TrimSpace(req.URL), strings.TrimSpace(req.Description)}
	if req.Token != "" {
		enc, err := crypto.Encrypt(req.Token)
		if err != nil {
			JSONError(w, 50000, "encrypt: "+err.Error())
			return
		}
		sets = append(sets, "token=?")
		args = append(args, enc)
	}
	args = append(args, id)
	q := "UPDATE argocd_instance SET " + strings.Join(sets, ",") + " WHERE id=?"
	if _, err := database.DB.Exec(q, args...); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "argocd_instance.update", "argocd_instance", strconv.FormatInt(id, 10), auditDetailFromReq(req))
	JSONSuccess(w, nil)
}

// DELETE /api/argocd-instances/{id}
// 删之前要检查有没有 project_env 在用
func HandleDeleteArgocdInstance(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM project_env WHERE argocd_instance_id=?`, id).Scan(&cnt)
	if cnt > 0 {
		JSONError(w, 40900, "还有 "+strconv.Itoa(cnt)+" 个项目环境使用此 ArgoCD 实例，不能删除")
		return
	}
	if _, err := database.DB.Exec(`DELETE FROM argocd_instance WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "argocd_instance.delete", "argocd_instance", strconv.FormatInt(id, 10), nil)
	JSONSuccess(w, nil)
}

// POST /api/argocd-instances/{id}/test
// （保留兼容）根据 ID 从 DB 读取测试
func HandleTestArgocdInstance(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	inst, err := LoadArgocdInstanceDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "argocd instance not found")
		return
	}
	doTestArgocd(w, r, inst.URL, inst.Token)
}

// POST /api/argocd-instances/test
// 新：前端 body 传 {id?, url?, token?} 测试连接。未保存时也能测。
// id 有 + 某字段空 → 用 DB 的值 fallback；纯 body 时全靠 body。
func HandleTestArgocdInstanceByBody(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    *int64 `json:"id"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	url, token := strings.TrimSpace(req.URL), req.Token
	// id 有则从 DB fallback 空字段
	if req.ID != nil && *req.ID > 0 {
		if inst, err := LoadArgocdInstanceDecrypted(*req.ID); err == nil {
			if url == "" {
				url = inst.URL
			}
			if token == "" {
				token = inst.Token
			}
		}
	}
	if url == "" || token == "" {
		JSONError(w, 40001, "url 和 token 必填")
		return
	}
	doTestArgocd(w, r, url, token)
}

func doTestArgocd(w http.ResponseWriter, r *http.Request, url, token string) {
	c := services.NewArgocdClient(url, token)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	version, err := c.Ping(ctx)
	if err != nil {
		JSONError(w, 50002, "argocd ping: "+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "version": version})
}

// LoadArgocdInstanceDecrypted 内部：加载并解密 token
func LoadArgocdInstanceDecrypted(id int64) (*models.ArgocdInstance, error) {
	var a models.ArgocdInstance
	err := database.DB.QueryRow(`SELECT id, name, url, token, description, created_at, updated_at
		FROM argocd_instance WHERE id=?`, id).
		Scan(&a.ID, &a.Name, &a.URL, &a.Token, &a.Description, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	a.Token, _ = crypto.Decrypt(a.Token)
	return &a, nil
}
