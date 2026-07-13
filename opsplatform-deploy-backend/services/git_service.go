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

	"gopkg.in/yaml.v3"
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

// TryAcquire 抢锁带超时；抢到返回 true，超时返回 false（不会改锁状态）
//
//	预览路径用：抢不到就放弃 git pull，保留旧 git_cache 走 precheck，
//	避免用户因为别的发布在跑而白等几分钟才看到 diff。
func (m *LockMgr) TryAcquire(key string, timeout time.Duration) bool {
	mu := m.get(key)
	if mu.TryLock() {
		return true
	}
	if timeout <= 0 {
		return false
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if mu.TryLock() {
			return true
		}
	}
	return false
}

// GitService 管理每个 project_env 的本地 clone + git 操作
type GitService struct {
	CacheDir string // e.g. /app/git-cache
	// Locks: 共享 cache（扫描/预检读路径）用的 per-env 锁。
	// 写路径（发布/新增模块）已改成"每操作独立浅克隆"(Checkout)，不再抢这把锁。
	Locks *LockMgr
	// PushLocks: 写路径按仓库串行 push（短锁，只护 fetch+push ~1s），
	// 让并发的编辑/helm 在各自 Checkout 里并行、只有 push 这一小段排队，避免推送 thundering-herd 反复重试。
	PushLocks *LockMgr
	User      string        // git commit author name
	Email     string        // git commit author email
	Token     func() string // 懒加载 gitlab PAT
}

func NewGitService(cacheDir, user, email string, tokenFn func() string) *GitService {
	return &GitService{
		CacheDir:  cacheDir,
		Locks:     NewLockMgr(),
		PushLocks: NewLockMgr(),
		User:      user,
		Email:     email,
		Token:     tokenFn,
	}
}

// RepoPath 返回某 project_env 的本地 clone 目录
func (g *GitService) RepoPath(projectEnvName string) string {
	return filepath.Join(g.CacheDir, projectEnvName)
}

