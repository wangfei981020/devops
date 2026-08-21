// SPDX-License-Identifier: LicenseRef-OpsAlert-Enterprise

package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 未授权时 MCP 的四条路由必须**全部 404**。
//
// 这条测的是本批次的核心承诺：需求是「不激活就看不到」，
// 不是「看得到但用不了」。返回 402 等于告诉外面"这里有个功能只是你没买"，
// 那是给噪音治理、回放实验室用的表现，不是给这里的。
//
// ⚠️ 一条都不能漏。漏掉 /mcp/tokens 的后果最严重：
// 没授权的人照样能建令牌，拿到令牌之后 RPC 端点是不是 404 就不重要了。
func TestUnlicensedHidesEveryRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	pub := r.Group("/api/v1")
	auth := r.Group("/api/v1")
	Register(pub, auth, &Server{Licensed: func() bool { return false }})

	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/mcp"},
		{"GET", "/api/v1/mcp/tokens"},
		{"POST", "/api/v1/mcp/tokens"},
		{"DELETE", "/api/v1/mcp/tokens/1"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s 未授权时应 404，实际 %d —— 未授权的人不该知道这个功能存在",
				c.method, c.path, w.Code)
		}
	}
}

// 判据必须在**每次请求时**求值，不是注册时。
//
// 注册时判的话，激活之后要重启进程路由才出现 —— 而多副本下重启是滚动的，
// 会出现「一半副本有、一半没有」的几分钟，表现为"刷新几次好一次"。
func TestGateIsEvaluatedPerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	licensed := false
	r := gin.New()
	pub := r.Group("/api/v1")
	auth := r.Group("/api/v1")
	Register(pub, auth, &Server{Licensed: func() bool { return licensed }})

	get := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/mcp/tokens", nil))
		return w.Code
	}
	if got := get(); got != http.StatusNotFound {
		t.Fatalf("未授权时应 404，实际 %d", got)
	}
	licensed = true
	// 授权之后不该再是 404。这里不关心具体成了什么码
	// （没有真实存储，handler 会自己失败），只关心**路由不再隐身**。
	if got := get(); got == http.StatusNotFound {
		t.Error("激活后仍然 404 —— 判据被固化在注册时了，必须每次请求求值")
	}
}
