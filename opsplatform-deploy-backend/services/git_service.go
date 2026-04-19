package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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
