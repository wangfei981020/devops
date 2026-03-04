package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"opsplatform/database"
	"opsplatform/models"
	"opsplatform/storage"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// formatDateTimeForDB 将各种日期格式转换为MySQL格式
func formatDateTimeForDB(dateStr string) string {
	if dateStr == "" {
		return ""
	}
	// 处理 ISO 格式 2026-02-25T17:36:00Z 或 2026-02-25T17:36:00
	dateStr = strings.Replace(dateStr, "T", " ", 1)
	dateStr = strings.TrimSuffix(dateStr, "Z")
	// 如果只有日期部分，返回原样
	if len(dateStr) == 10 {
		return dateStr
	}
	// 截取到秒
	if len(dateStr) > 19 {
		dateStr = dateStr[:19]
	}
	return dateStr
}

// calculateOverdue 自动计算逾期状态
// 逻辑：如果有计划修复时间，当前时间超过该时间，且状态不是"已解决"或"检测正常"，则为逾期
func calculateOverdue(plannedFixTime string, status string) bool {
	// 如果没有计划修复时间，不算逾期
	if plannedFixTime == "" {
		return false
	}
	// 如果状态是已解决或检测正常，不算逾期
	if status == "resolved" || status == "normal" {
		return false
	}
	// 解析计划修复时间
	plannedTime, err := parseDateTime(plannedFixTime)
	if err != nil {
		return false
	}
	// 当前时间超过计划修复时间，则为逾期
	return time.Now().After(plannedTime)
}

// parseDateTime 解析各种格式的日期时间字符串
func parseDateTime(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}
	// 处理 ISO 格式
	dateStr = strings.Replace(dateStr, "T", " ", 1)
	dateStr = strings.TrimSuffix(dateStr, "Z")
	
	// 尝试各种格式
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.ParseInLocation(format, dateStr, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %s", dateStr)
}

func requireDutyPermission(w http.ResponseWriter, r *http.Request, permissionCode string) bool {
	_, username, role := GetUserFromContext(r)
	ok, err := UserHasPermission(username, role, permissionCode)
	if err != nil {
		log.Printf("[值班权限] 检查失败 username=%s code=%s err=%v", username, permissionCode, err)
		sendError(w, "权限检查失败", http.StatusInternalServerError)
		return false
	}
	if !ok {
		sendError(w, "权限不足", http.StatusForbidden)
		return false
	}
	return true
}

func requireAnyDutyPermission(w http.ResponseWriter, r *http.Request, permissionCodes ...string) bool {
	_, username, role := GetUserFromContext(r)
	for _, code := range permissionCodes {
		ok, err := UserHasPermission(username, role, code)
		if err != nil {
			log.Printf("[值班权限] 检查失败 username=%s code=%s err=%v", username, code, err)
			sendError(w, "权限检查失败", http.StatusInternalServerError)
			return false
		}
		if ok {
			return true
		}
	}
	sendError(w, "权限不足", http.StatusForbidden)
	return false
}

// ========== 值班项目配置 ==========

// HandleGetDutyProjects 获取值班项目列表
func HandleGetDutyProjects(w http.ResponseWriter, r *http.Request) {
	if !requireAnyDutyPermission(w, r, "menu:duty_projects", "menu:duty") {
		return
	}
	status := r.URL.Query().Get("status")

	query := `SELECT id, name, code, COALESCE(description,''), status, sort_order, 
		created_at, COALESCE(created_by,''), COALESCE(updated_at,'')
		FROM duty_projects WHERE 1=1`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY sort_order ASC, created_at DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[值班项目] 查询失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := make([]models.DutyProject, 0)
	for rows.Next() {
		var p models.DutyProject
		err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.Description, &p.Status, &p.SortOrder,
			&p.CreatedAt, &p.CreatedBy, &p.UpdatedAt)
		if err != nil {
			log.Printf("扫描值班项目失败: %v", err)
			continue
		}
		projects = append(projects, p)
	}

	respondJSON(w, http.StatusOK, projects)
}

