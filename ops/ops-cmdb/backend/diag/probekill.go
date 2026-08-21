package diag

import (
	"fmt"
	"regexp"
	"strings"
)

// 被存活探针杀掉 ≠ 自己崩了。
//
// # 为什么要单独判
//
// 实测 DEV 的 g32-test/share-images-frontend（重启 **17144** 次）：
//
//	容器状态   waiting / CrashLoopBackOff
//	上次退出   **0（Completed）**        ← 应用是正常退出的
//	事件       Liveness probe failed: HTTP probe failed with statuscode: 403  ×85709
//	           Readiness probe failed: ... statuscode: 403                    ×120108
//	日志末尾   nginx worker 逐个 exited with code 0，优雅关闭
//
// 真相是：**应用活得好好的，是存活探针打不通，kubelet 反复把它杀掉重启。**
// 而诊断给出的是「容器启动后即崩溃（CrashLoopBackOff）」+「查看日志末尾的报错」——
// 方向完全反了：日志里根本没有报错，人照着查会一无所获。
//
// 🔴 两者的处置南辕北辙：
//
//	真崩溃    → 查启动报错、配置、依赖
//	探针杀    → 查探针配置和应用的健康端点（403 说明**服务是活的**，只是那个路径被拒）
//
// # 判据必须紧，否则会抢走真崩溃
//
// 崩溃的容器**也会**探针失败（进程都没了，当然探不通）。所以"有探针失败事件"
// 完全不够——那正是 ruleCrashLoop 被排在探针规则前面的原因。
//
// 这里加一条硬条件：**上次退出码必须是 0 或 143**。
//
//	0    正常退出（收到 SIGTERM 后优雅关闭）
//	143  被 SIGTERM 杀死（128+15）
//
// 崩溃退出码不可能是这两个。有了它，就不会从真崩溃手里抢走判定。
var reProbeDetail = regexp.MustCompile(`(?i)liveness probe failed:\s*(.+)$`)

// ruleProbeKilled 存活探针失败导致的反复重启。
func ruleProbeKilled(c *DiagnosisContext) *DiagnosisResult {
	for i := range c.Containers {
		cc := &c.Containers[i]
		if cc.RestartCount == 0 || cc.IsInit {
			continue
		}
		// 硬条件：应用不是崩的
		code := cc.LastExitCode
		if code == nil {
			code = cc.ExitCode
		}
		if code == nil || (*code != 0 && *code != 143) {
			continue
		}

		var killing, unhealthy *EventCtx
		for j := range c.Events {
			e := &c.Events[j]
			low := strings.ToLower(e.Message)
			switch {
			// kubelet 真的动手杀了 —— 最硬的证据
			case strings.EqualFold(e.Reason, "Killing") && strings.Contains(low, "liveness probe"):
				killing = e
			// ⚠️ 只认**存活**探针：就绪探针失败只会把 Pod 摘出 Service，**不会重启容器**。
			//	把它算进来会把「服务没流量」误判成「服务在重启」
			case strings.Contains(low, "liveness probe failed"):
				if unhealthy == nil || e.Count > unhealthy.Count {
					unhealthy = e
				}
			}
		}
		if killing == nil && unhealthy == nil {
			continue
		}

		detail := ""
		if unhealthy != nil {
			if m := reProbeDetail.FindStringSubmatch(strings.TrimSpace(unhealthy.Message)); m != nil {
				detail = strings.TrimSpace(m[1])
			}
		}

		// 🔴 最要紧的一条判据：**探针必须真的探到了东西**。
		//
		// 探针拿到 HTTP 状态码（403/404/500…）→ 端口通、进程在跑，
		//	所以"反复重启"确实是探针配置的问题。
		// 探针报 connection refused / no route / timeout → **端口根本没监听**，
		//	说明应用压根没起来 —— 探针失败是**结果**，不是原因。
		//
		// ⚠️ 实测被这一条坑过：g32-dev/bi-gateway-backend 重启 12884 次，
		//	日志里明写着 MySQL 连不上导致 Spring 上下文初始化失败、应用自己退出（143），
		//	而探针报的是 connection refused。第一版规则命中了，还给出
		//	「⚠️ 别去查启动报错」—— 而日志里**就是**启动报错，这条建议是有害的。
		//
		// 所以：探不到东西的一律放行，让日志规则去判真正的根因。
		if httpStatusIn(detail) == "" && !probeGotResponse(detail) {
			continue
		}
		root := fmt.Sprintf("存活探针探不通，kubelet 反复重启容器 %s（应用本身正常退出，退出码 %d）", cc.Name, *code)
		if detail != "" {
			root = fmt.Sprintf("存活探针探不通（探针报：%s），kubelet 反复重启容器 %s —— 应用本身正常退出，退出码 %d",
				detail, cc.Name, *code)
		}

		ev := []string{fmt.Sprintf("容器 %s 重启 %d 次，但上次退出码是 %d —— 崩溃不会是这个退出码", cc.Name, cc.RestartCount, *code)}
		if unhealthy != nil {
			ev = append(ev, fmt.Sprintf("事件 %s（累计 %d 次）: %s", unhealthy.Reason, unhealthy.Count, unhealthy.Message))
		}
		if killing != nil {
			ev = append(ev, fmt.Sprintf("事件 Killing（累计 %d 次）: %s", killing.Count, killing.Message))
		}

		sol := []Solution{
			{Text: "查这个容器的 livenessProbe 配的是哪个路径和端口，再确认应用在那个端点上真的返回 2xx/3xx"},
		}
		// HTTP 状态码是最有信息量的一档，能直接指出方向
		if sc := httpStatusIn(detail); sc != "" {
			sol = append(sol, Solution{
				Text: probeStatusHint(sc),
			})
		}
		sol = append(sol,
			Solution{Text: "⚠️ 别去查启动报错：退出码说明应用没崩，日志里也不会有崩溃信息"},
			Solution{Text: "确实需要更长启动时间才调 initialDelaySeconds / failureThreshold；" +
				"探针返回明确状态码时那是配置问题，调超时没用"},
		)

		return &DiagnosisResult{
			Matched: true, Confidence: "high",
			RootCause: root,
			Evidence:  ev,
			Solutions: sol,
		}
	}
	return nil
}

