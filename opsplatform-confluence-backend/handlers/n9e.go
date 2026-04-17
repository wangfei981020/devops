package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"opsplatform-confluence-backend/database"
	"opsplatform-confluence-backend/n9e"

	"github.com/gorilla/mux"
)

// ============ 维护窗口 CRUD ============

// HandleListMaintenanceWindows 获取维护窗口列表
func HandleListMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, project, COALESCE(environment,'PROD'), COALESCE(repeat_mode,'once'), maintenance_type, weekday, COALESCE(time_start,''), COALESCE(time_end,''), start_time, end_time, COALESCE(match_rules,''), COALESCE(operations,'[]'), note, created_at, updated_at FROM maintenance_windows ORDER BY id DESC`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var windows []map[string]interface{}
	for rows.Next() {
		var id, weekday int
		var project, environment, repeatMode, mType, timeStart, timeEnd, matchRules, operationsStr, note, createdAt, updatedAt string
		var startTime, endTime sql.NullString
		if err := rows.Scan(&id, &project, &environment, &repeatMode, &mType, &weekday, &timeStart, &timeEnd, &startTime, &endTime, &matchRules, &operationsStr, &note, &createdAt, &updatedAt); err != nil {
			continue
		}
		var operations json.RawMessage
		if operationsStr != "" && operationsStr != "null" {
			operations = json.RawMessage(operationsStr)
		} else {
			operations = json.RawMessage("[]")
		}
		windows = append(windows, map[string]interface{}{
			"id":               id,
			"project":          project,
			"environment":      environment,
			"repeat_mode":      repeatMode,
			"maintenance_type": mType,
			"weekday":          weekday,
			"time_start":       timeStart,
			"time_end":         timeEnd,
			"start_time":       startTime.String,
			"end_time":         endTime.String,
			"match_rules":      matchRules,
			"operations":       operations,
			"note":             note,
			"created_at":       createdAt,
			"updated_at":       updatedAt,
		})
	}
	if windows == nil {
		windows = []map[string]interface{}{}
	}
	respondSuccess(w, windows)
}

// HandleCreateMaintenanceWindow 创建维护窗口
func HandleCreateMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project     string          `json:"project"`
		Environment string          `json:"environment"`
		RepeatMode  string          `json:"repeat_mode"`
		Type        string          `json:"maintenance_type"`
		Weekday     int             `json:"weekday"`
		TimeStart   string          `json:"time_start"`
		TimeEnd     string          `json:"time_end"`
		Start       string          `json:"start_time"`
		End         string          `json:"end_time"`
		MatchRules  string          `json:"match_rules"`
		Operations  json.RawMessage `json:"operations"`
		Note        string          `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if req.Project == "" {
		respondBadRequest(w, "项目名不能为空")
		return
	}
	if req.RepeatMode == "weekly" {
		if req.Weekday < 1 || req.Weekday > 7 || req.TimeStart == "" || req.TimeEnd == "" {
			respondBadRequest(w, "每周重复需要指定星期和时间段")
			return
		}
	} else {
		req.RepeatMode = "once"
		if req.Start == "" || req.End == "" {
			respondBadRequest(w, "单次维护需要指定开始和结束时间")
			return
		}
	}
	if req.Type == "" {
		req.Type = "例行维护"
	}
	if req.Environment == "" {
		req.Environment = "PROD"
	}

	opsJSON := "[]"
	if req.Operations != nil && string(req.Operations) != "null" {
		opsJSON = string(req.Operations)
	}

	var result sql.Result
	var err error
	if req.RepeatMode == "weekly" {
		result, err = database.DB.Exec(
			`INSERT INTO maintenance_windows (project, environment, repeat_mode, maintenance_type, weekday, time_start, time_end, match_rules, operations, note) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Project, req.Environment, req.RepeatMode, req.Type, req.Weekday, req.TimeStart, req.TimeEnd, req.MatchRules, opsJSON, req.Note,
		)
	} else {
		result, err = database.DB.Exec(
			`INSERT INTO maintenance_windows (project, environment, repeat_mode, maintenance_type, start_time, end_time, match_rules, operations, note) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Project, req.Environment, req.RepeatMode, req.Type, req.Start, req.End, req.MatchRules, opsJSON, req.Note,
		)
	}
	if err != nil {
		respondInternalError(w, "创建失败: "+err.Error())
		return
	}
	id, _ := result.LastInsertId()
	respondSuccess(w, map[string]interface{}{"id": id, "message": "创建成功"})
}

