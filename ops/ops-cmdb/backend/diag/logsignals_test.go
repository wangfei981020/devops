package diag

import (
	"strings"
	"testing"
)

// 用**生产上真实抓到的日志**做输入。构造的日志容易只测到自己写的正则，
// 真实日志才会暴露转义色码、大小写、多行夹杂这些实际形态。

// gitlabRunnerTail 取自 UAT cicd/gitlab-runner-dev（当时已重启 5431 次）。
// 注意末尾是 PANIC 和 Quit —— 真根因（x509）在它们**上面**。
const gitlabRunnerTail = "\x1b[0;33mWARNING: Running in user-mode.                    \x1b[0;m \n" +
	"Merging configuration from template file \"/configmaps/config.template.toml\"\x1b[0;m \n" +
	"\x1b[31;1mERROR: Verifying runner... failed                 \x1b[0;m  \x1b[31;1mrunner\x1b[0;m=Tfy3t9ykz " +
	"\x1b[31;1mstatus\x1b[0;m=execute JSON request: execute request: couldn't execute POST against " +
	"https://gitlab-g32uat.slileisure.com/api/v4/runners/verify: Post \"https://gitlab-g32uat.slileisure.com/api/v4/runners/verify\": " +
	"tls: failed to verify certificate: x509: certificate has expired or is not yet valid: " +
	"current time 2026-08-18T10:27:31Z is after 2026-06-26T03:18:25Z\n" +
	"\x1b[31;1mPANIC: Failed to verify the runner.               \x1b[0;m \n" +
	"Quit"

// ksAPIServerTail 取自 UAT kubesphere-system/ks-apiserver（已重启 4977 次）。
const ksAPIServerTail = `W0818 10:28:25.402633       1 client_config.go:659] Neither --kubeconfig nor --master was specified.  Using the inClusterConfig.  This might not work.
W0818 10:28:25.403323       1 cache.go:48] In-memory cache will be used, this may cause data inconsistencies when running with multiple replicas.
E0818 10:28:25.416834       1 run.go:72] "command failed" err="unable to create cluster client: no matches for kind \"Cluster\" in version \"cluster.kubesphere.io/v1alpha1\""`

func ctxWithLog(container, tail string) *DiagnosisContext {
	return &DiagnosisContext{
		Containers: []ContainerCtx{{Name: container, State: "waiting", StateReason: "CrashLoopBackOff", RestartCount: 5431}},
		LogTails:   map[string]string{container: tail},
	}
}

// 🔴 这一条是整个改动的理由：改之前它只能得到「容器异常退出（退出码 1）」。
func TestCertExpiredBeatsPanic(t *testing.T) {
	res := ruleLogSignals(ctxWithLog("gitlab-runner-dev", gitlabRunnerTail))
	if res == nil {
		t.Fatal("没命中任何信号")
	}
	// 必须点名域名和过期时刻，不能只说"证书有问题"
	for _, want := range []string{"证书已过期", "gitlab-g32uat.slileisure.com", "2026-06-26T03:18:25Z"} {
		if !strings.Contains(res.RootCause, want) {
			t.Errorf("root_cause 里缺 %q，实际: %s", want, res.RootCause)
		}
	}
	// ⚠️ 信号优先级必须压过行位置：PANIC 那行更靠后，但它是泛化信号
	if strings.Contains(res.RootCause, "panic") || strings.Contains(res.RootCause, "PANIC") {
		t.Errorf("选中了更靠后的 PANIC 行而不是 x509：%s", res.RootCause)
	}
	// 方案要能直接点过去，而不是让人自己想去哪查
	var linked bool
	for _, s := range res.Solutions {
		if strings.Contains(s.Link, "/resources/certs") && strings.Contains(s.Link, "gitlab-g32uat") {
			linked = true
		}
		if strings.Contains(s.Text, "查看上面日志") {
			t.Errorf("方案又把活推回给人了: %s", s.Text)
		}
	}
	if !linked {
		t.Error("没给出跳转到证书页的链接")
	}
}

func TestCRDMissing(t *testing.T) {
	res := ruleLogSignals(ctxWithLog("ks-apiserver", ksAPIServerTail))
	if res == nil {
		t.Fatal("没命中任何信号")
	}
	for _, want := range []string{"Cluster", "cluster.kubesphere.io/v1alpha1"} {
		if !strings.Contains(res.RootCause, want) {
			t.Errorf("root_cause 里缺 %q，实际: %s", want, res.RootCause)
		}
	}
	if !strings.Contains(strings.Join(solTexts(res), " "), "kubectl get crd") {
		t.Error("方案里没给出确认 CRD 的具体命令")
	}
}