var reHTTPStatus = regexp.MustCompile(`statuscode:\s*(\d{3})`)

func httpStatusIn(s string) string {
	if m := reHTTPStatus.FindStringSubmatch(strings.ToLower(s)); m != nil {
		return m[1]
	}
	return ""
}

// probeStatusHint 把探针拿到的 HTTP 状态码翻成排查方向。
//
// 🔴 关键区别：**探针能拿到状态码，说明服务是活的、端口是通的**。
// 那就不是"应用起不来"，而是"这个路径不该这么配"。
func probeStatusHint(code string) string {
	switch code {
	case "401", "403":
		return "探针拿到 " + code + " —— **服务是活的**，只是这个路径要鉴权或被拒了。" +
			"健康检查端点应当免鉴权；多为加了全局鉴权/nginx location 规则时漏了探针路径"
	case "404":
		return "探针拿到 404 —— 服务活着，但**这个路径不存在**。多为探针路径写错，" +
			"或应用换版本后健康端点改了名"
	case "500", "502", "503":
		return "探针拿到 " + code + " —— 服务在跑但内部出错（多为依赖不可用）。" +
			"这时重启治不好，要查它依赖的下游"
	default:
		return "探针拿到 HTTP " + code + " —— 服务能响应，说明进程活着；对照应用文档确认这个端点应返回什么"
	}
}

// probeGotResponse 判断探针是否真的从服务拿到了响应。
//
// 拿到响应 = 进程在跑、端口在听 → "重启"才可能是探针的锅。
// 连不上 = 应用没起来 → 探针失败只是结果，真根因在日志里。
func probeGotResponse(detail string) bool {
	low := strings.ToLower(detail)
	// 连不上的各种形态：这些一律不算"探到了"
	for _, bad := range []string{
		"connection refused", "no route to host", "i/o timeout",
		"context deadline exceeded", "timeout", "no such host",
		"connect: ", "dial tcp", "eof",
	} {
		if strings.Contains(low, bad) {
			return false
		}
	}
	// 明确拿到了 HTTP 响应或命令返回码
	return strings.Contains(low, "statuscode") ||
		strings.Contains(low, "http probe failed") ||
		strings.Contains(low, "command") && strings.Contains(low, "exit")
}
