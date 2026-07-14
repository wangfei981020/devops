package services

import (
	"context"
	"encoding/json"
	"fmt"
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
	lastHpaHint := "" // v155(B+C): 最近一次核对到的 HPA 暂态提示，判成功时带进 Msg
	start := time.Now()
	// 双 deadline 设计（取代旧的固定 deadline）：
	//   - deadline：动态软 deadline，初始 = timeoutSec；每次 ready 数推进续 60s（最多到 hardDeadline）
	//   - hardDeadline：硬兜底 4 小时，无论如何不会超过
	// 这样 100 副本只要在稳步推进就能跑完，pod 起不来的几种致命态在下面的 fail-fast 分支立即返回。
	deadline := start.Add(time.Duration(timeoutSec) * time.Second)
	hardDeadline := start.Add(4 * time.Hour)
	if deadline.After(hardDeadline) {
		deadline = hardDeadline
	}
	// 进度追踪：lastReadyCount 推进时把 deadline 往后推 60s，停滞超过 stagnantTimeout 立即快败
	const stagnantTimeout = 5 * time.Minute
	// v132: 发布头 degradedGraceSec 秒内的 Degraded 不立即判败 —— 此刻新 RS 可能还没出现在
	// 资源树，Degraded 多半来自上一次发布残留的老崩溃 Pod（见 #1417 niuniu 误判）。
	const degradedGraceSec = 30
	lastReadyCount := -1
	lastProgressAt := start
	var last *AppStatus
	var lastErr error
	for {
		now := time.Now()
		// 硬超时兜底：4 小时铁口直断
		if now.After(hardDeadline) {
			snaps, detail := captureFailingPodData(ctx, client, appName)
			msg := "失败 · 总超时 4 小时（rollout 卡住，请到平台检查）"
			if detail != "" {
				msg = "失败 · " + sanitizeMsg(detail)
			}
			health := ""
			if last != nil {
				health = last.Health
			}
			logFailDecision(ctx, client, appName, "HARD_TIMEOUT", last, int(now.Sub(start).Seconds()))
			return &models.ArgocdAppResult{
				App:          appName,
				SyncStatus:   "Failed",
				Health:       health,
				DurationSec:  int(now.Sub(start).Seconds()),
				Msg:          msg,
				LastPolledAt: now,
				FailingPods:  snaps,
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			// v155(B+C) 到点回光：超时判败前再核对一次核心资源，已 Healthy 就判成功
			// （救掉"到点前一刻工作负载才就绪、或只被 HPA 暂态拖住"的误报）。
			if pass := tryLastMinutePass(ctx, client, appName, start); pass != nil {
				return pass
			}
			msg := "失败 · 等待服务部署完成超时（请到对应平台检查 Pod 状态）"
			if lastReadyCount > 0 {
				// 之前推进过 → 多半是中途停滞
				msg = "失败 · 进度停滞超时（当前 " +
					strconv.Itoa(lastReadyCount) + " 个 pod 已就绪，但 " +
					strconv.Itoa(int(stagnantTimeout/time.Minute)) + " 分钟未继续推进）"
			} else if last != nil && last.Message != "" {
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
			logFailDecision(ctx, client, appName, "SOFT_TIMEOUT", last, int(time.Since(start).Seconds()))
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
			// v132: 不再强求 SyncStatus=="Synced"。挂 HPA 的服务 live 副本数会被 HPA 改得跟
			// git 不一致 → ArgoCD 永久 OutOfSync，但本次 sync 操作其实已 Succeeded、工作负载也
			// Healthy，发布是成功的。强求 Synced 会把这类服务永远判失败（见 #1422 gateway 误判）。
			opOK := st.OperationPhase == "" || st.OperationPhase == "Succeeded"
			// v155(B+C): 成功判据从「app 级聚合 health」下沉到「核心资源」。
			// app 级 Health 会被 HPA 启动期暂态(FailedGetScale 找不到 deployment /
			// FailedGetResourceMetric 拿不到指标)拖成非 Healthy，导致工作负载其实已就绪却永远
			// 凑不满稳定窗口、最终超时误判失败。
			//   快路径：app 级已 Healthy → 直接算稳定（零额外开销）。
			//   慢路径：app 非 Healthy 但本次操作已成功 → 拉资源树核对「核心工作负载/网络资源」；
			//     核心全 Healthy 就算稳定，HPA 等附属的暂态只拼提示(lastHpaHint)、不否决成功。
			if ourOpStarted && opOK {
				if st.Health == "Healthy" {
					isStableTick = true
				} else if coreHealthy, coreDegraded, hint, treeOK := evaluateCoreHealth(ctx, client, appName); treeOK && coreHealthy && !coreDegraded {
					isStableTick = true
					lastHpaHint = hint
				}
			}
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
			//
			// v129: 加陈旧 condition 豁免 —— ArgoCD 报 Degraded 时不立信，先查 active RS
			// 的 pod 是否真不 Healthy。pod 都 Healthy 时说明 deployment.status.conditions
			// 残留了之前失败 sync 的旧条件，K8s 实际是 OK 的 → 跳过 fail 路径继续 poll。
			// 解决 #685 同款误判（撤销失败的 spec 时报失败）。
			if ourOpStarted && st.Health == "Degraded" && st.OperationPhase != "Running" {
				degradedElapsed := int(time.Since(start).Seconds())
				if degradedElapsed < degradedGraceSec {
					// v132: 发布头 30s 新 RS 可能还没在资源树出现，此刻 Degraded 多半源自上一次
					// 发布残留的老崩溃 Pod。给新 RS 一个出现的宽限，不立即判败（修 #1417 niuniu）。
					log.Printf("[poll] app=%s Degraded within first %ds, grace (likely stale/old-RS pod), keep polling",
						appName, degradedGraceSec)
				} else if isActiveRSPodsAllHealthy(ctx, client, appName) {
					log.Printf("[poll] app=%s reports Degraded but active RS pods all Healthy "+
						"(stale deployment.conditions?), keep polling", appName)
					// 不立即报 fail，等下一个 tick；ArgoCD 通常在新事件触发时刷新 condition
				} else {
					msg := "失败"
					if st.Message != "" {
						msg = "失败 · " + sanitizeMsg(st.Message)
					}
					snaps, detail := captureFailingPodData(ctx, client, appName)
					if detail != "" {
						msg = "失败 · " + sanitizeMsg(detail)
					}
					logFailDecision(ctx, client, appName, "DEGRADED", st, degradedElapsed)
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
				logFailDecision(ctx, client, appName, "OP_FAILED", st, int(time.Since(start).Seconds()))
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
			//
			// v128 起：去掉 OperationPhase != "Running" 条件。
			// 原因：ArgoCD operation Running 状态可能持续 5+ 分钟（等 K8s rollout 完成 ArgoCD 才肯
			// 转 Succeeded/Failed），期间 pod 已经 CrashLoopBackOff restart 6+ 次都拖到 300+s 才报失败。
			// detectFatalPodState 内部已经有足够保护（pickActiveReplicaSet 只看新 RS pod + RestartCount >= 3 阈值）。
			elapsed := int(time.Since(start).Seconds())
			if ourOpStarted && elapsed >= 30 &&
				(elapsed/intervalSec)%3 == 0 {
				snaps, detail, fatal, ready, total := inspectActiveRSPods(ctx, client, appName)
				if fatal {
					// pod 此刻还活着，立刻把 logs + events 字节抓进 snapshot
					enrichSnapshotWithLogs(ctx, client, appName, snaps)
					logFailDecision(ctx, client, appName, "POD_FATAL", st, elapsed)
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

				// === 进度追踪续命 ===
				// ready 数推进 → 重置停滞计数器 + 把 deadline 往后挪 60s（但不超过硬超时）
				// 这样 100 副本只要稳步推进就能跑完，不会被固定 timeoutSec 打断
				if ready > lastReadyCount {
					log.Printf("[poll] app=%s progress %d → %d / %d ready, refreshing deadline",
						appName, lastReadyCount, ready, total)
					lastReadyCount = ready
					lastProgressAt = now
					if newDeadline := now.Add(60 * time.Second); newDeadline.After(deadline) {
						deadline = newDeadline
					}
					if deadline.After(hardDeadline) {
						deadline = hardDeadline
					}
				}

				// === 进度停滞快败 ===
				// 已经有 pod ready 但 5 分钟内没有继续推进 → 后面的 pod 卡住了，快败不等总超时。
				// v132: 仅当高水位就绪数仍未达期望总数（lastReadyCount < total，真有 pod 没起来）
				// 才算停滞；lastReadyCount==total 是"已全部就绪=完成"，不是停滞 —— 修 5/5 误判
				// （HPA 服务全就绪却因 OutOfSync 凑不满稳定窗口，被当成"剩余 pod 起不来"砍掉）。
				if lastReadyCount > 0 && lastReadyCount < total && now.Sub(lastProgressAt) >= stagnantTimeout {
					// v155(B+C) 停滞判败前也回光一判：核心资源已 Healthy 就判成功
					if pass := tryLastMinutePass(ctx, client, appName, start); pass != nil {
						return pass
					}
					stagnantMsg := "失败 · 进度停滞 " + strconv.Itoa(int(stagnantTimeout/time.Minute)) +
						" 分钟（当前 " + strconv.Itoa(lastReadyCount) + "/" + strconv.Itoa(total) +
						" pod 就绪，剩余 pod 起不来）"
					stagSnaps, stagDetail := captureFailingPodData(ctx, client, appName)
					if stagDetail != "" {
						stagnantMsg = stagnantMsg + " · " + sanitizeMsg(stagDetail)
					}
					logFailDecision(ctx, client, appName, "STAGNATION", st, elapsed)
					return &models.ArgocdAppResult{
						App:          appName,
						SyncStatus:   "Failed",
						Health:       st.Health,
						DurationSec:  elapsed,
						Msg:          stagnantMsg,
						LastPolledAt: now,
						FailingPods:  stagSnaps,
					}
				}
			}

			// 稳定性计数：连续 stabilityTicks 次都 OK 才算真稳定，期间任何一次掉链
			// 立即清零（pod 短暂 Healthy 后 crash-loop → readyReplicas 回落）
			if isStableTick {
				consecutiveOK++
				if consecutiveOK >= stabilityTicks {
					// v132: 归一为 "Synced"。此刻真实 SyncStatus 可能是 OutOfSync（HPA 漂移），
					// 但本次发布判定为成功，统一回 Synced 让下游分类（通知/整体状态：
					// deploy_service okCount + notify_builder）一致认成功，避免"判成功但通知失败"。
					// v155(B+C): Health 归一 "Healthy"（判成功=核心资源健康；此刻 st.Health 可能被
					// HPA 暂态拖成 Degraded，直接透传会让 UI 出现"Synced/Degraded/已就绪"的矛盾）。
					// Msg 带上 HPA 暂态提示（无则空）。
					return &models.ArgocdAppResult{
						App:          appName,
						SyncStatus:   "Synced",
						Health:       "Healthy",
						DurationSec:  int(time.Since(start).Seconds()),
						Msg:          lastHpaHint,
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

// pickActiveReplicaSet 从 resource-tree 找本次发布对应的 ReplicaSet。
//
//	K8s 每次 deployment spec 改变就创建一个新 RS（pod-template-hash 后缀），
//	老 RS 跟新 RS 共存于资源树。本次发布产生的 RS = createdAt 最新的那个。
//
//	返回 nil 表示：
//	  - 应用不是 Deployment（StatefulSet / DaemonSet / 没 RS）
//	  - ArgoCD 不返回 createdAt（老版本）
//	caller 拿到 nil 应该 fallback 到看所有 pod（旧行为）。
func pickActiveReplicaSet(nodes []ResourceNode) *ResourceNode {
	var newest *ResourceNode
	for i := range nodes {
		if nodes[i].Kind != "ReplicaSet" {
			continue
		}
		if nodes[i].CreatedAt.IsZero() {
			continue // 没 createdAt 信息，没法挑最新
		}
		if newest == nil || nodes[i].CreatedAt.After(newest.CreatedAt) {
			newest = &nodes[i]
		}
	}
	return newest
}

// filterPodsByActiveRS 从 nodes 里挑 parentRefs 指向 activeRS 的 pod。
//
//	activeRS == nil → fallback：返回所有 Pod 节点（兼容老行为，
//	对 StatefulSet / 老 ArgoCD 等场景仍能工作）。
func filterPodsByActiveRS(nodes []ResourceNode, activeRS *ResourceNode) []ResourceNode {
	out := []ResourceNode{}
	for _, n := range nodes {
		if n.Kind != "Pod" {
			continue
		}
		if activeRS == nil {
			out = append(out, n)
			continue
		}
		for _, p := range n.ParentRefs {
			if p.Kind == "ReplicaSet" && p.Name == activeRS.Name {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// isActiveRSPodsAllHealthy 判 active RS 所有 pod 是否都 Healthy。
//
//	v129 用途：ArgoCD app.health = Degraded 时辨别是真不健康还是 deployment.status.conditions 残留。
//	复用 pickActiveReplicaSet + filterPodsByActiveRS（v127 已加），看 pod.Health。
//	找不到 active RS（StatefulSet 等）/ 资源树拉失败 / 无 pod → 返 false（让 Degraded 走原 fail 路径，保持兼容）。
func isActiveRSPodsAllHealthy(ctx context.Context, client *ArgocdClient, appName string) bool {
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := client.GetAppResourceTree(qctx, appName)
	if err != nil || len(nodes) == 0 {
		return false
	}
	activeRS := pickActiveReplicaSet(nodes)
	if activeRS == nil {
		return false
	}
	pods := filterPodsByActiveRS(nodes, activeRS)
	if len(pods) == 0 {
		return false
	}
	for _, p := range pods {
		if p.Health != "Healthy" {
			return false
		}
	}
	return true
}

// evaluateCoreHealth 拉资源树，判定「核心资源」是否全 Healthy，并识别 HPA 暂态提示（v155 B+C）。
//
//	核心资源（决定成功/失败）：Deployment / StatefulSet / DaemonSet / Ingress。
//	  - 工作负载(Deploy/STS/DS)：必须至少出现一个、且全部 Healthy。
//	  - Ingress：有 health 且非 Healthy 才算未就绪（很多环境 Ingress 无 health，空则不阻塞）。
//	  - Service：ArgoCD 通常不给 health，跳过（不阻塞）。
//	附属资源 HorizontalPodAutoscaler：非核心，其 Degraded/Progressing 不否决成功；仅当它非
//	  Healthy 时按 HealthMsg 里有没有 "metric" 拼一句提示（FailedGetResourceMetric→指标待就位；
//	  FailedGetScale 等→待生效）。
//
// 返回：
//
//	coreHealthy  核心工作负载/网络资源都就绪（判成功的主依据）
//	coreDegraded 核心资源里有 Degraded（真失败信号，回光时不放行）
//	hpaHint      HPA 暂态提示文案（无则空）
//	ok           资源树拉取成功（false 时调用方应忽略本次核对、按原路径走）
func evaluateCoreHealth(ctx context.Context, client *ArgocdClient, appName string) (coreHealthy, coreDegraded bool, hpaHint string, ok bool) {
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := client.GetAppResourceTree(qctx, appName)
	if err != nil || len(nodes) == 0 {
		return false, false, "", false
	}
	workloadSeen := false
	workloadAllHealthy := true
	for _, n := range nodes {
		switch n.Kind {
		case "Deployment", "StatefulSet", "DaemonSet":
			workloadSeen = true
			if n.Health != "Healthy" {
				workloadAllHealthy = false
			}
			if n.Health == "Degraded" {
				coreDegraded = true
			}
		case "Ingress":
			if n.Health != "" && n.Health != "Healthy" {
				workloadAllHealthy = false
				if n.Health == "Degraded" {
					coreDegraded = true
				}
			}
		case "HorizontalPodAutoscaler":
			if n.Health != "" && n.Health != "Healthy" {
				hpaHint = hpaHintFromMsg(n.HealthMsg)
			}
		}
	}
	coreHealthy = workloadSeen && workloadAllHealthy
	return coreHealthy, coreDegraded, hpaHint, true
}

// hpaHintFromMsg 按 HPA 的 health.message 分两态提示（不出现"自愈"字样）。
//
//	含 metric（FailedGetResourceMetric：拿不到 CPU/内存指标）→ 指标待就位；
//	其他（FailedGetScale 找不到伸缩目标等）→ 待生效。
func hpaHintFromMsg(msg string) string {
	if strings.Contains(strings.ToLower(msg), "metric") {
		return "已就绪 · HPA 指标待就位"
	}
	return "已就绪 · HPA 待生效"
}

// tryLastMinutePass 到点回光（v155 B+C）：超时/停滞判败前调它，核心资源已 Healthy 就返回成功结果，
// 否则返回 nil（让调用方继续走原判败逻辑）。核心资源有 Degraded（真坏）时不放行。
func tryLastMinutePass(ctx context.Context, client *ArgocdClient, appName string, start time.Time) *models.ArgocdAppResult {
	coreHealthy, coreDegraded, hint, ok := evaluateCoreHealth(ctx, client, appName)
	if !ok || !coreHealthy || coreDegraded {
		return nil
	}
	log.Printf("[poll] app=%s last-minute pass: core resources Healthy at deadline, treat as success (hpaHint=%q)", appName, hint)
	return &models.ArgocdAppResult{
		App:          appName,
		SyncStatus:   "Synced",
		Health:       "Healthy",
		DurationSec:  int(time.Since(start).Seconds()),
		Msg:          hint,
		LastPolledAt: time.Now(),
	}
}

// countPods 简单数下 nodes 里 Pod 节点总数（诊断日志用，看过滤前后差异）
func countPods(nodes []ResourceNode) int {
	c := 0
	for _, n := range nodes {
		if n.Kind == "Pod" {
			c++
		}
	}
	return c
}

// logFailDecision 在每条判败出口前打一行结构化诊断，把本次判定用到的全部输入定格下来。
//
//	branch: 判败分支（HARD_TIMEOUT / SOFT_TIMEOUT / DEGRADED / OP_FAILED / POD_FATAL / STAGNATION）
//	relaxed_would_pass=true 表示：按 v132 放宽判据（本次 sync 操作 Succeeded + Health Healthy
//	  + active RS 全部 Pod 就绪）本应成功 —— 即这是一次误判。上线后只要
//	  `grep 'relaxed_would_pass=true'` 就能一抓一个准，无需再回放截图反推。
//	fail 是终态且不频繁，这里多打一次 resource-tree 拉取换取"判败必有据"，值。
func logFailDecision(ctx context.Context, client *ArgocdClient, appName, branch string, st *AppStatus, elapsed int) {
	syncStatus, health, opPhase := "-", "-", "-"
	if st != nil {
		if st.SyncStatus != "" {
			syncStatus = st.SyncStatus
		}
		if st.Health != "" {
			health = st.Health
		}
		if st.OperationPhase != "" {
			opPhase = st.OperationPhase
		}
	}
	activeRSName, ready, total := "-", 0, 0
	allReady := false
	var podParts []string
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	nodes, err := client.GetAppResourceTree(qctx, appName)
	cancel()
	if err == nil && len(nodes) > 0 {
		activeRS := pickActiveReplicaSet(nodes)
		if activeRS != nil {
			activeRSName = activeRS.Name
		}
		pods := filterPodsByActiveRS(nodes, activeRS)
		total = len(pods)
		for _, p := range pods {
			if p.Health == "Healthy" {
				ready++
			}
			rc := p.RestartCount
			if rc == "" {
				rc = "0"
			}
			reason := p.StatusReason
			if reason == "" {
				reason = p.Health
			}
			if reason == "" {
				reason = "?"
			}
			podParts = append(podParts, fmt.Sprintf("%s:%s/r%s", shortPodName(p.Name), reason, rc))
		}
		allReady = total > 0 && ready == total
	}
	// relaxed_would_pass 用真实 st 算（不用上面被替换成 "-" 的展示串）
	relaxedWouldPass := false
	if st != nil {
		relaxedWouldPass = st.Health == "Healthy" &&
			(st.OperationPhase == "" || st.OperationPhase == "Succeeded") &&
			allReady
	}
	log.Printf("[fail-decision] app=%s branch=%s elapsed=%ds syncStatus=%s health=%s opPhase=%s "+
		"activeRS=%s ready=%d/%d pods=[%s] relaxed_would_pass=%v",
		appName, branch, elapsed, syncStatus, health, opPhase,
		activeRSName, ready, total, strings.Join(podParts, " "), relaxedWouldPass)
}

// shortPodName 取 pod 名最后一段（去掉 deployment-rshash- 前缀），让 [fail-decision] 行短一点
func shortPodName(name string) string {
	if i := strings.LastIndex(name, "-"); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

// snapshotFailingPods 仅抓 metadata（不拉日志/事件），用于不需要归档的轻量场景
//
//	拿不到（API 失败 / 没有 Pod 节点 / 全 Healthy）→ 返回 (nil, "")。
//	单次调用同时产出快照和文案，避免 fail 路径重复打 ArgoCD。
//
//	v127 起：只看"本次发布产生的新 RS 下的 pod"，老 RS 残留 crash pod 不算到本次 deploy 头上。
//	找不到 active RS（StatefulSet / 老 ArgoCD）时 fallback 到看所有 pod（同 v126 及之前行为）。
func snapshotFailingPods(ctx context.Context, client *ArgocdClient, appName string) ([]models.FailingPodSnapshot, string) {
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := client.GetAppResourceTree(qctx, appName)
	if err != nil || len(nodes) == 0 {
		return nil, ""
	}
	activeRS := pickActiveReplicaSet(nodes)
	pods := filterPodsByActiveRS(nodes, activeRS)
	if activeRS != nil {
		log.Printf("[poll] app=%s activeRS=%s (created %s) filtered %d→%d pods",
			appName, activeRS.Name, activeRS.CreatedAt.Format(time.RFC3339),
			countPods(nodes), len(pods))
	}
	var failing []ResourceNode
	for _, n := range pods {
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
	snaps, detail, fatal, _, _ := inspectActiveRSPods(ctx, client, appName)
	return snaps, detail, fatal
}

// inspectActiveRSPods 一次 GetAppResourceTree，同时返回：
//
//	(1) fatal pod 检测结果（同 detectFatalPodState 老语义）
//	(2) active RS 的 readyCount + totalCount —— 用于 PollUntilStable 的"进度追踪续命"
//
// readyCount = -1 表示资源树拉失败/无 active RS，调用方应忽略统计、不做进度判定。
func inspectActiveRSPods(ctx context.Context, client *ArgocdClient, appName string) (
	snaps []models.FailingPodSnapshot, detail string, isFatal bool, readyCount, totalCount int,
) {
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := client.GetAppResourceTree(qctx, appName)
	if err != nil || len(nodes) == 0 {
		return nil, "", false, -1, 0
	}
	activeRS := pickActiveReplicaSet(nodes)
	pods := filterPodsByActiveRS(nodes, activeRS)
	if activeRS != nil {
		log.Printf("[poll] app=%s activeRS=%s (created %s) filtered %d→%d pods (inspect)",
			appName, activeRS.Name, activeRS.CreatedAt.Format(time.RFC3339),
			countPods(nodes), len(pods))
	}
	totalCount = len(pods)
	var failing []ResourceNode
	for _, n := range pods {
		if n.Health == "Healthy" {
			readyCount++
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
		return nil, "", false, readyCount, totalCount
	}
	snaps = make([]models.FailingPodSnapshot, 0, len(failing))
	for _, p := range failing {
		snaps = append(snaps, models.FailingPodSnapshot{
			Name:         p.Name,
			UID:          p.UID,
			Namespace:    p.Namespace,
			StatusReason: p.StatusReason,
			RestartCount: p.RestartCount,
		})
	}
	return snaps, formatPodFailureMsg(failing), true, readyCount, totalCount
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
