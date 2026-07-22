package handlers

import (
	"context"
	"database/sql"
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
	// 按用户白名单过滤；admin / fail-open 时 enforce=false 不加 WHERE
	allowedNames, enforce := AllowedEnvNames(r)
	baseSQL := `SELECT id, name, display_name, env_type, git_repo, git_branch,
		chart_base_path, namespace, IFNULL(argocd_url,''), IFNULL(argocd_token,''), argocd_instance_id,
		lark_webhook, lark_secret, lark_bot_id, gitlab_repo_id, auto_sync, IFNULL(ingress_gateway,''), IFNULL(harbor_project,''), IFNULL(domain_suffix,''), IFNULL(default_namespaces,''), IFNULL(zkv_secrets_path,''), IFNULL(at_lark_ids,''), IFNULL(secret_prefix,''), created_at, updated_at
		FROM project_env`
	var rows *sql.Rows
	var err error
	if !enforce {
		rows, err = database.DB.Query(baseSQL + " ORDER BY name")
	} else if len(allowedNames) == 0 {
		// 用户白名单为空：直接返回空列表（不必查 DB）
		JSONSuccess(w, []models.ProjectEnv{})
		return
	} else {
		// IN 占位
		ph := strings.Repeat("?,", len(allowedNames))
		ph = ph[:len(ph)-1]
		args := make([]interface{}, len(allowedNames))
		for i, n := range allowedNames {
			args[i] = n
		}
		rows, err = database.DB.Query(baseSQL+" WHERE name IN ("+ph+") ORDER BY name", args...)
	}
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
			&p.LarkWebhook, &p.LarkSecret, &p.LarkBotID, &p.GitlabRepoID, &p.AutoSync, &p.IngressGateway, &p.HarborProject, &p.DomainSuffix, &p.DefaultNamespaces, &p.ZkvSecretsPath, &p.AtLarkIDs, &p.SecretPrefix, &p.CreatedAt, &p.UpdatedAt)
		p.ArgocdToken = maskToken(p.ArgocdToken)
		p.LarkSecret = maskToken(p.LarkSecret)
		list = append(list, p)
	}
	JSONSuccess(w, list)
}

func HandleGetProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	// 🔒 安全：先 load 再校验权限，且无权限时也返 404 而非 403。
	// 之前先 403 再 404 导致攻击者可以按 ID 序列枚举存在性（403 = 存在但无权限，404 = 不存在）
	p, err := loadProjectEnvMasked(id)
	if err != nil || !IsEnvIDAllowed(r, id) {
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
	GitlabRepoID     *int64 `json:"gitlab_repo_id"`
	LarkWebhook      string `json:"lark_webhook"`
	LarkSecret       string `json:"lark_secret"`
	AutoSync         int    `json:"auto_sync"`
}

// applyGitlabRepo 如果 req 带 gitlab_repo_id 则从表里读 URL 和默认分支覆盖 GitRepo/GitBranch
func applyGitlabRepo(req *projectEnvReq) error {
	if req.GitlabRepoID == nil || *req.GitlabRepoID <= 0 {
		return nil
	}
	g, err := LoadGitlabRepo(*req.GitlabRepoID)
	if err != nil {
		return fmt.Errorf("gitlab_repo_id %d 找不到", *req.GitlabRepoID)
	}
	req.GitRepo = g.RepoURL
	if req.GitBranch == "" {
		req.GitBranch = g.DefaultBranch
	}
	return nil
}

