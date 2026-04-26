package handlers

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
	"opsplatform-deploy-backend/services"
)

func HandleGetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var c models.GlobalConfig
	err := database.DB.QueryRow(`SELECT id, gitlab_url, gitlab_user, gitlab_email, gitlab_token,
		IFNULL(test_repo_path,''), IFNULL(deploy_center_base_url,''),
		lark_default_webhook, lark_default_secret,
		poll_interval_sec, poll_timeout_min, git_retry_count, updated_at,
		IFNULL(minio_endpoint,''), IFNULL(minio_bucket,'deploy-logs'),
		IFNULL(minio_access_key,''), IFNULL(minio_secret_key,''),
		IFNULL(minio_region,'us-east-1'), IFNULL(minio_retention_days, 90),
		IFNULL(history_retention_days, 180), last_history_cleanup_at
		FROM global_config WHERE id=1`).
		Scan(&c.ID, &c.GitlabURL, &c.GitlabUser, &c.GitlabEmail, &c.GitlabToken,
			&c.TestRepoPath, &c.DeployCenterBaseURL,
			&c.LarkDefaultWebhook, &c.LarkDefaultSecret,
			&c.PollIntervalSec, &c.PollTimeoutMin, &c.GitRetryCount, &c.UpdatedAt,
			&c.MinIOEndpoint, &c.MinIOBucket, &c.MinIOAccessKey, &c.MinIOSecretKey,
			&c.MinIORegion, &c.MinIORetentionDays,
			&c.HistoryRetentionDays, &c.LastHistoryCleanupAt)
	if err != nil {
		InternalErr(w, r, err)
		return
	}
	c.GitlabToken = maskToken(c.GitlabToken)
	c.LarkDefaultSecret = maskToken(c.LarkDefaultSecret)
	c.MinIOSecretKey = maskToken(c.MinIOSecretKey)
	JSONSuccess(w, c)
}

type updateGlobalConfigReq struct {
	GitlabURL           *string `json:"gitlab_url"`
	GitlabUser          *string `json:"gitlab_user"`
	GitlabEmail         *string `json:"gitlab_email"`
	GitlabToken         *string `json:"gitlab_token"`
	TestRepoPath        *string `json:"test_repo_path"`
	DeployCenterBaseURL *string `json:"deploy_center_base_url"`
	LarkDefaultWebhook  *string `json:"lark_default_webhook"`
	LarkDefaultSecret   *string `json:"lark_default_secret"`
	PollIntervalSec     *int    `json:"poll_interval_sec"`
	PollTimeoutMin      *int    `json:"poll_timeout_min"`
	GitRetryCount       *int    `json:"git_retry_count"`
	// MinIO 配置
	MinIOEndpoint      *string `json:"minio_endpoint"`
	MinIOBucket        *string `json:"minio_bucket"`
	MinIOAccessKey     *string `json:"minio_access_key"`
	MinIOSecretKey     *string `json:"minio_secret_key"`
	MinIORegion        *string `json:"minio_region"`
	MinIORetentionDays *int    `json:"minio_retention_days"`
	// 发布历史保留天数
	HistoryRetentionDays *int `json:"history_retention_days"`
}

func HandleUpdateGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var req updateGlobalConfigReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	sets := []string{}
	args := []interface{}{}
	addStr := func(col string, v *string) {
		if v != nil {
			sets = append(sets, col+"=?")
			args = append(args, *v)
		}
	}
	addStr("gitlab_url", req.GitlabURL)
	addStr("gitlab_user", req.GitlabUser)
	addStr("gitlab_email", req.GitlabEmail)
	addStr("test_repo_path", req.TestRepoPath)
	addStr("deploy_center_base_url", req.DeployCenterBaseURL)
	addStr("lark_default_webhook", req.LarkDefaultWebhook)
	if req.GitlabToken != nil && *req.GitlabToken != "" {
		enc, err := crypto.Encrypt(*req.GitlabToken)
		if err != nil {
			JSONError(w, 50000, "encrypt: "+err.Error())
			return
		}
		sets = append(sets, "gitlab_token=?")
		args = append(args, enc)
	}
	if req.LarkDefaultSecret != nil && *req.LarkDefaultSecret != "" {
		enc, _ := crypto.Encrypt(*req.LarkDefaultSecret)
		sets = append(sets, "lark_default_secret=?")
		args = append(args, enc)
	}
	if req.PollIntervalSec != nil {
		if *req.PollIntervalSec < 5 || *req.PollIntervalSec > 60 {
			JSONError(w, 40001, "poll_interval_sec 必须在 5-60")
			return
		}
		sets = append(sets, "poll_interval_sec=?")
		args = append(args, *req.PollIntervalSec)
	}
	if req.PollTimeoutMin != nil {
		if *req.PollTimeoutMin < 1 || *req.PollTimeoutMin > 10 {
			JSONError(w, 40001, "poll_timeout_min 必须在 1-10")
			return
		}
		sets = append(sets, "poll_timeout_min=?")
		args = append(args, *req.PollTimeoutMin)
	}
	if req.GitRetryCount != nil {
		if *req.GitRetryCount < 1 || *req.GitRetryCount > 10 {
			JSONError(w, 40001, "git_retry_count 必须在 1-10")
			return
		}
		sets = append(sets, "git_retry_count=?")
		args = append(args, *req.GitRetryCount)
	}
	// MinIO 字段
	addStr("minio_endpoint", req.MinIOEndpoint)
	addStr("minio_bucket", req.MinIOBucket)
	addStr("minio_access_key", req.MinIOAccessKey)
	addStr("minio_region", req.MinIORegion)
	if req.MinIOSecretKey != nil && *req.MinIOSecretKey != "" {
		enc, err := crypto.Encrypt(*req.MinIOSecretKey)
		if err != nil {
			JSONError(w, 50000, "encrypt: "+err.Error())
			return
		}
		sets = append(sets, "minio_secret_key=?")
		args = append(args, enc)
	}
	if req.MinIORetentionDays != nil {
		days := *req.MinIORetentionDays
		validDays := map[int]bool{7: true, 30: true, 90: true, 180: true}
		if !validDays[days] {
			JSONError(w, 40001, "minio_retention_days 必须是 7 / 30 / 90 / 180")
			return
		}
		sets = append(sets, "minio_retention_days=?")
		args = append(args, days)
	}
	if req.HistoryRetentionDays != nil {
		days := *req.HistoryRetentionDays
		validDays := map[int]bool{30: true, 90: true, 180: true, 365: true, 0: true}
		// 0 = 永久保留
		if !validDays[days] {
			JSONError(w, 40001, "history_retention_days 必须是 0 / 30 / 90 / 180 / 365")
			return
		}
		sets = append(sets, "history_retention_days=?")
		args = append(args, days)
	}
	if len(sets) == 0 {
		JSONSuccess(w, nil)
		return
	}
	q := "UPDATE global_config SET " + strings.Join(sets, ",") + " WHERE id=1"
	if _, err := database.DB.Exec(q, args...); err != nil {
		InternalErr(w, r, err)
		return
	}
	// MinIO 字段有任何变化时，异步刷新 bucket lifecycle
	// （admin 改保留天数 90→30 之后，立刻让 bucket 用新规则）
	if req.MinIOEndpoint != nil || req.MinIOBucket != nil || req.MinIOAccessKey != nil ||
		req.MinIOSecretKey != nil || req.MinIORegion != nil || req.MinIORetentionDays != nil {
		ApplyMinIOLifecycleNow()
	}
	Audit(r, "global_config.update", "global_config", "", map[string]interface{}{"fields": len(sets)})
	JSONSuccess(w, nil)
}

