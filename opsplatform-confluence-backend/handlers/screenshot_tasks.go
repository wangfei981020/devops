package handlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"opsplatform-confluence-backend/database"

	"github.com/gorilla/mux"
	"github.com/robfig/cron/v3"
)

// ScreenshotTask 截图任务
type ScreenshotTask struct {
	ID            int             `json:"id"`
	Name          string          `json:"name"`
	GrafanaConnID int             `json:"grafana_conn_id"`
	Dashboards    json.RawMessage `json:"dashboards"`
	Variables     json.RawMessage `json:"variables"`
	LarkConnIDs   json.RawMessage `json:"lark_conn_ids"`
	CronExpr      string          `json:"cron_expr"`
	TimeRange     string          `json:"time_range"`
	Width         int             `json:"width"`
	Height        int             `json:"height"`
	Theme         string          `json:"theme"`
	Enabled       bool            `json:"enabled"`
	LastRunAt     *string         `json:"last_run_at"`
	LastStatus    string          `json:"last_status"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

// TaskDashboard 任务中的 Dashboard 配置
type TaskDashboard struct {
	UID    string `json:"uid"`
	Title  string `json:"title"`
	Panels []int  `json:"panels"` // 为空则截全部
}

// ---- Cron 管理 ----

var (
	cronScheduler *cron.Cron
	cronEntries   map[int]cron.EntryID // taskID -> cronEntryID
	cronMu        sync.Mutex

	// 临时截图缓存（用于飞书发图）
	screenshotCache   = make(map[string]screenshotCacheItem)
	screenshotCacheMu sync.RWMutex
)

type screenshotCacheItem struct {
	Data      []byte
	ExpiresAt time.Time
}

// SaveScreenshotToCache 保存截图到缓存，返回访问 key
func SaveScreenshotToCache(data []byte) string {
	key := fmt.Sprintf("%d", time.Now().UnixNano())
	screenshotCacheMu.Lock()
	screenshotCache[key] = screenshotCacheItem{
		Data:      data,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	screenshotCacheMu.Unlock()
	return key
}

// HandleGetScreenshotImage 公开接口，返回截图图片
func HandleGetScreenshotImage(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	screenshotCacheMu.RLock()
	item, ok := screenshotCache[key]
	screenshotCacheMu.RUnlock()

	if !ok || time.Now().After(item.ExpiresAt) {
		if ok {
			screenshotCacheMu.Lock()
			delete(screenshotCache, key)
			screenshotCacheMu.Unlock()
		}
		http.Error(w, "图片不存在或已过期", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(item.Data)
}

// CleanExpiredScreenshots 清理过期缓存
func CleanExpiredScreenshots() {
	screenshotCacheMu.Lock()
	defer screenshotCacheMu.Unlock()
	now := time.Now()
	for k, v := range screenshotCache {
		if now.After(v.ExpiresAt) {
			delete(screenshotCache, k)
		}
	}
}

// InitScreenshotCron 启动截图定时任务
func InitScreenshotCron() {
	cronScheduler = cron.New()
	cronEntries = make(map[int]cron.EntryID)
	cronScheduler.Start()

	// 加载所有启用的任务
	rows, err := database.DB.Query(`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme FROM grafana_screenshot_tasks WHERE enabled = 1`)
	if err != nil {
		log.Printf("加载截图任务失败: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var task ScreenshotTask
		var dashboards, variables, larkConnIDs string
		if err := rows.Scan(&task.ID, &task.Name, &task.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &task.CronExpr, &task.TimeRange, &task.Width, &task.Height, &task.Theme); err != nil {
			continue
		}
		task.Dashboards = json.RawMessage(dashboards)
		task.Variables = json.RawMessage(variables)
		task.LarkConnIDs = json.RawMessage(larkConnIDs)

		registerCronTask(task)
		count++
	}
	log.Printf("截图定时任务已加载: %d 个", count)
}

// StopScreenshotCron 停止定时任务
func StopScreenshotCron() {
	if cronScheduler != nil {
		cronScheduler.Stop()
	}
}

func registerCronTask(task ScreenshotTask) {
	cronMu.Lock()
	defer cronMu.Unlock()

	// 先移除旧的
	if oldID, exists := cronEntries[task.ID]; exists {
		cronScheduler.Remove(oldID)
		delete(cronEntries, task.ID)
	}

	taskCopy := task
	entryID, err := cronScheduler.AddFunc(taskCopy.CronExpr, func() {
		executeScreenshotTask(taskCopy)
	})
	if err != nil {
		log.Printf("注册截图任务 [%s] 失败: %v", task.Name, err)
		return
	}
	cronEntries[task.ID] = entryID
	log.Printf("注册截图任务 [%s] cron=%s", task.Name, task.CronExpr)
}

func unregisterCronTask(taskID int) {
	cronMu.Lock()
	defer cronMu.Unlock()

	if entryID, exists := cronEntries[taskID]; exists {
		cronScheduler.Remove(entryID)
		delete(cronEntries, taskID)
	}
}

// executeScreenshotTask 执行截图任务
func executeScreenshotTask(task ScreenshotTask) {
	log.Printf("开始执行截图任务: [%s]", task.Name)

	// 解析 dashboards
	var dashboards []TaskDashboard
	if err := json.Unmarshal(task.Dashboards, &dashboards); err != nil {
		updateTaskStatus(task.ID, "error: "+err.Error())
		return
	}

	// 解析 variables
	var variables map[string]string
	json.Unmarshal(task.Variables, &variables)

	// 解析 lark conn ids
	var larkConnIDs []int
	json.Unmarshal(task.LarkConnIDs, &larkConnIDs)

	// 获取 Grafana 配置
	_, grafanaURL, _, grafanaToken, _, err := GetConnectionByID(task.GrafanaConnID)
	if err != nil {
		updateTaskStatus(task.ID, "error: Grafana连接不存在")
		return
	}
	grafanaURL = strings.TrimRight(grafanaURL, "/")

	// 逐个 Dashboard 截图
	var results []DashboardScreenshot
	for _, dash := range dashboards {
		result := screenshotDashboard(grafanaURL, grafanaToken, dash, variables, task.TimeRange, task.Width, task.Height, task.Theme)
		results = append(results, result)
	}

	// 发送到飞书
	larkURLs := getLarkWebhookURLsByIDs(larkConnIDs)
	for _, webhookURL := range larkURLs {
		if err := SendLarkScreenshotResult(webhookURL, task.Name, results, task.Width, task.Height); err != nil {
			log.Printf("发送飞书消息失败 [%s]: %v", task.Name, err)
		}
	}

	updateTaskStatus(task.ID, "success")
	log.Printf("截图任务完成: [%s]", task.Name)
}

func screenshotDashboard(baseURL, token string, dash TaskDashboard, variables map[string]string, timeRange string, width, height int, theme string) DashboardScreenshot {
	result := DashboardScreenshot{
		UID:   dash.UID,
		Title: dash.Title,
	}

	var from, to string
	if strings.HasPrefix(timeRange, "custom:") {
		// 自定义时间范围，格式: custom:HH:MM-HH:MM
		parts := strings.SplitN(strings.TrimPrefix(timeRange, "custom:"), "-", 2)
		if len(parts) == 2 {
			loc, _ := time.LoadLocation("Asia/Shanghai")
			if loc == nil {
				loc = time.UTC
			}
			now := time.Now().In(loc)
			fromTime, err1 := time.Parse("15:04", strings.TrimSpace(parts[0]))
			toTime, err2 := time.Parse("15:04", strings.TrimSpace(parts[1]))
			if err1 == nil && err2 == nil {
				fromAbs := time.Date(now.Year(), now.Month(), now.Day(), fromTime.Hour(), fromTime.Minute(), 0, 0, loc)
				toAbs := time.Date(now.Year(), now.Month(), now.Day(), toTime.Hour(), toTime.Minute(), 0, 0, loc)
				// 如果 to 早于 from，说明跨天，to 加一天
				if toAbs.Before(fromAbs) {
					toAbs = toAbs.Add(24 * time.Hour)
				}
				from = fmt.Sprintf("%d", fromAbs.UnixMilli())
				to = fmt.Sprintf("%d", toAbs.UnixMilli())
			} else {
				from = "now-1h"
				to = "now"
			}
		} else {
			from = "now-1h"
			to = "now"
		}
	} else {
		// 相对时间也转为绝对时间（epoch毫秒），让截图显示具体时间
		loc, _ := time.LoadLocation("Asia/Shanghai")
		if loc == nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		var duration time.Duration
		switch timeRange {
		case "30m":
			duration = 30 * time.Minute
		case "1h":
			duration = 1 * time.Hour
		case "3h":
			duration = 3 * time.Hour
		case "6h":
			duration = 6 * time.Hour
		case "12h":
			duration = 12 * time.Hour
		case "24h":
			duration = 24 * time.Hour
		default:
			duration = 1 * time.Hour
		}
		fromAbs := now.Add(-duration)
		from = fmt.Sprintf("%d", fromAbs.UnixMilli())
		to = fmt.Sprintf("%d", now.UnixMilli())
	}

	// 构建 var- 参数
	vars := ""
	for k, v := range variables {
		vars += "&var-" + k + "=" + v
	}

	// 如果指定了面板列表，按面板截图；否则整页截图
	if len(dash.Panels) > 0 {
		// 按面板截图（并发，限制 3）
		type panelResult struct {
			Index int
			Image PanelImage
			OK    bool
		}
		ch := make(chan panelResult, len(dash.Panels))
		sem := make(chan struct{}, 3)
		for i, panelID := range dash.Panels {
			go func(idx, pid int) {
				sem <- struct{}{}
				defer func() { <-sem }()
				renderPath := fmt.Sprintf("/render/d-solo/%s/panel?panelId=%d&from=%s&to=%s&width=%d&height=%d&theme=%s&tz=Asia%%2FShanghai%s",
					dash.UID, pid, from, to, width, height, theme, vars)

				client := &http.Client{Timeout: 120 * time.Second}
				req, err := http.NewRequest("GET", baseURL+renderPath, nil)
				if err != nil {
					ch <- panelResult{Index: idx}
					return
				}
				req.Header.Set("Authorization", "Bearer "+token)

				resp, err := client.Do(req)
				if err != nil {
					ch <- panelResult{Index: idx}
					return
				}
				imgData, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				if resp.StatusCode == 200 {
					ch <- panelResult{Index: idx, Image: PanelImage{PanelID: pid, Title: fmt.Sprintf("Panel %d", pid), Data: imgData}, OK: true}
				} else {
					ch <- panelResult{Index: idx}
				}
			}(i, panelID)
		}
		results := make([]panelResult, len(dash.Panels))
		for range dash.Panels {
			r := <-ch
			results[r.Index] = r
		}
		for _, r := range results {
			if r.OK {
				result.PanelImages = append(result.PanelImages, r.Image)
			}
		}
	} else {
		// 整个 Dashboard 一张长截图
		// 先获取面板数量来估算高度（每行约 250px，2 面板一行）
		fullHeight := height
		data, status, err := grafanaRequest(baseURL, token, "/api/dashboards/uid/"+dash.UID)
		if err != nil {
			log.Printf("获取Dashboard面板信息失败: %v (baseURL=%s)", err, baseURL)
		} else if status != 200 {
			log.Printf("获取Dashboard面板信息HTTP %d (baseURL=%s, uid=%s)", status, baseURL, dash.UID)
		}
		if err == nil && status == 200 {
			var dashData struct {
				Dashboard struct {
					Panels []json.RawMessage `json:"panels"`
				} `json:"dashboard"`
			}
			if json.Unmarshal(data, &dashData) == nil && len(dashData.Dashboard.Panels) > 0 {
				// 遍历所有面板（包括 row 内嵌套的面板）计算最大高度
				maxBottom := 0
				for _, raw := range dashData.Dashboard.Panels {
					var p struct {
						GridPos struct {
							H int `json:"h"`
							Y int `json:"y"`
						} `json:"gridPos"`
						Type   string            `json:"type"`
						Panels []json.RawMessage `json:"panels"`
					}
					json.Unmarshal(raw, &p)
					bottom := p.GridPos.Y + p.GridPos.H
					if bottom > maxBottom {
						maxBottom = bottom
					}
					// row 类型可能有嵌套面板
					for _, nestedRaw := range p.Panels {
						var np struct {
							GridPos struct {
								H int `json:"h"`
								Y int `json:"y"`
							} `json:"gridPos"`
						}
						json.Unmarshal(nestedRaw, &np)
						nb := np.GridPos.Y + np.GridPos.H
						if nb > maxBottom {
							maxBottom = nb
						}
					}
				}
				// 使用用户设置的高度，不自动扩大
				_ = maxBottom
			}
		}
		log.Printf("Dashboard %s 整页截图高度: %d px", dash.UID, fullHeight)

		renderPath := fmt.Sprintf("/render/d/%s/screenshot?from=%s&to=%s&width=%d&height=%d&theme=%s&kiosk=1&tz=Asia%%2FShanghai%s",
			dash.UID, from, to, width, fullHeight, theme, vars)
		log.Printf("渲染URL: %s", baseURL+renderPath)

		client := &http.Client{Timeout: 120 * time.Second}
		req, err := http.NewRequest("GET", baseURL+renderPath, nil)
		if err != nil {
			result.Error = fmt.Sprintf("请求创建失败: %v", err)
			return result
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			result.Error = fmt.Sprintf("截图请求失败: %v", err)
			return result
		}
		imgData, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 200 && len(imgData) > 0 {
			result.PanelImages = append(result.PanelImages, PanelImage{
				PanelID: 0,
				Title:   dash.Title,
				Data:    imgData,
			})
		} else {
			log.Printf("截图渲染失败: HTTP %d, body=%s, url=%s", resp.StatusCode, string(imgData[:min(len(imgData), 500)]), baseURL+renderPath)
			result.Error = fmt.Sprintf("截图失败: HTTP %d", resp.StatusCode)
		}
	}

	return result
}

func updateTaskStatus(taskID int, status string) {
	database.DB.Exec(`UPDATE grafana_screenshot_tasks SET last_run_at = NOW(), last_status = ? WHERE id = ?`, status, taskID)
}

func getLarkWebhookURLsByIDs(connIDs []int) []string {
	var urls []string
	for _, id := range connIDs {
		_, url, _, _, _, err := GetConnectionByID(id)
		if err == nil && url != "" {
			urls = append(urls, url)
		}
	}
	return urls
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// ---- HTTP Handlers ----

// HandleListScreenshotTasks 获取截图任务列表
func HandleListScreenshotTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme, enabled, last_run_at, last_status, created_at, updated_at FROM grafana_screenshot_tasks ORDER BY id ASC`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var tasks []ScreenshotTask
	for rows.Next() {
		var t ScreenshotTask
		var dashboards, variables, larkConnIDs string
		var lastRunAt sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &t.CronExpr, &t.TimeRange, &t.Width, &t.Height, &t.Theme, &t.Enabled, &lastRunAt, &t.LastStatus, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		t.Dashboards = json.RawMessage(dashboards)
		t.Variables = json.RawMessage(variables)
		t.LarkConnIDs = json.RawMessage(larkConnIDs)
		if lastRunAt.Valid {
			t.LastRunAt = &lastRunAt.String
		}
		tasks = append(tasks, t)
	}
	if tasks == nil {
		tasks = []ScreenshotTask{}
	}
	respondSuccess(w, tasks)
}

