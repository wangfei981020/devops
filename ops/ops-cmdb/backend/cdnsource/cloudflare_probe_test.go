package cdnsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Cloudflare 的两种「假成功」是这套体检最容易栽的地方，各测一条。

// 坑 1：rulesets 类接口权限不足时回的是 HTTP 200 + success=false，
// 而不是 403。只看状态码会把「没权限」判成「通过」——那样体检就白做了。
func TestProbeTreatsHTTP200WithSuccessFalseAsNoPermission(t *testing.T) {
	verdict, ok := verdictOf(200, errStr("[0] request is not authorized"))
	if ok {
		t.Fatal("HTTP 200 + request is not authorized 被判成了通过——正是本次要防的误判")
	}
	if verdict != "no_permission" {
		t.Errorf("应判为 no_permission，实际 %q", verdict)
	}
}

// 坑 2：真正的故障（超时、5xx）不能和权限不足混为一谈——
// 前者要重试或找 CF，后者是自己去控制台勾一下，处置完全不同。
func TestProbeSeparatesErrorFromNoPermission(t *testing.T) {
	if v, _ := verdictOf(500, errStr("internal error")); v != "error" {
		t.Errorf("500 应判为 error，实际 %q", v)
	}
	if v, _ := verdictOf(0, errStr("dial tcp: i/o timeout")); v != "error" {
		t.Errorf("网络错误应判为 error，实际 %q", v)
	}
	if v, ok := verdictOf(200, nil); v != "pass" || !ok {
		t.Errorf("无错误应判为 pass，实际 %q", v)
	}
	// 403 与 401 都是权限/认证问题
	for _, code := range []int{401, 403} {
		if v, _ := verdictOf(code, errStr("forbidden")); v != "no_permission" {
			t.Errorf("HTTP %d 应判为 no_permission，实际 %q", code, v)
		}
	}
}

// 账号级 token 调用只认用户级的端点时，CF 回 401 Invalid API Token /
// 400 does not support account owned tokens。那是端点自身的限制，**勾再多权限也不会变**。
// 报成 no_permission 会让人去控制台白折腾一轮——第一版就犯过这个错。
func TestProbeMarksAccountTokenLimitsAsNotApplicable(t *testing.T) {
	cases := []struct {
		code int
		msg  string
	}{
		{401, "[1000] Invalid API Token"},
		{400, "[1011] Page Rules endpoint does not support account owned tokens."},
	}
	for _, c := range cases {
		v, ok := verdictOf(c.code, errStr(c.msg))
		if v != "not_applicable" {
			t.Errorf("%q 应判为 not_applicable，实际 %q", c.msg, v)
		}
		if !ok {
			t.Errorf("%q 不该计入失败项——它不是需要处理的问题", c.msg)
		}
	}
	// 别误伤：真正的权限不足仍要判 no_permission
	if v, _ := verdictOf(200, errStr("[0] request is not authorized")); v != "no_permission" {
		t.Errorf("真权限不足被误判成 %q", v)
	}
}

// 端到端：串一个假 CF，确认 token 身份被解析出来、
// 且「列表通过但明细没权限」这个组合能被如实分开报告。
func TestProbeTokenReportsIdentityAndPerItemVerdict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/user/tokens/verify"):
			w.Write([]byte(`{"success":true,"errors":[],"result":
				{"id":"tok-abc123","status":"active","expires_on":"2027-01-01T00:00:00Z"}}`))
		case strings.HasSuffix(p, "/zones"):
			w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"zone1","name":"example.com"}]}`))
		case strings.Contains(p, "/rulesets/"):
			// 明细：权限不足，但 CF 仍回 200
			w.Write([]byte(`{"success":false,"errors":[{"code":0,"message":"request is not authorized"}],"result":null}`))
		case strings.HasSuffix(p, "/rulesets"):
			w.Write([]byte(`{"success":true,"errors":[],"result":
				[{"id":"rs1","name":"default","kind":"zone","phase":"http_ratelimit"}]}`))
		case strings.HasSuffix(p, "/graphql"):
			// GraphQL 恒 200，错误藏在 body 里
			w.Write([]byte(`{"data":null,"errors":[{"message":"not authorized for zone analytics"}]}`))
		default:
			w.Write([]byte(`{"success":true,"errors":[],"result":[]}`))
		}
	}))
	defer srv.Close()

	c := NewCloudflare("fake-token")
	c.base = srv.URL

	p := c.ProbeToken(context.Background(), "example.com")

	if p.TokenID != "tok-abc123" || p.TokenState != "active" {
		t.Errorf("token 身份没解析出来: id=%q state=%q", p.TokenID, p.TokenState)
	}
	if p.ProbedZone != "example.com" {
		t.Errorf("探测站点应为 example.com，实际 %q", p.ProbedZone)
	}

	find := func(sub string) *ProbeCheck {
		for i := range p.Checks {
			if strings.Contains(p.Checks[i].Name, sub) {
				return &p.Checks[i]
			}
		}
		return nil
	}

	if ck := find("规则集列表"); ck == nil || !ck.OK {
		t.Error("规则集列表应判为通过")
	}
	// 同一个 token、同一个 zone，列表通过而明细不通——这正是本次生产现象
	ck := find("规则集明细")
	if ck == nil {
		t.Fatal("没有产出规则集明细的探测项")
	}
	if ck.OK || ck.Verdict != "no_permission" {
		t.Errorf("规则集明细应判为 no_permission，实际 ok=%v verdict=%q", ck.OK, ck.Verdict)
	}
	if !strings.Contains(ck.Need, "Zone WAF Rules") {
		t.Errorf("http_ratelimit 应提示 Zone WAF Rules 权限，实际 %q", ck.Need)
	}
	if !strings.Contains(ck.Detail, "not authorized") {
		t.Errorf("应原样保留 CF 的错误文本，实际 %q", ck.Detail)
	}

	// GraphQL：HTTP 200 但 errors 非空，必须判为不通过
	if ck := find("流量分析"); ck == nil || ck.OK {
		t.Error("GraphQL 返回 errors 时不能判成通过")
	}

	if !strings.Contains(p.Summary, "tok-abc123") {
		t.Errorf("汇总里应带上 token id 方便与控制台对照，实际 %q", p.Summary)
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }
