package services

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
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
			msg := "失败 · 等待服务部署完成超时（请到对应平台检查 Pod 状态）"
			if last != nil && last.Message != "" {
				msg = "失败 · " + sanitizeMsg(last.Message)
			} else if lastErr != nil {
				msg = "失败 · " + sanitizeMsg(lastErr.Error())
			}
			// 同步抓 pod metadata + logs + events（pod 此刻还活着）
			snaps, detail := captureFailingPodData(ctx, client, appName)
			if detail != "" {
				msg = "失败 · " + sanitizeMsg(detail)
			}
			health := ""
			if last != nil {
				health = last.Health
			}
			return &models.ArgocdAppResult{
				App:          appName,
				SyncStatus:   "Failed",
				Health:       health,
				DurationSec:  int(time.Since(start).Seconds()),
				Msg:          msg,
				LastPolledAt: time.Now(),
				FailingPods:  snaps,
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
				App:          appName,
				DurationSec:  int(time.Since(start).Seconds()),
				LastPolledAt: time.Now(),
			}
			if err == nil && st != nil {
				tick.SyncStatus = st.SyncStatus
				tick.Health = st.Health
				// Phase 状态文案——按用户能看懂的人话表达：
				//   Phase 1: 我们触发的 sync 操作还没启动 → 「初始化中」
				//   Phase 2: argocd 操作 Running，正在应用 manifests → 「正在同步配置」
				//        ↑ 此时 status.sync.status 还是上次的 "Synced"、status.health.status
				//          还是旧 pod 的 "Healthy"（滞后），需要前两列也强制改成同步中样式，
				//          让三列一致（不然用户看到 Synced/Healthy/正在同步配置 觉得矛盾）
				//   Phase 3: 操作完成但 health 还 Progressing → 「等待 Pod 就绪」
				//   Phase 4: 已 Healthy 但还没满稳定窗口 → 「已 Healthy，稳定观察中 X/N」
				switch {
				case !syncStartedAt.IsZero() && !ourOpStarted:
					tick.SyncStatus = "Syncing"
					tick.Health = "Progressing"
					tick.Msg = "初始化中"
				case isStableTick && stabilityTicks > 1 && consecutiveOK+1 < stabilityTicks:
					tick.Health = "Progressing"
					tick.Msg = "已 Healthy，稳定观察中 " +
						strconv.Itoa(consecutiveOK+1) + "/" + strconv.Itoa(stabilityTicks)
				case st.OperationPhase == "Running":
					tick.SyncStatus = "Syncing"
					tick.Health = "Progressing"
					tick.Msg = "正在同步配置"
				case st.Health == "Progressing" &&
					(st.OperationPhase == "Succeeded" || st.OperationPhase == ""):
					tick.Msg = "等待 Pod 就绪"
				case st.Message != "":
					tick.Msg = sanitizeMsg(st.Message)
				}
			} else if err != nil {
				tick.SyncStatus = "—"
				tick.Health = "—"
				tick.Msg = "查询失败 · " + sanitizeMsg(err.Error())
			}
			onTick(tick)
		}

		if err == nil {
			last = st

			// Degraded → 立即失败（pod 起不来，不等超时）。但仅当「我们这次」的操作已起来
			// 且 phase 不是 Running 才算（防止 pre-sync 的旧 Degraded 被误读）
			if ourOpStarted && st.Health == "Degraded" && st.OperationPhase != "Running" {
				msg := "失败"
				if st.Message != "" {
					msg = "失败 · " + sanitizeMsg(st.Message)
				}
				snaps, detail := captureFailingPodData(ctx, client, appName)
				if detail != "" {
					msg = "失败 · " + sanitizeMsg(detail)
				}
				return &models.ArgocdAppResult{
					App:          appName,
					SyncStatus:   st.SyncStatus,
					Health:       st.Health,
					DurationSec:  int(time.Since(start).Seconds()),
					Msg:          msg,
					LastPolledAt: time.Now(),
					FailingPods:  snaps,
				}
			}

			// 操作 Failed → 立即失败
			if ourOpStarted && (st.OperationPhase == "Failed" || st.OperationPhase == "Error") {
				msg := "失败 · 同步 " + st.OperationPhase
				if st.Message != "" {
					msg = "失败 · " + sanitizeMsg(st.Message)
				}
				snaps, detail := captureFailingPodData(ctx, client, appName)
				if detail != "" {
					msg = "失败 · " + sanitizeMsg(detail)
				}
				return &models.ArgocdAppResult{
					App:          appName,
					SyncStatus:   "Failed",
					Health:       st.Health,
					DurationSec:  int(time.Since(start).Seconds()),
					Msg:          msg,
					LastPolledAt: time.Now(),
					FailingPods:  snaps,
				}
			}

			// Pod-level fail-fast：app 还在 Progressing，但具体 pod 已经卡死（CrashLoop/ImagePullBackOff 等）
			// 不等 ArgoCD 慢吞吞的 Degraded 判定，每 30s 扫一次 pod 终态。
			// 前 30s 不扫（给应用启动时间），避免新版本第一次拉镜像慢被误杀。
			elapsed := int(time.Since(start).Seconds())
			if ourOpStarted && st.OperationPhase != "Running" && elapsed >= 30 &&
				(elapsed/intervalSec)%3 == 0 {
				if snaps, detail, fatal := detectFatalPodState(ctx, client, appName); fatal {
					// pod 此刻还活着，立刻把 logs + events 字节抓进 snapshot
					enrichSnapshotWithLogs(ctx, client, appName, snaps)
					return &models.ArgocdAppResult{
						App:          appName,
						SyncStatus:   "Failed",
						Health:       st.Health,
						DurationSec:  elapsed,
						Msg:          "失败 · " + sanitizeMsg(detail),
						LastPolledAt: time.Now(),
						FailingPods:  snaps,
					}
				}
			}

			// 稳定性计数：连续 stabilityTicks 次都 OK 才算真稳定，期间任何一次掉链
			// 立即清零（pod 短暂 Healthy 后 crash-loop → readyReplicas 回落）
			if isStableTick {
				consecutiveOK++
				if consecutiveOK >= stabilityTicks {
					return &models.ArgocdAppResult{
						App:          appName,
						SyncStatus:   st.SyncStatus,
						Health:       st.Health,
						DurationSec:  int(time.Since(start).Seconds()),
						Msg:          "",
						LastPolledAt: time.Now(),
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
				App:          appName,
				SyncStatus:   "canceled",
				DurationSec:  int(time.Since(start).Seconds()),
				Msg:          "context canceled",
				LastPolledAt: time.Now(),
			}
		case <-time.After(sleep):
		}
	}
}

