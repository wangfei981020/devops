package handlers

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

func HandleListProjectEnvs(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, display_name, env_type, git_repo, git_branch,
		chart_base_path, namespace, argocd_url, argocd_token, lark_webhook, lark_secret, auto_sync, created_at, updated_at
		FROM project_env ORDER BY name`)
	if err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	defer rows.Close()
	list := []models.ProjectEnv{}
	for rows.Next() {
		var p models.ProjectEnv
		_ = rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.EnvType, &p.GitRepo, &p.GitBranch,
			&p.ChartBasePath, &p.Namespace, &p.ArgocdURL, &p.ArgocdToken, &p.LarkWebhook, &p.LarkSecret,
			&p.AutoSync, &p.CreatedAt, &p.UpdatedAt)
		p.ArgocdToken = maskToken(p.ArgocdToken)
		p.LarkSecret = maskToken(p.LarkSecret)
		list = append(list, p)
	}
	JSONSuccess(w, list)
}

func HandleGetProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	p, err := loadProjectEnvMasked(id)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	JSONSuccess(w, p)
}

type projectEnvReq struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	EnvType       string `json:"env_type"`
	GitRepo       string `json:"git_repo"`
	GitBranch     string `json:"git_branch"`
	ChartBasePath string `json:"chart_base_path"`
	Namespace     string `json:"namespace"`
	ArgocdURL     string `json:"argocd_url"`
	ArgocdToken   string `json:"argocd_token"`
	LarkWebhook   string `json:"lark_webhook"`
	LarkSecret    string `json:"lark_secret"`
	AutoSync      int    `json:"auto_sync"`
}

func HandleCreateProjectEnv(w http.ResponseWriter, r *http.Request) {
	var req projectEnvReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		JSONError(w, 40001, "name 必填")
		return
	}
	if req.EnvType != models.EnvUAT && req.EnvType != models.EnvPROD {
		JSONError(w, 40001, "env_type 必须是 uat 或 prod")
		return
	}
	if req.GitBranch == "" {
		req.GitBranch = "main"
	}
	if req.EnvType == models.EnvPROD {
		req.AutoSync = 0
	}
	encArgoToken, _ := crypto.Encrypt(req.ArgocdToken)
	encLarkSecret, _ := crypto.Encrypt(req.LarkSecret)
	res, err := database.DB.Exec(`INSERT INTO project_env
		(name, display_name, env_type, git_repo, git_branch, chart_base_path, namespace, argocd_url, argocd_token,
		 lark_webhook, lark_secret, auto_sync) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.Name, req.DisplayName, req.EnvType, req.GitRepo, req.GitBranch, req.ChartBasePath,
		req.Namespace, req.ArgocdURL, encArgoToken, req.LarkWebhook, encLarkSecret, req.AutoSync)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		JSONError(w, 50000, err.Error())
		return
	}
	id, _ := res.LastInsertId()
	JSONSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req projectEnvReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.EnvType == models.EnvPROD {
		req.AutoSync = 0
	}
	sets := []string{"display_name=?", "env_type=?", "git_repo=?", "git_branch=?", "chart_base_path=?",
		"namespace=?", "argocd_url=?", "lark_webhook=?", "auto_sync=?"}
	args := []interface{}{req.DisplayName, req.EnvType, req.GitRepo, req.GitBranch, req.ChartBasePath,
		req.Namespace, req.ArgocdURL, req.LarkWebhook, req.AutoSync}
	if req.ArgocdToken != "" {
		enc, _ := crypto.Encrypt(req.ArgocdToken)
		sets = append(sets, "argocd_token=?")
		args = append(args, enc)
	}
	if req.LarkSecret != "" {
		enc, _ := crypto.Encrypt(req.LarkSecret)
		sets = append(sets, "lark_secret=?")
		args = append(args, enc)
	}
	args = append(args, id)
	q := "UPDATE project_env SET " + joinComma(sets) + " WHERE id=?"
	if _, err := database.DB.Exec(q, args...); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

func HandleDeleteProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	_, _ = database.DB.Exec(`DELETE FROM module WHERE project_env_id=?`, id)
	if _, err := database.DB.Exec(`DELETE FROM project_env WHERE id=?`, id); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

func HandleTestProjectEnvGit(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	p, err := LoadProjectEnvDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	var user, encToken string
	_ = database.DB.QueryRow(`SELECT gitlab_user, gitlab_token FROM global_config WHERE id=1`).
		Scan(&user, &encToken)
	token, _ := crypto.Decrypt(encToken)
	if user == "" || token == "" {
		JSONError(w, 40001, "global gitlab_user/token 未配置")
		return
	}
	authURL := injectTokenHelper(p.GitRepo, user, token)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "ls-remote", "--heads", authURL, p.GitBranch).CombinedOutput()
	if err != nil {
		JSONError(w, 50001, "ls-remote: "+string(out))
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "ref": string(out)})
}

