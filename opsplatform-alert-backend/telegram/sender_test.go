package telegram

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"opsplatform-alert-backend/models"
)

func newTestSender(t *testing.T, h http.HandlerFunc) (*Sender, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	s := NewSender(models.NotifyChannel{
		ID: 7, Name: "tg-test", ChannelType: "telegram",
		BotToken: "123:ABC", ChatID: "-1001234567890",
	})
	s.apiBase = srv.URL
	s.sleep = func(time.Duration) {} // no real waiting in tests
	return s, srv
}

func TestSendCardPostsToSendMessage(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}

	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	})

	resp, err := s.SendCard("支付超时", "**服务:** api", "S2", nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp, `"ok":true`) {
		t.Errorf("response = %q, want the raw API body", resp)
	}
	if gotPath != "/bot123:ABC/sendMessage" {
		t.Errorf("path = %q, want /bot123:ABC/sendMessage", gotPath)
	}
	if gotBody["chat_id"] != "-1001234567890" {
		t.Errorf("chat_id = %v", gotBody["chat_id"])
	}
	if gotBody["parse_mode"] != "HTML" {
		t.Errorf("parse_mode = %v, want HTML", gotBody["parse_mode"])
	}
	if text, _ := gotBody["text"].(string); !strings.Contains(text, "<b>服务:</b> api") {
		t.Errorf("text = %q, want converted HTML", text)
	}
	if _, present := gotBody["message_thread_id"]; present {
		t.Error("message_thread_id must be omitted when thread_id is 0")
	}
}

func TestSendCardIncludesThreadIDWhenSet(t *testing.T) {
	var gotBody map[string]interface{}
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"ok":true}`))
	})
	s.channel.ThreadID = 99

	if _, err := s.SendCard("t", "c", "S2", nil, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["message_thread_id"] != float64(99) {
		t.Errorf("message_thread_id = %v, want 99", gotBody["message_thread_id"])
	}
}

func TestSendCardRetriesOnRateLimitUsingRetryAfter(t *testing.T) {
	var calls int32
	var slept []time.Duration

	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":7}}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	s.sleep = func(d time.Duration) { slept = append(slept, d) }

	if _, err := s.SendCard("t", "c", "S2", nil, false); err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if len(slept) != 1 || slept[0] != 7*time.Second {
		t.Errorf("slept = %v, want one 7s wait taken from retry_after", slept)
	}
}

func TestSendCardCapsRetryAfter(t *testing.T) {
	var slept []time.Duration
	var calls int32
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":9999}}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	s.sleep = func(d time.Duration) { slept = append(slept, d) }

	if _, err := s.SendCard("t", "c", "S2", nil, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(slept) != 1 || slept[0] != maxRetryAfter {
		t.Errorf("slept = %v, want it capped at %v", slept, maxRetryAfter)
	}
}

func TestSendCardDoesNotRetryOnBadRequest(t *testing.T) {
	var calls int32
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"error_code":400,"description":"chat not found"}`))
	})

	resp, err := s.SendCard("t", "c", "S2", nil, false)
	if err == nil {
		t.Fatal("expected an error for HTTP 400")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (400 must not be retried)", calls)
	}
	if !strings.Contains(resp, "chat not found") {
		t.Errorf("resp = %q, want it to propagate the API's failure body so an operator can see why", resp)
	}
}

func TestSendCardGivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"ok":false,"error_code":500}`))
	})

	resp, err := s.SendCard("t", "c", "S2", nil, false)
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if calls != int32(maxRetries+1) {
		t.Errorf("calls = %d, want %d", calls, maxRetries+1)
	}
	if !strings.Contains(resp, `"error_code":500`) {
		t.Errorf("resp = %q, want it to propagate the last failure body so an operator can see why", resp)
	}
}

// TestSendCardTreatsHTTP200OkFalseAsFailure pins down the highest-risk silent
// failure mode in doSend: Telegram can answer HTTP 200 while the body itself
// says the call failed (e.g. an unauthorized bot). Success requires both
// StatusCode == 200 AND parsed.OK; if a future edit loosened that to check
// only the status code, alerts would be dropped while every other test still
// passed. A 200 status is not retryable, so the handler must be hit exactly
// once.
func TestSendCardTreatsHTTP200OkFalseAsFailure(t *testing.T) {
	var calls int32
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		// Default status is 200 OK; only the body reports failure.
		w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	})

	if _, err := s.SendCard("t", "c", "S2", nil, false); err == nil {
		t.Fatal("expected an error when the body says ok:false, even on HTTP 200")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (HTTP 200 is not a retryable status)", calls)
	}
}

// TestSendCardFallsBackToOneSecondWhenRetryAfterMissing pins down doSend's
// fallback for a 429 body that omits parameters.retry_after entirely: it
// must still wait (rather than busy-looping), and the fallback is exactly
// one second. A future change to that default would otherwise go unnoticed.
func TestSendCardFallsBackToOneSecondWhenRetryAfterMissing(t *testing.T) {
	var calls int32
	var slept []time.Duration
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	s.sleep = func(d time.Duration) { slept = append(slept, d) }

	if _, err := s.SendCard("t", "c", "S2", nil, false); err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Errorf("slept = %v, want one 1s fallback wait when retry_after is absent", slept)
	}
}

func TestSendTextSendsPlainBody(t *testing.T) {
	var gotBody map[string]interface{}
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"ok":true}`))
	})

	if _, err := s.SendText("hello <world>", nil, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text, _ := gotBody["text"].(string); !strings.Contains(text, "hello &lt;world&gt;") {
		t.Errorf("text = %q, want escaped plain text", text)
	}
}

func TestTestWebhookSends(t *testing.T) {
	s, _ := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	if _, err := s.TestWebhook(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSendCardTransportErrorDoesNotLeakToken pins down the fix for a real
// credential leak: net/http wraps a dial failure in a *url.Error whose
// Error() embeds the full request URL, "bot<token>/sendMessage" included.
// That string used to flow unredacted into notify.MultiSender's SendResult,
// alert_logs.lark_response, and the notify-channels test endpoint's response
// body — any transport hiccup (refused connection, DNS blip, an
// attacker-supplied unreachable proxy_url) was enough to hand out a live bot
// token. This closes a server, so the request genuinely fails at the
// transport layer rather than getting an HTTP error response.
func TestSendCardTransportErrorDoesNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	closedURL := srv.URL
	srv.Close() // now unreachable: any request to it fails at dial time

	const token = "123456789:AA_SUPER_SECRET_TOKEN"
	s := NewSender(models.NotifyChannel{
		ID: 7, Name: "tg-test", ChannelType: "telegram",
		BotToken: token, ChatID: "-1001234567890",
	})
	s.apiBase = closedURL
	s.sleep = func(time.Duration) {} // no real waiting in tests

	_, err := s.SendCard("t", "c", "S2", nil, false)
	if err == nil {
		t.Fatal("expected an error against a closed server")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaks the bot token: %v", err)
	}
	if !strings.Contains(err.Error(), "****") {
		t.Errorf("error = %q, want the redacted token placeholder", err.Error())
	}
}

func TestNewSenderDoesNotLogProxyCredentials(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// A control character makes url.Parse fail, which is the only branch that
	// logs the proxy at all. url.Error quotes the whole input back, so the
	// error itself has to be redacted as well as the URL.
	NewSender(models.NotifyChannel{ID: 3, ProxyURL: "http://bob:s3cr3t@proxy.internal\x7f:8080"})

	out := buf.String()
	if out == "" {
		t.Fatal("expected a log line for an unparseable proxy_url")
	}
	if strings.Contains(out, "s3cr3t") || strings.Contains(out, "bob") {
		t.Errorf("log line leaks the proxy credential: %s", out)
	}
	if !strings.Contains(out, "proxy.internal") {
		t.Errorf("log line = %q, want the proxy host so the channel is identifiable", out)
	}
}

func TestProxyHostDropsCredentials(t *testing.T) {
	cases := map[string]string{
		"http://bob:s3cr3t@proxy.internal:8080":  "proxy.internal:8080",
		"socks5://user:pw@10.0.0.1:1080/":        "10.0.0.1:1080",
		"http://proxy.internal:8080":             "proxy.internal:8080",
		"proxy.internal:8080":                    "proxy.internal:8080",
		"http://bob:s3cr3t@proxy.internal:8080/": "proxy.internal:8080",
	}
	for raw, want := range cases {
		got := proxyHost(raw)
		if got != want {
			t.Errorf("proxyHost(%q) = %q, want %q", raw, got, want)
		}
		if strings.Contains(got, "s3cr3t") || strings.Contains(got, "pw") {
			t.Errorf("proxyHost(%q) = %q, leaks the proxy credential", raw, got)
		}
	}
}
