package diag

import (
	"strings"
	"testing"
)

// 🔴 DEV node12 实测形态：节点磁盘 100%，一批 Pod 卡在拉镜像。
// 逐个诊断的话每个都得到"镜像拉不下来"——局部正确、方向全错。
func TestNodePressureBeatsPodSymptom(t *testing.T) {
	c := &DiagnosisContext{
		Namespace: "g66-test", PodName: "frontend-136-x",
		NodeName: "node12", NodePressure: "DiskPressure",
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "ContainerCreating"}},
	}
	res := RuleProvider{}.Diagnose(c)
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(res.RootCause, "根因在**节点**不在这个 Pod") {
		t.Fatalf("没把根因指向节点: %s", res.RootCause)
	}
	if !strings.Contains(res.RootCause, "node12") {
		t.Errorf("没点名是哪个节点: %s", res.RootCause)
	}
	joined := strings.Join(solTexts(res), " ")
	// 最有价值的一句：别再逐个查
	if !strings.Contains(joined, "别再逐个查") {
		t.Error("没阻止「一个个查 Pod」这个错误方向")
	}
	// ImageGC 那个坑必须说
	if !strings.Contains(joined, "0 bytes eligible") {
		t.Error("没说清「清镜像没用」")
	}
	// ⚠️ cordon 可以，drain 要警告
	if !strings.Contains(joined, "别急着 drain") {
		t.Error("没提醒 drain 会连锁")
	}
}

// 🔴 判据必须紧：节点有压力 + 应用自己崩了，两件事没关系。
// 抢走真正的 Pod 故障比不命中更糟。
func TestNodePressureDoesNotStealAppCrash(t *testing.T) {
	c := &DiagnosisContext{
		NodeName: "node12", NodePressure: "MemoryPressure",
		Containers: []ContainerCtx{{
			Name: "app", State: "waiting", StateReason: "CrashLoopBackOff",
			RestartCount: 99, LastExitCode: i32(1),
		}},
		LogTails: map[string]string{"app": "nginx: [emerg] invalid number of arguments in \"proxy_pass\" directive in /etc/nginx/conf.d/a.conf:3"},
	}
	res := RuleProvider{}.Diagnose(c)
	if strings.Contains(res.RootCause, "根因在**节点**") {
		t.Fatalf("节点压力抢走了应用自己的配置错: %s", res.RootCause)
	}
	if !strings.Contains(res.RootCause, "nginx 配置有误") {
		t.Errorf("应该判成 nginx 配置错: %s", res.RootCause)
	}
}

// 没有节点压力时不该命中。
func TestNoNodePressureNoMatch(t *testing.T) {
	c := &DiagnosisContext{
		NodeName:   "node1",
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "ContainerCreating"}},
	}
	if res := ruleNodeUnderPressure(c); res != nil {
		t.Fatalf("没有压力却命中: %s", res.RootCause)
	}
}

// 🔴 config_audit 的结论必须进根因，而不是只说"配置引用有问题"。
func TestConfigIssuesNamed(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "CreateContainerConfigError"}},
		ConfigIssues: []string{
			"secret ls-tie-baccarat-game-db 的键 TIDB_HOST（来源：env）不存在",
			"secret ls-nacos 的键 REGISTER_HOST（来源：env）不存在",
		},
	}
	res := RuleProvider{}.Diagnose(c)
	for _, want := range []string{"ls-tie-baccarat-game-db", "TIDB_HOST"} {
		if !strings.Contains(res.RootCause, want) {
			t.Errorf("root_cause 缺 %q: %s", want, res.RootCause)
		}
	}
	// 有确定结论时不该再让人自己去调 config_audit
	if strings.Contains(strings.Join(solTexts(res), " "), "用 config_audit 查") {
		t.Error("已经点名了还让人自己去查")
	}
}

// 没拿到 config_audit 结论时，要指路过去，而不是干巴巴一句"检查一下"。
func TestConfigIssuesFallbackPointsToAudit(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "CreateContainerConfigError"}},
	}
	res := RuleProvider{}.Diagnose(c)
	if !strings.Contains(strings.Join(solTexts(res), " "), "config_audit") {
		t.Error("没指路到 config_audit")
	}
}

// 🔴 变更关联要用**真实镜像变更**，而不是只看 restartedAt 注解。
// 发版是换 tag，那个动作不写注解 —— 而"是不是刚发版引入的"是最先要问的。
func TestImageChangeOverridesAnnotation(t *testing.T) {
	c := &DiagnosisContext{
		Containers:  []ContainerCtx{{Name: "app", State: "waiting", StateReason: "CrashLoopBackOff", RestartCount: 5, LastExitCode: i32(1)}},
		LogTails:    map[string]string{"app": "panic: boom"},
		ImageChange: "🔴 2026-08-19 10:20 换过镜像（site-frontend）：site-frontend:v1 → site-frontend:v2 —— 先确认是不是这次发版引入的，是则回滚止血",
	}
	res := RuleProvider{}.Diagnose(c)
	if res.Change == nil || !res.Change.Related {
		t.Fatal("有镜像变更却没关联上")
	}
	if res.Change.Source != "deploy" {
		t.Errorf("来源应标 deploy，实际 %s", res.Change.Source)
	}
	if !strings.Contains(res.Change.Summary, "site-frontend:v2") {
		t.Errorf("没给出换到哪一版: %s", res.Change.Summary)
	}
}

// 🔴 OOM 时给实测峰值，而不是让人拍脑袋加倍。
func TestOOMUsesMeasuredUsage(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", RestartCount: 3, LastExitCode: i32(137), LastReason: "OOMKilled"}},
		MemUsage:   "近 6 小时峰值 1832 MiB（container_memory_working_set_bytes）",
	}
	res := RuleProvider{}.Diagnose(c)
	joined := strings.Join(res.Evidence, " ")
	if !strings.Contains(joined, "1832 MiB") {
		t.Errorf("证据里没有实测用量: %s", joined)
	}
	if !strings.Contains(solTexts(res)[0], "按上面的实测峰值定新 limit") {
		t.Errorf("没把「按实测定 limit」放在第一条: %v", solTexts(res))
	}
}
