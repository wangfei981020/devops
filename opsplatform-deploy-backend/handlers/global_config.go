package handlers

import (
	"net/http"

	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleGetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var g models.GlobalConfig
	err := database.DB.QueryRow(`SELECT id, gitlab_url, IFNULL(gitlab_token,''), gitlab_user, gitlab_email,
		harbor_url, harbor_user, IFNULL(harbor_password,''), argocd_url, IFNULL(argocd_token,''), updated_at
		FROM global_config WHERE id=1`).
		Scan(&g.ID, &g.GitlabURL, &g.GitlabToken, &g.GitlabUser, &g.GitlabEmail,
			&g.HarborURL, &g.HarborUser, &g.HarborPassword, &g.ArgocdURL, &g.ArgocdToken, &g.UpdatedAt)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	// 脱敏: 解密后取掩码
	if g.GitlabToken != "" {
		if plain, _ := crypto.Decrypt(g.GitlabToken); plain != "" {
			g.GitlabToken = crypto.Mask(plain)
		}
	}
	if g.HarborPassword != "" {
		if plain, _ := crypto.Decrypt(g.HarborPassword); plain != "" {
			g.HarborPassword = crypto.Mask(plain)
		}
	}
	if g.ArgocdToken != "" {
		if plain, _ := crypto.Decrypt(g.ArgocdToken); plain != "" {
			g.ArgocdToken = crypto.Mask(plain)
		}
	}
	jsonSuccess(w, g)
}

func HandleUpdateGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GitlabURL      string `json:"gitlab_url"`
		GitlabToken    string `json:"gitlab_token"`
		GitlabUser     string `json:"gitlab_user"`
		GitlabEmail    string `json:"gitlab_email"`
		HarborURL      string `json:"harbor_url"`
		HarborUser     string `json:"harbor_user"`
		HarborPassword string `json:"harbor_password"`
		ArgocdURL      string `json:"argocd_url"`
		ArgocdToken    string `json:"argocd_token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, 40000, "无效的请求")
		return
	}

	// 加密敏感字段; 若提交的是脱敏占位 (****) 则不更新该字段
	gitlabToken := ""
	if req.GitlabToken != "" && !isMasked(req.GitlabToken) {
		v, err := crypto.Encrypt(req.GitlabToken)
		if err != nil {
			jsonError(w, 50000, "加密失败: "+err.Error())
			return
		}
		gitlabToken = v
	}
	harborPwd := ""
	if req.HarborPassword != "" && !isMasked(req.HarborPassword) {
		v, err := crypto.Encrypt(req.HarborPassword)
		if err != nil {
			jsonError(w, 50000, err.Error())
			return
		}
		harborPwd = v
	}
	argocdToken := ""
	if req.ArgocdToken != "" && !isMasked(req.ArgocdToken) {
		v, err := crypto.Encrypt(req.ArgocdToken)
		if err != nil {
			jsonError(w, 50000, err.Error())
			return
		}
		argocdToken = v
	}

	// 拼 SQL: 仅更新非空敏感字段, 其它字段总是更新
	q := `UPDATE global_config SET gitlab_url=?, gitlab_user=?, gitlab_email=?, harbor_url=?, harbor_user=?, argocd_url=?`
	args := []interface{}{req.GitlabURL, req.GitlabUser, req.GitlabEmail, req.HarborURL, req.HarborUser, req.ArgocdURL}
	if gitlabToken != "" {
		q += ", gitlab_token=?"
		args = append(args, gitlabToken)
	}
	if harborPwd != "" {
		q += ", harbor_password=?"
		args = append(args, harborPwd)
	}
	if argocdToken != "" {
		q += ", argocd_token=?"
		args = append(args, argocdToken)
	}
	q += " WHERE id=1"

	_, err := database.DB.Exec(q, args...)
	if err != nil {
		jsonError(w, 50000, err.Error())
		return
	}
	jsonSuccess(w, nil)
}

func isMasked(s string) bool {
	for i := 0; i < len(s)-3; i++ {
		if s[i] == '*' && s[i+1] == '*' && s[i+2] == '*' && s[i+3] == '*' {
			return true
		}
	}
	return false
}

func HandleTestGlobalGitlab(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "GitLab 连通性测试待实现"})
}

func HandleTestGlobalHarbor(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "Harbor 连通性测试待实现"})
}

func HandleTestGlobalArgocd(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{"status": "stub", "msg": "ArgoCD 连通性测试待实现"})
}