// HandleCreateDutyProject 创建值班项目
func HandleCreateDutyProject(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty_project:create") {
		return
	}
	var p models.DutyProject
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if p.Name == "" || p.Code == "" {
		sendError(w, "项目名称和代码为必填项", http.StatusBadRequest)
		return
	}

	p.ID = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	user := r.Header.Get("X-Operator")

	if p.Status == "" {
		p.Status = "active"
	}

	_, err := database.DB.Exec(`INSERT INTO duty_projects (id, name, code, description, status, sort_order, created_at, created_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Code, p.Description, p.Status, p.SortOrder, now, user, now)
	if err != nil {
		log.Printf("[值班项目] 创建失败: %v", err)
		sendError(w, "创建失败，代码可能已存在", http.StatusInternalServerError)
		return
	}

	p.CreatedAt = now
	p.CreatedBy = user
	respondJSON(w, http.StatusCreated, p)
}

// HandleUpdateDutyProject 更新值班项目
func HandleUpdateDutyProject(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty_project:update") {
		return
	}
	vars := mux.Vars(r)
	id := vars["id"]

	var p models.DutyProject
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	_, err := database.DB.Exec(`UPDATE duty_projects SET name=?, code=?, description=?, status=?, sort_order=?, updated_at=? WHERE id=?`,
		p.Name, p.Code, p.Description, p.Status, p.SortOrder, now, id)
	if err != nil {
		log.Printf("[值班项目] 更新失败: %v", err)
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleDeleteDutyProject 删除值班项目
func HandleDeleteDutyProject(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty_project:delete") {
		return
	}
	vars := mux.Vars(r)
	id := vars["id"]

	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE project_id = ?", id).Scan(&count)
	if count > 0 {
		sendError(w, "该项目下存在值班记录，无法删除", http.StatusBadRequest)
		return
	}

	_, err := database.DB.Exec("DELETE FROM duty_projects WHERE id = ?", id)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// ========== 值班记录 ==========

// HandleGetDutyRecords 获取值班记录列表
func HandleGetDutyRecords(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "menu:duty") {
		return
	}
	status := r.URL.Query().Get("status")
	projectID := r.URL.Query().Get("project_id")
	handler := r.URL.Query().Get("handler")
	dutyPerson := r.URL.Query().Get("duty_person")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	isOverdue := r.URL.Query().Get("is_overdue")
	eventType := r.URL.Query().Get("event_type")
	responseTimeMin := r.URL.Query().Get("response_time_min")
	responseTimeMax := r.URL.Query().Get("response_time_max")

	query := `SELECT dr.id, dr.duty_date, dr.duty_person, dr.project_id, COALESCE(dp.name,'') as project_name,
		COALESCE(dr.task_desc,''), dr.feedback_type, COALESCE(dr.event_type,'customer_feedback'), COALESCE(dr.handler,''), COALESCE(dr.handle_result,''), COALESCE(dr.solution,''), COALESCE(dr.problem_desc,''),
		dr.first_call_time, dr.answer_time, dr.call_count, dr.is_answered, dr.response_time,
		dr.is_escalated, COALESCE(dr.escalate_to,''),
		dr.has_handover, COALESCE(dr.handover_person,''), COALESCE(dr.handover_content,''),
		dr.status, dr.planned_fix_time, COALESCE(dr.planned_fix_time_edited, 0),
		CASE
			WHEN dr.planned_fix_time IS NOT NULL
				AND CAST(dr.planned_fix_time AS CHAR) != ''
				AND dr.planned_fix_time < NOW()
				AND dr.status NOT IN ('resolved', 'normal')
			THEN 1 ELSE 0
		END as is_overdue,
		COALESCE(dr.overdue_reason,''),
		COALESCE(dr.attachments,'[]'),
		dr.created_at, COALESCE(dr.created_by,''), COALESCE(dr.updated_at,''), COALESCE(dr.updated_by,'')
		FROM duty_records dr
		LEFT JOIN duty_projects dp ON dr.project_id = dp.id
		WHERE 1=1`
	args := []interface{}{}

	if status != "" {
		query += " AND dr.status = ?"
		args = append(args, status)
	}
	if projectID != "" {
		query += " AND dr.project_id = ?"
		args = append(args, projectID)
	}
	if handler != "" {
		query += " AND dr.handler LIKE ?"
		args = append(args, "%"+handler+"%")
	}
	if dutyPerson != "" {
		query += " AND dr.duty_person LIKE ?"
		args = append(args, "%"+dutyPerson+"%")
	}
	if startDate != "" {
		query += " AND dr.duty_date >= ?"
		args = append(args, startDate)
	}
	if endDate != "" {
		query += " AND dr.duty_date <= ?"
		args = append(args, endDate)
	}
	if isOverdue == "1" || isOverdue == "true" {
		query += " AND dr.planned_fix_time IS NOT NULL AND CAST(dr.planned_fix_time AS CHAR) != '' AND dr.planned_fix_time < NOW() AND dr.status NOT IN ('resolved', 'normal')"
	}
	if eventType != "" {
		query += " AND dr.event_type = ?"
		args = append(args, eventType)
	}
	if responseTimeMin != "" {
		query += " AND dr.response_time >= ?"
		args = append(args, responseTimeMin)
	}
	if responseTimeMax != "" {
		query += " AND dr.response_time <= ?"
		args = append(args, responseTimeMax)
	}

	query += " ORDER BY dr.duty_date DESC, dr.created_at DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[值班记录] 查询失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	records := make([]models.DutyRecord, 0)
	for rows.Next() {
		var rec models.DutyRecord
		var firstCallTime, answerTime, plannedFixTime sql.NullString
		var attachmentsJSON string

		err := rows.Scan(&rec.ID, &rec.DutyDate, &rec.DutyPerson, &rec.ProjectID, &rec.ProjectName,
			&rec.TaskDesc, &rec.FeedbackType, &rec.EventType, &rec.Handler, &rec.HandleResult, &rec.Solution, &rec.ProblemDesc,
			&firstCallTime, &answerTime, &rec.CallCount, &rec.IsAnswered, &rec.ResponseTime,
			&rec.IsEscalated, &rec.EscalateTo,
			&rec.HasHandover, &rec.HandoverPerson, &rec.HandoverContent,
			&rec.Status, &plannedFixTime, &rec.PlannedFixTimeEdited, &rec.IsOverdue, &rec.OverdueReason,
			&attachmentsJSON,
			&rec.CreatedAt, &rec.CreatedBy, &rec.UpdatedAt, &rec.UpdatedBy)
		if err != nil {
			log.Printf("扫描值班记录失败: %v", err)
			continue
		}

		if firstCallTime.Valid {
			rec.FirstCallTime = firstCallTime.String
		}
		if answerTime.Valid {
			rec.AnswerTime = answerTime.String
		}
		if plannedFixTime.Valid {
			rec.PlannedFixTime = plannedFixTime.String
		}

		json.Unmarshal([]byte(attachmentsJSON), &rec.Attachments)
		if rec.Attachments == nil {
			rec.Attachments = []string{}
		}

		records = append(records, rec)
	}

	respondJSON(w, http.StatusOK, records)
}

// HandleGetDutyRecord 获取单个值班记录
func HandleGetDutyRecord(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "menu:duty") {
		return
	}
	vars := mux.Vars(r)
	id := vars["id"]

	var rec models.DutyRecord
	var firstCallTime, answerTime, plannedFixTime sql.NullString
	var attachmentsJSON string

	err := database.DB.QueryRow(`SELECT dr.id, dr.duty_date, dr.duty_person, dr.project_id, COALESCE(dp.name,'') as project_name,
		COALESCE(dr.task_desc,''), dr.feedback_type, COALESCE(dr.event_type,'customer_feedback'), COALESCE(dr.handler,''), COALESCE(dr.handle_result,''), COALESCE(dr.solution,''), COALESCE(dr.problem_desc,''),
		dr.first_call_time, dr.answer_time, dr.call_count, dr.is_answered, dr.response_time,
		dr.is_escalated, COALESCE(dr.escalate_to,''),
		dr.has_handover, COALESCE(dr.handover_person,''), COALESCE(dr.handover_content,''),
		dr.status, dr.planned_fix_time, COALESCE(dr.planned_fix_time_edited, 0),
		CASE
			WHEN dr.planned_fix_time IS NOT NULL
				AND CAST(dr.planned_fix_time AS CHAR) != ''
				AND dr.planned_fix_time < NOW()
				AND dr.status NOT IN ('resolved', 'normal')
			THEN 1 ELSE 0
		END as is_overdue,
		COALESCE(dr.overdue_reason,''),
		COALESCE(dr.attachments,'[]'),
		dr.created_at, COALESCE(dr.created_by,''), COALESCE(dr.updated_at,''), COALESCE(dr.updated_by,'')
		FROM duty_records dr
		LEFT JOIN duty_projects dp ON dr.project_id = dp.id
		WHERE dr.id = ?`, id).Scan(&rec.ID, &rec.DutyDate, &rec.DutyPerson, &rec.ProjectID, &rec.ProjectName,
		&rec.TaskDesc, &rec.FeedbackType, &rec.EventType, &rec.Handler, &rec.HandleResult, &rec.Solution, &rec.ProblemDesc,
		&firstCallTime, &answerTime, &rec.CallCount, &rec.IsAnswered, &rec.ResponseTime,
		&rec.IsEscalated, &rec.EscalateTo,
		&rec.HasHandover, &rec.HandoverPerson, &rec.HandoverContent,
		&rec.Status, &plannedFixTime, &rec.PlannedFixTimeEdited, &rec.IsOverdue, &rec.OverdueReason,
		&attachmentsJSON,
		&rec.CreatedAt, &rec.CreatedBy, &rec.UpdatedAt, &rec.UpdatedBy)

	if err != nil {
		if err == sql.ErrNoRows {
			sendError(w, "记录不存在", http.StatusNotFound)
		} else {
			log.Printf("[值班记录] 查询失败: %v", err)
			sendError(w, "查询失败", http.StatusInternalServerError)
		}
		return
	}

	if firstCallTime.Valid {
		rec.FirstCallTime = firstCallTime.String
	}
	if answerTime.Valid {
		rec.AnswerTime = answerTime.String
	}
	if plannedFixTime.Valid {
		rec.PlannedFixTime = plannedFixTime.String
	}

	json.Unmarshal([]byte(attachmentsJSON), &rec.Attachments)
	if rec.Attachments == nil {
		rec.Attachments = []string{}
	}

	respondJSON(w, http.StatusOK, rec)
}

// HandleCreateDutyRecord 创建值班记录
func HandleCreateDutyRecord(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty:create") {
		return
	}
	var rec models.DutyRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		fmt.Printf("[DEBUG] HandleCreateDutyRecord JSON解码失败: %v\n", err)
		sendError(w, "请求参数无效: "+err.Error(), http.StatusBadRequest)
		return
	}

	if rec.DutyDate == "" || rec.DutyPerson == "" || rec.ProjectID == "" {
		sendError(w, "值班日期、值班人、项目为必填项", http.StatusBadRequest)
		return
	}

	rec.ID = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	user := r.Header.Get("X-Operator")

	if rec.Status == "" {
		rec.Status = "pending"
	}
	if rec.FeedbackType == "" {
		rec.FeedbackType = "customer"
	}

	attachmentsJSON, _ := json.Marshal(rec.Attachments)
	if rec.Attachments == nil {
		attachmentsJSON = []byte("[]")
	}

	// 格式化日期字段
	dutyDate := formatDateTimeForDB(rec.DutyDate)
	var firstCallTime, answerTime, plannedFixTime interface{}
	plannedFixTimeEdited := 0
	if rec.FirstCallTime != "" {
		firstCallTime = formatDateTimeForDB(rec.FirstCallTime)
	}
	if rec.AnswerTime != "" {
		answerTime = formatDateTimeForDB(rec.AnswerTime)
	}
	// 状态是"检测正常"或"已解决"时，计划修复时间应为空
	if rec.Status == "normal" || rec.Status == "resolved" {
		plannedFixTime = nil
		rec.PlannedFixTime = ""
	} else if rec.PlannedFixTime != "" {
		plannedFixTime = formatDateTimeForDB(rec.PlannedFixTime)
		plannedFixTimeEdited = 1
	} else {
		// 如果没有设置计划修复时间，默认为当天 23:59:59
		today := time.Now().Format("2006-01-02")
		plannedFixTime = today + " 23:59:59"
		rec.PlannedFixTime = today + " 23:59:59"
	}

	// 自动计算逾期状态：如果有计划修复时间，当前时间超过该时间，且状态不是已解决/检测正常，则为逾期
	isOverdue := calculateOverdue(rec.PlannedFixTime, rec.Status)

	_, err := database.DB.Exec(`INSERT INTO duty_records (
		id, duty_date, duty_person, project_id, task_desc, feedback_type, event_type, handler, handle_result, solution, problem_desc,
		first_call_time, answer_time, call_count, is_answered, response_time,
		is_escalated, escalate_to, has_handover, handover_person, handover_content,
		status, planned_fix_time, planned_fix_time_edited, is_overdue, overdue_reason, attachments,
		created_at, created_by, updated_at, updated_by
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, dutyDate, rec.DutyPerson, rec.ProjectID, rec.TaskDesc, rec.FeedbackType, rec.EventType, rec.Handler, rec.HandleResult, rec.Solution, rec.ProblemDesc,
		firstCallTime, answerTime, rec.CallCount, rec.IsAnswered, rec.ResponseTime,
		rec.IsEscalated, rec.EscalateTo, rec.HasHandover, rec.HandoverPerson, rec.HandoverContent,
		rec.Status, plannedFixTime, plannedFixTimeEdited, isOverdue, rec.OverdueReason, string(attachmentsJSON),
		now, user, now, user)

	if err != nil {
		log.Printf("[值班记录] 创建失败: %v", err)
		sendError(w, "创建失败", http.StatusInternalServerError)
		return
	}

	rec.CreatedAt = now
	rec.CreatedBy = user

	// 查询项目名称用于审计日志
	var projectName string
	database.DB.QueryRow("SELECT name FROM duty_projects WHERE id = ?", rec.ProjectID).Scan(&projectName)
	if projectName == "" {
		projectName = rec.ProjectID
	}

	// 记录审计日志 - 记录完整的新增数据
	newDataJSON, _ := json.Marshal(rec)
	AddAuditLogFromRequest(r, "CREATE_DUTY_RECORD", "duty:"+rec.ID, user, "", string(newDataJSON),
		fmt.Sprintf("创建值班记录: 日期=%s, 项目=%s, 值班人=%s, 状态=%s", rec.DutyDate, projectName, rec.DutyPerson, rec.Status))

	respondJSON(w, http.StatusCreated, rec)
}