func HandleTestProjectEnvArgocd(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	p, err := LoadProjectEnvDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	if p.ArgocdURL == "" || p.ArgocdToken == "" {
		JSONError(w, 40001, "argocd_url/token 未配置")
		return
	}
	c := services.NewArgocdClient(p.ArgocdURL, p.ArgocdToken)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	version, err := c.Ping(ctx)
	if err != nil {
		JSONError(w, 50002, "argocd ping: "+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "version": version})
}

// --- scan modules ---

var gitSvc *services.GitService

func getGitService() *services.GitService {
	if gitSvc != nil {
		return gitSvc
	}
	var user, email string
	_ = database.DB.QueryRow(`SELECT gitlab_user, gitlab_email FROM global_config WHERE id=1`).
		Scan(&user, &email)
	gitSvc = services.NewGitService(Cfg.GitCacheDir, user, email, func() string {
		var t string
		_ = database.DB.QueryRow(`SELECT gitlab_token FROM global_config WHERE id=1`).Scan(&t)
		dec, _ := crypto.Decrypt(t)
		return dec
	})
	return gitSvc
}

func HandleScanModules(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	p, err := LoadProjectEnvDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	gs := getGitService()
	gs.Locks.Acquire(p.Name)
	defer gs.Locks.Release(p.Name)

	ctx, cancel := services.GitCtx(r.Context(), 60)
	defer cancel()
	if err := gs.EnsureClone(ctx, p.Name, p.GitRepo, p.GitBranch); err != nil {
		JSONError(w, 50001, "git: "+err.Error())
		return
	}
	chartDir := gs.RepoPath(p.Name) + "/" + p.ChartBasePath
	scanned, err := services.ScanModulesFromDir(chartDir)
	if err != nil {
		JSONError(w, 50000, "scan: "+err.Error())
		return
	}
	// Upsert to DB
	existing := map[string]int64{}
	rows, _ := database.DB.Query(`SELECT id, name FROM module WHERE project_env_id=?`, id)
	for rows.Next() {
		var mid int64
		var mname string
		_ = rows.Scan(&mid, &mname)
		existing[mname] = mid
	}
	rows.Close()
	seen := map[string]bool{}
	for _, s := range scanned {
		seen[s.Name] = true
		appName := s.Name + "-" + p.Name
		if mid, ok := existing[s.Name]; ok {
			_, _ = database.DB.Exec(`UPDATE module SET current_tag=?, image_repository=?, argocd_app_name=?, last_scanned_at=NOW() WHERE id=?`,
				s.CurrentTag, s.ImageRepository, appName, mid)
		} else {
			_, _ = database.DB.Exec(`INSERT INTO module (project_env_id, name, current_tag, image_repository, argocd_app_name, last_scanned_at)
				VALUES (?, ?, ?, ?, ?, NOW())`,
				id, s.Name, s.CurrentTag, s.ImageRepository, appName)
		}
	}
	for name, mid := range existing {
		if !seen[name] {
			_, _ = database.DB.Exec(`DELETE FROM module WHERE id=?`, mid)
		}
	}
	JSONSuccess(w, map[string]interface{}{"count": len(scanned)})
}

// --- load helpers ---

func loadProjectEnvMasked(id int64) (*models.ProjectEnv, error) {
	p, err := loadProjectEnvRaw(id)
	if err != nil {
		return nil, err
	}
	p.ArgocdToken = maskToken(p.ArgocdToken)
	p.LarkSecret = maskToken(p.LarkSecret)
	return p, nil
}

func loadProjectEnvRaw(id int64) (*models.ProjectEnv, error) {
	var p models.ProjectEnv
	err := database.DB.QueryRow(`SELECT id, name, display_name, env_type, git_repo, git_branch,
		chart_base_path, namespace, argocd_url, argocd_token, lark_webhook, lark_secret, auto_sync, created_at, updated_at
		FROM project_env WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.DisplayName, &p.EnvType, &p.GitRepo, &p.GitBranch,
			&p.ChartBasePath, &p.Namespace, &p.ArgocdURL, &p.ArgocdToken, &p.LarkWebhook, &p.LarkSecret,
			&p.AutoSync, &p.CreatedAt, &p.UpdatedAt)
	return &p, err
}

// LoadProjectEnvDecrypted 返回解密后的 token（内部使用）
func LoadProjectEnvDecrypted(id int64) (*models.ProjectEnv, error) {
	p, err := loadProjectEnvRaw(id)
	if err != nil {
		return nil, err
	}
	p.ArgocdToken, _ = crypto.Decrypt(p.ArgocdToken)
	p.LarkSecret, _ = crypto.Decrypt(p.LarkSecret)
	return p, nil
}
