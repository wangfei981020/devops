package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
)

// inflight tracker：滚动升级时让 preStop 知道还有多少异步发布任务在跑，
// 跑完了才让 K8s 发 SIGTERM。
//
//   - draining: 进入 drain 状态后，HandleUpdateImage/Restart/Rollback 拒新请求
//   - wg:       每开一个异步 goroutine 就 Add(1)，结束 Done()
//   - count:    并发安全计数（仅供 /internal/inflight 暴露）
var (
	draining atomic.Bool
	wg       sync.WaitGroup
	counter  atomic.Int32
)

// IsDraining 业务 handler 在接到新发布请求时调用，true 表示拒收
func IsDraining() bool { return draining.Load() }

// MarkDraining 进入 drain 状态。preStop hook 通过 /internal/drain 触发；
// main.go 收到 SIGTERM 时也兜底调用一次。
func MarkDraining() { draining.Store(true) }

// InflightTrack 包裹一个发布 goroutine：调用方 go InflightTrack(func(){ ... })
// 内部会负责 wg.Add/Done 和 counter ++/--
func InflightTrack(job func()) {
	wg.Add(1)
	counter.Add(1)
	go func() {
		defer wg.Done()
		defer counter.Add(-1)
		defer func() {
			// goroutine panic 不能拖垮 wg 计数；recover 避免 leak
			if r := recover(); r != nil {
				// 业务侧的 RecoverMiddleware 只兜 HTTP 请求；这里是裸 goroutine，需要单独 recover
				// 不重新 panic，避免影响其它 in-flight 任务
				_ = r
			}
		}()
		job()
	}()
}

// InflightCount 当前正在跑的发布任务数（用于 /internal/inflight）
func InflightCount() int32 { return counter.Load() }

// WaitInflight 阻塞直到所有 in-flight 任务跑完。仅用于进程优雅关闭兜底（preStop 之外的最后保险）。
func WaitInflight() { wg.Wait() }

// ---- HTTP endpoints (mounted on health server, no auth) ----

// HandleInternalDrain  GET/POST /internal/drain
//
//	preStop hook 调用，置 draining=true。从此 HandleUpdateImage/Restart/Rollback 拒新请求。
//	方法不区分 — Alpine BusyBox wget 默认 GET，避免 --post-data 兼容性问题。
func HandleInternalDrain(w http.ResponseWriter, r *http.Request) {
	draining.Store(true)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"draining": true})
}

// HandleInternalInflight  GET /internal/inflight
//
//	preStop hook 轮询直到 count == 0 才退出，让 K8s 才发 SIGTERM。
func HandleInternalInflight(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count":    InflightCount(),
		"draining": IsDraining(),
	})
}
