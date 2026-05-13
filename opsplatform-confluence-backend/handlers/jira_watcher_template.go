package handlers

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"opsplatform-confluence-backend/models"
)

const DefaultNotifyTemplate = `**单号:** {{.IssueKey}}
**标题:** {{.Summary}}
**流转:** {{.FromStatus}} → {{.ToStatus}}
**经办:** {{.Assignee}}    报告人: {{.Reporter}}
**优先级:** {{.Priority}}
**时间:** {{.UpdatedAt}}`

// TemplateVars 模板可用变量
type TemplateVars struct {
	IssueKey   string
	Summary    string
	FromStatus string
	ToStatus   string
	Assignee   string
	Reporter   string
	Priority   string
	URL        string
	Labels     string
	UpdatedAt  string
	ProjectKey string
}

// RenderNotifyTemplate 渲染通知模板
func RenderNotifyTemplate(tpl string, vars TemplateVars) (string, error) {
	if strings.TrimSpace(tpl) == "" {
		tpl = DefaultNotifyTemplate
	}
	t, err := template.New("notify").Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("模板解析失败: %v", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("模板渲染失败: %v", err)
	}
	return buf.String(), nil
}

// BuildVarsFromTask 从 SendTask 构造模板变量
func BuildVarsFromTask(task models.SendTask) TemplateVars {
	return TemplateVars{
		IssueKey:   task.IssueKey,
		Summary:    task.IssueSummary,
		FromStatus: task.FromStatus,
		ToStatus:   task.ToStatus,
		Assignee:   task.Assignee,
		Reporter:   task.Reporter,
		Priority:   task.Priority,
		URL:        task.JiraIssueURL,
		Labels:     strings.Join(task.Labels, ", "),
		UpdatedAt:  task.TransitionAt.Format("2006-01-02 15:04:05"),
		ProjectKey: task.ProjectKey,
	}
}

// BuildInteractiveCard 构造飞书 interactive 卡片
// atUsers: 解析后的 open_id 列表，空切片表示不 @ 任何人（Q14=B）
func BuildInteractiveCard(task models.SendTask, atUsers []models.AtUser, body string) map[string]interface{} {
	title := fmt.Sprintf("🚨 %s 进入【%s】", task.IssueKey, task.ToStatus)

	elements := []map[string]interface{}{
		{
			"tag": "div",
			"text": map[string]string{
				"tag":     "lark_md",
				"content": body,
			},
		},
	}

	// 拼 @ 段（atUsers 为空时省略整个 div 块）
	if len(atUsers) > 0 {
		var mentions []string
		for _, u := range atUsers {
			if u.OpenID != "" {
				mentions = append(mentions, fmt.Sprintf("<at id=%s></at>", u.OpenID))
			}
		}
		if len(mentions) > 0 {
			elements = append(elements, map[string]interface{}{
				"tag": "div",
				"text": map[string]string{
					"tag":     "lark_md",
					"content": strings.Join(mentions, " "),
				},
			})
		}
	}

	// 底部按钮
	elements = append(elements, map[string]interface{}{
		"tag": "action",
		"actions": []map[string]interface{}{
			{
				"tag":  "button",
				"text": map[string]string{"tag": "plain_text", "content": "🔗 打开 Jira 单"},
				"url":  task.JiraIssueURL,
				"type": "primary",
			},
		},
	})

	return map[string]interface{}{
		"config": map[string]bool{"wide_screen_mode": true},
		"header": map[string]interface{}{
			"title":    map[string]string{"tag": "plain_text", "content": title},
			"template": "orange",
		},
		"elements": elements,
	}
}
