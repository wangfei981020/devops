package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ops-cmdb-backend/diag"
	"ops-cmdb-backend/logx"
)

// 事件的历史回查：集群上过期了，CMDB 自己还存着。
//
// # 为什么必须有这一层
//
// K8s 事件默认只留 1 小时。一个挂了很久的 Pod，带原因的那条事件早就没了，
// 集群上只剩无穷无尽的 `BackOff` 重试记录 —— 诊断只能说「判不出来」。
//
// 实测 DEV/metersphere2，13 个 Pod 是这个下场。而 CMDB 的 k8s_events 表里
// 那条原因**好好地存着**：
//
//	Failed to pull image "docker.io/bitnami/kubectl:1.28.2-debian-11-r16":
//	rpc error: code = NotFound ... : not found
//
// 最能说明问题的是：同一批里另外 2 个 Pod（事件恰好还没过期）就正常判出来了。
// **同一个根因，13 个判不出、2 个判出，差别只在事件有没有过期** ——
// 而过期的那一份，我们自己采着。
//
// # 🔴 它在成本分层里的位置
//
//	0   规则        实时状态+事件+日志 → 精确根因              免费
//	1   证据提炼    从更大日志窗口捞报错行                     免费
//	1.5 历史回查    实时的没了 → 查 CMDB 自己采的历史          免费  ← 本文件
//	2   AI 兜底     连历史里都没有 → 才花钱                    贵
//
// CMDB 是唯一一个**既能实时查集群、又存了历史**的地方。不用它就等于白采了，
// 而且会把本来免费能解决的问题推给 AI 去花钱。
//
// ⚠️ 日志侧早就有 fillLogsFromLoki 走这个思路（kubelet 取不到时退到 Loki），
// 事件侧一直没有 —— 同一个道理只做了一半。

// eventHistoryLimit 回查多少条。够判根因即可，不是越多越好：
// 塞太多历史事件会把"此刻的状态"淹掉。
const eventHistoryLimit = 12

// hasInformativeEvent 判断实时事件里有没有"带原因"的那种。
//
// 🔴 判据不能只看「事件列表非空」。挂久了的 Pod 事件列表往往很长，
// 但**全是 BackOff/Pulled 这类重试记录**，一条原因都没有 ——
// 那种情况下"有事件"和"没事件"对诊断是一样的。
func hasInformativeEvent(evs []diag.EventCtx) bool {
	for _, e := range evs {
		r := strings.ToLower(e.Reason)
		// 这几种是"重试/进度"，不含原因
		if r == "backoff" || r == "pulled" || r == "pulling" || r == "created" ||
			r == "started" || r == "scheduled" || r == "killing" {
			continue
		}
		return true
	}
	return false
}

// fillEventsFromCMDB 实时事件里没有"带原因"的那条时，从 CMDB 采集的历史里补。
//
// ⚠️ 补进来的一律打 Historical 标记：历史事件说的是「当时」不是「此刻」。
// 不标的话，人会把几天前的报错当成现在的状态。
func (h *K8sDiagHandler) fillEventsFromCMDB(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	if hasInformativeEvent(dc.Events) {
		return // 实时的就够用，不查库
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT type, reason, message, count, last_at
		FROM k8s_events
		WHERE cluster_id=? AND namespace=? AND obj_name=?
		ORDER BY last_at DESC LIMIT ?`, cid, dc.Namespace, dc.PodName, eventHistoryLimit)
	if err != nil {
		// ⚠️ 查库失败不能静默：诊断会因此少一份证据，而结论看起来照常正常
		logx.J("k8s", "event_history_failed", map[string]any{
			"cluster_id": cid, "ns": dc.Namespace, "pod": dc.PodName, "err": err.Error(),
		})
		return
	}
	defer rows.Close()

	added := 0
	for rows.Next() {
		var typ, reason, msg string
		var cnt int32
		var lastAt *time.Time
		if rows.Scan(&typ, &reason, &msg, &cnt, &lastAt) != nil {
			continue
		}
		e := diag.EventCtx{
			Type: typ, Reason: reason, Message: msg, Count: cnt, Historical: true,
		}
		if lastAt != nil {
			e.LastSeen = *lastAt
		}
		dc.Events = append(dc.Events, e)
		added++
	}
	if added == 0 {
		return
	}
	logx.J("k8s", "event_history_filled", map[string]any{
		"cluster_id": cid, "ns": dc.Namespace, "pod": dc.PodName, "added": added,
	})

	// 🔴 把「这些是历史」写进上下文，让规则产出的证据里带上这句话。
	//	规则本身不知道事件从哪来，而使用者必须知道 ——
	//	否则一条三天前的报错会被当成此刻的状态去处置。
	dc.EventNote = fmt.Sprintf(
		"⚠️ 集群上的实时事件里已经没有原因了（K8s 事件默认只留 1 小时），"+
			"下面 %d 条来自 **CMDB 采集的历史**，说的是「当时」不是「此刻」。"+
			"要确认现在是否仍是这个原因，删掉这个 Pod 让它重建即可拿到新事件", added)
}
