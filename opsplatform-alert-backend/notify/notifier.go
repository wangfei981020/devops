// Package notify decouples the alert engine from any single chat platform.
// lark.Sender already satisfies Notifier as-is; telegram.Sender implements it too.
package notify

import (
	"fmt"

	"opsplatform-alert-backend/lark"
	"opsplatform-alert-backend/models"
	"opsplatform-alert-backend/telegram"
)

// Notifier is the surface the alert engine needs from a chat platform.
// It is deliberately identical to the method set lark.Sender already exposes,
// so swapping the engine over to this interface requires no behaviour change.
type Notifier interface {
	SendCard(title, content, severity string, atUsers []models.AtUser, atAll bool) (string, error)
	SendText(text string, atUsers []models.AtUser, atAll bool) (string, error)
	TestWebhook() (string, error)
}

// New builds a Notifier for a single channel. NewMulti (fan-out) builds each
// of its children through this same function, so wrapping the result here —
// rather than in each platform sender, or in NewMulti/the routing path — is
// the single choke point that every delivery path (single-channel, routed,
// and fanned-out) passes through exactly once.
func New(ch models.NotifyChannel) (Notifier, error) {
	n, err := newRaw(ch)
	if err != nil {
		return nil, err
	}
	return fenceNormalizingNotifier{inner: n}, nil
}

// newRaw builds the platform-specific Notifier, with no decoration.
func newRaw(ch models.NotifyChannel) (Notifier, error) {
	switch ch.ChannelType {
	case "", "lark":
		if ch.WebhookURL == "" {
			return nil, fmt.Errorf("channel %d (%s): webhook_url is empty", ch.ID, ch.Name)
		}
		return lark.NewSender(ch.ToLarkConfig()), nil
	case "telegram":
		if ch.BotToken == "" || ch.ChatID == "" {
			return nil, fmt.Errorf("channel %d (%s): bot_token and chat_id are required", ch.ID, ch.Name)
		}
		return telegram.NewSender(ch), nil
	default:
		return nil, fmt.Errorf("channel %d (%s): unknown channel_type %q", ch.ID, ch.Name, ch.ChannelType)
	}
}

// fenceNormalizingNotifier wraps a Notifier and normalizes fenced code
// blocks in SendCard's content before it reaches the underlying platform
// sender. See NormalizeFencedBlocks (fence.go) for why: an alert template
// that writes a fence inline (e.g. "```{{.stack}}```") renders to a fence
// whose delimiters share a line with content, which Lark's standard-markdown
// renderer refuses to treat as a code block. Telegram's own fence regex
// tolerates that shape, which is exactly why the defect only ever showed up
// on Lark. Normalizing once here means both platforms always see the same
// canonical, properly-delimited shape, regardless of how the operator wrote
// the template.
//
// Only the card content is touched. The title is never fenced-code content,
// and SendText's plain-text path is a separate message shape this defect
// does not reach — nothing about it changes here.
type fenceNormalizingNotifier struct {
	inner Notifier
}

func (f fenceNormalizingNotifier) SendCard(title, content, severity string, atUsers []models.AtUser, atAll bool) (string, error) {
	return f.inner.SendCard(title, NormalizeFencedBlocks(content), severity, atUsers, atAll)
}

func (f fenceNormalizingNotifier) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	return f.inner.SendText(text, atUsers, atAll)
}

func (f fenceNormalizingNotifier) TestWebhook() (string, error) {
	return f.inner.TestWebhook()
}
