package handlers

import (
	"strings"
	"testing"
)

var idx = map[string]cmEntry{
	"g50-uat/app-conf": {keys: "log_level,timeout", keyCount: 2},
}

// ConfigMap 有名录 → 可确定性判定「不存在」。
func TestJudgeRefConfigMapMissingIsDefinitive(t *testing.T) {
	f := judgeRef(refRow{ns: "g50-uat", kind: "configmap", name: "no-such", source: "volume",
		pods: []string{"a"}}, idx, secretInv{}, nil, false)
	if f == nil || f.Status != "missing" || f.Severity != "high" {
		t.Fatalf("名录里没有的 ConfigMap 应报 high/missing，实际 %+v", f)
	}
	if !strings.Contains(f.Basis, "确定性") {
		t.Errorf("依据里应写明这是确定性判定，实际: %s", f.Basis)
	}
}

// 引用的键不在名录里 → key_missing。这类问题肉眼极难发现。
func TestJudgeRefDetectsMissingKey(t *testing.T) {
	f := judgeRef(refRow{ns: "g50-uat", kind: "configmap", name: "app-conf", key: "log_lvl",
		source: "env", pods: []string{"a"}}, idx, secretInv{}, nil, false)
	if f == nil || f.Status != "key_missing" {
		t.Fatalf("键名对不上应报 key_missing，实际 %+v", f)
	}
}

// 键存在时不该报——名录判定必须是双向准确的。
func TestJudgeRefQuietWhenKeyPresent(t *testing.T) {
	if f := judgeRef(refRow{ns: "g50-uat", kind: "configmap", name: "app-conf", key: "timeout",
		source: "env", pods: []string{"a"}}, idx, secretInv{}, nil, false); f != nil {
		t.Errorf("键存在时不该报，实际 %+v", f)
	}
}

// 核心边界：Secret 没有名录，**无佐证时一律不报**。
// 若这里改成"全部标为待确认"，会产出几百条无效条目——CMDB-005 的老毛病。
func TestJudgeRefSecretSilentWithoutEvidence(t *testing.T) {
	r := refRow{ns: "g50-uat", kind: "secret", name: "harbor-id", source: "imagePullSecret",
		pods: []string{"a"}, badPods: 1}
	if f := judgeRef(r, idx, secretInv{}, map[string]bool{}, true); f != nil {
		t.Errorf("无事件佐证的 Secret 引用不该报出，实际 %+v", f)
	}
	if f := judgeRef(r, idx, secretInv{}, nil, false); f != nil {
		t.Errorf("事件都没取到时更不该下结论，实际 %+v", f)
	}
}

// 有事件佐证 → 才报，且拉取密钥要给出专属处置建议（DEV-002 场景）。
func TestJudgeRefSecretReportsWithEvidence(t *testing.T) {
	f := judgeRef(refRow{ns: "g50-uat", kind: "secret", name: "harbor-id",
		source: "imagePullSecret", pods: []string{"a"}, badPods: 1},
		idx, secretInv{}, map[string]bool{"g50-uat/harbor-id": true}, true)
	if f == nil || f.Status != "missing" {
		t.Fatalf("有 not found 佐证时应报缺失，实际 %+v", f)
	}
	if !strings.Contains(f.Issue, "拉取") || !strings.Contains(f.Action, "g50-uat") {
		t.Errorf("拉取密钥应给出定位到命名空间的处置建议，实际 issue=%q action=%q", f.Issue, f.Action)
	}
	if !strings.Contains(f.Basis, "事件") {
		t.Errorf("依据必须写明来自事件而非名录，实际: %s", f.Basis)
	}
}

// Pod 还在跑但配置已缺失 = 定时炸弹，措辞必须点出"重启即挂"。
func TestRestartWarningFlagsTimeBomb(t *testing.T) {
	live := restartWarning(refRow{pods: []string{"a", "b"}, badPods: 0})
	if !strings.Contains(live, "重启") {
		t.Errorf("Pod 仍在运行时应警告下次重启会失败，实际: %s", live)
	}
	if bad := restartWarning(refRow{pods: []string{"a"}, badPods: 1}); strings.Contains(bad, "重启会") {
		t.Errorf("已异常的 Pod 不该套用定时炸弹措辞，实际: %s", bad)
	}
}

// 正则对着 kubelet/kubectl 的真实措辞跑，不是对着想象中的格式。
func TestNotFoundPatternsMatchRealMessages(t *testing.T) {
	cases := []struct{ msg, want string }{
		{`Error: secret "logging-es-es-elastic-user" not found`, "logging-es-es-elastic-user"},
		{`Error: configmap "nginx-conf" not found`, "nginx-conf"},
		{`MountVolume.SetUp failed for volume "cfg" : configmap "app-conf" not found`, "app-conf"},
	}
	for _, c := range cases {
		m := notFoundPattern.FindStringSubmatch(c.msg)
		if len(m) < 3 || m[2] != c.want {
			t.Errorf("消息 %q 应抽出 %q，实际 %v", c.msg, c.want, m)
		}
	}
	// 下面这条是本地 K8s 1.29 实测抓到的原文——第一版正则是照着想象的格式写的，匹配不上，
	// 端到端验证时 case-pull-secret 一条都没报出来才发现。真实措辞在括号里给名字列表。
	real := `Unable to retrieve some image pull secrets (ghost-harbor-id); attempting to pull the image may not succeed.`
	m := pullSecretListPattern.FindStringSubmatch(real)
	if len(m) < 2 || strings.TrimSpace(m[1]) != "ghost-harbor-id" {
		t.Errorf("实测事件原文应抽出 ghost-harbor-id，实际 %v", m)
	}
	// 多个密钥的情况
	multi := `Unable to retrieve some image pull secrets (harbor-id, gcr-key); attempting to pull the image may not succeed.`
	if m := pullSecretListPattern.FindStringSubmatch(multi); len(m) < 2 || m[1] != "harbor-id, gcr-key" {
		t.Errorf("多密钥列表应整体抽出，实际 %v", m)
	}
	// 另一种单数措辞也要兼容
	single := `Unable to retrieve pull secret g50-uat/harbor-id for g50-uat/app-7c9 because the secret does not exist`
	if m := pullSecretSinglePattern.FindStringSubmatch(single); len(m) < 2 || m[1] != "harbor-id" {
		t.Errorf("单数措辞应抽出 harbor-id，实际 %v", m)
	}
}

