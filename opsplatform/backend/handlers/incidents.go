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

// HandleGetIncidents 获取失误记录列表
func HandleGetIncidents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	opType := r.URL.Query().Get("type")
	operator := r.URL.Query().Get("operator")

	query := `SELECT id, incident_time, operator, operation_type, COALESCE(operation_desc,''), 
		status, severity, COALESCE(reason,''), COALESCE(impact,''), COALESCE(solution,''), 
		COALESCE(checker,''), check_time, COALESCE(check_result,''), COALESCE(remark,''),
		created_at, COALESCE(created_by,''), COALESCE(updated_at,''), COALESCE(updated_by,'')
		FROM incidents WHERE 1=1`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if opType != "" {
		query += " AND operation_type = ?"
		args = append(args, opType)
	}
	if operator != "" {
		query += " AND operator LIKE ?"
		args = append(args, "%"+operator+"%")
	}

	query += " ORDER BY incident_time DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[事件] 查询失败: %v", err)
		sendError(w, "查询失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	incidents := make([]models.Incident, 0)
	for rows.Next() {
		var inc models.Incident
		var checkTime sql.NullString
		err := rows.Scan(&inc.ID, &inc.IncidentTime, &inc.Operator, &inc.OperationType, &inc.OperationDesc,
			&inc.Status, &inc.Severity, &inc.Reason, &inc.Impact, &inc.Solution,
			&inc.Checker, &checkTime, &inc.CheckResult, &inc.Remark,
			&inc.CreatedAt, &inc.CreatedBy, &inc.UpdatedAt, &inc.UpdatedBy)
		if err != nil {
			log.Printf("扫描失误记录失败: %v", err)
			continue
		}
		if checkTime.Valid {
			inc.CheckTime = checkTime.String
		}
		incidents = append(incidents, inc)
	}

	respondJSON(w, http.StatusOK, incidents)
}