// CleanWorkRoot 清掉上次进程崩溃遗留的隔离工作区临时目录（启动时调一次）。
func CleanWorkRoot(cacheDir string) {
	_ = os.RemoveAll(filepath.Join(cacheDir, "_work"))
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

// PullRebase 硬同步本地 cache 到远端 branch（跟 vm-agent BuildGitSyncCommand 同款逻辑）。
//
//	  git fetch --prune origin
//	  git checkout -f <branch>
//	  git reset --hard origin/<branch>
//	  git clean -fd
//
//	任何脏状态（dirty working tree / 半截 rebase / untracked / 远端历史被重写）
//	都强制对齐远端，零手工介入。
//
//	deploy-backend 的 git_cache 是只读副本（没人在 cache 里 commit），
//	reset/clean 零数据丢失风险。
//
//	为啥不用 git pull --rebase：碰到 dirty / 历史重写就 abort 卡死，
//	pullForPrecheck 静默吞错误后 cache 永远是旧的，需要 kubectl rm -rf 才能恢复。
//
//	函数名保留 PullRebase 不改（实现换成硬同步），调用方零改动。
func (g *GitService) PullRebase(ctx context.Context, projectEnvName, branch string) error {
	path := g.RepoPath(projectEnvName)
	if err := g.configRepo(ctx, path); err != nil {
		return err
	}
	steps := [][]string{
		{"-C", path, "fetch", "--prune", "origin"},
		{"-C", path, "checkout", "-f", branch},
		{"-C", path, "reset", "--hard", "origin/" + branch},
		{"-C", path, "clean", "-fd"},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %v: %w\n%s", args, err, out)
		}
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
			// 🔒 安全：scrub PAT/secret，避免 http://bot:PAT@... 写进 deployment.error_msg 给前端
			return fmt.Errorf("git push: %w\n%s", err, ScrubSecrets(out))
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

// ============================ 隔离工作区 Checkout ============================
//
// 写路径（发布 / 新增模块）的并发核心：每个操作开一份**独立浅克隆**到临时目录，
// 在里面并行编辑/commit，互不干扰；只有 push 这一小段按仓库串行（fetch+push ~1s）。
// 这样同一个 env 的 100 个操作也能并行，不再被单一共享工作目录串行卡住。
//
// 安全性：
//   - 同一模块的并发发布已被 DB 模块锁挡住（走不到这里），所以不会有两个写者改同一个模块文件；
//   - 不同文件的并发提交由 git 三方合并自然处理；
//   - 唯一会撞同一文件的是"多人同时新增模块都改 -apps/values.yaml"——push 被拒时
//     reset 到远端最新 + 由调用方 reapply 重做语义改动（AppendAppsEntry 查重幂等）再推，绝不覆盖别人。

// Checkout 是一次写操作的隔离浅克隆工作区，用完必须 Release（删临时目录）。
type Checkout struct {
	g       *GitService
	Dir     string
	envName string
	repoURL string
	branch  string
}

// AcquireCheckout 为一次写操作开一份独立浅克隆。调用方务必 defer c.Release()。
func (g *GitService) AcquireCheckout(ctx context.Context, envName, repoURL, branch string) (*Checkout, error) {
	workRoot := filepath.Join(g.CacheDir, "_work")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(workRoot, sanitizeName(envName)+"-")
	if err != nil {
		return nil, err
	}
	authURL := injectToken(repoURL, g.User, g.Token())
	cachePath := g.RepoPath(envName)
	// 优先从本地热缓存克隆（大仓库关键提速）：
	//   缓存由扫描调度/预填保持热(每几分钟 fetch)。用 --local 硬链接对象——瞬间完成、
	//   不重下整个大仓库、且硬链接能扛住缓存被 git gc（inode 引用计数保住对象）。
	//   然后只从远端拉本分支几分钟的增量并对齐。缓存不存在(冷)则回退远端浅克隆。
	useCache := false
	if fi, serr := os.Stat(filepath.Join(cachePath, ".git")); serr == nil && fi.IsDir() {
		useCache = true
	}
	var cloneErr error
	if useCache {
		cloneErr = g.checkoutFromCache(ctx, dir, cachePath, authURL, branch)
		if cloneErr != nil {
			// 缓存路径异常 → 回退远端浅克隆，不阻断
			_ = os.RemoveAll(dir)
			dir, err = os.MkdirTemp(workRoot, sanitizeName(envName)+"-")
			if err != nil {
				return nil, err
			}
			useCache = false
		}
	}
	if !useCache {
		out, e := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--single-branch", "--branch", branch, authURL, dir).CombinedOutput()
		if e != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("git clone: %w\n%s", e, ScrubSecrets(out))
		}
	}
	if err := g.configRepo(ctx, dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &Checkout{g: g, Dir: dir, envName: envName, repoURL: repoURL, branch: branch}, nil
}

// checkoutFromCache 从本地热缓存硬链接克隆(瞬间) + 从远端拉本分支增量对齐到最新。
func (g *GitService) checkoutFromCache(ctx context.Context, dir, cachePath, authURL, branch string) error {
	steps := [][]string{
		// --local 硬链接缓存对象；--no-checkout 先不铺工作树，等对齐远端最新再铺(只铺一次)
		{"clone", "--local", "--no-checkout", cachePath, dir},
		{"-C", dir, "remote", "set-url", "origin", authURL},
		{"-C", dir, "fetch", "--depth", "1", "origin", branch},
		{"-C", dir, "checkout", "-B", branch, "FETCH_HEAD"},
	}
	for _, args := range steps {
		if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %w\n%s", args[0], err, ScrubSecrets(out))
		}
	}
	return nil
}

// Release 删掉临时工作区。多次调用安全（清空 Dir 防重复删）。
func (c *Checkout) Release() {
	if c != nil && c.Dir != "" {
		_ = os.RemoveAll(c.Dir)
		c.Dir = ""
	}
}

// ReadFile / WriteFile 在隔离工作区内按相对路径读写。
func (c *Checkout) ReadFile(relPath string) ([]byte, error) {
	return os.ReadFile(filepath.Join(c.Dir, relPath))
}

func (c *Checkout) WriteFile(relPath string, content []byte) error {
	full := filepath.Join(c.Dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}

// commitAll 在隔离工作区 git add -A + commit，返回 short sha；无改动返回 ("", nil)。
func (c *Checkout) commitAll(ctx context.Context, operator, message string) (string, error) {
	dir := c.Dir
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "add", "-A").CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %w\n%s", err, scrubSecrets(out))
	}
	out, _ := exec.CommandContext(ctx, "git", "-C", dir, "diff", "--cached", "--name-only").CombinedOutput()
	if strings.TrimSpace(string(out)) == "" {
		return "", nil
	}
	authorName := c.g.User
	if operator != "" && operator != "system" && operator != c.g.User {
		authorName = c.g.User + "_" + operator
	}
	author := fmt.Sprintf("%s <%s>", authorName, c.g.Email)
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "commit", "--author", author, "-m", message).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %w\n%s", err, scrubSecrets(out))
	}
	shaOut, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--short", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	return strings.TrimSpace(string(shaOut)), nil
}