// captureFailingPodData 在 fail 决策那一刻同步抓全部数据：metadata + previous logs + current logs + events
//
//	关键：pod 此刻还活着，立刻把日志+事件**字节内容**抓到 Go 变量里。
//	archive 后续只是 PutLog 上传字节，不再依赖 pod 对象存在。
//	即便用户秒回滚把失败 pod 删了，归档已经握住数据。
func captureFailingPodData(ctx context.Context, client *ArgocdClient, appName string) ([]models.FailingPodSnapshot, string) {
	snaps, detail := snapshotFailingPods(ctx, client, appName)
	enrichSnapshotWithLogs(ctx, client, appName, snaps)
	return snaps, detail
}

// enrichSnapshotWithLogs 给已有的 snapshot 列表填充 PreviousLog/CurrentLog/EventsJSON
//
//	pod 此刻必须还活着（fail 决策刚做出来）。逐个 pod 同步抓 3 种数据进 Go 变量。
//	archive 后续直接用，不再调 GetPodLogs。
func enrichSnapshotWithLogs(ctx context.Context, client *ArgocdClient, appName string, snaps []models.FailingPodSnapshot) {
	for i := range snaps {
		// previous.log（关键 —— 崩溃前的输出）
		if logs, err := client.GetPodLogs(ctx, appName, snaps[i].Namespace, snaps[i].Name, "", 2000, true); err == nil {
			snaps[i].PreviousLog = logs
		} else {
			log.Printf("[capture] app=%s pod=%s previous: %v", appName, snaps[i].Name, err)
		}
		// current.log（兜底 —— 当前容器最近 200 行）
		if logs, err := client.GetPodLogs(ctx, appName, snaps[i].Namespace, snaps[i].Name, "", 200, false); err == nil {
			snaps[i].CurrentLog = logs
		}
		// events.json（FailedScheduling/ImagePullBackOff/BackOff 等）
		if events, err := client.GetPodEvents(ctx, appName, snaps[i].UID, snaps[i].Namespace, snaps[i].Name); err == nil && len(events) > 0 {
			if data, jerr := json.Marshal(events); jerr == nil {
				snaps[i].EventsJSON = data
			}
		}
	}
}

// snapshotFailingPods 仅抓 metadata（不拉日志/事件），用于不需要归档的轻量场景
//
//	拿不到（API 失败 / 没有 Pod 节点 / 全 Healthy）→ 返回 (nil, "")。
//	单次调用同时产出快照和文案，避免 fail 路径重复打 ArgoCD。
func snapshotFailingPods(ctx context.Context, client *ArgocdClient, appName string) ([]models.FailingPodSnapshot, string) {
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := client.GetAppResourceTree(qctx, appName)
	if err != nil || len(nodes) == 0 {
		return nil, ""
	}
	var failing []ResourceNode
	for _, n := range nodes {
		if n.Kind != "Pod" {
			continue
		}
		if n.Health == "Healthy" || n.Health == "" {
			continue
		}
		failing = append(failing, n)
	}
	if len(failing) == 0 {
		// 没找到失败 pod —— 退一步看 Deployment / 其它资源的 health.message
		for _, n := range nodes {
			if (n.Kind == "Deployment" || n.Kind == "StatefulSet" || n.Kind == "Rollout") &&
				n.Health != "" && n.Health != "Healthy" && n.HealthMsg != "" {
				return nil, n.Kind + " " + n.Name + " · " + n.HealthMsg
			}
		}
		return nil, ""
	}

	snaps := make([]models.FailingPodSnapshot, 0, len(failing))
	for _, p := range failing {
		snaps = append(snaps, models.FailingPodSnapshot{
			Name:         p.Name,
			UID:          p.UID,
			Namespace:    p.Namespace,
			StatusReason: p.StatusReason,
			RestartCount: p.RestartCount,
		})
	}
	return snaps, formatPodFailureMsg(failing)
}

