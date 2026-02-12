package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"opsplatform/database"
	"opsplatform/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// HandleGetTasks 获取任务列表
func HandleGetTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	project := r.URL.Query().Get("project")
	assignee := r.URL.Query().Get("assignee")
	priority := r.URL.Query().Get("priority")
	delayed := r.URL.Query().Get("delayed")       // "1" = 延期, "0" = 正常
	completion := r.URL.Query().Get("completion") // normal, delayed

	query := `SELECT id, project, title, source, category, priority, assignee, 
		start_time, end_time, status, COALESCE(result,''), COALESCE(remark,''),
		is_delayed, COALESCE(delay_reason,''), COALESCE(delay_desc,''), delay_end_time, 
		COALESCE(completion_type,''),
		created_at, COALESCE(created_by,''), COALESCE(updated_at,''), COALESCE(updated_by,'')
		FROM tasks WHERE 1=1`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if project != "" {
		query += " AND project = ?"
		args = append(args, project)
	}
	if assignee != "" {
		query += " AND assignee LIKE ?"
		args = append(args, "%"+assignee+"%")
	}
	if priority != "" {
		query += " AND priority = ?"
		args = append(args, priority)
	}
	if delayed == "1" {
		query += " AND is_delayed = 1"
	} else if delayed == "0" {
		query += " AND is_delayed = 0"
	}
	if completion == "normal" {
		query += " AND completion_type = 'normal'"
	} else if completion == "delayed" {
		query += " AND completion_type = 'delayed'"
	}

	query += " ORDER BY FIELD(priority, 'P0', 'P1', 'P2', 'P3'), created_at DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		sendError(w, "查询失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tasks := make([]models.Task, 0)
	for rows.Next() {
		var t models.Task
		var startTime, endTime, delayEndTime sql.NullString
		err := rows.Scan(&t.ID, &t.Project, &t.Title, &t.Source, &t.Category, &t.Priority, &t.Assignee,
			&startTime, &endTime, &t.Status, &t.Result, &t.Remark,
			&t.IsDelayed, &t.DelayReason, &t.DelayDesc, &delayEndTime,
			&t.CompletionType,
			&t.CreatedAt, &t.CreatedBy, &t.UpdatedAt, &t.UpdatedBy)
		if err != nil {
			log.Printf("扫描任务失败: %v", err)
			continue
		}
		if startTime.Valid {
			t.StartTime = startTime.String
		}
		if endTime.Valid {
			t.EndTime = endTime.String
		}
		if delayEndTime.Valid {
			t.DelayEndTime = delayEndTime.String
		}
		tasks = append(tasks, t)
	}

	respondJSON(w, http.StatusOK, tasks)
}

// HandleCreateTask 创建任务
func HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	var t models.Task
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if t.Title == "" {
		sendError(w, "需求描述不能为空", http.StatusBadRequest)
		return
	}

	t.ID = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	user := r.Header.Get("X-Operator")

	if t.Status == "" {
		t.Status = "pending"
	}
	if t.Priority == "" {
		t.Priority = "P2"
	}

	// 自动计算完成分类
	t.CompletionType = calcCompletionType(t)

	var startTime, endTime, delayEndTime interface{}
	if t.StartTime != "" {
		startTime = t.StartTime
	}
	if t.EndTime != "" {
		endTime = t.EndTime
	}
	if t.DelayEndTime != "" {
		delayEndTime = t.DelayEndTime
	}

	_, err := database.DB.Exec(`INSERT INTO tasks (id, project, title, source, category, priority, assignee, start_time, end_time, status, result, remark, is_delayed, delay_reason, delay_desc, delay_end_time, completion_type, created_at, created_by, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Project, t.Title, t.Source, t.Category, t.Priority, t.Assignee,
		startTime, endTime, t.Status, t.Result, t.Remark,
		t.IsDelayed, t.DelayReason, t.DelayDesc, delayEndTime, t.CompletionType,
		now, user, now, user)
	if err != nil {
		sendError(w, "创建失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	t.CreatedAt = now
	t.CreatedBy = user
	respondJSON(w, http.StatusCreated, t)
}

// HandleUpdateTask 更新任务
func HandleUpdateTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var t models.Task
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	user := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")

	// 自动计算完成分类
	t.CompletionType = calcCompletionType(t)

	var startTime, endTime, delayEndTime interface{}
	if t.StartTime != "" {
		startTime = t.StartTime
	}
	if t.EndTime != "" {
		endTime = t.EndTime
	}
	if t.DelayEndTime != "" {
		delayEndTime = t.DelayEndTime
	}

	_, err := database.DB.Exec(`UPDATE tasks SET project=?, title=?, source=?, category=?, priority=?, assignee=?, start_time=?, end_time=?, status=?, result=?, remark=?, is_delayed=?, delay_reason=?, delay_desc=?, delay_end_time=?, completion_type=?, updated_at=?, updated_by=? WHERE id=?`,
		t.Project, t.Title, t.Source, t.Category, t.Priority, t.Assignee,
		startTime, endTime, t.Status, t.Result, t.Remark,
		t.IsDelayed, t.DelayReason, t.DelayDesc, delayEndTime, t.CompletionType,
		now, user, id)
	if err != nil {
		sendError(w, "更新失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleBatchCreateTasks 批量创建任务
func HandleBatchCreateTasks(w http.ResponseWriter, r *http.Request) {
	var tasks []models.Task
	if err := json.NewDecoder(r.Body).Decode(&tasks); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}
	if len(tasks) == 0 {
		sendError(w, "任务列表不能为空", http.StatusBadRequest)
		return
	}

	user := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")
	successCount := 0
	failCount := 0

	for _, t := range tasks {
		if t.Title == "" {
			failCount++
			continue
		}
		t.ID = uuid.New().String()
		if t.Status == "" {
			t.Status = "pending"
		}
		if t.Priority == "" {
			t.Priority = "P2"
		}
		if t.Source == "" {
			t.Source = "other"
		}
		if t.Category == "" {
			t.Category = "feature"
		}
		t.CompletionType = calcCompletionType(t)

		var startTime, endTime, delayEndTime interface{}
		if t.StartTime != "" {
			startTime = t.StartTime
		}
		if t.EndTime != "" {
			endTime = t.EndTime
		}
		if t.DelayEndTime != "" {
			delayEndTime = t.DelayEndTime
		}

		_, err := database.DB.Exec(`INSERT INTO tasks (id, project, title, source, category, priority, assignee, start_time, end_time, status, result, remark, is_delayed, delay_reason, delay_desc, delay_end_time, completion_type, created_at, created_by, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.Project, t.Title, t.Source, t.Category, t.Priority, t.Assignee,
			startTime, endTime, t.Status, t.Result, t.Remark,
			t.IsDelayed, t.DelayReason, t.DelayDesc, delayEndTime, t.CompletionType,
			now, user, now, user)
		if err != nil {
			failCount++
			continue
		}
		successCount++
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success_count": successCount,
		"fail_count":    failCount,
		"message":       fmt.Sprintf("成功 %d 个，失败 %d 个", successCount, failCount),
	})
}

