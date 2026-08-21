package export

import (
	"testing"

	"ops-version-backend/internal/compare"
)

func snap(tag string) *compare.Snapshot { return &compare.Snapshot{Tag: tag} }

// 🔴 这几档必须严格分开，合并任何两个都会把"要处理的"和"不用管的"搅在一起。
func TestMatrixCellOf(t *testing.T) {
	cases := []struct {
		name string
		in   compare.Cell
		want matrixCell
	}{
		{"有版本号", compare.Cell{Verdict: compare.VerdictSame, Snap: snap("v1")},
			matrixCell{Text: "v1", Tag: "v1", Kind: kindVersion}},
		{"落后也只是版本不同，矩阵页不带方向", compare.Cell{Verdict: compare.VerdictBehind, Snap: snap("v0")},
			matrixCell{Text: "v0", Tag: "v0", Kind: kindVersion}},
		{"这一列没有这个服务", compare.Cell{Verdict: compare.VerdictMissing},
			matrixCell{Text: "—", Kind: kindMissing}},
		{"采集失败：连有没有都不知道", compare.Cell{Verdict: compare.VerdictNoData},
			matrixCell{Text: "未采集", Kind: kindUnjudgeable}},
		// 非版本化 tag 的版本号是真的，要显示；只是不能拿来判定是否同一制品
		{"非版本化 tag 照样显示版本号", compare.Cell{Verdict: compare.VerdictUnknown, Snap: snap("latest")},
			matrixCell{Text: "latest", Kind: kindUnjudgeable}},
		{"人为忽略", compare.Cell{Verdict: compare.VerdictIgnored, Snap: snap("v1")},
			matrixCell{Text: "已忽略", Kind: kindIgnored}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matrixCellOf(c.in)
			if got != c.want {
				t.Errorf("= %+v，要 %+v", got, c.want)
			}
		})
	}
}

func TestMatrixVerdict(t *testing.T) {
	ver := func(t string) matrixCell { return matrixCell{Text: t, Tag: t, Kind: kindVersion} }
	missing := matrixCell{Text: "—", Kind: kindMissing}
	dead := matrixCell{Text: "未采集", Kind: kindUnjudgeable}
	ign := matrixCell{Text: concIgnored, Kind: kindIgnored}

	cases := []struct {
		name  string
		cells []matrixCell
		want  string
	}{
		{"两列全同", []matrixCell{ver("v1"), ver("v1")}, concSame},
		{"五列全同", []matrixCell{ver("v1"), ver("v1"), ver("v1"), ver("v1"), ver("v1")}, concSame},
		{"两列不同", []matrixCell{ver("v1"), ver("v2")}, concDiff},
		{"三列里有一列不同", []matrixCell{ver("v1"), ver("v1"), ver("v9")}, concDiff},
		{"有一列没这个服务", []matrixCell{ver("v1"), missing}, concMissing},

		// 🔴 这条最要紧：采集失败 ≠ 对方没有。
		//    判反的话，对方 token 过期会被读成"对方把服务全下线了"。
		{"其余列一致但有一列采集失败：不能说一致", []matrixCell{ver("v1"), ver("v1"), dead}, concUnknown},

		// 🔴 一个采不到的列**不许污染整张表**。
		//    这三条是样本导出时抓到的真 bug：优先级写反，
		//    一列采集失败就把每一行都盖成「无法判定」，
		//    而其中好几行在另外两列之间明明差着版本。
		{"既不一致又采集失败：确凿的差异不能被盖掉", []matrixCell{ver("v1"), ver("v2"), dead}, concDiff},
		{"既缺失又采集失败：确凿的缺失不能被盖掉", []matrixCell{ver("v1"), missing, dead}, concMissing},
		{"既不一致又缺失：差异更常是行动项", []matrixCell{ver("v1"), ver("v2"), missing}, concDiff},

		// 两个都是非版本化 tag，没有任何可比的东西
		{"全是非版本化 tag", []matrixCell{dead, dead}, concUnknown},

		{"忽略的格子不参与判定", []matrixCell{ver("v1"), ver("v1"), ign}, concSame},
		{"忽略掉的那列本来不一致，也不算数", []matrixCell{ver("v1"), ver("v1"), ign}, concSame},
		{"整行都被逐格忽略", []matrixCell{ign, ign}, concIgnored},
		// 只剩一个版本号可比 = 没有"不一致"可言
		{"只配了一个平台", []matrixCell{ver("v1")}, concSame},
		{"其余列全被忽略", []matrixCell{ver("v1"), ign}, concSame},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := matrixVerdict(c.cells); got != c.want {
				t.Errorf("= %q，要 %q", got, c.want)
			}
		})
	}
}

// 空输入不能 panic —— 一个服务在所有列上都没有格子（理论上不该发生，
// 但矩阵页是导出的主体，它 panic 等于整个文件出不来）。
func TestMatrixVerdictEmpty(t *testing.T) {
	if got := matrixVerdict(nil); got != concMissing {
		t.Errorf("= %q，空输入要 %q", got, concMissing)
	}
}
