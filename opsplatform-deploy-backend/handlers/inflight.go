package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"opsplatform-deploy-backend/services"
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

	// 取消注册表：deployment_id → context.CancelFunc
	// 异步发布任务启动时 RegisterCancel(depID, cancel)，结束 / 取消时 UnregisterCancel(depID)
	// 用户点「取消等待」按钮时 CancelDeployment(depID) 触发对应 cancel，让 ctx.Done() 唤醒 PollUntilStable
	inflightCancels sync.Map // map[int64]context.CancelFunc
)

// RegisterCancel 给 deployment 注册一个 cancel 钩子（通常是 ctx 的 cancel）
// 调用方负责在任务结束时调 UnregisterCancel 释放，否则会有 cancel 函数泄漏
func RegisterCancel(deploymentID int64, cancel func()) {
	if deploymentID <= 0 || cancel == nil {
		return
	}
	inflightCancels.Store(deploymentID, cancel)
}

// UnregisterCancel 任务结束时清理
func UnregisterCancel(deploymentID int64) {
	if deploymentID <= 0 {
		return
	}
	inflightCancels.Delete(deploymentID)
}

// IsTracked 该 deployment 是否有本进程内活着的发布 goroutine 在跟（cancel 已注册）。
// 定时对账用它跳过"本进程正在正常跑"的发布，只收拾真正的孤儿。
func IsTracked(deploymentID int64) bool {
	_, ok := inflightCancels.Load(deploymentID)
	return ok
}

// CancelDeployment 触发对应 deployment 的 cancel；不存在返回 false（任务可能已结束）
func CancelDeployment(deploymentID int64) bool {
	v, ok := inflightCancels.LoadAndDelete(deploymentID)
	if !ok {
		return false
	}
	if cancel, ok := v.(func()); ok && cancel != nil {
		cancel()
	}
	return true
}

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
	gateInFlight, gateWaiting, gateCap := services.GateStats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count":         InflightCount(),
		"draining":      IsDraining(),
		"gate_inflight": gateInFlight, // 正在跑的 git/helm 重活
		"gate_waiting":  gateWaiting,  // 排队等名额的
		"gate_cap":      gateCap,      // 名额上限
	})
}
