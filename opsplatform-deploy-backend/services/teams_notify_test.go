package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testTeamsURL = "https://abc.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/x/triggers/manual/paths/invoke?api-version=1&sig=SECRETSIG"

func TestValidateTeamsWebhookDeploy(t *testing.T) {
	if err := ValidateTeamsWebhook(NormalizeTeamsWebhook(" " + testTeamsURL + "\u200b")); err != nil {
		t.Fatalf("合规地址（带空白和零宽字符）应通过: %v", err)
	}
	for _, bad := range []string{
		"http://abc.environment.api.powerplatform.com/powerautomate/x?sig=1",
		"https://evil.example.com/powerautomate/x?sig=1",
		"https://abc.environment.api.powerplatform.com:8443/powerautomate/x?sig=1",
		"https://abc.environment.api.powerplatform.com/other/x?sig=1",
		"https://abc.environment.api.powerplatform.com/powerautomate/x",
	} {
		if ValidateTeamsWebhook(bad) == nil {
			t.Fatalf("应拒绝: %s", bad)
		}
	}
}

func TestBuildTeamsPayloadStripsLarkAt(t *testing.T) {
	body := "**模块**: a\n\n<at id=\"ou_123\"></at> <at id=\"ou_456\"></at> 请检查失败模块"
	b, err := buildTeamsPayload("❌ [g32-prod] 更新失败 · 1 个模块", body, "red", "查看发布详情", "https://deploy.example/history?expand=1",
		[]TeamsMention{{Name: "dave", Account: "dave@example.com"}, {Name: "张三"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "<at id") || strings.Contains(s, "ou_123") {
		t.Fatalf("飞书 @ 应去掉: %s", s)
	}
	if !strings.Contains(s, "<at>dave</at>") || !strings.Contains(s, `"id":"dave@example.com"`) || !strings.Contains(s, "未能提醒：张三") {
		t.Fatalf("Teams @ 不对: %s", s)
	}
	if !strings.Contains(s, "请检查失败模块") || !strings.Contains(s, `"style":"attention"`) || !strings.Contains(s, "Action.OpenUrl") {
		t.Fatalf("卡片内容不对: %s", s)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
}

func TestSendTeamsRejectsNonTeamsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("不应发到非 Teams 地址")
	}))
	defer srv.Close()
	if err := SendTeamsCardWithRetry(context.Background(), srv.URL+"/powerautomate/x?sig=1", "t", "b", "green", "", "", nil); err == nil {
		t.Fatal("应拒绝")
	}
}

func TestTeamsStatus(t *testing.T) {
	mk := func(code int, ra string) *http.Response {
		h := http.Header{}
		if ra != "" {
			h.Set("Retry-After", ra)
		}
		return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(""))}
	}
	if _, err := teamsStatus(mk(202, "")); err != nil {
		t.Fatal("202 应算成功")
	}
	if w, err := teamsStatus(mk(429, "7")); err == nil || w.Seconds() != 7 {
		t.Fatalf("429 应按 Retry-After 等: %v %v", w, err)
	}
	if w, _ := teamsStatus(mk(403, "")); w >= 0 {
		t.Fatal("403 不应重试")
	}
}

func TestRedactTeamsURLDeploy(t *testing.T) {
	err := redactTeamsURL(io.ErrUnexpectedEOF, testTeamsURL)
	if strings.Contains(err.Error(), "SECRETSIG") {
		t.Fatal("sig 泄露")
	}
	e2 := redactTeamsURL(&testErr{"Post \"" + testTeamsURL + "\": dial tcp"}, testTeamsURL)
	if strings.Contains(e2.Error(), "SECRETSIG") || !strings.Contains(e2.Error(), "<teams-webhook>") {
		t.Fatalf("地址未脱敏: %s", e2)
	}
}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

type fakeTeamsRT struct {
	codes []int
	calls int
	last  []byte
}

func (f *fakeTeamsRT) RoundTrip(r *http.Request) (*http.Response, error) {
	f.last, _ = io.ReadAll(r.Body)
	code := f.codes[len(f.codes)-1]
	if f.calls < len(f.codes) {
		code = f.codes[f.calls]
	}
	f.calls++
	h := http.Header{}
	if code == 429 {
		h.Set("Retry-After", "1")
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

func TestSendTeamsCardWithRetryPayloadAnd429(t *testing.T) {
	old := teamsClient.Transport
	defer func() { teamsClient.Transport = old }()
	f := &fakeTeamsRT{codes: []int{429, 202}}
	teamsClient.Transport = f
	err := SendTeamsCardWithRetry(context.Background(), testTeamsURL, "✅ [g32-prod] 更新成功 · 2 个模块",
		"**模块**: a\n**版本号**: v1\n\n<at id=\"ou_1\"></at>", "green", "查看发布详情", "https://deploy.example/history?expand=9", nil)
	if err != nil || f.calls != 2 {
		t.Fatalf("429 后应重试一次成功: err=%v calls=%d", err, f.calls)
	}
	var env struct {
		Type        string `json:"type"`
		Attachments []struct {
			ContentType string                 `json:"contentType"`
			Content     map[string]interface{} `json:"content"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(f.last, &env); err != nil || env.Type != "message" || len(env.Attachments) != 1 ||
		env.Attachments[0].ContentType != "application/vnd.microsoft.card.adaptive" {
		t.Fatalf("payload 结构不对: %s", f.last)
	}
	s := string(f.last)
	for _, want := range []string{"更新成功", "**版本号**: v1", `"style":"good"`, "expand=9"} {
		if !strings.Contains(s, want) {
			t.Fatalf("payload 缺 %q: %s", want, s)
		}
	}
	if strings.Contains(s, "ou_1") {
		t.Fatalf("飞书 @ 不应出现: %s", s)
	}

	f2 := &fakeTeamsRT{codes: []int{403}}
	teamsClient.Transport = f2
	if err := SendTeamsCardWithRetry(context.Background(), testTeamsURL, "t", "b", "red", "", "", nil); err == nil || f2.calls != 1 {
		t.Fatalf("403 不应重试: err=%v calls=%d", err, f2.calls)
	}
}