func TestConnRefusedExtractsAddress(t *testing.T) {
	tail := `2026/08/18 10:00:00 failed to connect: dial tcp 10.4.3.21:5432: connect: connection refused`
	res := ruleLogSignals(ctxWithLog("app", tail))
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(res.RootCause, "10.4.3.21:5432") {
		t.Errorf("没抽出目标地址: %s", res.RootCause)
	}
	// 「拒绝」和「超时」的排查方向相反，这句区分必须在
	if !strings.Contains(strings.Join(solTexts(res), " "), "和防火墙拦截") {
		t.Error("没有把「拒绝 vs 超时」的区别说出来")
	}
}

// JVM 堆内 OOM 与容器被内核 OOMKilled 是两回事，方案不能混。
func TestJVMOOMNotContainerOOM(t *testing.T) {
	tail := "Exception in thread \"main\" java.lang.OutOfMemoryError: Java heap space\n\tat com.foo.Bar.main(Bar.java:12)"
	res := ruleLogSignals(ctxWithLog("app", tail))
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(res.RootCause, "JVM 堆内存耗尽") {
		t.Errorf("root_cause: %s", res.RootCause)
	}
	if !strings.Contains(strings.Join(solTexts(res), " "), "只调容器 memory limit 对堆内 OOM 无效") {
		t.Error("没说清调 limit 无效这件事")
	}
}

// ⚠️ 回归：日志里没有任何已知特征时必须放行，让后面的通用规则接手。
// 这条规则如果乱认，会把所有崩溃都盖成一个错误的根因。
func TestNoSignalFallsThrough(t *testing.T) {
	if res := ruleLogSignals(ctxWithLog("app", "starting up\nlistening on :8080\nready")); res != nil {
		t.Fatalf("不该命中，却给了: %s", res.RootCause)
	}
}

// ⚠️ 回归：读不到日志时不能命中（也就不会把「没读到」说成「没问题」）。
func TestNoLogNoMatch(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app"}},
		LogTails:   map[string]string{},
		LogErrors:  map[string]string{"app": "无权限读日志(HTTP 403)"},
	}
	if res := ruleLogSignals(c); res != nil {
		t.Fatalf("没有日志却命中了: %s", res.RootCause)
	}
}

// 🔴 多容器 Pod 每次诊断结论必须一样。
// 原 ruleDNSFailure 遍历的是 map（迭代顺序随机），多容器时结论会来回跳。
func TestMultiContainerDeterministic(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{
			{Name: "istio-proxy"},
			{Name: "app"},
		},
		LogTails: map[string]string{
			"istio-proxy": "warn: dial tcp 127.0.0.1:15000: connect: connection refused",
			"app":         "panic: something bad",
		},
	}
	first := ruleLogSignals(c).RootCause
	for i := 0; i < 50; i++ {
		if got := ruleLogSignals(c).RootCause; got != first {
			t.Fatalf("同一输入两次结论不同:\n  %s\n  %s", first, got)
		}
	}
	// conn-refused 优先级高于 process-panic，所以应当选中 istio-proxy 那条
	if !strings.Contains(first, "连接被拒绝") {
		t.Errorf("信号优先级没按表走: %s", first)
	}
}

// 日志取不到时，通用规则必须把「为什么取不到」写进证据 ——
// 否则人打开看到日志是空的，会得出「日志里没东西」然后往别处查。
func TestLogGapIsStated(t *testing.T) {
	c := &DiagnosisContext{
		Phase:      "Running",
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "CrashLoopBackOff", RestartCount: 9}},
		LogTails:   map[string]string{},
		LogErrors:  map[string]string{"app": "无权限读日志(HTTP 403)：当前凭据缺 pods/log 的 get 权限"},
	}
	res := ruleCrashLoop(c)
	if res == nil {
		t.Fatal("CrashLoopBackOff 没命中")
	}
	joined := strings.Join(res.Evidence, "\n")
	if !strings.Contains(joined, "没能读到这个容器的日志") || !strings.Contains(joined, "403") {
		t.Errorf("没说出日志读不到及原因:\n%s", joined)
	}
}

func solTexts(r *DiagnosisResult) []string {
	out := make([]string, 0, len(r.Solutions))
	for _, s := range r.Solutions {
		out = append(out, s.Text)
	}
	return out
}

// 🔴 kubelet 用 HTTP 200 + 一句占位符表示「没有日志可给」。
// 把它当成日志内容存下去，就是把「读不到」伪装成「读到了」——
// 规则拿着垃圾匹配不到任何东西，而界面显示得有模有样。本地实测被骗过一次。
func TestLogPlaceholderIsNotContent(t *testing.T) {
	cases := map[string]bool{
		"unable to retrieve container logs for containerd://8cdfe489333df0": true,
		"failed to try resolving symlinks in path /var/log/pods/x":          true,
		"":                  false,
		"panic: real crash": false,
		// ⚠️ 真日志里偶然出现这句话不该被吞：那时它前后还有别的行
		"starting\nunable to retrieve container logs for containerd://x\ndone": false,
	}
	for in, want := range cases {
		if got := isLogPlaceholder(in); got != want {
			t.Errorf("isLogPlaceholder(%q) = %v, want %v", in, got, want)
		}
	}
}

// 🔴 public/nacos-v1 实测日志（重启 8528 次）。
//
// 这一条抓的是「匹配用小写、提取用原文」的 bug：
// 信号命中了 `caused by:`，但提取正则没带 (?i)，对原文匹配不上，
// build 返回 nil 被跳过 —— **整条信号静默失效**，落回通用 CrashLoop。
func TestJavaCauseMixedCase(t *testing.T) {
	tail := "\t... 42 common frames omitted\n" +
		"Caused by: com.alibaba.nacos.api.exception.NacosException: Nacos Server did not start because dumpservice bean construction failure :\n" +
		"No DataSource set\n" +
		"Caused by: java.lang.IllegalStateException: No DataSource set\n" +
		"\tat org.springframework.util.Assert.state(Assert.java:76)"
	res := ruleLogSignals(ctxWithLog("container-lw1f94", tail))
	if res == nil {
		t.Fatal("没命中任何信号 —— 提取正则的大小写口径又对不上了")
	}
	// 依赖类信号优先级更高，应当命中它而不是泛化的 java-exception
	if !strings.Contains(res.RootCause, "依赖不可用") && !strings.Contains(res.RootCause, "DataSource") {
		t.Errorf("root_cause 不够具体: %s", res.RootCause)
	}
}

// 🔴 大小写口径必须一致：这三条信号的提取正则都写成了小写，
// 而日志里是混合大小写。逐条钉住。
func TestExtractorsAreCaseInsensitive(t *testing.T) {
	cases := map[string]string{
		"Caused by: java.lang.IllegalStateException: boom":                         "IllegalStateException",
		"panic: runtime error: index out of range":                                 "runtime error",
		"Exception in thread \"main\" java.lang.OutOfMemoryError: Java heap space": "JVM 堆内存耗尽",
	}
	for tail, want := range cases {
		res := ruleLogSignals(ctxWithLog("app", tail))
		if res == nil {
			t.Errorf("[%s] 没命中", tail)
			continue
		}
		if !strings.Contains(res.RootCause, want) {
			t.Errorf("[%s] root_cause 缺 %q: %s", tail, want, res.RootCause)
		}
	}
}

// 🔴 启动阶段依赖不可用 ≠ 应用有问题。方案必须指向依赖。
func TestStartupDependencyPointsAtDependency(t *testing.T) {
	tail := "Error creating bean with name 'gatewayRouteController': Could not open JDBC Connection for transaction"
	res := ruleLogSignals(ctxWithLog("app", tail))
	if res == nil || !strings.Contains(res.RootCause, "数据库连不上") {
		t.Fatalf("判定不对: %+v", res)
	}
	joined := strings.Join(solTexts(res), " ")
	if !strings.Contains(joined, "先修**依赖**") {
		t.Error("没把矛头指向依赖")
	}
	// 重启次数高不代表应用有问题——这句必须在，否则人会去重启/回滚应用
	if !strings.Contains(joined, "重启次数再高也说明不了应用有问题") {
		t.Error("没说清「重启次数高 ≠ 应用有问题」")
	}
}

// 🔴 g50-dev/g50-plaza 实测：退出码 2、重启 11205 次，
// 日志末尾 30 行全是每 3 秒一条的 redis health check success。
// 这时说「查看上面日志末尾定位报错」是一句废话——末尾全是 success。
func TestUninformativeTailIsCalledOut(t *testing.T) {
	lines := []string{}
	for i := 0; i < 20; i++ {
		lines = append(lines, "2026/08/19 01:41:0"+string(rune('0'+i%10))+" [   INFO] [wrapper.go:265] [116] redis health check success")
	}
	tail := strings.Join(lines, "\n")

	noErr, rep := tailLooksUninformative(tail)
	if !noErr || !rep {
		t.Fatalf("没识别出「刷屏且无报错」: noError=%v repetitive=%v", noErr, rep)
	}

	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", State: "terminated", StateReason: "Error", ExitCode: i32(2), RestartCount: 11205}},
		LogTails:   map[string]string{"app": tail},
	}
	res := ruleNonZeroExit(c)
	if res == nil {
		t.Fatal("没命中")
	}
	joined := strings.Join(solTexts(res), " ")
	if strings.Contains(joined, "查看上面日志末尾定位报错") {
		t.Error("末尾全是 success，却还让人去看日志末尾找报错")
	}
	for _, want := range []string{"没有任何报错", "已被顶出去", "query_loki"} {
		if !strings.Contains(joined, want) {
			t.Errorf("方案里缺 %q: %s", want, joined)
		}
	}
}