// HandleUpdateMaintenanceWindow 更新维护窗口
func HandleUpdateMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Project     string          `json:"project"`
		Environment string          `json:"environment"`
		RepeatMode  string          `json:"repeat_mode"`
		Type        string          `json:"maintenance_type"`
		Weekday     int             `json:"weekday"`
		TimeStart   string          `json:"time_start"`
		TimeEnd     string          `json:"time_end"`
		Start       string          `json:"start_time"`
		End         string          `json:"end_time"`
		MatchRules  string          `json:"match_rules"`
		Operations  json.RawMessage `json:"operations"`
		Note        string          `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if req.Environment == "" {
		req.Environment = "PROD"
	}
	if req.RepeatMode == "" {
		req.RepeatMode = "once"
	}
	opsJSON := "[]"
	if req.Operations != nil && string(req.Operations) != "null" {
		opsJSON = string(req.Operations)
	}

	var err error
	if req.RepeatMode == "weekly" {
		_, err = database.DB.Exec(
			`UPDATE maintenance_windows SET project=?, environment=?, repeat_mode=?, maintenance_type=?, weekday=?, time_start=?, time_end=?, start_time=NULL, end_time=NULL, match_rules=?, operations=?, note=?, updated_at=NOW() WHERE id=?`,
			req.Project, req.Environment, req.RepeatMode, req.Type, req.Weekday, req.TimeStart, req.TimeEnd, req.MatchRules, opsJSON, req.Note, id,
		)
	} else {
		_, err = database.DB.Exec(
			`UPDATE maintenance_windows SET project=?, environment=?, repeat_mode=?, maintenance_type=?, weekday=0, time_start='', time_end='', start_time=?, end_time=?, match_rules=?, operations=?, note=?, updated_at=NOW() WHERE id=?`,
			req.Project, req.Environment, req.RepeatMode, req.Type, req.Start, req.End, req.MatchRules, opsJSON, req.Note, id,
		)
	}
	if err != nil {
		respondInternalError(w, "更新失败")
		return
	}
	respondSuccess(w, map[string]string{"message": "更新成功"})
}

// HandleDeleteMaintenanceWindow 删除维护窗口
func HandleDeleteMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	_, err := database.DB.Exec(`DELETE FROM maintenance_windows WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, "删除失败")
		return
	}
	respondSuccess(w, map[string]string{"message": "已删除"})
}

// ============ N9E 告警统计 ============

// maintenanceWindow 维护窗口结构
type maintenanceWindow struct {
	Project string
	Type    string
	Start   time.Time
	End     time.Time
}

// effectiveAlert 有效告警
type effectiveAlert struct {
	RuleName        string   `json:"rule_name"`
	Severity        int      `json:"severity"`
	TriggerTime     int64    `json:"trigger_time"`
	RecoverTime     int64    `json:"recover_time"`
	IsRecovered     int      `json:"is_recovered"`
	GroupName       string   `json:"group_name"`
	Tags            []string `json:"tags"`
	RawCount        int      `json:"raw_count"`
	Impact          string   `json:"impact"`
	Handler         string   `json:"handler"`
	Status          string   `json:"status"`
	Note            string   `json:"note"`
	IsMaintenance   bool     `json:"is_maintenance"`
	MaintenanceType string   `json:"maintenance_type"`
}

// HandleN9EAlertStats 获取告警统计
func HandleN9EAlertStats(w http.ResponseWriter, r *http.Request) {
	connIDStr := r.URL.Query().Get("conn_id")
	stimeStr := r.URL.Query().Get("stime")
	etimeStr := r.URL.Query().Get("etime")

	if stimeStr == "" || etimeStr == "" {
		respondBadRequest(w, "stime 和 etime 不能为空")
		return
	}

	stime, err := strconv.ParseInt(stimeStr, 10, 64)
	if err != nil {
		respondBadRequest(w, "stime 格式错误")
		return
	}
	etime, err := strconv.ParseInt(etimeStr, 10, 64)
	if err != nil {
		respondBadRequest(w, "etime 格式错误")
		return
	}

	// 获取 N9E 连接
	client, err := getN9EClient(connIDStr)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	// 从 N9E 拉取历史告警
	events, total, err := client.FetchAlertHisEvents(stime, etime)
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取告警数据失败: "+err.Error())
		return
	}

	// 获取维护窗口
	windows := loadMaintenanceWindows()

	// 处理告警：去重 + 维护窗口合并
	effective := processAlerts(events, windows)

	// 统计
	var handledCount int
	for _, a := range effective {
		if a.Status == "已处理" || a.IsMaintenance {
			handledCount++
		}
	}
	completionRate := 0.0
	if len(effective) > 0 {
		completionRate = float64(handledCount) / float64(len(effective)) * 100
	}

	respondSuccess(w, map[string]interface{}{
		"total_alerts":     total,
		"effective_alerts": effective,
		"effective_count":  len(effective),
		"handled_count":    handledCount,
		"completion_rate":  completionRate,
	})
}

