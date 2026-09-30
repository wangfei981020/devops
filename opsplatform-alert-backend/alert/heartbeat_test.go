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
	m1 := []hbMissing{{Key: "a"}, {Key: "b"}}
	m2 := []hbMissing{{Key: "b"}, {Key: "a"}}
	if heartbeatSignature(m1) != heartbeatSignature(m2) {
		t.Error("顺序不同但成员相同，签名应当一致，否则每轮都会被当成新故障重复告警")
	}
	m3 := []hbMissing{{Key: "a"}, {Key: "b"}, {Key: "c"}}
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
