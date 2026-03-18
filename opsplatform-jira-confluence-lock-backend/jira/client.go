package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"opsplatform-jira-confluence-lock-backend/database"
)

// Client Jira REST API 客户端
type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
}

// NewClient 从 system_settings 创建 Jira 客户端
func NewClient() (*Client, error) {
	var jiraURL, username, password string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_url'`).Scan(&jiraURL)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_username'`).Scan(&username)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_password'`).Scan(&password)

	if jiraURL == "" {
		return nil, fmt.Errorf("Jira URL 未配置")
	}

	return &Client{
		BaseURL:  jiraURL,
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// NewClientFromParams 从参数创建（测试连接用）
func NewClientFromParams(jiraURL, username, password string) *Client {
	return &Client{
		BaseURL:  jiraURL,
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Do 发送认证请求
func (c *Client) Do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	} else if c.Password != "" {
		req.Header.Set("Authorization", "Bearer "+c.Password)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.HTTP.Do(req)
}

// Get 发送 GET 请求
func (c *Client) Get(path string) ([]byte, int, error) {
	resp, err := c.Do("GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// GetJSON 发送 GET 并解析 JSON
func (c *Client) GetJSON(path string, target interface{}) error {
	data, status, err := c.Get(path)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("Jira API 错误 %d: %s", status, string(data))
	}
	return json.Unmarshal(data, target)
}

// TestConnection 测试 Jira 连接
func (c *Client) TestConnection() (map[string]interface{}, error) {
	data, status, err := c.Get("/rest/api/2/myself")
	if err != nil {
		return nil, fmt.Errorf("连接失败: %v", err)
	}
	if status == 401 || status == 403 {
		return nil, fmt.Errorf("认证失败 (HTTP %d)", status)
	}
	if status != 200 {
		return nil, fmt.Errorf("Jira API 错误 (HTTP %d): %s", status, string(data))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// IssueSearchResult Jira 搜索结果
type IssueSearchResult struct {
	Issues []Issue `json:"issues"`
	Total  int     `json:"total"`
}

// Issue Jira Issue
type Issue struct {
	Key    string                 `json:"key"`
	Fields map[string]interface{} `json:"fields"`
}

// SearchIssues 用 JQL 搜索 Issue
func (c *Client) SearchIssues(jql string, maxResults int) ([]Issue, error) {
	encodedJQL := url.QueryEscape(jql)
	path := fmt.Sprintf("/rest/api/2/search?jql=%s&maxResults=%d", encodedJQL, maxResults)

	var result IssueSearchResult
	if err := c.GetJSON(path, &result); err != nil {
		return nil, err
	}
	return result.Issues, nil
}
