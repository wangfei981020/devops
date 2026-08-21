package diag

import (
	"strings"
	"testing"
)

func i32(v int32) *int32 { return &v }

// 🔴 g32-test/share-images-frontend 实测形态：重启 17144 次，
// 但退出码是 0（Completed），真凶是存活探针 403。
// 改这一版之前给出的是「容器启动后即崩溃，去看日志报错」—— 日志里根本没有报错。
func TestProbeKillNotCrash(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{
			Name: "share-images-frontend", State: "waiting", StateReason: "CrashLoopBackOff",
			RestartCount: 17144, LastReason: "Completed", LastExitCode: i32(0),
		}},
		Events: []EventCtx{
			{Type: "Warning", Reason: "Unhealthy", Count: 120108, Message: "Readiness probe failed: HTTP probe failed with statuscode: 403"},
			{Type: "Warning", Reason: "Unhealthy", Count: 85709, Message: "Liveness probe failed: HTTP probe failed with statuscode: 403"},
		},
	}
	res := ruleProbeKilled(c)
	if res == nil {
		t.Fatal("没命中探针规则")
	}
	if !strings.Contains(res.RootCause, "存活探针探不通") || !strings.Contains(res.RootCause, "403") {
		t.Errorf("root_cause 不够具体: %s", res.RootCause)
	}
	joined := strings.Join(solTexts(res), " ")
	// 403 = 服务活着，只是路径被拒。这个判断是这条规则的全部价值
	if !strings.Contains(joined, "服务是活的") {
		t.Error("没说清 403 意味着服务活着")
	}
	if !strings.Contains(joined, "别去查启动报错") {
		t.Error("没挡住「去查启动报错」这个错误方向")
	}
}

// ⚠️ 最要紧的回归：真崩溃（退出码非 0）绝不能被探针规则抢走。
// 崩掉的容器也会探针失败——进程都没了当然探不通。
func TestRealCrashNotStolen(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{
			Name: "app", State: "waiting", StateReason: "CrashLoopBackOff",
			RestartCount: 900, LastReason: "Error", LastExitCode: i32(1),
		}},
		Events: []EventCtx{
			{Type: "Warning", Reason: "Unhealthy", Count: 5000, Message: "Liveness probe failed: connection refused"},
			{Type: "Normal", Reason: "Killing", Count: 400, Message: "Container app failed liveness probe, will be restarted"},
		},
	}
	if res := ruleProbeKilled(c); res != nil {
		t.Fatalf("退出码 1 的真崩溃被探针规则抢走了: %s", res.RootCause)
	}
}

// ⚠️ 就绪探针失败**不会重启容器**（只把 Pod 摘出 Service）。
// 把它算进来会把「服务没流量」误判成「服务在重启」。
func TestReadinessOnlyDoesNotMatch(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", RestartCount: 3, LastExitCode: i32(0)}},
		Events: []EventCtx{
			{Type: "Warning", Reason: "Unhealthy", Count: 999, Message: "Readiness probe failed: HTTP probe failed with statuscode: 503"},
		},
	}
	if res := ruleProbeKilled(c); res != nil {
		t.Fatalf("只有就绪探针失败却命中了: %s", res.RootCause)
	}
}

func TestProbeStatusHints(t *testing.T) {
	for code, want := range map[string]string{
		"404": "这个路径不存在",
		"503": "依赖不可用",
		"401": "服务是活的",
	} {
		if got := probeStatusHint(code); !strings.Contains(got, want) {
			t.Errorf("%s 的提示缺 %q: %s", code, want, got)
		}
	}
}

// 🔴 g50-test 那一族的实测日志：根因精确到 nginx 配置第 42 行。
func TestNginxConfigError(t *testing.T) {
	tail := "/docker-entrypoint.sh: Configuration complete; ready for start up\n" +
		`2026/08/19 00:19:27 [emerg] 1#1: invalid number of arguments in "proxy_pass" directive in /etc/nginx/conf.d/frontend.conf:42` + "\n" +
		`nginx: [emerg] invalid number of arguments in "proxy_pass" directive in /etc/nginx/conf.d/frontend.conf:42`
	res := ruleLogSignals(ctxWithLog("g50-lobby-pc-game-frontend", tail))
	if res == nil {
		t.Fatal("没命中")
	}
	for _, want := range []string{"nginx 配置有误", "frontend.conf", "42", "proxy_pass"} {
		if !strings.Contains(res.RootCause, want) {
			t.Errorf("root_cause 缺 %q: %s", want, res.RootCause)
		}
	}
	if !strings.Contains(strings.Join(solTexts(res), " "), "nginx -t") {
		t.Error("没给出改完先验证的办法")
	}
}

func TestMissingModule(t *testing.T) {
	for _, tail := range []string{
		"Error: Cannot find module 'express'",
		"ModuleNotFoundError: No module named 'requests'",
		"Caused by: java.lang.ClassNotFoundException: com.foo.Bar",
	} {
		res := ruleLogSignals(ctxWithLog("app", tail))
		if res == nil || !strings.Contains(res.RootCause, "找不到依赖") {
			t.Errorf("[%s] 判定不对: %+v", tail, res)
			continue
		}
		// 重启无效这件事必须说，否则人会反复重启
		if !strings.Contains(strings.Join(solTexts(res), " "), "重启无效") {
			t.Errorf("[%s] 没说清重启无效", tail)
		}
	}
}

