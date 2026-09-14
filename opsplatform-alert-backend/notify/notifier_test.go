package notify

import (
	"testing"

	"opsplatform-alert-backend/models"
)

func TestNewReturnsLarkSenderForLarkChannel(t *testing.T) {
	n, err := New(models.NotifyChannel{ID: 1, ChannelType: "lark", WebhookURL: "https://example.com/hook"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n == nil {
		t.Fatal("expected a notifier, got nil")
	}
}

func TestNewDefaultsEmptyTypeToLark(t *testing.T) {
	n, err := New(models.NotifyChannel{ID: 1, ChannelType: "", WebhookURL: "https://example.com/hook"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n == nil {
		t.Fatal("expected a notifier, got nil")
	}
}

func TestNewRejectsUnknownType(t *testing.T) {
	if _, err := New(models.NotifyChannel{ID: 1, ChannelType: "carrier-pigeon"}); err == nil {
		t.Fatal("expected an error for unknown channel type")
	}
}

func TestNewRejectsTelegramWithoutCredentials(t *testing.T) {
	if _, err := New(models.NotifyChannel{ID: 1, ChannelType: "telegram", BotToken: "", ChatID: ""}); err == nil {
		t.Fatal("expected an error when bot_token/chat_id are missing")
	}
}

// TestFenceNormalizingNotifierNormalizesSendCardContent proves the decorator
// applied inside New rewrites SendCard's content before the underlying
// notifier ever sees it — the actual fix for the Lark rendering defect, as
// opposed to just the pure function it relies on being correct in isolation.
func TestFenceNormalizingNotifierNormalizesSendCardContent(t *testing.T) {
	stub := &recordingNotifier{}
	n := fenceNormalizingNotifier{inner: stub}

	if _, err := n.SendCard("title", "```{{stack}}``` inline", "S1", nil, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "```\n{{stack}}\n``` inline"
	if stub.lastContent != want {
		t.Errorf("inner notifier got content %q, want %q", stub.lastContent, want)
	}
	// The title must never be touched by fence normalization.
	if stub.lastTitle != "title" {
		t.Errorf("inner notifier got title %q, want unchanged %q", stub.lastTitle, "title")
	}
}

// TestFenceNormalizingNotifierLeavesSendTextAlone proves SendText's
// plain-text path is untouched: the decorator only rewrites SendCard's
// content argument.
func TestFenceNormalizingNotifierLeavesSendTextAlone(t *testing.T) {
	stub := &recordingNotifier{}
	n := fenceNormalizingNotifier{inner: stub}

	raw := "```inline fence``` should pass through untouched"
	if _, err := n.SendText(raw, nil, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.lastText != raw {
		t.Errorf("inner notifier got text %q, want unchanged %q", stub.lastText, raw)
	}
}

// recordingNotifier is a minimal Notifier stub that records the arguments it
// was called with, so a decorator test can assert on what actually reached
// the wrapped notifier.
type recordingNotifier struct {
	lastTitle   string
	lastContent string
	lastText    string
}

func (r *recordingNotifier) SendCard(title, content, severity string, atUsers []models.AtUser, atAll bool) (string, error) {
	r.lastTitle = title
	r.lastContent = content
	return "", nil
}

func (r *recordingNotifier) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	r.lastText = text
	return "", nil
}

func (r *recordingNotifier) TestWebhook() (string, error) { return "", nil }