// isConfiguredEnvType 校验 env_type 已在 deploy_environment（环境管理）中启用配置。
func isConfiguredEnvType(t string) bool {
	if t == "" {
		return false
	}
	var cnt int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM deploy_environment WHERE name=? AND enabled=1`, t).Scan(&cnt)
	return cnt > 0
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
	if !isConfiguredEnvType(req.EnvType) {
		JSONError(w, 40001, "env_type 未在「环境管理」中配置，请先添加该环境（dev/test/uat/prod…）")
		return
	}
	// gitlab_repo_id 选了则从表里复制 url/default_branch 到 req
	if err := applyGitlabRepo(&req); err != nil {
		JSONError(w, 40001, err.Error())
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
	// v129: PROD auto_sync 默认强制关，但 admin / 拥有 prod_auto_sync button 权限的用户可放开。
	// 之前是无条件 = 0；现在改成"无权限才 = 0"。
	if req.EnvType == models.EnvPROD && !IsAdmin(r) && !HasButton(r, "prod_auto_sync") {
		req.AutoSync = 0
	}
	encLarkSecret, _ := crypto.Encrypt(req.LarkSecret)
	res, err := database.DB.Exec(`INSERT INTO project_env
		(name, display_name, env_type, git_repo, git_branch, chart_base_path, namespace, argocd_instance_id,
		 lark_bot_id, gitlab_repo_id, lark_webhook, lark_secret, auto_sync) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		req.Name, req.DisplayName, req.EnvType, req.GitRepo, req.GitBranch, req.ChartBasePath,
		req.Namespace, req.ArgocdInstanceID, req.LarkBotID, req.GitlabRepoID, req.LarkWebhook, encLarkSecret, req.AutoSync)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			JSONError(w, 40900, "name 已存在")
			return
		}
		InternalErr(w, r, err)
		return
	}
	id, _ := res.LastInsertId()
	Audit(r, "project_env.create", "project_env", req.Name, auditDetailFromReq(req))
	JSONSuccess(w, map[string]interface{}{"id": id})
}

func HandleUpdateProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	var req projectEnvReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	// v129: PROD auto_sync 默认强制关，但 admin / 拥有 prod_auto_sync button 权限的用户可放开。
	// 之前是无条件 = 0；现在改成"无权限才 = 0"。
	if req.EnvType == models.EnvPROD && !IsAdmin(r) && !HasButton(r, "prod_auto_sync") {
		req.AutoSync = 0
	}
	// gitlab_repo_id 选了则从表里复制 url/default_branch 到 req
	if err := applyGitlabRepo(&req); err != nil {
		JSONError(w, 40001, err.Error())
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
	sets := []string{"display_name=?", "env_type=?", "git_repo=?", "git_branch=?", "chart_base_path=?",
		"namespace=?", "argocd_instance_id=?", "lark_bot_id=?", "gitlab_repo_id=?", "lark_webhook=?", "auto_sync=?"}
	args := []interface{}{req.DisplayName, req.EnvType, req.GitRepo, req.GitBranch, req.ChartBasePath,
		req.Namespace, req.ArgocdInstanceID, req.LarkBotID, req.GitlabRepoID, req.LarkWebhook, req.AutoSync}
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
	Audit(r, "project_env.update", "project_env", fmt.Sprintf("%d", id), auditDetailFromReq(req))
	JSONSuccess(w, nil)
}

func HandleDeleteProjectEnv(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	_, _ = database.DB.Exec(`DELETE FROM module WHERE project_env_id=?`, id)
	if _, err := database.DB.Exec(`DELETE FROM project_env WHERE id=?`, id); err != nil {
		InternalErr(w, r, err)
		return
	}
	Audit(r, "project_env.delete", "project_env", fmt.Sprintf("%d", id), nil)
	JSONSuccess(w, nil)
}

// POST /api/project-envs/{id}/test-git （保留兼容，按 ID 取 DB 值）
func HandleTestProjectEnvGit(w http.ResponseWriter, r *http.Request) {
	id := ParseID(mux.Vars(r)["id"])
	p, err := LoadProjectEnvDecrypted(id)
	if err != nil {
		JSONError(w, 40400, "project_env not found")
		return
	}
	doTestGit(w, r, p.GitRepo, p.GitBranch)
}