// HandleCreateScreenshotTask 创建截图任务
func HandleCreateScreenshotTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string          `json:"name"`
		GrafanaConnID int             `json:"grafana_conn_id"`
		Dashboards    json.RawMessage `json:"dashboards"`
		Variables     json.RawMessage `json:"variables"`
		LarkConnIDs   json.RawMessage `json:"lark_conn_ids"`
		CronExpr      string          `json:"cron_expr"`
		TimeRange     string          `json:"time_range"`
		Width         int             `json:"width"`
		Height        int             `json:"height"`
		Theme         string          `json:"theme"`
		Enabled       bool            `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if req.Name == "" {
		respondBadRequest(w, "任务名称不能为空")
		return
	}
	if req.CronExpr == "" {
		req.CronExpr = "0 * * * *"
	}
	if req.TimeRange == "" {
		req.TimeRange = "1h"
	}
	if req.Width == 0 {
		req.Width = 1000
	}
	if req.Height == 0 {
		req.Height = 500
	}
	if req.Theme == "" {
		req.Theme = "light"
	}

	// 验证 cron 表达式
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(req.CronExpr); err != nil {
		respondBadRequest(w, "无效的 Cron 表达式: "+err.Error())
		return
	}

	dashboardsStr := "{}"
	if req.Dashboards != nil {
		dashboardsStr = string(req.Dashboards)
	}
	variablesStr := "{}"
	if req.Variables != nil {
		variablesStr = string(req.Variables)
	}
	larkConnIDsStr := "[]"
	if req.LarkConnIDs != nil {
		larkConnIDsStr = string(req.LarkConnIDs)
	}

	result, err := database.DB.Exec(
		`INSERT INTO grafana_screenshot_tasks (name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.GrafanaConnID, dashboardsStr, variablesStr, larkConnIDsStr, req.CronExpr, req.TimeRange, req.Width, req.Height, req.Theme, req.Enabled,
	)
	if err != nil {
		respondInternalError(w, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()

	// 如果启用，注册 cron
	if req.Enabled {
		task := ScreenshotTask{
			ID:            int(id),
			Name:          req.Name,
			GrafanaConnID: req.GrafanaConnID,
			Dashboards:    json.RawMessage(dashboardsStr),
			Variables:     json.RawMessage(variablesStr),
			LarkConnIDs:   json.RawMessage(larkConnIDsStr),
			CronExpr:      req.CronExpr,
			TimeRange:     req.TimeRange,
			Width:         req.Width,
			Height:        req.Height,
			Theme:         req.Theme,
		}
		registerCronTask(task)
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "创建截图任务", "screenshot_task", fmt.Sprintf("%d", id), req.Name, GetClientIP(r))

	respondSuccess(w, map[string]interface{}{"id": id, "message": "创建成功"})
}

// HandleUpdateScreenshotTask 更新截图任务
func HandleUpdateScreenshotTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Name          string          `json:"name"`
		GrafanaConnID int             `json:"grafana_conn_id"`
		Dashboards    json.RawMessage `json:"dashboards"`
		Variables     json.RawMessage `json:"variables"`
		LarkConnIDs   json.RawMessage `json:"lark_conn_ids"`
		CronExpr      string          `json:"cron_expr"`
		TimeRange     string          `json:"time_range"`
		Width         int             `json:"width"`
		Height        int             `json:"height"`
		Theme         string          `json:"theme"`
		Enabled       bool            `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	// 验证 cron 表达式
	if req.CronExpr != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(req.CronExpr); err != nil {
			respondBadRequest(w, "无效的 Cron 表达式: "+err.Error())
			return
		}
	}

	dashboardsStr := "[]"
	if req.Dashboards != nil {
		dashboardsStr = string(req.Dashboards)
	}
	variablesStr := "{}"
	if req.Variables != nil {
		variablesStr = string(req.Variables)
	}
	larkConnIDsStr := "[]"
	if req.LarkConnIDs != nil {
		larkConnIDsStr = string(req.LarkConnIDs)
	}

	_, err := database.DB.Exec(
		`UPDATE grafana_screenshot_tasks SET name=?, grafana_conn_id=?, dashboards=?, variables=?, lark_conn_ids=?, cron_expr=?, time_range=?, width=?, height=?, theme=?, enabled=?, updated_at=NOW() WHERE id=?`,
		req.Name, req.GrafanaConnID, dashboardsStr, variablesStr, larkConnIDsStr, req.CronExpr, req.TimeRange, req.Width, req.Height, req.Theme, req.Enabled, id,
	)
	if err != nil {
		respondInternalError(w, "更新失败: "+err.Error())
		return
	}

	// 更新 cron
	taskID, _ := strconv.Atoi(id)
	if req.Enabled {
		task := ScreenshotTask{
			ID:            taskID,
			Name:          req.Name,
			GrafanaConnID: req.GrafanaConnID,
			Dashboards:    json.RawMessage(dashboardsStr),
			Variables:     json.RawMessage(variablesStr),
			LarkConnIDs:   json.RawMessage(larkConnIDsStr),
			CronExpr:      req.CronExpr,
			TimeRange:     req.TimeRange,
			Width:         req.Width,
			Height:        req.Height,
			Theme:         req.Theme,
		}
		registerCronTask(task)
	} else {
		unregisterCronTask(taskID)
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "更新截图任务", "screenshot_task", id, req.Name, GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "更新成功"})
}

// HandleDeleteScreenshotTask 删除截图任务
func HandleDeleteScreenshotTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var name string
	database.DB.QueryRow(`SELECT name FROM grafana_screenshot_tasks WHERE id = ?`, id).Scan(&name)

	taskID, _ := strconv.Atoi(id)
	unregisterCronTask(taskID)

	_, err := database.DB.Exec(`DELETE FROM grafana_screenshot_tasks WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, "删除失败")
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "删除截图任务", "screenshot_task", id, name, GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "已删除"})
}

