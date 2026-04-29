package tasks

import (
	"sync"
)

// logBuffer 内存日志缓冲，支持：
//   - 写入（任务输出 → buffer）
//   - 按 byte offset 读（HTTP 一次性拉某段）
//   - 订阅推送（HTTP SSE 流式拉新内容）
//
// 设计上：日志全部存内存（单任务上限 ~5MB，超过截断），任务结束后随 task 整体被 retention GC 清掉
type logBuffer struct {
	mu      sync.Mutex
	data    []byte
	closed  bool
	maxSize int                // 单任务日志上限
	subs    map[chan []byte]struct{} // 订阅者：每个 SSE 连接一个 chan
}

func newLogBuffer() *logBuffer {
	return &logBuffer{
		maxSize: 5 * 1024 * 1024, // 5MB
		subs:    make(map[chan []byte]struct{}),
	}
}

// WriteString 写入并广播给所有订阅者
func (lb *logBuffer) WriteString(s string) {
	lb.mu.Lock()
	if lb.closed {
		lb.mu.Unlock()
		return
	}
	b := []byte(s)
	// 上限保护
	if len(lb.data)+len(b) > lb.maxSize {
		remain := lb.maxSize - len(lb.data)
		if remain <= 0 {
			lb.mu.Unlock()
			return
		}
		b = b[:remain]
	}
	lb.data = append(lb.data, b...)

	// 复制 subs 切片避免 hold 锁广播
	subs := make([]chan []byte, 0, len(lb.subs))
	for ch := range lb.subs {
		subs = append(subs, ch)
	}
	lb.mu.Unlock()

	// 非阻塞广播；满了就丢（订阅者会在下次 ReadFrom 时拿到完整 since=offset 起点）
	for _, ch := range subs {
		select {
		case ch <- b:
		default:
		}
	}
}

// Close 关闭：通知所有订阅者结束、不再接收写入
func (lb *logBuffer) Close() {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if lb.closed {
		return
	}
	lb.closed = true
	for ch := range lb.subs {
		close(ch)
	}
	lb.subs = nil
}

// IsClosed 任务是否结束
func (lb *logBuffer) IsClosed() bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.closed
}

// ReadFrom 从 offset 开始读全部已有日志，返回 (数据, 当前总长度)
//
//	offset 超过 len 时返回空 slice + 当前 len
//	offset < 0 当 0 处理
func (lb *logBuffer) ReadFrom(offset int) ([]byte, int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	if offset >= len(lb.data) {
		return nil, len(lb.data)
	}
	out := make([]byte, len(lb.data)-offset)
	copy(out, lb.data[offset:])
	return out, len(lb.data)
}

// Subscribe 注册订阅；返回 chan 接收增量内容；调用方 done 时调 Unsubscribe
//
//	chan 大小 16，满了写入会丢（订阅方读得太慢的话；通过 ReadFrom 可补齐）
func (lb *logBuffer) Subscribe() chan []byte {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	ch := make(chan []byte, 16)
	if lb.closed {
		close(ch)
		return ch
	}
	lb.subs[ch] = struct{}{}
	return ch
}

// Unsubscribe 取消订阅
func (lb *logBuffer) Unsubscribe(ch chan []byte) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	if _, ok := lb.subs[ch]; ok {
		delete(lb.subs, ch)
		// 不 close ch（让订阅方自己读完缓冲）；下次 lb.Close 也不会再触
	}
}
