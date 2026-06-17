// Package notify 飞书机器人 webhook 通知。
package notify

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

var client = &http.Client{Timeout: 10 * time.Second}

// SendFeishu 发飞书自定义机器人文本消息。webhook 为空则静默跳过。
func SendFeishu(webhook, text string) error {
	if webhook == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	})
	resp, err := client.Post(webhook, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
