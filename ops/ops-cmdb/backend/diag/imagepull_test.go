package diag

import (
	"strings"
	"testing"
	"time"
)

// 事件取自 DEV 集群实测（cluster_id=6）。

func pullCtx(ns, image string, evs ...EventCtx) *DiagnosisContext {
	return &DiagnosisContext{
		Namespace:  ns,
		Containers: []ContainerCtx{{Name: "app", Image: image, State: "waiting", StateReason: "ImagePullBackOff"}},
		Events:     evs,
		LogTails:   map[string]string{},
	}
}

// 🔴 g50-uat 那 29 个 Pod 的真实形态：根因是缺拉取密钥，和镜像存不存在无关。
func TestPullSecretMissingNamesTheSecret(t *testing.T) {
	res := ruleImagePullDetailed(pullCtx(
		"g50-uat",
		"dev-harbor.slleisure.com/g50-uat/g50-classic-baccarat-game-frontend:20260515081809-21",
		EventCtx{Type: "Warning", Reason: "FailedToRetrieveImagePullSecret", Count: 19647,
			Message: "Unable to retrieve some image pull secrets (harbor-id); attempting to pull the image may not succeed."},
	))
	if res == nil {
		t.Fatal("没命中")
	}
	for _, want := range []string{"g50-uat", "harbor-id", "不存在"} {
		if !strings.Contains(res.RootCause, want) {
			t.Errorf("root_cause 缺 %q：%s", want, res.RootCause)
		}
	}
	joined := strings.Join(solTexts(res), " ")
	// 必须挡住"去查仓库"这个错误方向 —— 镜像连拉都没拉
	if !strings.Contains(joined, "别去查仓库") {
		t.Error("没有挡住「去查仓库」这个错误排查方向")
	}
	if !strings.Contains(joined, "kubectl -n g50-uat get secret harbor-id") {
		t.Error("没给出可直接执行的确认命令")
	}
}

// 🔴 g32-dev/maxwin24d 的真实形态：只剩 BackOff（Normal 类型！），
// 原因事件已过 TTL。必须诚实说判不出来。
func TestOnlyBackOffAdmitsUnknown(t *testing.T) {
	image := "dev-harbor.slleisure.com/g32-dev/maxwin24d-game-server-backend:20260310180408-12"
	res := ruleImagePullDetailed(pullCtx("g32-dev", image,
		// ⚠️ Normal 而不是 Warning —— 原来的 eventContaining 只看 Warning，读不到它
		EventCtx{Type: "Normal", Reason: "BackOff", Count: 255314,
			Message: `Back-off pulling image "` + image + `"`},
	))
	if res == nil {
		t.Fatal("没命中")
	}
	if !strings.Contains(res.RootCause, "判不出来") {
		t.Errorf("没有承认判不出来：%s", res.RootCause)
	}
	if res.Confidence != "low" {
		t.Errorf("判不出来时 confidence 应为 low，实际 %s", res.Confidence)
	}
	// 用户明确要的：把完整地址摆出来让人手动验
	joined := strings.Join(solTexts(res), " ")
	if !strings.Contains(joined, image) {
		t.Errorf("没把完整镜像地址给出来：%s", joined)
	}
	// ⚠️ 不能再给那三条通用建议假装分析过了
	if strings.Contains(joined, "核对镜像名/tag 是否正确") {
		t.Error("又退回三条通用建议了")
	}
	// 证据里必须说清「查不到 ≠ 没有报错」
	if !strings.Contains(strings.Join(res.Evidence, " "), "不是没有报错") {
		t.Error("没说清「原始报错查不到」和「没有报错」的区别")
	}
}

// 🔴 措辞用**运行时实际吐的原话**，不用凭印象写的。
// 第一版只认 registry 协议的 "manifest unknown"，而 containerd 说的是
// "code = NotFound … : not found" —— 端到端跑的时候当场没命中。
func TestImageNotFound(t *testing.T) {
	image := "docker.io/library/busybox:this-tag-does-not-exist-9z9z"
	realMsg := `Failed to pull image "` + image + `": rpc error: code = NotFound desc = ` +
		`failed to pull and unpack image "` + image + `": failed to resolve reference "` + image + `": ` +
		image + `: not found`

	for name, msg := range map[string]string{
		"containerd 实测原话": realMsg,
		"registry 协议措辞":   `Failed to pull image "` + image + `": manifest unknown`,
		"docker 措辞":       `Failed to pull image "` + image + `": manifest for ` + image + ` not found`,
	} {
		res := ruleImagePullDetailed(pullCtx("g32-dev", image,
			EventCtx{Type: "Warning", Reason: "Failed", Count: 12, Message: msg}))
		if res == nil || !strings.Contains(res.RootCause, "仓库里没有这个镜像或 tag") {
			t.Errorf("[%s] 判定不对: %+v", name, res)
			continue
		}
		if !strings.Contains(strings.Join(solTexts(res), " "), "构建流水线") {
			t.Errorf("[%s] 没指向构建流水线（多为没推上去）", name)
		}
	}
}