// CommitAndPush 提交隔离工作区的改动并推送。
//
//	edit：产生本次改动的语义操作（读文件→改→写回工作区）。返回 changed=false 表示无改动。
//	     push 被远端拒（有人抢先推了）时会 reset 到远端最新，再调 edit **重做一遍**（幂等），
//	     所以 edit 必须是"基于当前工作区内容重新计算"的纯操作，绝不能覆盖别人已合入的东西。
//	返回：最终 commit short sha；changed=false 时返回 ("", false, nil)。
//	push 阶段按仓库串行（短锁），避免并发推送互相 reject 反复重试。
func (c *Checkout) CommitAndPush(ctx context.Context, operator, message string, retries int, edit func() (changed bool, err error), st *StageTimer) (sha string, changed bool, err error) {
	if retries <= 0 {
		retries = 5
	}
	changed, err = edit()
	if err != nil {
		return "", false, err
	}
	st.Mark("edit")
	if !changed {
		return "", false, nil
	}
	sha, err = c.commitAll(ctx, operator, message)
	if err != nil {
		return "", false, err
	}
	st.Mark("git_commit")
	if sha == "" {
		return "", false, nil
	}

	// push 按仓库串行（短锁）
	c.g.PushLocks.Acquire(c.repoURL)
	defer c.g.PushLocks.Release(c.repoURL)
	st.Mark("push_lock")

	for attempt := 1; attempt <= retries; attempt++ {
		out, perr := exec.CommandContext(ctx, "git", "-C", c.Dir, "push", "origin", c.branch).CombinedOutput()
		if perr == nil {
			st.Mark("git_push")
			return sha, true, nil
		}
		lower := strings.ToLower(string(out))
		if !strings.Contains(lower, "rejected") && !strings.Contains(lower, "non-fast-forward") && !strings.Contains(lower, "fetch first") {
			return "", false, fmt.Errorf("git push: %w\n%s", perr, ScrubSecrets(out))
		}
		// 被抢先推了 → 硬同步到远端最新，重做语义改动，再推
		if rerr := c.resetToRemote(ctx); rerr != nil {
			return "", false, fmt.Errorf("push conflict, resync failed: %w", rerr)
		}
		if changed, eerr := edit(); eerr != nil {
			return "", false, fmt.Errorf("push conflict, reapply failed: %w", eerr)
		} else if !changed {
			// 远端已经包含了等价改动（罕见）→ 视为完成
			return "", false, nil
		}
		if sha, err = c.commitAll(ctx, operator, message); err != nil {
			return "", false, err
		}
		if sha == "" {
			return "", false, nil
		}
	}
	st.Mark("git_push")
	return "", false, fmt.Errorf("git push: exceeded %d retries (仓库写入竞争过高)", retries)
}

// CommitPushReapply 提交当前工作区已写入的改动并推送；push 被拒时 reset 到远端最新，
// 调 reapply() 在最新基线上重新生成改动，再提交重试。
//
//	与 CommitAndPush 的区别：初始改动由调用方在调用前写好（用于"改动 + helm 校验"这种
//	需要在 commit 前先验证的流程）；reapply 只在冲突时触发。push 按仓库串行。
func (c *Checkout) CommitPushReapply(ctx context.Context, operator, message string, retries int, reapply func() error, st *StageTimer) (string, error) {
	if retries <= 0 {
		retries = 5
	}
	sha, err := c.commitAll(ctx, operator, message)
	if err != nil {
		return "", err
	}
	st.Mark("git_commit")
	if sha == "" {
		return "", fmt.Errorf("没有产生任何改动（模块可能已存在）")
	}
	c.g.PushLocks.Acquire(c.repoURL)
	defer c.g.PushLocks.Release(c.repoURL)
	st.Mark("push_lock")

	for attempt := 1; attempt <= retries; attempt++ {
		out, perr := exec.CommandContext(ctx, "git", "-C", c.Dir, "push", "origin", c.branch).CombinedOutput()
		if perr == nil {
			st.Mark("git_push")
			return sha, nil
		}
		lower := strings.ToLower(string(out))
		if !strings.Contains(lower, "rejected") && !strings.Contains(lower, "non-fast-forward") && !strings.Contains(lower, "fetch first") {
			return "", fmt.Errorf("git push: %w\n%s", perr, ScrubSecrets(out))
		}
		if rerr := c.resetToRemote(ctx); rerr != nil {
			return "", fmt.Errorf("push conflict, resync failed: %w", rerr)
		}
		if rerr := reapply(); rerr != nil {
			return "", fmt.Errorf("push conflict, reapply failed: %w", rerr)
		}
		if sha, err = c.commitAll(ctx, operator, message); err != nil {
			return "", err
		}
		if sha == "" {
			return "", fmt.Errorf("重放后无改动（模块可能已被他人添加）")
		}
	}
	st.Mark("git_push")
	return "", fmt.Errorf("git push: 超过 %d 次重试仍失败（仓库写入竞争过高）", retries)
}

