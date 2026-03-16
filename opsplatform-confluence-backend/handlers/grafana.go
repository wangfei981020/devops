package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// getGrafanaConfig 根据 conn_id 获取 Grafana 连接配置
func getGrafanaConfig(r *http.Request) (baseURL, apiToken string, err error) {
	connID := r.URL.Query().Get("conn_id")
	if connID != "" {
		id, e := strconv.Atoi(connID)
		if e != nil {
			return "", "", fmt.Errorf("无效的 conn_id")
		}
		_, url, _, password, _, e := GetConnectionByID(id)
		if e != nil {
			return "", "", fmt.Errorf("连接不存在")
		}
		return strings.TrimRight(url, "/"), password, nil
	}
	// fallback: 获取默认 grafana 连接
	url, _, password, _, e := GetDefaultConnection("grafana")
	if e != nil {
		return "", "", fmt.Errorf("未配置 Grafana 连接")
	}
	return strings.TrimRight(url, "/"), password, nil
}

// grafanaRequest 向 Grafana 发起 API 请求
func grafanaRequest(baseURL, apiToken, path string) ([]byte, int, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", baseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

// HandleGetGrafanaFolders 获取 Grafana 文件夹列表
func HandleGetGrafanaFolders(w http.ResponseWriter, r *http.Request) {
	baseURL, token, err := getGrafanaConfig(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	data, status, err := grafanaRequest(baseURL, token, "/api/folders?limit=100")
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取 Grafana 文件夹失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("Grafana 返回 %d: %s", status, string(data)))
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleGetGrafanaDashboards 获取指定文件夹下的仪表板列表
func HandleGetGrafanaDashboards(w http.ResponseWriter, r *http.Request) {
	baseURL, token, err := getGrafanaConfig(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	folderID := r.URL.Query().Get("folder_id")
	path := "/api/search?type=dash-db&limit=100"
	if folderID != "" {
		path += "&folderIds=" + folderID
	}

	data, status, err := grafanaRequest(baseURL, token, path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取仪表板列表失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("Grafana 返回 %d: %s", status, string(data)))
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleGetGrafanaDashboard 获取仪表板详情（含面板列表）
func HandleGetGrafanaDashboard(w http.ResponseWriter, r *http.Request) {
	baseURL, token, err := getGrafanaConfig(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	uid := r.URL.Query().Get("uid")
	if uid == "" {
		respondBadRequest(w, "缺少 uid 参数")
		return
	}

	data, status, err := grafanaRequest(baseURL, token, "/api/dashboards/uid/"+uid)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取仪表板详情失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("Grafana 返回 %d: %s", status, string(data)))
		return
	}

	respondSuccess(w, json.RawMessage(data))
}

// HandleGrafanaRenderPanel 代理 Grafana 面板渲染（返回 PNG 图片）
func HandleGrafanaRenderPanel(w http.ResponseWriter, r *http.Request) {
	baseURL, token, err := getGrafanaConfig(r)
	if err != nil {
		respondBadRequest(w, err.Error())
		return
	}

	uid := r.URL.Query().Get("uid")
	panelID := r.URL.Query().Get("panelId")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	width := r.URL.Query().Get("width")
	height := r.URL.Query().Get("height")
	theme := r.URL.Query().Get("theme")

	if uid == "" || panelID == "" {
		respondBadRequest(w, "缺少 uid 或 panelId 参数")
		return
	}
	if from == "" {
		from = "now-24h"
	}
	if to == "" {
		to = "now"
	}
	if width == "" {
		width = "800"
	}
	if height == "" {
		height = "400"
	}
	if theme == "" {
		theme = "dark"
	}

	// 透传所有 var- 参数（Grafana 模板变量）
	vars := ""
	for key, values := range r.URL.Query() {
		if strings.HasPrefix(key, "var-") {
			for _, v := range values {
				vars += "&" + key + "=" + v
			}
		}
	}

	renderPath := fmt.Sprintf("/render/d-solo/%s/panel?panelId=%s&from=%s&to=%s&width=%s&height=%s&theme=%s%s",
		uid, panelID, from, to, width, height, theme, vars)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("GET", baseURL+renderPath, nil)
	if err != nil {
		respondError(w, http.StatusBadGateway, "创建渲染请求失败: "+err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		respondError(w, http.StatusBadGateway, "Grafana 渲染请求失败: "+err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		respondError(w, http.StatusBadGateway, fmt.Sprintf("Grafana 渲染返回 %d: %s", resp.StatusCode, string(body)))
		return
	}

	// 直接返回图片
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-cache")
	io.Copy(w, resp.Body)
}

// HandleTestGrafanaConnection 测试 Grafana 连接
func HandleTestGrafanaConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"url"`
		APIToken string `json:"api_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if req.URL == "" || req.APIToken == "" {
		respondBadRequest(w, "URL 和 API Token 不能为空")
		return
	}

	baseURL := strings.TrimRight(req.URL, "/")
	data, status, err := grafanaRequest(baseURL, req.APIToken, "/api/org")
	if err != nil {
		respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("连接失败，Grafana 返回 %d", status))
		return
	}

	var orgInfo map[string]interface{}
	json.Unmarshal(data, &orgInfo)

	respondSuccess(w, map[string]interface{}{
		"message": "连接成功",
		"org":     orgInfo["name"],
	})
}