// 🔴 g32-dev/bi-gateway-backend 实测：重启 12884 次、退出码 143、存活探针失败 ——
// 看起来完全符合"探针杀"的形态，**但真根因是 MySQL 连不上导致 Spring 启动失败**。
//
// 区分点：探针报的是 connection refused，说明**端口根本没监听**，
// 应用压根没起来 —— 探针失败是结果不是原因。
// 第一版规则命中了它，还给出「别去查启动报错」，而日志里就是启动报错。
func TestProbeRefusedIsNotProbeProblem(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{
			Name: "bi-gateway-backend", State: "running",
			RestartCount: 12884, LastReason: "Error", LastExitCode: i32(143),
		}},
		Events: []EventCtx{
			{Type: "Warning", Reason: "Unhealthy", Count: 64354,
				Message: `Liveness probe failed: Get "http://10.42.14.97:8088/actuator/health/liveness": dial tcp 10.42.14.97:8088: connect: connection refused`},
		},
	}
	if res := ruleProbeKilled(c); res != nil {
		t.Fatalf("探针连不上说明应用没起来，不该判成探针问题: %s", res.RootCause)
	}
}

// ⚠️ 反面：探针拿到了状态码 = 服务在跑，这时才是探针配置问题。
func TestProbeWithStatusCodeStillMatches(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", RestartCount: 99, LastExitCode: i32(0)}},
		Events: []EventCtx{
			{Type: "Warning", Reason: "Unhealthy", Count: 500, Message: "Liveness probe failed: HTTP probe failed with statuscode: 403"},
		},
	}
	if res := ruleProbeKilled(c); res == nil {
		t.Fatal("拿到状态码却没命中")
	}
}

// 🔴 137 = SIGKILL ≠ OOM。
// 优雅停机超时被 kubelet 强杀也是 137（探针判死、删 Pod 时最常见）。
// 原来一律说「疑似 OOM」并给三条内存方案 —— 对探针强杀是**无效建议**。
func TestExit137WithProbeIsNotOOM(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", RestartCount: 50, LastExitCode: i32(137), LastReason: "Error"}},
		Events: []EventCtx{
			{Type: "Warning", Reason: "Unhealthy", Count: 900, Message: "Liveness probe failed: dial tcp 10.0.0.1:8088: connect: connection refused"},
		},
	}
	res := ruleOOM(c)
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(res.RootCause, "更可能是 kubelet 判死后强杀") {
		t.Errorf("仍在直接断定 OOM: %s", res.RootCause)
	}
	if strings.Contains(strings.Join(solTexts(res), " "), "调高该容器 memory limit") {
		t.Error("对探针强杀仍给出「调高 memory limit」这条无效建议")
	}
}

// ⚠️ 带 OOMKilled 标记时必须仍然是确定的 OOM，方案照旧。
func TestRealOOMUnchanged(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", RestartCount: 3, LastExitCode: i32(137), LastReason: "OOMKilled"}},
	}
	res := ruleOOM(c)
	if res == nil || !strings.Contains(res.RootCause, "OOM 被杀") || res.Confidence != "high" {
		t.Fatalf("真 OOM 的判定被改坏了: %+v", res)
	}
	if !strings.Contains(strings.Join(solTexts(res), " "), "memory limit") {
		t.Error("真 OOM 却不给调 limit 的建议")
	}
}

// 🔴 g50-dev/g50-vip-cbac 实测（重启 10067 次）：日志既没报错也不是刷屏，
// 而是**直接断在**"正在连 MySQL"那一步。说出它停在哪，比"容器启动后即崩溃"有用得多。
func TestTailCutOffNamesLastAction(t *testing.T) {
	tail := "config: [redis], key: [mode], value: [single]\n" +
		"SetServerPrefix prefix=g50-vip-cbac-game-server\n" +
		"redisClientUniversalInit ping success\n" +
		"[DBCrypt.go:90] [DBADecryptPeb 解密]解密结果=******\n" +
		"[conf.go:507] GetMySqlGameDb username=root, host=mysql-8.public:3306, database=g50_vip_cbac_game_dev"
	noErr, rep := tailLooksUninformative(tail)
	if !noErr || rep {
		t.Fatalf("应该是「无报错且不重复」: noError=%v repetitive=%v", noErr, rep)
	}
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "CrashLoopBackOff", RestartCount: 10067, LastExitCode: i32(1)}},
		LogTails:   map[string]string{"app": tail},
	}
	res := ruleCrashLoop(c)
	joined := strings.Join(solTexts(res), " ")
	for _, want := range []string{"直接断掉", "GetMySqlGameDb", "被外部强制终止"} {
		if !strings.Contains(joined, want) {
			t.Errorf("方案里缺 %q:\n%s", want, joined)
		}
	}
}
