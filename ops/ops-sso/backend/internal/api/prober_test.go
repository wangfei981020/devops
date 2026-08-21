package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func at(h, m int) time.Time {
	return time.Date(2026, 8, 10, h, m, 0, 0, time.Local)
}

// TestInQuietHours 静默窗口。
//
// # 为什么值得单独测
//
// 它的失效方向是**静默关掉整个功能**：格式写错时如果当成"全天静默"，
// 拨测就再也不跑了，而界面上只会显示「从没测过」——
// 看起来像执行器坏了，实际是一个填错的窗口把它关掉了。
func TestInQuietHours(t *testing.T) {
	cases := []struct {
		name string
		spec string
		now  time.Time
		want bool
	}{
		{"没配就不静默", "", at(3, 0), false},
		{"窗口内", "02:00-04:00", at(3, 0), true},
		{"窗口外", "02:00-04:00", at(5, 0), false},
		{"起点算在内", "02:00-04:00", at(2, 0), true},
		{"终点不算在内", "02:00-04:00", at(4, 0), false},

		// 跨零点是最常见的写法（深夜批处理），也最容易实现错
		{"跨零点·夜里", "22:00-02:00", at(23, 30), true},
		{"跨零点·凌晨", "22:00-02:00", at(1, 0), true},
		{"跨零点·白天不静默", "22:00-02:00", at(12, 0), false},

		// ⚠️ 格式错必须**当成没配**，不能当成全天静默
		{"格式错·缺分钟", "2-4", at(3, 0), false},
		{"格式错·乱写", "上午", at(3, 0), false},
		{"格式错·小时越界", "25:00-26:00", at(3, 0), false},
		{"格式错·只有一段", "02:00", at(3, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inQuietHours(c.spec, c.now); got != c.want {
				t.Fatalf("spec=%q now=%s: want %v, got %v", c.spec, c.now.Format("15:04"), c.want, got)
			}
		})
	}
}

// TestProbeOnceTreats4xxAsAlive 4xx 说明服务活着，不能算故障。
//
// 需要登录的系统对匿名探测几乎必然返回 401/403 —— 把它算成 fail，
// 会让每一个这样的系统永远显示红色，而红色一旦成为常态就没人看了。
func TestProbeOnceStatusSemantics(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{200, "ok"},
		{302, "ok"}, // 跳登录页 = 活着
		{401, "ok"}, // 要登录 = 活着
		{403, "ok"},
		{404, "ok"}, // 路径不对是配置问题，不是"服务挂了"
		{500, "fail"},
		{502, "fail"},
		{503, "fail"},
	}
	for _, c := range cases {
		srv := newStatusServer(c.status)
		got, detail := probeOnce(t.Context(), srv.URL)
		srv.Close()
		if got != c.want {
			t.Fatalf("HTTP %d: want %s, got %s (%s)", c.status, c.want, got, detail)
		}
	}
}

func newStatusServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
}
