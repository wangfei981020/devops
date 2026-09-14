package notify

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"opsplatform-alert-backend/models"
)

// stubNotifier records calls and returns a canned outcome.
type stubNotifier struct {
	resp  string
	err   error
	calls int
}

func (s *stubNotifier) SendCard(title, content, severity string, atUsers []models.AtUser, atAll bool) (string, error) {
	s.calls++
	return s.resp, s.err
}
func (s *stubNotifier) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	s.calls++
	return s.resp, s.err
}
func (s *stubNotifier) TestWebhook() (string, error) { s.calls++; return s.resp, s.err }

func newMultiWithStubs(stubs ...*stubNotifier) *MultiSender {
	m := &MultiSender{}
	for i, st := range stubs {
		m.channels = append(m.channels, models.NotifyChannel{
			ID: i + 1, Name: "ch", ChannelType: "lark",
		})
		m.notifiers = append(m.notifiers, st)
	}
	return m
}

func TestMultiSenderFansOutToEveryChannel(t *testing.T) {
	a := &stubNotifier{resp: `{"code":0}`}
	b := &stubNotifier{resp: `{"ok":true}`}
	m := newMultiWithStubs(a, b)

	resp, err := m.SendCard("t", "c", "S2", nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.calls != 1 || b.calls != 1 {
		t.Errorf("calls a=%d b=%d, want 1 each", a.calls, b.calls)
	}

	var results []SendResult
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		t.Fatalf("response is not a SendResult array: %v (%s)", err, resp)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Status != "success" {
			t.Errorf("result %+v, want status success", r)
		}
	}
}

func TestMultiSenderKeepsGoingWhenOneChannelFails(t *testing.T) {
	a := &stubNotifier{err: errors.New("boom")}
	b := &stubNotifier{resp: `{"ok":true}`}
	m := newMultiWithStubs(a, b)

	resp, err := m.SendCard("t", "c", "S2", nil, false)
	if err != nil {
		t.Fatalf("a partial success must not be an error, got %v", err)
	}
	if b.calls != 1 {
		t.Error("the healthy channel must still be attempted")
	}
	if !strings.Contains(resp, "boom") {
		t.Errorf("response %q should record the failure reason", resp)
	}
	if got := StatusFromResponse(resp); got != "partial" {
		t.Errorf("StatusFromResponse() = %q, want partial", got)
	}
}

func TestMultiSenderErrorsWhenEveryChannelFails(t *testing.T) {
	a := &stubNotifier{err: errors.New("boom a")}
	b := &stubNotifier{err: errors.New("boom b")}
	m := newMultiWithStubs(a, b)

	resp, err := m.SendCard("t", "c", "S2", nil, false)
	if err == nil {
		t.Fatal("expected an error when every channel fails")
	}
	if got := StatusFromResponse(resp); got != "failed" {
		t.Errorf("StatusFromResponse() = %q, want failed", got)
	}
}

// panickingNotifier stands in for a notifier bug — a nil map write, an index
// out of range. The cron scheduler runs each job as a bare goroutine, so before
// fanOut recovered, one of these took the whole alert process down.
type panickingNotifier struct{ calls int }

func (p *panickingNotifier) SendCard(title, content, severity string, atUsers []models.AtUser, atAll bool) (string, error) {
	p.calls++
	panic("kaboom")
}
func (p *panickingNotifier) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	p.calls++
	panic("kaboom")
}
func (p *panickingNotifier) TestWebhook() (string, error) { p.calls++; panic("kaboom") }

func TestMultiSenderSurvivesAPanickingChannel(t *testing.T) {
	bad := &panickingNotifier{}
	good := &stubNotifier{resp: `{"ok":true}`}
	m := &MultiSender{
		channels: []models.NotifyChannel{
			{ID: 7, Name: "tg-broken", ChannelType: "telegram"},
			{ID: 8, Name: "lark-ok", ChannelType: "lark"},
		},
		notifiers: []Notifier{bad, good},
	}

	// Reaching the next line at all is the point: an unrecovered panic here
	// would abort the test binary the same way it aborts the alert process.
	resp, err := m.SendCard("t", "c", "S2", nil, false)
	if err != nil {
		t.Fatalf("a partial success must not be an error, got %v", err)
	}
	if bad.calls != 1 {
		t.Errorf("panicking channel calls = %d, want 1", bad.calls)
	}
	if good.calls != 1 {
		t.Error("the healthy channel must still be attempted after the panic")
	}

	var results []SendResult
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		t.Fatalf("response is not a SendResult array: %v (%s)", err, resp)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Status != "failed" || !strings.Contains(results[0].Error, "kaboom") {
		t.Errorf("panic must be recorded as that channel's error, got %+v", results[0])
	}
	if results[1].Status != "success" {
		t.Errorf("healthy channel result = %+v, want success", results[1])
	}
	if got := StatusFromResponse(resp); got != "partial" {
		t.Errorf("StatusFromResponse() = %q, want partial", got)
	}
	if got := FailedChannels(resp); len(got) != 1 || got[0] != "tg-broken" {
		t.Errorf("FailedChannels() = %v, want [tg-broken]", got)
	}
}

func TestStatusFromResponseIgnoresNonMultiPayloads(t *testing.T) {
	if got := StatusFromResponse(`{"code":0,"msg":"ok"}`); got != "" {
		t.Errorf("StatusFromResponse() = %q, want empty for a plain Lark body", got)
	}
	if got := StatusFromResponse(""); got != "" {
		t.Errorf("StatusFromResponse(\"\") = %q, want empty", got)
	}
}

func TestNewMultiRejectsEmptyChannelList(t *testing.T) {
	if _, err := NewMulti(nil); err == nil {
		t.Fatal("expected an error for an empty channel list")
	}
}

func TestNewMultiSkipsBrokenChannelsButKeepsUsableOnes(t *testing.T) {
	chs := []models.NotifyChannel{
		{ID: 1, ChannelType: "lark", WebhookURL: "https://example.com/hook"},
		{ID: 2, ChannelType: "telegram"}, // missing credentials, unusable
	}
	n, err := NewMulti(chs)
	if err != nil {
		t.Fatalf("one broken channel must not sink the whole rule: %v", err)
	}
	m := n.(*MultiSender)
	if len(m.notifiers) != 1 {
		t.Errorf("got %d notifiers, want 1 usable", len(m.notifiers))
	}
}
