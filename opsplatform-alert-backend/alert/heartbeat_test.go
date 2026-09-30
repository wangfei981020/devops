package alert

import (
	"strings"
	"testing"
	"time"
)

// The dimensions are the named capture groups, and nothing else. Getting this
// wrong does not crash — it produces `sum by ()` or an aggregation on a label
// that never exists, which collapses every combination into one group and makes
// the whole rule quietly useless.
func TestHeartbeatDimNamesComeFromNamedGroups(t *testing.T) {
	dims, err := heartbeatDimNames(`加入房间,siteId:\s*(?P<site_id>\d+).*?gameRoomId:\s*(?P<room_id>\d+)`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(dims) != 2 || dims[0] != "site_id" || dims[1] != "room_id" {
		t.Errorf("维度应为 [site_id room_id]，实际 %v", dims)
	}
}

// A pattern with only unnamed groups aggregates by nothing. Failing loudly at
// save time beats a rule that runs every five minutes and never alerts.
func TestHeartbeatDimNamesRejectsPatternWithoutNamedGroups(t *testing.T) {
	for _, p := range []string{`gameRoomId:(\d+)`, `plain text`, ``} {
		if _, err := heartbeatDimNames(p); err == nil {
			t.Errorf("%q 没有命名组，应当报错", p)
		}
	}
	if _, err := heartbeatDimNames(`(?P<a>[`); err == nil {
		t.Error("无法编译的正则应当报错")
	}
}

func TestBuildHeartbeatQueryShape(t *testing.T) {
	q, err := buildHeartbeatQuery(
		`{container="game-center-client-backend"} |= "加入房间,siteId:"`,
		`siteId:\s*(?P<site_id>\d+)`,
		[]string{"site_id", "room_id"}, "15m")
	if err != nil {
		t.Fatal(err)
	}
	want := "sum by (site_id, room_id) (count_over_time(" +
		`{container="game-center-client-backend"} |= "加入房间,siteId:"` +
		" | regexp `siteId:\\s*(?P<site_id>\\d+)` [15m]))"
	if q != want {
		t.Errorf("查询语句不对\n  实际 %s\n  期望 %s", q, want)
	}
	// Backticks around the pattern are what let the author write \s instead of
	// \\s; a double-quoted string here would silently change the regex.
	if !strings.Contains(q, "| regexp `") {
		t.Error("正则必须用反引号包裹，否则作者写的每个反斜杠都要再转义一次")
	}
}

// A backtick inside the pattern would close the delimiter early and produce a
// syntactically broken query that Loki rejects at runtime — far from where the
// mistake was made.
func TestBuildHeartbeatQueryRejectsBacktickInPattern(t *testing.T) {
	if _, err := buildHeartbeatQuery(`{a="b"}`, "foo`bar", []string{"x"}, "5m"); err == nil {
		t.Error("正则里含反引号应当报错")
	}
}

// Heartbeat mode needs the full selector. A pipeline-only value (what the
// multi-namespace mode stores) would produce `sum by (...) (count_over_time(|=
// "x" | regexp ...))`, which is not valid LogQL.
func TestBuildHeartbeatQueryRequiresSelector(t *testing.T) {
	if _, err := buildHeartbeatQuery(`|= "加入房间"`, `(?P<x>\d+)`, []string{"x"}, "5m"); err == nil {
		t.Error("只有管道、没有流选择器时应当报错")
	}
	if _, err := buildHeartbeatQuery(``, `(?P<x>\d+)`, []string{"x"}, "5m"); err == nil {
		t.Error("空查询应当报错")
	}
}

// The key identifies a combination across runs. If it depended on Loki's label
// ordering, every cycle would look like "the set changed" and the alert
// interval would never suppress anything.
func TestDimKeyIsOrderIndependent(t *testing.T) {
	dims := []string{"site_id", "room_id"}
	a := dimKey(map[string]string{"site_id": "S1", "room_id": "R1"}, dims)
	b := dimKey(map[string]string{"room_id": "R1", "site_id": "S1"}, dims)
	if a != b {
		t.Errorf("同一组合的 key 应当稳定: %q vs %q", a, b)
	}
	c := dimKey(map[string]string{"site_id": "S1", "room_id": "R2"}, dims)
	if a == c {
		t.Error("不同组合不能产生相同的 key")
	}
}

// The signature decides "same outage or a new one". It must ignore ordering
// (so a reshuffled result does not re-alert) but catch membership changes (so a
// newly quiet room gets through the interval immediately).
func TestHeartbeatSignatureTracksMembershipNotOrder(t *testing.T) {
	m1 := []HeartbeatEntry{{Key: "a"}, {Key: "b"}}
	m2 := []HeartbeatEntry{{Key: "b"}, {Key: "a"}}
	if heartbeatSignature(m1) != heartbeatSignature(m2) {
		t.Error("顺序不同但成员相同，签名应当一致，否则每轮都会被当成新故障重复告警")
	}
	m3 := []HeartbeatEntry{{Key: "a"}, {Key: "b"}, {Key: "c"}}
	if heartbeatSignature(m1) == heartbeatSignature(m3) {
		t.Error("多了一个异常组合，签名必须变化，否则新故障会被告警间隔压住")
	}
	if heartbeatSignature(nil) == heartbeatSignature(m1) {
		t.Error("空集合与非空集合的签名不能相同")
	}
}

// "once" is not a duration. Parsing it to 0 would make it indistinguishable
// from "not configured", which means the opposite — alert on every cycle.
func TestHeartbeatIntervalHandlesForms(t *testing.T) {
	if d := heartbeatInterval("30m"); d.Minutes() != 30 {
		t.Errorf("30m 应解析为 30 分钟，实际 %v", d)
	}
	if d := heartbeatInterval("2d"); d.Hours() != 48 {
		t.Errorf("2d 应解析为 48 小时（ParseDuration 本身不认 d），实际 %v", d)
	}
	if d := heartbeatInterval(AlertIntervalOnce); d != 0 {
		t.Errorf("once 不是时长，应返回 0 交给调用方单独处理，实际 %v", d)
	}
	if d := heartbeatInterval(""); d != 0 {
		t.Errorf("空值应返回 0（每轮都发），实际 %v", d)
	}
}

// hbFixture 造一份贴近真实的判定输入。
//
// 监控范围来自字典的对应关系表，所以这里的重点是各种「关系在、但不该监控」的情形：
// 站点没关注、桌台非在用、关系指向了已经不存在的对象。
func hbFixture() *HeartbeatResult {
	dict := &Dict{
		TZName: "UTC",
		Sites: map[string]DictSite{
			"S_BP": {SiteID: "S_BP", SiteName: "BPreal", Watched: true},
			"S_CP": {SiteID: "S_CP", SiteName: "C88Real", Watched: true},
			"S_NO": {SiteID: "S_NO", SiteName: "", Watched: false}, // 字典里有、没关注
		},
		Rooms: map[string]DictRoom{
			"R1": {RoomID: "R1", RoomNo: "C001", TableNo: "C01", InService: true},
			"R2": {RoomID: "R2", RoomNo: "C002", TableNo: "C02", InService: true},
			"R9": {RoomID: "R9", RoomNo: "D060", TableNo: "D60", InService: false}, // 非在用
		},
		Pairs: []DictPair{
			{SiteID: "S_BP", RoomID: "R1", Source: "auto", Hits: 800},
			{SiteID: "S_BP", RoomID: "R2", Source: "auto", Hits: 300},
			{SiteID: "S_CP", RoomID: "R1", Source: "auto", Hits: 900},
			{SiteID: "S_BP", RoomID: "R9", Source: "auto", Hits: 500}, // 桌台非在用 → 出局
			{SiteID: "S_NO", RoomID: "R1", Source: "auto", Hits: 400}, // 站点没关注 → 出局
			{SiteID: "S_GONE", RoomID: "R1", Source: "manual"},        // 站点已不存在 → 出局
			{SiteID: "S_BP", RoomID: "R_GONE", Source: "manual"},      // 桌台已不存在 → 出局
		},
	}
	dims := []string{"site_id", "room_id"}
	mk := func(site, room string) (string, hbDim) {
		lb := map[string]string{"site_id": site, "room_id": room}
		k := dimKey(lb, dims)
		return k, hbDim{Key: k, Labels: lb, Count: 1}
	}
	// 当前窗口：BP/R1 和 CP/R1 有活动；另外 S_XX/R1 有活动但压根不在关系表里
	cur := map[string]hbDim{}
	for _, e := range [][2]string{{"S_BP", "R1"}, {"S_CP", "R1"}, {"S_XX", "R1"}} {
		k, d := mk(e[0], e[1])
		cur[k] = d
	}
	return &HeartbeatResult{Dims: dims, Current: cur, Dict: dict, TimeRange: "10m"}
}

func hbNames(list []HeartbeatEntry) map[string]bool {
	out := map[string]bool{}
	for _, e := range list {
		out[e.SiteID+"|"+e.RoomID] = true
	}
	return out
}

// 🔴 监控范围来自对应关系表，不来自日志。
//
// 这是整件事的核心：之前用 Loki 基线扫出「出现过的组合」来决定监控谁，一张整周
// 没有日志的在用桌台根本进不了基线，于是永远不告警——而它恰恰是最该告警的。
// 这条钉住「关系表里有、日志里一次都没出现过」的组合必须出现在异常列表里。
func TestScopeComesFromDictNotFromLogs(t *testing.T) {
	res := hbFixture()
	classifyHeartbeat(res, time.Now())

	missing := hbNames(res.Missing)
	if !missing["S_BP|R2"] {
		t.Error("S_BP|R2 在关系表里、是关注站点的在用桌台、本窗口没有活动 —— 必须告警。" +
			"漏掉它说明范围又变成「日志里出现过什么」了")
	}
	if len(res.Scope) != 3 {
		t.Errorf("监控范围应当是 3 个(BP/R1, BP/R2, CP/R1)，实际 %d", len(res.Scope))
	}
}

// 不该监控的三类各自计数，别混成一个数：运维平台那边要据此知道该去清哪一类。
func TestScopeExclusionsAreCountedSeparately(t *testing.T) {
	res := hbFixture()
	classifyHeartbeat(res, time.Now())

	if res.SkipNotWatched != 1 {
		t.Errorf("站点未关注应为 1(S_NO)，实际 %d", res.SkipNotWatched)
	}
	if res.SkipNotInService != 1 {
		t.Errorf("桌台非在用应为 1(R9)，实际 %d", res.SkipNotInService)
	}
	if res.SkipUnknown != 2 {
		t.Errorf("指向已失效对象的应为 2(S_GONE, R_GONE)，实际 %d", res.SkipUnknown)
	}
	// 加减法必须自洽，否则预览上那行链条是假的
	total := len(res.Scope) + res.SkipNotWatched + res.SkipNotInService + res.SkipUnknown
	if total != res.PairTotal {
		t.Errorf("范围 %d + 未关注 %d + 非在用 %d + 已失效 %d = %d，与关系总数 %d 对不上",
			len(res.Scope), res.SkipNotWatched, res.SkipNotInService, res.SkipUnknown,
			total, res.PairTotal)
	}
}

// 日志里有活动、却不在监控范围里的组合要单独列出来。
//
// 它是关系表的体检项：日志证明这个组合真实存在，范围里却没有它。不摆出来的话，
// 一张漏配的桌台会永远安静地不被监控，而且没有任何迹象。
func TestOutOfScopeSurfacesMissingRelations(t *testing.T) {
	res := hbFixture()
	classifyHeartbeat(res, time.Now())

	oos := hbNames(res.OutOfScope)
	if !oos["S_XX|R1"] {
		t.Error("S_XX|R1 本窗口有日志但不在关系表里，应当出现在「范围外」列表里")
	}
	if oos["S_BP|R1"] {
		t.Error("S_BP|R1 在监控范围内，不该出现在「范围外」")
	}
}

// 没有字典就没有监控范围。这不是「监控全部」——静默地什么都不判定，
// 看起来和「一切正常」完全一样。
func TestClassifyWithoutDictMonitorsNothing(t *testing.T) {
	res := hbFixture()
	res.Dict = nil
	classifyHeartbeat(res, time.Now())

	if len(res.Scope) != 0 || len(res.Missing) != 0 || len(res.Alive) != 0 {
		t.Errorf("没有字典时不该判定任何组合，实际 范围%d 异常%d 活跃%d",
			len(res.Scope), len(res.Missing), len(res.Alive))
	}
	if len(res.OutOfScope) != len(res.Current) {
		t.Errorf("没有字典时有活动的组合应当全部记为范围外，实际 %d / %d",
			len(res.OutOfScope), len(res.Current))
	}
}

// ─────────────────────────── 维护抑制 ───────────────────────────

// 中台给的是「这张桌台在哪些站点维护中」，所以抑制要精确到组合：
// 同一张桌台在 A 站点维护时，B 站点照常监控。一刀切会让 B 站点真出问题时没人知道。
func TestMaintenanceSuppressionIsPerSitePair(t *testing.T) {
	res := hbFixture()
	rm := res.Dict.Rooms["R2"]
	rm.Maintaining = true
	rm.MaintainSites = []string{"S_BP"} // 只在 BP 维护
	res.Dict.Rooms["R2"] = rm
	// 让 CP 也用 R2，好验证它不受影响
	res.Dict.Pairs = append(res.Dict.Pairs, DictPair{SiteID: "S_CP", RoomID: "R2", Source: "auto"})

	classifyHeartbeat(res, time.Now())

	if !hbNames(res.Maintaining)["S_BP|R2"] {
		t.Error("S_BP|R2 正在维护，应当被抑制")
	}
	if hbNames(res.Missing)["S_BP|R2"] {
		t.Error("维护中的组合不该进异常列表 —— 维护期间没有日志是预期的")
	}
	if !hbNames(res.Missing)["S_CP|R2"] {
		t.Error("R2 只在 BP 站点维护，CP 站点应当照常监控并告警 —— " +
			"整台一刀切会让 CP 真出问题时没人知道")
	}
}

// 拿不到站点范围时（维护判定配成 status_equals）只能整台抑制。
// 这时候宁可漏报也不能误报——维护中的桌台没日志是必然的。
func TestMaintenanceWithoutSiteListSuppressesWholeTable(t *testing.T) {
	res := hbFixture()
	rm := res.Dict.Rooms["R2"]
	rm.Maintaining = true
	rm.MaintainSites = nil
	res.Dict.Rooms["R2"] = rm

	classifyHeartbeat(res, time.Now())
	if !hbNames(res.Maintaining)["S_BP|R2"] {
		t.Error("没给出站点范围时应当整台抑制")
	}
}

// action=annotate 的意思是「照常告警但标注出来」。把它也抑制掉就违背了配置意图——
// 而配置的人以为自己只是加了个标签。
func TestAnnotateWindowDoesNotSuppress(t *testing.T) {
	res := hbFixture()
	res.Dict.Windows = []DictWindow{{
		Name: "标注型窗口", RepeatType: "daily",
		StartTime: "00:00", EndTime: "23:59", TableNos: "*", Action: "annotate",
	}}
	classifyHeartbeat(res, time.Now())

	if len(res.Maintaining) != 0 {
		t.Errorf("action=annotate 不该抑制，实际抑制了 %d 个", len(res.Maintaining))
	}
	if !hbNames(res.Missing)["S_BP|R2"] {
		t.Error("annotate 窗口内仍应照常告警")
	}
}

// suppress 窗口内不告警。
func TestSuppressWindowSuppresses(t *testing.T) {
	res := hbFixture()
	res.Dict.Windows = []DictWindow{{
		Name: "凌晨保养", RepeatType: "daily",
		StartTime: "00:00", EndTime: "23:59", TableNos: "*", Action: "suppress",
	}}
	classifyHeartbeat(res, time.Now())

	if len(res.Missing) != 0 {
		t.Errorf("suppress 窗口内不该有异常告警，实际 %d 个", len(res.Missing))
	}
	if len(res.Maintaining) == 0 {
		t.Error("被窗口抑制的组合要列进 Maintaining，「为什么这张桌台没告警」必须能当场回答")
	}
}

// ─────────────────────────── 维度校验 ───────────────────────────

// 维度名对不上时要当场报错。不拦的话表现是范围里每个组合都「没有活动」——
// 一轮几十条告警，而根因只是正则里的组名拼错了。
func TestCheckHeartbeatDims(t *testing.T) {
	if err := checkHeartbeatDims([]string{"site_id", "room_id"}); err != nil {
		t.Errorf("正确的维度不该报错: %v", err)
	}
	for _, bad := range [][]string{
		{"site_id"},           // 缺 room_id
		{"room_id"},           // 缺 site_id
		{"site_id", "roomid"}, // 拼错
		{"site_id", "room_id", "extra"},
	} {
		if err := checkHeartbeatDims(bad); err == nil {
			t.Errorf("%v 应当报错", bad)
		}
	}
}
