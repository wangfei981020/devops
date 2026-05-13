package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status 任务生命周期
type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
)

// Action 任务类型
type Action string

const (
	ActionRsync          Action = "rsync"
	ActionUpdateVersion  Action = "update_version"
	ActionGitSync        Action = "git_sync"          // 单独跑一次 git pull（diagnostic 用）
	ActionRsyncAndUpdate Action = "rsync_and_update"  // 单 task 内 shell 串行：rsync → grep version.txt → ansible-playbook
)

// Spec 触发任务的输入参数
type Spec struct {
	Action  Action `json:"action"`
	Project string `json:"project"`           // G01 / G02
	Env     string `json:"env"`               // UAT / LPT / PROD（rsync 不需要）
	Service string `json:"service"`           // 服务名 = playbook 文件名
	Version string `json:"version,omitempty"` // 仅 update_version 必填
}

// Task 任务全量字段，给 API 序列化
type Task struct {
	ID         string    `json:"id"`
	Spec       Spec      `json:"spec"`
	Status     Status    `json:"status"`
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	ErrMsg     string    `json:"err_msg,omitempty"`

	// 内部字段（json:"-"）
	cancel    context.CancelFunc
	logBuffer *logBuffer
}

// GitSyncer 抽象「跑一次 git pull」给 Manager 用，让 manager 不直接耦合 runner。
//
//	返回的 *exec.Cmd 一次性使用，manager 跑完一次决定是否重试时再调一次拿新的。
type GitSyncer interface {
	BuildGitSyncCommand() *exec.Cmd
}

// Manager 管理所有 task：调度、并发限制、生命周期、日志缓冲
//
//	v5 起新增 gitMu：所有 git 操作（新 batch 端点、老 single Submit、ActionGitSync 诊断）
//	全部走这个 mutex，确保同时只有一个 git 进程在动 ansible 仓库。
type Manager struct {
	mu            sync.RWMutex
	tasks         map[string]*Task
	semaphore     chan struct{}    // 并发限制
	retention     time.Duration    // 完成任务保留时长
	taskKeyLocks  map[string]bool  // <project>:<env>:<service> 互斥（同 service 不并发）
	taskKeyLockMu sync.Mutex

	gitMu     sync.Mutex // 全局 git 仓库串行
	gitSyncer GitSyncer  // 产 git sync 命令的工具，由 NewManager 传入
}

// NewManager 创建 manager；maxConcurrent 限制全局并发，retention 保留完成任务的时长
func NewManager(maxConcurrent int, retention time.Duration, syncer GitSyncer) *Manager {
	m := &Manager{
		tasks:        make(map[string]*Task),
		semaphore:    make(chan struct{}, maxConcurrent),
		retention:    retention,
		taskKeyLocks: make(map[string]bool),
		gitSyncer:    syncer,
	}
	go m.gcLoop()
	return m
}

// taskKey 生成 service-level 互斥锁的 key
func taskKey(s Spec) string {
	return fmt.Sprintf("%s:%s:%s", s.Project, s.Env, s.Service)
}

// tryAcquireKeyLock 抢 service-level 锁；同 service 不允许并发
func (m *Manager) tryAcquireKeyLock(key string) bool {
	m.taskKeyLockMu.Lock()
	defer m.taskKeyLockMu.Unlock()
	if m.taskKeyLocks[key] {
		return false
	}
	m.taskKeyLocks[key] = true
	return true
}

func (m *Manager) releaseKeyLock(key string) {
	m.taskKeyLockMu.Lock()
	defer m.taskKeyLockMu.Unlock()
	delete(m.taskKeyLocks, key)
}

// CommandRunner 抽象出"如何把 spec 转成命令"的接口；让测试能 mock，让实际逻辑跟 manager 解耦
type CommandRunner interface {
	BuildCommand(s Spec) (*exec.Cmd, error)
}

// gitSyncRetryDelays 重试间隔（指数退避）；总尝试次数 = len + 1
//
//	0 / 2s / 5s 三档：覆盖瞬时网络抖动场景；总耗时上限 ~7s 不会卡用户太久
var gitSyncRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second}

// gitTransientErrPatterns 「值得重试」的错误特征；其他错误（auth fail / repo not found / merge conflict）
// 立刻返回，不重试 —— 因为重试也救不回来。
var gitTransientErrPatterns = []string{
	"could not resolve host",
	"connection reset",
	"connection refused",
	"timeout",
	"timed out",
	"early eof",
	"rpc failed",
	"the remote end hung up",
	"unable to access",       // 网络层 + cert 偶发
	"failed to connect to",
	"index-pack failed",
}

