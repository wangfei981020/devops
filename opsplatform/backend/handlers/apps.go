package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"opsplatform/database"

	"github.com/gorilla/mux"
)

type ExternalApp struct {
	ID        string `json:"id"`
	AppKey    string `json:"app_key"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	IconSVG   string `json:"icon_svg"`
	GroupName string `json:"group_name"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// HandleGetExternalApps 获取外部应用列表
func HandleGetExternalApps(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, app_key, name, url, COALESCE(icon_svg,''), COALESCE(group_name,''), sort_order, status, created_at, updated_at FROM external_apps ORDER BY group_name, sort_order`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	apps := []ExternalApp{}
	for rows.Next() {
		var a ExternalApp
		rows.Scan(&a.ID, &a.AppKey, &a.Name, &a.URL, &a.IconSVG, &a.GroupName, &a.SortOrder, &a.Status, &a.CreatedAt, &a.UpdatedAt)
		apps = append(apps, a)
	}
	respondSuccess(w, apps)
}

// HandleGetMyExternalApps 获取当前用户有权限的外部应用
func HandleGetMyExternalApps(w http.ResponseWriter, r *http.Request) {
	userIDCtx, username, role := GetUserFromContext(r)
	log.Printf("[外部应用调试] username=%s, role=%s, userIDCtx=%s", username, role, userIDCtx)

	// 管理员看到所有启用的应用
	if role == "admin" || role == "super_admin" || role == "超级管理员" {
		rows, err := database.DB.Query(`SELECT id, app_key, name, url, COALESCE(icon_svg,''), COALESCE(group_name,''), sort_order FROM external_apps WHERE status = 'active' ORDER BY group_name, sort_order`)
		if err != nil {
			log.Printf("[外部应用调试] 管理员查询失败: %v", err)
			respondInternalError(w, "查询失败")
			return
		}
		defer rows.Close()
		apps := []ExternalApp{}
		for rows.Next() {
			var a ExternalApp
			rows.Scan(&a.ID, &a.AppKey, &a.Name, &a.URL, &a.IconSVG, &a.GroupName, &a.SortOrder)
			apps = append(apps, a)
		}
		log.Printf("[外部应用调试] 管理员返回 %d 个应用", len(apps))
		respondSuccess(w, apps)
		return
	}

	// 非管理员：查角色关联
	var userID string
	err := database.DB.QueryRow(`SELECT id FROM users WHERE username = ?`, username).Scan(&userID)
	if err != nil {
		log.Printf("[外部应用调试] 查询用户ID失败: username=%s, err=%v", username, err)
	}
	if userID == "" {
		log.Printf("[外部应用调试] 用户ID为空，返回空列表: username=%s", username)
		respondSuccess(w, []ExternalApp{})
		return
	}
	log.Printf("[外部应用调试] 用户ID: %s", userID)

	rows, err := database.DB.Query(`
		SELECT DISTINCT a.id, a.app_key, a.name, a.url, COALESCE(a.icon_svg,''), COALESCE(a.group_name,''), a.sort_order
		FROM external_apps a
		INNER JOIN role_external_apps ra ON ra.app_key = a.app_key
		INNER JOIN user_roles ur ON ur.role_id = ra.role_id
		WHERE ur.user_id = ? AND a.status = 'active'
		ORDER BY COALESCE(a.group_name,''), a.sort_order
	`, userID)
	if err != nil {
		log.Printf("[外部应用调试] 角色关联查询失败: userID=%s, err=%v", userID, err)
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	apps := []ExternalApp{}
	for rows.Next() {
		var a ExternalApp
		rows.Scan(&a.ID, &a.AppKey, &a.Name, &a.URL, &a.IconSVG, &a.GroupName, &a.SortOrder)
		apps = append(apps, a)
	}
	log.Printf("[外部应用调试] 非管理员 %s 返回 %d 个应用", username, len(apps))
	respondSuccess(w, apps)
}

// HandleCreateExternalApp 创建外部应用
func HandleCreateExternalApp(w http.ResponseWriter, r *http.Request) {
	var app ExternalApp
	if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if app.AppKey == "" || app.Name == "" || app.URL == "" {
		respondBadRequest(w, "app_key、name、url 必填")
		return
	}

	app.ID = "app_" + time.Now().Format("20060102150405")
	if app.Status == "" {
		app.Status = "active"
	}

	_, err := database.DB.Exec(`INSERT INTO external_apps (id, app_key, name, url, icon_svg, group_name, sort_order, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		app.ID, app.AppKey, app.Name, app.URL, app.IconSVG, app.GroupName, app.SortOrder, app.Status)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			respondBadRequest(w, "app_key 已存在")
			return
		}
		respondInternalError(w, "创建失败")
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("创建外部应用: "+app.Name, app.ID, username, "", "", "", GetClientIP(r))

	respondCreated(w, app)
}

