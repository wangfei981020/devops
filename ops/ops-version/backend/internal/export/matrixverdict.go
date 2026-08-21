package export

import "ops-version-backend/internal/compare"

// ─────────────────────────────────────────────────────────────
// 矩阵页的判定：**横着看，没有基准**
// ─────────────────────────────────────────────────────────────
//
// 🔴 这一层存在的理由：`compare.Verdict` 全都是**相对基准**的
// （落后 / 超前 / 该列没有 / 基准没有），而导出的矩阵可能是
// **别的两家公司之间**的对账 —— 我方根本不在里面，谁也不是基准。
// 那时"落后 8 个版本"这种话没有主语，收到表的人看不懂。
//
// 去掉基准之后只剩一个问法：**这几列彼此一不一样**。
// 于是判定变成无方向的四态（一致 / 不一致 / 缺失 / 无法判定），
// 加上一个「已忽略」——那是我们主动决定不比的。
//
// ⚠️ 这里**不复用** compare 的 rank()：那个是"哪个判定更该被看到"，
//
//	按的是有基准时的严重度顺序。两套逻辑长得像，合并会让其中一边悄悄改变含义。
const (
	concSame    = "一致"
	concDiff    = "不一致"
	concMissing = "缺失"
	concUnknown = "无法判定"
	concIgnored = "已忽略"
)

// cellKind 一个格子在矩阵判定里扮演什么角色。
type cellKind int

const (
	// kindVersion 有版本号，参与比对
	kindVersion cellKind = iota
	// kindMissing 这一列确实没有这个服务
	kindMissing
	// kindUnjudgeable 有东西但不能拿来判定：采集失败 / 非版本化 tag / 同名冲突
	//
	// 🔴 必须和 kindMissing 分开。合并的话，对方 token 过期
	// （整列采不到）会显示成「对方把服务全下线了」—— 判反。
	kindUnjudgeable
	// kindIgnored 人为忽略，主动不比
	//
	// 🔴 也必须单列。忽略是"不用管"，无法判定是"要去查"，
	// 混在一起就把"要处理的"和"不用管的"搅成一堆。
	kindIgnored
)

// matrixCell 矩阵页一个格子要显示什么、算不算数。
type matrixCell struct {
	// Text 格子里的字。**只有版本号或占位符，不加任何判定词** ——
	// 这一列是拿来复制去搜索、去填工单的（用户实测：混了判定会被一起复制走）。
	//
	// 三种空态各有各的字：「—」这列确实没有 / 「未采集」我们没采到 /
	// 「已忽略」主动不比。它们的处理方向完全不同，不能都写成「—」。
	Text string
	// Tag 参与比对的版本号。空 = 这一格不参与。
	Tag  string
	Kind cellKind
}

// matrixCellOf 把一个带基准语义的 Cell 翻成矩阵页的格子。
func matrixCellOf(c compare.Cell) matrixCell {
	switch c.Verdict {
	case compare.VerdictIgnored:
		// ⚠️ 写「已忽略」而不是留空：留空和「对方没有」长得一样，
		//    而几个月后没人说得清某个服务为什么不在表里。
		return matrixCell{Text: concIgnored, Kind: kindIgnored}

	case compare.VerdictNoData:
		// 🔴 写「未采集」而不是「—」。
		//
		//    三种空态在纸面上必须**各有各的字**，否则全靠结论列去分：
		//      —      = 这一列确实没有这个服务   → 找对方确认
		//      未采集 = 我们没采到               → 查我们自己的采集
		//      已忽略 = 主动决定不比             → 什么都不用做
		//    都写成「—」的话，对方 token 过期会被读成
		//    「对方把服务全下线了」——处理方向正好反了。
		return matrixCell{Text: "未采集", Kind: kindUnjudgeable}

	case compare.VerdictUnknown, compare.VerdictConflict:
		// 非版本化 tag（latest / v3）或同名冲突。
		// ⚠️ 版本号**照样显示**——它是真的，只是不能拿来判定是否同一制品。
		t := tagOf(c)
		if t == "" {
			t = "—"
		}
		return matrixCell{Text: t, Kind: kindUnjudgeable}
	}

	if t := tagOf(c); t != "" {
		return matrixCell{Text: t, Tag: t, Kind: kindVersion}
	}
	return matrixCell{Text: "—", Kind: kindMissing}
}

func tagOf(c compare.Cell) string {
	if c.Snap == nil {
		return ""
	}
	return c.Snap.Tag
}

// matrixVerdict 一行的结论。先命中先算。
func matrixVerdict(cells []matrixCell) string {
	var tags []string
	var unjudgeable, missing bool
	ignored := 0

	for _, m := range cells {
		switch m.Kind {
		case kindIgnored:
			ignored++
		case kindUnjudgeable:
			unjudgeable = true
		case kindMissing:
			missing = true
		case kindVersion:
			tags = append(tags, m.Tag)
		}
	}

	// 🔴 顺序是：**先看有没有确凿的坏消息，没有才说不知道**。
	//
	// 一开始我写反了（无法判定排最前），样本一导出就露馅：
	// 三列里有一列采集失败，整张表**每一行**都成了「无法判定」——
	// 而其中好几行在另外两列之间明明差着版本。
	// 那条确凿的差异被藏在"不知道"后面，就没人会去处理它了。
	//
	// ⚠️ 一个采不到的列**不该污染整张表**。它只能让"看起来一致"的行
	//    降级成"不知道"（因为确实不能说一致），不能盖掉已经比出来的差异。
	if diff := hasDiff(tags); diff {
		return concDiff
	}
	if missing {
		return concMissing
	}
	if unjudgeable {
		// 其余列都一致，但有一列不知道 —— 不能说"一致"，
		// 那等于替一个没查到的列打包票。
		return concUnknown
	}
	if len(tags) == 0 {
		// 每一格都被忽略了。
		// （整行忽略的服务不在 Rows 里，所以这只可能是"逐格忽略"凑齐了一整行。）
		if ignored > 0 {
			return concIgnored
		}
		return concMissing
	}
	return concSame
}

// hasDiff 参与比对的版本号是否不全相同。
//
// ⚠️ 少于两个时恒为 false：只配了一个平台、或其余列全被忽略，
// 都没有"不一致"可言 —— 那不是"一致"也不是"不一致"，是无从比较，
// 交给后面几档去定。
func hasDiff(tags []string) bool {
	for i := 1; i < len(tags); i++ {
		if tags[i] != tags[0] {
			return true
		}
	}
	return false
}

// concStyleOf 结论对应的底色。整行都用它 —— 和收表人已经习惯的那两版表一致。
func concStyleOf(st *styles, conc string) int {
	switch conc {
	case concSame:
		return st.same
	case concDiff:
		return st.diff
	case concMissing:
		return st.missing
	case concIgnored:
		return st.ignored
	default: // concUnknown
		return st.noData
	}
}

// concMonoStyleOf 同上，但等宽 —— 版本号那几列用，位数才对得齐。
func concMonoStyleOf(st *styles, conc string) int {
	switch conc {
	case concSame:
		return st.sameMono
	case concDiff:
		return st.diffMono
	case concMissing:
		return st.missingMono
	case concIgnored:
		return st.ignoredMono
	default:
		return st.noDataMono
	}
}
