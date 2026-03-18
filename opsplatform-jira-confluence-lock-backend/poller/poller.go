package poller

import (
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"opsplatform-jira-confluence-lock-backend/confluence"
	"opsplatform-jira-confluence-lock-backend/database"
	"opsplatform-jira-confluence-lock-backend/jira"
	"opsplatform-jira-confluence-lock-backend/retry"
)

// Start 启动轮询器
func Start() {
	go func() {
		// 等服务完全启动
		time.Sleep(10 * time.Second)
		log.Println("[轮询器] 启动")

		for {
			interval := getPollingInterval()
			if interval > 0 {
				poll()
			} else {
				log.Println("[轮询器] 轮询已禁用 (间隔=0)")
			}

			if interval < 60 {
				interval = 300 // 最小 5 分钟
			}
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}()
}

// getPollingInterval 从 system_settings 获取轮询间隔（秒），0=禁用
func getPollingInterval() int {
	var val string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'poll_interval'`).Scan(&val)
	if val == "" {
		return 300 // 默认 5 分钟
	}
	n, _ := strconv.Atoi(val)
	return n
}

// poll 执行一次轮询
func poll() {
	// 1. 获取所有启用的触发规则
	rules, err := getEnabledTriggerRules()
	if err != nil || len(rules) == 0 {
		return
	}

	// 2. 创建 Jira 客户端
	jiraClient, err := jira.NewClient()
	if err != nil {
		log.Printf("[轮询器] 创建 Jira 客户端失败: %v", err)
		return
	}

	// 3. 按规则查询 Jira
	for _, rule := range rules {
		pollRule(jiraClient, rule)
	}
}

type triggerRule struct {
	ID             int64
	RuleName       string
	Action         string
	ProjectKey     string
	TransitionName string
}

func getEnabledTriggerRules() ([]triggerRule, error) {
	rows, err := database.DB.Query(`SELECT id, rule_name, action, project_key, transition_name FROM trigger_rules WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []triggerRule
	for rows.Next() {
		var r triggerRule
		if err := rows.Scan(&r.ID, &r.RuleName, &r.Action, &r.ProjectKey, &r.TransitionName); err == nil {
			rules = append(rules, r)
		}
	}
	return rules, nil
}

// pollRule 针对单条规则轮询 Jira
func pollRule(jiraClient *jira.Client, rule triggerRule) {
	// 构建 JQL: 查找指定项目中当前状态匹配的工单
	// 只查最近 30 天的，避免扫描太多历史数据
	jql := fmt.Sprintf(`status = "%s" AND updatedDate >= -30d`, rule.TransitionName)
	if rule.ProjectKey != "" {
		jql = fmt.Sprintf(`project = "%s" AND %s`, rule.ProjectKey, jql)
	}
	jql += " ORDER BY updated DESC"

	issues, err := jiraClient.SearchIssues(jql, 50)
	if err != nil {
		log.Printf("[轮询器] 查询 Jira 失败 (规则: %s): %v", rule.RuleName, err)
		return
	}

	if len(issues) == 0 {
		return
	}

	log.Printf("[轮询器] 规则 [%s] 查到 %d 个工单", rule.RuleName, len(issues))

	for _, issue := range issues {
		processIssue(issue, rule)
	}
}

// processIssue 处理单个 Jira 工单
func processIssue(issue jira.Issue, rule triggerRule) {
	issueKey := issue.Key
	fields := issue.Fields

	// 提取摘要
	summary := ""
	if s, ok := fields["summary"].(string); ok {
		summary = s
	}

	// 检查是否已处理过（同一个 issue + 同一个 action + 成功状态）
	var count int
	database.DB.QueryRow(`
		SELECT COUNT(*) FROM webhook_events
		WHERE jira_issue_key = ? AND action = ? AND status = 'success'
	`, issueKey, rule.Action).Scan(&count)
	if count > 0 {
		return // 已成功处理过，跳过
	}

	// 检查是否正在处理中
	database.DB.QueryRow(`
		SELECT COUNT(*) FROM webhook_events
		WHERE jira_issue_key = ? AND action = ? AND status = 'processing'
	`, issueKey, rule.Action).Scan(&count)
	if count > 0 {
		return
	}

	// 提取 Confluence URL
	confluenceURL := extractConfluenceURL(fields)
	if confluenceURL == "" {
		// 没有 Confluence URL，兼容跳过，不记录错误
		return
	}

	pageID := extractPageID(confluenceURL)
	if pageID == "" {
		return
	}

	log.Printf("[轮询器] 发现待处理工单: %s, 动作: %s, 页面: %s", issueKey, rule.Action, pageID)

	// 插入事件记录
	result, err := database.DB.Exec(`
		INSERT INTO webhook_events (jira_issue_key, jira_issue_summary, confluence_page_id, confluence_page_url, webhook_event, transition_name, action, status)
		VALUES (?, ?, ?, ?, 'poller', ?, ?, 'processing')
	`, issueKey, summary, pageID, confluenceURL, rule.TransitionName, rule.Action)
	if err != nil {
		log.Printf("[轮询器] 插入事件记录失败: %v", err)
		return
	}

	eventID, _ := result.LastInsertId()

	// 执行锁定/解锁（带重试）
	go executeAction(eventID, rule.Action, pageID, issueKey)
}

// executeAction 执行锁定/解锁操作
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
			users, groups := getActivePermissions()
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
		log.Printf("[轮询器] %s 最终失败: %v", operation, err)
		database.DB.Exec(`
			UPDATE webhook_events SET status='failed', retry_count=?, error_message=?, updated_at=NOW() WHERE id=?
		`, retryCount, err.Error(), eventID)
	} else {
		log.Printf("[轮询器] %s 成功", operation)
		database.DB.Exec(`
			UPDATE webhook_events SET status='success', retry_count=?, updated_at=NOW() WHERE id=?
		`, retryCount, eventID)
	}
}

// getActivePermissions 获取启用的权限规则
func getActivePermissions() ([]string, []string) {
	rows, err := database.DB.Query(`SELECT rule_type, rule_value FROM permission_rules WHERE enabled = 1`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	var users, groups []string
	for rows.Next() {
		var ruleType, ruleValue string
		if err := rows.Scan(&ruleType, &ruleValue); err == nil {
			if ruleType == "user" {
				users = append(users, ruleValue)
			} else if ruleType == "group" {
				groups = append(groups, ruleValue)
			}
		}
	}
	return users, groups
}

// extractConfluenceURL 从 Jira fields 提取 Confluence URL
var confluenceURLPattern = regexp.MustCompile(`https?://[^\s<>"]+/pages/viewpage\.action\?pageId=\d+`)

func extractConfluenceURL(fields map[string]interface{}) string {
	if fields == nil {
		return ""
	}

	// 从 description 查找
	if desc, ok := fields["description"].(string); ok {
		if u := confluenceURLPattern.FindString(desc); u != "" {
			return u
		}
	}

	// 遍历所有自定义字段
	for key, val := range fields {
		if !strings.HasPrefix(key, "customfield_") {
			continue
		}
		switch v := val.(type) {
		case string:
			if u := confluenceURLPattern.FindString(v); u != "" {
				return u
			}
		case map[string]interface{}:
			for _, sv := range v {
				if s, ok := sv.(string); ok {
					if u := confluenceURLPattern.FindString(s); u != "" {
						return u
					}
				}
			}
		}
	}
	return ""
}

// extractPageID 从 URL 提取 pageId
func extractPageID(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("pageId")
}
