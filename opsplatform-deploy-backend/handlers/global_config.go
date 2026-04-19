package handlers

import (
	"context"
	"net/http"
	"os/exec"
	"time"

	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/models"
)

func HandleGetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var c models.GlobalConfig
	err := database.DB.QueryRow(`SELECT id, gitlab_url, gitlab_user, gitlab_email, gitlab_token,
		lark_default_webhook, lark_default_secret,
		poll_interval_sec, poll_timeout_min, git_retry_count, updated_at
		FROM global_config WHERE id=1`).
		Scan(&c.ID, &c.GitlabURL, &c.GitlabUser, &c.GitlabEmail, &c.GitlabToken,
			&c.LarkDefaultWebhook, &c.LarkDefaultSecret,
			&c.PollIntervalSec, &c.PollTimeoutMin, &c.GitRetryCount, &c.UpdatedAt)
	if err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	c.GitlabToken = maskToken(c.GitlabToken)
	c.LarkDefaultSecret = maskToken(c.LarkDefaultSecret)
	JSONSuccess(w, c)
}

type updateGlobalConfigReq struct {
	GitlabURL          *string `json:"gitlab_url"`
	GitlabUser         *string `json:"gitlab_user"`
	GitlabEmail        *string `json:"gitlab_email"`
	GitlabToken        *string `json:"gitlab_token"`
	LarkDefaultWebhook *string `json:"lark_default_webhook"`
	LarkDefaultSecret  *string `json:"lark_default_secret"`
	PollIntervalSec    *int    `json:"poll_interval_sec"`
	PollTimeoutMin     *int    `json:"poll_timeout_min"`
	GitRetryCount      *int    `json:"git_retry_count"`
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
	if len(sets) == 0 {
		JSONSuccess(w, nil)
		return
	}
	q := "UPDATE global_config SET " + joinComma(sets) + " WHERE id=1"
	if _, err := database.DB.Exec(q, args...); err != nil {
		JSONError(w, 50000, err.Error())
		return
	}
	JSONSuccess(w, nil)
}

func HandleTestGlobalGitlab(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL   string `json:"gitlab_url"`
		User  string `json:"gitlab_user"`
		Token string `json:"gitlab_token"`
	}
	// 允许空 body，DecodeJSON 失败时也能 fallback 到 DB
	_ = DecodeJSON(newDiscardW(), r, &req)
	url, user, token := req.URL, req.User, req.Token
	if url == "" || user == "" || token == "" {
		var t string
		_ = database.DB.QueryRow(`SELECT gitlab_url, gitlab_user, gitlab_token FROM global_config WHERE id=1`).
			Scan(&url, &user, &t)
		if token == "" {
			dec, _ := crypto.Decrypt(t)
			token = dec
		}
	}
	if url == "" || user == "" || token == "" {
		JSONError(w, 40001, "gitlab_url/user/token 必填")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	authURL := injectTokenHelper(url, user, token)
	cmd := exec.CommandContext(ctx, "git", "ls-remote", authURL, "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		JSONError(w, 50001, "git ls-remote failed: "+string(out))
		return
	}
	JSONSuccess(w, map[string]interface{}{"ok": true, "head": string(out)})
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

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// 一个 fake response writer，用来在 test-gitlab 里静默 DecodeJSON 失败
type discardW struct{}

func (discardW) Header() http.Header       { return http.Header{} }
func (discardW) Write(p []byte) (int, error) { return len(p), nil }
func (discardW) WriteHeader(_ int)          {}
func newDiscardW() http.ResponseWriter      { return discardW{} }