// getN9EClient 根据连接ID或默认连接创建客户端
func getN9EClient(connIDStr string) (*n9e.Client, error) {
	var url, password string

	if connIDStr != "" {
		id, err := strconv.Atoi(connIDStr)
		if err != nil {
			return nil, fmt.Errorf("无效的连接ID")
		}
		_, url, _, password, _, err = GetConnectionByID(id)
		if err != nil {
			return nil, fmt.Errorf("N9E 连接不存在")
		}
	} else {
		var err error
		url, _, password, _, err = GetDefaultConnection("n9e")
		if err != nil {
			return nil, fmt.Errorf("未配置 N9E 连接，请先在设置中添加")
		}
	}

	return n9e.NewClientFromParams(url, password), nil
}

// loadMaintenanceWindows 从数据库加载维护窗口
func loadMaintenanceWindows() []maintenanceWindow {
	rows, err := database.DB.Query(`SELECT project, maintenance_type, start_time, end_time FROM maintenance_windows`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var windows []maintenanceWindow
	for rows.Next() {
		var project, mType, startStr, endStr string
		if err := rows.Scan(&project, &mType, &startStr, &endStr); err != nil {
			continue
		}
		start, err1 := time.Parse("2006-01-02 15:04:05", startStr)
		end, err2 := time.Parse("2006-01-02 15:04:05", endStr)
		if err1 != nil || err2 != nil {
			// 尝试其他格式
			start, err1 = time.Parse("2006-01-02T15:04:05", startStr)
			end, err2 = time.Parse("2006-01-02T15:04:05", endStr)
			if err1 != nil || err2 != nil {
				continue
			}
		}
		windows = append(windows, maintenanceWindow{
			Project: project,
			Type:    mType,
			Start:   start,
			End:     end,
		})
	}
	return windows
}

// processAlerts 处理原始告警事件，按事件去重 + 维护窗口合并
func processAlerts(events []n9e.AlertEvent, windows []maintenanceWindow) []effectiveAlert {
	// 按 rule_name + hash 分组，每个独立的触发-恢复周期算一条事件
	type eventKey struct {
		RuleName string
		Hash     string
	}

	// 先按 hash 去重（同一个 hash 代表同一个告警事件的多次通知）
	hashMap := make(map[string]*n9e.AlertEvent)
	hashCount := make(map[string]int)
	for i := range events {
		e := &events[i]
		if existing, ok := hashMap[e.Hash]; ok {
			hashCount[e.Hash]++
			// 保留最早的触发时间和最晚的恢复时间
			if e.TriggerTime < existing.TriggerTime {
				existing.TriggerTime = e.TriggerTime
			}
			if e.RecoverTime > existing.RecoverTime {
				existing.RecoverTime = e.RecoverTime
				existing.IsRecovered = e.IsRecovered
			}
		} else {
			hashMap[e.Hash] = e
			hashCount[e.Hash] = 1
		}
	}

	// 检查每个去重后的事件是否在维护窗口内
	// 维护窗口内同一项目的所有告警合并为1条
	type mwKey struct {
		Project string
		MWStart int64
		MWEnd   int64
	}
	mwAlerts := make(map[mwKey]*effectiveAlert)
	var nonMWAlerts []effectiveAlert

	for hash, event := range hashMap {
		triggerT := time.Unix(event.TriggerTime, 0)

		// 检查是否在任何维护窗口内
		var matchedMW *maintenanceWindow
		for i := range windows {
			mw := &windows[i]
			if triggerT.After(mw.Start) && triggerT.Before(mw.End) {
				matchedMW = mw
				break
			}
		}

		if matchedMW != nil {
			key := mwKey{
				Project: matchedMW.Project,
				MWStart: matchedMW.Start.Unix(),
				MWEnd:   matchedMW.End.Unix(),
			}
			if existing, ok := mwAlerts[key]; ok {
				existing.RawCount += hashCount[hash]
				// 在规则名中追加
				if !strings.Contains(existing.RuleName, event.RuleName) {
					existing.RuleName += ", " + event.RuleName
				}
			} else {
				mwAlerts[key] = &effectiveAlert{
					RuleName:        fmt.Sprintf("%s维护 (%s)", matchedMW.Project, event.RuleName),
					Severity:        event.Severity,
					TriggerTime:     matchedMW.Start.Unix(),
					RecoverTime:     matchedMW.End.Unix(),
					IsRecovered:     1,
					GroupName:       event.GroupName,
					Tags:            event.Tags,
					RawCount:        hashCount[hash],
					Impact:          "无影响",
					Handler:         "",
					Status:          "已处理",
					Note:            matchedMW.Type,
					IsMaintenance:   true,
					MaintenanceType: matchedMW.Type,
				}
			}
		} else {
			nonMWAlerts = append(nonMWAlerts, effectiveAlert{
				RuleName:    event.RuleName,
				Severity:    event.Severity,
				TriggerTime: event.TriggerTime,
				RecoverTime: event.RecoverTime,
				IsRecovered: event.IsRecovered,
				GroupName:   event.GroupName,
				Tags:        event.Tags,
				RawCount:    hashCount[hash],
				Impact:      "",
				Handler:     "",
				Status:      "",
				Note:        "",
			})
		}
	}

	// 合并结果
	var result []effectiveAlert
	for _, a := range mwAlerts {
		result = append(result, *a)
	}
	result = append(result, nonMWAlerts...)

	// 按触发时间排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].TriggerTime > result[j].TriggerTime
	})

	return result
}

