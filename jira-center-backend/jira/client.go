package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"jira-center-backend/database"
)

// Client Jira REST API 客户端
type Client struct {
	BaseURL  string
	Username string
	Password string // 密码 或 Personal Access Token
	HTTP     *http.Client
}

// NewClient 从 system_settings 创建 Jira 客户端
func NewClient() (*Client, error) {
	var url, username, password string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_url'`).Scan(&url)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_username'`).Scan(&username)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_password'`).Scan(&password)

	if url == "" {
		return nil, fmt.Errorf("Jira URL 未配置")
	}

	return &Client{
		BaseURL:  url,
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// NewClientFromParams 从参数创建 Jira 客户端（用于测试连接）
func NewClientFromParams(url, username, password string) *Client {
	return &Client{
		BaseURL:  url,
		Username: username,
		Password: password,
		HTTP:     &http.Client{Timeout: 15 * time.Second},
	}
}

// Do 发送认证请求到 Jira
// 认证策略：有用户名则 BasicAuth，无用户名则用 Password 字段作为 Bearer Token (PAT)
func (c *Client) Do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	} else if c.Password != "" {
		// Personal Access Token (PAT) — Bearer 认证
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

// GetRaw 发送 GET 并返回原始 JSON bytes
func (c *Client) GetRaw(path string) (json.RawMessage, error) {
	data, status, err := c.Get(path)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("Jira API 错误 %d: %s", status, string(data))
	}
	return json.RawMessage(data), nil
}

// Post 发送 POST 请求
func (c *Client) Post(path string, body io.Reader) ([]byte, int, error) {
	resp, err := c.Do("POST", path, body)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// TestConnection 测试 Jira 连接
func (c *Client) TestConnection() (map[string]interface{}, error) {
	// 先测试连通性（不带认证）
	resp, err := c.HTTP.Get(c.BaseURL + "/rest/api/2/serverInfo")
	if err != nil {
		return nil, fmt.Errorf("无法连接到 Jira 服务器: %v", err)
	}
	resp.Body.Close()

	// 再测试认证
	data, status, err := c.Get("/rest/api/2/myself")
	if err != nil {
		return nil, fmt.Errorf("连接失败: %v", err)
	}
	if status == 403 {
		return nil, fmt.Errorf("认证被拒绝 (HTTP 403)。可能原因: 1) 用户名或密码/令牌错误 2) Jira 已触发 CAPTCHA 验证，请先在浏览器中登录 Jira (%s) 完成人机验证后重试", c.BaseURL)
	}
	if status == 401 {
		return nil, fmt.Errorf("认证失败 (HTTP 401)。请检查用户名/密码或个人访问令牌是否正确")
	}
	if status != 200 {
		return nil, fmt.Errorf("认证失败 (HTTP %d): %s", status, string(data))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