// POST /api/project-envs/test-git  新：body 方式，未保存也能测
// body = {git_repo, git_branch}
func HandleTestProjectEnvGitByBody(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GitRepo   string `json:"git_repo"`
		GitBranch string `json:"git_branch"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	repo := strings.TrimSpace(req.GitRepo)
	branch := strings.TrimSpace(req.GitBranch)
	if repo == "" {
		JSONError(w, 40001, "git_repo 必填")
		return
	}
	if branch == "" {
		branch = "main"
	}
	// 校验 host 必须跟全局 gitlab_url 一致（已有的输入校验）
	if err := ValidateGitRepoURL(repo); err != nil {
		JSONError(w, 40001, err.Error())
		return
	}
	doTestGit(w, r, repo, branch)
}

func doTestGit(w http.ResponseWriter, r *http.Request, repo, branch string) {
	var user, encToken string
	_ = database.DB.QueryRow(`SELECT gitlab_user, gitlab_token FROM global_config WHERE id=1`).
		Scan(&user, &encToken)
	token, _ := crypto.Decrypt(encToken)
	if user == "" || token == "" {
		JSONError(w, 40001, "global gitlab_user/token 未配置")
		return
	}
	authURL := injectTokenHelper(repo, user, token)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "ls-remote", "--heads", authURL, branch).CombinedOutput()
	if err != nil {
		JSONError(w, 50001, "ls-remote: "+services.ScrubSecrets(out))
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "ref": services.ScrubSecrets(out)})
}

// POST /api/project-envs/{id}/test-argocd （保留兼容）
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
	doTestArgocd(w, r, argoURL, argoToken)
}

// POST /api/project-envs/test-argocd  新：body = {argocd_instance_id}
// 前端传 argocd 实例 ID，后端查 DB 拿 URL+Token 测试。不需要环境本身保存。
func HandleTestProjectEnvArgocdByBody(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ArgocdInstanceID *int64 `json:"argocd_instance_id"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.ArgocdInstanceID == nil || *req.ArgocdInstanceID <= 0 {
		JSONError(w, 40001, "argocd_instance_id 必填")
		return
	}
	inst, err := LoadArgocdInstanceDecrypted(*req.ArgocdInstanceID)
	if err != nil {
		JSONError(w, 40400, "argocd_instance 找不到")
		return
	}
	doTestArgocd(w, r, inst.URL, inst.Token)
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
	if !IsEnvIDAllowed(r, id) {
		JSONError(w, 40300, "无权扫描该环境")
		return
	}
	count, err := ScanModulesByProjectEnvID(r.Context(), id)
	if err != nil {
		if err.Error() == "project_env not found" {
			JSONError(w, 40400, err.Error())
			return
		}
		InternalErr(w, r, err)
		return
	}
	Audit(r, "project_env.scan_modules", "project_env", fmt.Sprintf("%d", id), map[string]interface{}{"count": count})
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
	// ArgoCD app 命名按 k8s DNS-1123 全小写 kebab-case 惯例（helm 生成的 Application 资源强制小写），
	// 拼出的 app name 一律 ToLower 保证匹配。
	//
	// app name 后缀（service- 之后那段）有两种生产约定，跟着 git 上的 helm 模板走：
	//   - global.spec.project（老格式 g50/g33）→ "atmosphere-frontend-g50-uat"
	//   - global.env（新格式 g32）            → "atmosphere-frontend-uat"
	// ResolveAppNameSuffix 读 {chartBasePath}-apps/templates/applications.yaml 自动识别。
	// 未用 app-of-apps 模式 / 模板解析失败时回退 ToLower(p.Name)，跟以前行为一致。
	suffix := gs.ResolveAppNameSuffix(p.Name, p.ChartBasePath)
	for _, s := range scanned {
		seen[s.Name] = true
		appName := strings.ToLower(s.Name) + "-" + suffix
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
		lark_webhook, lark_secret, lark_bot_id, gitlab_repo_id, auto_sync, IFNULL(ingress_gateway,''), IFNULL(harbor_project,''), IFNULL(domain_suffix,''), IFNULL(default_namespaces,''), IFNULL(zkv_secrets_path,''), IFNULL(at_lark_ids,''), IFNULL(secret_prefix,''), created_at, updated_at
		FROM project_env WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.DisplayName, &p.EnvType, &p.GitRepo, &p.GitBranch,
			&p.ChartBasePath, &p.Namespace, &p.ArgocdURL, &p.ArgocdToken, &p.ArgocdInstanceID,
			&p.LarkWebhook, &p.LarkSecret, &p.LarkBotID, &p.GitlabRepoID, &p.AutoSync, &p.IngressGateway, &p.HarborProject, &p.DomainSuffix, &p.DefaultNamespaces, &p.ZkvSecretsPath, &p.AtLarkIDs, &p.SecretPrefix, &p.CreatedAt, &p.UpdatedAt)
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
