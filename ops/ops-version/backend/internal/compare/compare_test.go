package compare

import (
	"strings"
	"testing"
	"time"
)

func col(id int64, name, env, status string) Column {
	return Column{OrgID: id, OrgName: name, Env: env, SyncStatus: status}
}
func snap(key, tag string, build int, versioned bool) Snapshot {
	s := Snapshot{ServiceKey: key, Tag: tag, IsVersioned: versioned}
	if build >= 0 {
		b := build
		s.BuildNo = &b
	}
	return s
}

// 🔴 最重要的一条：我方只在 UAT 部署、没有 PROD，
// 要拿我方 UAT 去比对方的 UAT 和 PROD —— 列是自由组合，不是同环境对同环境
func TestCrossEnvColumns(t *testing.T) {
	ourUAT := col(1, "我方", "UAT", "success")
	aUAT := col(2, "A公司", "UAT", "success")
	aPROD := col(2, "A公司", "PROD", "success")

	plan := Plan{Columns: []Column{ourUAT, aUAT, aPROD}, Baseline: ourUAT}
	data := map[string][]Snapshot{
		ourUAT.Key(): {snap("wallet", "t-114", 114, true)},
		aUAT.Key():   {snap("wallet", "t-114", 114, true)},
		aPROD.Key():  {snap("wallet", "t-109", 109, true)},
	}
	res := Compare(plan, data)
	if len(res.Rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(res.Rows))
	}
	cells := res.Rows[0].Cells
	if cells[1].Verdict != VerdictSame {
		t.Errorf("A公司 UAT 应一致, got %s", cells[1].Verdict)
	}
	if cells[2].Verdict != VerdictBehind || cells[2].Delta == nil || *cells[2].Delta != 5 {
		t.Errorf("A公司 PROD 应落后 5, got %s delta=%v", cells[2].Verdict, cells[2].Delta)
	}
}

// 🔴 采集失败 ≠ 对方没部署。这两个混了，token 过期会显示成"对方把服务全下线了"
func TestSyncFailureIsNotMissing(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	dead := col(2, "B公司", "PROD", "auth_failed")
	gone := col(3, "C公司", "PROD", "success") // 采集成功，但真的没这个服务

	plan := Plan{Columns: []Column{base, dead, gone}, Baseline: base}
	data := map[string][]Snapshot{
		base.Key(): {snap("wallet", "t-114", 114, true)},
		dead.Key(): {snap("wallet", "t-114", 114, true)}, // 即使有陈旧数据也不该用
		gone.Key(): {},
	}
	res := Compare(plan, data)
	cells := res.Rows[0].Cells

	if cells[1].Verdict != VerdictNoData {
		t.Errorf("采集失败的列必须是 NoData, got %s", cells[1].Verdict)
	}
	if cells[1].Snap != nil {
		t.Error("采集失败时不能用陈旧快照冒充当前状态")
	}
	if cells[2].Verdict != VerdictMissing {
		t.Errorf("采集成功但确实没有 → Missing, got %s", cells[2].Verdict)
	}
	if len(res.UnhealthyColumns) != 1 {
		t.Errorf("失败的列必须单独报出来供 UI 顶部提示, got %d", len(res.UnhealthyColumns))
	}
	// 一致性分母排除 NoData：一个组织挂了不该让整表看起来"差异激增"
	if res.Rows[0].Comparable != 1 {
		t.Errorf("Comparable 应排除 NoData 列, got %d", res.Rows[0].Comparable)
	}
	t.Logf("失败列的提示语: %q", cells[1].Note)
}

