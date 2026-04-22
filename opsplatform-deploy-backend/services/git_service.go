package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// tokenInURLRe 匹配 http(s)://user:token@host 形式的凭证
var tokenInURLRe = regexp.MustCompile(`(https?://)([^:/\s]+):([^@/\s]+)@`)

// ScrubSecrets 清洗 git 输出里的 URL 凭证，避免 PAT 泄露到 error / log / DB
// 输入 "http://bot:abc@host/..." 变为 "http://bot:***@host/..."
func ScrubSecrets(b []byte) string {
	return tokenInURLRe.ReplaceAllString(string(b), "$1$2:***@")
}

// 保留小写别名给包内复用
func scrubSecrets(b []byte) string { return ScrubSecrets(b) }

type LockMgr struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewLockMgr() *LockMgr {
	return &LockMgr{locks: make(map[string]*sync.Mutex)}
}

func (m *LockMgr) get(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	return l
}

func (m *LockMgr) Acquire(key string) { m.get(key).Lock() }
func (m *LockMgr) Release(key string) { m.get(key).Unlock() }

// GitService 管理每个 project_env 的本地 clone + git 操作
type GitService struct {
	CacheDir string        // e.g. /app/git-cache
	Locks    *LockMgr
	User     string        // git commit author name
	Email    string        // git commit author email
	Token    func() string // 懒加载 gitlab PAT
}

func NewGitService(cacheDir, user, email string, tokenFn func() string) *GitService {
	return &GitService{
		CacheDir: cacheDir,
		Locks:    NewLockMgr(),
		User:     user,
		Email:    email,
		Token:    tokenFn,
	}
}

// RepoPath 返回某 project_env 的本地 clone 目录
func (g *GitService) RepoPath(projectEnvName string) string {
	return filepath.Join(g.CacheDir, projectEnvName)
}

// EnsureClone 没 clone 就克隆，已 clone 就 pull --rebase
func (g *GitService) EnsureClone(ctx context.Context, projectEnvName, repoURL, branch string) error {
	path := g.RepoPath(projectEnvName)
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return g.PullRebase(ctx, projectEnvName, branch)
	}
	if err := os.MkdirAll(g.CacheDir, 0o755); err != nil {
		return err
	}
	authURL := injectToken(repoURL, g.User, g.Token())
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", branch, authURL, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w\n%s", err, out)
	}
	return g.configRepo(ctx, path)
}

// PullRebase 拉取最新并 rebase 本地改动
func (g *GitService) PullRebase(ctx context.Context, projectEnvName, branch string) error {
	path := g.RepoPath(projectEnvName)
	if err := g.configRepo(ctx, path); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "-C", path, "pull", "--rebase", "origin", branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull --rebase: %w\n%s", err, out)
	}
	return nil
}

func (g *GitService) configRepo(ctx context.Context, path string) error {
	for _, args := range [][]string{
		{"-C", path, "config", "user.name", g.User},
		{"-C", path, "config", "user.email", g.Email},
	} {
		if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git config: %w\n%s", err, out)
		}
	}
	return nil
}

// injectToken 把 gitlab token 注入 http(s) URL
// http://gitlab.xx/foo.git + user=bot + token=abc -> http://bot:abc@gitlab.xx/foo.git
func injectToken(url, user, token string) string {
	if token == "" {
		return url
	}
	for _, proto := range []string{"https://", "http://"} {
		if strings.HasPrefix(url, proto) {
			return proto + user + ":" + token + "@" + strings.TrimPrefix(url, proto)
		}
	}
	return url
}

// GitCtx 返回一个带超时的 context（默认 60s）
func GitCtx(parent context.Context, seconds int) (context.Context, context.CancelFunc) {
	if seconds <= 0 {
		seconds = 60
	}
	return context.WithTimeout(parent, time.Duration(seconds)*time.Second)
}

// WriteFile 在 clone 目录里写一个相对路径的文件
func (g *GitService) WriteFile(projectEnvName, relPath string, content []byte) error {
	full := filepath.Join(g.RepoPath(projectEnvName), relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}

// ReadFile 读一个相对路径
func (g *GitService) ReadFile(projectEnvName, relPath string) ([]byte, error) {
	full := filepath.Join(g.RepoPath(projectEnvName), relPath)
	return os.ReadFile(full)
}

// CommitAll 在 clone 目录下 git add -A + commit
// 返回新 commit hash (short)。没有 staged changes 时返回 ("", nil) 代表 no-op。
// operator: 实际操作人 username。作为 commit author 的后缀（e.g. "deploy-bot_user1"），
//   email 沿用 global_config 的 gitlab_email（机器人邮箱）。
//   operator 为空或 "system" 时不拼后缀，author 就是机器人本身。
func (g *GitService) CommitAll(ctx context.Context, projectEnvName, operator, message string) (string, error) {
	path := g.RepoPath(projectEnvName)
	if out, err := exec.CommandContext(ctx, "git", "-C", path, "add", "-A").CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %w\n%s", err, scrubSecrets(out))
	}
	out, _ := exec.CommandContext(ctx, "git", "-C", path, "diff", "--cached", "--name-only").CombinedOutput()
	if strings.TrimSpace(string(out)) == "" {
		return "", nil
	}
	authorName := g.User
	if operator != "" && operator != "system" && operator != g.User {
		authorName = g.User + "_" + operator
	}
	author := fmt.Sprintf("%s <%s>", authorName, g.Email)
	cmd := exec.CommandContext(ctx, "git", "-C", path, "commit", "--author", author, "-m", message)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %w\n%s", err, scrubSecrets(out))
	}
	shaOut, err := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--short", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(shaOut)), nil
}

// Push 推当前分支；冲突自动 pull --rebase 重试 retries 次
func (g *GitService) Push(ctx context.Context, projectEnvName, branch string, retries int) error {
	if retries <= 0 {
		retries = 3
	}
	path := g.RepoPath(projectEnvName)
	for attempt := 1; attempt <= retries; attempt++ {
		cmd := exec.CommandContext(ctx, "git", "-C", path, "push", "origin", branch)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		lower := strings.ToLower(string(out))
		if !strings.Contains(lower, "rejected") && !strings.Contains(lower, "non-fast-forward") {
			return fmt.Errorf("git push: %w\n%s", err, out)
		}
		if rerr := g.PullRebase(ctx, projectEnvName, branch); rerr != nil {
			return fmt.Errorf("push conflict, rebase failed: %w", rerr)
		}
	}
	return fmt.Errorf("git push: exceeded %d retries", retries)
}

// CommitURL 拼 gitlab commit 页面 url
// http://gitlab.xx/group/proj.git → http://gitlab.xx/group/proj/-/commit/<sha>
func CommitURL(repoURL, sha string) string {
	base := strings.TrimSuffix(repoURL, ".git")
	return base + "/-/commit/" + sha
}
