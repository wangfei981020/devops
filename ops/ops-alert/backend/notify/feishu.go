package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type feishuConfig struct {
	WebhookURL string `json:"webhook_url"`
	Secret     string `json:"secret,omitempty"`
}

type feishuSender struct {
	cfg    feishuConfig
	client *http.Client
}

func newFeishu(raw []byte) (Sender, error) {
	var c feishuConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("解析飞书配置: %w", err)
	}
	if c.WebhookURL == "" {
		return nil, fmt.Errorf("飞书渠道缺少 webhook_url")
	}
	return &feishuSender{cfg: c, client: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (f *feishuSender) Type() string { return TypeFeishu }

func (f *feishuSender) Caps() Caps {
	return Caps{RichCard: true, Mention: true, Buttons: true}
}

func (f *feishuSender) Send(ctx context.Context, msg Message) error {
	body := map[string]any{
		"msg_type": "interactive",
		"card":     f.card(msg),
	}
	if f.cfg.Secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		body["timestamp"] = ts
		body["sign"] = sign(ts, f.cfg.Secret)
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.cfg.WebhookURL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("投递失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	// ⚠️ 关键：飞书限流时返回的是 HTTP 200 + code != 0。
	// 只看 HTTP 状态码会把限流当成投递成功，于是「已通知」是假的——
	// 而值班的人以为没人叫他就是没事。这类静默失败正是本产品要根治的。
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("响应无法解析，按失败处理: %s", string(raw))
	}
	if out.Code != 0 {
		return fmt.Errorf("飞书返回 code=%d: %s", out.Code, out.Msg)
	}
	return nil
}

func (f *feishuSender) card(msg Message) map[string]any {
	color := map[string]string{"critical": "red", "warning": "orange", "info": "blue"}[msg.Severity]
	if color == "" {
		color = "grey"
	}
	elements := []any{}
	if len(msg.Fields) > 0 {
		var text string
		for _, kv := range msg.Fields {
			text += "**" + kv.Key + "**：" + kv.Value + "\n"
		}
		elements = append(elements, map[string]any{
			"tag": "div", "text": map[string]any{"tag": "lark_md", "content": text},
		})
	}
	if msg.Sample != "" {
		elements = append(elements, map[string]any{
			"tag": "div", "text": map[string]any{"tag": "lark_md", "content": "```\n" + msg.Sample + "\n```"},
		})
	}
	if len(msg.Mentions) > 0 || msg.AtAll {
		at := ""
		if msg.AtAll {
			at = "<at id=all></at>"
		}
		for _, m := range msg.Mentions {
			at += "<at id=" + m + "></at>"
		}
		elements = append(elements, map[string]any{
			"tag": "div", "text": map[string]any{"tag": "lark_md", "content": at},
		})
	}
	if msg.Link != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "查看事件"},
				"url":  msg.Link,
				"type": "primary",
			}},
		})
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": color,
			"title":    map[string]any{"tag": "plain_text", "content": "[" + severityLabel(msg.Severity) + "] " + msg.Title},
		},
		"elements": elements,
	}
}

func sign(ts, secret string) string {
	h := hmac.New(sha256.New, []byte(ts+"\n"+secret))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
