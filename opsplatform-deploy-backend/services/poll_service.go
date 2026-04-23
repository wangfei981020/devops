package services

import (
	"context"
	"time"

	"opsplatform-deploy-backend/models"
)

// PollUntilStable 周期查询 app 状态直到 Synced+Healthy 或超时。
// intervalSec: 每次查询间隔；timeoutSec: 最长等待。
// 返回最终状态（timeout 时 Msg 里写明）
func PollUntilStable(ctx context.Context, client *ArgocdClient, appName string, intervalSec, timeoutSec int) *models.ArgocdAppResult {
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
			// 典型场景：CrashLoopBackOff / ImagePullError / readiness probe 永远 fail
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