// ⚠️ 反面：末尾确实有报错时，照常让人去看日志，别乱改建议。
func TestInformativeTailKeepsOriginalAdvice(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", State: "terminated", StateReason: "Error", ExitCode: i32(1), RestartCount: 5}},
		LogTails:   map[string]string{"app": "starting\nloading config\nFATAL: something broke\nbye"},
	}
	res := ruleNonZeroExit(c)
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(strings.Join(solTexts(res), " "), "查看上面日志末尾定位报错") {
		t.Error("末尾有报错却不让人去看了")
	}
}

// 🔴 顺序：日志里同时有 conn-refused 和 JDBC 启动失败时，
// 必须给出更有信息量的那条（bi-gateway 实测形态）。
func TestStartupDependencyBeatsConnRefused(t *testing.T) {
	tail := "Caused by: java.net.ConnectException: Connection refused\n" +
		"\tat java.base/sun.nio.ch.Net.connect0(Native Method)\n" +
		"Exception encountered during context initialization - cancelling refresh attempt: " +
		"org.springframework.beans.factory.UnsatisfiedDependencyException: Error creating bean with name " +
		"'uidGenerator' defined in class path resource [com/sl/config/BaiduUidConfig.class]: " +
		"Could not open JDBC Connection for transaction"
	res := ruleLogSignals(ctxWithLog("bi-gateway-backend", tail))
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(res.RootCause, "启动阶段依赖不可用") {
		t.Errorf("被泛化的 conn-refused 抢走了: %s", res.RootCause)
	}
}

// ⚠️ 反面：只有 conn-refused、没有启动失败迹象时，仍应判成连接被拒。
func TestPlainConnRefusedStillWorks(t *testing.T) {
	res := ruleLogSignals(ctxWithLog("app", "2026/08/19 dial tcp 10.4.3.21:5432: connect: connection refused"))
	if res == nil || !strings.Contains(res.RootCause, "连接被拒绝") {
		t.Fatalf("判定不对: %+v", res)
	}
}

// 🔴 g66-dev/g66-baccarat-resource-backend 实测（重启 3311 次）：
// 直接用 IP 连 Redis，证书没有 IP SAN。
// 原来的证书规则只认 "certificate is valid for X, not Y" 这一种措辞，整条漏掉。
func TestCertNoIPSAN(t *testing.T) {
	tail := "2026-08-19 02:14:04 [ERROR] redis.go:67 redis initialization failed, error: " +
		"tls: failed to verify certificate: x509: cannot validate certificate for 10.170.80.6 " +
		"because it doesn't contain any IP SANs\n" +
		"redis initialization failed, program exit: tls: failed to verify certificate: x509: " +
		"cannot validate certificate for 10.170.80.6 because it doesn't contain any IP SANs"
	res := ruleLogSignals(ctxWithLog("app", tail))
	if res == nil {
		t.Fatal("没命中")
	}
	for _, want := range []string{"IP SAN", "10.170.80.6"} {
		if !strings.Contains(res.RootCause, want) {
			t.Errorf("root_cause 缺 %q: %s", want, res.RootCause)
		}
	}
	if !strings.Contains(strings.Join(solTexts(res), " "), "换成 Service 域名") {
		t.Error("没给出最常见的正解（IP 换域名）")
	}
}

// 🔴 g50-test/g50-link-baccarat 实测（重启 9016 次）：镜像里缺 .so。
// missing-module 只覆盖 Node/Python/Java，C/Go 动态链接整类漏掉。
func TestMissingSharedLibrary(t *testing.T) {
	res := ruleLogSignals(ctxWithLog("app",
		"./app: error while loading shared libraries: libsignature.so: cannot open shared object file: No such file or directory"))
	if res == nil || !strings.Contains(res.RootCause, "缺动态链接库") {
		t.Fatalf("判定不对: %+v", res)
	}
	if !strings.Contains(res.RootCause, "libsignature.so") {
		t.Errorf("没抽出库名: %s", res.RootCause)
	}
	if !strings.Contains(strings.Join(solTexts(res), " "), "重启无效") {
		t.Error("没说清重启无效")
	}
}

