package handlers

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"

	"github.com/gorilla/mux"
	"jira-center-backend/jira"
)

// HandleGetProjects 获取 Jira 项目列表
func HandleGetProjects(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/project")
	if err != nil {
		respondInternalError(w, "获取项目列表失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetProject 获取单个项目详情
func HandleGetProject(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/project/" + key)
	if err != nil {
		respondInternalError(w, "获取项目失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleSearchIssues 搜索工单
func HandleSearchIssues(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
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

	params := url.Values{}
	params.Set("jql", jql)
	params.Set("startAt", startAt)
	params.Set("maxResults", maxResults)
	params.Set("fields", fields)
	path := "/rest/api/2/search?" + params.Encode()

	data, err := client.GetRaw(path)
	if err != nil {
		respondInternalError(w, "搜索工单失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetIssue 获取工单详情
func HandleGetIssue(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	expand := r.URL.Query().Get("expand")
	if expand == "" {
		expand = "changelog,renderedFields"
	}

	data, err := client.GetRaw("/rest/api/2/issue/" + key + "?expand=" + expand)
	if err != nil {
		respondInternalError(w, "获取工单失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetIssueComments 获取工单评论
func HandleGetIssueComments(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/issue/" + key + "/comment")
	if err != nil {
		respondInternalError(w, "获取评论失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetTransitions 获取工单可用的状态扭转
func HandleGetTransitions(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/issue/" + key + "/transitions")
	if err != nil {
		respondInternalError(w, "获取状态扭转失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleDoTransition 执行工单状态扭转
func HandleDoTransition(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	var req struct {
		TransitionID string `json:"transition_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if req.TransitionID == "" {
		respondBadRequest(w, "transition_id 不能为空")
		return
	}

	body, _ := json.Marshal(map[string]interface{}{
		"transition": map[string]string{"id": req.TransitionID},
	})

	_, status, err := client.Post("/rest/api/2/issue/"+key+"/transitions", bytes.NewReader(body))
	if err != nil {
		respondInternalError(w, "状态扭转失败: "+err.Error())
		return
	}
	if status != 204 && status != 200 {
		respondError(w, http.StatusBadGateway, "Jira 状态扭转失败 (HTTP "+http.StatusText(status)+")")
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "工单状态扭转", "issue", key, "执行状态扭转 transition_id="+req.TransitionID, GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "状态扭转成功"})
}

// HandleGetPriorities 获取优先级列表
func HandleGetPriorities(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/priority")
	if err != nil {
		respondInternalError(w, "获取优先级失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetStatuses 获取状态列表
func HandleGetStatuses(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/status")
	if err != nil {
		respondInternalError(w, "获取状态失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleSearchUsers 搜索 Jira 用户
func HandleSearchUsers(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		query = "."
	}

	userParams := url.Values{}
	userParams.Set("username", query)
	userParams.Set("maxResults", "50")
	data, err := client.GetRaw("/rest/api/2/user/search?" + userParams.Encode())
	if err != nil {
		respondInternalError(w, "搜索用户失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetProjectFields 获取项目的字段配置
func HandleGetProjectFields(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/field")
	if err != nil {
		respondInternalError(w, "获取字段失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetStats 获取统计数据（增强版：支持日期范围和字段聚合）
func HandleGetStats(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	project := r.URL.Query().Get("project")
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")
	days := r.URL.Query().Get("days")
	groupBy := r.URL.Query().Get("group_by") // status, priority, assignee, issuetype, resolution

	// 构建日期条件
	dateCondition := ""
	if dateFrom != "" && dateTo != "" {
		dateCondition = "created >= \"" + dateFrom + "\" AND created <= \"" + dateTo + " 23:59\""
	} else if days != "" && days != "0" {
		dateCondition = "created >= -" + days + "d"
	} else {
		dateCondition = "created >= -30d"
	}

	jqlPrefix := ""
	if project != "" {
		jqlPrefix = "project = " + project + " AND "
	}

	type statResult struct {
		Label string      `json:"label"`
		JQL   string      `json:"jql"`
		Data  interface{} `json:"data"`
	}

	// 统计字段列表（请求所有字段，包括自定义字段）
	fields := "*all"

	queries := []struct {
		Label string
		JQL   string
	}{
		{"created_trend", jqlPrefix + dateCondition + " ORDER BY created ASC"},
		{"resolved_trend", jqlPrefix + "resolved >= " + resolvedDateCondition(dateFrom, dateTo, days) + " ORDER BY resolved ASC"},
		{"open_issues", jqlPrefix + "resolution = Unresolved ORDER BY priority DESC"},
		{"all_in_range", jqlPrefix + dateCondition + " ORDER BY created DESC"},
	}

	var wg sync.WaitGroup
	results := make([]statResult, len(queries))

	for i, q := range queries {
		wg.Add(1)
		go func(idx int, query struct {
			Label string
			JQL   string
		}) {
			defer wg.Done()
			sp := url.Values{}
			sp.Set("jql", query.JQL)
			sp.Set("maxResults", "1000")
			sp.Set("fields", fields)
			data, err := client.GetRaw("/rest/api/2/search?" + sp.Encode())
			if err != nil {
				log.Printf("[Stats] %s 查询失败: %v", query.Label, err)
				results[idx] = statResult{Label: query.Label, JQL: query.JQL, Data: nil}
				return
			}
			results[idx] = statResult{Label: query.Label, JQL: query.JQL, Data: json.RawMessage(data)}
		}(i, q)
	}
	wg.Wait()

	// 如果指定了 group_by，额外返回聚合数据
	resp := map[string]interface{}{
		"queries":  results,
		"group_by": groupBy,
	}

	respondSuccess(w, resp)
}

func resolvedDateCondition(dateFrom, dateTo, days string) string {
	if dateFrom != "" && dateTo != "" {
		return "\"" + dateFrom + "\" AND resolved <= \"" + dateTo + " 23:59\""
	}
	if days != "" && days != "0" {
		return "-" + days + "d"
	}
	return "-30d"
}

// HandleGetIssueTypes 获取工单类型列表
func HandleGetIssueTypes(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	project := r.URL.Query().Get("project")
	path := "/rest/api/2/issuetype"
	if project != "" {
		path = "/rest/api/2/project/" + project
	}

	data, err := client.GetRaw(path)
	if err != nil {
		respondInternalError(w, "获取工单类型失败: "+err.Error())
		return
	}

	// 如果是项目详情，提取 issueTypes
	if project != "" {
		var proj map[string]json.RawMessage
		if err := json.Unmarshal(data, &proj); err == nil {
			if it, ok := proj["issueTypes"]; ok {
				respondSuccess(w, it)
				return
			}
		}
	}

	respondSuccess(w, data)
}

// HandleGetResolutions 获取解决方案列表
func HandleGetResolutions(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, err := client.GetRaw("/rest/api/2/resolution")
	if err != nil {
		respondInternalError(w, "获取解决方案失败: "+err.Error())
		return
	}

	respondSuccess(w, data)
}

// HandleGetLabels 获取标签列表（Jira Server 不一定有此 API，fallback 到空数组）
func HandleGetLabels(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	// Jira Server 可以通过 JQL gadget 接口获取 labels
	data, _, err := client.Get("/rest/api/1.0/labels/suggest?query=")
	if err != nil || !isJSON(data) {
		respondSuccess(w, []string{})
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

func isJSON(data []byte) bool {
	return len(data) > 0 && (data[0] == '{' || data[0] == '[')
}

// HandleJiraProxy 通用 Jira API 代理 (管理员专用)
func HandleJiraProxy(w http.ResponseWriter, r *http.Request) {
	client, err := jira.NewClient()
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondBadRequest(w, "path 参数必填")
		return
	}

	if r.Method == "GET" {
		data, status, err := client.Get(path)
		if err != nil {
			respondInternalError(w, err.Error())
			return
		}
		if status != 200 && status != 201 {
			respondError(w, status, string(data))
			return
		}
		if isJSON(data) {
			respondSuccess(w, json.RawMessage(data))
		} else {
			respondSuccess(w, string(data))
		}
	} else {
		data, status, err := client.Post(path, r.Body)
		if err != nil {
			respondInternalError(w, err.Error())
			return
		}
		if status != 200 && status != 201 {
			respondError(w, status, string(data))
			return
		}
		if isJSON(data) {
			respondSuccess(w, json.RawMessage(data))
		} else {
			respondSuccess(w, string(data))
		}
	}
}