// HandleUpdateDutyRecord 更新值班记录
func HandleUpdateDutyRecord(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty:update") {
		return
	}
	vars := mux.Vars(r)
	id := vars["id"]

	var rec models.DutyRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	user := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")

	attachmentsJSON, _ := json.Marshal(rec.Attachments)
	if rec.Attachments == nil {
		attachmentsJSON = []byte("[]")
	}

	// 格式化日期字段
	dutyDate := formatDateTimeForDB(rec.DutyDate)
	var firstCallTime, answerTime, plannedFixTime interface{}
	if rec.FirstCallTime != "" {
		firstCallTime = formatDateTimeForDB(rec.FirstCallTime)
	}
	if rec.AnswerTime != "" {
		answerTime = formatDateTimeForDB(rec.AnswerTime)
	}
	// 状态是"检测正常"或"已解决"时，计划修复时间应为空
	if rec.Status == "normal" || rec.Status == "resolved" {
		plannedFixTime = nil
		rec.PlannedFixTime = ""
	} else if rec.PlannedFixTime != "" {
		plannedFixTime = formatDateTimeForDB(rec.PlannedFixTime)
	}

	// 查询旧记录：计划修复时间、是否已编辑过、状态
	var oldPlannedFixTime sql.NullString
	var oldPlannedFixTimeEdited int
	var oldStatus string
	err := database.DB.QueryRow("SELECT planned_fix_time, COALESCE(planned_fix_time_edited, 0), status FROM duty_records WHERE id = ?", id).Scan(&oldPlannedFixTime, &oldPlannedFixTimeEdited, &oldStatus)
	if err != nil {
		sendError(w, "记录不存在", http.StatusNotFound)
		return
	}

	// 判断计划修复时间是否变化
	oldVal := ""
	if oldPlannedFixTime.Valid {
		oldVal = formatDateTimeForDB(oldPlannedFixTime.String)
	}
	newVal := ""
	if rec.PlannedFixTime != "" {
		newVal = formatDateTimeForDB(rec.PlannedFixTime)
	}
	plannedFixTimeChanged := oldVal != newVal
	plannedFixTimeEdited := oldPlannedFixTimeEdited

	// 权限判断：
	// 1. 如果计划修复时间没有变化，不需要额外权限
	// 2. 如果计划修复时间已经被编辑过(planned_fix_time_edited=1)，需要 duty:edit_planned_fix_time 权限
	// 3. 如果计划修复时间未被编辑过，且状态不是"检测正常"或"已解决"，允许任何人首次编辑
	if plannedFixTimeChanged {
		if oldPlannedFixTimeEdited == 1 {
			// 已经被编辑过，需要权限
			if !requireDutyPermission(w, r, "duty:edit_planned_fix_time") {
				return
			}
		} else {
			// 首次编辑：状态不是 normal/resolved 时允许
			if oldStatus == "normal" || oldStatus == "resolved" {
				// 状态已完成，需要权限
				if !requireDutyPermission(w, r, "duty:edit_planned_fix_time") {
					return
				}
			}
			// 首次编辑成功，标记为已编辑
			plannedFixTimeEdited = 1
		}
	}

	// 自动计算逾期状态：如果有计划修复时间，当前时间超过该时间，且状态不是已解决/检测正常，则为逾期
	isOverdue := calculateOverdue(rec.PlannedFixTime, rec.Status)

	_, err = database.DB.Exec(`UPDATE duty_records SET
		duty_date=?, duty_person=?, project_id=?, task_desc=?, feedback_type=?, event_type=?, handler=?, handle_result=?, solution=?, problem_desc=?,
		first_call_time=?, answer_time=?, call_count=?, is_answered=?, response_time=?,
		is_escalated=?, escalate_to=?, has_handover=?, handover_person=?, handover_content=?,
		status=?, planned_fix_time=?, planned_fix_time_edited=?, is_overdue=?, overdue_reason=?, attachments=?,
		updated_at=?, updated_by=?
		WHERE id=?`,
		dutyDate, rec.DutyPerson, rec.ProjectID, rec.TaskDesc, rec.FeedbackType, rec.EventType, rec.Handler, rec.HandleResult, rec.Solution, rec.ProblemDesc,
		firstCallTime, answerTime, rec.CallCount, rec.IsAnswered, rec.ResponseTime,
		rec.IsEscalated, rec.EscalateTo, rec.HasHandover, rec.HandoverPerson, rec.HandoverContent,
		rec.Status, plannedFixTime, plannedFixTimeEdited, isOverdue, rec.OverdueReason, string(attachmentsJSON),
		now, user, id)

	if err != nil {
		log.Printf("[值班记录] 更新失败: %v", err)
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	// 查询项目名称用于审计日志
	var projectName string
	database.DB.QueryRow("SELECT name FROM duty_projects WHERE id = ?", rec.ProjectID).Scan(&projectName)
	if projectName == "" {
		projectName = rec.ProjectID
	}

	// 记录审计日志 - 记录更新后的数据
	newDataJSON, _ := json.Marshal(rec)
	AddAuditLogFromRequest(r, "UPDATE_DUTY_RECORD", "duty:"+id, user, "", string(newDataJSON),
		fmt.Sprintf("更新值班记录: 日期=%s, 项目=%s, 值班人=%s, 状态=%s", rec.DutyDate, projectName, rec.DutyPerson, rec.Status))

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleUpdateDutyPlannedFixTime 单独更新计划修复时间
// 权限规则：首次编辑不需要权限，之后需要 duty:edit_planned_fix_time 权限
func HandleUpdateDutyPlannedFixTime(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		PlannedFixTime string `json:"planned_fix_time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	// 读取当前状态和编辑标记
	var status string
	var plannedFixTimeEdited int
	err := database.DB.QueryRow("SELECT COALESCE(status,'pending'), COALESCE(planned_fix_time_edited, 0) FROM duty_records WHERE id = ?", id).Scan(&status, &plannedFixTimeEdited)
	if err != nil {
		if err == sql.ErrNoRows {
			sendError(w, "记录不存在", http.StatusNotFound)
		} else {
			log.Printf("[值班记录] 读取状态失败: %v", err)
			sendError(w, "查询失败", http.StatusInternalServerError)
		}
		return
	}

	// 权限判断：
	// 1. 如果已经被编辑过，需要权限
	// 2. 如果状态是 normal/resolved，需要权限
	// 3. 否则允许首次编辑
	if plannedFixTimeEdited == 1 {
		if !requireDutyPermission(w, r, "duty:edit_planned_fix_time") {
			return
		}
	} else if status == "normal" || status == "resolved" {
		if !requireDutyPermission(w, r, "duty:edit_planned_fix_time") {
			return
		}
	}
	// 首次编辑，标记为已编辑
	newPlannedFixTimeEdited := 1

	var plannedFixTime interface{}
	if req.PlannedFixTime != "" {
		plannedFixTime = formatDateTimeForDB(req.PlannedFixTime)
	}
	isOverdue := calculateOverdue(req.PlannedFixTime, status)

	user := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err = database.DB.Exec(`
		UPDATE duty_records
		SET planned_fix_time=?, planned_fix_time_edited=?, is_overdue=?, updated_at=?, updated_by=?
		WHERE id=?
	`, plannedFixTime, newPlannedFixTimeEdited, isOverdue, now, user, id)
	if err != nil {
		log.Printf("[值班记录] 更新计划修复时间失败: %v", err)
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "计划修复时间更新成功"})
}

// HandleDeleteDutyRecord 删除值班记录
func HandleDeleteDutyRecord(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty:delete") {
		return
	}
	vars := mux.Vars(r)
	id := vars["id"]
	user := r.Header.Get("X-Operator")

	// 先查询被删除的记录，用于审计日志
	var dutyDate, dutyPerson, projectID, status string
	database.DB.QueryRow("SELECT duty_date, duty_person, project_id, status FROM duty_records WHERE id = ?", id).Scan(&dutyDate, &dutyPerson, &projectID, &status)

	// 查询项目名称
	var projectName string
	database.DB.QueryRow("SELECT name FROM duty_projects WHERE id = ?", projectID).Scan(&projectName)
	if projectName == "" {
		projectName = projectID
	}

	_, err := database.DB.Exec("DELETE FROM duty_records WHERE id = ?", id)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	// 记录审计日志 - 记录被删除的数据摘要
	oldData := fmt.Sprintf(`{"duty_date":"%s","duty_person":"%s","project":"%s","status":"%s"}`, dutyDate, dutyPerson, projectName, status)
	AddAuditLogFromRequest(r, "DELETE_DUTY_RECORD", "duty:"+id, user, oldData, "",
		fmt.Sprintf("删除值班记录: 日期=%s, 项目=%s, 值班人=%s", dutyDate, projectName, dutyPerson))

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// HandleBatchUpdateDutyStatus 批量修改处理结果
func HandleBatchUpdateDutyStatus(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty:update") {
		return
	}
	var req struct {
		IDs    []string `json:"ids"`
		Status string   `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "参数错误", http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 || req.Status == "" {
		sendError(w, "请选择记录和状态", http.StatusBadRequest)
		return
	}

	user := r.Context().Value("username")
	now := time.Now().Format("2006-01-02 15:04:05")

	placeholders := make([]string, len(req.IDs))
	args := make([]interface{}, len(req.IDs)+3)
	args[0] = req.Status
	args[1] = now
	args[2] = user
	for i, id := range req.IDs {
		placeholders[i] = "?"
		args[i+3] = id
	}

	query := fmt.Sprintf("UPDATE duty_records SET status=?, updated_at=?, updated_by=? WHERE id IN (%s)", strings.Join(placeholders, ","))
	result, err := database.DB.Exec(query, args...)
	if err != nil {
		log.Printf("[批量修改] 失败: %v", err)
		sendError(w, "批量修改失败", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	respondJSON(w, http.StatusOK, map[string]interface{}{"message": "批量修改成功", "affected": affected})
}

// HandleBatchDeleteDutyRecords 批量删除值班记录
func HandleBatchDeleteDutyRecords(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty:delete") {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "参数错误", http.StatusBadRequest)
		return
	}
	if len(req.IDs) == 0 {
		sendError(w, "请选择要删除的记录", http.StatusBadRequest)
		return
	}

	placeholders := make([]string, len(req.IDs))
	args := make([]interface{}, len(req.IDs))
	for i, id := range req.IDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM duty_records WHERE id IN (%s)", strings.Join(placeholders, ","))
	result, err := database.DB.Exec(query, args...)
	if err != nil {
		log.Printf("[批量删除] 失败: %v", err)
		sendError(w, "批量删除失败", http.StatusInternalServerError)
		return
	}

	affected, _ := result.RowsAffected()
	respondJSON(w, http.StatusOK, map[string]interface{}{"message": "批量删除成功", "affected": affected})
}

// HandleGetDutyStats 获取值班统计数据
func HandleGetDutyStats(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "menu:duty") {
		return
	}
	stats := map[string]interface{}{}

	var total, resolved, inProgress, pending, temporary, normal int
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records").Scan(&total)
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE status='resolved'").Scan(&resolved)
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE status='in_progress'").Scan(&inProgress)
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE status='pending'").Scan(&pending)
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE status='temporary'").Scan(&temporary)
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE status='normal'").Scan(&normal)

	stats["total"] = total
	stats["resolved"] = resolved
	stats["in_progress"] = inProgress
	stats["pending"] = pending
	stats["temporary"] = temporary
	stats["normal"] = normal

	var overdue int
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE planned_fix_time IS NOT NULL AND CAST(planned_fix_time AS CHAR) != '' AND planned_fix_time < NOW() AND status NOT IN ('resolved', 'normal')").Scan(&overdue)
	stats["overdue"] = overdue

	thisMonth := time.Now().Format("2006-01")
	var monthTotal int
	database.DB.QueryRow("SELECT COUNT(*) FROM duty_records WHERE duty_date LIKE ?", thisMonth+"%").Scan(&monthTotal)
	stats["this_month"] = monthTotal

	handlerStats := []map[string]interface{}{}
	rows, err := database.DB.Query(`
		SELECT handler, 
			COUNT(*) as total,
			SUM(CASE WHEN status='resolved' THEN 1 ELSE 0 END) as resolved,
			SUM(CASE WHEN status='in_progress' THEN 1 ELSE 0 END) as in_progress,
			SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status='temporary' THEN 1 ELSE 0 END) as temporary,
			SUM(CASE WHEN planned_fix_time IS NOT NULL AND CAST(planned_fix_time AS CHAR) != '' AND planned_fix_time < NOW() AND status NOT IN ('resolved', 'normal') THEN 1 ELSE 0 END) as overdue
		FROM duty_records
		WHERE handler != ''
		GROUP BY handler 
		ORDER BY total DESC`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var h string
			var t, r, ip, p, temp, o int
			rows.Scan(&h, &t, &r, &ip, &p, &temp, &o)
			handlerStats = append(handlerStats, map[string]interface{}{
				"handler":     h,
				"total":       t,
				"resolved":    r,
				"in_progress": ip,
				"pending":     p,
				"temporary":   temp,
				"overdue":     o,
			})
		}
	}
	stats["by_handler"] = handlerStats

	projectStats := []map[string]interface{}{}
	rows2, err := database.DB.Query(`
		SELECT COALESCE(dp.name, '未知项目') as project_name, 
			COUNT(*) as total,
			SUM(CASE WHEN dr.status='resolved' THEN 1 ELSE 0 END) as resolved,
			SUM(CASE WHEN dr.planned_fix_time IS NOT NULL AND CAST(dr.planned_fix_time AS CHAR) != '' AND dr.planned_fix_time < NOW() AND dr.status NOT IN ('resolved', 'normal') THEN 1 ELSE 0 END) as overdue
		FROM duty_records dr
		LEFT JOIN duty_projects dp ON dr.project_id = dp.id
		GROUP BY dr.project_id
		ORDER BY total DESC`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var pn string
			var t, r, o int
			rows2.Scan(&pn, &t, &r, &o)
			projectStats = append(projectStats, map[string]interface{}{
				"project":  pn,
				"total":    t,
				"resolved": r,
				"overdue":  o,
			})
		}
	}
	stats["by_project"] = projectStats

	respondJSON(w, http.StatusOK, stats)
}