// HandleCreateIncident 创建失误记录
func HandleCreateIncident(w http.ResponseWriter, r *http.Request) {
	var inc models.Incident
	if err := json.NewDecoder(r.Body).Decode(&inc); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if inc.Operator == "" || inc.IncidentTime == "" {
		sendError(w, "操作人和发生时间为必填项", http.StatusBadRequest)
		return
	}

	inc.ID = uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	user := r.Header.Get("X-Operator")

	if inc.Status == "" {
		inc.Status = "pending"
	}
	if inc.Severity == "" {
		inc.Severity = "medium"
	}
	if inc.OperationType == "" {
		inc.OperationType = "other"
	}

	var checkTime interface{}
	if inc.CheckTime != "" {
		checkTime = inc.CheckTime
	} else {
		checkTime = nil
	}

	_, err := database.DB.Exec(`INSERT INTO incidents (id, incident_time, operator, operation_type, operation_desc, status, severity, reason, impact, solution, checker, check_time, check_result, remark, created_at, created_by, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inc.ID, inc.IncidentTime, inc.Operator, inc.OperationType, inc.OperationDesc,
		inc.Status, inc.Severity, inc.Reason, inc.Impact, inc.Solution,
		inc.Checker, checkTime, inc.CheckResult, inc.Remark,
		now, user, now, user)
	if err != nil {
		log.Printf("[事件] 创建失败: %v", err)
		sendError(w, "创建失败", http.StatusInternalServerError)
		return
	}

	inc.CreatedAt = now
	inc.CreatedBy = user
	respondJSON(w, http.StatusCreated, inc)
}

// HandleUpdateIncident 更新失误记录
func HandleUpdateIncident(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var inc models.Incident
	if err := json.NewDecoder(r.Body).Decode(&inc); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	user := r.Header.Get("X-Operator")
	now := time.Now().Format("2006-01-02 15:04:05")

	var checkTime interface{}
	if inc.CheckTime != "" {
		checkTime = inc.CheckTime
	} else {
		checkTime = nil
	}

	_, err := database.DB.Exec(`UPDATE incidents SET incident_time=?, operator=?, operation_type=?, operation_desc=?, status=?, severity=?, reason=?, impact=?, solution=?, checker=?, check_time=?, check_result=?, remark=?, updated_at=?, updated_by=? WHERE id=?`,
		inc.IncidentTime, inc.Operator, inc.OperationType, inc.OperationDesc,
		inc.Status, inc.Severity, inc.Reason, inc.Impact, inc.Solution,
		inc.Checker, checkTime, inc.CheckResult, inc.Remark,
		now, user, id)
	if err != nil {
		log.Printf("[事件] 更新失败: %v", err)
		sendError(w, "更新失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "更新成功"})
}

// HandleDeleteIncident 删除失误记录
func HandleDeleteIncident(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_, err := database.DB.Exec("DELETE FROM incidents WHERE id = ?", id)
	if err != nil {
		sendError(w, "删除失败", http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "删除成功"})
}

// HandleGetIncidentStats 获取失误统计
func HandleGetIncidentStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{}

	var total, pending, resolved, closed int
	database.DB.QueryRow("SELECT COUNT(*) FROM incidents").Scan(&total)
	database.DB.QueryRow("SELECT COUNT(*) FROM incidents WHERE status='pending'").Scan(&pending)
	database.DB.QueryRow("SELECT COUNT(*) FROM incidents WHERE status='resolved'").Scan(&resolved)
	database.DB.QueryRow("SELECT COUNT(*) FROM incidents WHERE status='closed'").Scan(&closed)

	stats["total"] = total
	stats["pending"] = pending
	stats["resolved"] = resolved
	stats["closed"] = closed

	// 本月统计
	thisMonth := time.Now().Format("2006-01")
	var monthTotal int
	database.DB.QueryRow("SELECT COUNT(*) FROM incidents WHERE incident_time LIKE ?", thisMonth+"%").Scan(&monthTotal)
	stats["this_month"] = monthTotal

	// 按类型统计
	typeStats := []map[string]interface{}{}
	rows, err := database.DB.Query("SELECT operation_type, COUNT(*) as cnt FROM incidents GROUP BY operation_type ORDER BY cnt DESC")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var opType string
			var cnt int
			rows.Scan(&opType, &cnt)
			typeStats = append(typeStats, map[string]interface{}{"type": opType, "count": cnt})
		}
	}
	stats["by_type"] = typeStats

	// 按操作人统计
	operatorStats := []map[string]interface{}{}
	rows2, err := database.DB.Query("SELECT operator, COUNT(*) as cnt FROM incidents GROUP BY operator ORDER BY cnt DESC LIMIT 10")
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var op string
			var cnt int
			rows2.Scan(&op, &cnt)
			operatorStats = append(operatorStats, map[string]interface{}{"operator": op, "count": cnt})
		}
	}
	stats["by_operator"] = operatorStats

	respondJSON(w, http.StatusOK, stats)
}

// HandleExportIncidents 导出失误记录
func HandleExportIncidents(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT incident_time, operator, operation_type, COALESCE(operation_desc,''), status, severity, COALESCE(reason,''), COALESCE(impact,''), COALESCE(solution,''), COALESCE(checker,''), COALESCE(check_time,''), COALESCE(check_result,''), COALESCE(remark,'') FROM incidents ORDER BY incident_time DESC`)
	if err != nil {
		sendError(w, "导出失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=incidents_%s.csv", time.Now().Format("20060102")))
	w.Write([]byte("\xEF\xBB\xBF")) // UTF-8 BOM
	w.Write([]byte("发生时间,操作人,操作类型,操作描述,状态,严重程度,异常原因,影响范围,解决方案,检查人,检查时间,检查结果,备注\n"))

	for rows.Next() {
		var t, op, opType, desc, st, sev, reason, impact, sol, checker, ct, cr, remark string
		rows.Scan(&t, &op, &opType, &desc, &st, &sev, &reason, &impact, &sol, &checker, &ct, &cr, &remark)
		line := fmt.Sprintf("\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\",\"%s\"\n",
			t, op, opType, desc, st, sev, reason, impact, sol, checker, ct, cr, remark)
		w.Write([]byte(line))
	}
}