// HandleTestGlobalGitlab 测试 GitLab 凭证
// 策略：
//   1) 如果前端传了 test_repo_path 或 DB 有配置 → 用该仓库做 git ls-remote（最精确）
//   2) 否则调 GitLab API /api/v4/version → 200 表示 token 有效
//      403 表示 token 只有 git scope（典型 impersonation token），提示用户
//      401 表示 token 无效
func HandleTestGlobalGitlab(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL          string `json:"gitlab_url"`
		User         string `json:"gitlab_user"`
		Token        string `json:"gitlab_token"`
		TestRepoPath string `json:"test_repo_path"`
	}
	_ = DecodeJSON(newDiscardW(), r, &req)
	url, user, token, repoPath := req.URL, req.User, req.Token, req.TestRepoPath

	// 任一字段空就从 DB fallback
	if url == "" || user == "" || token == "" || repoPath == "" {
		var dbURL, dbUser, dbEnc, dbPath string
		_ = database.DB.QueryRow(`SELECT gitlab_url, gitlab_user, gitlab_token, IFNULL(test_repo_path,'')
			FROM global_config WHERE id=1`).
			Scan(&dbURL, &dbUser, &dbEnc, &dbPath)
		if url == "" {
			url = dbURL
		}
		if user == "" {
			user = dbUser
		}
		if token == "" {
			dec, _ := crypto.Decrypt(dbEnc)
			token = dec
		}
		if repoPath == "" {
			repoPath = dbPath
		}
	}

	if url == "" || user == "" || token == "" {
		JSONError(w, 40001, "gitlab_url/user/token 必填")
		return
	}

	// 策略 1：有 test_repo_path → 用具体仓库做 git ls-remote（最可靠）
	if repoPath != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		repoURL := strings.TrimRight(url, "/") + "/" + strings.TrimLeft(repoPath, "/")
		if !strings.HasSuffix(repoURL, ".git") {
			repoURL += ".git"
		}
		authURL := injectTokenHelper(repoURL, user, token)
		out, err := exec.CommandContext(ctx, "git", "ls-remote", authURL, "HEAD").CombinedOutput()
		if err != nil {
			JSONError(w, 50001, "git ls-remote failed: "+services.ScrubSecrets(out))
			return
		}
		JSONSuccess(w, map[string]interface{}{
			"ok":     true,
			"method": "git",
			"head":   strings.TrimSpace(services.ScrubSecrets(out)),
		})
		return
	}

	// 策略 2：没填 test_repo_path → 调 API /api/v4/version
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	apiURL := strings.TrimRight(url, "/") + "/api/v4/version"
	httpReq, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	httpReq.Header.Set("PRIVATE-TOKEN", token)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		JSONError(w, 50001, "GitLab 连接失败："+err.Error())
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200:
		JSONSuccess(w, map[string]interface{}{"ok": true, "method": "api"})
	case 401:
		JSONError(w, 40100, "Token 无效（GitLab 返回 401）")
	case 403:
		JSONError(w, 40300, "Token 存在但缺少 read_api scope（git-only token 的典型情况）。"+
			"建议：在「测试仓库路径」填一个有权限的仓库（如 argocd/uat-k8s-platform），点测试会用 git 协议精确验证。")
	default:
		JSONError(w, 50001, "GitLab API 返回异常状态码："+resp.Status)
	}
}

func injectTokenHelper(url, user, token string) string {
	for _, p := range []string{"https://", "http://"} {
		if len(url) > len(p) && url[:len(p)] == p {
			return p + user + ":" + token + "@" + url[len(p):]
		}
	}
	return url
}

func maskToken(s string) string {
	if s == "" {
		return ""
	}
	return "••••••••"
}

// 一个 fake response writer，用来在 test-gitlab 里静默 DecodeJSON 失败
type discardW struct{}

func (discardW) Header() http.Header       { return http.Header{} }
func (discardW) Write(p []byte) (int, error) { return len(p), nil }
func (discardW) WriteHeader(_ int)          {}
func newDiscardW() http.ResponseWriter      { return discardW{} }