// HandleGetDutyStatsDetail 获取详细统计数据（支持筛选）
func HandleGetDutyStatsDetail(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "menu:duty") {
		return
	}
	projectID := r.URL.Query().Get("project_id")
	handler := r.URL.Query().Get("handler")
	dutyPerson := r.URL.Query().Get("duty_person")
	eventType := r.URL.Query().Get("event_type")
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	whereClause := "WHERE 1=1"
	args := []interface{}{}

	if projectID != "" {
		whereClause += " AND dr.project_id = ?"
		args = append(args, projectID)
	}
	if handler != "" {
		whereClause += " AND dr.handler LIKE ?"
		args = append(args, "%"+handler+"%")
	}
	if dutyPerson != "" {
		whereClause += " AND dr.duty_person LIKE ?"
		args = append(args, "%"+dutyPerson+"%")
	}
	if eventType != "" {
		whereClause += " AND dr.event_type = ?"
		args = append(args, eventType)
	}
	if startDate != "" {
		whereClause += " AND dr.duty_date >= ?"
		args = append(args, startDate)
	}
	if endDate != "" {
		whereClause += " AND dr.duty_date <= ?"
		args = append(args, endDate+" 23:59:59")
	}

	result := map[string]interface{}{}

	// 总览数据
	overview := map[string]interface{}{}
	var total, resolved, inProgress, pending, temporary, normal, overdue int
	var avgResponse, minResponse, maxResponse sql.NullFloat64

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM duty_records dr %s", whereClause)
	database.DB.QueryRow(countQuery, args...).Scan(&total)

	statusQuery := fmt.Sprintf(`
		SELECT 
			SUM(CASE WHEN status='resolved' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='in_progress' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='temporary' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='normal' THEN 1 ELSE 0 END),
			SUM(CASE WHEN planned_fix_time IS NOT NULL AND CAST(planned_fix_time AS CHAR) != '' AND planned_fix_time < NOW() AND status NOT IN ('resolved', 'normal') THEN 1 ELSE 0 END),
			AVG(CASE WHEN response_time > 0 THEN response_time ELSE NULL END),
			MIN(CASE WHEN response_time > 0 THEN response_time ELSE NULL END),
			MAX(CASE WHEN response_time > 0 THEN response_time ELSE NULL END)
		FROM duty_records dr %s`, whereClause)
	database.DB.QueryRow(statusQuery, args...).Scan(&resolved, &inProgress, &pending, &temporary, &normal, &overdue, &avgResponse, &minResponse, &maxResponse)

	overview["total"] = total
	overview["resolved"] = resolved
	overview["in_progress"] = inProgress
	overview["pending"] = pending
	overview["temporary"] = temporary
	overview["normal"] = normal
	overview["overdue"] = overdue
	if avgResponse.Valid {
		overview["avg_response"] = int(avgResponse.Float64)
	}
	if minResponse.Valid {
		overview["min_response"] = int(minResponse.Float64)
	}
	if maxResponse.Valid {
		overview["max_response"] = int(maxResponse.Float64)
	}
	result["overview"] = overview

	// 按处理人统计
	byHandler := []map[string]interface{}{}
	handlerQuery := fmt.Sprintf(`
		SELECT COALESCE(handler, '未分配') as handler,
			COUNT(*) as total,
			SUM(CASE WHEN status='resolved' THEN 1 ELSE 0 END) as resolved,
			SUM(CASE WHEN status='in_progress' THEN 1 ELSE 0 END) as in_progress,
			SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status='temporary' THEN 1 ELSE 0 END) as temporary,
			SUM(CASE WHEN status='normal' THEN 1 ELSE 0 END) as normal,
			SUM(CASE WHEN planned_fix_time IS NOT NULL AND CAST(planned_fix_time AS CHAR) != '' AND planned_fix_time < NOW() AND status NOT IN ('resolved', 'normal') THEN 1 ELSE 0 END) as overdue
		FROM duty_records dr
		%s AND COALESCE(handler, '') != ''
		GROUP BY handler
		ORDER BY total DESC`, whereClause)
	rowsHandler, errHandler := database.DB.Query(handlerQuery, args...)
	if errHandler != nil {
		log.Printf("[统计] 按处理人统计查询失败: %v", errHandler)
	} else {
		for rowsHandler.Next() {
			var h string
			var t, r, ip, p, temp, n, o int
			rowsHandler.Scan(&h, &t, &r, &ip, &p, &temp, &n, &o)
			byHandler = append(byHandler, map[string]interface{}{
				"handler":     h,
				"total":       t,
				"resolved":    r,
				"in_progress": ip,
				"pending":     p,
				"temporary":   temp,
				"normal":      n,
				"overdue":     o,
			})
		}
		rowsHandler.Close()
	}
	result["by_handler"] = byHandler

	// 按值班人统计
	byDutyPerson := []map[string]interface{}{}
	dutyPersonQuery := fmt.Sprintf(`
		SELECT COALESCE(duty_person, '未分配') as duty_person,
			COUNT(*) as total,
			SUM(CASE WHEN status='resolved' THEN 1 ELSE 0 END) as resolved,
			SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status='normal' THEN 1 ELSE 0 END) as normal,
			SUM(CASE WHEN planned_fix_time IS NOT NULL AND CAST(planned_fix_time AS CHAR) != '' AND planned_fix_time < NOW() AND status NOT IN ('resolved', 'normal') THEN 1 ELSE 0 END) as overdue,
			SUM(CASE WHEN is_escalated=1 THEN 1 ELSE 0 END) as escalated,
			COALESCE(AVG(CASE WHEN response_time > 0 THEN response_time ELSE NULL END), 0) as avg_response
		FROM duty_records dr
		%s AND COALESCE(duty_person, '') != ''
		GROUP BY duty_person
		ORDER BY total DESC`, whereClause)
	rowsDutyPerson, errDutyPerson := database.DB.Query(dutyPersonQuery, args...)
	if errDutyPerson != nil {
		log.Printf("[统计] 按值班人统计查询失败: %v", errDutyPerson)
	} else {
		for rowsDutyPerson.Next() {
			var dp string
			var t, r, p, n, o, e int
			var avgResp float64
			rowsDutyPerson.Scan(&dp, &t, &r, &p, &n, &o, &e, &avgResp)
			byDutyPerson = append(byDutyPerson, map[string]interface{}{
				"duty_person":  dp,
				"total":        t,
				"resolved":     r,
				"pending":      p,
				"normal":       n,
				"overdue":      o,
				"escalated":    e,
				"avg_response": int(avgResp),
			})
		}
		rowsDutyPerson.Close()
	}
	result["by_duty_person"] = byDutyPerson

	// 按项目统计
	byProject := []map[string]interface{}{}
	projectQuery := fmt.Sprintf(`
		SELECT COALESCE(dp.name, '未知项目') as project_name,
			COUNT(*) as total,
			SUM(CASE WHEN dr.status='resolved' THEN 1 ELSE 0 END) as resolved,
			SUM(CASE WHEN dr.status='pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN dr.status='normal' THEN 1 ELSE 0 END) as normal,
			SUM(CASE WHEN dr.planned_fix_time IS NOT NULL AND CAST(dr.planned_fix_time AS CHAR) != '' AND dr.planned_fix_time < NOW() AND dr.status NOT IN ('resolved', 'normal') THEN 1 ELSE 0 END) as overdue
		FROM duty_records dr
		LEFT JOIN duty_projects dp ON dr.project_id = dp.id
		%s
		GROUP BY dr.project_id
		ORDER BY total DESC`, whereClause)
	rowsProject, errProject := database.DB.Query(projectQuery, args...)
	if errProject != nil {
		log.Printf("[统计] 按项目统计查询失败: %v", errProject)
	} else {
		for rowsProject.Next() {
			var pn string
			var t, r, p, n, o int
			rowsProject.Scan(&pn, &t, &r, &p, &n, &o)
			byProject = append(byProject, map[string]interface{}{
				"project":  pn,
				"total":    t,
				"resolved": r,
				"pending":  p,
				"normal":   n,
				"overdue":  o,
			})
		}
		rowsProject.Close()
	}
	result["by_project"] = byProject

	// 按事件类型统计
	byEventType := []map[string]interface{}{}
	eventTypeQuery := fmt.Sprintf(`
		SELECT event_type, COUNT(*) as count
		FROM duty_records dr
		%s AND event_type != ''
		GROUP BY event_type
		ORDER BY count DESC`, whereClause)
	rowsEventType, errEventType := database.DB.Query(eventTypeQuery, args...)
	if errEventType != nil {
		log.Printf("[统计] 按事件类型统计查询失败: %v", errEventType)
	} else {
		for rowsEventType.Next() {
			var et string
			var c int
			rowsEventType.Scan(&et, &c)
			byEventType = append(byEventType, map[string]interface{}{
				"event_type": et,
				"count":      c,
			})
		}
		rowsEventType.Close()
	}
	result["by_event_type"] = byEventType

	// 按反馈类型统计
	byFeedback := []map[string]interface{}{}
	feedbackQuery := fmt.Sprintf(`
		SELECT feedback_type, COUNT(*) as count
		FROM duty_records dr
		%s AND feedback_type != ''
		GROUP BY feedback_type
		ORDER BY count DESC`, whereClause)
	rowsFeedback, errFeedback := database.DB.Query(feedbackQuery, args...)
	if errFeedback != nil {
		log.Printf("[统计] 按反馈类型统计查询失败: %v", errFeedback)
	} else {
		for rowsFeedback.Next() {
			var ft string
			var c int
			rowsFeedback.Scan(&ft, &c)
			byFeedback = append(byFeedback, map[string]interface{}{
				"feedback_type": ft,
				"count":         c,
			})
		}
		rowsFeedback.Close()
	}
	result["by_feedback"] = byFeedback

	// 处理人拨打详情
	callDetails := []map[string]interface{}{}
	callQuery := fmt.Sprintf(`
		SELECT COALESCE(handler, '未分配') as handler,
			SUM(call_count) as total_calls,
			SUM(CASE WHEN is_answered='已接听' THEN 1 ELSE 0 END) as answered,
			SUM(CASE WHEN is_answered='未接听' THEN 1 ELSE 0 END) as not_answered,
			AVG(call_count) as avg_call_count,
			MIN(CASE WHEN response_time > 0 THEN response_time ELSE NULL END) as first_response,
			AVG(CASE WHEN response_time > 0 THEN response_time ELSE NULL END) as avg_response,
			MAX(CASE WHEN response_time > 0 THEN response_time ELSE NULL END) as max_response
		FROM duty_records dr
		%s AND COALESCE(handler, '') != ''
		GROUP BY handler
		ORDER BY total_calls DESC`, whereClause)
	rowsCall, errCall := database.DB.Query(callQuery, args...)
	if errCall != nil {
		log.Printf("[统计] 处理人拨打详情查询失败: %v", errCall)
	} else {
		for rowsCall.Next() {
			var h string
			var totalCalls, answered, notAnswered int
			var avgCallCount, firstResp, avgResp, maxResp sql.NullFloat64
			rowsCall.Scan(&h, &totalCalls, &answered, &notAnswered, &avgCallCount, &firstResp, &avgResp, &maxResp)
			
			answerRate := 0
			if answered+notAnswered > 0 {
				answerRate = answered * 100 / (answered + notAnswered)
			}
			
			detail := map[string]interface{}{
				"handler":      h,
				"total_calls":  totalCalls,
				"answered":     answered,
				"not_answered": notAnswered,
				"answer_rate":  answerRate,
			}
			if avgCallCount.Valid {
				detail["avg_call_count"] = fmt.Sprintf("%.1f", avgCallCount.Float64)
			}
			if firstResp.Valid {
				detail["first_response"] = int(firstResp.Float64)
			}
			if avgResp.Valid {
				detail["avg_response"] = int(avgResp.Float64)
			}
			if maxResp.Valid {
				detail["max_response"] = int(maxResp.Float64)
			}
			callDetails = append(callDetails, detail)
		}
		rowsCall.Close()
	}
	result["call_details"] = callDetails

	// 响应时长分布
	responseTime := []map[string]interface{}{}
	responseQuery := fmt.Sprintf(`
		SELECT 
			CASE 
				WHEN response_time <= 5 THEN '0-5分钟'
				WHEN response_time <= 15 THEN '5-15分钟'
				WHEN response_time <= 30 THEN '15-30分钟'
				WHEN response_time <= 60 THEN '30-60分钟'
				ELSE '60分钟以上'
			END as range_label,
			COUNT(*) as count
		FROM duty_records dr
		%s AND response_time > 0
		GROUP BY range_label
		ORDER BY MIN(response_time)`, whereClause)
	rowsResponse, errResponse := database.DB.Query(responseQuery, args...)
	if errResponse != nil {
		log.Printf("[统计] 响应时长分布查询失败: %v", errResponse)
	} else {
		for rowsResponse.Next() {
			var rl string
			var c int
			rowsResponse.Scan(&rl, &c)
			responseTime = append(responseTime, map[string]interface{}{
				"range": rl,
				"count": c,
			})
		}
		rowsResponse.Close()
	}
	result["response_time"] = responseTime

	// 按处理结果统计 - 从数据库动态查询
	byStatus := []map[string]interface{}{}
	statusLabelMap := map[string]string{
		"resolved":    "已解决",
		"pending":     "待解决",
		"in_progress": "正在解决",
		"processing":  "处理中",
		"temporary":   "临时解决",
		"normal":      "检测正常",
		"closed":      "已关闭",
		"escalated":   "已上报",
	}
	statusQuery2 := fmt.Sprintf(`
		SELECT status, COUNT(*) as cnt 
		FROM duty_records dr %s 
		GROUP BY status 
		ORDER BY cnt DESC`, whereClause)
	rowsStatus, errStatus := database.DB.Query(statusQuery2, args...)
	if errStatus != nil {
		log.Printf("[统计] 按状态统计查询失败: %v", errStatus)
	} else {
		defer rowsStatus.Close()
		for rowsStatus.Next() {
			var status string
			var cnt int
			rowsStatus.Scan(&status, &cnt)
			label := statusLabelMap[status]
			if label == "" {
				label = status
			}
			byStatus = append(byStatus, map[string]interface{}{
				"status": status,
				"label":  label,
				"count":  cnt,
			})
		}
	}
	result["by_status"] = byStatus

	// 每日趋势（最近30天）
	trend := []map[string]interface{}{}
	trendQuery := fmt.Sprintf(`
		SELECT DATE(duty_date) as day,
			COUNT(*) as total,
			SUM(CASE WHEN planned_fix_time IS NOT NULL AND CAST(planned_fix_time AS CHAR) != '' AND planned_fix_time < NOW() AND status NOT IN ('resolved', 'normal') THEN 1 ELSE 0 END) as overdue
		FROM duty_records dr
		%s
		GROUP BY DATE(duty_date)
		ORDER BY day DESC
		LIMIT 30`, whereClause)
	rowsTrend, errTrend := database.DB.Query(trendQuery, args...)
	if errTrend != nil {
		log.Printf("[统计] 每日趋势查询失败: %v", errTrend)
	} else {
		for rowsTrend.Next() {
			var day string
			var t, o int
			rowsTrend.Scan(&day, &t, &o)
			trend = append(trend, map[string]interface{}{
				"date":    day,
				"total":   t,
				"overdue": o,
			})
		}
		rowsTrend.Close()
	}
	result["trend"] = trend

	// 上报问题统计
	byEscalate := []map[string]interface{}{}
	escalateQuery := fmt.Sprintf(`
		SELECT escalate_to, COUNT(*) as count
		FROM duty_records dr
		%s AND is_escalated=1 AND escalate_to != ''
		GROUP BY escalate_to
		ORDER BY count DESC`, whereClause)
	rowsEscalate, errEscalate := database.DB.Query(escalateQuery, args...)
	if errEscalate != nil {
		log.Printf("[统计] 上报问题统计查询失败: %v", errEscalate)
	} else {
		for rowsEscalate.Next() {
			var name string
			var c int
			rowsEscalate.Scan(&name, &c)
			byEscalate = append(byEscalate, map[string]interface{}{
				"escalate_to": name,
				"count":       c,
			})
		}
		rowsEscalate.Close()
	}
	result["by_escalate"] = byEscalate

	// 未上报数量（补充完整的上报统计）
	var notEscalated int
	notEscalatedQuery := fmt.Sprintf(`SELECT COUNT(*) FROM duty_records dr %s AND (is_escalated=0 OR is_escalated IS NULL)`, whereClause)
	database.DB.QueryRow(notEscalatedQuery, args...).Scan(&notEscalated)
	result["not_escalated"] = notEscalated

	// 拨打次数分布（按次数分组）
	callDistribution := []map[string]interface{}{}
	callDistQuery := fmt.Sprintf(`
		SELECT 
			CASE 
				WHEN call_count = 1 THEN '1次接通'
				WHEN call_count = 2 THEN '2次接通'
				WHEN call_count = 3 THEN '3次接通'
				WHEN call_count > 3 THEN '3次以上'
				ELSE '未拨打'
			END as call_range,
			COUNT(*) as count,
			SUM(CASE WHEN is_answered='已接听' THEN 1 ELSE 0 END) as answered,
			SUM(CASE WHEN is_answered='未接听' THEN 1 ELSE 0 END) as not_answered
		FROM duty_records dr
		%s AND call_count > 0
		GROUP BY call_range
		ORDER BY call_count`, whereClause)
	rowsCallDist, errCallDist := database.DB.Query(callDistQuery, args...)
	if errCallDist != nil {
		log.Printf("[统计] 拨打次数分布查询失败: %v", errCallDist)
	} else {
		for rowsCallDist.Next() {
			var cr string
			var c, a, na int
			rowsCallDist.Scan(&cr, &c, &a, &na)
			callDistribution = append(callDistribution, map[string]interface{}{
				"range":        cr,
				"count":        c,
				"answered":     a,
				"not_answered": na,
			})
		}
		rowsCallDist.Close()
	}
	result["call_distribution"] = callDistribution

	// 平均拨打次数和接通率
	var avgCallCount sql.NullFloat64
	var totalAnswered, totalNotAnswered int
	callStatsQuery := fmt.Sprintf(`
		SELECT 
			AVG(CASE WHEN call_count > 0 THEN call_count ELSE NULL END),
			SUM(CASE WHEN is_answered='已接听' THEN 1 ELSE 0 END),
			SUM(CASE WHEN is_answered='未接听' AND call_count > 0 THEN 1 ELSE 0 END)
		FROM duty_records dr %s`, whereClause)
	database.DB.QueryRow(callStatsQuery, args...).Scan(&avgCallCount, &totalAnswered, &totalNotAnswered)
	callStats := map[string]interface{}{}
	if avgCallCount.Valid {
		callStats["avg_call_count"] = fmt.Sprintf("%.1f", avgCallCount.Float64)
	}
	if totalAnswered+totalNotAnswered > 0 {
		callStats["answer_rate"] = totalAnswered * 100 / (totalAnswered + totalNotAnswered)
	}
	callStats["total_answered"] = totalAnswered
	callStats["total_not_answered"] = totalNotAnswered
	result["call_stats"] = callStats

	respondJSON(w, http.StatusOK, result)
}

