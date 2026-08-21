package compare

import "testing"

func planOf(ign IgnoreSet) Plan {
	base := Column{OrgID: 1, OrgName: "SL", Env: "UAT", SyncStatus: "success"}
	c2 := Column{OrgID: 2, OrgName: "印尼", Env: "UAT", SyncStatus: "success"}
	c3 := Column{OrgID: 3, OrgName: "马来", Env: "UAT", SyncStatus: "success"}
	return Plan{Columns: []Column{base, c2, c3}, Baseline: base, Ignores: ign}
}

// 🔴 单元格忽略**只影响那一格**，同一行其他列照常判定。
// 这是用户明确要的语义："印尼不跑 wallet" 不该让"马来 vs SL 的 wallet 差异"也消失。
func TestIgnoreCellDoesNotAffectOtherColumns(t *testing.T) {
	// ⚠️ 列标识是 StableKey：orgID/projectID/env。中间那段是项目 ——
	// 同一平台同一环境下的两个项目是两列，共用标识会让忽略规则串到隔壁项目去。
	p := planOf(IgnoreSet{Cells: map[string][]string{"wallet": {"2/0/UAT"}}})
	data := map[string][]Snapshot{
		"SL/UAT": {snap("wallet", "v2", 2, true)},
		"印尼/UAT": {snap("wallet", "v1", 1, true)}, // 被忽略
		"马来/UAT": {snap("wallet", "v1", 1, true)}, // 应当照常判成落后
	}
	res := Compare(p, data)
	if len(res.Rows) != 1 {
		t.Fatalf("行数 = %d，要 1", len(res.Rows))
	}
	cells := res.Rows[0].Cells
	if cells[1].Verdict != VerdictIgnored {
		t.Errorf("印尼列应为 ignored，实得 %s", cells[1].Verdict)
	}
	if cells[2].Verdict != VerdictBehind {
		t.Errorf("马来列应照常判成 behind，实得 %s —— 忽略串到别的列了", cells[2].Verdict)
	}
	// 忽略的格子不进分母，也不算差异
	if res.Rows[0].Comparable != 1 {
		t.Errorf("可比列数 = %d，要 1（忽略的不进分母）", res.Rows[0].Comparable)
	}
	if !res.Rows[0].HasDiff {
		t.Error("马来落后了，这一行应当算有差异")
	}
	if res.Summary[VerdictIgnored] != 1 {
		t.Errorf("统计里应有 1 个 ignored，实得 %d —— 忽略必须看得见", res.Summary[VerdictIgnored])
	}
}

// 整行忽略：不进 Rows，但名字要报出来
func TestIgnoreRowReportsNames(t *testing.T) {
	p := planOf(IgnoreSet{Services: []string{"wallet", "bi-*"}})
	data := map[string][]Snapshot{
		"SL/UAT": {snap("wallet", "v2", 2, true), snap("bi-report", "v1", 1, true), snap("order", "v3", 3, true)},
		"印尼/UAT": {snap("order", "v3", 3, true)},
		"马来/UAT": {snap("order", "v3", 3, true)},
	}
	res := Compare(p, data)
	if len(res.Rows) != 1 || res.Rows[0].ServiceKey != "order" {
		t.Fatalf("只该剩 order，实得 %d 行", len(res.Rows))
	}
	// 🔴 名字必须给出来 —— 只给数字的话人没法确认自己排除了什么
	if len(res.IgnoredRows) != 2 {
		t.Fatalf("IgnoredRows = %v，要 2 个", res.IgnoredRows)
	}
	if res.IgnoredRows[0] != "bi-report" || res.IgnoredRows[1] != "wallet" {
		t.Errorf("IgnoredRows = %v，要 [bi-report wallet]（含通配命中的）", res.IgnoredRows)
	}
}

// 忽略优先于"采集失败" —— 人为决定不比的东西，不该显示成需要处理的 no_data
func TestIgnoreBeatsNoData(t *testing.T) {
	p := planOf(IgnoreSet{Cells: map[string][]string{"wallet": {"2/0/UAT"}}})
	p.Columns[1].SyncStatus = "auth_failed" // 印尼列采集失败
	data := map[string][]Snapshot{"SL/UAT": {snap("wallet", "v2", 2, true)}, "马来/UAT": {snap("wallet", "v2", 2, true)}}
	res := Compare(p, data)
	if got := res.Rows[0].Cells[1].Verdict; got != VerdictIgnored {
		t.Errorf("忽略应优先于 no_data，实得 %s —— 会让人去查一个不需要处理的采集失败", got)
	}
}

// 基准列不允许被忽略 —— 忽略了基准，整行就没有参照物
func TestBaselineColumnCannotBeIgnored(t *testing.T) {
	p := planOf(IgnoreSet{Cells: map[string][]string{"wallet": {"1/0/UAT"}}})
	data := map[string][]Snapshot{
		"SL/UAT": {snap("wallet", "v2", 2, true)}, "印尼/UAT": {snap("wallet", "v1", 1, true)}, "马来/UAT": {snap("wallet", "v2", 2, true)},
	}
	res := Compare(p, data)
	if got := res.Rows[0].Cells[0].Verdict; got == VerdictIgnored {
		t.Error("基准列被忽略了 —— 整行失去参照物")
	}
	if got := res.Rows[0].Cells[1].Verdict; got != VerdictBehind {
		t.Errorf("其余列应照常判定，实得 %s", got)
	}
}

func TestIgnoreSetHelpers(t *testing.T) {
	s := IgnoreSet{Services: []string{"a", "bi-*"}, Cells: map[string][]string{"c": {"2/0/UAT"}}}
	if !s.IgnoredRow("bi-report") || s.IgnoredRow("other") {
		t.Error("整行通配匹配不对")
	}
	// ⚠️ 整行忽略时 IgnoredCell 返回 false —— 两者分工不同，
	//    混在一起会让"已忽略格子数"把整行的也算进去
	if s.IgnoredCell("a", "2/0/UAT") {
		t.Error("整行忽略不该同时算作单元格忽略")
	}
	if !s.IgnoredCell("c", "2/0/UAT") || s.IgnoredCell("c", "3/0/UAT") {
		t.Error("单元格匹配不对")
	}
	if (IgnoreSet{}).IsEmpty() != true {
		t.Error("空集合判定不对")
	}
}