// HandleDeleteTask 删除任务
func HandleDeleteTask(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := database.DB.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// HandleGetTaskStats 获取任务统计
func HandleGetTaskStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{}

	var total, pending, inProgress, completed, delayed int
	database.DB.QueryRow("SELECT COUNT(*) FROM tasks").Scan(&total)
	database.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE status='pending'").Scan(&pending)
	database.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE status='in_progress'").Scan(&inProgress)
	database.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE status='completed'").Scan(&completed)
	database.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE is_delayed=1").Scan(&delayed)

	stats["total"] = total
	stats["pending"] = pending
	stats["in_progress"] = inProgress
	stats["completed"] = completed
	stats["delayed"] = delayed

	// 正常完成 vs 延期完成
	var normalComplete, delayedComplete int
	database.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE status='completed' AND completion_type='normal'").Scan(&normalComplete)
	database.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE status='completed' AND completion_type='delayed'").Scan(&delayedComplete)
	stats["normal_complete"] = normalComplete
	stats["delayed_complete"] = delayedComplete

	respondJSON(w, http.StatusOK, stats)
}

// HandleGetTaskProjects 获取任务项目列表
func HandleGetTaskProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query("SELECT DISTINCT project FROM tasks WHERE project != '' ORDER BY project")
	if err != nil {
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := make([]string, 0)
	for rows.Next() {
		var p string
		rows.Scan(&p)
		projects = append(projects, p)
	}

	respondJSON(w, http.StatusOK, projects)
}

// HandleExportTasks 导出任务
func HandleExportTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT project, title, source, category, priority, assignee, COALESCE(start_time,''), COALESCE(end_time,''), status, COALESCE(result,''), COALESCE(remark,''), is_delayed, COALESCE(delay_reason,''), COALESCE(delay_desc,''), COALESCE(delay_end_time,''), COALESCE(completion_type,'') FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		sendError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=tasks_%s.csv", time.Now().Format("20060102")))
	w.Write([]byte("\xEF\xBB\xBF"))
	w.Write([]byte("项目,需求描述,需求来源,任务分类,优先级,负责人,开始时间,结束时间,状态,结果,备注,是否延期,延期原因,延期说明,延期结束时间,完成分类\n"))

	for rows.Next() {
		var proj, title, src, cat, pri, assignee, st, et, status, result, remark, delayReason, delayDesc, delayEnd, compType string
		var isDelayed bool
		rows.Scan(&proj, &title, &src, &cat, &pri, &assignee, &st, &et, &status, &result, &remark, &isDelayed, &delayReason, &delayDesc, &delayEnd, &compType)
		delayed := "否"
		if isDelayed {
			delayed = "是"
		}
		line := fmt.Sprintf("\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\"\n",
			proj, title, src, cat, pri, assignee, st, et, status, result, remark, delayed, delayReason, delayDesc, delayEnd, compType)
		w.Write([]byte(line))
	}
}

// calcCompletionType 自动计算完成分类
func calcCompletionType(t models.Task) string {
	if t.Status != "completed" {
		return ""
	}
	if t.IsDelayed {
		return "delayed"
	}
	return "normal"
}
