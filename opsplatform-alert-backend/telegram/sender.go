// Package telegram sends alert messages through the Telegram Bot API.
package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"opsplatform-alert-backend/models"
)

const (
	defaultAPIBase = "https://api.telegram.org"
	maxRetries     = 3
	// maxRetryAfter bounds how long a single 429 can park a send. Telegram
	// occasionally reports very large retry_after values; blocking the engine
	// for that long is worse than failing the send.
	maxRetryAfter = 60 * time.Second
)

// backoffDelays is used for 5xx responses and transport errors. Rate limits use
// the server-provided retry_after instead.
var backoffDelays = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

// Sender delivers messages to one Telegram chat.
type Sender struct {
	channel models.NotifyChannel
	client  *http.Client

	// injection points for tests
	apiBase string
	sleep   func(time.Duration)
}

// apiResponse is the envelope every Bot API method returns.
type apiResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func NewSender(ch models.NotifyChannel) *Sender {
	transport := &http.Transport{}
	if ch.ProxyURL != "" {
		if p, err := url.Parse(ch.ProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(p)
		} else {
			// Never log the URL itself: it can carry user:pass@. The host is
			// enough to tell an operator which proxy is misconfigured, and the
			// error is unwrapped first because url.Parse returns a *url.Error
			// that quotes the whole input back — in Go-escaped form, which a
			// plain string redaction would not match.
			log.Printf("[Telegram] channel %d: bad proxy_url (host=%q): %v",
				ch.ID, proxyHost(ch.ProxyURL), parseFailureReason(err, ch.ProxyURL))
		}
	}
	return &Sender{
		channel: ch,
		client:  &http.Client{Timeout: 15 * time.Second, Transport: transport},
		apiBase: defaultAPIBase,
		sleep:   time.Sleep,
	}
}

// parseFailureReason returns why a URL failed to parse, without the URL. A
// *url.Error's own message embeds the input it was given, so only its wrapped
// cause is safe to log; redactSecret is a second line of defence for anything
// else that reaches here.
func parseFailureReason(err error, raw string) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	return redactSecret(err, raw)
}

// proxyHost extracts the host:port from a proxy URL, dropping any credential.
// url.Parse has already failed by the time this is called, so it falls back to
// slicing rather than trusting the parser — but it never returns the userinfo.
func proxyHost(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i != -1 {
		s = s[i+3:]
	}
	if i := strings.LastIndex(s, "@"); i != -1 {
		s = s[i+1:]
	}
	if i := strings.IndexAny(s, "/?#"); i != -1 {
		s = s[:i]
	}
	return s
}

// SendCard renders the alert as Telegram HTML. Telegram has no card concept, so
// severity becomes an emoji prefix instead of a coloured header.
func (s *Sender) SendCard(title, content, severity string, atUsers []models.AtUser, atAll bool) (string, error) {
	return s.sendMessage(BuildMessage(title, content, severity, atUsers, atAll, time.Now()))
}

// SendText sends an unformatted message, escaped so raw log text is safe.
func (s *Sender) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	body := EscapeHTML(text)
	if mentions := buildMentions(atUsers, atAll); mentions != "" {
		body += "\n" + mentions
	}
	return s.sendMessage(TruncateRunes(body, MaxMessageRunes))
}

func (s *Sender) TestWebhook() (string, error) {
	return s.SendCard(
		"Webhook 测试",
		"**状态:** 连接成功 ✅\n\n这是一条测试消息，确认 Telegram 配置正常。",
		"info",
		nil,
		false,
	)
}

func (s *Sender) sendMessage(text string) (string, error) {
	payload := map[string]interface{}{
		"chat_id":                  s.channel.ChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	if s.channel.ThreadID > 0 {
		payload["message_thread_id"] = s.channel.ThreadID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", s.apiBase, s.channel.BotToken)

	var lastResp string
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		respStr, retryAfter, err := s.doSend(endpoint, body)
		if err == nil {
			return respStr, nil
		}
		lastResp, lastErr = respStr, err

		if retryAfter < 0 {
			// Permanent failure (4xx other than 429): retrying cannot help.
			return respStr, err
		}
		if attempt == maxRetries {
			break
		}

		delay := backoffDelays[attempt]
		if retryAfter > 0 {
			delay = time.Duration(retryAfter) * time.Second
			if delay > maxRetryAfter {
				delay = maxRetryAfter
			}
		}
		log.Printf("[Telegram] channel %d: retry %d in %v (%v)", s.channel.ID, attempt+1, delay, err)
		s.sleep(delay)
	}

	return lastResp, fmt.Errorf("telegram send failed after %d retries: %w", maxRetries, lastErr)
}

// doSend performs one request. The returned retryAfter is:
//
//	>0  seconds the server asked us to wait (429)
//	 0  retryable failure, caller picks its own backoff (5xx, transport error)
//	-1  permanent failure, do not retry
func (s *Sender) doSend(endpoint string, body []byte) (string, int, error) {
	resp, err := s.client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		// A transport failure (dial refused, DNS error, timeout, ...) is
		// returned by net/http as a *url.Error, whose Error() embeds the full
		// request URL — which contains the bot token. Redact it here so the
		// leak cannot reach a log line, an alert record, or an API response.
		return "", 0, fmt.Errorf("failed to send telegram message: %w", redactSecret(err, s.channel.BotToken))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read response: %w", err)
	}
	respStr := string(raw)

	var parsed apiResponse
	_ = json.Unmarshal(raw, &parsed)

	if resp.StatusCode == http.StatusOK && parsed.OK {
		return respStr, 0, nil
	}

	apiErr := fmt.Errorf("telegram API error: status=%d code=%d desc=%s",
		resp.StatusCode, parsed.ErrorCode, parsed.Description)

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		retryAfter := parsed.Parameters.RetryAfter
		if retryAfter <= 0 {
			retryAfter = 1
		}
		return respStr, retryAfter, apiErr
	case resp.StatusCode >= 500:
		return respStr, 0, apiErr
	default:
		return respStr, -1, apiErr
	}
}

// redactSecret strips a secret out of an error's message. It exists because
// net/url wraps failures in a *url.Error that includes the full URL — the bot
// token in bot<token>/sendMessage, or a proxy's user:pass@ — so passing such an
// error through unmodified would leak the credential into server logs, the
// notify-channels test endpoint's response, and alert_logs.lark_response.
func redactSecret(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), secret, "****")
	return errors.New(msg)
}
