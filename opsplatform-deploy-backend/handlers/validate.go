package handlers

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"opsplatform-deploy-backend/database"
)

// safeNameRe 严格限制项目/环境/模块等名称：小写字母/数字/- ，以字母数字开头，长度 1-64
// 用作 git cache 目录名，防止 .. / 空格 / 特殊字符逃逸
var safeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidateName 校验名称是否合法（用于 project_env.name、project.name、contact.name 等）
func ValidateName(s string) error {
	if !safeNameRe.MatchString(s) {
		return fmt.Errorf("名称必须匹配 %s", safeNameRe.String())
	}
	return nil
}

// safeUsernameRe 用户名：字母数字+下划线/点/-，1-50
var safeUsernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,50}$`)

// ValidateLabel 宽松校验：允许中文/字母数字，禁止控制字符和 ..，长度 1-64
func ValidateLabel(s string) error {
	if s == "" {
		return fmt.Errorf("必填")
	}
	if len([]rune(s)) > 64 {
		return fmt.Errorf("长度不能超过 64 字符")
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return fmt.Errorf("包含非法控制字符")
		}
	}
	if strings.Contains(s, "..") {
		return fmt.Errorf("不能包含 ..")
	}
	return nil
}

// ValidateUsername 校验用户名
func ValidateUsername(s string) error {
	if !safeUsernameRe.MatchString(s) {
		return fmt.Errorf("用户名需为 1-50 位字母/数字/._- 组合")
	}
	return nil
}

// CleanRelPath 清洗相对路径，阻止 .. 逃逸。返回清洗后的路径或错误
// 允许：a/b/c.yaml
// 拒绝：../, 绝对路径，包含 ..
func CleanRelPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return "", fmt.Errorf("不允许绝对路径")
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("路径不能包含 ..")
	}
	if strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("路径逃逸到根目录")
	}
	return clean, nil
}

// ValidateGitRepoURL 校验 git_repo URL：
// 1. 必须是 http(s) 协议
// 2. host 必须属于 global_config.gitlab_url 的 host（避免 PAT 被发到外部仓库）
func ValidateGitRepoURL(repoURL string) error {
	u, err := url.Parse(repoURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("git_repo URL 格式错误")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("git_repo 只支持 http/https")
	}

	// 从 global_config 取 gitlab_url 做 host allowlist
	var gitlabURL string
	_ = database.DB.QueryRow(`SELECT IFNULL(gitlab_url,'') FROM global_config WHERE id=1`).Scan(&gitlabURL)
	if gitlabURL == "" {
		// 没配全局 gitlab，跳过 host 校验（兼容早期部署）
		return nil
	}
	gu, err := url.Parse(gitlabURL)
	if err != nil || gu.Host == "" {
		return nil
	}
	if !strings.EqualFold(u.Host, gu.Host) {
		return fmt.Errorf("git_repo host (%s) 必须与全局 gitlab_url host (%s) 一致", u.Host, gu.Host)
	}
	return nil
}

// ValidateLarkWebhookURL 校验 Lark webhook URL：必须 http(s) 且 host 看着像 lark/feishu
func ValidateLarkWebhookURL(webhook string) error {
	if webhook == "" {
		return fmt.Errorf("webhook 必填")
	}
	u, err := url.Parse(webhook)
	if err != nil || u.Host == "" {
		return fmt.Errorf("webhook URL 格式错误")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook 只支持 http/https")
	}
	host := strings.ToLower(u.Host)
	if !strings.Contains(host, "lark") && !strings.Contains(host, "feishu") {
		return fmt.Errorf("webhook host 必须是 lark/feishu 域名")
	}
	return nil
}
