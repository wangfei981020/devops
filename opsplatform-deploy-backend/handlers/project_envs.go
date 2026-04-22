package handlers

import (
	"context"
	"fmt"
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
		chart_base_path, namespace, IFNULL(argocd_url,''), IFNULL(argocd_token,''), argocd_instance_id,
		lark_webhook, lark_secret, lark_bot_id, auto_sync, created_at, updated_at
		FROM project_env ORDER BY name`)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	defer rows.Close()
	list := []models.ProjectEnv{}
	for rows.Next() {
		var p models.ProjectEnv
		_ = rows.Scan(&p.ID, &p.Name, &p.DisplayName, &p.EnvType, &p.GitRepo, &p.GitBranch,
			&p.ChartBasePath, &p.Namespace, &p.ArgocdURL, &p.ArgocdToken, &p.ArgocdInstanceID,
			&p.LarkWebhook, &p.LarkSecret, &p.LarkBotID, &p.AutoSync, &p.CreatedAt, &p.UpdatedAt)
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
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	EnvType          string `json:"env_type"`
	GitRepo          string `json:"git_repo"`
	GitBranch        string `json:"git_branch"`
	ChartBasePath    string `json:"chart_base_path"`
	Namespace        string `json:"namespace"`
	ArgocdInstanceID *int64 `json:"argocd_instance_id"`
	LarkBotID        *int64 `json:"lark_bot_id"`
	LarkWebhook      string `json:"lark_webhook"`
	LarkSecret       string `json:"lark_secret"`
	AutoSync         int    `json:"auto_sync"`
}

func HandleCreateProjectEnv(w http.ResponseWriter, r *http.Request) {
	var req projectEnvReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	if err := ValidateName(req.Name); err != nil {
		JSONError(w, 40001, "name: "+err.Error())
		return
	}
	if req.EnvType != models.EnvUAT && req.EnvType != models.EnvPROD {
		JSONError(w, 40001, "env_type 必须是 uat 或 prod")
		return
	}
	if err := ValidateGitRepoURL(req.GitRepo); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	if cb, err := CleanRelPath(req.ChartBasePath); err != nil {
		JSONError(w, 40001, "chart_base_path: "+err.Error())
		return
	} else {
		req.ChartBasePath = cb
	}
	if req.GitBranch == "" {
		req.GitBranch = "main"
	}
	if req.EnvType == models.EnvPROD {
		req.AutoSync = 0
	}
	encLarkSecret, _ := crypto.Encrypt(req.LarkSecret)
	res, err := database.DB.Exec(`INSERT INTO project_env
		(name, display_name, env_type, git_repo, git_branch, chart_base_path, namespace, argocd_instance_id,
		 lark_bot_id, lark_webhook, lark_secret, auto_sync) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.Name, req.DisplayName, req.EnvType, req.GitRepo, req.GitBranch, req.ChartBasePath,
		req.Namespace, req.ArgocdInstanceID, req.LarkBotID, req.LarkWebhook, encLarkSecret, req.AutoSync)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		InternalErr(w, r, err)
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
	if err := ValidateGitRepoURL(req.GitRepo); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	if cb, err := CleanRelPath(req.ChartBasePath); err != nil {
		JSONError(w, 40001, "chart_base_path: "+err.Error())
		return
	} else {
		req.ChartBasePath = cb
	}
	sets := []string{"display_name=?", "env_type=?", "git_repo=?", "git_branch=?", "chart_base_path=?",
		"namespace=?", "argocd_instance_id=?", "lark_bot_id=?", "lark_webhook=?", "auto_sync=?"}
	args := []interface{}{req.DisplayName, req.EnvType, req.GitRepo, req.GitBranch, req.ChartBasePath,
		req.Namespace, req.ArgocdInstanceID, req.LarkBotID, req.LarkWebhook, req.AutoSync}
	if req.LarkSecret != "" {
		enc, _ := crypto.Encrypt(req.LarkSecret)
		sets = append(sets, "lark_secret=?")
		args = append(args, enc)
	}
	args = append(args, id)
	q := "UPDATE project_env SET " + strings.Join(sets, ",") + " WHERE id=?"
	if _, err := database.DB.Exec(q, args...); err != nil {
		InternalErr(w, r, err)
		return
	}
	JSONSuccess(w, nil)
}

func HandleDeleteProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	_, _ = database.DB.Exec(`DELETE FROM module WHERE project_env_id=?`, id)
	if _, err := database.DB.Exec(`DELETE FROM project_env WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
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
		JSONError(w, 50001, "ls-remote: "+services.ScrubSecrets(out))
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "ref": services.ScrubSecrets(out)})
}

func HandleTestProjectEnvArgocd(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	p, err := LoadProjectEnvDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	argoURL, argoToken, err := ResolveArgocdForEnv(p)
	if err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	c := services.NewArgocdClient(argoURL, argoToken)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	version, err := c.Ping(ctx)
	if err != nil {
		JSONError(w, 50002, "argocd ping: "+err.Error())
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "version": version})
}

// ResolveArgocdForEnv 返回 project_env 实际使用的 ArgoCD URL+Token
// 优先用 argocd_instance_id 引用的实例，否则回退到遗留字段
func ResolveArgocdForEnv(p *models.ProjectEnv) (url, token string, err error) {
	if p.ArgocdInstanceID != nil && *p.ArgocdInstanceID > 0 {
		inst, e := LoadArgocdInstanceDecrypted(*p.ArgocdInstanceID)
		if e != nil {
			return "", "", fmt.Errorf("argocd_instance %d 找不到", *p.ArgocdInstanceID)
		}
		return inst.URL, inst.Token, nil
	}
	// 回退（遗留数据）
	if p.ArgocdURL != "" {
		return p.ArgocdURL, p.ArgocdToken, nil
	}
	return "", "", fmt.Errorf("未配置 ArgoCD 实例")
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
	count, err := ScanModulesByProjectEnvID(r.Context(), id)
	if err != nil {
		if err.Error() == "project_env not found" {
			JSONError(w, 40400, err.Error())
			return
		}
		InternalErr(w, r, err)
		return
	}
	JSONSuccess(w, map[string]interface{}{"count": count})
}

// ScanModulesByProjectEnvID 抽出的内部版本，给 scheduler 直接调用（跳过 HTTP 层）
func ScanModulesByProjectEnvID(ctx context.Context, id int64) (int, error) {
	p, err := LoadProjectEnvDecrypted(id)
	if err != nil {
		return 0, fmt.Errorf("project_env not found")
	}
	gs := getGitService()
	gs.Locks.Acquire(p.Name)
	defer gs.Locks.Release(p.Name)

	gitCtx, cancel := services.GitCtx(ctx, 60)
	defer cancel()
	if err := gs.EnsureClone(gitCtx, p.Name, p.GitRepo, p.GitBranch); err != nil {
		return 0, fmt.Errorf("git: %w", err)
	}
	chartDir := gs.RepoPath(p.Name) + "/" + p.ChartBasePath
	scanned, err := services.ScanModulesFromDir(chartDir)
	if err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}
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
		ns := s.Namespace
		if ns == "" {
			ns = p.Namespace
		}
		if mid, ok := existing[s.Name]; ok {
			_, _ = database.DB.Exec(`UPDATE module SET current_tag=?, image_repository=?, argocd_app_name=?, namespace=?, last_scanned_at=NOW() WHERE id=?`,
				s.CurrentTag, s.ImageRepository, appName, ns, mid)
		} else {
			_, _ = database.DB.Exec(`INSERT INTO module (project_env_id, name, current_tag, image_repository, argocd_app_name, namespace, last_scanned_at)
				VALUES (?, ?, ?, ?, ?, ?, NOW())`,
				id, s.Name, s.CurrentTag, s.ImageRepository, appName, ns)
		}
	}
	for name, mid := range existing {
		if !seen[name] {
			_, _ = database.DB.Exec(`DELETE FROM module WHERE id=?`, mid)
		}
	}
	return len(scanned), nil
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
		chart_base_path, namespace, IFNULL(argocd_url,''), IFNULL(argocd_token,''), argocd_instance_id,
		lark_webhook, lark_secret, lark_bot_id, auto_sync, created_at, updated_at
		FROM project_env WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.DisplayName, &p.EnvType, &p.GitRepo, &p.GitBranch,
			&p.ChartBasePath, &p.Namespace, &p.ArgocdURL, &p.ArgocdToken, &p.ArgocdInstanceID,
			&p.LarkWebhook, &p.LarkSecret, &p.LarkBotID, &p.AutoSync, &p.CreatedAt, &p.UpdatedAt)
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
