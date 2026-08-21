package handlers

import (
	"strings"
	"testing"
	"time"
)

// 有效期解析的三态与边界。
//
// 守的是 OPSCMDB-031 P1-70：这是一个能只读访问全库 87 个工具的凭据，
// 而它**永不失效**——接口里没有任何 expires_at/ttl，
// 界面上也看不出它什么时候会失效，因为它不会。
func TestParseTokenExpiry(t *testing.T) {
	t.Run("空 = 永久（NULL）", func(t *testing.T) {
		v, err := parseTokenExpiry("")
		if err != nil {
			t.Fatalf("空值不该报错：%v", err)
		}
		if v != nil {
			t.Errorf("空值必须落成 NULL 而不是零值日期——零值日期在任何按时间比较的地方都表现成「早就过期了」：%v", v)
		}
		v2, _ := parseTokenExpiry("   ")
		if v2 != nil {
			t.Error("只有空格也算空")
		}
	})

	t.Run("只给日期 → 当天结束", func(t *testing.T) {
		day := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
		v, err := parseTokenExpiry(day)
		if err != nil {
			t.Fatalf("日期写法必须支持（界面的日期选择器给的就是它）：%v", err)
		}
		got := v.(time.Time)
		if got.Hour() != 23 {
			t.Errorf("只给日期时要按当天结束算，否则「选今天」等于立刻过期：%v", got)
		}
	})

	t.Run("RFC3339", func(t *testing.T) {
		want := time.Now().Add(48 * time.Hour).Truncate(time.Second)
		v, err := parseTokenExpiry(want.Format(time.RFC3339))
		if err != nil {
			t.Fatalf("RFC3339 必须支持：%v", err)
		}
		if !v.(time.Time).Equal(want) {
			t.Errorf("解析出来的时刻不对：%v vs %v", v, want)
		}
	})

	t.Run("🔴 过去的时刻必须当场拒绝", func(t *testing.T) {
		// 建一条生下来就是死的令牌，调用方会拿着它调半天只看到 401，
		// 而查不出为什么。一个不会让人停下来的检查等于没有检查
		_, err := parseTokenExpiry(time.Now().Add(-time.Hour).Format(time.RFC3339))
		if err == nil {
			t.Fatal("过去的时刻被接受了")
		}
		if !strings.Contains(err.Error(), "一次都用不了") {
			t.Errorf("错误信息要说清后果：%v", err)
		}
	})

	t.Run("格式不对要报错而不是静默当成永久", func(t *testing.T) {
		// 静默当成永久是最坏的方向：管理员以为设了有效期，实际这条永不失效
		_, err := parseTokenExpiry("下个月")
		if err == nil {
			t.Fatal("解析不了却没报错——管理员会以为设上了有效期，而它永不失效")
		}
		if !strings.Contains(err.Error(), "2026-12-31") {
			t.Errorf("要给出正确写法的例子：%v", err)
		}
	})
}