// HandleN9ETestConnection 测试 N9E 连接
func HandleN9ETestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       int    `json:"id"`
		URL      string `json:"url"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	// 如果密码是掩码且有ID，从数据库读取真实密码
	if req.Password == "********" && req.ID > 0 {
		database.DB.QueryRow(`SELECT password FROM service_connections WHERE id = ?`, req.ID).Scan(&req.Password)
	}

	if req.URL == "" {
		respondBadRequest(w, "URL 不能为空")
		return
	}

	client := n9e.NewClientFromParams(req.URL, req.Password)
	if err := client.TestConnection(); err != nil {
		respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
		return
	}

	respondSuccess(w, map[string]interface{}{
		"message": "连接成功",
		"user":    "n9e-api",
	})
}

// HandleN9EBusiGroups 获取 N9E 业务组列表（用于前端选择项目）
func HandleN9EBusiGroups(w http.ResponseWriter, r *http.Request) {
	connIDStr := r.URL.Query().Get("conn_id")

	client, err := getN9EClient(connIDStr)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	data, status, err := client.Get("/api/n9e/busi-groups?all=true&limit=500")
	if err != nil {
		respondError(w, http.StatusBadGateway, "获取业务组失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("N9E API 错误 (HTTP %d)", status))
		return
	}

	var resp struct {
		Dat []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"dat"`
		Err string `json:"err"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		// 直接返回原始数据
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	var groups []map[string]interface{}
	for _, g := range resp.Dat {
		groups = append(groups, map[string]interface{}{
			"id":   g.ID,
			"name": g.Name,
		})
	}
	if groups == nil {
		groups = []map[string]interface{}{}
	}
	respondSuccess(w, groups)
}

// HandleN9EMarkMaintenance 标记选中的告警为维护（事后补填维护窗口）
func HandleN9EMarkMaintenance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		Type    string `json:"maintenance_type"`
		Start   string `json:"start_time"`
		End     string `json:"end_time"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if req.Project == "" || req.Start == "" || req.End == "" {
		respondBadRequest(w, "项目名、开始时间和结束时间不能为空")
		return
	}
	if req.Type == "" {
		req.Type = "紧急维护"
	}

	// 创建维护窗口
	result, err := database.DB.Exec(
		`INSERT INTO maintenance_windows (project, maintenance_type, start_time, end_time, note) VALUES (?, ?, ?, ?, ?)`,
		req.Project, req.Type, req.Start, req.End, req.Note,
	)
	if err != nil {
		respondInternalError(w, "创建维护窗口失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()
	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "标记维护窗口", "maintenance_window", fmt.Sprintf("%d", id),
		fmt.Sprintf("%s %s %s~%s", req.Project, req.Type, req.Start, req.End), GetClientIP(r))

	respondSuccess(w, map[string]interface{}{
		"id":      id,
		"message": "维护窗口已创建，告警将自动合并",
	})
}

