package jira

import (
	"encoding/json"
	"time"

	"opsplatform-confluence-backend/models"
)

// IssueWithChangelog 用于解码 Jira /rest/api/2/search?expand=changelog 的单条 issue
type IssueWithChangelog struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
		Assignee  *struct{ DisplayName string `json:"displayName"` } `json:"assignee"`
		Reporter  *struct{ DisplayName string `json:"displayName"` } `json:"reporter"`
		Priority  *struct{ Name string `json:"name"` } `json:"priority"`
		Labels    []string `json:"labels"`
		Updated   string   `json:"updated"`
		Project   struct {
			Key string `json:"key"`
		} `json:"project"`
	} `json:"fields"`
	Changelog struct {
		Histories []struct {
			Created string `json:"created"`
			Items   []struct {
				Field      string `json:"field"`
				FromString string `json:"fromString"`
				ToString   string `json:"toString"`
			} `json:"items"`
		} `json:"histories"`
	} `json:"changelog"`
}

// SearchResponse 整体响应
type SearchResponse struct {
	Total  int                  `json:"total"`
	Issues []IssueWithChangelog `json:"issues"`
}

// jiraTimeLayout Jira REST 返回的时间格式 "2006-01-02T15:04:05.000-0700"
const jiraTimeLayout = "2006-01-02T15:04:05.000-0700"

// ParseStatusTransitions 从 changelog 里提取 since 之后的所有 status 流转
func ParseStatusTransitions(issue IssueWithChangelog, since time.Time) []models.StatusTransition {
	var out []models.StatusTransition
	for _, h := range issue.Changelog.Histories {
		at, err := time.Parse(jiraTimeLayout, h.Created)
		if err != nil {
			continue
		}
		if at.Before(since) {
			continue
		}
		for _, item := range h.Items {
			if item.Field != "status" {
				continue
			}
			out = append(out, models.StatusTransition{
				From: item.FromString,
				To:   item.ToString,
				At:   at,
			})
		}
	}
	return out
}

// DecodeSearchResponse 反序列化 Jira search 响应
func DecodeSearchResponse(data []byte) (*SearchResponse, error) {
	var resp SearchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
