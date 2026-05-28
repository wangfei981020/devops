package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LarkCard: 飞书交互卡片
type LarkAlertPayload struct {
	Title          string
	Project        string
	ClusterName    string
	Location       string
	Target         string // "cluster" 或 "nodepool"
	NodepoolName   string
	CurrentVersion string
	LatestVersion  string
	VersionsBehind int
	Threshold      int
	StdEOL         string
	DaysToEOL      *int
	MentionLarkIDs []string
	MentionNames   []string
	DetailURL      string
}

// BuildLarkCard: 拼飞书卡片 JSON
func BuildLarkCard(p LarkAlertPayload) map[string]any {
	mentions := []string{}
	for i, id := range p.MentionLarkIDs {
		name := id
		if i < len(p.MentionNames) {
			name = p.MentionNames[i]
		}
		mentions = append(mentions, fmt.Sprintf(`<at id="%s">%s</at>`, id, name))
	}
	mentionLine := ""
	if len(mentions) > 0 {
		mentionLine = "通知: " + strings.Join(mentions, " ")
	}

	targetLine := fmt.Sprintf("**集群**: %s (%s)", p.ClusterName, p.Location)
	if p.Target == "nodepool" {
		targetLine = fmt.Sprintf("**集群**: %s (%s)\n**节点池**: %s", p.ClusterName, p.Location, p.NodepoolName)
	}

	eolLine := ""
	if p.StdEOL != "" {
		eolLine = fmt.Sprintf("**标准 EOL**: %s", p.StdEOL)
		if p.DaysToEOL != nil {
			emoji := ""
			if *p.DaysToEOL <= 30 {
				emoji = " 🔥"
			} else if *p.DaysToEOL <= 90 {
				emoji = " ⚠️"
			}
			eolLine += fmt.Sprintf("（剩 %d 天%s）", *p.DaysToEOL, emoji)
		}
	}

	contentLines := []string{
		fmt.Sprintf("**项目**: %s", p.Project),
		targetLine,
		fmt.Sprintf("**当前版本**: `%s`", p.CurrentVersion),
		fmt.Sprintf("**最新版本**: `%s`", p.LatestVersion),
		fmt.Sprintf("**落后版本数**: **%d** （阈值 ≥%d）", p.VersionsBehind, p.Threshold),
	}
	if eolLine != "" {
		contentLines = append(contentLines, eolLine)
	}
	if mentionLine != "" {
		contentLines = append(contentLines, "", mentionLine)
	}

	elements := []map[string]any{
		{
			"tag": "div",
			"text": map[string]any{
				"tag":     "lark_md",
				"content": strings.Join(contentLines, "\n"),
			},
		},
	}
	if p.DetailURL != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []map[string]any{
				{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": "查看详情"},
					"type": "primary",
					"url":  p.DetailURL,
				},
			},
		})
	}

	header := map[string]any{
		"title":    map[string]any{"tag": "plain_text", "content": "🔴 " + p.Title},
		"template": "red",
	}

	return map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"config":   map[string]any{"wide_screen_mode": true},
			"header":   header,
			"elements": elements,
		},
	}
}

// SendLarkCard: POST 到 webhook URL，返回响应文本（含 code/msg）。错误时返回 err。
func SendLarkCard(webhookURL string, card map[string]any) (string, error) {
	body, _ := json.Marshal(card)
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("POST", webhookURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return string(rb), fmt.Errorf("lark webhook HTTP %d: %s", resp.StatusCode, string(rb))
	}
	return string(rb), nil
}