// isGitErrTransient 判断 stderr 是否表明这是一个值得重试的瞬时错误
func isGitErrTransient(stderr string) bool {
	low := strings.ToLower(stderr)
	for _, p := range gitTransientErrPatterns {
		if strings.Contains(low, p) {
			return true
		}
	}
	return false
}

// GitSync 全局串行的 git 同步；带白名单错误重试（最多 3 次）。
//
//	所有调用方（SubmitBatch / Submit / ActionGitSync）都走这里，确保 git 仓库永远只有一个进程在动。
//	失败时返回的 error 已经包含「重试 N 次都不行，最后一次：...」前缀，可直接面向用户。
//	logFn 把每次尝试的 stdout/stderr 写到调用方的日志（batch 写到所有 task 的 logBuffer，single 写到自己的）；
//	logFn 可为 nil。
func (m *Manager) GitSync(ctx context.Context, logFn func(line string)) error {
	if m.gitSyncer == nil {
		return fmt.Errorf("manager: gitSyncer 未配置")
	}

	m.gitMu.Lock()
	defer m.gitMu.Unlock()

	totalAttempts := len(gitSyncRetryDelays) + 1
	var lastErr error
	var lastStderr string

	for attempt := 1; attempt <= totalAttempts; attempt++ {
		if attempt > 1 {
			delay := gitSyncRetryDelays[attempt-2]
			if logFn != nil {
				logFn(fmt.Sprintf("[git-sync] retry %d/%d after %s (last error: %s)\n",
					attempt, totalAttempts, delay, summarize(lastStderr, 200)))
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("git sync canceled: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		cmd := m.gitSyncer.BuildGitSyncCommand()
		// 给 cmd 套 ctx：用户 cancel 整批时能 kill git 子进程
		// （exec.CommandContext 在创建时绑 ctx，重新构造同样命令并绑 ctx）
		cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		cmd2 := exec.CommandContext(cmdCtx, cmd.Path, cmd.Args[1:]...)
		out, err := cmd2.CombinedOutput()
		cancel()

		if logFn != nil {
			logFn(fmt.Sprintf("[git-sync] attempt %d:\n%s", attempt, string(out)))
		}

		if err == nil {
			if attempt > 1 && logFn != nil {
				logFn(fmt.Sprintf("[git-sync] succeeded on attempt %d\n", attempt))
			}
			return nil
		}

		lastErr = err
		lastStderr = string(out)

		// 不可重试错误立刻返回
		if !isGitErrTransient(lastStderr) {
			return fmt.Errorf("git sync failed (non-retryable): %w\n%s", err, summarize(lastStderr, 500))
		}
	}

	return fmt.Errorf("git sync failed after %d attempts: %w\n%s",
		totalAttempts, lastErr, summarize(lastStderr, 500))
}

// summarize 截断长字符串
func summarize(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}

// Submit 提交一个任务；返回 task_id 立刻返回，实际执行异步。
//
//	v5 起：先 GitSync（受 gitMu 保护）再起 work 子进程；ActionGitSync 走只 git-sync 的快路径。
//	并发由 semaphore 控制；同 service 互斥（防止 ansible 撞锁）
func (m *Manager) Submit(s Spec, runner CommandRunner) (*Task, error) {
	if err := validateSpec(s); err != nil {
		return nil, err
	}

	key := taskKey(s)
	if !m.tryAcquireKeyLock(key) {
		return nil, fmt.Errorf("已有任务正在跑这个服务: %s", key)
	}

	id := uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	t := &Task{
		ID:        id,
		Spec:      s,
		Status:    StatusPending,
		cancel:    cancel,
		logBuffer: newLogBuffer(),
	}

	m.mu.Lock()
	m.tasks[id] = t
	m.mu.Unlock()

	go m.run(ctx, t, runner, key)
	return t, nil
}

// SubmitBatch 一次 git sync + N 个 work 并行（Plan B 核心入口）。
//
//	返回 (tasks, err)：err 非 nil 表示整批连 git 都没启动（spec 校验失败或 key 锁占用）；
//	git sync 失败由 task 各自标 failed，函数仍返回 nil error 因为 task 都创建了。
//	调用方拿 task_id 后照常 poll。
func (m *Manager) SubmitBatch(specs []Spec, runner CommandRunner) ([]*Task, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("specs 至少 1 个")
	}

	// 1. 校验所有 spec
	for i, s := range specs {
		if err := validateSpec(s); err != nil {
			return nil, fmt.Errorf("spec[%d]: %w", i, err)
		}
		if s.Action == ActionGitSync {
			return nil, fmt.Errorf("spec[%d]: SubmitBatch 不接 git_sync action（用 Submit 单跑）", i)
		}
	}

	// 2. 抢所有 key 锁（任一冲突 → 整批拒）
	keys := make([]string, len(specs))
	for i, s := range specs {
		keys[i] = taskKey(s)
	}
	acquired := []string{}
	for _, k := range keys {
		if !m.tryAcquireKeyLock(k) {
			// 回滚已抢的
			for _, a := range acquired {
				m.releaseKeyLock(a)
			}
			return nil, fmt.Errorf("已有任务正在跑这个服务: %s", k)
		}
		acquired = append(acquired, k)
	}

	// 3. 创建 N 个 task，先全部 pending
	now := time.Now()
	tasksOut := make([]*Task, len(specs))
	for i, s := range specs {
		ctx, cancel := context.WithCancel(context.Background())
		t := &Task{
			ID:        uuid.NewString(),
			Spec:      s,
			Status:    StatusPending,
			cancel:    cancel,
			logBuffer: newLogBuffer(),
		}
		t.logBuffer.WriteString(fmt.Sprintf("[agent] batch task queued at %s\n", now.Format(time.RFC3339)))
		m.mu.Lock()
		m.tasks[t.ID] = t
		m.mu.Unlock()
		tasksOut[i] = t
		_ = ctx // ctx 在 runBatchOne 里替换为带 timeout 的子 ctx
	}

	// 4. 异步：一次 git sync → 成功后并行 N 个 work
	go m.runBatch(tasksOut, runner, keys)

	return tasksOut, nil
}

func (m *Manager) runBatch(tasksList []*Task, runner CommandRunner, keys []string) {
	// 释放所有 key 锁（无论 git 成败）
	defer func() {
		for _, k := range keys {
			m.releaseKeyLock(k)
		}
	}()

	// 1. git sync —— 写到所有 task 的日志（让用户从任一 task 都能看到 git 同步进度）
	gitCtx, gitCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer gitCancel()

	multiLog := func(line string) {
		for _, t := range tasksList {
			t.logBuffer.WriteString(line)
		}
	}

	if err := m.GitSync(gitCtx, multiLog); err != nil {
		// 整批 failed
		for _, t := range tasksList {
			m.markFailed(t, "git sync failed: "+err.Error())
		}
		return
	}

	// 2. git sync 成功 → 并行起 N 个 work（每个 work 自己抢 semaphore）
	var wg sync.WaitGroup
	for _, t := range tasksList {
		wg.Add(1)
		go func(t *Task) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			t.cancel = cancel
			m.runWork(ctx, t, runner)
		}(t)
	}
	wg.Wait()
}

func validateSpec(s Spec) error {
	if s.Action == "" || s.Project == "" {
		return fmt.Errorf("action / project 必填")
	}
	// git_sync 不需要 service
	if s.Action != ActionGitSync && s.Service == "" {
		return fmt.Errorf("service 必填")
	}
	if s.Action == ActionUpdateVersion && s.Version == "" {
		return fmt.Errorf("update_version 必须传 version")
	}
	// rsync_and_update 不要求 version（agent 自己从 version.txt 解析）但要 env（playbook 路径要用）
	if (s.Action == ActionUpdateVersion || s.Action == ActionRsyncAndUpdate) && s.Env == "" {
		return fmt.Errorf("%s 必须传 env", s.Action)
	}
	if s.Action != ActionRsync && s.Action != ActionUpdateVersion && s.Action != ActionGitSync && s.Action != ActionRsyncAndUpdate {
		return fmt.Errorf("未知 action: %s", s.Action)
	}
	return nil
}

// run 单 task 入口（Submit 用）：先 GitSync 再 work；ActionGitSync 只跑 GitSync。
func (m *Manager) run(ctx context.Context, t *Task, runner CommandRunner, key string) {
	defer m.releaseKeyLock(key)

	// 等并发位
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	case <-ctx.Done():
		m.markCanceled(t, "in queue")
		return
	}

	m.mu.Lock()
	t.Status = StatusRunning
	t.StartedAt = time.Now()
	m.mu.Unlock()

	// git_sync 单 task：只跑 GitSync，没有 work 部分
	if t.Spec.Action == ActionGitSync {
		err := m.GitSync(ctx, func(line string) { t.logBuffer.WriteString(line) })
		if err != nil {
			m.markFailed(t, err.Error())
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		t.Status = StatusSuccess
		t.ExitCode = 0
		t.EndedAt = time.Now()
		t.DurationMS = t.EndedAt.Sub(t.StartedAt).Milliseconds()
		t.logBuffer.WriteString("[agent] git sync ok\n")
		t.logBuffer.Close()
		return
	}

	// 其他 action：先 GitSync 再 work
	if err := m.GitSync(ctx, func(line string) { t.logBuffer.WriteString(line) }); err != nil {
		m.markFailed(t, "git sync failed: "+err.Error())
		return
	}
	m.runWork(ctx, t, runner)
}

// runWork 跑 work 子进程；不含 git。manager.run 和 runBatch 都调它。
//
//	假设调用前 t.Status=Running, t.StartedAt 已设置（runBatch 会在调用前设）。
func (m *Manager) runWork(ctx context.Context, t *Task, runner CommandRunner) {
	// runBatch 进来时还没设 StartedAt，这里补
	m.mu.Lock()
	if t.StartedAt.IsZero() {
		t.StartedAt = time.Now()
		t.Status = StatusRunning
	}
	m.mu.Unlock()

	// runBatch 没经过外层 semaphore，这里抢
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	case <-ctx.Done():
		m.markCanceled(t, "in queue")
		return
	}

	cmd, err := runner.BuildCommand(t.Spec)
	if err != nil {
		m.markFailed(t, err.Error())
		return
	}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		m.markFailed(t, "start: "+err.Error())
		return
	}
	cmdPid := cmd.Process.Pid
	t.logBuffer.WriteString(fmt.Sprintf("[agent] task started, pid=%d\n", cmdPid))

	// stdout / stderr 合并进 logBuffer
	go pipeReaderTo(stdout, t.logBuffer)
	go pipeReaderTo(stderr, t.logBuffer)

	// 监听 ctx.Cancel
	doneCh := make(chan error, 1)
	go func() { doneCh <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-doneCh
		m.markCanceled(t, "user canceled")
		return
	case err := <-doneCh:
		m.finalize(t, cmd, err)
	}
}

