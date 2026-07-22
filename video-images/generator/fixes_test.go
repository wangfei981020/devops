package main

import (
	"os/exec"
	"sync"
	"testing"
	"time"
)

// TestStopAllRace 复现 StopAll 与 Wait 协程对 e.done 的并发访问,
// 用 `go test -race` 跑:修复前(无锁读)会报 data race,修复后应干净通过。
func TestStopAllRace(t *testing.T) {
	m := NewManager("1/30", 5, true, 0, nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		cmd := exec.Command("sleep", "5")
		if err := cmd.Start(); err != nil {
			t.Skipf("无法启动 sleep(环境不支持): %v", err)
		}
		e := &procEntry{stream: Stream{Name: "s"}, cmd: cmd, startedAt: time.Now()}
		m.procs[e.stream.Name+string(rune(i))] = e
		// 模拟 start() 里的 Wait 协程:进程结束时在锁内写 e.done
		wg.Add(1)
		go func(e *procEntry) {
			defer wg.Done()
			_ = e.cmd.Wait()
			m.mu.Lock()
			e.done = true
			m.mu.Unlock()
		}(e)
	}

	m.StopAll() // 与上面的 Wait 协程并发读写 e.done
	wg.Wait()
}

func TestOrdinalFromHostname(t *testing.T) {
	cases := map[string]int{
		"video-images-generator-3": 3,
		"video-images-generator-0": 0,
		"pod-12":                   12,
		"nohyphen":                 0, // 无 - 分隔 → 兜底 0(并打 WARN)
		"trailing-":                0, // 尾段为空 → Atoi 失败 → 兜底 0(并打 WARN)
		"gen-abc":                  0, // 尾段非数字 → 兜底 0(并打 WARN)
	}
	for h, want := range cases {
		if got := OrdinalFromHostname(h); got != want {
			t.Errorf("OrdinalFromHostname(%q)=%d, 期望 %d", h, got, want)
		}
	}
}
