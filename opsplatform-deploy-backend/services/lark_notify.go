package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// SendLarkCard 发送飞书 interactive 卡片
// color: "green"|"red"|"orange"|"blue"
// linkURL 可空；不空则底部加 "查看" 按钮
// atLarkIDs 可空；不空则在 body 顶部艾特这些用户
func SendLarkCard(ctx context.Context, webhook, secret, title, body, color, linkLabel, linkURL string, atLarkIDs ...string) error {
	if webhook == "" {
		return fmt.Errorf("empty webhook")
	}
	// 把 @ 提及插到 body 顶部（飞书 lark_md 支持 <at id="xxx"></at>）
	if len(atLarkIDs) > 0 {
		ats := ""
		for _, id := range atLarkIDs {
			if id != "" {
				ats += fmt.Sprintf(`<at id="%s"></at> `, id)
			}
		}
		if ats != "" {
			body = ats + "\n" + body
		}
	}
	payload := map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"template": color,
				"title":    map[string]interface{}{"tag": "plain_text", "content": title},
			},
			"elements": []interface{}{
				map[string]interface{}{
					"tag":  "div",
					"text": map[string]interface{}{"tag": "lark_md", "content": body},
				},
			},
		},
	}
	if linkURL != "" {
		elems := payload["card"].(map[string]interface{})["elements"].([]interface{})
		elems = append(elems, map[string]interface{}{
			"tag": "action",
			"actions": []interface{}{
				map[string]interface{}{
					"tag":  "button",
					"text": map[string]interface{}{"tag": "plain_text", "content": linkLabel},
					"url":  linkURL,
					"type": "default",
				},
			},
		})
		payload["card"].(map[string]interface{})["elements"] = elems
	}

	if secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := signLark(ts, secret)
		payload["timestamp"] = ts
		payload["sign"] = sig
	}

	body2, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", webhook, bytes.NewReader(body2))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("lark webhook %d", resp.StatusCode)
	}
	return nil
}

func signLark(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	h := hmac.New(sha256.New, []byte(stringToSign))
	h.Write([]byte(""))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