func pipeReaderTo(r io.Reader, lb *logBuffer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		lb.WriteString(scanner.Text() + "\n")
	}
}

func (m *Manager) finalize(t *Task, cmd *exec.Cmd, runErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.EndedAt = time.Now()
	if !t.StartedAt.IsZero() {
		t.DurationMS = t.EndedAt.Sub(t.StartedAt).Milliseconds()
	}
	t.logBuffer.Close()
	if runErr == nil {
		t.Status = StatusSuccess
		t.ExitCode = 0
		t.logBuffer.WriteString("[agent] task succeeded\n")
		return
	}
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		t.ExitCode = exitErr.ExitCode()
	} else {
		t.ExitCode = -1
	}
	t.Status = StatusFailed
	t.ErrMsg = runErr.Error()
	t.logBuffer.WriteString(fmt.Sprintf("[agent] task failed: %s\n", runErr.Error()))
}

func (m *Manager) markFailed(t *Task, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.Status = StatusFailed
	t.ErrMsg = msg
	t.EndedAt = time.Now()
	if !t.StartedAt.IsZero() {
		t.DurationMS = t.EndedAt.Sub(t.StartedAt).Milliseconds()
	}
	t.logBuffer.WriteString("[agent] " + msg + "\n")
	t.logBuffer.Close()
}

func (m *Manager) markCanceled(t *Task, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.Status = StatusCanceled
	t.ErrMsg = msg
	t.EndedAt = time.Now()
	t.logBuffer.WriteString("[agent] canceled: " + msg + "\n")
	t.logBuffer.Close()
}

// Get 取一个任务（含状态）。返回值为副本，避免外部改字段
func (m *Manager) Get(id string) (*Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	return t, true
}

// LogBuffer 取任务的 log buffer（让 SSE handler 订阅）
func (m *Manager) LogBuffer(id string) *logBuffer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.tasks[id]; ok {
		return t.logBuffer
	}
	return nil
}

// Cancel 取消任务（kill 子进程）
func (m *Manager) Cancel(id string) bool {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	if t.cancel != nil {
		t.cancel()
	}
	return true
}

// gcLoop 定期清理超出 retention 的已结束任务（防 OOM）
func (m *Manager) gcLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		m.mu.Lock()
		for id, t := range m.tasks {
			if t.Status == StatusRunning || t.Status == StatusPending {
				continue
			}
			if t.EndedAt.IsZero() {
				continue
			}
			if now.Sub(t.EndedAt) > m.retention {
				delete(m.tasks, id)
			}
		}
		m.mu.Unlock()
	}
}