// 反向回归：普通日志里的 not found 不能误触发。
func TestNotFoundPatternIgnoresUnrelatedText(t *testing.T) {
	for _, msg := range []string{
		`GET /api/secret not found`,
		`page not found`,
		`secret manager returned not found`, // 无引号包裹的名字，不构成资源引用
	} {
		if notFoundPattern.MatchString(msg) {
			t.Errorf("不该匹配无关文本: %q", msg)
		}
	}
}

// 键名精确匹配：前缀相同的键不能被误判为存在。
func TestCMEntryHasKeyIsExact(t *testing.T) {
	e := cmEntry{keys: "log_level,timeout"}
	if e.hasKey("log") || e.hasKey("time") {
		t.Error("前缀不应算命中，否则「键不存在」会漏报")
	}
	if !e.hasKey("timeout") {
		t.Error("完整键名应命中")
	}
}

// ── Secret 名录（按集群开关）────────────────────────────────────────────

// 名录可用时，Secret 缺失是确定性判定，不再依赖事件。
func TestJudgeRefSecretDefinitiveWhenInventoryUsable(t *testing.T) {
	inv := secretInv{enabled: true, usable: true, names: map[string]bool{"g50-uat/exists": true}}
	f := judgeRef(refRow{ns: "g50-uat", kind: "secret", name: "harbor-id", source: "imagePullSecret",
		pods: []string{"p1"}}, nil, inv, nil, false)
	if f == nil || f.Status != "missing" || f.Severity != "high" {
		t.Fatalf("名录可用且不含该名字时应确定性判缺失，实际 %+v", f)
	}
	if !strings.Contains(f.Basis, "确定性") {
		t.Errorf("依据应写明是确定性判定，实际: %s", f.Basis)
	}
	// 名录里有的不该报
	if f2 := judgeRef(refRow{ns: "g50-uat", kind: "secret", name: "exists", source: "env"},
		nil, inv, nil, false); f2 != nil {
		t.Errorf("名录中存在的 Secret 不该报出，实际 %+v", f2)
	}
}

// 最关键的一条：开了开关但一条都没采到（403）时，绝不能把所有引用判成缺失。
// 那会一次产生几百条误报，比不给结论危险得多。
func TestJudgeRefSecretNoFalsePositiveWhenInventoryEmpty(t *testing.T) {
	inv := secretInv{enabled: true, usable: false, names: map[string]bool{}} // 开了但空
	f := judgeRef(refRow{ns: "g50-uat", kind: "secret", name: "harbor-id", source: "imagePullSecret"},
		nil, inv, nil, false)
	if f != nil {
		t.Fatalf("名录为空时应退回事件佐证、不下确定性结论，实际报了 %+v", f)
	}
	// 有事件佐证时仍然要报
	ev := map[string]bool{"g50-uat/harbor-id": true}
	if f2 := judgeRef(refRow{ns: "g50-uat", kind: "secret", name: "harbor-id", source: "imagePullSecret"},
		nil, inv, ev, true); f2 == nil {
		t.Error("有事件佐证时应报出缺失")
	}
}

// 未开启名录的集群（UAT/生产）行为不变：只认事件佐证。
func TestJudgeRefSecretFallsBackWhenDisabled(t *testing.T) {
	inv := secretInv{enabled: false}
	if f := judgeRef(refRow{ns: "cesar", kind: "secret", name: "x", source: "env"},
		nil, inv, nil, false); f != nil {
		t.Errorf("未开启名录且无佐证时不该报，实际 %+v", f)
	}
}

// capability 文案必须让人看出这次判定可不可信——三种状态说法不能一样。
func TestSecretCapabilityDistinguishesThreeStates(t *testing.T) {
	usable := secretCapability(secretInv{enabled: true, usable: true})
	emptyInv := secretCapability(secretInv{enabled: true, usable: false})
	disabled := secretCapability(secretInv{enabled: false})
	if usable == emptyInv || emptyInv == disabled || usable == disabled {
		t.Fatal("三种状态的说明不能相同")
	}
	if !strings.Contains(usable, "确定性") {
		t.Errorf("可用状态应说明可确定性判定: %s", usable)
	}
	for _, s := range []string{emptyInv, disabled} {
		if !strings.Contains(s, "不等于") {
			t.Errorf("不可用状态必须点明「未报出不等于没问题」: %s", s)
		}
	}
}
