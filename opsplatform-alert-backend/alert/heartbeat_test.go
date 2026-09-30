package alert

import (
	"strings"
	"testing"
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

// hbFixture 造一份贴近真实的判定输入：两个打星站点 + 一个没登记的站点。
//
// 「没登记的站点」是这里的重点。生产上实测有 4 个这样的站点，它们从没进过字典，
// 却贡献了 72 个组合里的 31 个。
func hbFixture() *HeartbeatResult {
	dict := &Dict{
		Sites: map[string]DictSite{
			"S_BP": {SiteID: "S_BP", SiteName: "BPreal", Watched: true},
			"S_CP": {SiteID: "S_CP", SiteName: "C88Real", Watched: true},
			"S_NO": {SiteID: "S_NO", SiteName: "", Watched: false}, // 字典里有、没打星
		},
		Rooms: map[string]DictRoom{
			"R1": {RoomID: "R1", RoomNo: "C001", InService: true},
			"R2": {RoomID: "R2", RoomNo: "C002", InService: true},
			"R9": {RoomID: "R9", RoomNo: "D060", InService: false}, // 非在用
		},
	}
	mk := func(site, room string, n float64) (string, hbDim) {
		k := site + "|" + room
		return k, hbDim{Key: k, Labels: map[string]string{"site_id": site, "room_id": room}, Count: n}
	}
	base := map[string]hbDim{}
	for _, e := range []struct {
		s, r string
		n    float64
	}{
		{"S_BP", "R1", 8000}, // 打星站点 + 在用房间 + 高频 → 监控
		{"S_BP", "R2", 3000}, // 同上
		{"S_CP", "R1", 9000}, // 同上
		{"S_BP", "R9", 5000}, // 房间非在用 → 排除
		{"S_NO", "R1", 4000}, // 站点没打星 → 排除
		{"S_XX", "R1", 6000}, // 站点压根不在字典里 → 必须排除
		{"S_BP", "R1x", 10},  // 低频 → 排除
	} {
		k, d := mk(e.s, e.r, e.n)
		base[k] = d
	}
	// 当前窗口：只有 BP/R1 和 CP/R1 有活动；另外故意让两个「本该被排除」的组合
	// 也有活动，验证它们不会混进 Alive 列表。
	cur := map[string]hbDim{}
	for _, k := range []string{"S_BP|R1", "S_CP|R1", "S_XX|R1", "S_BP|R9"} {
		cur[k] = base[k]
	}
	return &HeartbeatResult{Baseline: base, Current: cur, Dict: dict}
}

func hbNames(list []HeartbeatEntry) map[string]bool {
	out := map[string]bool{}
	for _, e := range list {
		out[e.SiteID+"|"+e.RoomID] = true
	}
	return out
}

// 🔴 没登记的站点一个都不许出现 —— 不管它当前有没有活动。
//
// 这条是用户在生产预览里逮到的：活跃列表里赫然列着一个未命名站点，而同一屏上
// 「站点未关注」显示 0。根因是过滤只作用在「可能告警的」那批，活跃的整批绕过。
func TestClassifyExcludesUnknownSiteEvenWhenActive(t *testing.T) {
	res := hbFixture()
	classifyHeartbeat(res, 500)

	alive, missing := hbNames(res.Alive), hbNames(res.Missing)
	for _, bad := range []string{"S_XX|R1", "S_NO|R1", "S_BP|R9"} {
		if alive[bad] {
			t.Errorf("%s 不该出现在「有活动」列表里 —— 它不在监控范围内", bad)
		}
		if missing[bad] {
			t.Errorf("%s 不该出现在「异常」列表里 —— 它不在监控范围内", bad)
		}
	}
	// S_XX|R1 当前是有活动的，如果过滤顺序写反，它会落进 Alive
	if len(res.Alive) != 2 {
		t.Errorf("监控范围内有活动的应当是 2 个(BP/R1, CP/R1)，实际 %d: %v", len(res.Alive), alive)
	}
}

// 统计数字必须自洽：实际监控 = 活跃 + 异常，且 基线 = 监控 + 两类排除。
// 对不上的话，预览上那行「基线 72 － 31 － 5 ＝ 36」就是假的。
func TestClassifyCountsAddUp(t *testing.T) {
	res := hbFixture()
	classifyHeartbeat(res, 500)

	if got, want := res.Monitored(), len(res.Alive)+len(res.Missing); got != want {
		t.Errorf("实际监控 %d ≠ 活跃 %d + 异常 %d", got, len(res.Alive), len(res.Missing))
	}
	total := res.Monitored() + res.SkippedByDict + res.SkippedLowTraffic
	if total != len(res.Baseline) {
		t.Errorf("监控 %d + 站点房间过滤 %d + 低频 %d = %d，与基线 %d 对不上",
			res.Monitored(), res.SkippedByDict, res.SkippedLowTraffic, total, len(res.Baseline))
	}
}

// 没配字典时不能把所有组合都当成"未知站点"排除掉 —— 那会让规则一声不响地
// 什么都不监控。没有字典就是没有过滤依据，全部纳入，只是显示原始 id。
func TestClassifyWithoutDictMonitorsEverything(t *testing.T) {
	res := hbFixture()
	res.Dict = nil
	classifyHeartbeat(res, 500)

	if res.SkippedByDict != 0 {
		t.Errorf("没有字典时不该有「站点/房间」过滤，实际 %d", res.SkippedByDict)
	}
	if res.Monitored() != len(res.Baseline)-res.SkippedLowTraffic {
		t.Errorf("没有字典时除低频外应当全部纳入，实际监控 %d / 基线 %d（低频 %d）",
			res.Monitored(), len(res.Baseline), res.SkippedLowTraffic)
	}
}
