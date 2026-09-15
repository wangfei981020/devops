package lark

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"opsplatform-alert-backend/models"
	"opsplatform-alert-backend/timezone"
)

// Sender sends messages to Lark/Feishu
type Sender struct {
	config models.LarkConfig
}

func NewSender(config models.LarkConfig) *Sender {
	return &Sender{config: config}
}

// genSign generates signature for signed webhook
func (s *Sender) genSign(timestamp int64) (string, error) {
	if s.config.Secret == "" {
		return "", nil
	}
	strToSign := fmt.Sprintf("%d\n%s", timestamp, s.config.Secret)
	h := hmac.New(sha256.New, []byte(strToSign))
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// SendCard sends an interactive card message
func (s *Sender) SendCard(title, content string, severity string, atUsers []models.AtUser, atAll bool) (string, error) {
	build := func(body string) map[string]interface{} {
		return s.buildCard(title, body, severity, atUsers, atAll)
	}

	card := build(content)
	if body, err := json.Marshal(card); err == nil && len(body) > maxCardBytes {
		// Say so in the log: the reader of a truncated card can see the marker,
		// but the operator asking "why is the stack cut" needs the numbers, and
		// a rule whose cards are cut every time wants its max_alerts lowered.
		fitted := truncateCardContent(build, content)
		card = build(fitted)
		log.Printf("[Lark] card over %d bytes (%d), content truncated from %d to %d runes",
			maxCardBytes, len(body), len([]rune(content)), len([]rune(fitted)))
	}

	// Add signature if secret is set
	if s.config.Secret != "" {
		ts := time.Now().Unix()
		sign, err := s.genSign(ts)
		if err != nil {
			return "", fmt.Errorf("failed to generate sign: %w", err)
		}
		card["timestamp"] = fmt.Sprintf("%d", ts)
		card["sign"] = sign
	}

	return s.send(card)
}

// buildCard assembles the card payload for one body of content. It is a
// separate function because the size check has to marshal a candidate card,
// not just the content: what Lark measures is the whole JSON body, and the
// header, mentions and footer all count towards it.
func (s *Sender) buildCard(title, content string, severity string, atUsers []models.AtUser, atAll bool) map[string]interface{} {
	// Build card elements
	elements := []interface{}{}

	// Main content markdown
	elements = append(elements, map[string]interface{}{
		"tag":     "markdown",
		"content": content,
	})

	// Add @mentions section
	if atAll || len(atUsers) > 0 {
		atContent := ""
		if atAll {
			atContent = "<at id=all>所有人</at>"
		} else {
			for _, u := range atUsers {
				if u.UserID == "" {
					continue
				}
				atContent += fmt.Sprintf("<at id=%s>%s</at> ", u.UserID, u.Name)
			}
		}
		elements = append(elements, map[string]interface{}{
			"tag":     "markdown",
			"content": atContent,
		})
	}

	// Divider
	elements = append(elements, map[string]interface{}{
		"tag": "hr",
	})

	// Footer with time
	elements = append(elements, map[string]interface{}{
		"tag": "note",
		"elements": []interface{}{
			map[string]interface{}{
				"tag":     "plain_text",
				"content": fmt.Sprintf("告警时间: %s", timezone.FormatWithZone(time.Now())),
			},
		},
	})

	// Header color based on severity
	headerTemplate := "red"
	titlePrefix := "🚨🚨🚨"
	switch severity {
	case "S3", "info":
		headerTemplate = "orange"
		titlePrefix = "⚠️"
	case "S2", "warning":
		headerTemplate = "red"
		titlePrefix = "🔴🔴"
	case "S1", "critical":
		headerTemplate = "red"
		titlePrefix = "🚨🚨🚨"
	case "recovery":
		headerTemplate = "green"
		titlePrefix = "✅"
	case "report":
		headerTemplate = "green"
		titlePrefix = ""
	}

	// Title with optional prefix (no leading space when prefix is empty)
	titleContent := title
	if titlePrefix != "" {
		titleContent = titlePrefix + " " + title
	}

	return map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			// width_mode "fill" makes the card span the chat window instead of
			// stopping at the 600px default, which is what a fenced log block
			// needs: Lark does NOT wrap inside a code block, so anything past the
			// card's edge can only be reached by dragging a horizontal scrollbar.
			//
			// NOT wide_screen_mode. That is a legacy field this platform's
			// gke-version, ops-alert and confluence senders all still pass, and
			// it is silently ignored — measured on a real alert, the visible
			// width was 64 characters both before and after setting it. Lark's
			// current card JSON 1.0 reference documents width_mode (default /
			// compact / fill) and does not list wide_screen_mode at all.
			//
			// This widens every card this sender produces, not only the ones
			// carrying log context.
			"config": map[string]interface{}{"width_mode": "fill"},
			"header": map[string]interface{}{
				"title": map[string]interface{}{
					"tag":     "plain_text",
					"content": titleContent,
				},
				"template": headerTemplate,
			},
			"elements": elements,
		},
	}
}

