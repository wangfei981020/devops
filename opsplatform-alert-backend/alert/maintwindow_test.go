package alert

import (
	"errors"
	"testing"
	"time"
)

// 运维平台那边用 time.Local，判定却发生在这边 —— 告警平台的 DSN 是钉死 UTC 的。
// 按本地时区解释「02:00」会整整偏出时差那么多，而且不报任何错，只表现成维护期间
// 照样告警。这条是整个窗口功能里最容易出错、后果又最隐蔽的一处。
func TestMaintWindowUsesOpsPlatformTimezone(t *testing.T) {
	d := &Dict{
		TZName: "CST", TZOffsetSec: 8 * 3600, // 运维平台在 UTC+8
		Windows: []DictWindow{{
			Name: "每日凌晨保养", RepeatType: "daily",
			StartTime: "02:00", EndTime: "04:00", TableNos: "*", Action: "suppress",
		}},
	}
	// UTC 19:00 = 北京时间次日 03:00，正处在窗口内
	inWindow := time.Date(2026, 9, 30, 19, 0, 0, 0, time.UTC)
	if w := ActiveMaintWindow(d, "D059", "D59", inWindow); w == nil {
		t.Error("UTC 19:00 就是北京时间 03:00，应当命中凌晨保养窗口 —— 没命中说明时区没生效")
	}
	// UTC 03:00 = 北京时间 11:00，大白天，不该命中
	outWindow := time.Date(2026, 9, 30, 3, 0, 0, 0, time.UTC)
	if w := ActiveMaintWindow(d, "D059", "D59", outWindow); w != nil {
		t.Errorf("北京时间 11:00 不该命中凌晨保养窗口，实际命中 %q", w.Name)
	}
}

// 没有时区信息时退回 UTC 而不是本地时区：本地时区是容器的 TZ，跟运维平台没有
// 任何关系，拿它当默认值等于随机猜一个偏移。
func TestMaintWindowFallsBackToUTCWithoutTZ(t *testing.T) {
	if loc := dictLocation(&Dict{}); loc != time.UTC {
		t.Errorf("没有时区信息时应退回 UTC，实际 %v", loc)
	}
	if loc := dictLocation(nil); loc != time.UTC {
		t.Errorf("字典为 nil 时应退回 UTC，实际 %v", loc)
	}
}

// 跨零点窗口（23:00~01:00）在 00:30 时命中的是「昨天那一实例」。
// 只查当天的话这段时间会完全漏掉，而深夜恰恰是例行保养最常安排的时段。
func TestMaintWindowSpansMidnight(t *testing.T) {
	d := &Dict{TZName: "UTC", Windows: []DictWindow{{
		Name: "跨零点保养", RepeatType: "daily",
		StartTime: "23:00", EndTime: "01:00", TableNos: "*",
	}}}
	for _, tc := range []struct {
		hm   string
		want bool
	}{
		{"23:30", true},  // 起始日
		{"00:30", true},  // 次日凌晨，属于昨天那个实例
		{"01:30", false}, // 已经出窗口
		{"12:00", false},
	} {
		var h, m int
		_, _ = parseHMTest(tc.hm, &h, &m)
		now := time.Date(2026, 9, 30, h, m, 0, 0, time.UTC)
		got := ActiveMaintWindow(d, "X", "X", now) != nil
		if got != tc.want {
			t.Errorf("%s 命中=%v，期望 %v", tc.hm, got, tc.want)
		}
	}
}

// 含起不含止。相邻两个窗口若都算闭区间，交界那一秒会同时命中两个，
// 而「命中哪个窗口」决定了用哪条 action。
func TestMaintWindowBoundaryIsHalfOpen(t *testing.T) {
	d := &Dict{TZName: "UTC", Windows: []DictWindow{{
		RepeatType: "daily", StartTime: "02:00", EndTime: "04:00", TableNos: "*",
	}}}
	at := func(h, m int) time.Time { return time.Date(2026, 9, 30, h, m, 0, 0, time.UTC) }
	if ActiveMaintWindow(d, "X", "X", at(2, 0)) == nil {
		t.Error("02:00 整应当算在窗口内（含起）")
	}
	if ActiveMaintWindow(d, "X", "X", at(4, 0)) != nil {
		t.Error("04:00 整应当算在窗口外（不含止）")
	}
}

// 周几用 1=周一 … 7=周日，而 Go 的 time.Weekday 里周日是 0。
// 换算漏掉的话周日的窗口会静默失效，或者错配到周一。
func TestMaintWindowWeeklySundayIsSeven(t *testing.T) {
	d := &Dict{TZName: "UTC", Windows: []DictWindow{{
		RepeatType: "weekly", Weekdays: "7", StartTime: "02:00", EndTime: "04:00", TableNos: "*",
	}}}
	sunday := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC) // 2026-10-04 是周日
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("测试数据错了，2026-10-04 是 %v", sunday.Weekday())
	}
	if ActiveMaintWindow(d, "X", "X", sunday) == nil {
		t.Error("配置 weekdays=7 应当命中周日")
	}
	monday := time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)
	if ActiveMaintWindow(d, "X", "X", monday) != nil {
		t.Error("weekdays=7 不该命中周一")
	}
}

// 桌台匹配：房间号、桌台号、* 三种写法，且大小写不敏感。
// 空列表不能当成「全部」—— 那会让一条没填桌台的窗口静默抑制掉所有告警。
func TestWindowCoversRoom(t *testing.T) {
	cases := []struct {
		tableNos, roomNo, tableNo string
		want                      bool
	}{
		{"*", "D059", "D59", true},
		{"D059", "D059", "D59", true},
		{"d059", "D059", "D59", true}, // 大小写不敏感
		{"D59", "D059", "D59", true},  // 桌台号=该桌台全部房间
		{"D060", "D059", "D59", false},
		{"", "D059", "D59", false}, // 空列表不是「全部」
		{"D001, D059 ,D002", "D059", "D59", true},
	}
	for _, c := range cases {
		w := &DictWindow{TableNos: c.tableNos}
		if got := windowCoversRoom(w, c.roomNo, c.tableNo); got != c.want {
			t.Errorf("table_nos=%q room=%q table=%q → %v，期望 %v",
				c.tableNos, c.roomNo, c.tableNo, got, c.want)
		}
	}
}

// 时间格式非法时窗口不生效，而不是当成 00:00 —— 那会把整天都算进维护期，
// 一条填错的窗口就能让所有告警静默。
func TestWindowInstanceRejectsBadTime(t *testing.T) {
	for _, bad := range []string{"", "25:00", "02:99", "abc", "2"} {
		w := &DictWindow{RepeatType: "daily", StartTime: bad, EndTime: "04:00", TableNos: "*"}
		if _, _, ok := windowInstance(w, time.Now(), time.UTC); ok {
			t.Errorf("start_time=%q 非法，窗口不该生效", bad)
		}
	}
}

func parseHMTest(v string, h, m *int) (int, error) {
	a, b, ok := parseHM(v)
	*h, *m = a, b
	if !ok {
		return 0, errBadHM
	}
	return 2, nil
}

var errBadHM = errors.New("bad hm")
