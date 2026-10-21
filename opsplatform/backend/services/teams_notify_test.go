package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const goodTeams = "https://default00000000000000000000000000000.00.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/0a1b2c/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=AbC-123_x"

func TestValidateTeamsWebhook(t *testing.T) {
	if err := ValidateTeamsWebhook(goodTeams); err != nil {
		t.Fatalf("真实格式的地址被拒: %v", err)
	}
	// 复制带进来的不可见字符，清洗后应能通过
	dirty := strings.Replace(goodTeams, "/triggers/", "/trig​gers /", 1)
	if err := ValidateTeamsWebhook(NormalizeTeamsWebhook(dirty)); err != nil {
		t.Errorf("清洗后应通过: %v", err)
	}
	for name, bad := range map[string]string{
		"http":   strings.Replace(goodTeams, "https://", "http://", 1),
		"其他主机":   strings.Replace(goodTeams, "environment.api.powerplatform.com", "evil.com", 1),
		"后缀伪造":   strings.Replace(goodTeams, ".powerplatform.com:443", ".powerplatform.com.evil.com:443", 1),
		"非443端口": strings.Replace(goodTeams, ":443", ":8443", 1),
		"缺sig":   strings.Replace(goodTeams, "&sig=AbC-123_x", "", 1),
		"路径不对":   strings.Replace(goodTeams, "/powerautomate/", "/x/", 1),
		"路径..":   strings.Replace(goodTeams, "/0a1b2c/", "/../../x/", 1),
		"lark地址": "https://open.larksuite.com/open-apis/bot/v2/hook/abc",
		"带账号":    strings.Replace(goodTeams, "https://", "https://u:p@", 1), // scan-secrets: allow
	} {
		if ValidateTeamsWebhook(bad) == nil {
			t.Errorf("%s 应被拒绝", name)
		}
	}
}

func TestTeamsAccountFor(t *testing.T) {
	cases := []struct {
		name, explicit, domain, want string
		derived                      bool
	}{
		{"alice", "", "example.com", "alice@example.com", true},
		{"Bob", "", "example.com", "bob@example.com", true},
		{"carol", " Carol.W@EXAMPLE.com", "example.com", "carol.w@example.com", false},
		{"Mary Ann Lee", "", "example.com", "", false},
		{"张三", "", "example.com", "", false},
		{"alice", "", "", "", false},
	}
	for _, c := range cases {
		got, d := TeamsAccountFor(c.name, c.explicit, c.domain)
		if got != c.want || d != c.derived {
			t.Errorf("TeamsAccountFor(%q,%q,%q)=(%q,%v) 期望 (%q,%v)", c.name, c.explicit, c.domain, got, d, c.want, c.derived)
		}
	}
}

func TestBuildTeamsPayload(t *testing.T) {
	b, err := buildTeamsPayload("🔴 桌台 N13 维护中", "**桌台**：N13\n**状态**：维护中", "red", "查看详情", "https://ops.example/table-alert",
		[]TeamsMention{{Name: "alice", Account: "alice@example.com", Derived: true}, {Name: "alice2", Account: "alice@example.com"}, {Name: "Mary Ann Lee"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"type":"message"`, `"style":"attention"`, `"text":"**桌台**：N13"`, `"text":"**状态**：维护中"`,
		`通知: <at>alice</at>`, `"id":"alice@example.com"`, `未能提醒：Mary Ann Lee`, `"type":"Action.OpenUrl"`, `"url":"https://ops.example/table-alert"`} {
		if !strings.Contains(s, want) {
			t.Errorf("卡片缺少 %s", want)
		}
	}
	if strings.Count(s, `"type":"mention"`) != 1 {
		t.Error("同一账号只应 @ 一次")
	}
	if strings.Contains(s, `\u003c`) {
		t.Error("不应做 HTML 转义")
	}
	long, _ := buildTeamsPayload("t", strings.Repeat("长", teamsMaxBodyRunes+100), "green", "", "", nil)
	if !strings.Contains(string(long), "内容过长已截断") || strings.Contains(string(long), "Action.OpenUrl") {
		t.Error("超长正文应截断；没有链接时不应有按钮")
	}
}

func TestTeamsMentionLog(t *testing.T) {
	got := TeamsMentionLog([]TeamsMention{{Name: "alice", Account: "alice@example.com", Derived: true}, {Name: "carol", Account: "carol.w@example.com"}, {Name: "张三"}})
	want := "@ alice(alice@example.com 默认) carol(carol.w@example.com 填写)；未配置 Teams 账号：张三"
	if got != want {
		t.Errorf("日志行\n  实际 %s\n  期望 %s", got, want)
	}
}

// 发送：用 httptest 替代 Teams，绕过地址校验直接测状态码处理
func sendTo(t *testing.T, handler http.HandlerFunc, retry bool) (int32, error) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		io.Copy(io.Discard, r.Body)
		handler(w, r)
	}))
	defer srv.Close()
	// 测试地址过不了 Teams 校验，临时只测发送和状态码分支
	err := func() error {
		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			wait, err := postTeamsForTest(srv.URL)
			if err == nil {
				return nil
			}
			lastErr = err
			if !retry || wait < 0 {
				break
			}
		}
		return lastErr
	}()
	return atomic.LoadInt32(&n), err
}

func TestTeamsStatusHandling(t *testing.T) {
	for _, code := range []int{200, 202} {
		if _, err := sendTo(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }, false); err != nil {
			t.Errorf("HTTP %d 应算成功: %v", code, err)
		}
	}
	n, err := sendTo(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }, true)
	if err == nil || n != 1 {
		t.Errorf("400 应直接失败不重试，实际请求 %d 次 err=%v", n, err)
	}
	n, err = sendTo(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, true)
	if err == nil || n != 3 {
		t.Errorf("500 应重试到 3 次，实际 %d 次", n)
	}
	var seen int32
	n, err = sendTo(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&seen, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(202)
	}, true)
	if err != nil || n != 2 {
		t.Errorf("429 一次后应重试成功，实际 %d 次 err=%v", n, err)
	}
	if (&TeamsStatusError{StatusCode: 429}).Error() == "" {
		t.Error("限流应有说明")
	}
}

func TestRedactTeamsURL(t *testing.T) {
	err := redactTeamsURL(io.ErrUnexpectedEOF, goodTeams)
	if err.Error() != io.ErrUnexpectedEOF.Error() {
		t.Errorf("不含地址的错误不应被改动: %v", err)
	}
	e2 := redactTeamsURL(&wrapped{"Post \"" + goodTeams + "\": dial tcp: refused"}, goodTeams)
	if strings.Contains(e2.Error(), "AbC-123_x") || !strings.Contains(e2.Error(), "<teams-webhook>") {
		t.Errorf("地址或 sig 没打码: %v", e2)
	}
}

type wrapped struct{ s string }

func (w *wrapped) Error() string { return w.s }

// 生产路径的地址校验挡在 httptest 前面：这里复用 sendTeamsOnce 的状态码分支逻辑
func postTeamsForTest(u string) (time.Duration, error) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", u, strings.NewReader("{}"))
	resp, err := teamsClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return teamsStatus(resp)
}