// SendText sends a simple text message
func (s *Sender) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	// Build at section for text
	atSection := ""
	if atAll {
		atSection = "<at user_id=\"all\">所有人</at>"
	} else {
		for _, u := range atUsers {
			if u.UserID == "" {
				continue
			}
			atSection += fmt.Sprintf("<at user_id=\"%s\">%s</at> ", u.UserID, u.Name)
		}
	}

	msg := map[string]interface{}{
		"msg_type": "text",
		"content": map[string]interface{}{
			"text": text + "\n" + atSection,
		},
	}

	if s.config.Secret != "" {
		ts := time.Now().Unix()
		sign, err := s.genSign(ts)
		if err != nil {
			return "", fmt.Errorf("failed to generate sign: %w", err)
		}
		msg["timestamp"] = fmt.Sprintf("%d", ts)
		msg["sign"] = sign
	}

	return s.send(msg)
}

func (s *Sender) send(payload interface{}) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Retry with exponential backoff: 0s, 2s, 4s, 8s
	maxRetries := 3
	retryDelays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

	var lastErr error
	var lastResp string

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[Lark] 重试第%d次 (等待%v)...", attempt, retryDelays[attempt-1])
			time.Sleep(retryDelays[attempt-1])
		}

		respStr, err := s.doSend(body)
		if err == nil {
			return respStr, nil
		}

		lastErr = err
		lastResp = respStr

		// Only retry on rate limit errors
		if !isRateLimitError(err) {
			return respStr, err
		}
		log.Printf("[Lark] 限流错误: %v", err)
	}

	return lastResp, fmt.Errorf("lark发送失败(重试%d次): %w", maxRetries, lastErr)
}

func (s *Sender) doSend(body []byte) (string, error) {
	log.Printf("[Lark] Sending to %s, type=%s", s.config.LarkType, s.config.Name)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(s.config.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to send lark message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	respStr := string(respBody)
	log.Printf("[Lark] Response: %s", respStr)

	// Check response
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return respStr, nil
	}

	// Feishu returns code=0 for success, larksuite returns StatusCode=0
	if code, ok := result["code"].(float64); ok && code != 0 {
		msg := ""
		if m, ok := result["msg"].(string); ok {
			msg = m
		}
		return respStr, fmt.Errorf("lark API error: code=%v msg=%s", code, msg)
	}
	if code, ok := result["StatusCode"].(float64); ok && code != 0 {
		msg := ""
		if m, ok := result["StatusMessage"].(string); ok {
			msg = m
		}
		return respStr, fmt.Errorf("lark API error: StatusCode=%v msg=%s", code, msg)
	}

	return respStr, nil
}

// isRateLimitError checks if the error is a rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "frequency limited") || strings.Contains(errStr, "11232")
}

// TestWebhook sends a test message to verify webhook configuration
func (s *Sender) TestWebhook() (string, error) {
	return s.SendCard(
		"Webhook 测试",
		"**状态:** 连接成功 ✅\n\n这是一条测试消息，确认 Webhook 配置正常。",
		"info",
		nil,
		false,
	)
}