// HandleExportDutyRecords 导出值班记录
func HandleExportDutyRecords(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty:export") {
		return
	}
	rows, err := database.DB.Query(`
		SELECT dr.duty_date, dr.duty_person, COALESCE(dp.name,'') as project_name,
			COALESCE(dr.task_desc,''), dr.feedback_type, COALESCE(dr.handler,''), COALESCE(dr.handle_result,''), COALESCE(dr.solution,''), COALESCE(dr.problem_desc,''),
			COALESCE(dr.first_call_time,''), COALESCE(dr.answer_time,''), dr.call_count, dr.is_answered, dr.response_time,
			dr.is_escalated, COALESCE(dr.escalate_to,''),
			dr.has_handover, COALESCE(dr.handover_person,''), COALESCE(dr.handover_content,''),
			dr.status, COALESCE(dr.planned_fix_time,''), dr.is_overdue, COALESCE(dr.overdue_reason,'')
		FROM duty_records dr
		LEFT JOIN duty_projects dp ON dr.project_id = dp.id
		ORDER BY dr.duty_date DESC`)
	if err != nil {
		sendError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=duty_records_%s.csv", time.Now().Format("20060102")))
	w.Write([]byte("\xEF\xBB\xBF"))
	w.Write([]byte("值班日期,值班人,项目,任务描述,反馈类型,处理人,处理结果,解决方案,问题描述,首次拨打时间,接听时间,拨打次数,是否接听,响应时间(分钟),是否升级,升级给,是否交接,交接人,交接内容,状态,计划修复时间,是否逾期,逾期原因\n"))

	feedbackTypeMap := map[string]string{"proactive": "主动反馈", "customer": "客户反馈"}
	statusMap := map[string]string{"resolved": "已解决", "unresolved": "未解决", "pending": "待解决", "temporary": "临时解决"}
	escalateMap := map[string]string{"leader": "组长", "hod": "HOD"}
	boolMap := map[bool]string{true: "是", false: "否"}

	for rows.Next() {
		var dutyDate, dutyPerson, projectName, taskDesc, feedbackType, handler, handleResult, solution, problemDesc string
		var firstCallTime, answerTime string
		var callCount int
		var isAnswered bool
		var responseTime int
		var isEscalated bool
		var escalateTo string
		var hasHandover bool
		var handoverPerson, handoverContent string
		var status, plannedFixTime string
		var isOverdue bool
		var overdueReason string

		rows.Scan(&dutyDate, &dutyPerson, &projectName, &taskDesc, &feedbackType, &handler, &handleResult, &solution, &problemDesc,
			&firstCallTime, &answerTime, &callCount, &isAnswered, &responseTime,
			&isEscalated, &escalateTo, &hasHandover, &handoverPerson, &handoverContent,
			&status, &plannedFixTime, &isOverdue, &overdueReason)

		line := fmt.Sprintf("\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",%d,\"%s\",%d,\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\"\n",
			dutyDate, dutyPerson, projectName, taskDesc,
			feedbackTypeMap[feedbackType], handler, handleResult, solution, problemDesc,
			firstCallTime, answerTime, callCount, boolMap[isAnswered], responseTime,
			boolMap[isEscalated], escalateMap[escalateTo],
			boolMap[hasHandover], handoverPerson, handoverContent,
			statusMap[status], plannedFixTime, boolMap[isOverdue], overdueReason)
		w.Write([]byte(line))
	}
}

