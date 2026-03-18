package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"opsplatform-jira-confluence-lock-backend/confluence"
	"opsplatform-jira-confluence-lock-backend/database"
	"opsplatform-jira-confluence-lock-backend/retry"

	"github.com/gorilla/mux"
)

// HandleJiraWebhook 处理 Jira Webhook 事件
func HandleJiraWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondBadRequest(w, "读取请求体失败")
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		respondBadRequest(w, "无效的 JSON 数据")
		return
	}

	// 提取 webhook 事件类型
	webhookEvent, _ := payload["webhookEvent"].(string)
	log.Printf("[Webhook] 收到事件: %s", webhookEvent)

	// 只处理 issue 更新事件（工作流变更）
	if webhookEvent != "jira:issue_updated" {
		respondSuccess(w, map[string]string{"message": "事件已忽略", "event": webhookEvent})
		return
	}

	// 提取 transition 信息
	transitionName := extractTransitionName(payload)
	if transitionName == "" {
		respondSuccess(w, map[string]string{"message": "非工作流变更事件"})
		return
	}

	// 提取 issue 信息
	issue, _ := payload["issue"].(map[string]interface{})
	if issue == nil {
		respondBadRequest(w, "缺少 issue 信息")
		return
	}

	issueKey := getString(issue, "key")
	fields, _ := issue["fields"].(map[string]interface{})
	issueSummary := ""
	if fields != nil {
		issueSummary, _ = fields["summary"].(string)
	}

	// 提取项目 key
	projectKey := ""
	if project, ok := fields["project"].(map[string]interface{}); ok {
		projectKey, _ = project["key"].(string)
	}

	log.Printf("[Webhook] Issue: %s, Transition: %s, Project: %s", issueKey, transitionName, projectKey)

	// 匹配触发规则
	action, ruleName, matched := MatchTriggerRule(projectKey, transitionName)
	if !matched {
		// 记录为 skipped
		database.DB.Exec(`
			INSERT INTO webhook_events (jira_issue_key, jira_issue_summary, webhook_event, transition_name, action, status, raw_payload)
			VALUES (?, ?, ?, ?, '', 'skipped', ?)
		`, issueKey, issueSummary, webhookEvent, transitionName, string(body))

		respondSuccess(w, map[string]string{"message": "未匹配任何触发规则", "transition": transitionName})
		return
	}

	log.Printf("[Webhook] 匹配规则: %s, 动作: %s", ruleName, action)

	// 从 Jira 字段提取 Confluence 页面 URL
	confluenceURL := extractConfluenceURL(fields)
	pageID := ""
	if confluenceURL != "" {
		pageID = extractPageID(confluenceURL)
	}

	if pageID == "" {
		// 记录失败
		database.DB.Exec(`
			INSERT INTO webhook_events (jira_issue_key, jira_issue_summary, confluence_page_url, webhook_event, transition_name, action, status, error_message, raw_payload)
			VALUES (?, ?, ?, ?, ?, ?, 'failed', '无法从 Jira 单中提取 Confluence 页面 ID', ?)
		`, issueKey, issueSummary, confluenceURL, webhookEvent, transitionName, action, string(body))

		respondError(w, http.StatusBadRequest, "无法从 Jira 单中提取 Confluence 页面 ID")
		return
	}

	// 插入事件记录
	result, err := database.DB.Exec(`
		INSERT INTO webhook_events (jira_issue_key, jira_issue_summary, confluence_page_id, confluence_page_url, webhook_event, transition_name, action, status, raw_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'processing', ?)
	`, issueKey, issueSummary, pageID, confluenceURL, webhookEvent, transitionName, action, string(body))
	if err != nil {
		respondInternalError(w, "记录事件失败")
		return
	}

	eventID, _ := result.LastInsertId()

	// 先返回 202 Accepted，异步处理
	respondJSON(w, http.StatusAccepted, apiResponse{
		Code:    0,
		Message: "事件已接收，正在处理",
		Data: map[string]interface{}{
			"event_id":  eventID,
			"action":    action,
			"page_id":   pageID,
			"issue_key": issueKey,
		},
	})

	// 异步执行锁定/解锁，带重试
	go executeAction(eventID, action, pageID, issueKey)
}

