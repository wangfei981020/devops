package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// schemeOf 的退路要一条条钉住。
//
// 🔴 背景：v0.109.0 里代码已经有三条退路（X-Forwarded-Proto / Origin / Referer），
// 生产上**仍然**返回 http://（OPSCMDB-031 P0-22）——说明运行时那三条头一条都没命中。
// 「修了没生效」比「没修」更糟：进度表上是 ✅，没人会再看它。
func TestSchemeOf(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mk := func(headers map[string]string) *gin.Context {
		req := httptest.NewRequest("GET", "http://x/api/idp-config", nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		return c
	}
	cases := []struct {
		name string
		h    map[string]string
		want string
	}{
		{"X-Forwarded-Proto 最可信", map[string]string{"X-Forwarded-Proto": "https"}, "https"},
		// 多层代理各追加一个，取**最外层**那个
		{"多层代理取第一个", map[string]string{"X-Forwarded-Proto": "https, http"}, "https"},
		{"Origin 兜底", map[string]string{"Origin": "https://cmdb.example.com"}, "https"},
		{"Referer 兜底", map[string]string{"Referer": "https://cmdb.example.com/sso"}, "https"},
		// 下面四条是 2026-08-20 新增的退路
		{"X-Forwarded-Scheme", map[string]string{"X-Forwarded-Scheme": "https"}, "https"},
		{"Forwarded RFC7239", map[string]string{"Forwarded": `for=1.2.3.4;proto=https;host=x`}, "https"},
		{"Forwarded 带引号", map[string]string{"Forwarded": `proto="https"`}, "https"},
		{"X-Forwarded-Port 443", map[string]string{"X-Forwarded-Port": "443"}, "https"},
		// ⚠️ 什么都没有时**如实**返回 http，不要为了"生产多半是 https"就默认成 https：
		//	那会让本地 http 部署拿到错的值，而且同样不报错
		{"什么都没有 → 如实 http", map[string]string{}, "http"},
		{"X-Forwarded-Port 80 不算 https", map[string]string{"X-Forwarded-Port": "80"}, "http"},
	}
	for _, c := range cases {
		if got := schemeOf(mk(c.h)); got != c.want {
			t.Errorf("%s: schemeOf = %q，期望 %q", c.name, got, c.want)
		}
	}
}
