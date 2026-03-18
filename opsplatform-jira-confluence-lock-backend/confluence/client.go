package confluence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"opsplatform-jira-confluence-lock-backend/database"
)

// Client Confluence Server REST API 客户端
type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
}

// NewClient 从 system_settings 创建客户端
func NewClient() (*Client, error) {
	var url, username, password string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_url'`).Scan(&url)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_username'`).Scan(&username)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_password'`).Scan(&password)

	if url == "" {
		return nil, fmt.Errorf("Confluence URL 未配置")
	}

	return &Client{
		BaseURL:  url,
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// NewClientFromParams 从参数创建客户端（用于测试连接）
func NewClientFromParams(url, username, password string) *Client {
	return &Client{
		BaseURL:  url,
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

// TestConnection 测试 Confluence 连接
func (c *Client) TestConnection() (map[string]interface{}, error) {
	data, status, err := c.Get("/rest/api/user/current")
	if err != nil {
		return nil, fmt.Errorf("无法连接到 Confluence: %v", err)
	}
	if status == 401 || status == 403 {
		return nil, fmt.Errorf("认证失败 (HTTP %d)，请检查用户名/密码", status)
	}
	if status != 200 {
		return nil, fmt.Errorf("Confluence API 错误 (HTTP %d): %s", status, string(data))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetPageInfo 获取页面基本信息
func (c *Client) GetPageInfo(pageID string) (map[string]interface{}, error) {
	data, status, err := c.Get(fmt.Sprintf("/rest/api/content/%s?expand=space,version", pageID))
	if err != nil {
		return nil, fmt.Errorf("获取页面信息失败: %v", err)
	}
	if status != 200 {
		return nil, fmt.Errorf("获取页面失败 (HTTP %d): %s", status, string(data))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// LockPage 锁定页面编辑权限 - 只允许指定用户和组编辑
func (c *Client) LockPage(pageID string, users []string, groups []string) error {
	// Confluence Server 7.x: PUT /rest/api/content/{id}/restriction
	// 设置 update 操作的限制
	type UserRestriction struct {
		Type     string `json:"type"`
		Username string `json:"username"`
	}
	type GroupRestriction struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	type Restrictions struct {
		User  struct {
			Results []UserRestriction `json:"results"`
			Size    int               `json:"size"`
		} `json:"user"`
		Group struct {
			Results []GroupRestriction `json:"results"`
			Size    int                `json:"size"`
		} `json:"group"`
	}
	type OperationRestriction struct {
		Operation    string       `json:"operation"`
		Restrictions Restrictions `json:"restrictions"`
	}

	var userResults []UserRestriction
	for _, u := range users {
		userResults = append(userResults, UserRestriction{Type: "known", Username: u})
	}
	var groupResults []GroupRestriction
	for _, g := range groups {
		groupResults = append(groupResults, GroupRestriction{Type: "group", Name: g})
	}

	payload := []OperationRestriction{
		{
			Operation: "update",
			Restrictions: Restrictions{
				User: struct {
					Results []UserRestriction `json:"results"`
					Size    int               `json:"size"`
				}{Results: userResults, Size: len(userResults)},
				Group: struct {
					Results []GroupRestriction `json:"results"`
					Size    int                `json:"size"`
				}{Results: groupResults, Size: len(groupResults)},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	resp, err := c.Do("PUT", fmt.Sprintf("/rest/experimental/content/%s/restriction", pageID), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("调用 Confluence API 失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("设置页面限制失败 (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// UnlockPage 解锁页面 - 移除所有编辑限制，恢复默认权限
func (c *Client) UnlockPage(pageID string) error {
	// 删除 update 操作的所有限制
	resp, err := c.Do("DELETE", fmt.Sprintf("/rest/experimental/content/%s/restriction/byOperation/update", pageID), nil)
	if err != nil {
		return fmt.Errorf("调用 Confluence API 失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	// 204 No Content 或 200 都算成功
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return fmt.Errorf("移除页面限制失败 (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
