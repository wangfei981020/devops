package main

import (
	"context"
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Snapshotter 快照模式:不常驻 ffmpeg,每 interval 对每路流连一下、抓 1 帧就断。
// 带宽 ≈ 持久模式的 ~10%,贴合"N 秒刷新一次"。信号量限并发 + 死流退避。
type Snapshotter struct {
	mu       sync.Mutex
	owned    map[string]Stream
	last     map[string]time.Time // 上次抓帧时间
	fails    map[string]int       // 连续失败次数(退避用)
	next     map[string]time.Time // 下次允许抓帧时间(退避)
	inflight map[string]bool      // 正在抓/排队中的流,不重复派发(防 goroutine 堆积)
	interval time.Duration
	timeout  time.Duration
	quality  int
	sem      chan struct{}
	metrics  *Metrics
}

func NewSnapshotter(interval, timeout time.Duration, quality, maxConc int, m *Metrics) *Snapshotter {
	if maxConc < 1 {
		maxConc = 1
	}
	return &Snapshotter{
		owned: map[string]Stream{}, last: map[string]time.Time{}, fails: map[string]int{},
		next: map[string]time.Time{}, inflight: map[string]bool{},
		interval: interval, timeout: timeout, quality: quality, sem: make(chan struct{}, maxConc), metrics: m,
	}
}

func (s *Snapshotter) SetOwned(owned []Stream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]Stream, len(owned))
	for _, st := range owned {
		next[st.Name] = st
	}
	// 清掉不再归属的流在各 map 里的残留(含 inflight,防泄漏)
	del := func(name string) {
		delete(s.last, name)
		delete(s.fails, name)
		delete(s.next, name)
		delete(s.inflight, name)
	}
	for name := range s.last {
		if _, ok := next[name]; !ok {
			del(name)
		}
	}
	for name := range s.fails {
		if _, ok := next[name]; !ok {
			del(name)
		}
	}
	for name := range s.next {
		if _, ok := next[name]; !ok {
			del(name)
		}
	}
	for name := range s.inflight {
		if _, ok := next[name]; !ok {
			del(name)
		}
	}
	s.owned = next
}

func (s *Snapshotter) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx)
		}
	}
}

func (s *Snapshotter) tick(ctx context.Context) {
	now := time.Now()
	s.mu.Lock()
	var due []Stream
	for name, st := range s.owned {
		if s.inflight[name] {
			continue // 上一次还在抓/排队,不重复派发(防 goroutine 无限堆积)
		}
		if nt, ok := s.next[name]; ok && now.Before(nt) {
			continue // 退避中
		}
		if last, ok := s.last[name]; !ok || now.Sub(last) >= s.interval {
			s.inflight[name] = true
			s.last[name] = now
			due = append(due, st)
		}
	}
	s.mu.Unlock()

	for _, st := range due {
		go s.snapshot(ctx, st)
	}
}

func (s *Snapshotter) snapshot(ctx context.Context, st Stream) {
	// 无论走哪条路径,结束都清掉 inflight 标记(让下一轮能再派发这路流)
	defer func() {
		s.mu.Lock()
		delete(s.inflight, st.Name)
		s.mu.Unlock()
	}()
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return
	}

	ctx2, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	out := filepath.Join(st.OutputPath, st.OutputFile)
	args := []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-rw_timeout", "10000000",
		"-i", st.InputURL,
		"-frames:v", "1",
		"-vf", "scale=360:-1",
		"-c:v", "mjpeg", "-q:v", strconv.Itoa(s.quality),
		"-f", "image2", "-update", "1", out,
	}
	cmd := exec.CommandContext(ctx2, "ffmpeg", args...)
	cmd.Stderr = ffmpegLogWriter{name: st.Name}
	err := cmd.Run()

	s.mu.Lock()
	_, stillOwned := s.owned[st.Name] // 已被移除/重分片走的流,不再往 map 里写(防泄漏)
	if err != nil {
		fails := s.fails[st.Name] + 1
		delay := backoffFor(fails)
		if stillOwned {
			s.fails[st.Name] = fails
			s.next[st.Name] = time.Now().Add(delay)
		}
		s.mu.Unlock()
		if s.metrics != nil {
			s.metrics.SnapFail.Add(1)
		}
		log.Printf("WARN 快照失败 %s(连续 %d 次),%.0fs 后重试: %v", st.Name, fails, delay.Seconds(), err)
		return
	}
	if stillOwned {
		s.fails[st.Name] = 0
		delete(s.next, st.Name)
	}
	s.mu.Unlock()
	if s.metrics != nil {
		s.metrics.Snaps.Add(1)
	}
}

// backoffFor 指数退避,复用与持久模式一致的参数
func backoffFor(fails int) time.Duration {
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
