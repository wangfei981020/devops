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
	"time"

	"opsplatform-alert-backend/models"
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
				"content": fmt.Sprintf("告警时间: %s", time.Now().Format("2006-01-02 15:04:05")),
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
	}

	card := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title": map[string]interface{}{
					"tag":     "plain_text",
					"content": fmt.Sprintf("%s %s", titlePrefix, title),
				},
				"template": headerTemplate,
			},
			"elements": elements,
		},
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

// SendText sends a simple text message
func (s *Sender) SendText(text string, atUsers []models.AtUser, atAll bool) (string, error) {
	// Build at section for text
	atSection := ""
	if atAll {
		atSection = "<at user_id=\"all\">所有人</at>"
	} else {
		for _, u := range atUsers {
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
