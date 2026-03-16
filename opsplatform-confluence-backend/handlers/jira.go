package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"opsplatform-confluence-backend/database"
	"opsplatform-confluence-backend/jira"
)

// getJiraClientFromRequest 根据请求中的 conn_id 参数获取 Jira 客户端
func getJiraClientFromRequest(r *http.Request) (*jira.Client, error) {
	connID := r.URL.Query().Get("conn_id")
	if connID != "" {
		return NewJiraClientByConnID(connID)
	}
	return jira.NewClient()
}

// HandleGetJiraProjects 获取 Jira 项目列表
func HandleGetJiraProjects(w http.ResponseWriter, r *http.Request) {
	client, err := getJiraClientFromRequest(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/project")
	if err != nil {
		respondInternalError(w, "获取Jira项目失败: "+err.Error())
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleJiraSearch JQL 搜索
func HandleJiraSearch(w http.ResponseWriter, r *http.Request) {
	client, err := getJiraClientFromRequest(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	jql := r.URL.Query().Get("jql")
	startAt := r.URL.Query().Get("startAt")
	maxResults := r.URL.Query().Get("maxResults")
	fields := r.URL.Query().Get("fields")

	if startAt == "" {
		startAt = "0"
	}
	if maxResults == "" {
		maxResults = "50"
	}
	if fields == "" {
		fields = "*all"
	}

	path := fmt.Sprintf("/rest/api/2/search?jql=%s&startAt=%s&maxResults=%s&fields=%s",
		url.QueryEscape(jql), startAt, maxResults, url.QueryEscape(fields))

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "Jira搜索失败: "+err.Error())
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleGetJiraIssue 获取单个 Jira Issue
func HandleGetJiraIssue(w http.ResponseWriter, r *http.Request) {
	client, err := getJiraClientFromRequest(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	// 从URL中获取 issue key
	issueKey := r.URL.Query().Get("key")
	if issueKey == "" {
		respondBadRequest(w, "缺少 issue key")
		return
	}

	path := fmt.Sprintf("/rest/api/2/issue/%s", url.PathEscape(issueKey))
	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取Issue失败: "+err.Error())
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleGetJiraIssueTypes 获取 Jira Issue 类型
func HandleGetJiraIssueTypes(w http.ResponseWriter, r *http.Request) {
	client, err := getJiraClientFromRequest(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	projectKey := r.URL.Query().Get("project")
	var path string
	if projectKey != "" {
		path = fmt.Sprintf("/rest/api/2/project/%s", url.PathEscape(projectKey))
	} else {
		path = "/rest/api/2/issuetype"
	}

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取Issue类型失败: "+err.Error())
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleGetJiraFields 获取 Jira 所有字段定义（用于自定义字段映射）
func HandleGetJiraFields(w http.ResponseWriter, r *http.Request) {
	client, err := getJiraClientFromRequest(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/field")
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取字段定义失败: "+err.Error())
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleTestJiraConnection 测试 Jira 连接
func HandleTestJiraConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"jira_url"`
		Username string `json:"jira_username"`
		Password string `json:"jira_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if req.URL == "" {
		respondBadRequest(w, "Jira URL 不能为空")
		return
	}

	if req.Password == "********" {
		database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_password'`).Scan(&req.Password)
	}

	client := jira.NewClientFromParams(req.URL, req.Username, req.Password)
	userInfo, err := client.TestConnection()
	if err != nil {
		respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
		return
	}

	respondSuccess(w, map[string]interface{}{
		"message":  "连接成功",
		"user":     userInfo["displayName"],
		"username": userInfo["name"],
	})
}

// HandleJiraProxy 通用 Jira API 代理
func HandleJiraProxy(w http.ResponseWriter, r *http.Request) {
	client, err := getJiraClientFromRequest(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	targetPath := r.URL.Query().Get("path")
	if targetPath == "" {
		respondBadRequest(w, "缺少 path 参数")
		return
	}

	var body io.Reader
	if r.Body != nil && r.Method != "GET" {
		body = r.Body
	}

	resp, err := client.Do(r.Method, targetPath, body)
	if err != nil {
		respondError(w, http.StatusBadGateway, "Jira代理请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 && resp.StatusCode != 204 {
		log.Printf("[JiraProxy] %s %s -> %d", r.Method, targetPath, resp.StatusCode)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(data)
}
