package services

import (
	"context"
	"strconv"
	"time"

	"opsplatform-deploy-backend/models"
)

// PollUntilStable 周期查询 app 状态直到「我们触发的这次 sync 操作」完成 + Synced+Healthy
// 持续稳定 N 次才返回成功；Degraded 立即失败；超时按最后状态返回。
//
// **两层修正**：
// 1. argocd Sync API 是异步的，调完立即返回。如果立刻去 poll，常会拿到上一次 sync 的
//    旧状态（Synced+Healthy 旧 pod 还跑着），被误判为 0s 成功。靠 `syncStartedAt`
//    + `OperationStartedAt` 比对识别「我们这次」的操作。
// 2. pod 可能 readinessProbe 通过短暂 Healthy → app panic / livenessProbe 失败 →
//    CrashLoopBackOff。如果第一次 poll 命中 Healthy 就返回，30s 后通知出去时 pod
//    实际在重启。靠 `stabilityTicks` 要求**连续** N 次都 Healthy 才确认成功。
//
// intervalSec: 每次查询间隔；timeoutSec: 最长等待。
// syncStartedAt: 调 argocd Sync API 之前那一刻的 wall-clock；用于判定 OperationState
//   是不是这次 sync 触发的（避免命中上一次的 Succeeded）。零值表示不做该校验
//   （restart 路径用零值，因为 resource action 不更新 OperationState）。
// stabilityTicks: 需要连续观测到 success 状态多少次才算真稳定。<=1 退化为旧行为
//   （首次 Healthy 就返回）。默认推荐 6（配合 5s interval = 30s 稳定窗口）。
// onTick: 每次 poll 完成后（不管拿到什么状态）触发一次，用于上层渐进式状态展示。
func PollUntilStable(
	ctx context.Context,
	client *ArgocdClient,
	appName string,
	intervalSec, timeoutSec int,
	syncStartedAt time.Time,
	stabilityTicks int,
	onTick func(cur *models.ArgocdAppResult),
) *models.ArgocdAppResult {
	if stabilityTicks < 1 {
		stabilityTicks = 1
	}
	consecutiveOK := 0
	start := time.Now()
	deadline := start.Add(time.Duration(timeoutSec) * time.Second)
	var last *AppStatus
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			msg := "timeout waiting for Synced+Healthy"
			if last != nil {
				msg = "timeout: last " + last.SyncStatus + "/" + last.Health
				if last.OperationPhase != "" {
					msg += " op=" + last.OperationPhase
				}
				if last.Message != "" {
					msg += " · " + last.Message
				}
			} else if lastErr != nil {
				msg = "timeout: " + lastErr.Error()
			}
			status := "timeout"
			health := ""
			if last != nil {
				status = last.SyncStatus
				health = last.Health
			}
			return &models.ArgocdAppResult{
				App:         appName,
				SyncStatus:  status,
				Health:      health,
				DurationSec: int(time.Since(start).Seconds()),
				Msg:         msg,
			}
		}

		qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		st, err := client.GetAppStatus(qctx, appName)
		cancel()

		// 计算当前是否符合「我们触发的成功状态」
		isStableTick := false
		ourOpStarted := false
		if err == nil && st != nil {
			ourOpStarted = syncStartedAt.IsZero() || isOurOperationStarted(st, syncStartedAt)
			isStableTick = ourOpStarted &&
				st.SyncStatus == "Synced" && st.Health == "Healthy" &&
				(st.OperationPhase == "" || st.OperationPhase == "Succeeded")
		}

		// 上报当前 tick 状态（无论查询成功或失败），让上层能够实时看到状态推进
		if onTick != nil {
			tick := &models.ArgocdAppResult{
				App:         appName,
				DurationSec: int(time.Since(start).Seconds()),
			}
			if err == nil && st != nil {
				tick.SyncStatus = st.SyncStatus
				tick.Health = st.Health
				if st.Message != "" {
					tick.Msg = st.Message
				}
				// 如果"我们这次"的操作还没启动，即使 Health 是 Healthy 也算 Progressing
				// 给前端展示，避免误导
				if !syncStartedAt.IsZero() && !ourOpStarted {
					tick.Health = "Progressing"
					if tick.Msg == "" {
						tick.Msg = "等待 ArgoCD 处理新 revision"
					}
				}
				// 在稳定窗口内（已 Healthy 但还没满 N 次）显式告知前端「稳定中 X/N」
				if isStableTick && stabilityTicks > 1 && consecutiveOK+1 < stabilityTicks {
					tick.Health = "Progressing"
					tick.Msg = "已 Healthy，稳定观察中 " +
						strconv.Itoa(consecutiveOK+1) + "/" + strconv.Itoa(stabilityTicks)
				}
			} else if err != nil {
				tick.SyncStatus = "—"
				tick.Health = "—"
				tick.Msg = "query error: " + err.Error()
			}
			onTick(tick)
		}

		if err == nil {
			last = st

			// Degraded → 立即失败（pod 起不来，不等超时）。但仅当「我们这次」的操作已起来
			// 且 phase 不是 Running 才算（防止 pre-sync 的旧 Degraded 被误读）
			if ourOpStarted && st.Health == "Degraded" && st.OperationPhase != "Running" {
				msg := "degraded"
				if st.Message != "" {
					msg = "degraded: " + st.Message
				}
				return &models.ArgocdAppResult{
					App:         appName,
					SyncStatus:  st.SyncStatus,
					Health:      st.Health,
					DurationSec: int(time.Since(start).Seconds()),
					Msg:         msg,
				}
			}

			// 操作 Failed → 立即失败
			if ourOpStarted && (st.OperationPhase == "Failed" || st.OperationPhase == "Error") {
				msg := "argocd sync " + st.OperationPhase
				if st.Message != "" {
					msg += ": " + st.Message
				}
				return &models.ArgocdAppResult{
					App:         appName,
					SyncStatus:  st.SyncStatus,
					Health:      st.Health,
					DurationSec: int(time.Since(start).Seconds()),
					Msg:         msg,
				}
			}

			// 稳定性计数：连续 stabilityTicks 次都 OK 才算真稳定，期间任何一次掉链
			// 立即清零（pod 短暂 Healthy 后 crash-loop → readyReplicas 回落）
			if isStableTick {
				consecutiveOK++
				if consecutiveOK >= stabilityTicks {
					return &models.ArgocdAppResult{
						App:         appName,
						SyncStatus:  st.SyncStatus,
						Health:      st.Health,
						DurationSec: int(time.Since(start).Seconds()),
						Msg:         "",
					}
				}
			} else {
				consecutiveOK = 0
			}
		} else {
			lastErr = err
			consecutiveOK = 0 // 查询出错也清零稳定计数
		}

		sleep := time.Duration(intervalSec) * time.Second
		if sleep > remaining {
			sleep = remaining
		}
		select {
		case <-ctx.Done():
			return &models.ArgocdAppResult{
				App:         appName,
				SyncStatus:  "canceled",
				DurationSec: int(time.Since(start).Seconds()),
				Msg:         "context canceled",
			}
		case <-time.After(sleep):
		}
	}
}

// isOurOperationStarted 判定 argocd 当前/最近的 sync 操作是不是「我们这次」触发的。
// 用 OperationStartedAt > syncStartedAt(-2s 容差) 来判断。容差用于 wall-clock 抖动。
func isOurOperationStarted(st *AppStatus, syncStartedAt time.Time) bool {
	if st == nil || st.OperationStartedAt.IsZero() {
		return false
	}
	return st.OperationStartedAt.After(syncStartedAt.Add(-2 * time.Second))
}
