package handlers

import (
	"net/http"
	"strconv"

	"opsplatform-jira-confluence-lock-backend/database"
)

type WebhookEvent struct {
	ID               int64  `json:"id"`
	JiraIssueKey     string `json:"jira_issue_key"`
	JiraIssueSummary string `json:"jira_issue_summary"`
	ConfluencePageID string `json:"confluence_page_id"`
	ConfluencePageURL string `json:"confluence_page_url"`
	WebhookEvent     string `json:"webhook_event"`
	TransitionName   string `json:"transition_name"`
	Action           string `json:"action"`
	Status           string `json:"status"`
	RetryCount       int    `json:"retry_count"`
	ErrorMessage     string `json:"error_message"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// HandleGetEvents 查询事件日志
func HandleGetEvents(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	status := r.URL.Query().Get("status")
	issueKey := r.URL.Query().Get("issue_key")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// 构建查询条件
	query := `SELECT id, jira_issue_key, jira_issue_summary, confluence_page_id, COALESCE(confluence_page_url,''), webhook_event, transition_name, action, status, retry_count, COALESCE(error_message,''), created_at, updated_at FROM webhook_events WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM webhook_events WHERE 1=1`
	var args []interface{}

	if status != "" {
		query += ` AND status = ?`
		countQuery += ` AND status = ?`
		args = append(args, status)
	}
	if issueKey != "" {
		query += ` AND jira_issue_key LIKE ?`
		countQuery += ` AND jira_issue_key LIKE ?`
		args = append(args, "%"+issueKey+"%")
	}

	// 总数
	var total int
	database.DB.QueryRow(countQuery, args...).Scan(&total)

	// 分页查询
	query += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	queryArgs := append(args, pageSize, offset)

	rows, err := database.DB.Query(query, queryArgs...)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var events []WebhookEvent
	for rows.Next() {
		var e WebhookEvent
		if err := rows.Scan(&e.ID, &e.JiraIssueKey, &e.JiraIssueSummary, &e.ConfluencePageID, &e.ConfluencePageURL,
			&e.WebhookEvent, &e.TransitionName, &e.Action, &e.Status, &e.RetryCount, &e.ErrorMessage,
			&e.CreatedAt, &e.UpdatedAt); err == nil {
			events = append(events, e)
		}
	}
	if events == nil {
		events = []WebhookEvent{}
	}

	respondSuccess(w, map[string]interface{}{
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"items":     events,
	})
}
