package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 审计结果必须分三态：success / fail / accepted。
//
//	## 为什么值得一条专门的测试
//
//	原来的判据是「HTTP < 400 就是 success」。看起来没毛病，直到遇上 202：
//
//	  域名同步 → 202 Accepted（活儿在后台 goroutine 里跑一两分钟）
//	           → 审计当场记下 success
//	           → 后台真跑起来，GoDaddy 凭据 401，执行记录记 fail
//
//	同一件事，系统里留下两条互相矛盾的记录，而人看见的是显眼的那条绿色 ——
//	401 就这么被盖了近 20 分钟没人发现（OPSCMDB-009）。
//
//	⚠️ 这不是"少记了一条"，是**审计在说谎**。审计说谎比没有审计更糟：
//	没有审计时人会自己去查，审计说成功时人就不查了。
//
//	所以 202 必须单独成一态。这条测试钉住这个判定，
//	防止有人为了"少一个状态值"再把它并回 success。
func TestAuditStatusFromHTTPCode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		code int
		want string
		why  string
	}{
		{http.StatusOK, "success", "同步完成的接口"},
		{http.StatusCreated, "success", "建对象"},
		{http.StatusAccepted, "accepted", "⚠️ 202 是「受理」不是「成功」——真结果在执行记录里"},
		{http.StatusBadRequest, "fail", "参数错"},
		{http.StatusConflict, "fail", "正在同步中"},
		{http.StatusUnauthorized, "fail", "凭据不对"},
		{http.StatusBadGateway, "fail", "外部厂商挂了"},
		{http.StatusInternalServerError, "fail", "自己崩了"},
	}

	for _, c := range cases {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Writer.WriteHeader(c.code)

		got := auditStatusOf(ctx.Writer.Status())
		if got != c.want {
			t.Errorf("HTTP %d → 审计状态 %q，应为 %q（%s）", c.code, got, c.want, c.why)
		}
	}
}