// executeAction 异步执行锁定/解锁操作
func executeAction(eventID int64, action, pageID, issueKey string) {
	cfg := retry.DefaultConfig()
	operation := fmt.Sprintf("页面 %s %s (Issue: %s)", pageID, action, issueKey)

	retryCount, err := retry.Do(cfg, operation, func() error {
		client, err := confluence.NewClient()
		if err != nil {
			return fmt.Errorf("创建 Confluence 客户端失败: %v", err)
		}

		switch action {
		case "lock":
			users, groups := GetActivePermissions()
			if len(users) == 0 && len(groups) == 0 {
				return fmt.Errorf("未配置任何权限规则，无法锁定页面")
			}
			return client.LockPage(pageID, users, groups)
		case "unlock":
			return client.UnlockPage(pageID)
		default:
			return fmt.Errorf("未知动作: %s", action)
		}
	})

	if err != nil {
		log.Printf("[Webhook] %s 最终失败: %v", operation, err)
		database.DB.Exec(`
			UPDATE webhook_events SET status='failed', retry_count=?, error_message=?, updated_at=NOW() WHERE id=?
		`, retryCount, err.Error(), eventID)
	} else {
		log.Printf("[Webhook] %s 成功", operation)
		database.DB.Exec(`
			UPDATE webhook_events SET status='success', retry_count=?, updated_at=NOW() WHERE id=?
		`, retryCount, eventID)
	}
}

// HandleRetryEvent 手动重试失败事件
func HandleRetryEvent(w http.ResponseWriter, r *http.Request) {
	idStr := mux.Vars(r)["id"]

	var action, pageID, issueKey, status string
	err := database.DB.QueryRow(`
		SELECT action, confluence_page_id, jira_issue_key, status FROM webhook_events WHERE id=?
	`, idStr).Scan(&action, &pageID, &issueKey, &status)
	if err != nil {
		respondBadRequest(w, "事件不存在")
		return
	}

	if status == "processing" {
		respondBadRequest(w, "事件正在处理中")
		return
	}

	// 重置状态
	var eventID int64
	fmt.Sscanf(idStr, "%d", &eventID)
	database.DB.Exec(`UPDATE webhook_events SET status='processing', retry_count=0, error_message='', updated_at=NOW() WHERE id=?`, eventID)

	go executeAction(eventID, action, pageID, issueKey)

	respondSuccess(w, map[string]string{"message": "已开始重试"})
}

// extractTransitionName 从 webhook payload 提取工作流变更名称
func extractTransitionName(payload map[string]interface{}) string {
	changelog, ok := payload["changelog"].(map[string]interface{})
	if !ok {
		return ""
	}

	items, ok := changelog["items"].([]interface{})
	if !ok {
		return ""
	}

	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		field, _ := itemMap["field"].(string)
		if field == "status" {
			toString, _ := itemMap["toString"].(string)
			return toString
		}
	}
	return ""
}

// extractConfluenceURL 从 Jira issue fields 提取 Confluence URL
// 支持从 description 和所有自定义字段中查找
func extractConfluenceURL(fields map[string]interface{}) string {
	if fields == nil {
		return ""
	}

	// 先从 description 字段查找
	if desc, ok := fields["description"].(string); ok {
		if u := findConfluenceURL(desc); u != "" {
			return u
		}
	}

	// 遍历所有自定义字段查找
	for key, val := range fields {
		if !strings.HasPrefix(key, "customfield_") {
			continue
		}
		switch v := val.(type) {
		case string:
			if u := findConfluenceURL(v); u != "" {
				return u
			}
		case map[string]interface{}:
			// 某些自定义字段是对象，尝试提取 value 或其他文本
			for _, sv := range v {
				if s, ok := sv.(string); ok {
					if u := findConfluenceURL(s); u != "" {
						return u
					}
				}
			}
		}
	}

	return ""
}

// findConfluenceURL 从文本中提取 Confluence 页面 URL
var confluenceURLPattern = regexp.MustCompile(`https?://[^\s<>"]+/pages/viewpage\.action\?pageId=\d+`)

func findConfluenceURL(text string) string {
	return confluenceURLPattern.FindString(text)
}

// extractPageID 从 Confluence URL 提取 pageId 参数
func extractPageID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("pageId")
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