// HandleRunScreenshotTask 手动执行截图任务
func HandleRunScreenshotTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var task ScreenshotTask
	var dashboards, variables, larkConnIDs string
	err := database.DB.QueryRow(
		`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme FROM grafana_screenshot_tasks WHERE id = ?`, id,
	).Scan(&task.ID, &task.Name, &task.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &task.CronExpr, &task.TimeRange, &task.Width, &task.Height, &task.Theme)
	if err != nil {
		respondError(w, http.StatusNotFound, "任务不存在")
		return
	}
	task.Dashboards = json.RawMessage(dashboards)
	task.Variables = json.RawMessage(variables)
	task.LarkConnIDs = json.RawMessage(larkConnIDs)

	// 异步执行
	go executeScreenshotTask(task)

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "手动执行截图任务", "screenshot_task", id, task.Name, GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "任务已触发执行"})
}

// HandlePreviewScreenshotTask 预览截图（不发送飞书，返回 base64 图片）
func HandlePreviewScreenshotTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var task ScreenshotTask
	var dashboards, variables, larkConnIDs string
	err := database.DB.QueryRow(
		`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme FROM grafana_screenshot_tasks WHERE id = ?`, id,
	).Scan(&task.ID, &task.Name, &task.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &task.CronExpr, &task.TimeRange, &task.Width, &task.Height, &task.Theme)
	if err != nil {
		respondError(w, http.StatusNotFound, "任务不存在")
		return
	}
	task.Dashboards = json.RawMessage(dashboards)
	task.Variables = json.RawMessage(variables)

	// 解析 dashboards
	var taskDashboards []TaskDashboard
	if err := json.Unmarshal(task.Dashboards, &taskDashboards); err != nil {
		respondBadRequest(w, "Dashboard 配置解析失败")
		return
	}

	var varMap map[string]string
	json.Unmarshal(task.Variables, &varMap)

	// 获取 Grafana 配置
	_, grafanaURL, _, grafanaToken, _, err := GetConnectionByID(task.GrafanaConnID)
	if err != nil {
		respondBadRequest(w, "Grafana 连接不存在")
		return
	}
	grafanaURL = strings.TrimRight(grafanaURL, "/")

	// 截图
	type PreviewPanel struct {
		PanelID int    `json:"panel_id"`
		Title   string `json:"title"`
		Base64  string `json:"base64"`
	}
	type PreviewDashboard struct {
		UID    string         `json:"uid"`
		Title  string         `json:"title"`
		Error  string         `json:"error,omitempty"`
		Panels []PreviewPanel `json:"panels"`
	}

	var results []PreviewDashboard
	for _, dash := range taskDashboards {
		result := screenshotDashboard(grafanaURL, grafanaToken, dash, varMap, task.TimeRange, task.Width, task.Height, task.Theme)
		pd := PreviewDashboard{UID: result.UID, Title: result.Title, Error: result.Error}
		for _, img := range result.PanelImages {
			pd.Panels = append(pd.Panels, PreviewPanel{
				PanelID: img.PanelID,
				Title:   img.Title,
				Base64:  "data:image/png;base64," + encodeBase64(img.Data),
			})
		}
		results = append(results, pd)
	}

	respondSuccess(w, results)
}