// HandleUploadDutyAttachment 上传值班记录附件
func HandleUploadDutyAttachment(w http.ResponseWriter, r *http.Request) {
	if !requireDutyPermission(w, r, "duty:upload") {
		return
	}
	err := r.ParseMultipartForm(32 << 20) // 32MB
	if err != nil {
		sendError(w, "解析表单失败", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		sendError(w, "未选择文件", http.StatusBadRequest)
		return
	}

	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true}
	contentTypes := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
		".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
	}
	urls := []string{}

	store := storage.GetStorage()
	ctx := r.Context()

	for _, fileHeader := range files {
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		if !allowedExts[ext] {
			sendError(w, fmt.Sprintf("不支持的文件类型: %s，仅支持图片格式", ext), http.StatusBadRequest)
			return
		}

		if fileHeader.Size > 10*1024*1024 {
			sendError(w, fmt.Sprintf("文件 %s 超过10MB限制", fileHeader.Filename), http.StatusBadRequest)
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			log.Printf("[上传] 打开文件失败: %v", err)
			continue
		}
		defer file.Close()

		objectName := fmt.Sprintf("duty/%s_%s%s", time.Now().Format("20060102150405"), uuid.New().String()[:8], ext)
		contentType := contentTypes[ext]
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		fileURL, err := store.Upload(ctx, objectName, file, fileHeader.Size, contentType)
		if err != nil {
			log.Printf("[上传] 存储文件失败: %v", err)
			continue
		}

		urls = append(urls, fileURL)
	}

	if len(urls) == 0 {
		sendError(w, "上传失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"urls":    urls,
		"message": fmt.Sprintf("成功上传 %d 个文件", len(urls)),
	})
}

