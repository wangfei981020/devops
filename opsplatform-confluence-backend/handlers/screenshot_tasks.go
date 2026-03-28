package handlers

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
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
	SortOrder     int             `json:"sort_order"`
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

	reloadAllCronTasks()
}

// reloadAllCronTasks 重新加载所有定时任务，按 cron_expr 分组，同组按 sort_order 串行执行
func reloadAllCronTasks() {
	cronMu.Lock()
	defer cronMu.Unlock()

	// 清除所有已注册的 cron
	for taskID, entryID := range cronEntries {
		cronScheduler.Remove(entryID)
		delete(cronEntries, taskID)
	}

	rows, err := database.DB.Query(`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme, sort_order FROM grafana_screenshot_tasks WHERE enabled = 1 ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		log.Printf("加载截图任务失败: %v", err)
		return
	}
	defer rows.Close()

	// 按 cron_expr 分组
	groups := make(map[string][]ScreenshotTask)
	var order []string // 保持分组顺序
	for rows.Next() {
		var task ScreenshotTask
		var dashboards, variables, larkConnIDs string
		if err := rows.Scan(&task.ID, &task.Name, &task.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &task.CronExpr, &task.TimeRange, &task.Width, &task.Height, &task.Theme, &task.SortOrder); err != nil {
			continue
		}
		task.Dashboards = json.RawMessage(dashboards)
		task.Variables = json.RawMessage(variables)
		task.LarkConnIDs = json.RawMessage(larkConnIDs)

		if _, exists := groups[task.CronExpr]; !exists {
			order = append(order, task.CronExpr)
		}
		groups[task.CronExpr] = append(groups[task.CronExpr], task)
	}

	count := 0
	for _, cronExpr := range order {
		tasks := groups[cronExpr]
		tasksCopy := make([]ScreenshotTask, len(tasks))
		copy(tasksCopy, tasks)

		entryID, err := cronScheduler.AddFunc(cronExpr, func() {
			executeScreenshotTaskGroup(tasksCopy)
		})
		if err != nil {
			log.Printf("注册截图任务组 cron=%s 失败: %v", cronExpr, err)
			continue
		}
		// 用第一个任务的 ID 作为 key（标记该组已注册）
		for _, t := range tasks {
			cronEntries[t.ID] = entryID
		}
		count += len(tasks)
		log.Printf("注册截图任务组 cron=%s, %d个任务, 排序: %v", cronExpr, len(tasks), func() []string {
			var names []string
			for _, t := range tasks {
				names = append(names, fmt.Sprintf("%s(%d)", t.Name, t.SortOrder))
			}
			return names
		}())
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
	reloadAllCronTasks()
}

func unregisterCronTask(taskID int) {
	reloadAllCronTasks()
}

// taskScreenshotResult 截图阶段的结果
type taskScreenshotResult struct {
	Task       ScreenshotTask
	Results    []DashboardScreenshot
	LarkURLs   []string
	Error      string
}

// executeScreenshotTask 执行单个截图任务（截图+发送，用于手动执行等场景）
func executeScreenshotTask(task ScreenshotTask) {
	result := captureScreenshots(task)
	if result.Error != "" {
		return
	}
	sendTaskNotifications(result)
}

// executeScreenshotTaskGroup 按排序串行截图+串行发送（定时任务使用）
func executeScreenshotTaskGroup(tasks []ScreenshotTask) {
	if len(tasks) == 0 {
		return
	}
	log.Printf("开始执行截图任务组: %d 个任务", len(tasks))

	// 阶段1: 按 sort_order 逐个截图（避免并发压垮 Grafana renderer），失败自动重试1次
	results := make([]taskScreenshotResult, len(tasks))
	for i, task := range tasks {
		r := captureScreenshots(task)
		// 如果截图失败，等待 10 秒后重试一次
		if r.Error != "" {
			log.Printf("截图任务 [%s] 失败，10秒后重试: %s", task.Name, r.Error)
			time.Sleep(10 * time.Second)
			r = captureScreenshots(task)
			if r.Error != "" {
				log.Printf("截图任务 [%s] 重试仍然失败: %s", task.Name, r.Error)
			}
		}
		results[i] = r
	}
	log.Printf("截图任务组截图阶段完成，开始按排序发送通知")

	// 阶段2: 按 sort_order 顺序串行发送通知
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		sendTaskNotifications(r)
	}
	log.Printf("截图任务组全部完成")
}

// captureScreenshots 执行截图阶段（不发送）
func captureScreenshots(task ScreenshotTask) taskScreenshotResult {
	log.Printf("开始截图: [%s]", task.Name)

	var dashboards []TaskDashboard
	if err := json.Unmarshal(task.Dashboards, &dashboards); err != nil {
		updateTaskStatus(task.ID, "error: "+err.Error())
		return taskScreenshotResult{Task: task, Error: err.Error()}
	}

	var variables map[string]string
	json.Unmarshal(task.Variables, &variables)

	var larkConnIDs []int
	json.Unmarshal(task.LarkConnIDs, &larkConnIDs)

	_, grafanaURL, _, grafanaToken, _, err := GetConnectionByID(task.GrafanaConnID)
	if err != nil {
		updateTaskStatus(task.ID, "error: Grafana连接不存在")
		return taskScreenshotResult{Task: task, Error: "Grafana连接不存在"}
	}
	grafanaURL = strings.TrimRight(grafanaURL, "/")

	var results []DashboardScreenshot
	for _, dash := range dashboards {
		result := screenshotDashboard(grafanaURL, grafanaToken, dash, variables, task.TimeRange, task.Width, task.Height, task.Theme)
		results = append(results, result)
	}

	larkURLs := getLarkWebhookURLsByIDs(larkConnIDs)
	log.Printf("截图完成: [%s], %d个Dashboard", task.Name, len(results))

	return taskScreenshotResult{
		Task:     task,
		Results:  results,
		LarkURLs: larkURLs,
	}
}

// sendTaskNotifications 发送单个任务的飞书通知
func sendTaskNotifications(r taskScreenshotResult) {
	log.Printf("发送通知: [%s] (排序:%d)", r.Task.Name, r.Task.SortOrder)
	for _, webhookURL := range r.LarkURLs {
		if err := SendLarkScreenshotResult(webhookURL, r.Task.Name, r.Results, r.Task.Width, r.Task.Height); err != nil {
			log.Printf("发送飞书消息失败 [%s]: %v", r.Task.Name, err)
		}
	}
	updateTaskStatus(r.Task.ID, "success")
	log.Printf("通知发送完成: [%s]", r.Task.Name)
}

func screenshotDashboard(baseURL, token string, dash TaskDashboard, variables map[string]string, timeRange string, width, height int, theme string) DashboardScreenshot {
	result := DashboardScreenshot{
		UID:   dash.UID,
		Title: dash.Title,
	}

	var from, to string
	if strings.HasPrefix(timeRange, "epoch:") {
		// 绝对时间戳格式: epoch:fromMs-toMs
		parts := strings.SplitN(strings.TrimPrefix(timeRange, "epoch:"), "-", 2)
		if len(parts) == 2 {
			from = parts[0]
			to = parts[1]
		} else {
			from = "now-1h"
			to = "now"
		}
	} else if strings.HasPrefix(timeRange, "custom:") {
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
				renderPath := fmt.Sprintf("/render/d-solo/%s/panel?panelId=%d&from=%s&to=%s&width=%d&height=%d&theme=%s&timeout=600&tz=Asia%%2FShanghai%s",
					dash.UID, pid, from, to, width, height, theme, vars)

				client := &http.Client{Timeout: 660 * time.Second}
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

		// 超过8天的时间范围降低渲染负载，避免 renderer Chrome 崩溃
		scaleParam := ""
		renderHeight := fullHeight
		fromMs, _ := strconv.ParseInt(from, 10, 64)
		toMs, _ := strconv.ParseInt(to, 10, 64)
		if fromMs > 0 && toMs > 0 && (toMs-fromMs) > 8*24*3600*1000 {
			scaleParam = "&scale=1"
		}

		renderPath := fmt.Sprintf("/render/d/%s/screenshot?from=%s&to=%s&width=%d&height=%d&theme=%s&kiosk=1&timeout=600%s&tz=Asia%%2FShanghai%s",
			dash.UID, from, to, width, renderHeight, theme, scaleParam, vars)
		log.Printf("渲染URL: %s", baseURL+renderPath)

		client := &http.Client{Timeout: 660 * time.Second}
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
	rows, err := database.DB.Query(`SELECT id, name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme, sort_order, enabled, last_run_at, last_status, created_at, updated_at FROM grafana_screenshot_tasks ORDER BY sort_order ASC, id ASC`)
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
		if err := rows.Scan(&t.ID, &t.Name, &t.GrafanaConnID, &dashboards, &variables, &larkConnIDs, &t.CronExpr, &t.TimeRange, &t.Width, &t.Height, &t.Theme, &t.SortOrder, &t.Enabled, &lastRunAt, &t.LastStatus, &t.CreatedAt, &t.UpdatedAt); err != nil {
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
		SortOrder     int             `json:"sort_order"`
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
		`INSERT INTO grafana_screenshot_tasks (name, grafana_conn_id, dashboards, variables, lark_conn_ids, cron_expr, time_range, width, height, theme, sort_order, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.GrafanaConnID, dashboardsStr, variablesStr, larkConnIDsStr, req.CronExpr, req.TimeRange, req.Width, req.Height, req.Theme, req.SortOrder, req.Enabled,
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
		SortOrder     int             `json:"sort_order"`
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
		`UPDATE grafana_screenshot_tasks SET name=?, grafana_conn_id=?, dashboards=?, variables=?, lark_conn_ids=?, cron_expr=?, time_range=?, width=?, height=?, theme=?, sort_order=?, enabled=?, updated_at=NOW() WHERE id=?`,
		req.Name, req.GrafanaConnID, dashboardsStr, variablesStr, larkConnIDsStr, req.CronExpr, req.TimeRange, req.Width, req.Height, req.Theme, req.SortOrder, req.Enabled, id,
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

// PreviewPanel 预览面板
type PreviewPanel struct {
	PanelID int    `json:"panel_id"`
	Title   string `json:"title"`
	Base64  string `json:"base64"`
}

// PreviewDashboard 预览 Dashboard
type PreviewDashboard struct {
	UID    string         `json:"uid"`
	Title  string         `json:"title"`
	Error  string         `json:"error,omitempty"`
	Panels []PreviewPanel `json:"panels"`
}

// previewDashboardByPanels 逐面板截图+拼成一张图（用于长时间范围>8天）
func previewDashboardByPanels(baseURL, token string, dash TaskDashboard, variables map[string]string, fromMs, toMs int64, width, height int, theme string) PreviewDashboard {
	result := PreviewDashboard{UID: dash.UID, Title: dash.Title}

	// 1. 获取 Dashboard 面板信息
	data, status, err := grafanaRequest(baseURL, token, "/api/dashboards/uid/"+dash.UID)
	if err != nil || status != 200 {
		result.Error = fmt.Sprintf("获取Dashboard信息失败: %v (status=%d)", err, status)
		return result
	}

	type PanelInfo struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Type    string `json:"type"`
		GridPos struct {
			H int `json:"h"`
			W int `json:"w"`
			X int `json:"x"`
			Y int `json:"y"`
		} `json:"gridPos"`
		Panels []PanelInfo `json:"panels"` // row 嵌套面板
	}
	var dashData struct {
		Dashboard struct {
			Panels []PanelInfo `json:"panels"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal(data, &dashData); err != nil {
		result.Error = "解析Dashboard面板信息失败"
		return result
	}

	// 2. 展开所有面板（包括 row 内嵌套的），按 Y 坐标排序
	var allPanels []PanelInfo
	for _, p := range dashData.Dashboard.Panels {
		if p.Type == "row" {
			for _, np := range p.Panels {
				allPanels = append(allPanels, np)
			}
		} else {
			allPanels = append(allPanels, p)
		}
	}

	if len(allPanels) == 0 {
		result.Error = "Dashboard 没有面板"
		return result
	}

	log.Printf("Dashboard %s 逐面板截图模式: %d 个面板", dash.UID, len(allPanels))

	// 3. 构建 var- 参数
	from := fmt.Sprintf("%d", fromMs)
	to := fmt.Sprintf("%d", toMs)
	vars := ""
	for k, v := range variables {
		vars += "&var-" + k + "=" + v
	}

	// 4. 逐面板截图（并发3个）
	type panelResult struct {
		Index int
		Info  PanelInfo
		Data  []byte
		OK    bool
	}
	ch := make(chan panelResult, len(allPanels))
	sem := make(chan struct{}, 3)

	for i, panel := range allPanels {
		go func(idx int, p PanelInfo) {
			sem <- struct{}{}
			defer func() { <-sem }()

			// 每个面板按 gridPos 计算宽高（Grafana 栅格24列）
			panelWidth := width * p.GridPos.W / 24
			panelHeight := p.GridPos.H * 30 // Grafana 每个网格单元约30px
			if panelHeight < 200 {
				panelHeight = 200
			}

			renderPath := fmt.Sprintf("/render/d-solo/%s/panel?panelId=%d&from=%s&to=%s&width=%d&height=%d&theme=%s&timeout=600&scale=1&tz=Asia%%2FShanghai%s",
				dash.UID, p.ID, from, to, panelWidth, panelHeight, theme, vars)

			client := &http.Client{Timeout: 660 * time.Second}
			req, err := http.NewRequest("GET", baseURL+renderPath, nil)
			if err != nil {
				ch <- panelResult{Index: idx, Info: p}
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)

			resp, err := client.Do(req)
			if err != nil {
				log.Printf("面板 %d [%s] 截图请求失败: %v", p.ID, p.Title, err)
				ch <- panelResult{Index: idx, Info: p}
				return
			}
			imgData, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == 200 && len(imgData) > 0 {
				log.Printf("面板 %d [%s] 截图成功 (%d bytes)", p.ID, p.Title, len(imgData))
				ch <- panelResult{Index: idx, Info: p, Data: imgData, OK: true}
			} else {
				log.Printf("面板 %d [%s] 截图失败: HTTP %d", p.ID, p.Title, resp.StatusCode)
				ch <- panelResult{Index: idx, Info: p}
			}
		}(i, panel)
	}

	panelResults := make([]panelResult, len(allPanels))
	for range allPanels {
		r := <-ch
		panelResults[r.Index] = r
	}

	// 5. 按 gridPos 拼成一张完整图
	// 计算总高度
	maxBottom := 0
	for _, p := range allPanels {
		bottom := (p.GridPos.Y + p.GridPos.H) * 30
		if bottom > maxBottom {
			maxBottom = bottom
		}
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, maxBottom))
	// 填充白色背景
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)

	successCount := 0
	for _, pr := range panelResults {
		if !pr.OK || len(pr.Data) == 0 {
			continue
		}
		img, err := png.Decode(bytes.NewReader(pr.Data))
		if err != nil {
			log.Printf("面板 %d 图片解码失败: %v", pr.Info.ID, err)
			continue
		}

		// 计算面板在画布上的位置
		x := width * pr.Info.GridPos.X / 24
		y := pr.Info.GridPos.Y * 30
		dstRect := image.Rect(x, y, x+img.Bounds().Dx(), y+img.Bounds().Dy())
		draw.Draw(canvas, dstRect, img, image.Point{}, draw.Over)
		successCount++
	}

	if successCount == 0 {
		result.Error = "所有面板截图均失败"
		return result
	}

	// 6. 编码为 PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		result.Error = "拼图编码失败: " + err.Error()
		return result
	}

	log.Printf("Dashboard %s 逐面板拼图完成: %d/%d 面板成功, 图片大小 %d bytes", dash.UID, successCount, len(allPanels), buf.Len())

	result.Panels = []PreviewPanel{{
		PanelID: 0,
		Title:   dash.Title,
		Base64:  "data:image/png;base64," + encodeBase64(buf.Bytes()),
	}}
	return result
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

	// 支持自定义时间范围覆盖（from=2026-03-18&to=2026-03-25）
	timeRange := task.TimeRange
	if fromDate := r.URL.Query().Get("from"); fromDate != "" {
		if toDate := r.URL.Query().Get("to"); toDate != "" {
			loc, _ := time.LoadLocation("Asia/Shanghai")
			if loc == nil {
				loc = time.UTC
			}
			now := time.Now().In(loc)
			today := now.Format("2006-01-02")

			_, err1 := time.ParseInLocation("2006-01-02", fromDate, loc)
			_, err2 := time.ParseInLocation("2006-01-02", toDate, loc)
			if err1 == nil && err2 == nil {
				// 结束时间：如果是今天，用当前时间；否则用当天 23:59:59
				var toMs int64
				if toDate == today {
					toMs = now.UnixMilli()
				} else {
					toTime, _ := time.ParseInLocation("2006-01-02", toDate, loc)
					toMs = toTime.Add(24*time.Hour - time.Second).UnixMilli()
				}
				// 开始时间：用结束时间往前推对应天数的同一时刻
				fromTime, _ := time.ParseInLocation("2006-01-02", fromDate, loc)
				toDateParsed, _ := time.ParseInLocation("2006-01-02", toDate, loc)
				if toDate == today {
					// 精确到当前时刻往前推
					days := toDateParsed.Sub(fromTime).Hours() / 24
					fromMs := now.Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
					timeRange = fmt.Sprintf("epoch:%d-%d", fromMs, toMs)
				} else {
					fromMs := fromTime.UnixMilli()
					timeRange = fmt.Sprintf("epoch:%d-%d", fromMs, toMs)
				}
			}
		}
	}

	// 获取 Grafana 配置
	_, grafanaURL, _, grafanaToken, _, err := GetConnectionByID(task.GrafanaConnID)
	if err != nil {
		respondBadRequest(w, "Grafana 连接不存在")
		return
	}
	grafanaURL = strings.TrimRight(grafanaURL, "/")

	var results []PreviewDashboard
	for _, dash := range taskDashboards {
		result := screenshotDashboard(grafanaURL, grafanaToken, dash, varMap, timeRange, task.Width, task.Height, task.Theme)
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

// HandleGetDashboardPanels 获取 Dashboard 的面板列表（用于前端逐面板截图进度）
func HandleGetDashboardPanels(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var task ScreenshotTask
	var dashboards, variables string
	err := database.DB.QueryRow(
		`SELECT id, grafana_conn_id, dashboards, variables, width, height FROM grafana_screenshot_tasks WHERE id = ?`, id,
	).Scan(&task.ID, &task.GrafanaConnID, &dashboards, &variables, &task.Width, &task.Height)
	if err != nil {
		respondError(w, http.StatusNotFound, "任务不存在")
		return
	}

	var taskDashboards []TaskDashboard
	if err := json.Unmarshal([]byte(dashboards), &taskDashboards); err != nil {
		respondBadRequest(w, "Dashboard 配置解析失败")
		return
	}

	_, grafanaURL, _, grafanaToken, _, err := GetConnectionByID(task.GrafanaConnID)
	if err != nil {
		respondBadRequest(w, "Grafana 连接不存在")
		return
	}
	grafanaURL = strings.TrimRight(grafanaURL, "/")

	type PanelInfo struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Type    string `json:"type"`
		X       int    `json:"x"`
		Y       int    `json:"y"`
		W       int    `json:"w"`
		H       int    `json:"h"`
	}
	type DashboardPanels struct {
		UID    string      `json:"uid"`
		Title  string      `json:"title"`
		Panels []PanelInfo `json:"panels"`
	}

	var results []DashboardPanels
	for _, dash := range taskDashboards {
		dp := DashboardPanels{UID: dash.UID, Title: dash.Title}

		data, status, err := grafanaRequest(grafanaURL, grafanaToken, "/api/dashboards/uid/"+dash.UID)
		if err != nil || status != 200 {
			results = append(results, dp)
			continue
		}

		var dashData struct {
			Dashboard struct {
				Panels []struct {
					ID      int    `json:"id"`
					Title   string `json:"title"`
					Type    string `json:"type"`
					GridPos struct {
						H int `json:"h"`
						W int `json:"w"`
						X int `json:"x"`
						Y int `json:"y"`
					} `json:"gridPos"`
					Panels []struct {
						ID      int    `json:"id"`
						Title   string `json:"title"`
						Type    string `json:"type"`
						GridPos struct {
							H int `json:"h"`
							W int `json:"w"`
							X int `json:"x"`
							Y int `json:"y"`
						} `json:"gridPos"`
					} `json:"panels"`
				} `json:"panels"`
			} `json:"dashboard"`
		}
		json.Unmarshal(data, &dashData)

		for _, p := range dashData.Dashboard.Panels {
			if p.Type == "row" {
				for _, np := range p.Panels {
					dp.Panels = append(dp.Panels, PanelInfo{ID: np.ID, Title: np.Title, Type: np.Type, X: np.GridPos.X, Y: np.GridPos.Y, W: np.GridPos.W, H: np.GridPos.H})
				}
			} else {
				dp.Panels = append(dp.Panels, PanelInfo{ID: p.ID, Title: p.Title, Type: p.Type, X: p.GridPos.X, Y: p.GridPos.Y, W: p.GridPos.W, H: p.GridPos.H})
			}
		}
		results = append(results, dp)
	}

	respondSuccess(w, results)
}

// HandleRenderSinglePanel 渲染单个面板，返回 base64 图片
func HandleRenderSinglePanel(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	panelID := r.URL.Query().Get("panelId")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	dashUID := r.URL.Query().Get("dashUid")

	if panelID == "" || from == "" || to == "" || dashUID == "" {
		respondBadRequest(w, "缺少参数: panelId, from, to, dashUid")
		return
	}

	var task ScreenshotTask
	var variables string
	err := database.DB.QueryRow(
		`SELECT id, grafana_conn_id, variables, width, height, theme FROM grafana_screenshot_tasks WHERE id = ?`, id,
	).Scan(&task.ID, &task.GrafanaConnID, &variables, &task.Width, &task.Height, &task.Theme)
	if err != nil {
		respondError(w, http.StatusNotFound, "任务不存在")
		return
	}

	var varMap map[string]string
	json.Unmarshal([]byte(variables), &varMap)

	_, grafanaURL, _, grafanaToken, _, err := GetConnectionByID(task.GrafanaConnID)
	if err != nil {
		respondBadRequest(w, "Grafana 连接不存在")
		return
	}
	grafanaURL = strings.TrimRight(grafanaURL, "/")

	// 面板宽高从查询参数获取
	pw, _ := strconv.Atoi(r.URL.Query().Get("width"))
	ph, _ := strconv.Atoi(r.URL.Query().Get("height"))
	if pw == 0 { pw = task.Width }
	if ph == 0 { ph = 400 }

	vars := ""
	for k, v := range varMap {
		vars += "&var-" + k + "=" + v
	}

	renderPath := fmt.Sprintf("/render/d-solo/%s/panel?panelId=%s&from=%s&to=%s&width=%d&height=%d&theme=%s&timeout=600&scale=1&tz=Asia%%2FShanghai%s",
		dashUID, panelID, from, to, pw, ph, task.Theme, vars)

	client := &http.Client{Timeout: 660 * time.Second}
	req2, err := http.NewRequest("GET", grafanaURL+renderPath, nil)
	if err != nil {
		respondInternalError(w, "请求创建失败")
		return
	}
	req2.Header.Set("Authorization", "Bearer "+grafanaToken)

	resp, err := client.Do(req2)
	if err != nil {
		respondInternalError(w, "截图请求失败: "+err.Error())
		return
	}
	imgData, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != 200 || len(imgData) == 0 {
		respondInternalError(w, fmt.Sprintf("截图失败: HTTP %d", resp.StatusCode))
		return
	}

	respondSuccess(w, map[string]string{
		"base64": "data:image/png;base64," + encodeBase64(imgData),
	})
}