// HandleTestSendScreenshotTask 测试发送截图到飞书
func HandleTestSendScreenshotTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var task ScreenshotTask
	var dashboards, variables, larkConnIDs string
	err := database.DB.QueryRow(
		`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme FROM grafana_screenshot_tasks WHERE id = ?`, id,
	).Scan(&task.ID, &task.Name, &task.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &task.CronExpr, &task.TimeRange, &task.Width, &task.Height, &task.Theme)
	if err != nil {
		respondError(w, http.StatusNotFound, "任务不存在")
		return
	}
	task.Dashboards = json.RawMessage(dashboards)
	task.Variables = json.RawMessage(variables)
	task.LarkConnIDs = json.RawMessage(larkConnIDs)

	// 解析飞书连接
	var larkIDs []int
	json.Unmarshal(task.LarkConnIDs, &larkIDs)
	larkURLs := getLarkWebhookURLsByIDs(larkIDs)
	if len(larkURLs) == 0 {
		respondBadRequest(w, "该任务未配置飞书连接")
		return
	}

	// 解析 dashboards & variables
	var taskDashboards []TaskDashboard
	json.Unmarshal(task.Dashboards, &taskDashboards)
	var varMap map[string]string
	json.Unmarshal(task.Variables, &varMap)

	// 获取 Grafana 配置
	_, grafanaURL, _, grafanaToken, _, err := GetConnectionByID(task.GrafanaConnID)
	if err != nil {
		respondBadRequest(w, "Grafana 连接不存在")
		return
	}
	grafanaURL = strings.TrimRight(grafanaURL, "/")

	// 截图
	var results []DashboardScreenshot
	for _, dash := range taskDashboards {
		result := screenshotDashboard(grafanaURL, grafanaToken, dash, varMap, task.TimeRange, task.Width, task.Height, task.Theme)
		results = append(results, result)
	}

	// 发送到飞书
	var sendErrors []string
	for _, webhookURL := range larkURLs {
		if err := SendLarkScreenshotResult(webhookURL, task.Name+"(测试)", results, task.Width, task.Height); err != nil {
			sendErrors = append(sendErrors, err.Error())
		}
	}

	if len(sendErrors) > 0 {
		respondError(w, http.StatusInternalServerError, fmt.Sprintf("部分发送失败: %s", strings.Join(sendErrors, "; ")))
		return
	}

	respondSuccess(w, map[string]string{"message": fmt.Sprintf("已发送到 %d 个飞书群", len(larkURLs))})
}

// HandleToggleScreenshotTask 启用/禁用截图任务
func HandleToggleScreenshotTask(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	_, err := database.DB.Exec(`UPDATE grafana_screenshot_tasks SET enabled = ?, updated_at = NOW() WHERE id = ?`, req.Enabled, id)
	if err != nil {
		respondInternalError(w, "更新失败")
		return
	}

	taskID, _ := strconv.Atoi(id)
	if req.Enabled {
		// 重新加载任务并注册
		var task ScreenshotTask
		var dashboards, variables, larkConnIDs string
		database.DB.QueryRow(
			`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme FROM grafana_screenshot_tasks WHERE id = ?`, id,
		).Scan(&task.ID, &task.Name, &task.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &task.CronExpr, &task.TimeRange, &task.Width, &task.Height, &task.Theme)
		task.Dashboards = json.RawMessage(dashboards)
		task.Variables = json.RawMessage(variables)
		task.LarkConnIDs = json.RawMessage(larkConnIDs)
		registerCronTask(task)
	} else {
		unregisterCronTask(taskID)
	}

	status := "已启用"
	if !req.Enabled {
		status = "已禁用"
	}
	respondSuccess(w, map[string]string{"message": status})
}
