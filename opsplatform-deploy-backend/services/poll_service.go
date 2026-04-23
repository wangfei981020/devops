package services

import (
	"context"
	"time"

	"opsplatform-deploy-backend/models"
)

// PollUntilStable 周期查询 app 状态直到 Synced+Healthy / Degraded / 超时。
// intervalSec: 每次查询间隔；timeoutSec: 最长等待。
//
// onTick（可为 nil）：每次 poll 完成后（不管拿到什么状态）触发一次，用于上层做渐进式
// 状态展示。传入的是"中间态快照"——App、SyncStatus、Health、DurationSec 都是当前这一刻
// 的值。上层不应该长持有这个指针，参数是栈上构造的临时对象。
//
// 返回最终状态（timeout 时 Msg 写明）。
func PollUntilStable(
	ctx context.Context,
	client *ArgocdClient,
	appName string,
	intervalSec, timeoutSec int,
	onTick func(cur *models.ArgocdAppResult),
) *models.ArgocdAppResult {
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
			} else if err != nil {
				tick.SyncStatus = "—"
				tick.Health = "—"
				tick.Msg = "query error: " + err.Error()
			}
			onTick(tick)
		}

		if err == nil {
			last = st
			// Synced + Healthy → 立即成功
			if st.SyncStatus == "Synced" && st.Health == "Healthy" {
				return &models.ArgocdAppResult{
					App:         appName,
					SyncStatus:  st.SyncStatus,
					Health:      st.Health,
					DurationSec: int(time.Since(start).Seconds()),
					Msg:         "",
				}
			}
			// Degraded → 立即失败（pod 起不来，不等超时）
			if st.Health == "Degraded" {
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
		} else {
			lastErr = err
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
