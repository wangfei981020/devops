package alert

import (
	"fmt"
	"strings"
	"time"
)

// 例行维护窗口的本地判定。
//
// 判定为什么放在这边而不是让运维平台算好一个布尔值：字典是带缓存的，算好的值会在
// 缓存里停留到下次刷新，窗口边界就会偏出去十几分钟——而窗口边界正是这件事唯一重要
// 的部分。窗口定义本身极少变动，反倒非常适合走缓存。
//
// 代价是同一套规则在两个代码库里各有一份实现，迟早分叉。所以这里每一条规则都对着
// 运维平台那份写，并且用测试钉住了双方约定的边界语义（跨零点、周日=7、含起不含止）。

// dictLocation 返回运维平台那边的时区。
//
// 🔴 窗口里的 "02:00" 是运维平台的墙上时间。用本地时区去解释它，抑制窗口会整整偏出
// 时差那么多，而且不会报任何错——只表现成「维护期间照样告警」或者「白天莫名不告警」。
// 拿不到时区信息时退回 UTC 并在调用处留痕，不猜。
func dictLocation(d *Dict) *time.Location {
	if d == nil {
		return time.UTC
	}
	if d.TZName != "" || d.TZOffsetSec != 0 {
		return time.FixedZone(d.TZName, d.TZOffsetSec)
	}
	return time.UTC
}

// windowCoversRoom 判断这条窗口是否适用于某张桌台。
//
// 房间号和桌台号都能匹配：一张桌台可能有多个房间（N13 下有 N013 和 N013-2），
// 写桌台号是「这张桌台的全部房间」的批量写法。
func windowCoversRoom(w *DictWindow, roomNo, tableNo string) bool {
	list := strings.TrimSpace(w.TableNos)
	if list == "" {
		return false
	}
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" {
			return true
		}
		if roomNo != "" && strings.EqualFold(item, roomNo) {
			return true
		}
		if tableNo != "" && strings.EqualFold(item, tableNo) {
			return true
		}
	}
	return false
}

// windowInstance 算出某个自然日上这条窗口的 [开始, 结束)。
// end <= start 视为跨零点，结束落到次日。日期不符合重复规则时返回 ok=false。
func windowInstance(w *DictWindow, day time.Time, loc *time.Location) (time.Time, time.Time, bool) {
	sh, sm, ok1 := parseHM(w.StartTime)
	eh, em, ok2 := parseHM(w.EndTime)
	if !ok1 || !ok2 {
		return time.Time{}, time.Time{}, false
	}

	switch w.RepeatType {
	case "weekly":
		// time.Weekday 里周日是 0，而配置用的是 1=周一 … 7=周日
		wd := int(day.Weekday())
		if wd == 0 {
			wd = 7
		}
		if !csvHasInt(w.Weekdays, wd) {
			return time.Time{}, time.Time{}, false
		}
	case "monthly":
		if !csvHasInt(w.MonthDays, day.Day()) {
			return time.Time{}, time.Time{}, false
		}
	case "once":
		if day.Format("2006-01-02") != strings.TrimSpace(w.OnceDate) {
			return time.Time{}, time.Time{}, false
		}
	}
	// daily 不额外判断

	start := time.Date(day.Year(), day.Month(), day.Day(), sh, sm, 0, 0, loc)
	end := time.Date(day.Year(), day.Month(), day.Day(), eh, em, 0, 0, loc)
	if !end.After(start) {
		end = end.AddDate(0, 0, 1)
	}
	return start, end, true
}

// ActiveMaintWindow 返回此刻正覆盖这张桌台的例行窗口，没有则返回 nil。
//
// 前后各查一天，是为了跨零点的窗口：now=01:30 时，命中的是「昨天 23:00 起」那个
// 实例，只查今天会漏掉。
func ActiveMaintWindow(d *Dict, roomNo, tableNo string, now time.Time) *DictWindow {
	if d == nil || len(d.Windows) == 0 {
		return nil
	}
	loc := dictLocation(d)
	local := now.In(loc)
	for i := range d.Windows {
		w := &d.Windows[i]
		if !windowCoversRoom(w, roomNo, tableNo) {
			continue
		}
		for _, offset := range []int{-1, 0} {
			day := local.AddDate(0, 0, offset)
			start, end, ok := windowInstance(w, day, loc)
			if !ok {
				continue
			}
			// 含起不含止：02:00~04:00 的窗口里 04:00 整已经算窗口外，
			// 否则相邻两个窗口会在交界那一秒同时命中。
			if !local.Before(start) && local.Before(end) {
				return w
			}
		}
	}
	return nil
}

func parseHM(v string) (int, int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d:%d", &h, &m); err != nil {
		return 0, 0, false
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func csvHasInt(csv string, n int) bool {
	for _, p := range strings.Split(csv, ",") {
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &v); err == nil && v == n {
			return true
		}
	}
	return false
}