// ⚠️ "secret … not found" 绝不能被判成「镜像不存在」——
// 两者的排查方向完全相反：一个去仓库查，一个去命名空间建密钥。
func TestSecretNotFoundIsNotImageNotFound(t *testing.T) {
	res := ruleImagePullDetailed(pullCtx("g32-dev", "dev-harbor.slleisure.com/g32-dev/foo:v1",
		EventCtx{Type: "Warning", Reason: "Failed",
			Message: `Failed to pull image: secret "harbor-id" not found`}))
	if res != nil && strings.Contains(res.RootCause, "仓库里没有这个镜像") {
		t.Errorf("把缺密钥判成了镜像不存在: %s", res.RootCause)
	}
}

func TestUnauthorizedVsUnreachable(t *testing.T) {
	image := "dev-harbor.slleisure.com/g32-dev/foo:v1"
	auth := ruleImagePullDetailed(pullCtx("g32-dev", image,
		EventCtx{Type: "Warning", Reason: "Failed", Message: `Failed to pull image: unauthorized: authentication required`}))
	if !strings.Contains(auth.RootCause, "拒绝了这次拉取") {
		t.Errorf("凭据类判错: %s", auth.RootCause)
	}
	net := ruleImagePullDetailed(pullCtx("g32-dev", image,
		EventCtx{Type: "Warning", Reason: "Failed", Message: `Failed to pull image: dial tcp 10.0.0.1:443: i/o timeout`}))
	if !strings.Contains(net.RootCause, "连不上镜像仓库") {
		t.Errorf("网络类判错: %s", net.RootCause)
	}
	// 两类的排查方向完全不同，不能给一样的方案
	if strings.Join(solTexts(auth), "") == strings.Join(solTexts(net), "") {
		t.Error("凭据问题和网络问题给了同样的方案")
	}
}

// ⚠️ 回归：不是拉镜像失败的 Pod，这条规则必须放行。
func TestNotImagePullFallsThrough(t *testing.T) {
	c := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "CrashLoopBackOff"}},
	}
	if res := ruleImagePullDetailed(c); res != nil {
		t.Fatalf("不该命中: %s", res.RootCause)
	}
}

// 🔴 DEV/metersphere2 实测：13 个 Pod 集群上只剩 BackOff，
// 而 CMDB 库里存着带原因的那条。回查历史后必须能判出来 ——
// 但**必须标明是历史**，否则一条三天前的报错会被当成此刻的状态。
func TestHistoricalEventUsedButLabeled(t *testing.T) {
	image := "docker.io/bitnami/kubectl:1.28.2-debian-11-r16"
	c := pullCtx("metersphere2", image,
		// 实时的只剩重试记录
		EventCtx{Type: "Normal", Reason: "BackOff", Count: 784335,
			Message: `Back-off pulling image "` + image + `"`},
		// 这条来自 CMDB 历史
		EventCtx{Type: "Warning", Reason: "Failed", Count: 5232, Historical: true,
			LastSeen: time.Date(2026, 8, 19, 10, 44, 0, 0, time.UTC),
			Message: `Failed to pull image "` + image + `": rpc error: code = NotFound desc = ` +
				`failed to resolve reference "` + image + `": ` + image + `: not found`},
	)
	c.EventNote = "⚠️ 集群上的实时事件里已经没有原因了，下面 1 条来自 CMDB 采集的历史"

	res := ruleImagePullDetailed(c)
	if res == nil {
		t.Fatal("没命中")
	}
	// 必须真的判出根因，而不是退回「事件已过期，判不出来」
	if !strings.Contains(res.RootCause, "仓库里没有这个镜像或 tag") {
		t.Fatalf("回查历史后仍没判出根因: %s", res.RootCause)
	}
	// 🔴 必须标明依据是历史
	if !strings.Contains(res.RootCause, "历史事件") {
		t.Errorf("根因里没说明依据是历史事件: %s", res.RootCause)
	}
	if res.Confidence != "medium" {
		t.Errorf("依据历史事件时置信度应降到 medium，实际 %s", res.Confidence)
	}
	joined := strings.Join(res.Evidence, "\n")
	if !strings.Contains(joined, "CMDB 采集的历史事件") {
		t.Error("证据里没标出历史来源")
	}
	if !strings.Contains(joined, "2026-08-19 10:44") {
		t.Error("没给出这条历史事件的时间——不给时间就无法判断它还算不算数")
	}
}

// ⚠️ 实时事件够用时不该被历史干扰，置信度也不该降。
func TestLiveEventKeepsHighConfidence(t *testing.T) {
	res := ruleImagePullDetailed(pullCtx("g50-uat", "x/y:v1",
		EventCtx{Type: "Warning", Reason: "FailedToRetrieveImagePullSecret", Count: 9,
			Message: "Unable to retrieve some image pull secrets (harbor-id); attempting to pull the image may not succeed."}))
	if res == nil || res.Confidence != "high" {
		t.Fatalf("实时事件的置信度不该降: %+v", res)
	}
	if strings.Contains(res.RootCause, "历史") {
		t.Errorf("实时事件却标成了历史: %s", res.RootCause)
	}
}
