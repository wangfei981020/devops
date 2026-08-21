package cdnsource

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 群里对时说的都是北京时间，CF 回的是 UTC。转错 8 小时会让人对着错误的时间段找证据。
func TestToCSTConvertsAndNeverFabricates(t *testing.T) {
	if got := toCST("2026-08-14T08:53:30Z"); got != "2026-08-14 16:53:30" {
		t.Errorf("UTC 08:53:30 应转成北京时间 16:53:30，实际 %q", got)
	}
	// 解析不了就原样返回——宁可显示成 UTC 让人察觉，也不能编一个看似合理的北京时间
	if got := toCST("not-a-time"); got != "not-a-time" {
		t.Errorf("无法解析时应原样返回，实际 %q", got)
	}
}

// GraphQL 恒回 HTTP 200，错误藏在 body 里。
// 若把它当成功、返回空列表，调用方会得出「这段时间没被拦过」——与事实完全相反。
func TestFirewallEventsFailsLoudlyOnGraphQLErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 注意：200
		w.Write([]byte(`{"data":null,"errors":[{"message":"not authorized"}]}`))
	}))
	defer srv.Close()

	c := NewCloudflare("t")
	c.base = srv.URL
	evs, err := c.FirewallEvents(context.Background(), FirewallEventQuery{
		ZoneID: "z1", Since: time.Now().Add(-time.Hour), Until: time.Now(),
	})
	if err == nil {
		t.Fatal("GraphQL 返回 errors 时必须报错，否则会被误读成「没有安全事件」")
	}
	if evs != nil {
		t.Error("失败时不能同时返回事件列表")
	}
	if !strings.Contains(err.Error(), "not authorized") {
		t.Errorf("应保留 CF 原始错误，实际 %q", err.Error())
	}
}

// 带 host 过滤时要真的把变量声明也补上，否则 GraphQL 会报变量未定义。
func TestFirewallEventsParsesAndFiltersByHost(t *testing.T) {
	var gotQuery string
	var gotVars map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.Unmarshal(body, &req)
		gotQuery, gotVars = req.Query, req.Variables

		w.Write([]byte(`{"data":{"viewer":{"zones":[{"firewallEventsAdaptive":[
			{"datetime":"2026-08-14T08:53:30Z","action":"managed_challenge","source":"ratelimit",
			 "ruleId":"a64542f7","clientIP":"8.214.161.28","clientCountryName":"CN",
			 "clientRequestHTTPHost":"openapi-gateway.g32-prod.com",
			 "clientRequestPath":"/api/wallet/transactions/callback",
			 "clientRequestHTTPMethodName":"POST","userAgent":"forest/1.5.35",
			 "edgeResponseStatus":403}]}]}}}`))
	}))
	defer srv.Close()

	c := NewCloudflare("t")
	c.base = srv.URL
	evs, err := c.FirewallEvents(context.Background(), FirewallEventQuery{
		ZoneID: "z1", Host: "openapi-gateway.g32-prod.com",
		Since: time.Now().Add(-time.Hour), Until: time.Now(),
	})
	if err != nil {
		t.Fatalf("不该出错: %v", err)
	}

	if !strings.Contains(gotQuery, "$host:String!") {
		t.Error("带 host 过滤时必须补上 $host 变量声明，否则 GraphQL 报变量未定义")
	}
	if !strings.Contains(gotQuery, "clientRequestHTTPHost:$host") {
		t.Error("host 过滤条件没进 filter")
	}
	if gotVars["host"] != "openapi-gateway.g32-prod.com" {
		t.Errorf("host 变量没传对: %v", gotVars["host"])
	}

	if len(evs) != 1 {
		t.Fatalf("应解析出 1 条事件，实际 %d", len(evs))
	}
	e := evs[0]
	if e.Action != "managed_challenge" || e.Source != "ratelimit" {
		t.Errorf("动作/来源解析错: %s / %s", e.Action, e.Source)
	}
	if e.DatetimeCST != "2026-08-14 16:53:30" {
		t.Errorf("北京时间列不对: %q", e.DatetimeCST)
	}
	if e.ClientIP != "8.214.161.28" || e.EdgeStatus != 403 {
		t.Errorf("客户端IP/边缘状态码解析错: %s / %d", e.ClientIP, e.EdgeStatus)
	}
}

// 不带 host 时不能塞入 $host 变量声明，否则 CF 会因「变量已声明未使用」报错。
func TestFirewallEventsOmitsHostVarWhenNotFiltering(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query string `json:"query"`
		}
		json.Unmarshal(body, &req)
		gotQuery = req.Query
		w.Write([]byte(`{"data":{"viewer":{"zones":[{"firewallEventsAdaptive":[]}]}}}`))
	}))
	defer srv.Close()

	c := NewCloudflare("t")
	c.base = srv.URL
	if _, err := c.FirewallEvents(context.Background(), FirewallEventQuery{
		ZoneID: "z1", Since: time.Now().Add(-time.Hour), Until: time.Now(),
	}); err != nil {
		t.Fatalf("不该出错: %v", err)
	}
	if strings.Contains(gotQuery, "$host") {
		t.Error("没有 host 过滤时不应出现 $host 变量")
	}
}