// ============ N9E 连接管理辅助 ============

// NewN9EClientByConnID 根据连接ID创建 N9E 客户端
func NewN9EClientByConnID(connID string) (*n9e.Client, error) {
	id, err := strconv.Atoi(connID)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("无效的连接ID")
	}
	_, url, _, password, _, err := GetConnectionByID(id)
	if err != nil {
		return nil, fmt.Errorf("连接不存在")
	}
	return n9e.NewClientFromParams(url, password), nil
}

// GetN9EConnections 获取所有 N9E 连接（供前端选择）
func HandleListN9EConnections(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, name, url, is_default FROM service_connections WHERE type = 'n9e' AND status = 'active' ORDER BY is_default DESC, id ASC`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var conns []map[string]interface{}
	for rows.Next() {
		var id int
		var name, url string
		var isDefault bool
		if err := rows.Scan(&id, &name, &url, &isDefault); err != nil {
			continue
		}
		conns = append(conns, map[string]interface{}{
			"id":         id,
			"name":       name,
			"url":        url,
			"is_default": isDefault,
		})
	}
	if conns == nil {
		conns = []map[string]interface{}{}
	}
	respondSuccess(w, conns)
}

// HandleN9EAlertEvents 代理 N9E 历史告警分页请求（前端分页调用，避免超时）
func HandleN9EAlertEvents(w http.ResponseWriter, r *http.Request) {
	connIDStr := r.URL.Query().Get("conn_id")
	stime := r.URL.Query().Get("stime")
	etime := r.URL.Query().Get("etime")
	limit := r.URL.Query().Get("limit")
	page := r.URL.Query().Get("p")
	bgid := r.URL.Query().Get("bgid")
	severity := r.URL.Query().Get("severity")

	if stime == "" || etime == "" {
		respondBadRequest(w, "stime 和 etime 不能为空")
		return
	}
	if limit == "" {
		limit = "500"
	}
	if page == "" {
		page = "1"
	}

	client, err := getN9EClient(connIDStr)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}

	path := fmt.Sprintf("/api/n9e/alert-his-events/list?stime=%s&etime=%s&limit=%s&p=%s", stime, etime, limit, page)
	if bgid != "" {
		path += "&bgid=" + bgid
	}
	if severity != "" && !strings.Contains(severity, ",") {
		// n9e severity 只支持单个值
		path += "&severity=" + severity
	}
	data, status, err := client.Get(path)
	if err != nil {
		respondError(w, http.StatusBadGateway, "请求N9E失败: "+err.Error())
		return
	}
	if status != 200 {
		respondError(w, http.StatusBadGateway, fmt.Sprintf("N9E API 错误 (HTTP %d)", status))
		return
	}

	var resp n9e.AlertHisEventsResp
	if err := json.Unmarshal(data, &resp); err != nil {
		respondError(w, http.StatusBadGateway, "响应解析失败")
		return
	}

	respondSuccess(w, map[string]interface{}{
		"list":  resp.Dat.List,
		"total": resp.Dat.Total,
	})
}

// HandleN9EProcessAlerts 处理前端传来的原始告警事件（去重 + 维护窗口合并）
func HandleN9EProcessAlerts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []n9e.AlertEvent `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	windows := loadMaintenanceWindows()
	effective := processAlerts(req.Events, windows)

	var handledCount int
	for _, a := range effective {
		if a.Status == "已处理" || a.IsMaintenance {
			handledCount++
		}
	}
	completionRate := 0.0
	if len(effective) > 0 {
		completionRate = float64(handledCount) / float64(len(effective)) * 100
	}

	respondSuccess(w, map[string]interface{}{
		"effective_alerts": effective,
		"effective_count":  len(effective),
		"handled_count":    handledCount,
		"completion_rate":  completionRate,
	})
}

// init 确保 sql 包被使用
var _ = sql.ErrNoRows
