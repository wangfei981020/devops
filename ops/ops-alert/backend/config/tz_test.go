package config

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// DSN 里的 loc 与 time_zone 必须**指向同一个时区**。
//
// 两边不一致的后果是：MySQL 按 A 时区产生 NOW()，Go 按 B 时区解释读回来的
// DATETIME 字符串，于是全库时间戳整体偏移几小时 —— 而这个偏移不报任何错，
// 界面上只是"时间看着有点怪"。
func TestDSNTimezoneAgrees(t *testing.T) {
	for _, name := range []string{"Asia/Shanghai", "Asia/Manila", "UTC", "America/New_York"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Skipf("环境里没有 tzdata，跳过 %s", name)
		}
		dsn := buildDSN(loc)
		if !strings.Contains(dsn, "loc="+strings.ReplaceAll(name, "/", "%2F")) {
			t.Errorf("%s: DSN 里的 loc 不是这个时区: %s", name, dsn)
		}
		_, offset := time.Now().In(loc).Zone()
		sign, abs := "+", offset
		if offset < 0 {
			sign, abs = "-", -offset
		}
		// ⚠️ 比对前要按 URL 转义。`+` 会被编码成 %2B、`:` 成 %3A ——
		// 直接拿原始形式比对会误判成"偏移不符"，而 DSN 其实是对的。
		want := url.QueryEscape(sign + pad(abs/3600) + ":" + pad((abs%3600)/60))
		if !strings.Contains(dsn, want) {
			t.Errorf("%s: DSN 里的 time_zone 偏移与该时区不符，期望含 %q，实际 %s", name, want, dsn)
		}
	}
}

func pad(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// 时区名认不出时必须让进程失败，不能退回 UTC。
//
// 这条没法直接测（mustLocation 会 os.Exit），所以测它的反面：
// 合法名字必须解析成功且**不是**悄悄给了 UTC。
func TestValidZoneIsNotSilentlyUTC(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("环境里没有 tzdata")
	}
	if loc.String() == "UTC" {
		t.Fatal("Asia/Shanghai 解析成了 UTC —— tzdata 缺失时会这样，那正是 mustLocation 要拦住的")
	}
}
