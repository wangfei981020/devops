package handlers

import (
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleListEnvironments(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, display_name, auto_sync,
		IFNULL(argocd_url,''), IFNULL(argocd_token,''), IFNULL(description,''),
		sort_order, created_at, updated_at FROM environments ORDER BY sort_order, id`)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	defer rows.Close()

	list := []models.Environment{}
	for rows.Next() {
		var e models.Environment
		rows.Scan(&e.ID, &e.Name, &e.DisplayName, &e.AutoSync,
			&e.ArgocdURL, &e.ArgocdToken, &e.Description,
			&e.SortOrder, &e.CreatedAt, &e.UpdatedAt)
		// token 脱敏
		if e.ArgocdToken != "" {
			if plain, err := crypto.Decrypt(e.ArgocdToken); err == nil && plain != "" {
				e.ArgocdToken = crypto.Mask(plain)
			}
		}
		list = append(list, e)
	}
	jsonSuccess(w, list)
}

func HandleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		AutoSync    int    `json:"auto_sync"`
		ArgocdURL   string `json:"argocd_url"`
		ArgocdToken string `json:"argocd_token"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}
	if req.Name == "" {
		jsonError(w, 40001, "环境名不能为空")
		return
	}

	encryptedToken := ""
	if req.ArgocdToken != "" && !isMasked(req.ArgocdToken) {
		v, err := crypto.Encrypt(req.ArgocdToken)
		if err != nil {
			jsonError(w, 50000, "加密失败: "+err.Error())
			return
		}
		encryptedToken = v
	}

	res, err := database.DB.Exec(`INSERT INTO environments
		(name, display_name, auto_sync, argocd_url, argocd_token, description, sort_order)
		VALUES (?,?,?,?,?,?,?)`,
		req.Name, req.DisplayName, req.AutoSync, req.ArgocdURL, encryptedToken, req.Description, req.SortOrder)
	if err != nil {
		jsonError(w, 40900, "创建失败: "+err.Error())
		return
	}
	id, _ := res.LastInsertId()
	jsonSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var req struct {
		DisplayName string `json:"display_name"`
		AutoSync    int    `json:"auto_sync"`
		ArgocdURL   string `json:"argocd_url"`
		ArgocdToken string `json:"argocd_token"`
		Description string `json:"description"`
		SortOrder   int    `json:"sort_order"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}

	// token: 空或脱敏 → 不更新; 否则加密
	if req.ArgocdToken == "" || isMasked(req.ArgocdToken) {
		_, err := database.DB.Exec(`UPDATE environments SET display_name=?, auto_sync=?, argocd_url=?, description=?, sort_order=? WHERE id=?`,
			req.DisplayName, req.AutoSync, req.ArgocdURL, req.Description, req.SortOrder, id)
		if err != nil {
			jsonError(w, 50000, err.Error())
			return
		}
	} else {
		encToken, err := crypto.Encrypt(req.ArgocdToken)
		if err != nil {
			jsonError(w, 50000, "加密失败: "+err.Error())
			return
		}
		_, err = database.DB.Exec(`UPDATE environments SET display_name=?, auto_sync=?, argocd_url=?, argocd_token=?, description=?, sort_order=? WHERE id=?`,
			req.DisplayName, req.AutoSync, req.ArgocdURL, encToken, req.Description, req.SortOrder, id)
		if err != nil {
			jsonError(w, 50000, err.Error())
			return
		}
	}
	jsonSuccess(w, nil)
}

func HandleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	var c int
	database.DB.QueryRow(`SELECT COUNT(*) FROM project_envs WHERE env_id=?`, id).Scan(&c)
	if c > 0 {
		jsonError(w, 40900, "环境被项目使用中，无法删除")
		return
	}
	_, err := database.DB.Exec(`DELETE FROM environments WHERE id=?`, id)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

// HandleTestEnvironmentArgocd 测试该环境 ArgoCD 连通性
func HandleTestEnvironmentArgocd(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "环境 ArgoCD 连通性测试待实现 (阶段2)"})
}