// HandleGenerateTestDutyRecords 生成测试值班记录
func HandleGenerateTestDutyRecords(w http.ResponseWriter, r *http.Request) {
	countStr := r.URL.Query().Get("count")
	count := 20
	if countStr != "" {
		fmt.Sscanf(countStr, "%d", &count)
	}
	if count > 100 {
		count = 100
	}

	dutyPersons := []string{"张三", "李四", "王五", "赵六", "钱七"}
	handlers := []string{"技术A", "技术B", "运维C", "开发D", "测试E"}
	projects := []string{}
	eventTypes := []string{"error", "warning", "info", "critical", "notice"}
	feedbackTypes := []string{"customer", "internal", "monitor", "third_party"}
	statusList := []string{"resolved", "pending", "processing", "closed", "escalated"}
	taskDescs := []string{"服务器报警", "数据库异常", "接口超时", "用户投诉", "系统升级", "网络故障", "性能问题", "安全告警"}

	rows, err := database.DB.Query("SELECT id FROM duty_projects WHERE status='active' LIMIT 10")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			rows.Scan(&id)
			projects = append(projects, id)
		}
	}
	if len(projects) == 0 {
		projects = []string{"project-1", "project-2", "project-3"}
	}

	user := r.Header.Get("X-Operator")
	now := time.Now()
	inserted := 0

	for i := 0; i < count; i++ {
		id := uuid.New().String()
		dutyDate := now.AddDate(0, 0, -i%30).Format("2006-01-02 15:04:05")
		dutyPerson := dutyPersons[i%len(dutyPersons)]
		projectID := projects[i%len(projects)]
		taskDesc := taskDescs[i%len(taskDescs)]
		feedbackType := feedbackTypes[i%len(feedbackTypes)]
		eventType := eventTypes[i%len(eventTypes)]
		handler := handlers[i%len(handlers)]
		status := statusList[i%len(statusList)]
		problemDesc := fmt.Sprintf("测试问题描述 #%d - %s", i+1, taskDesc)
		handleResult := fmt.Sprintf("已处理: %s", problemDesc)
		callCount := (i % 5)
		isAnswered := callCount > 0 && i%3 != 0
		responseTime := (i%10 + 1) * 5
		isEscalated := i%7 == 0
		escalateTo := ""
		if isEscalated {
			escalateTo = "leader"
		}
		isOverdue := i%9 == 0
		plannedFixTime := now.AddDate(0, 0, -i%30+1).Format("2006-01-02") + " 23:59:59"
		createdAt := now.Format("2006-01-02 15:04:05")
		firstCallTime := ""
		answerTime := ""
		if callCount > 0 {
			firstCallTime = now.AddDate(0, 0, -i%30).Add(time.Duration(i%60) * time.Minute).Format("2006-01-02 15:04:05")
			if isAnswered {
				answerTime = now.AddDate(0, 0, -i%30).Add(time.Duration(i%60+responseTime) * time.Minute).Format("2006-01-02 15:04:05")
			}
		}

		_, err := database.DB.Exec(`INSERT INTO duty_records (
			id, duty_date, duty_person, project_id, task_description, feedback_type, event_type,
			handler, status, problem_description, handle_result, call_count, is_answered, 
			response_time, is_escalated, escalate_to, is_overdue, planned_fix_time, 
			first_call_time, answer_time, created_at, created_by, attachments
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, dutyDate, dutyPerson, projectID, taskDesc, feedbackType, eventType,
			handler, status, problemDesc, handleResult, callCount, isAnswered,
			responseTime, isEscalated, escalateTo, isOverdue, plannedFixTime,
			firstCallTime, answerTime, createdAt, user, "[]")
		if err != nil {
			log.Printf("[测试数据] 插入失败: %v", err)
			continue
		}
		inserted++
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("成功生成 %d 条测试数据", inserted),
		"count":   inserted,
	})
}
