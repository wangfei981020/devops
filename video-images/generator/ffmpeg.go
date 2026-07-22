package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	backoffBase   = 5 * time.Second  // 退避起步
	backoffMax    = 2 * time.Minute  // 退避上限
	stableRunTime = 60 * time.Second // 跑够这么久算"稳定",失败计数清零
)

// Manager 持久模式:每路流一个常驻 ffmpeg,key 为输出文件名 Name(每路流唯一身份)
type Manager struct {
	mu        sync.Mutex
	fps       string
	quality   int
	skipKey   bool
	stagger   time.Duration
	metrics   *Metrics
	procs     map[string]*procEntry
	fails     map[string]int       // 连续快速失败次数(退避用)
	nextStart map[string]time.Time // 下次允许启动的时间(退避)
}

type procEntry struct {
	stream    Stream
	cmd       *exec.Cmd
	done      bool
	startedAt time.Time
}

func NewManager(fps string, quality int, skipKey bool, stagger time.Duration, m *Metrics) *Manager {
	return &Manager{fps: fps, quality: quality, skipKey: skipKey, stagger: stagger, metrics: m,
		procs: map[string]*procEntry{}, fails: map[string]int{}, nextStart: map[string]time.Time{}}
}

// Reconcile 对账:停不再归属/已死的,起缺失的(带启动错峰 + 死流退避)
func (m *Manager) Reconcile(owned []Stream) {
	now := time.Now()
	m.mu.Lock()
	desired := make(map[string]Stream, len(owned))
	for _, s := range owned {
		desired[s.Name] = s
	}
	for name, e := range m.procs {
		d, want := desired[name]
		if !want {
			log.Printf("INFO 停止不再归属的流 %s", e.stream.Name)
			delete(m.procs, name)
			delete(m.fails, name)
			delete(m.nextStart, name)
			m.terminate(e)
			continue
		}
		if e.stream.InputURL != d.InputURL {
			// 地址(域名/路径/vhost)改了 → 停掉老进程,下面按新地址重启(修"改地址不生效"的 bug)
			log.Printf("INFO 流 %s 地址变更,重启: %s => %s", name, e.stream.InputURL, d.InputURL)
			delete(m.procs, name)
			delete(m.fails, name)
			delete(m.nextStart, name)
			m.terminate(e)
			continue
		}
		if e.done {
			// 跑够久算稳定,计数清零;否则算快速失败,退避加深
			if now.Sub(e.startedAt) > stableRunTime {
				m.fails[name] = 0
			} else {
				m.fails[name]++
			}
			delay := m.backoff(m.fails[name])
			m.nextStart[name] = now.Add(delay)
			if m.metrics != nil {
				m.metrics.Restarts.Add(1)
			}
			log.Printf("WARN 流 %s ffmpeg 已退出(连续失败 %d 次),%.0fs 后重试: %s", e.stream.Name, m.fails[name], delay.Seconds(), e.stream.InputURL)
			if m.fails[name] >= 5 {
				log.Printf("WARN 流 %s 反复失败,疑似源持续异常(断流/地址错/无关键帧),已退避重试", e.stream.Name)
			}
			delete(m.procs, name)
			m.terminate(e)
		}
	}
	// 只启动到点(退避已过)且未在运行的流
	var toStart []Stream
	for name, s := range desired {
		if _, ok := m.procs[name]; ok {
			continue
		}
		if t, ok := m.nextStart[name]; ok && now.Before(t) {
			continue // 退避中,先不起
		}
		toStart = append(toStart, s)
	}
	m.mu.Unlock()

	for _, s := range toStart {
		m.start(s)
		if m.stagger > 0 {
			time.Sleep(m.stagger)
		}
	}
}

// backoff 指数退避: base * 2^(fails-1),封顶 max
func (m *Manager) backoff(fails int) time.Duration {
	if fails <= 1 {
		return backoffBase
	}
	d := backoffBase
	for i := 1; i < fails && d < backoffMax; i++ {
		d *= 2
	}
	if d > backoffMax {
		d = backoffMax
	}
	return d
}

func (m *Manager) start(s Stream) {
	out := filepath.Join(s.OutputPath, s.OutputFile)
	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	if m.skipKey {
		args = append(args, "-skip_frame", "nokey")
	}
	args = append(args,
		"-rw_timeout", "10000000",
		"-i", s.InputURL,
		"-vf", fmt.Sprintf("fps=%s,scale=360:-1", m.fps),
		"-c:v", "mjpeg", "-q:v", strconv.Itoa(m.quality),
		"-update", "1", "-threads", "1", "-an", "-f", "image2", out,
	)
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = ffmpegLogWriter{name: s.Name}
	if err := cmd.Start(); err != nil {
		log.Printf("ERROR 启动 ffmpeg 失败 %s: %v", s.Name, err)
		return
	}

	m.mu.Lock()
	e := &procEntry{stream: s, cmd: cmd, startedAt: time.Now()}
	m.procs[s.Name] = e
	m.mu.Unlock()
	log.Printf("INFO 启动 ffmpeg 流 %s: %s -> %s PID=%d", s.Name, s.InputURL, out, cmd.Process.Pid)

	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		e.done = true
		m.mu.Unlock()
		if err != nil {
			log.Printf("WARN ffmpeg 退出 %s: %v(直播流可能已断)", s.Name, err)
		}
	}()
}

func (m *Manager) terminate(e *procEntry) {
	if e.done {
		return
	}
	go func() {
		_ = e.cmd.Process.Signal(syscall.SIGTERM)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			d := e.done
			m.mu.Unlock()
			if d {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		log.Printf("WARN 流 %s 终止超时,强制 kill", e.stream.Name)
		_ = e.cmd.Process.Kill()
	}()
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	entries := make([]*procEntry, 0, len(m.procs))
	for name, e := range m.procs {
		entries = append(entries, e)
		delete(m.procs, name)
	}
	m.mu.Unlock()

	for _, e := range entries {
		m.mu.Lock()
		d := e.done // Wait 协程在锁内写 e.done,这里也须加锁读(-race)
		m.mu.Unlock()
		if !d {
			_ = e.cmd.Process.Signal(syscall.SIGTERM)
		}
	}
	time.Sleep(5 * time.Second)
	for _, e := range entries {
		m.mu.Lock()
		d := e.done
		m.mu.Unlock()
		if !d {
			_ = e.cmd.Process.Kill()
		}
	}
}

// StreamStatus 供 /streams 接口:某路流"实际在拉的地址"
type StreamStatus struct {
	Name    string `json:"name"`
	URL     string `json:"url"` // ffmpeg 实际启动时用的地址(能看出配置改了有没有生效)
	PID     int    `json:"pid"`
	Running bool   `json:"running"`
	Since   string `json:"since"`
}

// Status 返回当前实际在跑的流 + 它们真正拉的地址
func (m *Manager) Status() []StreamStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]StreamStatus, 0, len(m.procs))
	for name, e := range m.procs {
		pid := 0
		if e.cmd != nil && e.cmd.Process != nil {
			pid = e.cmd.Process.Pid
		}
		out = append(out, StreamStatus{
			Name: name, URL: e.stream.InputURL, PID: pid,
			Running: !e.done, Since: e.startedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return out
}

// ffmpegLogWriter 把 ffmpeg 的 stderr 引到统一日志,带流名
type ffmpegLogWriter struct{ name string }

func (w ffmpegLogWriter) Write(p []byte) (int, error) {
	if msg := strings.TrimSpace(string(p)); msg != "" {
		log.Printf("FFMPEG[%s] %s", w.name, msg)
	}
	return len(p), nil
}
