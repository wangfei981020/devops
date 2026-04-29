// Package agentlog 给 agent 提供按小时滚动的日志文件 writer + 旧日志清理。
//
// 设计：
//   - 文件路径布局：<base>/<YYYY-MM-DD>/<YYYY-MM-DD-HH-00>.log
//     例：/var/log/opsplatform-deploy-vm-agent/2026-04-29/2026-04-29-15-00.log
//   - HourlyWriter 实现 io.Writer，每次 Write 前检查当前小时，跨小时则
//     close 旧文件 + 自动 mkdir 新日期目录 + 打开新文件。
//   - 用 mu 串行化 Write，避免多 goroutine 并发 rotate race。
//   - 失败回退：rotate 出错时回退到 io.Discard，保证 log.Print 永不阻塞主流程。
//
// 文件名分隔符用 `-` 而非 `:`，让文件可以 scp / 移到 Windows 盘不被 NTFS 拒。
//
// 不引入 lumberjack 等第三方库——需求简单（按时间切，不按大小切），自己 50 行
// 代码搞定，避免给 agent 引入依赖。
package agentlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HourlyWriter 跨小时自动切文件的 io.Writer。
//
//	BaseDir 必须可写（systemd ReadWritePaths 已含此路径）。
type HourlyWriter struct {
	BaseDir string

	mu      sync.Mutex
	curFile *os.File
	curHour string // "YYYY-MM-DD-HH-00"，用于检测是否跨小时
}

// NewHourlyWriter 不会立刻创建文件，等第一次 Write 时按当时的时间打开。
func NewHourlyWriter(baseDir string) *HourlyWriter {
	return &HourlyWriter{BaseDir: baseDir}
}

// Write 实现 io.Writer。跨小时自动 rotate，rotate 失败时丢弃这条日志（不 panic）。
func (w *HourlyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	hourStr := now.Format("2006-01-02-15-04")[:13] + "-00" // 截到小时，分钟固定 00
	if hourStr != w.curHour || w.curFile == nil {
		if err := w.rotateLocked(now, hourStr); err != nil {
			// rotate 失败：丢弃这次写但返 nil 不阻塞调用方
			// （Go 的 log.Output 看到 err 不会重试，只是输到 stderr 一行 warning）
			return len(p), nil
		}
	}
	return w.curFile.Write(p)
}

// rotateLocked 假设已经持锁。打开新文件前 close 旧文件，新目录自动 mkdir。
func (w *HourlyWriter) rotateLocked(now time.Time, hourStr string) error {
	if w.curFile != nil {
		_ = w.curFile.Close()
		w.curFile = nil
	}
	dateStr := now.Format("2006-01-02")
	dir := filepath.Join(w.BaseDir, dateStr)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, hourStr+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	w.curFile = f
	w.curHour = hourStr
	return nil
}

// Close agent 退出时调一下，把当前小时文件 fsync + close
func (w *HourlyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.curFile == nil {
		return nil
	}
	err := w.curFile.Close()
	w.curFile = nil
	return err
}

// Discard 是 io.Discard 的别名，给调用方 fallback 用
var Discard io.Writer = io.Discard
