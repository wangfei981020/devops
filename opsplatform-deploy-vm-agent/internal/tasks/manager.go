package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
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
	ActionRsync         Action = "rsync"
	ActionUpdateVersion Action = "update_version"
	ActionGitSync       Action = "git_sync" // 单独跑一次 git pull（diagnostic 用）
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

// Manager 管理所有 task：调度、并发限制、生命周期、日志缓冲
type Manager struct {
	mu            sync.RWMutex
	tasks         map[string]*Task
	semaphore     chan struct{}    // 并发限制
	retention     time.Duration    // 完成任务保留时长
	taskKeyLocks  map[string]bool  // <project>:<env>:<service> 互斥（同 service 不并发）
	taskKeyLockMu sync.Mutex
}

// NewManager 创建 manager；maxConcurrent 限制全局并发，retention 保留完成任务的时长
func NewManager(maxConcurrent int, retention time.Duration) *Manager {
	m := &Manager{
		tasks:        make(map[string]*Task),
		semaphore:    make(chan struct{}, maxConcurrent),
		retention:    retention,
		taskKeyLocks: make(map[string]bool),
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

// Submit 提交一个任务；返回 task_id 立刻返回，实际执行异步
//
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

func validateSpec(s Spec) error {
	if s.Action == "" || s.Project == "" || s.Service == "" {
		return fmt.Errorf("action / project / service 必填")
	}
	if (s.Action == ActionUpdateVersion) && s.Version == "" {
		return fmt.Errorf("update_version 必须传 version")
	}
	if (s.Action == ActionUpdateVersion) && s.Env == "" {
		return fmt.Errorf("update_version 必须传 env")
	}
	if s.Action != ActionRsync && s.Action != ActionUpdateVersion && s.Action != ActionGitSync {
		return fmt.Errorf("未知 action: %s", s.Action)
	}
	return nil
}

// run 在 goroutine 里跑任务：先抢 semaphore，再 spawn 子进程，pipe stdout/stderr 进 logBuffer
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

	cmd, err := runner.BuildCommand(t.Spec)
	if err != nil {
		m.markFailed(t, err.Error())
		return
	}
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	m.mu.Lock()
	t.Status = StatusRunning
	t.StartedAt = time.Now()
	m.mu.Unlock()

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