// 🔴 非版本化 tag 两边字符串相同也不能判绿
func TestNonVersionedTagNeverGreen(t *testing.T) {
	base := col(1, "我方", "PROD", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{Columns: []Column{base, other}, Baseline: base}
	data := map[string][]Snapshot{
		base.Key():  {snap("nginx", "stable", -1, false)},
		other.Key(): {snap("nginx", "stable", -1, false)},
	}
	res := Compare(plan, data)
	if got := res.Rows[0].Cells[1].Verdict; got != VerdictUnknown {
		t.Errorf("非版本化 tag 必须 unknown 而不是 same, got %s", got)
	}
}

// 🔴 构建号解析不出时 Delta 必须是 nil 而不是 0（0 会被读成"差 0 个版本"=一致）
func TestSemverNoFakeDelta(t *testing.T) {
	base := col(1, "我方", "PROD", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{Columns: []Column{base, other}, Baseline: base}
	data := map[string][]Snapshot{
		base.Key():  {snap("kite", "v0.14.1", -1, true)},
		other.Key(): {snap("kite", "v0.13.0", -1, true)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Verdict != VerdictBehind {
		t.Errorf("want behind, got %s", c.Verdict)
	}
	if c.Delta != nil {
		t.Errorf("构建号解析不出时 Delta 必须为 nil，不能编个数字, got %d", *c.Delta)
	}
	t.Logf("提示语正确说明了限制: %q", c.Note)
}

// 冲突优先于一切版本判定
func TestConflictWins(t *testing.T) {
	base := col(1, "我方", "PROD", "success")
	other := col(2, "B公司", "PROD", "success")
	plan := Plan{Columns: []Column{base, other}, Baseline: base}
	s := snap("settle", "t-44", 44, true)
	s.HasConflict = true
	data := map[string][]Snapshot{
		base.Key():  {snap("settle", "t-44", 44, true)},
		other.Key(): {s},
	}
	if got := Compare(plan, data).Rows[0].Cells[1].Verdict; got != VerdictConflict {
		t.Errorf("冲突必须优先，即使 tag 相同, got %s", got)
	}
}

// 别名映射：对方改了服务名不该冒出成对的假缺失
func TestAliasAvoidsFakeMissing(t *testing.T) {
	base := col(1, "我方", "PROD", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		Aliases: map[int64]map[string]string{2: {"openapi-svc": "openapi-backend"}},
	}
	data := map[string][]Snapshot{
		base.Key():  {snap("openapi-backend", "t-97", 97, true)},
		other.Key(): {snap("openapi-svc", "t-97", 97, true)},
	}
	res := Compare(plan, data)
	if len(res.Rows) != 1 {
		t.Fatalf("配了别名应归成一行，实得 %d 行（没生效就会是「仅我方有」+「仅对方有」两行）", len(res.Rows))
	}
	if res.Rows[0].Cells[1].Verdict != VerdictSame {
		t.Errorf("want same, got %s", res.Rows[0].Cells[1].Verdict)
	}
}

// 发布中：声明的 tag 与实跑的不一致。这是附加标记，不影响主判定
func TestDeployingFlag(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	prod := col(1, "我方", "PROD", "success")
	plan := Plan{Columns: []Column{base, prod}, Baseline: base}
	s := snap("wallet", "t-114", 114, true)
	s.RunningTag = "t-109" // YAML 改了，pod 还没滚完
	data := map[string][]Snapshot{
		base.Key(): {snap("wallet", "t-114", 114, true)},
		prod.Key(): {s},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Verdict != VerdictSame {
		t.Errorf("声明版本一致，主判定就该是 same, got %s", c.Verdict)
	}
	if !c.Deploying {
		t.Error("实跑版本不同必须标记 Deploying —— 否则「改了但一个 pod 都没起来」会显示成已升级")
	}
	t.Logf("提示语: %q", c.Note)
}

// 手工版本基线："这次交付大家都该是这个 tag"
func TestBaselinePin(t *testing.T) {
	a := col(1, "A公司", "PROD", "success")
	b := col(2, "B公司", "PROD", "success")
	plan := Plan{Columns: []Column{a, b}, Baseline: a, BaselinePin: "t-70"}
	data := map[string][]Snapshot{
		a.Key(): {snap("gw", "t-70", 70, true)},
		b.Key(): {snap("gw", "t-69", 69, true)},
	}
	res := Compare(plan, data)
	if res.Rows[0].Cells[1].Verdict != VerdictBehind {
		t.Errorf("B 不等于 pin，应判 behind, got %s", res.Rows[0].Cells[1].Verdict)
	}
}

// 🔴 每行的 cell 数必须等于列数，一个都不能少。
// 少一格会让前端按列渲染时整行错位 —— 数据看着正常，只是对应错了列。
// 触发条件：某个服务**基准列没有**（key 来自其他列）。
func TestEveryRowHasCellForEveryColumn(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(1, "我方", "PROD", "success")
	plan := Plan{Columns: []Column{base, other}, Baseline: base}
	data := map[string][]Snapshot{
		base.Key():  {snap("only-in-uat", "t-1", 1, true)},
		other.Key(): {snap("only-in-prod", "t-2", 2, true)}, // 基准列没有它
	}
	res := Compare(plan, data)
	if len(res.Rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(res.Rows))
	}
	for _, row := range res.Rows {
		if len(row.Cells) != len(plan.Columns) {
			t.Errorf("服务 %s 只有 %d 个 cell，列数是 %d —— 前端会整行错位",
				row.ServiceKey, len(row.Cells), len(plan.Columns))
		}
		for i, c := range row.Cells {
			if c.Column.Key() != plan.Columns[i].Key() {
				t.Errorf("服务 %s 第 %d 个 cell 对应的列是 %s，应为 %s",
					row.ServiceKey, i, c.Column.Key(), plan.Columns[i].Key())
			}
		}
	}
}

// ─────────────── 归因 ───────────────

func snapT(key, tag string, build int) Snapshot {
	b := build
	return Snapshot{ServiceKey: key, Tag: tag, BuildNo: &b, IsVersioned: true}
}

// 🔴 最重要的一条：**没绑复制规则 ≠ 镜像没推过去**。
// 混成一个的话，一个「忘了绑定」会被显示成「镜像没同步」，
// 人会跑去查 Harbor 的复制规则，而真正要做的是在界面上把规则绑到组织。
func TestUnboundIsNotUnsynced(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{Columns: []Column{base, other}, Baseline: base}
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-114", 114)},
		other.Key(): {snapT("wallet", "t-110", 110)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Sync != SyncAttrUnknown {
		t.Errorf("没有任何同步数据时必须是 unknown（我们不知道），实得 %s", c.Sync)
	}
	t.Logf("提示语: %q", c.SyncNote)
}

// 有绑定、有记录、成功 → 差异的原因在对方（没发版）
func TestSyncedMeansTheirTurn(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		SyncFacts: map[int64]map[string]SyncFact{
			2: {"wallet\x00t-114": {Status: "Succeed",
				FinishedAt: time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)}},
		},
	}
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-114", 114)},
		other.Key(): {snapT("wallet", "t-110", 110)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Sync != SyncAttrSynced {
		t.Errorf("镜像同步成功时应归因为 synced，实得 %s", c.Sync)
	}
	t.Logf("提示语: %q", c.SyncNote)
}

// 有绑定但记录里没有这个 tag → 确实没推过去，是我们的锅
func TestNotSyncedIsOurFault(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		// 有这个组织的记录，但只同步过旧版本
		SyncFacts: map[int64]map[string]SyncFact{
			2: {"wallet\x00t-110": {Status: "Succeed"}},
		},
	}
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-114", 114)},
		other.Key(): {snapT("wallet", "t-110", 110)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Sync != SyncAttrNotSynced {
		t.Errorf("基准版本不在复制记录里 → not_synced，实得 %s", c.Sync)
	}
	t.Logf("提示语: %q", c.SyncNote)
}

// 🔴 归因判的是**基准列的 tag**，不是对方当前跑的 tag。
// 判错的话：对方跑着旧版本、那个旧版本当然同步成功过，
// 于是每一行都显示「已同步」，整个功能失去意义。
func TestAttributeUsesBaselineTagNotTheirs(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		SyncFacts: map[int64]map[string]SyncFact{
			// 只有对方**当前跑的**那个旧版本同步过，我方新版本没有
			2: {"wallet\x00t-110": {Status: "Succeed"}},
		},
	}
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-114", 114)},
		other.Key(): {snapT("wallet", "t-110", 110)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Sync == SyncAttrSynced {
		t.Error("拿对方当前 tag 去查同步记录了 —— 那样每行都会是「已同步」，功能等于没做")
	}
}

// 同步失败要带出具体原因，否则人不知道该找谁
func TestSyncFailedCarriesReason(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		SyncFacts: map[int64]map[string]SyncFact{
			2: {"wallet\x00t-114": {Status: "Failed", ErrMsg: "unauthorized: 目标仓库拒绝推送"}},
		},
	}
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-114", 114)},
		other.Key(): {snapT("wallet", "t-110", 110)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Sync != SyncAttrFailed {
		t.Fatalf("want sync_failed, got %s", c.Sync)
	}
	if !strings.Contains(c.SyncNote, "目标仓库拒绝推送") {
		t.Errorf("失败原因必须带出来，实得 %q", c.SyncNote)
	}
}

// 认不出的状态（进行中等）落到 unknown，不能当成没同步
func TestInProgressIsUnknownNotUnsynced(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		SyncFacts: map[int64]map[string]SyncFact{
			2: {"wallet\x00t-114": {Status: "InProgress"}},
		},
	}
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-114", 114)},
		other.Key(): {snapT("wallet", "t-110", 110)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Sync != SyncAttrUnknown {
		t.Errorf("进行中必须是 unknown，实得 %s", c.Sync)
	}
}

// 一致的格子不做归因 —— 没什么可归因的，标上反而是噪音
func TestNoAttributionForSame(t *testing.T) {
	base := col(1, "我方", "UAT", "success")
	other := col(2, "A公司", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		SyncFacts: map[int64]map[string]SyncFact{2: {}},
	}
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-114", 114)},
		other.Key(): {snapT("wallet", "t-114", 114)},
	}
	c := Compare(plan, data).Rows[0].Cells[1]
	if c.Sync != "" {
		t.Errorf("一致的格子不该有归因，实得 %s", c.Sync)
	}
}

// ─────────────── 服务白名单 ───────────────

// 留空 = 全放行；填了就只比这些
func TestServiceIncludeFilters(t *testing.T) {
	base := col(1, "SL", "UAT", "success")
	other := col(2, "PA", "PROD", "success")
	data := map[string][]Snapshot{
		base.Key():  {snapT("wallet", "t-1", 1), snapT("risk", "t-2", 2), snapT("bi-task", "t-3", 3)},
		other.Key(): {snapT("wallet", "t-1", 1), snapT("risk", "t-2", 2), snapT("bi-task", "t-3", 3)},
	}

	all := Compare(Plan{Columns: []Column{base, other}, Baseline: base}, data)
	if len(all.Rows) != 3 {
		t.Fatalf("留空应比全部，实得 %d 行", len(all.Rows))
	}

	only := Compare(Plan{
		Columns: []Column{base, other}, Baseline: base,
		ServiceInclude: []string{"wallet", "bi-*"},
	}, data)
	got := []string{}
	for _, r := range only.Rows {
		got = append(got, r.ServiceKey)
	}
	if len(got) != 2 || got[0] != "bi-task" || got[1] != "wallet" {
		t.Errorf("白名单应只留 wallet 和 bi-task，实得 %v", got)
	}
}

// 🔴 白名单在**归拢之后**生效：别名要先跑完。
// 否则「对方叫 openapi-svc、我方叫 openapi-backend」时，
// 白名单写我方的名字会把对方那条漏掉 —— 表现为「配了别名却还是少一行」。
func TestServiceIncludeRunsAfterAlias(t *testing.T) {
	base := col(1, "SL", "UAT", "success")
	other := col(2, "PA", "PROD", "success")
	plan := Plan{
		Columns: []Column{base, other}, Baseline: base,
		Aliases:        map[int64]map[string]string{2: {"openapi-svc": "openapi-backend"}},
		ServiceInclude: []string{"openapi-backend"},
	}
	data := map[string][]Snapshot{
		base.Key():  {snapT("openapi-backend", "t-97", 97)},
		other.Key(): {snapT("openapi-svc", "t-97", 97)},
	}
	res := Compare(plan, data)
	if len(res.Rows) != 1 {
		t.Fatalf("别名归拢后白名单应命中，实得 %d 行", len(res.Rows))
	}
	if res.Rows[0].Cells[1].Verdict != VerdictSame {
		t.Errorf("want same, got %s", res.Rows[0].Cells[1].Verdict)
	}
}

// 白名单与采集层用同一套通配语义 —— 两处不一致的话，
// 人在两个输入框里写同样的东西会得到不同结果
func TestServiceMatchSameSemanticsAsCollector(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"wallet-*", "wallet-client", true},
		{"*-backend", "risk-backend", true},
		{"*api*", "openapi-svc", true},
		{"*", "anything", true},
		{"wallet", "wallet-client", false},
	}
	for _, c := range cases {
		if got := matchService(c.pat, c.s); got != c.want {
			t.Errorf("matchService(%q,%q)=%v want %v", c.pat, c.s, got, c.want)
		}
	}
}
