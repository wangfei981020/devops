package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type webhookConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// 签名密钥。给对端验签用：X-OpsAlert-Timestamp + X-OpsAlert-Signature。
	// 没有签名的 webhook 等于任何人都能往对方系统里灌告警。
	Secret string `json:"secret,omitempty"`
}

type webhookSender struct {
	cfg    webhookConfig
	client *http.Client
}

func newWebhook(raw []byte) (Sender, error) {
	var c webhookConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("解析 webhook 配置: %w", err)
	}
	if c.URL == "" {
		return nil, fmt.Errorf("webhook 渠道缺少 url")
	}
	if c.Method == "" {
		c.Method = http.MethodPost
	}
	return &webhookSender{cfg: c, client: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (w *webhookSender) Type() string { return TypeWebhook }

// Caps：webhook 收的是结构化 JSON，不存在渲染降级问题。
func (w *webhookSender) Caps() Caps { return Caps{} }

func (w *webhookSender) Send(ctx context.Context, msg Message) error {
	fields := map[string]string{}
	for _, f := range msg.Fields {
		fields[f.Key] = f.Value
	}
	payload := map[string]any{
		"title":    msg.Title,
		"severity": msg.Severity,
		"fields":   fields,
		"sample":   msg.Sample,
		"link":     msg.Link,
	}
	buf, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, w.cfg.Method, w.cfg.URL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.cfg.Headers {
		req.Header.Set(k, v)
	}
	if w.cfg.Secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(w.cfg.Secret))
		mac.Write([]byte(ts))
		mac.Write(buf)
		req.Header.Set("X-OpsAlert-Timestamp", ts)
		req.Header.Set("X-OpsAlert-Signature", hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("投递失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