// 🔴 UAT trivy-system 实测：应用自己打了 FATAL，那行就是它的自述。
func TestFatalLineExtracted(t *testing.T) {
	tail := `2026-08-19T01:56:26Z	FATAL	Fatal error	image scan error: unable to find the specified image ` +
		`"harbor.slileisure.com/bitnami/postgresql:11.11.0": UNAUTHORIZED: project bitnami not found`
	res := ruleLogSignals(ctxWithLog("app", tail))
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(res.RootCause, "致命错误") || !strings.Contains(res.RootCause, "bitnami") {
		t.Errorf("没把 FATAL 行的内容抽出来: %s", res.RootCause)
	}
}

// ⚠️ FATAL 是泛化兜底，不能抢走更具体的信号。
func TestFatalDoesNotStealSpecific(t *testing.T) {
	tail := "FATAL something bad\n" +
		"nginx: [emerg] invalid number of arguments in \"proxy_pass\" directive in /etc/nginx/conf.d/a.conf:3"
	res := ruleLogSignals(ctxWithLog("app", tail))
	if res == nil || !strings.Contains(res.RootCause, "nginx 配置有误") {
		t.Fatalf("FATAL 抢走了更具体的 nginx 判定: %+v", res)
	}
}

// TestInvisibleCharInQuotedIdent 用**真实生产日志原文**测：
// g32-dev/niuniu-game-server-backend 重启 1344 次，真根因是
// TIDB_DATABASE 的值末尾多了一个换行符（OPSCMDB-064）。
//
// 🔴 这条测试的价值在于**双向**：既要认出换行符那条，
// 又不能把普通的凭据错误也说成"含不可见字符"。
func TestInvisibleCharInQuotedIdent(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantInvis bool
		wantIdent string
	}{
		{
			name:      "库名末尾带换行（生产原文）",
			line:      "Error 1044 (42000): Access denied for user 'g32_ndev'@'%' to database 'g33_niuniu_dev\n'",
			wantInvis: true,
			wantIdent: "g33_niuniu_dev\n",
		},
		{
			name:      "库名末尾带制表符",
			line:      "Access denied for user 'app'@'%' to database 'mydb\t'",
			wantInvis: true,
			wantIdent: "mydb\t",
		},
		{
			// 反向：真的凭据错误不能被改判
			name:      "普通凭据错误",
			line:      "Error 1045 (28000): Access denied for user 'app'@'10.0.0.1' (using password: YES)",
			wantInvis: false,
		},
		{
			// 反向：库名里带空格是**合法**的，报出来会是误判
			name:      "库名含空格不算",
			line:      "Access denied for user 'app'@'%' to database 'my db'",
			wantInvis: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ident, ch := quotedIdentWithInvisible(tc.line)
			if tc.wantInvis {
				if ident != tc.wantIdent {
					t.Fatalf("标识符 = %q，want %q", ident, tc.wantIdent)
				}
				if ch == "" {
					t.Fatal("认出了标识符却没说是哪种字符")
				}
			} else if ident != "" {
				t.Fatalf("不该报，却报了 %q（%s）", ident, ch)
			}
		})
	}
}

// TestAuthFailedRoutesToInvisibleChar 端到端：同一条规则要能分流到两种根因。
func TestAuthFailedRoutesToInvisibleChar(t *testing.T) {
	run := func(tail string) *DiagnosisResult {
		return ruleLogSignals(&DiagnosisContext{
			Containers: []ContainerCtx{{Name: "app"}},
			LogTails:   map[string]string{"app": tail},
		})
	}
	real := "Error 1044 (42000): Access denied for user 'g32_ndev'@'%' to database 'g33_niuniu_dev\n'"
	r := run(real)
	if r == nil || !r.Matched {
		t.Fatal("没匹配上 auth-failed 规则")
	}
	if !strings.Contains(r.RootCause, "不可见字符") {
		t.Fatalf("根因判成了 %q —— 按这个建议查永远查不出来（OPSCMDB-064）", r.RootCause)
	}
	// 反向：普通凭据错误仍要走原路径
	plain := "Error 1045 (28000): Access denied for user 'app'@'10.0.0.1' (using password: YES)"
	r2 := run(plain)
	if r2 == nil || !strings.Contains(r2.RootCause, "凭据不对") {
		t.Fatalf("普通凭据错误被改判了：%+v", r2)
	}
}
