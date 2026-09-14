package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"

	"opsplatform-alert-backend/models"
)

// interChannelDelay spaces out sends so a burst across channels does not trip
// per-chat rate limits. Telegram group chats allow roughly 20 messages/minute.
const interChannelDelay = 300 * time.Millisecond

// SendResult is one channel's outcome. A JSON array of these is what gets
// stored in alert_logs.lark_response, so the log detail view can show which
// channel failed rather than a single opaque string.
type SendResult struct {
	ChannelID   int    `json:"channel_id"`
	ChannelType string `json:"channel_type"`
	ChannelName string `json:"channel_name"`
	Status      string `json:"status"` // success / failed
	Response    string `json:"response,omitempty"`
	Error       string `json:"error,omitempty"`
}

// MultiSender fans one alert out to several channels. It satisfies Notifier so
// the engine cannot tell the difference between one channel and five.
type MultiSender struct {
	channels  []models.NotifyChannel
	notifiers []Notifier
}

// NewMulti builds a fan-out notifier. A channel that cannot be constructed
// (bad config) is logged and skipped rather than failing the whole rule —
// one misconfigured channel must not silence the healthy ones.
func NewMulti(channels []models.NotifyChannel) (Notifier, error) {
	m := &MultiSender{}
	for _, ch := range channels {
		n, err := New(ch)
		if err != nil {
			log.Printf("[Notify] skipping channel %d (%s): %v", ch.ID, ch.Name, err)
			continue
		}
		m.channels = append(m.channels, ch)
		m.notifiers = append(m.notifiers, n)
	}
	if len(m.notifiers) == 0 {
		return nil, fmt.Errorf("no usable notification channel among %d configured", len(channels))
	}
	return m, nil
}

func (m *MultiSender) SendCard(title, content, severity string, atUsers []models.AtUser, atAll bool) (string, error) {
	return m.fanOut(func(n Notifier) (string, error) {
		return n.SendCard(title, content, severity, atUsers, atAll)
	})
}

func (m *MultiSender) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	return m.fanOut(func(n Notifier) (string, error) {
		return n.SendText(text, atUsers, atAll)
	})
}

func (m *MultiSender) TestWebhook() (string, error) {
	return m.fanOut(func(n Notifier) (string, error) { return n.TestWebhook() })
}

// fanOut sends sequentially (not in parallel) so the per-channel rate limiting
// inside each sender stays meaningful. It returns an error only when every
// channel failed; a partial success is recorded in the payload instead.
func (m *MultiSender) fanOut(send func(Notifier) (string, error)) (string, error) {
	results := make([]SendResult, 0, len(m.notifiers))
	failed := 0

	for i, n := range m.notifiers {
		if i > 0 {
			time.Sleep(interChannelDelay)
		}
		ch := m.channels[i]
		r := SendResult{
			ChannelID:   ch.ID,
			ChannelType: ch.ChannelType,
			ChannelName: ch.Name,
		}
		resp, err := sendGuarded(send, n, ch)
		if err != nil {
			failed++
			r.Status = "failed"
			r.Error = err.Error()
			r.Response = resp
			log.Printf("[Notify] channel %d (%s) failed: %v", ch.ID, ch.Name, err)
		} else {
			r.Status = "success"
			r.Response = resp
		}
		results = append(results, r)
	}

	payload, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("failed to marshal send results: %w", err)
	}

	if failed == len(m.notifiers) {
		return string(payload), fmt.Errorf("all %d notification channels failed", failed)
	}
	return string(payload), nil
}

// sendGuarded runs one channel's send with a recover in place. Every delivery
// path in the engine goes through here, and the cron scheduler driving it runs
// each job as a bare goroutine — an unrecovered panic in any notifier would
// take the whole alert process down. Turning it into that channel's error keeps
// the remaining channels in the fan-out running, exactly like a send failure.
func sendGuarded(send func(Notifier) (string, error), n Notifier, ch models.NotifyChannel) (resp string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic while sending to channel %d (%s): %v", ch.ID, ch.Name, rec)
			log.Printf("[Notify] channel %d (%s) panicked: %v\n%s", ch.ID, ch.Name, rec, debug.Stack())
		}
	}()
	return send(n)
}

// FailedChannels names the channels that did not deliver in a fan-out payload,
// so a caller can say which half of a partial send is broken. It returns nil
// for anything that is not a SendResult array.
func FailedChannels(resp string) []string {
	if !strings.HasPrefix(strings.TrimSpace(resp), "[") {
		return nil
	}
	var results []SendResult
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		return nil
	}
	var names []string
	for _, r := range results {
		if r.Status == "success" {
			continue
		}
		name := r.ChannelName
		if name == "" {
			name = fmt.Sprintf("#%d", r.ChannelID)
		}
		names = append(names, name)
	}
	return names
}

// StatusFromResponse derives an alert_logs.status value from a fan-out payload.
// It returns "" for anything that is not a SendResult array (e.g. a raw Lark
// response from an older log row), letting the caller keep its own status.
func StatusFromResponse(resp string) string {
	if !strings.HasPrefix(strings.TrimSpace(resp), "[") {
		return ""
	}
	var results []SendResult
	if err := json.Unmarshal([]byte(resp), &results); err != nil || len(results) == 0 {
		return ""
	}
	failed := 0
	for _, r := range results {
		if r.Status != "success" {
			failed++
		}
	}
	switch {
	case failed == 0:
		return "success"
	case failed == len(results):
		return "failed"
	default:
		return "partial"
	}
}
