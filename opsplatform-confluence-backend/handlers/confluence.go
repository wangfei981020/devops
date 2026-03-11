package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"opsplatform-confluence-backend/confluence"

	"github.com/gorilla/mux"
)

// getConfluenceClientFromRequest 根据请求中的 conn_id 参数获取 Confluence 客户端
func getConfluenceClientFromRequest(r *http.Request) (*confluence.Client, error) {
	connID := r.URL.Query().Get("conn_id")
	if connID != "" {
		return NewConfluenceClientByConnID(connID)
	}
	return confluence.NewClient()
}

// ===== 空间 (Spaces) =====

// HandleGetSpaces 获取所有空间
func HandleGetSpaces(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	// 支持分页和类型过滤
	start := r.URL.Query().Get("start")
	limit := r.URL.Query().Get("limit")
	spaceType := r.URL.Query().Get("type") // global, personal

	path := "/rest/api/space?expand=description.plain,metadata.labels"
	if start != "" {
		path += "&start=" + start
	}
	if limit != "" {
		path += "&limit=" + limit
	} else {
		path += "&limit=25"
	}
	if spaceType != "" {
		path += "&type=" + spaceType
	}

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取空间列表失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// HandleGetSpace 获取空间详情
func HandleGetSpace(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	key := mux.Vars(r)["key"]
	path := fmt.Sprintf("/rest/api/space/%s?expand=description.plain,metadata.labels,homepage", key)

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取空间详情失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// HandleGetSpaceContent 获取空间下的内容
func HandleGetSpaceContent(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	key := mux.Vars(r)["key"]
	contentType := r.URL.Query().Get("type") // page, blogpost
	start := r.URL.Query().Get("start")
	limit := r.URL.Query().Get("limit")

	path := fmt.Sprintf("/rest/api/space/%s/content", key)
	if contentType != "" {
		path += "/" + contentType
	}
	path += "?expand=version,ancestors"
	if start != "" {
		path += "&start=" + start
	}
	if limit != "" {
		path += "&limit=" + limit
	} else {
		path += "&limit=25"
	}

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取空间内容失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// ===== 内容 (Content / Pages) =====

// HandleGetContent 获取内容详情（页面/博客）
func HandleGetContent(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	id := mux.Vars(r)["id"]
	path := fmt.Sprintf("/rest/api/content/%s?expand=body.storage,version,ancestors,space,children.page,metadata.labels", id)

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取内容详情失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// HandleGetContentChildren 获取子页面
func HandleGetContentChildren(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	id := mux.Vars(r)["id"]
	start := r.URL.Query().Get("start")
	limit := r.URL.Query().Get("limit")

	path := fmt.Sprintf("/rest/api/content/%s/child/page?expand=version", id)
	if start != "" {
		path += "&start=" + start
	}
	if limit != "" {
		path += "&limit=" + limit
	} else {
		path += "&limit=25"
	}

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取子页面失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// HandleCreateContent 创建内容（页面/博客）
func HandleCreateContent(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	data, status, err := client.Post("/rest/api/content", r.Body)
	if err != nil {
		respondInternalError(w, "创建内容失败: "+err.Error())
		return
	}
	if status != 200 && status != 201 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("创建内容失败 (HTTP %d): %s", status, string(data)))
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "创建Confluence内容", "content", "", "创建页面/博客", GetClientIP(r))

	respondSuccess(w, json.RawMessage(data))
}

// HandleUpdateContent 更新内容
func HandleUpdateContent(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	id := mux.Vars(r)["id"]
	data, status, err := client.Put(fmt.Sprintf("/rest/api/content/%s", id), r.Body)
	if err != nil {
		respondInternalError(w, "更新内容失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("更新内容失败 (HTTP %d): %s", status, string(data)))
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "更新Confluence内容", "content", id, "更新页面/博客", GetClientIP(r))

	respondSuccess(w, json.RawMessage(data))
}

// HandleDeleteContent 删除内容
func HandleDeleteContent(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	id := mux.Vars(r)["id"]
	_, status, err := client.Delete(fmt.Sprintf("/rest/api/content/%s", id))
	if err != nil {
		respondInternalError(w, "删除内容失败: "+err.Error())
		return
	}
	if status != 204 && status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("删除内容失败 (HTTP %d)", status))
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "删除Confluence内容", "content", id, "删除页面/博客", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "内容已删除"})
}

// ===== 搜索 =====

// HandleSearch 搜索内容 (CQL)
func HandleSearch(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	cql := r.URL.Query().Get("cql")
	keyword := r.URL.Query().Get("keyword")
	start := r.URL.Query().Get("start")
	limit := r.URL.Query().Get("limit")

	// 如果提供了 keyword 而非 cql，自动构建 CQL
	if cql == "" && keyword != "" {
		cql = fmt.Sprintf(`type=page AND text~"%s"`, keyword)
	}
	if cql == "" {
		respondBadRequest(w, "请提供搜索条件 (cql 或 keyword)")
		return
	}

	path := "/rest/api/search?cql=" + url.QueryEscape(cql) + "&expand=content.space,content.version"
	if start != "" {
		path += "&start=" + start
	}
	if limit != "" {
		path += "&limit=" + limit
	} else {
		path += "&limit=25"
	}

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "搜索失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// ===== 标签 (Labels) =====

// HandleGetContentLabels 获取内容标签
func HandleGetContentLabels(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	id := mux.Vars(r)["id"]
	data, err := client.GetRaw(fmt.Sprintf("/rest/api/content/%s/label", id))
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取标签失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// HandleAddContentLabel 添加内容标签
func HandleAddContentLabel(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	id := mux.Vars(r)["id"]
	data, status, err := client.Post(fmt.Sprintf("/rest/api/content/%s/label", id), r.Body)
	if err != nil {
		respondInternalError(w, "添加标签失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("添加标签失败 (HTTP %d): %s", status, string(data)))
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// ===== 评论 (Comments) =====

// HandleGetContentComments 获取内容评论
func HandleGetContentComments(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	id := mux.Vars(r)["id"]
	start := r.URL.Query().Get("start")
	limit := r.URL.Query().Get("limit")

	path := fmt.Sprintf("/rest/api/content/%s/child/comment?expand=body.storage,version", id)
	if start != "" {
		path += "&start=" + start
	}
	if limit != "" {
		path += "&limit=" + limit
	} else {
		path += "&limit=25"
	}

	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取评论失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// ===== 用户 =====

// HandleGetConfluenceUsers 搜索 Confluence 用户
func HandleGetConfluenceUsers(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		respondBadRequest(w, "请提供搜索关键词")
		return
	}

	path := fmt.Sprintf("/rest/api/search/user?cql=user.fullname~\"%s\"&limit=20", query)
	data, err := client.GetRaw(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "搜索用户失败: "+err.Error())
		return
	}
	respondSuccess(w, json.RawMessage(data))
}

// ===== 通用代理 =====

// HandleConfluenceProxy 通用 Confluence API 代理（管理员）
func HandleConfluenceProxy(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		respondBadRequest(w, "请提供 API 路径")
		return
	}
	if !strings.HasPrefix(path, "/rest/") {
		path = "/rest/api/" + strings.TrimPrefix(path, "/")
	}

	var data []byte
	var status int

	switch r.Method {
	case "GET":
		data, status, err = client.Get(path)
	case "POST":
		data, status, err = client.Post(path, r.Body)
	case "PUT":
		data, status, err = client.Put(path, r.Body)
	case "DELETE":
		data, status, err = client.Delete(path)
	default:
		respondBadRequest(w, "不支持的方法")
		return
	}

	if err != nil {
		respondInternalError(w, "代理请求失败: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}

// ===== 仪表盘 =====

// HandleGetDashboardData 获取 Confluence 仪表盘数据
func HandleGetDashboardData(w http.ResponseWriter, r *http.Request) {
	client, err := getConfluenceClientFromRequest(r)
	if err != nil {
		respondInternalError(w, err.Error())
		return
	}

	dashboard := make(map[string]interface{})

	// 空间数量
	var spacesResult struct {
		Size int `json:"size"`
	}
	if err := client.GetJSON("/rest/api/space?limit=0", &spacesResult); err == nil {
		dashboard["total_spaces"] = spacesResult.Size
	}

	// 最近更新的内容
	recentData, err := client.GetRaw("/rest/api/content?orderby=lastmodified%20desc&limit=10&expand=space,version")
	if err == nil {
		dashboard["recent_content"] = json.RawMessage(recentData)
	}

	// 当前用户信息
	userInfo, err := client.GetRaw("/rest/api/user/current")
	if err == nil {
		dashboard["current_user"] = json.RawMessage(userInfo)
	}

	respondSuccess(w, dashboard)
}
