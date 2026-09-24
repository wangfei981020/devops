package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
)

// ============================================================================
// Lark（飞书）机器人通知
//
// 实现与发布控制台 opsplatform-deploy-backend/services/lark_notify.go 保持一致，
// 便于两边行为统一：同样的卡片结构、同样的签名算法、同样的重试策略。
// ============================================================================

// SendLarkCardWithRetry 包装 SendLarkCard，失败重试 3 次（1s/2s 指数退避）。
//
//	告警通知都走这个，避免临时网抖把通知吞了。
//	测试发送（页面上的「发送测试」按钮）不要重试，直接用 SendLarkCard。
func SendLarkCardWithRetry(ctx context.Context, webhook, secret, title, body, color, linkLabel, linkURL string, atLarkIDs ...string) error {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := SendLarkCard(attemptCtx, webhook, secret, title, body, color, linkLabel, linkURL, atLarkIDs...)
		cancel()
		if err == nil {
			if attempt > 1 {
				log.Printf("[lark] 第 %d 次尝试发送成功", attempt)
			}
			return nil
		}
		lastErr = err
		log.Printf("[lark] 第 %d/3 次发送失败: %v", attempt, err)
		if attempt < 3 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("重试过程中被取消: %w", ctx.Err())
			}
		}
	}
	return fmt.Errorf("重试 3 次仍失败: %w", lastErr)
}

// SendLarkCard 发送飞书 interactive 卡片。
//
//	color: "green" | "red" | "orange" | "blue"
//	linkURL 可空；不空则底部加跳转按钮
//	atLarkIDs 可空；不空则在正文顶部艾特这些人
func SendLarkCard(ctx context.Context, webhook, secret, title, body, color, linkLabel, linkURL string, atLarkIDs ...string) error {
	if webhook == "" {
		return fmt.Errorf("webhook 为空")
	}

	// 把 @ 提及插到正文顶部（飞书 lark_md 支持 <at id="xxx"></at>）
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
		payload["timestamp"] = ts
		payload["sign"] = signLark(ts, secret)
	}

	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", webhook, bytes.NewReader(raw))
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
		return fmt.Errorf("lark webhook 返回 %d", resp.StatusCode)
	}
	return nil
}

func signLark(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	h := hmac.New(sha256.New, []byte(stringToSign))
	h.Write([]byte(""))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