// HandleUpdateExternalApp 更新外部应用
func HandleUpdateExternalApp(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var app ExternalApp
	if err := json.NewDecoder(r.Body).Decode(&app); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	_, err := database.DB.Exec(`UPDATE external_apps SET name=?, url=?, icon_svg=?, group_name=?, sort_order=?, status=? WHERE id=?`,
		app.Name, app.URL, app.IconSVG, app.GroupName, app.SortOrder, app.Status, id)
	if err != nil {
		respondInternalError(w, "更新失败")
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("更新外部应用: "+app.Name, id, username, "", "", "", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "更新成功"})
}

// HandleDeleteExternalApp 删除外部应用
func HandleDeleteExternalApp(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var appName string
	database.DB.QueryRow(`SELECT name FROM external_apps WHERE id = ?`, id).Scan(&appName)

	database.DB.Exec(`DELETE FROM role_external_apps WHERE app_key = (SELECT app_key FROM external_apps WHERE id = ?)`, id)
	database.DB.Exec(`DELETE FROM external_apps WHERE id = ?`, id)

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("删除外部应用: "+appName, id, username, "", "", "", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "删除成功"})
}

// HandleGetAppRoles 获取应用的角色权限
func HandleGetAppRoles(w http.ResponseWriter, r *http.Request) {
	appKey := mux.Vars(r)["appKey"]

	rows, err := database.DB.Query(`SELECT role_id FROM role_external_apps WHERE app_key = ?`, appKey)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	roleIDs := []string{}
	for rows.Next() {
		var rid string
		rows.Scan(&rid)
		roleIDs = append(roleIDs, rid)
	}
	respondSuccess(w, roleIDs)
}

// HandleUpdateAppRoles 更新应用的角色权限
func HandleUpdateAppRoles(w http.ResponseWriter, r *http.Request) {
	appKey := mux.Vars(r)["appKey"]

	var body struct {
		RoleIDs []string `json:"role_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	// 先删后插
	database.DB.Exec(`DELETE FROM role_external_apps WHERE app_key = ?`, appKey)
	for _, rid := range body.RoleIDs {
		id := "rea_" + rid + "_" + appKey
		database.DB.Exec(`INSERT IGNORE INTO role_external_apps (id, role_id, app_key) VALUES (?, ?, ?)`, id, rid, appKey)
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("更新应用权限: "+appKey, appKey, username, "", "", "", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "更新成功"})
}

// HandleTestAppURL 测试外部应用 URL 连通性
func HandleTestAppURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		respondBadRequest(w, "请提供 url")
		return
	}

	if !strings.HasPrefix(body.URL, "http://") && !strings.HasPrefix(body.URL, "https://") {
		respondBadRequest(w, "URL 必须以 http:// 或 https:// 开头")
		return
	}

	// Pod 内 localhost 指向自身，替换为宿主机地址进行测试
	testURL := body.URL
	testURL = strings.Replace(testURL, "://localhost:", "://host.docker.internal:", 1)
	testURL = strings.Replace(testURL, "://localhost/", "://host.docker.internal/", 1)
	if strings.HasSuffix(testURL, "://localhost") {
		testURL = strings.Replace(testURL, "://localhost", "://host.docker.internal", 1)
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(testURL)
	if err != nil {
		respondSuccess(w, map[string]interface{}{
			"reachable": false,
			"message":   "连接失败: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	respondSuccess(w, map[string]interface{}{
		"reachable":   true,
		"status_code": resp.StatusCode,
		"message":     "连接成功，HTTP " + http.StatusText(resp.StatusCode),
	})
}

// HandleGetAppGroups 获取所有分组名称
func HandleGetAppGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT DISTINCT group_name FROM external_apps WHERE group_name != '' ORDER BY group_name`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	groups := []string{}
	for rows.Next() {
		var g string
		rows.Scan(&g)
		groups = append(groups, g)
	}
	respondSuccess(w, groups)
}