// resetToRemote 把隔离工作区硬同步到远端分支最新（丢弃本地 commit，供 reapply 用）。
func (c *Checkout) resetToRemote(ctx context.Context) error {
	steps := [][]string{
		{"-C", c.Dir, "fetch", "--depth", "1", "origin", c.branch},
		{"-C", c.Dir, "reset", "--hard", "FETCH_HEAD"},
		{"-C", c.Dir, "clean", "-fd"},
	}
	for _, args := range steps {
		if out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %w\n%s", args, err, ScrubSecrets(out))
		}
	}
	return nil
}

// sanitizeName 把 env 名清成可安全做临时目录前缀的形式
func sanitizeName(s string) string {
	return strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(s)
}

// ====================== ArgoCD app name suffix 智能解析 ======================
//
// 背景：deploy-center 要拼 ArgoCD app name 才能调 ArgoCD API（restart/sync/health）。
// 老规则：app_name = "{service}-{project_env.name}"
// 但生产 helm 模板里有两种命名约定：
//   g50/g33-uat-apps/templates/applications.yaml:
//     name: {{ .name }}-{{ $.Values.global.spec.project }}   ← 老格式
//   g32-uat-apps/templates/applications.yaml:
//     name: {{ .name }}-{{ $.Values.global.env }}            ← 新格式
//
// 同样的 service "atmosphere-frontend" 在两种约定下渲染出来不一样：
//   - 老：atmosphere-frontend-g32-uat
//   - 新：atmosphere-frontend-uat
//
// 不能写死规则。改成「跟着 git 走」：scanModules 时读这两个文件，
// 正则识别 metadata.name 引用的是 global.<X>，再从 values.yaml 取 global.<X> 的值。
// 解析失败就回退 strings.ToLower(projectEnvName) 保持当前行为，对未配应用编排
// 的简单项目无侵入。

// appNameTplRe 匹配 helm 模板里 metadata.name 形如 "{{ .name }}-{{ $.Values.global.X }}" 的写法
//
//	支持：trim 标记（{{- .name -}}）、字段路径含点号（spec.project）
//	(?m) 让 ^ 匹配行首
var appNameTplRe = regexp.MustCompile(`(?m)^\s*name:\s*\{\{-?\s*\.name\s*-?\}\}-\{\{-?\s*\$\.Values\.global\.([\w.]+)\s*-?\}\}`)

// ResolveAppNameSuffix 读 {chartBasePath}-apps/templates/applications.yaml 智能识别后缀
//
//	约定：app-of-apps 编排目录是子 chart 目录的 sibling，名字 "{chartBasePath}-apps"
//	例如 chartBasePath = "argocd-apps/charts/g32-uat"
//	     则 apps  目录 = "argocd-apps/charts/g32-uat-apps"
//
//	找不到模板 / 解析失败 / values 里没有对应字段 → 回退 strings.ToLower(projectEnvName)
//	（兼容老项目和未用 app-of-apps 模式的简单项目）
func (g *GitService) ResolveAppNameSuffix(projectEnvName, chartBasePath string) string {
	fallback := strings.ToLower(projectEnvName)
	if chartBasePath == "" {
		return fallback
	}

	repoRoot := g.RepoPath(projectEnvName)
	appsDir := filepath.Join(repoRoot, chartBasePath+"-apps")
	tplPath := filepath.Join(appsDir, "templates", "applications.yaml")
	valuesPath := filepath.Join(appsDir, "values.yaml")

	tpl, err := os.ReadFile(tplPath)
	if err != nil {
		return fallback
	}
	m := appNameTplRe.FindStringSubmatch(string(tpl))
	if len(m) < 2 {
		return fallback
	}
	keyPath := m[1] // "env" / "spec.project" / 任意点号嵌套

	raw, err := os.ReadFile(valuesPath)
	if err != nil {
		return fallback
	}
	var v map[string]interface{}
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return fallback
	}
	cur, ok := v["global"].(map[string]interface{})
	if !ok {
		return fallback
	}
	var val interface{} = cur
	for _, p := range strings.Split(keyPath, ".") {
		mp, ok := val.(map[string]interface{})
		if !ok {
			return fallback
		}
		val, ok = mp[p]
		if !ok {
			return fallback
		}
	}
	str, ok := val.(string)
	if !ok || str == "" {
		return fallback
	}
	return strings.ToLower(str)
}
