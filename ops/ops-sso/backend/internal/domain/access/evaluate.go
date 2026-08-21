package access

import "sort"

// Evaluate 判定 subject 能否访问 app。
//
// rules 传的是**候选集**：调用方可以只查与该应用相关的规则（全局 + 该应用所属分组 + 该应用），
// 也可以把全量规则丢进来 —— 本函数会自己筛掉不适用的，结果一样。
//
// 判定顺序见 package 注释。这里的每一步都对应 evaluate_test.go 里的一组用例，
// 改动这个函数必须先让那些用例失败，否则说明你改的东西没有被测到。
func Evaluate(sub Subject, app App, rules []Rule) Decision {
	// 未认证不进入授权：没有主体就没有「他能不能」这个问题。
	// 返回 deny 而不是放行 —— 认证中间件漏掉时的后果必须是进不去，不能是全放。
	if sub.UserID == 0 {
		return Decision{Effect: Deny, Reason: ReasonNoSubject}
	}

	inGroup := make(map[int64]bool, len(app.GroupIDs))
	for _, g := range app.GroupIDs {
		inGroup[g] = true
	}

	type cand struct {
		rule      Rule
		subjRank  int
		scopeRank int
		depth     int
	}
	var cands []cand

	for _, r := range rules {
		// ── 作用域是否管得着这个应用 ──
		switch r.Scope {
		case ScopeGlobal:
		case ScopeGroup:
			if !inGroup[r.ScopeID] {
				continue
			}
		case ScopeApp:
			if r.ScopeID != app.ID {
				continue
			}
		default:
			continue // 未知作用域一律不生效，而不是当成全局
		}

		// ── 主体是否命中这个人 ──
		rank, depth, ok := subjectRank(r, sub)
		if !ok {
			continue
		}

		cands = append(cands, cand{rule: r, subjRank: rank, scopeRank: scopeRank(r.Scope), depth: depth})
	}

	if len(cands) == 0 {
		return Decision{Effect: Deny, Reason: ReasonDefaultDeny}
	}

	// 排序：主体具体度 → 作用域具体度 → deny 优先 → ID（保证结果稳定可复现）。
	//
	// 最后那个 ID 不是凑数的：没有它，两条完全同级的规则谁赢取决于数据库返回顺序，
	// 同一次请求重放两遍可能得到不同结论，排障时会怀疑人生。
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.subjRank != b.subjRank {
			return a.subjRank > b.subjRank
		}
		if a.scopeRank != b.scopeRank {
			return a.scopeRank > b.scopeRank
		}
		if (a.rule.Effect == Deny) != (b.rule.Effect == Deny) {
			return a.rule.Effect == Deny
		}
		return a.rule.ID < b.rule.ID
	})

	trace := make([]Step, 0, len(cands))
	for _, c := range cands {
		trace = append(trace, Step{
			Rule:         c.rule,
			SubjectRank:  c.subjRank,
			ScopeRank:    c.scopeRank,
			Outcome:      OutcomeShadowed,
			MatchedDepth: c.depth,
		})
	}

	// ── 第一优先：强制拒绝。任何作用域、任何主体具体度都盖不住它 ──
	for i, c := range cands {
		if c.rule.Enforced && c.rule.Effect == Deny {
			trace[i].Outcome = OutcomeEnforced
			r := c.rule
			return Decision{Effect: Deny, Reason: ReasonEnforcedDeny, Rule: &r, Trace: reorderWinnerFirst(trace, i)}
		}
	}

	// ── 否则第一条就是赢家 ──
	trace[0].Outcome = OutcomeWon
	for i := 1; i < len(cands); i++ {
		if cands[i].subjRank == cands[0].subjRank && cands[i].scopeRank == cands[0].scopeRank {
			// 同等具体度却没赢，只可能是输给了 deny
			trace[i].Outcome = OutcomeLostToDeny
		}
	}
	win := cands[0].rule
	return Decision{Effect: win.Effect, Reason: ReasonRule, Rule: &win, Trace: trace}
}

// subjectRank 判断规则的主体是否命中此人，并给出具体度。
func subjectRank(r Rule, sub Subject) (rank, depth int, ok bool) {
	switch r.SubjectType {
	case SubjectPublic:
		return rankPublic, 0, true
	case SubjectUser:
		if r.SubjectID == sub.UserID {
			return rankUser, 0, true
		}
	case SubjectGroup:
		if sub.GroupIDs[r.SubjectID] {
			return rankGroup, 0, true
		}
	case SubjectRole:
		if sub.RoleIDs[r.SubjectID] {
			return rankRole, 0, true
		}
	case SubjectDept:
		// 子树匹配：DeptDepth 里含祖先链，所以挂在上级部门的规则也能命中下级的人。
		// 越深的部门越具体 —— 「后端组 deny」应当盖住「研发中心 allow」。
		if d, in := sub.DeptDepth[r.SubjectID]; in {
			return rankDeptBase + d, d, true
		}
	}
	return 0, 0, false
}

func scopeRank(s Scope) int {
	switch s {
	case ScopeApp:
		return rankScopeApp
	case ScopeGroup:
		return rankScopeGroup
	default:
		return rankScopeGlobal
	}
}

// reorderWinnerFirst 把赢家挪到轨迹第一条。
//
// 强制拒绝可能排在很后面（比如它主体粗、作用域也粗），但界面上必须先看到
// 「是它定的」，再看被它盖掉的那些。顺序错了，运维会以为系统判错了。
func reorderWinnerFirst(trace []Step, i int) []Step {
	if i == 0 {
		return trace
	}
	out := make([]Step, 0, len(trace))
	out = append(out, trace[i])
	out = append(out, trace[:i]...)
	out = append(out, trace[i+1:]...)
	return out
}

// VisibleApps 过滤出此人能看见的应用，供门户「我的入口」使用。
//
// 单独一个函数而不是让调用方循环 Evaluate，是为了让门户与网关**共用同一套判定**：
// 门户上看得见但点进去被网关拒，是这类产品最伤人的体验之一。
func VisibleApps(sub Subject, apps []App, rules []Rule) map[int64]Decision {
	out := make(map[int64]Decision, len(apps))
	for _, a := range apps {
		out[a.ID] = Evaluate(sub, a, rules)
	}
	return out
}
