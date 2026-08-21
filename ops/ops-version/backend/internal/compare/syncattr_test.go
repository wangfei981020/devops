package compare

import (
	"strings"
	"testing"
)

func planWith(facts map[int64]map[string]SyncFact, gaps map[int64]string) Plan {
	return Plan{SyncFacts: facts, SyncGaps: gaps}
}

func baseSnap(tag string) *Snapshot { return &Snapshot{ServiceKey: "wallet", Tag: tag} }

// 🔴 「没有复制记录」的成因必须原样说出来，不能笼统成「未绑定复制规则」。
//
// 三种成因的下一步完全不同（去绑规则 / 去点拉取 / 去查权限），
// 混成一句话会把人引向错误的方向 —— 用户和我都被这句话引偏过：
// 照它去查绑定，而绑定明明是对的，真因是覆盖面不够。
func TestSyncGapReasonIsPassedThrough(t *testing.T) {
	want := "没有任何复制规则「推给 PA」—— 去「镜像同步」页…"
	p := planWith(nil, map[int64]string{7: want})
	attr, note := attribute(p, 7, "wallet", baseSnap("v1"))
	if attr != SyncAttrUnknown {
		t.Errorf("没有记录时必须是 unknown，实得 %s", attr)
	}
	if note != want {
		t.Errorf("成因没被透传：\n实得 %q\n要   %q", note, want)
	}
}

// 没填成因时也不能说「未绑定」—— 那是在猜
func TestSyncGapFallbackDoesNotClaimUnbound(t *testing.T) {
	_, note := attribute(planWith(nil, nil), 7, "wallet", baseSnap("v1"))
	if strings.Contains(note, "未绑定") {
		t.Errorf("没查过成因就断言「未绑定」是在猜：%q", note)
	}
}

// 🔴 「这个版本没推」和「这个服务不在复制范围内」是两件事。
//
// 前者是**事实**（可以去补推），后者是**我们不知道**（它可能本来就不该被推）。
// 不分的话，一个不在复制范围内的服务会显示成「镜像未同步」，
// 人会去查为什么没推，而真相是它压根不该出现在那儿。
func TestServiceNotInScopeIsUnknownNotMissing(t *testing.T) {
	facts := map[int64]map[string]SyncFact{
		7: {"other-svc\x00v9": {Status: "Succeed"}},
	}
	attr, note := attribute(planWith(facts, nil), 7, "wallet", baseSnap("v1"))
	if attr != SyncAttrUnknown {
		t.Errorf("服务从没出现在复制记录里 → 应为 unknown，实得 %s（%s）", attr, note)
	}
	if !strings.Contains(note, "不在任何复制规则的范围内") {
		t.Errorf("没说清它可能不在复制范围内：%q", note)
	}
}

// 服务推过别的版本、只是这个版本没推 → 这是事实，可以判 not_synced
func TestOtherTagPushedThenThisTagIsMissing(t *testing.T) {
	facts := map[int64]map[string]SyncFact{
		7: {"wallet\x00v0": {Status: "Succeed"}},
	}
	attr, note := attribute(planWith(facts, nil), 7, "wallet", baseSnap("v1"))
	if attr != SyncAttrNotSynced {
		t.Errorf("同服务推过别的版本 → 这个版本没推是事实，应为 not_synced，实得 %s", attr)
	}
	if !strings.Contains(note, "推过别的版本") {
		t.Errorf("没说清「推过别的版本」这个前提：%q", note)
	}
}

// 基准侧没有这个服务时不谈同步
func TestNoBaseNoAttribution(t *testing.T) {
	if attr, _ := attribute(planWith(nil, nil), 7, "wallet", nil); attr != SyncAttrUnknown {
		t.Errorf("基准侧没有此服务时应为 unknown，实得 %s", attr)
	}
}