// formatPodFailureMsg 拼第一个失败 pod 的人话详情（多个失败 pod 通常同因；UI cell 不够长不全列）
func formatPodFailureMsg(failing []ResourceNode) string {
	if len(failing) == 0 {
		return ""
	}
	p := failing[0]
	parts := []string{"pod " + p.Name}
	if p.StatusReason != "" {
		parts = append(parts, p.StatusReason)
	}
	extras := []string{}
	if p.RestartCount != "" && p.RestartCount != "0" {
		extras = append(extras, "重启 "+p.RestartCount+" 次")
	}
	if p.ContainersOK != "" {
		extras = append(extras, p.ContainersOK+" ready")
	}
	if len(extras) > 0 {
		parts = append(parts, "（"+strings.Join(extras, "，")+"）")
	}
	out := strings.Join(parts, " ")
	if len(failing) > 1 {
		out += " · 另有 " + strconv.Itoa(len(failing)-1) + " 个 pod 同样异常"
	}
	return out
}

// enrichFailureDetail 旧接口兼容（只返字符串，不返快照）
func enrichFailureDetail(ctx context.Context, client *ArgocdClient, appName string) string {
	_, msg := snapshotFailingPods(ctx, client, appName)
	return msg
}

// detectFatalPodState 扫 pod 找永久性故障（不会自愈）
//
//	- ImagePullBackOff / ErrImagePull / CreateContainerConfigError / InvalidImageName /
//	  RegistryUnavailable → 镜像/配置问题，立即 fatal
//	- CrashLoopBackOff + RestartCount >= 3 → 应用启动有 bug，立即 fatal
//
//	返回 (snapshot, msg, isFatal)。同时把命中的 pod 收进快照供 archive 用。
//	非 fatal 状态（Pending、Progressing 等）返回 isFatal=false，让 caller 继续等。
func detectFatalPodState(ctx context.Context, client *ArgocdClient, appName string) ([]models.FailingPodSnapshot, string, bool) {
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := client.GetAppResourceTree(qctx, appName)
	if err != nil || len(nodes) == 0 {
		return nil, "", false
	}
	var failing []ResourceNode
	for _, n := range nodes {
		if n.Kind != "Pod" {
			continue
		}
		switch n.StatusReason {
		case "ImagePullBackOff", "ErrImagePull",
			"CreateContainerConfigError", "InvalidImageName", "RegistryUnavailable":
			failing = append(failing, n)
		case "CrashLoopBackOff":
			rc, _ := strconv.Atoi(n.RestartCount)
			if rc >= 3 {
				failing = append(failing, n)
			}
		}
	}
	if len(failing) == 0 {
		return nil, "", false
	}
	snaps := make([]models.FailingPodSnapshot, 0, len(failing))
	for _, p := range failing {
		snaps = append(snaps, models.FailingPodSnapshot{
			Name:         p.Name,
			UID:          p.UID,
			Namespace:    p.Namespace,
			StatusReason: p.StatusReason,
			RestartCount: p.RestartCount,
		})
	}
	return snaps, formatPodFailureMsg(failing), true
}

// sanitizeMsg 把任何透传到前端的失败/状态文案里的 "argocd"/"ArgoCD" 字眼清掉。
// 防御性兜底：argocd 自身的 health.message 通常是 k8s 风格（不含 argocd 字眼），但
// 某些插件 / 版本 / controller 错误会出现 "argocd application controller failed to..."
// 之类，统一在出口过滤。
func sanitizeMsg(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "ArgoCD", "")
	s = strings.ReplaceAll(s, "ArgoCd", "")
	s = strings.ReplaceAll(s, "argocd", "")
	s = strings.ReplaceAll(s, "Argocd", "")
	// 折叠多余空白 + 清两侧空格/标点
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.Trim(s, " ·:")
}

// isOurOperationStarted 判定 argocd 当前/最近的 sync 操作是不是「我们这次」触发的。
// 用 OperationStartedAt > syncStartedAt(-2s 容差) 来判断。容差用于 wall-clock 抖动。
func isOurOperationStarted(st *AppStatus, syncStartedAt time.Time) bool {
	if st == nil || st.OperationStartedAt.IsZero() {
		return false
	}
	return st.OperationStartedAt.After(syncStartedAt.Add(-2 * time.Second))
}
